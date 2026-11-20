package server

// API của M15: nút cập nhật TPAN trong panel (mục 6.14 của docs/KE-HOACH.md;
// bảng đường dẫn và quy ước ở types_m15.go).
//
// KHÔNG handler nào ở file này đọc thân request (không gọi apiReadJSON, không
// đụng r.Body): trình duyệt chỉ nói "kiểm", "chạy", "quay về". Nguồn tải là
// settings update.self_url hoặc hằng core.DefaultSelfURL, phiên bản là thứ nằm
// ở nguồn tải. Thứ duy nhất đọc từ request là hai cờ dry_run=1 và auto=1 (so
// với đúng chữ "1"), người đang đăng nhập và IP của kết nối; hai thứ sau đi
// vào settings và nhật ký, không vào dòng lệnh nào.
//
// Panel không tự chạy Plan self.update: core.SelfUpdate.Start nhờ systemd khởi
// một đơn vị tạm rồi trả về ngay (vì sao: đầu file core/types_m15.go). Vì vậy
// các handler ở đây không dùng jobStart, không có job id, không có SSE: câu
// trả lời 202 mang lượt chạy vừa ghi, kết quả đọc lại ở GET /api/v1/self.

import (
	"net/http"
	"runtime"
	"sync"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
)

// apiSelf dựng ca sử dụng self-update. System không bọc jobLockSystem: Start
// không chạy Plan nào trong tiến trình này (đơn vị tạm tự lấy khoá của nó).
func (s *Server) apiSelf() *core.SelfUpdate {
	st := s.deps.Store
	return &core.SelfUpdate{Sys: s.deps.Sys, Settings: st, Journal: st, Arch: runtime.GOARCH, Version: buildinfo.Version}
}

// selfChecks nhớ lần "Kiểm bản mới" gần nhất của tiến trình này, kể cả lần
// không hỏi được nguồn tải. Chỉ nằm trong bộ nhớ: panel khởi động lại (cũng là
// lúc vừa cập nhật xong) thì quên, và lần kiểm sau hỏi lại nguồn tải. Zero
// value dùng được.
type selfChecks struct {
	mu sync.Mutex
	// last không bao giờ bị sửa sau khi nhớ (set thay cả con trỏ), nên get trả
	// thẳng con trỏ mà không cần chép.
	last *SelfCheckInfo
}

func (c *selfChecks) get() *SelfCheckInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func (c *selfChecks) set(info SelfCheckInfo) {
	c.mu.Lock()
	c.last = &info
	c.mu.Unlock()
}

// selfState gom câu trả lời của GET /api/v1/self. Không ra mạng và không chạy
// file cũ: đọc settings, kiểm file .old có không, hỏi systemd một lệnh.
func (s *Server) selfState(r *http.Request) (SelfState, error) {
	ctx, su := r.Context(), s.apiSelf()
	st, err := su.Status(ctx)
	if err != nil {
		return SelfState{}, err
	}
	busy, err := su.Busy(ctx)
	if err != nil {
		return SelfState{}, err
	}
	out := SelfState{SelfStatus: st, Busy: busy, Check: s.selfCheck.get()}
	last, ok, err := su.Last(ctx)
	if err != nil {
		return SelfState{}, err
	}
	if ok {
		// Dấu "running" còn đó mà đơn vị tạm không chạy: nó bị giết giữa chừng
		// hoặc máy tắt trước khi kịp ghi kết quả. Chỉ đổi trong câu trả lời,
		// không ghi vào settings: lượt chạy tay sau đó vẫn đọc được dấu gốc.
		if last.Status == core.SelfRunning && !busy && s.now().Sub(last.Started) > selfInterruptAfter {
			last.Status = core.SelfInterrupted
		}
		out.Last = &last
	}
	return out, nil
}

