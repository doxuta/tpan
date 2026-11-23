package server

// M28: lọc bản xem trước theo vai ở tầng API (lớp thứ hai, độc lập với cửa
// sitefs của System). Đặc tả: docs/thay-doi/M28-thiet-ke.md mục 3.7, bất biến
// B10, B11, B12.
//
// Vì sao cần lớp này khi cửa đã không đọc qua symlink: bản xem trước chứa nội
// dung file root đọc được (vhost mặc định của máy, file của site khác trong
// một Plan gộp, .htaccess của site). Vai user chỉ được thấy file của chính
// site mình; và với file trong thư mục site, chỉ thấy những byte mà chính
// user Linux của site tự đọc được. Nếu ngày nào đó lớp đọc lại đi theo symlink
// (lỗ T1 của v0.2.0), quyền file của kernel vẫn chặn ở đây.

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/sitefs"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Giá trị của FileChange.Hidden.
const (
	hiddenForeign    = "foreign"
	hiddenUnverified = "unverified"
)

// verifyTimeout là hạn của một lần đọc lại bằng user của site. Lệnh tự chết
// sau 2 giây (timeout -k 1 2), hạn này là lớp thứ hai.
const verifyTimeout = 4 * time.Second

// writePreview lọc pv theo vai của người gọi rồi trả 200. Mọi handler trả bản
// xem trước của engine đi qua đây (hoặc qua filterPreview khi bản xem trước
// nằm trong một kiểu bọc); TestMoiHandlerXemTruocQuaWritePreview giữ điều đó.
func (s *Server) writePreview(w http.ResponseWriter, r *http.Request, pv engine.Preview, targets ...validate.Domain) {
	if err := s.filterPreview(r, &pv, targets...); err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pv)
}

// filterPreview sửa pv tại chỗ. Admin: không làm gì. Vai user: tập được phép
// là các site của người gọi cộng targets (domain handler đã phân quyền xong,
// ví dụ site đang được tạo). Với từng file:
//   - không site nào trong tập làm chủ: xoá Old, New, OldLink, Hidden=foreign;
//   - trong thư mục site và có Old: chỉ giữ Old khi user Linux của site đọc
//     lại ra đúng những byte đó (verifyAsSiteUser), không thì xoá Old,
//     Hidden=unverified;
//   - file TPAN quản cho chính site (vhost, pool, cron, log): giữ nguyên.
func (s *Server) filterPreview(r *http.Request, pv *engine.Preview, targets ...validate.Domain) error {
	if who(r).IsAdmin() {
		return nil
	}
	var layouts []site.Layout
	// Store nil chỉ có trong test: tập được phép chỉ còn targets (ẩn nhiều
	// hơn chứ không lộ hơn).
	if s.deps.Store != nil {
		mine, err := s.deps.Store.SitesOf(r.Context(), scopeOf(r))
		if err != nil {
			return err
		}
		for _, x := range mine {
			layouts = append(layouts, x.Layout())
		}
	}
	for _, d := range targets {
		layouts = append(layouts, site.LayoutOf(d))
	}
	for i := range pv.Steps {
		files := pv.Steps[i].Info.Files
		if len(files) == 0 {
			continue
		}
		// Chép trước khi sửa: không sửa ngầm mảng của người khác.
		files = append([]engine.FileChange(nil), files...)
		for j := range files {
			s.filterFile(r.Context(), &files[j], layouts)
		}
		pv.Steps[i].Info.Files = files
	}
	return nil
}

func (s *Server) filterFile(ctx context.Context, f *engine.FileChange, layouts []site.Layout) {
	var owner *site.Layout
	for k := range layouts {
		if layouts[k].Owns(f.Path) {
			owner = &layouts[k]
			break
		}
	}
	switch {
	case owner == nil:
		f.Old, f.New, f.OldLink = nil, nil, ""
		f.Hidden = hiddenForeign
	case sitefs.InZone(f.Path) && len(f.Old) > 0:
		if !verifyAsSiteUser(ctx, s.deps.Sys, owner.User, f.Path, f.Old) {
			f.Old = nil
			f.Hidden = hiddenUnverified
		}
	}
}

