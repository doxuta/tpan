package core

// Thân các hàm của SelfUpdate thêm ở M15: nút cập nhật TPAN trong panel (mục
// 6.14 của docs/KE-HOACH.md; hợp đồng và sơ đồ ở types_m15.go).
//
// Hai nửa, chạy ở hai tiến trình khác nhau:
//   - Check, Busy, Last, RollbackPreview, Start chạy trong `tpan serve`. Start
//     không chạy Plan nào: nó ghi "ai bấm" vào settings rồi nhờ systemd khởi
//     một đơn vị tạm.
//   - Run chạy trong đơn vị tạm đó (`tpan internal self-update`), ở cgroup
//     riêng nên sống qua bước `systemctl restart tpan`. Nó chạy đúng Plan của
//     `tpan self-update` rồi ghi kết quả vào settings cho màn đọc lại.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

// Dòng mô tả của đơn vị tạm (hiện trong `systemctl status` và journal). Chữ cố
// định, không dấu để journal ở máy nào cũng đọc được.
const (
	selfUnitDescUpdate   = "tpan: cap nhat tu panel"
	selfUnitDescRollback = "tpan: quay ve ban truoc tu panel"
)

// selfOutputMax là số ký tự tối đa của output một lệnh được chép vào lời từ
// chối (lời đó hiện trên màn hình và nằm trong settings).
const selfOutputMax = 300

// oneLine gom s thành một dòng (mọi khoảng trắng, xuống dòng thành một dấu
// cách) và cắt còn tối đa limit ký tự.
func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}

// selfUnitCmd là dòng lệnh khởi đơn vị tạm cho action. Mọi chữ ở đây là hằng:
// không tham số nào của Start (người bấm, IP) đi vào argv, nên không có gì từ
// request tới được dòng lệnh chạy bằng root. action lạ thì lỗi.
func selfUnitCmd(action string) (system.Cmd, error) {
	args := []string{"--unit", SelfUnit, "--collect", "--description"}
	switch action {
	case SelfActionUpdate:
		args = append(args, selfUnitDescUpdate, TpanBin, "internal", "self-update")
	case SelfActionRollback:
		args = append(args, selfUnitDescRollback, TpanBin, "internal", "self-update", "--rollback")
	default:
		return system.Cmd{}, fmt.Errorf("loại lượt chạy %q không hợp lệ (chỉ %s hoặc %s)", action, SelfActionUpdate, SelfActionRollback)
	}
	return system.Cmd{Name: "systemd-run", Args: args}, nil
}

// prepare dựng Plan của action như CLI: self.update với gốc tải lấy từ
// settings, hoặc self.rollback.
func (s *SelfUpdate) prepare(ctx context.Context, action string) (*Planned, error) {
	if action == SelfActionRollback {
		return s.PrepareRollback(ctx)
	}
	return s.Prepare(ctx, "")
}

// versionOf chạy `<bin> version` và trả chữ thứ hai của dòng "tpan <bản>
// (commit ..., build ...)". Rỗng khi file không chạy được hoặc in dòng lạ.
func (s *SelfUpdate) versionOf(ctx context.Context, bin string) string {
	res, err := s.Sys.Run(ctx, system.Cmd{Name: bin, Args: []string{"version"}})
	if f := strings.Fields(res.Stdout); err == nil && len(f) >= 2 && f[0] == "tpan" {
		return f[1]
	}
	return ""
}

// saveRun ghi run vào SettingSelfLast và trả đúng chuỗi JSON đã ghi.
func (s *SelfUpdate) saveRun(ctx context.Context, run SelfRun) (string, error) {
	b, err := json.Marshal(run)
	if err != nil {
		return "", fmt.Errorf("ghi lượt chạy vào setting %s: %w", SettingSelfLast, err)
	}
	if err := s.Settings.SetSetting(ctx, SettingSelfLast, string(b)); err != nil {
		return "", fmt.Errorf("ghi lượt chạy vào setting %s: %w", SettingSelfLast, err)
	}
	return string(b), nil
}