func (s *Server) writeSelfState(w http.ResponseWriter, r *http.Request) {
	st, err := s.selfState(r)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleSelf: GET /api/v1/self.
func (s *Server) handleSelf(w http.ResponseWriter, r *http.Request) { s.writeSelfState(w, r) }

// handleSelfUpdate: POST /api/v1/self/update. Có dry_run=1 là nút "Kiểm bản
// mới" (hai lệnh curl tới nguồn tải, không tải file chạy); không có là chạy.
func (s *Server) handleSelfUpdate(w http.ResponseWriter, r *http.Request) {
	if !apiDryRun(r) {
		s.selfStart(w, r, core.SelfActionUpdate)
		return
	}
	// auto=1 (dòng phiên bản cuối trang tự gọi mỗi lần mở panel): lần kiểm còn
	// trong hạn thì dùng lại, kể cả lần lỗi, để máy không ra mạng được không
	// phải chờ hết thời gian chờ của curl ở mọi lần tải trang. Người bấm nút
	// (không có auto) thì luôn hỏi mới.
	fresh := false
	if r.URL.Query().Get("auto") == "1" {
		if c := s.selfCheck.get(); c != nil && s.now().Sub(c.CheckedAt) <= SelfCheckTTL {
			fresh = true
		}
	}
	if !fresh {
		c, err := s.apiSelf().Check(r.Context())
		if r.Context().Err() != nil {
			// Người gọi đã đóng trang giữa lúc kiểm: lỗi này không nói gì về
			// nguồn tải, nên không nhớ nó (không thì auto=1 trả lại "lỗi" đó
			// suốt 6 giờ). Không còn ai đọc câu trả lời.
			return
		}
		info := SelfCheckInfo{SelfCheck: c, CheckedAt: s.now()}
		if err != nil {
			// Không phải lỗi HTTP: màn vẫn cần version, has_old, last, và hiện
			// lý do ngay dưới nút. Check đã điền current, url, asset.
			info.Error = &i18n.Text{
				Vi: "Không hỏi được nguồn tải: " + err.Error(),
				En: "Could not reach the download source: " + err.Error(),
			}
		}
		s.selfCheck.set(info)
	}
	s.writeSelfState(w, r)
}

// handleSelfRollback: POST /api/v1/self/rollback. Có dry_run=1 là bản xem
// trước cho hộp xác nhận (tên bản đang chạy và bản sẽ quay về); lời từ chối
// (self_no_old, self_rollback_db) ra 409 ngay ở đây để hộp không mời bấm thứ
// chắc chắn bị từ chối.
func (s *Server) handleSelfRollback(w http.ResponseWriter, r *http.Request) {
	if !apiDryRun(r) {
		s.selfStart(w, r, core.SelfActionRollback)
		return
	}
	p, err := s.apiSelf().RollbackPreview(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// selfStart là phần chạy thật của hai nút, theo thứ tự ở types_m15.go. action
// là hằng do handler truyền (core.SelfActionUpdate | core.SelfActionRollback),
// không phải chữ của request. Mỗi câu trả lời để lại đúng một dòng nhật ký:
// "started" hoặc mã từ chối.
func (s *Server) selfStart(w http.ResponseWriter, r *http.Request, action string) {
	// Target của nhật ký là bản đang chạy lúc bấm; dòng kết quả do đơn vị tạm
	// ghi sau đó mang "<bản cũ> -> <bản mới>".
	act, target := "self."+action, buildinfo.Version
	if s.deps.Demo {
		const code = "demo_no_self_update"
		s.audit(r, act, target, code, "")
		writeError(w, http.StatusConflict, code, i18n.T(
			"Bản demo chạy trên máy giả, không có gì để cập nhật hay quay về. Trên máy chủ thật, nút này thay file chạy của tpan rồi khởi động lại panel.",
			"The demo runs on a simulated machine, so there is nothing to update or roll back. On a real server this button replaces the tpan executable and restarts the panel."))
		return
	}
	// Panel đang có job thì không cho bấm: job đó sẽ bị cắt khi panel khởi
	// động lại. Giữ chỗ trong lúc gọi Start để không job nào chen vào giữa
	// lúc kiểm và lúc khởi đơn vị tạm; nhả ngay sau đó, vì lượt chạy nằm ở
	// tiến trình khác và lượt lỗi (hay bản tải về trùng bản đang chạy) không
	// khởi động lại panel.
	if !s.api.claim() {
		s.jobBusy(w, r, act, target)
		return
	}
	// actor là người thật đang thao tác (guard đã gắn, routes.go). Actor và IP
	// đi vào settings update.self_last cho lệnh nội bộ đọc lại; dòng lệnh của
	// đơn vị tạm là hằng.
	run, err := s.apiSelf().Start(r.Context(), action, engine.ActorFrom(r.Context()), clientIP(r), s.now())
	s.api.release()
	if err != nil {
		s.jobRefused(w, r, act, target, err)
		return
	}
	s.audit(r, act, target, "started", "")
	writeJSON(w, http.StatusAccepted, SelfStarted{Run: run})
}
