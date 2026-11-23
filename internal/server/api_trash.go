package server

// API của M6: thùng rác và hàng loạt (đường dẫn và quy ước ở types_m6.go).
//
//   - Khôi phục và xoá hẳn một site trong thùng rác là job một Plan, cùng
//     khuôn planJob với SSL (api_ssl.go). DELETE /sites/{domain} (đưa vào
//     thùng rác) ở api_job.go.
//   - Số ngày giữ chỉ ghi bảng settings, không đụng máy: không phải job.
//   - POST /apply là job hàng loạt (engine.ExecuteBatch) rồi các job SSL của
//     pha D; xem handleApply về việc chỉ gắn job đầu vào SSE.
//   - GET /export chỉ đọc bảng sites.
//
// Bộ hẹn giờ dọn thùng rác ở trash_timer.go.

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// ---------------------------------------------------------------- thùng rác

// handleSiteRestore lấy một site ra khỏi thùng rác. Từ chối (409) nếu site
// không ở thùng rác hoặc domain đã được tạo lại ngoài tpan; lời từ chối của
// core về thẳng người gọi kèm checks.
func (s *Server) handleSiteRestore(w http.ResponseWriter, r *http.Request) {
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	sites := s.apiSites()
	// Kick đúng domain: site vừa sống lại, health kiểm nó ngay thay vì chờ
	// vòng 5 phút.
	s.planJobKick(w, r, jobOpts{action: "site.restore", target: d.String(), kick: d.String()}, func(ctx context.Context) (apiPlan, error) {
		return sites.PrepareRestore(ctx, d)
	})
}

// handleTrashPurge xoá hẳn một site đang ở thùng rác (không hoàn tác được).
// Site đang active thì core từ chối site_not_trashed: xoá hẳn chỉ đi qua
// thùng rác.
func (s *Server) handleTrashPurge(w http.ResponseWriter, r *http.Request) {
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	sites := s.apiSites()
	s.planJob(w, r, "site.purge", d.String(), func(ctx context.Context) (apiPlan, error) {
		return sites.PreparePurge(ctx, d)
	})
}

// trashOverview đọc số ngày giữ, các site và DB rời đang ở thùng rác của
// scope (rỗng là của mọi người, scopeOf).
func (s *Server) trashOverview(ctx context.Context, scope string) (TrashOverview, error) {
	sites := s.apiSites()
	days, err := sites.TrashDays(ctx)
	if err != nil {
		return TrashOverview{}, err
	}
	list, err := sites.Trash(ctx)
	if err != nil {
		return TrashOverview{}, err
	}
	ov := TrashOverview{Days: days, Sites: []site.Site{}, DBs: []account.Database{}}
	for _, st := range list {
		// Store luôn ghi chủ (rỗng thành admin), nên so thẳng được.
		if scope == "" || st.Owner == scope {
			ov.Sites = append(ov.Sites, st)
		}
	}
	dbs, err := s.deps.Store.StandaloneDBs(ctx, scope)
	if err != nil {
		return TrashOverview{}, err
	}
	for _, db := range dbs {
		if db.Status == account.DBTrashed {
			ov.DBs = append(ov.DBs, db)
		}
	}
	return ov, nil
}