func selfBusyRefusal() *RefusedError {
	return refuse(CodeSelfBusy, i18n.T(
		"một lượt cập nhật hoặc quay về tpan đang chạy (đơn vị "+SelfUnit+"); đợi nó xong rồi thử lại. Xem nó đang làm gì: journalctl -u "+SelfUnit,
		"a tpan update or rollback is already running (unit "+SelfUnit+"); wait for it to finish, then try again. To see what it is doing: journalctl -u "+SelfUnit))
}

// Check hỏi gốc tải xem bản đang phát hành là gì: bản xem trước của nút "Cập
// nhật". Chỉ đọc máy và hai lệnh curl (steps.SelfLatest); không tải file chạy.
//
// Không hỏi được gốc tải thì trả lỗi thường (không phải RefusedError), kèm
// SelfCheck đã điền Current, URL, Asset để người gọi vẫn hiện được "đang chạy
// bản nào, hỏi ở đâu".
//
// Máy không có file ở TpanBin (panel chạy thử từ chỗ khác, chế độ demo) không
// phải lỗi: khi đó không có sha256 để so, UpToDate chỉ dựa vào tên bản.
func (s *SelfUpdate) Check(ctx context.Context) (SelfCheck, error) {
	c := SelfCheck{Current: s.Version, Asset: "tpan-linux-" + s.Arch}
	base, err := s.baseURL(ctx)
	if err != nil {
		return c, err
	}
	c.URL = base
	sum, latest, err := steps.SelfLatest(ctx, s.Sys, base, s.Arch)
	if err != nil {
		return c, fmt.Errorf("hỏi gốc tải bản phát hành: %w", err)
	}
	c.SHA256, c.Latest = sum, latest

	// sha256 tính trong Go từ chính file đang cài, như SelfDownload làm với
	// file tải về: cùng một phép so ở cả hai đầu.
	data, err := s.Sys.ReadFile(TpanBin)
	switch {
	case err == nil:
		installed := sha256.Sum256(data)
		c.UpToDate = hex.EncodeToString(installed[:]) == sum
	case !errors.Is(err, fs.ErrNotExist):
		return c, fmt.Errorf("đọc %s để so sha256 với bản ở gốc tải: %w", TpanBin, err)
	}
	if latest != "" && latest == s.Version {
		c.UpToDate = true
	}
	return c, nil
}

// Busy: đơn vị tạm SelfUnit đang chạy? Hỏi `systemctl is-active`: nó in đúng
// một chữ trạng thái và thoát mã khác 0 khi đơn vị không chạy (kể cả khi đơn
// vị không tồn tại), nên mã thoát khác 0 là câu trả lời, không phải lỗi. Lỗi
// chỉ khi không chạy nổi systemctl.
func (s *SelfUpdate) Busy(ctx context.Context) (bool, error) {
	res, err := s.Sys.Run(ctx, system.Cmd{Name: "systemctl", Args: []string{"is-active", SelfUnit + ".service"}})
	if ee := (*system.ExitError)(nil); err != nil && !errors.As(err, &ee) {
		return false, fmt.Errorf("hỏi systemd về đơn vị %s: %w", SelfUnit, err)
	}
	switch strings.TrimSpace(res.Stdout) {
	case "active", "activating", "deactivating":
		return true, nil
	}
	return false, nil
}

// Last đọc lượt chạy gần nhất từ SettingSelfLast. ok false khi chưa có lượt
// nào; JSON hỏng hoặc thiếu Action, Status cũng coi như chưa có, để một giá
// trị lạ trong settings không làm hỏng cả màn.
func (s *SelfUpdate) Last(ctx context.Context) (SelfRun, bool, error) {
	v, ok, err := s.Settings.GetSetting(ctx, SettingSelfLast)
	if err != nil {
		return SelfRun{}, false, fmt.Errorf("đọc setting %s: %w", SettingSelfLast, err)
	}
	var run SelfRun
	if !ok || json.Unmarshal([]byte(v), &run) != nil || run.Action == "" || run.Status == "" {
		return SelfRun{}, false, nil
	}
	return run, true, nil
}

