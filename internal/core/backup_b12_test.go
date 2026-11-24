package core

import (
	"slices"
	"testing"
)

// B12: chủ cũ đã xoá hẳn site, khách khác tạo lại cùng domain. Backup của chủ
// mới không được đẩy backup của chủ cũ ra khỏi số giữ, và không tính vào hạn
// mức của chủ mới.
func TestDonBackupChiTheoChuHienTai(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	// Máy thật dùng một DB cho bảng sites và backups; siteEnv tách bảng sites
	// ra bộ nhớ, nên ghi cả hai. AddBackup chép chủ từ bảng sites lúc ghi dòng.
	setOwner := func(o string) {
		st, _, err := be.sites.Site(ctx, mustDomain(t, dom))
		if err != nil {
			t.Fatal(err)
		}
		st.Owner = o
		if err := be.sites.SaveSite(ctx, st); err != nil {
			t.Fatal(err)
		}
		if err := be.st.SaveSite(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	setOwner("binh")
	var old []Backup
	for _, s := range []string{"20261001-000000", "20261002-000000", "20261003-000000"} {
		old = append(old, be.seedFull(t, dom, s, "b", "b", ReasonManual))
	}
	setOwner("khoa")
	var mine []Backup
	for _, s := range []string{"20261101-000000", "20261102-000000", "20261103-000000"} {
		mine = append(mine, be.seedFull(t, dom, s, "k", "k", ReasonManual))
	}

	if n, err := be.b.fullCount(ctx, dom); err != nil || n != 3 {
		t.Errorf("fullCount = %d, %v; muốn 3 (chỉ bản của chủ hiện tại)", n, err)
	}

	mustSet(t, be.st, SettingBackupKeep, "2")
	if err := be.b.prune(ctx, dom); err != nil {
		t.Fatal(err)
	}
	ids := be.ids(t, dom)
	for _, r := range old {
		if !slices.Contains(ids, r.ID) || !be.exists(r.Path) {
			t.Errorf("mất backup %s của chủ cũ; còn %v", r.ID, ids)
		}
	}
	if slices.Contains(ids, mine[0].ID) || be.exists(mine[0].Path) {
		t.Errorf("bản cũ nhất của chủ hiện tại phải bị dọn (Keep 2): %v", ids)
	}
	for _, r := range mine[1:] {
		if !slices.Contains(ids, r.ID) {
			t.Errorf("mất bản %s phải giữ; còn %v", r.ID, ids)
		}
	}
}