func (s *Server) handleTrash(w http.ResponseWriter, r *http.Request) {
	ov, err := s.trashOverview(r.Context(), scopeOf(r))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

// handleTrashSettings đặt số ngày giữ. Giống handleSSLSettings: chỉ ghi
// settings nên không giành chỗ chạy job. Số ngày mới chỉ áp cho site vào
// thùng rác sau lần đặt (core.Sites.SetTrashDays).
func (s *Server) handleTrashSettings(w http.ResponseWriter, r *http.Request) {
	const action = "trash.settings"
	var body TrashSettings
	if !apiReadJSON(w, r, &body) {
		return
	}
	target := strconv.Itoa(body.Days)
	if err := s.apiSites().SetTrashDays(r.Context(), body.Days); err != nil {
		_, eb := apiErrorStatus(err)
		s.audit(r, action, target, eb.Error.Code, "")
		apiFail(w, r, err)
		return
	}
	s.audit(r, action, target, "ok", "")
	ov, err := s.trashOverview(r.Context(), scopeOf(r))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

// ---------------------------------------------------------------- hàng loạt

// handleApply áp một danh sách site (mục 7): ?dry_run=1 trả core.ApplyPreview
// (kể cả khi không có gì để làm, để giao diện hiện vì sao từng dòng bị bỏ qua
// hay bị từ chối); chạy thật thì 409 nothing_to_do khi Empty, không thì 202.
func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	const action = "apply"
	var body ApplyRequest
	// Trần như sites.yaml: 500 dòng không vừa 64 KiB của các request khác.
	if !apiReadJSONMax(w, r, &body, core.MaxSitesFileBytes) {
		return
	}
	list, err := body.Resolve()
	if err != nil {
		apiFail(w, r, err)
		return
	}
	// M13: user thường chỉ chạm site của mình (core coi site của người khác như
	// không tồn tại, site mới thuộc người gọi); admin là mọi site.
	in := core.ApplyInput{Sites: list, Prune: body.Prune, Scope: scopeOf(r)}
	// Nhật ký ghi số dòng chứ không ghi từng domain: lô tới 500 site, và từng
	// site đã có trong job (sự kiện unit.done).
	target := fmt.Sprintf("%d site", len(list))
	if body.Prune {
		target += ", prune"
	}
	sites := s.apiSites()

	if apiDryRun(r) {
		ap, err := sites.PrepareApply(r.Context(), in)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		p, err := ap.Preview(r.Context())
		if err != nil {
			apiFail(w, r, err)
			return
		}
		p.Items = apiNonNil(p.Items)
		if p.Preview != nil {
			if err := s.filterPreview(r, p.Preview, applyCreated(p.Items)...); err != nil {
				apiFail(w, r, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, p)
		return
	}

	if !s.api.claim() {
		s.jobBusy(w, r, action, target)
		return
	}
	ap, err := sites.PrepareApply(r.Context(), in)
	if err != nil {
		s.api.release()
		s.jobRefused(w, r, action, target, err)
		return
	}
	if ap.Empty() {
		s.api.release()
		s.audit(r, action, target, "nothing_to_do", "")
		writeError(w, http.StatusConflict, "nothing_to_do", i18n.T(
			"Không có gì để làm: mọi site trong danh sách đã có, hoặc không tạo được (xem bản xem trước để biết vì sao).",
			"Nothing to do: every site in the list already exists or cannot be created (the preview says why)."))
		return
	}
	s.jobStart(w, r, jobOpts{action: action, target: target}, applyJob(ap))
}

// applyJob chạy Apply.Execute như một job của API.
//
// Execute chạy nhiều job: job hàng loạt trước, rồi mỗi site có SSL một job SSL
// (pha D), mỗi job một JobID. jobStart lấy job id từ sự kiện đầu tiên và đẩy
// mọi sự kiện nó nhận vào SSE của job đó, nên ở đây chỉ chuyển sự kiện của job
// đầu (job hàng loạt): sự kiện của job SSL lẫn vào sẽ làm luồng SSE có hai
// job.start, hai job.done và Seq chạy lại từ 1. Job SSL có trang riêng ở Lịch
// sử (types_m6.go).
//
// Mật khẩu chỉ có trong ApplyResult, tức là sau cả pha D; GET /jobs/{id} của
// job hàng loạt (chờ goroutine của job xong để không mất mật khẩu) vì vậy chờ
// cả các job SSL.
func applyJob(ap *core.Apply) jobFunc {
	return func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
		res, err := ap.Execute(ctx, firstJobOnly(onEvent))
		// Report nil: job hàng loạt không chạy được hay không xong; status
		// rỗng thì jobStart ghi "error" vào nhật ký.
		var out jobResult
		if res.Report != nil {
			out.status = res.Report.Job.Status
		}
		// Kể cả khi err khác nil, mật khẩu đã có vẫn phải tới người dùng (như
		// site.add): mất nó là mất tài khoản quản trị.
		for _, c := range res.Created {
			if !c.WPInstalled {
				continue
			}
			out.secrets = append(out.secrets, AdminSecret{
				URL:      "http://" + c.Domain + "/wp-admin/",
				User:     c.AdminUser,
				Password: c.AdminPassword.Reveal(),
			})
		}
		return out, err
	}
}

// firstJobOnly bọc onEvent để chỉ chuyển sự kiện của job đầu tiên nó thấy.
// Có mutex vì engine gọi onEvent từ goroutine của nó; mỗi job tự xếp hàng
// sự kiện của mình, nhưng hàm này không dựa vào điều đó.
func firstJobOnly(onEvent func(engine.Event)) func(engine.Event) {
	var mu sync.Mutex
	first := ""
	return func(ev engine.Event) {
		mu.Lock()
		if first == "" {
			first = ev.JobID
		}
		mine := ev.JobID == first
		mu.Unlock()
		if mine {
			onEvent(ev)
		}
	}
}

// handleExport trả sites.yaml dựng từ trạng thái hiện tại (mọi site active),
// dạng file tải về.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	f, err := s.apiSites().ExportScope(r.Context(), scopeOf(r))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	b, err := f.Marshal()
	if err != nil {
		apiFail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="sites.yaml"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}