// RollbackPreview trả tên bản đang chạy và bản sẽ quay về, cho hộp xác nhận
// của nút "Quay về bản trước". Từ chối đúng như PrepareRollback (self_no_old,
// CodeSelfRollbackDB): hộp xác nhận không mời bấm thứ chắc chắn bị từ chối.
// Old rỗng khi file cũ không in được dòng phiên bản.
func (s *SelfUpdate) RollbackPreview(ctx context.Context) (SelfRollbackPreview, error) {
	if _, err := s.PrepareRollback(ctx); err != nil {
		return SelfRollbackPreview{}, err
	}
	return SelfRollbackPreview{Current: s.Version, Old: s.versionOf(ctx, tpanOld)}, nil
}

// Start là phần chạy trong `tpan serve` khi người dùng bấm nút: kiểm điều
// kiện, ghi dấu "đang chạy" kèm người bấm vào settings, rồi nhờ systemd khởi
// đơn vị tạm. Trả về ngay khi đơn vị đã khởi, không đợi lượt chạy xong. Thứ tự
// từng bước ở types_m15.go.
func (s *SelfUpdate) Start(ctx context.Context, action, actor, ip string, now time.Time) (SelfRun, error) {
	cmd, err := selfUnitCmd(action)
	if err != nil {
		return SelfRun{}, err
	}
	busy, err := s.Busy(ctx)
	if err != nil {
		return SelfRun{}, err
	}
	if busy {
		return SelfRun{}, selfBusyRefusal()
	}
	// Dựng Plan chỉ để lấy lời từ chối (self_no_old, self_rollback_db,
	// self_url_invalid) về thẳng người bấm; Plan thật do đơn vị tạm dựng lại.
	// Prepare không tải gì: trước lần tải đầu nó chỉ kiểm gốc tải và kiến trúc.
	if _, err := s.prepare(ctx, action); err != nil {
		return SelfRun{}, err
	}

	prev, hadPrev, err := s.Settings.GetSetting(ctx, SettingSelfLast)
	if err != nil {
		return SelfRun{}, fmt.Errorf("đọc setting %s: %w", SettingSelfLast, err)
	}
	run := SelfRun{Action: action, Status: SelfRunning, From: s.Version, Actor: actor, IP: ip, Started: now}
	// Ghi trước khi khởi: lệnh nội bộ đọc người bấm từ đây ngay khi nó chạy.
	mark, err := s.saveRun(ctx, run)
	if err != nil {
		return SelfRun{}, err
	}

	res, err := s.Sys.Run(ctx, cmd)
	if err == nil {
		return run, nil
	}

	// systemd từ chối đơn vị thứ hai cùng tên: hai người bấm cùng tích tắc,
	// người sau lọt qua Busy ở trên nhưng dừng ở đây.
	refusal := selfBusyRefusal()
	if !strings.Contains(res.Stderr, "already loaded") {
		reason := oneLine(res.Stderr, selfOutputMax)
		if reason == "" {
			reason = oneLine(err.Error(), selfOutputMax)
		}
		byHand := "sudo tpan self-update"
		if action == SelfActionRollback {
			byHand += " --rollback"
		}
		refusal = refuse(CodeSelfStart, i18n.T(
			"không khởi được đơn vị tạm "+SelfUnit+" bằng systemd-run: "+reason+". Chưa có gì thay đổi; vẫn làm được ở dòng lệnh: "+byHand,
			"could not start the transient unit "+SelfUnit+" with systemd-run: "+reason+". Nothing has changed; it can still be done on the command line: "+byHand))
	}

	// Xoá dấu "đang chạy" vừa ghi, nếu không màn sẽ chờ một lượt không có thật.
	// Chỉ khi settings còn đúng là dấu đó: lượt đang chạy của người kia có thể
	// vừa ghi kết quả của nó vào đây, và kết quả đó không được bị đè. Ghi bằng
	// ctx không huỷ được, để request bị ngắt giữa chừng không để dấu lại. Ghi
	// lỗi thì dấu còn đó và API sẽ trả "interrupted" (SelfInterrupted).
	bg := context.WithoutCancel(ctx)
	if cur, _, gerr := s.Settings.GetSetting(bg, SettingSelfLast); gerr == nil && cur == mark {
		if hadPrev {
			_ = s.Settings.SetSetting(bg, SettingSelfLast, prev)
		} else {
			run.Status, run.Error, run.Finished = SelfFailed, refusal.Message.Vi, now
			_, _ = s.saveRun(bg, run)
		}
	}
	return SelfRun{}, refusal
}

