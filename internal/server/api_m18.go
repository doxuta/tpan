package server

// API của M18: đích sao lưu S3, đẩy, tải về, liệt kê kho, sinh user-data
// cloud-init (mục 3.11 của docs/thay-doi/M18-thiet-ke.md, R1, R6, R9, R15,
// R16 của bản soát hẹp ở mục 9.3). Bảng đường dẫn và kiểu ở types_m18.go;
// luật (đều admin) ở routes.go; mã HTTP 400 mới ở apiErrorStatus (api.go).
//
// Ba luật bí mật (B3) mà mọi handler ở đây giữ:
//   - secret_key bọc vào system.Secret ngay sau khi đọc thân; từ đó nó chỉ đi
//     vào core, JSON của Secret luôn là "••••".
//   - Nhật ký (audit) không bao giờ ghi thân request: target là
//     "<endpoint> <bucket> <tiền tố>" đã qua kiểm, "?" khi chưa qua kiểm, hoặc
//     id bản sao lưu đã qua backupIDRe.
//   - Không route nào nhận hay trả khoá sao lưu; đặt đích qua API không sinh nó.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

const (
	// targetBodyMax: thân của POST /backups/target có CA tối đa 64 KB và hai
	// khoá; 128 KiB là đủ mà không cho đẩy thân lớn vào bộ nhớ (S30).
	targetBodyMax = 128 << 10
	// cloudInitBodyMax: 1,5 MiB, đủ cho danh sách site tối đa của sites.yaml.
	cloudInitBodyMax = 3 << 19
	// remoteLockWaitAPI: đặt, gỡ đích qua API chờ khoá lượt đẩy tối đa chừng
	// này rồi core từ chối job_running (409). Lượt đẩy có thể chạy hàng giờ;
	// request không được treo theo nó.
	remoteLockWaitAPI = 10 * time.Second
)

// apiRemote dựng ca sử dụng kho S3 trên Deps. System bọc jobLockSystem như
// apiSites: job đặt, gỡ, thử đích lấy khoá job của máy qua engine, và với API
// thì chờ có hạn. Job đẩy, tải, gói chạy trên heldLockSystem của core nên lớp
// bọc này không đổi gì với chúng (B10).
func (s *Server) apiRemote() *core.Remote {
	st := s.deps.Store
	lock := s.remoteLock
	if lock == nil {
		lock = system.FileLock{Path: system.DefaultRemoteLockPath}
	}
	return &core.Remote{
		Sys:      jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
		Settings: st, Store: st, Sites: st, Journal: st,
		Snapshot: st.SnapshotDB,
		Hostname: s.deps.Hostname,
		Version:  buildinfo.Version,
		NewStore: s.newS3Store,
		Lock:     lock,
		LockWait: remoteLockWaitAPI,
		Now:      s.deps.Now,
	}
}

// ---------------------------------------------------------------- đích

