package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// TestMigration1900OnlyPrimaries (B5): nâng DB của bản trước M19 có ba site
// (một trong thùng rác, hai chủ chung một tên miền gốc vì P4 chưa vá): migration
// không hỏng, mỗi site đúng một dòng tên chính, không tên phụ nào, in_cert theo ssl.
func TestMigration1900OnlyPrimaries(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tpan.db")
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := migrate(ctx, db, migrationsBelow(t, 1900)); err != nil {
		t.Fatalf("migrate bản cũ: %v", err)
	}
	for _, q := range []string{
		`INSERT INTO sites (domain, slug, status, php_version, ssl, created_at, owner) VALUES ('a.example.com', 's1', 'active', '8.3', 1, ?, 'usera')`,
		`INSERT INTO sites (domain, slug, status, php_version, ssl, created_at, owner) VALUES ('b.example.com', 's2', 'active', '8.3', 0, ?, 'userb')`,
		`INSERT INTO sites (domain, slug, status, php_version, ssl, created_at, owner, trashed_at) VALUES ('old.example.net', 's3', 'trashed', '8.3', 0, ?, 'admin', '2026-11-20T00:00:00.000000000Z')`,
	} {
		if _, err := db.ExecContext(ctx, q, fmtTime(t0)); err != nil {
			t.Fatalf("dựng dữ liệu cũ: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open sau khi nâng: %v", err)
	}
	defer func() { _ = s.Close() }()

	rows, err := s.db.QueryContext(ctx, "SELECT name, site, owner, kind, mode, state, in_cert, created_at FROM site_names ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var name, site, owner, kind, mode, state, created string
		var inCert int
		if err := rows.Scan(&name, &site, &owner, &kind, &mode, &state, &inCert, &created); err != nil {
			t.Fatal(err)
		}
		if created != fmtTime(t0) {
			t.Errorf("%s: created_at %s, muốn chép từ sites", name, created)
		}
		got = append(got, strings.Join([]string{name, site, owner, kind, mode, state, strconv.Itoa(inCert)}, " "))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"a.example.com a.example.com usera primary serve active 1",
		"b.example.com b.example.com userb primary serve active 0",
		"old.example.net old.example.net admin primary serve active 0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("site_names sau nâng =\n%v\nmuốn\n%v", got, want)
	}
	var redirects int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM site_redirects").Scan(&redirects); err != nil || redirects != 0 {
		t.Errorf("site_redirects: %d dòng, %v", redirects, err)
	}
	// Site cũ đọc ra không có tên phụ nào; site SSL có tên chính trong chứng chỉ.
	sts, err := s.Sites(ctx)
	if err != nil || len(sts) != 3 {
		t.Fatalf("Sites = %d, %v", len(sts), err)
	}
	for _, st := range sts {
		if len(st.Names.Serve)+len(st.Names.Redirect)+len(st.Names.Rules) != 0 {
			t.Errorf("%s có tên phụ hay luật sau nâng: %+v", st.Domain, st.Names)
		}
	}
}