// applyCreated là các domain trong lô apply mà core sẽ tạo mới cho người gọi:
// tập đích thêm vào bộ lọc. Chỉ dòng "create": dòng của người khác core đã
// từ chối (refused, không có bước nào), dòng "skip", "trash" là site đã có
// của người gọi (đã nằm trong SitesOf).
func applyCreated(items []core.ApplyItem) []validate.Domain {
	var out []validate.Domain
	for _, it := range items {
		if it.Action != core.ApplyCreate {
			continue
		}
		if d, err := validate.ParseDomain(it.Domain); err == nil {
			out = append(out, d)
		}
	}
	return out
}

// verifyAsSiteUser cho biết một tiến trình chạy bằng user Linux của site đọc
// path có ra đúng old không. Không dùng head: FIFO tráo vào làm head chặn ở
// open và để lại tiến trình mồ côi (S1). dd iflag=nonblock mở FIFO xong ngay,
// nofollow từ chối symlink ở tên cuối, và quyền file của kernel làm phần còn
// lại: user của site không đọc được file của người khác dù lớp đọc của root
// có đi theo đâu. timeout chạy bên trong runuser để lệnh con chết theo hạn kể
// cả khi runuser bị giết.
func verifyAsSiteUser(ctx context.Context, sys system.System, user, path string, old []byte) bool {
	// User rỗng là chạy bằng root: mất hết ý nghĩa của phép đọc lại.
	if user == "" || sys == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()
	res, err := sys.Run(ctx, system.Cmd{
		Name: "timeout",
		Args: []string{"-k", "1", "2", "dd", "if=" + path, "iflag=nonblock,nofollow",
			"bs=" + strconv.FormatInt(sitefs.MaxFile+1, 10), "count=1", "status=none"},
		AsUser: user,
	})
	return err == nil && res.ExitCode == 0 && res.Stdout == string(old)
}

// sitePathUnsafe là ErrorBody 409 cho lỗi của cửa sitefs (bảng "Mã lỗi ở API"
// ở mục 3.7). ok=false khi err không phải lỗi của cửa, hoặc là "không có"
// (người gọi tự xử lý như trước). Câu chỉ nêu đường dẫn, lý do và cách xử lý,
// không bao giờ có nội dung file hay đích symlink.
func sitePathUnsafe(err error) (ErrorBody, bool) {
	var se *sitefs.Error
	if !errors.As(err, &se) || errors.Is(err, fs.ErrNotExist) {
		return ErrorBody{}, false
	}
	vi, en := sitefsReason(se.Err)
	hint := "ls -la " + se.Path
	return ErrorBody{Error: ErrorDetail{
		Code: "site_path_unsafe",
		Message: i18n.T(
			"tpan không đụng tới "+se.Path+" trong thư mục site: "+vi+". Xem bằng `"+hint+"`, đưa về thư mục, file thường của site rồi thử lại.",
			"tpan refused to touch "+se.Path+" inside the site directory: "+en+". Look at it with `"+hint+"`, turn it back into a plain directory or regular file owned by the site, then retry."),
	}}, true
}

// sitefsReason trả lý do song ngữ cho một lỗi của cửa: Err* thì tiếng Việt lấy
// từ chính lỗi; lỗi của kernel thì nêu lỗi đó (chuỗi của Go đã là tiếng Anh).
func sitefsReason(err error) (vi, en string) {
	for _, r := range []struct {
		err error
		en  string
	}{
		{sitefs.ErrPath, "invalid path inside the site directory (empty, has \".\", \"..\", a NUL byte, or too deep)"},
		{sitefs.ErrSymlink, "it is a symlink: tpan never follows symlinks inside a site directory"},
		{sitefs.ErrNotRegular, "not a regular file: tpan never opens directories, devices, FIFOs or sockets where a regular file is expected"},
		{sitefs.ErrTooLarge, "larger than the limit tpan reads inside a site directory"},
		{sitefs.ErrOwner, "not owned by the site's user: tpan only reads files the site itself owns"},
		{sitefs.ErrLinked, "it has more than one name (hardlink): tpan does not change owner or mode through a hardlink"},
		{sitefs.ErrTooMany, "the directory has too many entries"},
		{sitefs.ErrRootUnsafe, "the parent of all site directories is not owned by root or is writable by others (fix: chown root:root /var/www && chmod 755 /var/www)"},
	} {
		if errors.Is(err, r.err) {
			return r.err.Error(), r.en
		}
	}
	if err == nil {
		return "lỗi không rõ", "unknown error"
	}
	return "lỗi của hệ điều hành: " + err.Error(), "operating system error: " + err.Error()
}
