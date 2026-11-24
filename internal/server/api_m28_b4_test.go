package server

import (
	"database/sql"
	"testing"
)

// TestXemTruocBiTuChoiCoNhatKyB4: bản xem trước của POST /sites và POST /apply
// bị từ chối site_not_found (dò domain gốc của khách khác, P4) để lại dấu trong
// nhật ký. /apply chỉ ghi số dòng, không ghi tên; xem trước không bị từ chối
// thì không ghi gì.
func TestXemTruocBiTuChoiCoNhatKyB4(t *testing.T) {
	e, h := p4Env(t)
	p4SameAsMissing(t, p4Create(e, h, authzA, "www.b.example.net", "?dry_run=1"), "www.b.example.net")
	p4Create(e, h, authzA, "www.a.example.com", "?dry_run=1") // được phép: không ghi
	body := `{"defaults":{"admin_email":"x@example.com"},"sites":[{"domain":"zz.b.example.net"},{"domain":"zz.c.example.org"},{"domain":"zz.a.example.com"}]}`
	e.do(h, "POST", "/api/v1/apply?dry_run=1", body, e.cookies[authzA])
	e.do(h, "POST", "/api/v1/apply?dry_run=1", `{"defaults":{"admin_email":"x@example.com"},"sites":[{"domain":"zz.a.example.com"}]}`, e.cookies[authzA])

	db, err := sql.Open("sqlite", e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query("SELECT action, target, result FROM audit WHERE actor = ? AND result LIKE '%dry_run' ORDER BY id", authzA)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got [][3]string
	for rows.Next() {
		var r [3]string
		if err := rows.Scan(&r[0], &r[1], &r[2]); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := [][3]string{
		{"site.add", "www.b.example.net", "site_not_found_dry_run"},
		{"apply", "3 site, 2 site_not_found", "site_not_found_dry_run"},
	}
	if len(got) != len(want) {
		t.Fatalf("audit dry_run của A = %v; muốn %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dòng %d = %v; muốn %v", i, got[i], want[i])
		}
	}
}
