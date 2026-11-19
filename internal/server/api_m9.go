package server

// API của M9: backup và khôi phục (mục 6.5), thao tác DB (mục 6.9), cron
// (mục 6.10) của docs/KE-HOACH.md. Bảng đường dẫn và quy ước ở types_m9.go.
//
// Mọi POST đi qua planJob/planJobKick (api_ssl.go): ?dry_run=1 trả bản xem
// trước, không thì giành chỗ chạy job, chuẩn bị Plan (lời từ chối của core về
// thẳng người gọi kèm checks, có ghi nhật ký) rồi chạy ở nền và trả 202.
// Mã HTTP của các lời từ chối mới nằm ở apiErrorStatus (api.go).

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/validate"
)

// apiBackups dựng ca sử dụng backup và thao tác DB. Sites đã chờ khoá có hạn
// (apiSites); Now theo đồng hồ của server để test có giờ cố định.
func (s *Server) apiBackups() *core.Backups {
	return &core.Backups{Sites: s.apiSites(), Store: s.deps.Store, Now: s.now}
}

// apiCron dựng ca sử dụng của màn Cron.
func (s *Server) apiCron() *core.Cron {
	b := s.apiBackups()
	return &core.Cron{Sites: b.Sites, Runs: s.deps.Store, Backups: b, Now: s.now}
}

// ---------------------------------------------------------------- màn Backup

// handleBackups trả cấu hình và danh sách backup (mới nhất trước), lọc theo
// ?domain= nếu có. Domain lọc cũng phải qua validate: chuỗi lạ không đi tới
// store, và người gọi biết mình gõ sai thay vì nhận danh sách rỗng.
func (s *Server) handleBackups(w http.ResponseWriter, r *http.Request) {
	domain := ""
	if q := r.URL.Query().Get("domain"); q != "" {
		d, err := validate.ParseDomain(q)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		domain = d.String()
	}
	b := s.apiBackups()
	set, err := b.Settings(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	items, err := b.List(r.Context(), domain)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, BackupsOverview{Settings: set, Items: apiNonNil(items)})
}

// handleSiteBackup tạo một bản backup đầy đủ của site (bấm tay). Kick mặc định
// của planJob: backup không đổi gì của site, nhưng kiểm nhẹ lại sau job như
// mọi job khác là đủ rẻ.
func (s *Server) handleSiteBackup(w http.ResponseWriter, r *http.Request) {
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	b := s.apiBackups()
	s.planJob(w, r, "backup.create", d.String(), func(ctx context.Context) (apiPlan, error) {
		return b.PrepareCreate(ctx, d, core.ReasonManual)
	})
}

// backupIDRe: id do store sinh là 16 hex; nhận rộng như core (backupIDRe ở
// core/backup.go) để dòng ghi tay vẫn khôi phục được. Chặn ở đây để chuỗi lạ
// (xuống dòng, ký tự điều khiển) không đi vào nhật ký hay câu trả lời.
var backupIDRe = regexp.MustCompile(`^[0-9a-f]{1,64}$`)

// backupNotFound là lời từ chối khi id không trỏ tới dòng nào (404 qua
// apiErrorStatus). id đã qua backupIDRe, in lại được.
func backupNotFound(id string) error {
	return &core.RefusedError{Code: "backup_not_found", Message: i18n.T(
		"Không có bản backup "+id+". Xem các bản đang có ở màn Backup (hoặc: sudo tpan backup list).",
		"There is no backup "+id+". See the existing ones on the Backup screen (or: sudo tpan backup list).")}
}

