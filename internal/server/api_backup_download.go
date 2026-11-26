package server

// GET /api/v1/backups/{id}/download (chủ bản backup hoặc admin): tải file
// backup (.tar.gz của cả site, hoặc .sql) về máy người dùng. Chỉ bản còn file
// trên máy; bản chỉ còn trên kho thì admin tải về máy chủ trước.

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	const action = "backup.download"
	id := r.PathValue("id")
	if !backupIDRe.MatchString(id) {
		apiFail(w, r, backupNotFound("?"))
		return
	}
	row, ok, err := s.deps.Store.Backup(r.Context(), id)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	if !ok {
		apiFail(w, r, backupNotFound(id))
		return
	}
	if !row.Local {
		apiFail(w, r, &core.RefusedError{Code: core.CodeBackupRemoteOnly, Message: i18n.T(
			"Bản backup "+id+" chỉ còn trên kho, chưa có trên máy chủ. Quản trị viên bấm Tải về máy ở màn Backup trước, rồi mới tải về máy tính được.",
			"Backup "+id+" only exists in remote storage, not on the server. An administrator has to fetch it to the server on the Backup screen first; then it can be downloaded.")})
		return
	}
	// Dòng do tpan ghi luôn nằm dưới thư mục backup; dòng lạ (sửa tay) thì từ chối.
	p := row.Path
	if path.Clean(p) != p || !strings.HasPrefix(p, site.BackupRoot+"/") {
		s.audit(r, action, id, "bad_path", "")
		apiFail(w, r, backupNotFound(id))
		return
	}
	f, fi, err := system.OpenRegularNoFollow(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			apiFail(w, r, &core.RefusedError{Code: "backup_corrupt", Message: i18n.T(
				"File của bản backup "+id+" không còn trên máy chủ.", "The file of backup "+id+" is no longer on the server.")})
			return
		}
		apiFail(w, r, err)
		return
	}
	defer func() { _ = f.Close() }()
	s.audit(r, action, id, "ok", "")
	name := path.Base(p)
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", `attachment; filename="`+strings.NewReplacer(`"`, "", `\`, "", "\r", "", "\n", "").Replace(name)+`"`)
	h.Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		slog.Warn("tải backup về bị ngắt giữa chừng", "id", id, "err", err)
	}
}