// handleBackupTarget trả phần xem của đích: không khoá nào, kể cả dạng che,
// ngoài bốn ký tự cuối của access key (core.TargetView).
func (s *Server) handleBackupTarget(w http.ResponseWriter, r *http.Request) {
	v, err := s.apiRemote().Target(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// targetInput đổi thân request thành đầu vào của core. secret_key thành
// system.Secret ở đây, trước mọi việc khác.
//   - Hai khoá rỗng: giữ hai khoá đang lưu (KeepKeys). Core chỉ cho giữ khi
//     endpoint, vùng, bucket, CA không đổi (R1), và kiểm TRƯỚC khi dựng kho,
//     nên không request nào ký bằng khoá cũ tới được một host mới.
//   - ca_pem "": giữ CA đang có; "none": bỏ CA riêng; còn lại: đặt CA mới.
//   - path_style thiếu: true (kho tự dựng như MinIO, Garage chỉ hiểu kiểu này).
//   - GenerateKey luôn false: khoá sao lưu chỉ sinh ở CLI (S5).
func targetInput(body TargetRequest) core.TargetInput {
	in := core.TargetInput{
		Endpoint: body.Endpoint, Region: body.Region, Bucket: body.Bucket, Prefix: body.Prefix,
		PathStyle: body.PathStyle == nil || *body.PathStyle,
		Keep:      body.Keep,
		KeepKeys:  body.AccessKey == "" && body.SecretKey == "",
		AccessKey: body.AccessKey,
		SecretKey: system.NewSecret(body.SecretKey),
	}
	switch body.CAPEM {
	case "":
		in.CA = core.CAKeep
	case "none":
		in.CA = core.CANone
	default:
		in.CA, in.CAPEM = core.CASet, []byte(body.CAPEM)
	}
	return in
}

// targetAudit là target của nhật ký: "<endpoint> <bucket> <tiền tố>" khi cả ba
// qua kiểm (cùng dạng với Plan.Target của core), "?" khi không. Giá trị chưa
// qua kiểm có thể là bất cứ thứ gì người gọi gõ, kể cả một khoá dán nhầm ô.
func targetAudit(in core.TargetInput, hostname string) string {
	ep, err := s3.CheckEndpoint(in.Endpoint)
	if err != nil || s3.CheckBucket(in.Bucket) != nil {
		return "?"
	}
	prefix := in.Prefix
	if prefix == "" {
		prefix = s3.DefaultPrefix(hostname)
	}
	if s3.CheckPrefix(prefix) != nil {
		return "?"
	}
	return ep + " " + in.Bucket + " " + prefix
}

// handleBackupTargetSet đặt hay đổi đích (Plan backup.target.set). Điều kiện
// đầu của core thử thật tới kho (Check), cả lúc ?dry_run=1.
func (s *Server) handleBackupTargetSet(w http.ResponseWriter, r *http.Request) {
	var body TargetRequest
	if !apiReadJSONMax(w, r, &body, targetBodyMax) {
		return
	}
	in := targetInput(body)
	rem := s.apiRemote()
	s.planJob(w, r, "backup.target.set", targetAudit(in, s.deps.Hostname), func(ctx context.Context) (apiPlan, error) {
		ts, err := rem.PrepareSetTarget(ctx, in)
		if err != nil {
			return nil, err
		}
		return ts, nil
	})
}

// handleBackupTargetRemove gỡ đích (Plan backup.target.rm). Không xoá gì trên
// kho, không xoá khoá sao lưu.
func (s *Server) handleBackupTargetRemove(w http.ResponseWriter, r *http.Request) {
	rem := s.apiRemote()
	s.planJob(w, r, "backup.target.rm", "s3", func(ctx context.Context) (apiPlan, error) {
		p, err := rem.PrepareRemoveTarget(ctx)
		if err != nil {
			return nil, err
		}
		return p, nil
	})
}

// handleBackupTargetTest thử đích đang lưu (Plan backup.target.test, một
// S3Probe): kết quả nằm trong job, xem ở Lịch sử.
func (s *Server) handleBackupTargetTest(w http.ResponseWriter, r *http.Request) {
	rem := s.apiRemote()
	s.planJob(w, r, "backup.target.test", "s3", func(ctx context.Context) (apiPlan, error) {
		p, err := rem.PrepareTestTarget(ctx)
		if err != nil {
			return nil, err
		}
		return p, nil
	})
}

// ---------------------------------------------------------------- đẩy, tải

// backupAuditID là id để ghi nhật ký: đúng dạng thì chính nó, không thì "?"
// (chuỗi lạ không vào nhật ký nguyên dạng).
func backupAuditID(id string) string {
	if backupIDRe.MatchString(id) {
		return id
	}
	return "?"
}

// previewPlan trả bản xem trước của một Plan core vừa dựng, qua writePreview
// như mọi bản xem trước khác (M28, bất biến B12): route hôm nay chỉ admin gọi
// được nên bộ lọc theo vai không đổi gì, nhưng route mở cho vai user về sau
// vẫn tự có lọc.
func (s *Server) previewPlan(w http.ResponseWriter, r *http.Request, p *core.Planned, err error) {
	if err != nil {
		apiFail(w, r, err)
		return
	}
	pv, err := p.Preview(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	s.writePreview(w, r, pv)
}

// pushNote là câu của 202: việc chưa chạy, và hàng chờ mất nếu panel khởi
// động lại trước lượt kế tiếp.
var pushNote = i18n.T(
	"Đã xếp hàng: chạy ở lượt đẩy kế tiếp; mất nếu panel khởi động lại trước đó; xem kết quả ở Lịch sử.",
	"Queued: runs in the next upload run; lost if the panel restarts before then; see the result in History.")

// handleBackupPush xếp một bản vào hàng chờ của bộ đẩy nền (pusher.go). Không
// chạy job trong request: đẩy vài GB mất hàng giờ, và job đẩy chỉ chạy dưới
// khoá lượt đẩy do lượt đẩy giữ.
func (s *Server) handleBackupPush(w http.ResponseWriter, r *http.Request) {
	const action = "backup.push"
	id := r.PathValue("id")
	rem := s.apiRemote()
	if apiDryRun(r) {
		p, err := rem.PreparePush(r.Context(), id)
		s.previewPlan(w, r, p, err)
		return
	}
	target := backupAuditID(id)
	// Kiểm ngay trong request (đã đặt đích, bản có, đầy đủ, còn trên máy) để
	// lời từ chối về thẳng người gọi thay vì chỉ nằm trong log của bộ đẩy.
	if _, err := rem.PreparePush(r.Context(), id); err != nil {
		s.jobRefused(w, r, action, target, err)
		return
	}
	// Người bấm "đẩy" muốn thử lại cả bản đã lỗi đủ năm lần (như `tpan backup
	// push <id>`): đặt lại số đếm trước khi vào hàng chờ.
	if err := s.deps.Store.ResetBackupTries(r.Context(), id); err != nil {
		apiFail(w, r, err)
		return
	}
	s.pusher.enqueue(id)
	s.audit(r, action, target, "queued", "")
	writeJSON(w, http.StatusAccepted, QueuedResponse{Queued: true, Note: pushNote})
}

// pullNote là câu của 202 của tải về.
var pullNote = i18n.T(
	"Đang tải về ở nền (chờ lượt đẩy đang chạy nếu có); xem kết quả ở Lịch sử, rồi khôi phục.",
	"Downloading in the background (after any upload run in progress); see the result in History, then restore.")

// handleBackupPull tải một bản từ kho về máy. Không dùng claim, jobStart: tải
// về không giữ khoá job của máy (B10), nên không được giữ chỗ "đang có job"
// duy nhất của API suốt lúc tải (có thể hàng giờ); nó chỉ chờ khoá lượt đẩy
// trong core.Remote.Pull (R6).
func (s *Server) handleBackupPull(w http.ResponseWriter, r *http.Request) {
	const action = "backup.pull"
	id := r.PathValue("id")
	rem := s.apiRemote()
	if apiDryRun(r) {
		p, err := rem.PreparePull(r.Context(), id)
		s.previewPlan(w, r, p, err)
		return
	}
	target := backupAuditID(id)
	// Kiểm trước để lời từ chối (backup_already_local, backup_not_on_machine,
	// s3_not_configured…) về thẳng người gọi. Pull dựng lại Plan sau khi giữ
	// khoá: trong lúc chờ, dọn kho có thể đã đổi dòng.
	if _, err := rem.PreparePull(r.Context(), id); err != nil {
		s.jobRefused(w, r, action, target, err)
		return
	}
	// Chờ khoá lượt đẩy tới core.RemoteLockWait (30 phút) thay vì 10 giây của
	// đặt đích: người cần khôi phục chờ lượt đẩy đang chạy xong (R6a).
	rem.LockWait = 0
	// ctx giữ giá trị của request (người chạy: job ghi cột actor) nhưng không
	// huỷ theo request; dừng khi server dừng.
	ar := r.WithContext(context.WithoutCancel(r.Context()))
	ctx, cancel := context.WithCancel(ar.Context())
	stop := context.AfterFunc(s.bgCtx(), cancel)
	s.api.wg.Go(func() {
		defer cancel()
		defer stop()
		rep, err := rem.Pull(ctx, id, nil)
		result := "ok"
		switch {
		case err != nil:
			result = "error"
			var re *core.RefusedError
			if errors.As(err, &re) {
				result = re.Code
			}
			if ctx.Err() == nil {
				slog.Error("tải bản sao lưu từ kho", "id", target, "err", s3.CleanMessage(err.Error()))
			}
		case rep.Job.Status != engine.JobSucceeded:
			result = string(rep.Job.Status)
		}
		s.audit(ar, action, target, result, rep.Job.ID)
	})
	writeJSON(w, http.StatusAccepted, QueuedResponse{Queued: true, Note: pullNote})
}

// handleBackupRemote liệt kê trên kho, ghép với bảng backups.
func (s *Server) handleBackupRemote(w http.ResponseWriter, r *http.Request) {
	l, err := s.apiRemote().ListRemote(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	l.Objects, l.Missing = apiNonNil(l.Objects), apiNonNil(l.Missing)
	writeJSON(w, http.StatusOK, l)
}

// ---------------------------------------------------------------- cloud-init

// handleCloudInitRender sinh user-data chế độ site mới. Không đổi gì trên máy.
// Server không tải gì từ base (S12): hai sha256 bắt buộc, core từ chối
// cloudinit_invalid khi thiếu (AllowFetch false). Mật khẩu quản trị của máy
// mới có đúng một lần trong câu trả lời này; user-data chỉ có bản băm.
func (s *Server) handleCloudInitRender(w http.ResponseWriter, r *http.Request) {
	const action = "cloudinit.render"
	var body CloudInitRequest
	if !apiReadJSONMax(w, r, &body, cloudInitBodyMax) {
		return
	}
	ci := core.CloudInit{Sys: s.deps.Sys, Version: buildinfo.Version, Hash: store.HashPassword}
	res, err := ci.Render(r.Context(), core.CloudInitInput{
		Web: core.WebServer(body.Web), Private: body.Private, Sites: body.Sites, Base: body.Base,
		SumAMD64: body.SHA256AMD64, SumARM64: body.SHA256ARM64, MaxBytes: body.MaxBytes,
	})
	if err != nil {
		s.jobRefused(w, r, action, "", err)
		return
	}
	s.audit(r, action, "", "ok", "")
	// Câu trả lời mang mật khẩu: không để trình duyệt hay proxy giữ lại.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, CloudInitResponse{
		UserData: string(res.UserData), Bytes: res.Bytes, AdminPassword: res.AdminPassword.Reveal(),
	})
}