// Run là thân của `tpan internal self-update [--rollback]`, chạy TRONG đơn vị
// tạm. Thứ tự từng bước ở types_m15.go.
//
// Trả SelfRun đã ghi vào settings và Report của job (cho CLI in). Job hỏng
// không phải lỗi của Run: xem run.Status. Lỗi trả về có ba trường hợp:
//   - action lạ hoặc không đọc được settings: chưa làm gì, SelfRun rỗng;
//   - Plan bị từ chối lúc dựng: kết quả (failed) ĐÃ được ghi, và chính
//     *RefusedError đó được trả kèm để người gọi lấy mã từ chối cho audit;
//   - job đã chạy nhưng không ghi được kết quả vào settings.
func (s *SelfUpdate) Run(ctx context.Context, action string, now func() time.Time, onEvent func(engine.Event)) (SelfRun, engine.Report, error) {
	if _, err := selfUnitCmd(action); err != nil {
		return SelfRun{}, engine.Report{}, err
	}
	last, ok, err := s.Last(ctx)
	if err != nil {
		return SelfRun{}, engine.Report{}, err
	}
	var run SelfRun
	if ok && last.Status == SelfRunning && last.Action == action {
		// Dấu Start vừa ghi: lượt này do người đó bấm trong panel.
		run = SelfRun{Action: action, From: last.From, Actor: last.Actor, IP: last.IP, Started: last.Started}
	} else {
		// Ai đó gõ lệnh nội bộ bằng tay trên máy chủ.
		run = SelfRun{Action: action, From: s.Version, IP: "cli", Started: now()}
	}
	ctx = engine.WithActor(ctx, run.Actor)
	// Đơn vị tạm có thể bị dừng giữa chừng (systemctl stop, máy tắt): ctx bị
	// huỷ, job hoàn tác. Phần đọc lại bản đang cài và ghi kết quả vẫn phải
	// chạy, nếu không màn treo ở "đang chạy".
	bg := context.WithoutCancel(ctx)

	var (
		rep     engine.Report
		refusal *RefusedError
	)
	p, err := s.prepare(ctx, action)
	if err != nil {
		run.Status, run.Error = SelfFailed, err.Error()
		if errors.As(err, &refusal) {
			run.Error = refusal.Message.Vi
		}
	} else {
		rep, err = p.Execute(ctx, onEvent)
		run.JobID = rep.Job.ID
		run.To = s.versionOf(bg, TpanBin)
		switch {
		case rep.Job.Status != engine.JobSucceeded:
			run.Status, run.Error = SelfFailed, rep.Job.Error
			if run.Error == "" && err != nil {
				run.Error = err.Error()
			}
			if run.Error == "" {
				run.Error = fmt.Sprintf("job %s kết thúc ở trạng thái %s", rep.Job.ID, rep.Job.Status)
			}
		case action == SelfActionUpdate && run.To == run.From:
			// Bản tải về trùng bản đang chạy: Plan đã bỏ qua bước thay file.
			run.Status = SelfSame
		default:
			run.Status = SelfOK
		}
	}
	run.Finished = now()
	if _, err := s.saveRun(bg, run); err != nil {
		return run, rep, err
	}
	if refusal != nil {
		return run, rep, refusal
	}
	return run, rep, nil
}
