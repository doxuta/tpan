package demo

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
)

// Máy demo có đích S3 đọc lại được bằng chính mã thật, gói panel gần nhất, và
// backup ở đủ ba trạng thái kho cùng một bản chỉ trên máy.
func TestM18Seed(t *testing.T) {
	ctx, sys, st := newDemo(t)
	want := s3.TargetID("https://s3.example.com", "tpan-demo", "tpan/demo")

	// Đích đọc lại bằng đúng hàm của core: setting nào sai dạng là lỗi ở đây.
	id, ok, err := core.CurrentTargetID(ctx, st)
	if err != nil || !ok || id != want {
		t.Fatalf("CurrentTargetID = %q, %v, %v; muốn %q", id, ok, err, want)
	}
	for k, v := range map[string]string{core.SettingS3Keep: "7", core.SettingS3PathStyle: "1"} {
		if got, _, err := st.GetSetting(ctx, k); err != nil || got != v {
			t.Errorf("setting %s = %q (%v); muốn %q", k, got, err, v)
		}
	}

	// panel_last: <tên>|<RFC 3339>|<sha256 hex>.
	last, _, err := st.GetSetting(ctx, core.SettingS3PanelLast)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(last, "|")
	if len(parts) != 3 || !strings.HasSuffix(parts[0], ".tar.gz.age") || len(parts[2]) != 64 {
		t.Fatalf("panel_last = %q", last)
	}
	if _, err := time.Parse(time.RFC3339, parts[1]); err != nil {
		t.Errorf("panel_last giờ %q: %v", parts[1], err)
	}

	all, err := st.Backups(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var machine, both, remoteOnly, failed int
	for _, b := range all {
		switch b.Remote {
		case site.RemoteFailed:
			failed++
			// Bản đẩy lỗi vẫn còn trên máy; lỗi ngắn, một dòng.
			if !b.Local || b.RemoteTarget != want || b.RemoteError == "" || len(b.RemoteError) > 100 || strings.ContainsAny(b.RemoteError, "\r\n") {
				t.Errorf("bản đẩy lỗi %s: local=%v, đích %q, lỗi %q", b.ID, b.Local, b.RemoteTarget, b.RemoteError)
			}
		case site.RemoteStored:
			if b.RemoteTarget != want || b.RemoteKey == "" || b.RemoteAt.IsZero() || b.RemoteError != "" {
				t.Errorf("bản stored %s thiếu cột kho: %+v", b.ID, b)
			}
			_, statErr := sys.Stat(b.Path)
			if b.Local {
				both++
				if statErr != nil {
					t.Errorf("bản %s ghi local=1 mà file mất: %v", b.ID, statErr)
				}
			} else {
				remoteOnly++
				if !errors.Is(statErr, fs.ErrNotExist) {
					t.Errorf("bản %s chỉ còn trên kho mà file còn trên máy (%v)", b.ID, statErr)
				}
			}
		default:
			if !b.Local {
				t.Errorf("bản %s local=0 mà không ở trên kho", b.ID)
			}
			if b.RemoteKey != "" || b.RemoteError != "" {
				t.Errorf("bản chỉ trên máy %s có cột kho: %+v", b.ID, b)
			}
			machine++
		}
	}
	if machine == 0 || both != 1 || remoteOnly != 1 || failed != 1 {
		t.Errorf("trên máy %d, cả hai %d, chỉ trên kho %d, đẩy lỗi %d; muốn >=1, 1, 1, 1", machine, both, remoteOnly, failed)
	}
}

// Demo không có kho thật nên không có khoá nào: không ba file bí mật trên máy
// giả, và các file của tpan.db (kể cả -wal) không chứa chuỗi nào có dạng khoá.
func TestM18KhongCoKhoa(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	sys, st, err := New(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	for _, p := range []string{steps.S3KeysFile, steps.S3CAFile, steps.BackupKeyFile} {
		if _, err := sys.Stat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("máy demo có %s (%v)", p, err)
		}
	}

	// Khoá truy cập AWS (AKIA…/ASIA…), khoá age (AGE-SECRET-KEY-…, age1…), tên
	// trường của s3.json và khối khoá riêng PEM.
	keyLike := regexp.MustCompile(`(?i)\b(AKIA|ASIA)[0-9A-Z]{16}\b|AGE-SECRET-KEY-|\bage1[0-9a-z]{50,}|secret_key|access_key|BEGIN [A-Z ]*PRIVATE KEY`)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var scanned int
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("đọc %s: %v", e.Name(), err)
		}
		scanned++
		if m := keyLike.Find(b); m != nil {
			t.Errorf("%s có chuỗi giống khoá: %q", e.Name(), m)
		}
	}
	if scanned == 0 {
		t.Fatal("không file nào để quét")
	}
	// Quét cả nội dung các setting qua store, phòng khi dữ liệu còn nằm trong WAL.
	settings, err := st.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range settings {
		if m := keyLike.FindString(k + "=" + v); m != "" {
			t.Errorf("setting %s có chuỗi giống khoá: %q", k, m)
		}
	}
}
