package site

// Thùng rác (mục 5, 6.4 của docs/KE-HOACH.md). Tách khỏi types.go để Layout
// không phải đổi (M12 cũng sửa quanh Layout).

// TrashRoot là thư mục cha của mọi site trong thùng rác. Nằm dưới
// /var/lib/tpan (0700 root): web server và user của các site khác không đọc
// được file của site đã xoá.
const TrashRoot = "/var/lib/tpan/trash"

// TrashDir là chỗ thư mục site (/var/www/<domain>) được chuyển tới khi vào
// thùng rác: TrashRoot/<slug>. Id của thùng rác là slug: suy được từ domain,
// duy nhất (cột slug UNIQUE), và một domain chỉ có một dòng trong bảng sites
// nên không bao giờ có hai bản cùng nằm trong thùng rác.
func TrashDir(l Layout) string {
	if l.Domain.IsZero() {
		return ""
	}
	return TrashRoot + "/" + l.Slug.String()
}
