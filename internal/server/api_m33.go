package server

// API của M33: sao lưu DB rời (docs/thay-doi/M33.md).
//
//	POST /api/v1/dbs/{name}/backups   luật ownerDB   xuất DB rời ra một bản sao lưu
//	POST /api/v1/dbs/{name}/import    luật ownerDB   nhập một bản sao lưu vào DB rời đang có
//
// Bản sao lưu của DB rời là một dòng của bảng backups (cột domain mang tên
// DB), nên xem, tải về và xoá vẫn đi qua các đường đã có của màn Backup:
// GET /backups (thêm bộ lọc ?db=), GET /backups/{id}/download,
// DELETE /backups/{id}. POST /backups/{id}/restore nhận cả dòng của DB rời
// (dbBackupRestore): DB còn thì nhập vào, DB đã xoá hẳn thì tạo lại rồi nhập.

import (
	"context"
	"net/http"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
)

// handleDBBackup: bấm "Sao lưu ngay" cho một DB rời.
func (s *Server) handleDBBackup(w http.ResponseWriter, r *http.Request) {
	s.dbJob(w, r, "dbs.backup", func(d *core.DBs, ctx context.Context, name string) (*core.Planned, error) {
		return d.PrepareBackup(ctx, name, core.ReasonManual)
	})
}

// handleStandaloneDBImport: nhập một bản sao lưu đã có của đúng DB rời đó.
func (s *Server) handleStandaloneDBImport(w http.ResponseWriter, r *http.Request) {
	var body DBImportRequest
	if !apiReadJSON(w, r, &body) {
		return
	}
	name, target := pathDBName(r)
	dbs := s.apiDBs()
	s.planJob(w, r, "dbs.import", target, func(ctx context.Context) (apiPlan, error) {
		return dbs.PrepareImport(ctx, name, body.BackupID)
	})
}

// dbBackupRestore xử lý POST /backups/{id}/restore khi dòng là bản sao lưu của
// một DB rời. Guard ownerBackup đã kiểm người gọi là chủ của bản (hoặc admin).
//
//   - DB đang có: nhập vào (core từ chối khi DB ở thùng rác hay đang khoá).
//     User thường còn phải là chủ của DB hiện tại: hai chủ khác nhau có thể
//     sinh cùng một tên DB, và bản của người này không được ghi đè DB của
//     người kia. Không phải thì trả như không có bản đó.
//   - DB không còn (đã xoá hẳn): tạo lại cho chủ ghi trên bản sao lưu rồi
//     nhập. User DB mới có mật khẩu mới, trả một lần qua GET /jobs/{id}.
func (s *Server) dbBackupRestore(w http.ResponseWriter, r *http.Request, row site.Backup) {
	ctx := r.Context()
	name, id := row.Domain, row.ID
	dbs := s.apiDBs()
	db, exists, err := s.deps.Store.StandaloneDB(ctx, name)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	if exists {
		if scope := scopeOf(r); scope != "" && db.Owner != scope {
			if apiDryRun(r) {
				apiFail(w, r, backupNotFound(id))
				return
			}
			s.jobRefused(w, r, "dbs.import", id, backupNotFound(id))
			return
		}
		s.planJob(w, r, "dbs.import", name, func(ctx context.Context) (apiPlan, error) {
			return dbs.PrepareImport(ctx, name, id)
		})
		return
	}
	s.secretJob(w, r, jobOpts{action: "dbs.recreate", target: name}, func(ctx context.Context) (*core.Planned, *DBSecret, error) {
		add, err := dbs.PrepareRecreate(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		return add.Planned, &DBSecret{DB: add.DB.Name, User: add.DB.Name, Password: add.Password.Reveal()}, nil
	})
}
