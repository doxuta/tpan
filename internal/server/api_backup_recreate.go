package server

// Khôi phục một bản backup khi site của nó đã bị xoá hẳn (không còn trong bảng
// sites). Trước bản này POST /backups/{id}/restore trả site_not_found, nên bản
// "before-purge" mà tpan tự tạo lúc xoá hẳn không dùng được từ panel.
//
// Cách làm: tạo lại site cùng tên miền (user Linux, DB, pool, vhost, WordPress
// mới) rồi khôi phục bản backup lên nó. Là hai job nối nhau trong một lần
// bấm: job_id trả về là của job tạo site; job khôi phục chạy ngay sau và hiện
// ở Lịch sử. Trong lúc đó panel vẫn báo "đang có việc khác" cho thao tác khác.
// wp-config.php của bản backup được thay bằng bản của site mới (mật khẩu DB
// mới), như mọi lần khôi phục.

import (
	"context"
	"errors"
	"net/http"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/validate"
)

// backupRecreate xử lý POST /backups/{id}/restore khi site d không còn.
// rowOwner là chủ ghi trên dòng backup.
func (s *Server) backupRecreate(w http.ResponseWriter, r *http.Request, d validate.Domain, id, rowOwner string) {
	const action = "backup.restore"
	ctx := r.Context()
	b := s.apiBackups()
	refused := func(err error) {
		if apiDryRun(r) {
			apiFail(w, r, err)
			return
		}
		s.jobRefused(w, r, action, d.String(), err)
	}
	if err := b.CheckRestoreSource(ctx, d, id); err != nil {
		refused(err)
		return
	}
	// Chủ của site tạo lại: user thường là chính họ (guard đã kiểm họ là chủ
	// bản backup); admin thì trả site về chủ cũ nếu user đó còn, không thì admin.
	me := who(r)
	owner := me.Name
	if me.IsAdmin() {
		owner = rowOwner
		if _, found, err := s.deps.Store.User(ctx, owner); err != nil {
			apiFail(w, r, err)
			return
		} else if !found {
			owner = account.AdminName
		}
	}
	// WordPress mới cần một email quản trị; DB của nó bị bản backup thay ngay
	// sau đó nên email này không ở lại.
	email, err := validate.ParseEmail("admin@" + d.String())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	sites := s.apiSites()
	sites.NotAdmin = !me.IsAdmin()
	in := core.SiteInput{Domain: d, Email: email, Owner: owner}

	if apiDryRun(r) {
		add, err := sites.PrepareAdd(ctx, in)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		pv, err := add.Preview(ctx)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		pv.Kind = action
		pv.Title.Vi = "Site " + d.String() + " đã xoá hẳn: tạo lại site (các bước dưới đây) rồi khôi phục mã nguồn và DB từ bản backup " + id + ". Chạy thành hai việc nối nhau, việc khôi phục hiện ở Lịch sử"
		pv.Title.En = "Site " + d.String() + " was deleted for good: create it again (the steps below), then restore its code and database from backup " + id + ". This runs as two jobs in a row; the restore shows in History"
		s.writePreview(w, r, pv, d)
		return
	}

	if !s.api.claim() {
		s.jobBusy(w, r, action, d.String())
		return
	}
	add, err := sites.PrepareAdd(ctx, in)
	if err != nil {
		s.api.release()
		s.jobRefused(w, r, action, d.String(), err)
		return
	}
	s.jobStart(w, r, jobOpts{action: action, target: d.String(), kick: d.String()}, func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
		res, err := add.Execute(ctx, onEvent)
		if err != nil || res.Report.Job.Status != engine.JobSucceeded {
			return jobResult{status: res.Report.Job.Status}, err
		}
		pl, err := b.PrepareRestore(ctx, d, id)
		if err != nil {
			return jobResult{status: engine.JobRolledBack}, errors.Join(errors.New("đã tạo lại site "+d.String()+" nhưng chưa khôi phục được bản backup "+id+"; bấm Khôi phục lần nữa ở bản đó"), err)
		}
		// Sự kiện của job thứ hai không đưa vào luồng của job đầu (khác job id).
		rep, err := pl.Execute(ctx, nil)
		return jobResult{status: rep.Job.Status}, err
	})
}