// handleBackupRestore khôi phục site từ một bản backup. Site lấy từ dòng
// backup, không từ người gọi: không thể khôi phục bản của site này vào site
// khác. Dòng phải đọc trước planJobKick vì kick cần domain ngay từ đầu; core
// đọc lại và kiểm đủ (đúng site, bản full, sha256) lúc dựng Plan.
func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	const action = "backup.restore"
	id := r.PathValue("id")
	// refused: xem trước không ghi nhật ký (như planJobKick), chạy thật thì có.
	refused := func(target string, err error) {
		if apiDryRun(r) {
			apiFail(w, r, err)
			return
		}
		s.jobRefused(w, r, action, target, err)
	}
	if !backupIDRe.MatchString(id) {
		// Không in lại id lạ, kể cả vào nhật ký.
		refused("?", backupNotFound("?"))
		return
	}
	b := s.apiBackups()
	row, ok, err := s.deps.Store.Backup(r.Context(), id)
	if err != nil {
		apiFail(w, r, fmt.Errorf("đọc backup %s: %w", id, err))
		return
	}
	if !ok {
		refused(id, backupNotFound(id))
		return
	}
	d, err := validate.ParseDomain(row.Domain)
	if err != nil {
		// Dòng do tpan ghi luôn có domain đúng dạng; sai là dòng bị sửa tay.
		apiFail(w, r, fmt.Errorf("backup %s có domain %q không hợp lệ trong tpan.db: %w", id, row.Domain, err))
		return
	}
	// Kick đúng site: mã nguồn và DB vừa đổi, health kiểm nó ngay.
	s.planJobKick(w, r, jobOpts{action: action, target: d.String(), kick: d.String()}, func(ctx context.Context) (apiPlan, error) {
		return b.PrepareRestore(ctx, d, id)
	})
}

// handleBackupSettings đặt số bản giữ và giờ backup hằng ngày (At rỗng: tắt).
// Là job vì ghi hoặc gỡ file cron của lịch backup. Target của nhật ký ghi
// đúng thứ người gọi gửi, At qua %q để ký tự điều khiển không vào nhật ký nguyên dạng.
func (s *Server) handleBackupSettings(w http.ResponseWriter, r *http.Request) {
	var body BackupSettingsRequest
	if !apiReadJSON(w, r, &body) {
		return
	}
	target := fmt.Sprintf("keep=%d at=%q", body.Keep, body.At)
	b := s.apiBackups()
	s.planJob(w, r, "backup.settings", target, func(ctx context.Context) (apiPlan, error) {
		return b.PrepareSettings(ctx, body.Keep, body.At)
	})
}

// ---------------------------------------------------------------- màn Cron

// handleCron trả lịch cron của mọi site, lần chạy gần nhất và lịch backup.
// Chỉ đọc; chạy cron là việc của dòng cron (tpan cron run), không qua API.
func (s *Server) handleCron(w http.ResponseWriter, r *http.Request) {
	ov, err := s.apiCron().List(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	ov.Sites = apiNonNil(ov.Sites)
	writeJSON(w, http.StatusOK, ov)
}

// ---------------------------------------------------------------- thao tác DB

// dbDomain đọc {domain} ở đường dẫn; sai thì đã trả 400.
func dbDomain(w http.ResponseWriter, r *http.Request) (validate.Domain, bool) {
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return validate.Domain{}, false
	}
	return d, true
}

// handleDBPassword đổi mật khẩu user DB của site. Kick đúng site: giữa hai
// lệnh của bước đổi, site báo lỗi kết nối DB vài giây.
func (s *Server) handleDBPassword(w http.ResponseWriter, r *http.Request) {
	d, ok := dbDomain(w, r)
	if !ok {
		return
	}
	b := s.apiBackups()
	s.planJobKick(w, r, jobOpts{action: "db.passwd", target: d.String(), kick: d.String()}, func(ctx context.Context) (apiPlan, error) {
		return b.PrepareDBPassword(ctx, d)
	})
}

// handleDBDump xuất DB của site ra một file .sql (bản xuất có trong
// GET /backups?domain=D, Kind "sql"). Không đổi gì của site.
func (s *Server) handleDBDump(w http.ResponseWriter, r *http.Request) {
	d, ok := dbDomain(w, r)
	if !ok {
		return
	}
	b := s.apiBackups()
	s.planJob(w, r, "db.dump", d.String(), func(ctx context.Context) (apiPlan, error) {
		return b.PrepareDBDump(ctx, d)
	})
}

// handleDBImport nhập một bản xuất đã có của đúng site đó. Kick đúng site: DB
// vừa bị thay.
func (s *Server) handleDBImport(w http.ResponseWriter, r *http.Request) {
	d, ok := dbDomain(w, r)
	if !ok {
		return
	}
	var body DBImportRequest
	if !apiReadJSON(w, r, &body) {
		return
	}
	b := s.apiBackups()
	s.planJobKick(w, r, jobOpts{action: "db.import", target: d.String(), kick: d.String()}, func(ctx context.Context) (apiPlan, error) {
		return b.PrepareDBImport(ctx, d, body.BackupID)
	})
}
