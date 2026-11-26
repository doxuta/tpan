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
	"fmt"
	"net/http"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
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
	// M29: dựng lại đúng site cũ theo những gì dòng backup nhớ: loại site, bản
	// PHP (nếu máy còn bản đó), các tên phụ. SSL không nhớ được: chứng chỉ đã
	// bị gỡ cùng site, phải xin lại.
	row, _, err := s.deps.Store.Backup(ctx, id)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	origin, _, err := s.deps.Store.BackupOrigin(ctx, id)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	app, aliases := origin.App, origin.Aliases
	in.App = site.App(app)
	phpNote := ""
	if v := row.PHPVersion; v != "" {
		ok, err := sites.PHPAvailable(v)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		if ok {
			in.PHPVersion = v
		} else {
			phpNote = v
		}
	}
	// Tên www mặc định: giữ đúng kiểu cũ, hoặc không thêm nếu site cũ không có.
	// Dòng backup có trước M29 không ghi tên phụ nào: để mặc định như tạo site mới.
	var extra []store.BackupAlias
	if app != "" {
		in.WWW = core.WWWNone
		for _, a := range aliases {
			if a.Name == "www."+d.String() {
				in.WWW = site.NameMode(a.Mode)
				continue
			}
			extra = append(extra, a)
		}
	}

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
		pv.Title.Vi = "Site " + d.String() + " đã xoá hẳn: tạo lại site (các bước dưới đây) rồi khôi phục mã nguồn và DB từ bản backup " + id + ". Chạy thành các việc nối nhau, việc khôi phục hiện ở Lịch sử"
		pv.Title.En = "Site " + d.String() + " was deleted for good: create it again (the steps below), then restore its code and database from backup " + id + ". This runs as jobs in a row; the restore shows in History"
		var backVi, backEn []string
		if len(extra) > 0 {
			names := make([]string, len(extra))
			for i, a := range extra {
				names[i] = a.Name
			}
			backVi = append(backVi, "tên phụ cũ ("+strings.Join(names, ", ")+")")
			backEn = append(backEn, "the old alias domains ("+strings.Join(names, ", ")+")")
		}
		if n := len(origin.Rules); n > 0 {
			backVi = append(backVi, fmt.Sprintf("%d luật chuyển hướng cũ", n))
			backEn = append(backEn, fmt.Sprintf("%d old redirect rule(s)", n))
		}
		if len(backVi) > 0 {
			pv.Title.Vi += ". Sau đó thêm lại " + strings.Join(backVi, " và ")
			pv.Title.En += ". Then these are added back: " + strings.Join(backEn, " and ")
		}
		if phpNote != "" {
			pv.Title.Vi += ". Site cũ chạy PHP " + phpNote + " nhưng máy không còn bản đó, nên site mới dùng bản mặc định"
			pv.Title.En += ". The old site ran PHP " + phpNote + ", which this machine no longer has, so the new site uses the default version"
		}
		pv.Title.Vi += ". SSL không khôi phục được: xin lại ở trang site sau khi xong"
		pv.Title.En += ". SSL cannot be restored: request it again on the site page afterwards"
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
		out := jobResult{status: rep.Job.Status}
		// Site PHP trống tạo lại có user DB mới: mật khẩu của nó phải tới tay
		// người bấm, kể cả khi bước khôi phục hỏng (site mới vẫn còn đó).
		if pw := res.DBPassword.Reveal(); pw != "" {
			out.dbSecret = &DBSecret{DB: res.DBName, User: res.DBName, Password: pw}
		}
		if err != nil || rep.Job.Status != engine.JobSucceeded {
			return out, err
		}
		// Thêm lại từng tên phụ cũ, mỗi tên một job riêng (hiện ở Lịch sử). Một
		// tên không thêm lại được (đã thuộc site khác, vhost khác đang khai...)
		// không làm hỏng việc khôi phục: ghi nhật ký rồi làm tên kế tiếp.
		for _, a := range extra {
			target := d.String() + " " + a.Name
			n, perr := validate.ParseDomain(a.Name)
			if perr == nil {
				var op *core.NamesOp
				if op, perr = sites.PrepareAliasAdd(ctx, d, n, site.NameMode(a.Mode)); perr == nil {
					nres, xerr := op.Execute(ctx, nil)
					result := "ok"
					if xerr != nil {
						result = "error"
					} else if nres.Report.Job.Status != engine.JobSucceeded {
						result = string(nres.Report.Job.Status)
					}
					s.auditAs(ctx, clientIP(r), "site.alias.add", target, result, nres.Report.Job.ID)
					continue
				}
			}
			_, body := apiErrorStatus(perr)
			s.auditAs(ctx, clientIP(r), "site.alias.add", target, body.Error.Code, "")
		}
		// Rồi các luật chuyển hướng cũ, cùng cách.
		for _, ru := range origin.Rules {
			target := d.String() + " " + ru.Source
			op, perr := sites.PrepareRedirectAdd(ctx, d, core.RedirectInput{Source: ru.Source, Target: ru.Target, Code: ru.Code, KeepQuery: ru.KeepQuery != 0})
			if perr != nil {
				_, body := apiErrorStatus(perr)
				s.auditAs(ctx, clientIP(r), "site.redirect.add", target, body.Error.Code, "")
				continue
			}
			nres, xerr := op.Execute(ctx, nil)
			result := "ok"
			if xerr != nil {
				result = "error"
			} else if nres.Report.Job.Status != engine.JobSucceeded {
				result = string(nres.Report.Job.Status)
			}
			s.auditAs(ctx, clientIP(r), "site.redirect.add", target, result, nres.Report.Job.ID)
		}
		return out, nil
	})
}
