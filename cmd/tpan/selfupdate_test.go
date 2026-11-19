package main

import (
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/system"
)

// selfRelease: máy có tpan bản đang chạy (buildinfo.Version), máy chủ tải trả
// bản newVer cho đúng kiến trúc của máy chạy test. `tpan version` của mỗi file
// in nội dung file đó.
func (c *cli) selfRelease(newVer string) {
	f := c.fake
	f.SetFile(core.TpanBin, []byte("tpan "+buildinfo.Version), 0o755, system.Root)
	data := []byte("tpan " + newVer)
	sum := sha256.Sum256(data)
	f.Handle([]string{"curl", "-fsSL", "--max-time", "30"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: hex.EncodeToString(sum[:]) + "  tpan-linux-" + runtime.GOARCH + "\n"}
	})
	f.Handle([]string{"curl", "-fsSL", "--max-time", "300"}, func(cmd system.Cmd) system.Result {
		f.SetFile(cmd.Args[slices.Index(cmd.Args, "-o")+1], data, 0o644, system.Root)
		return system.Result{}
	})
	for _, p := range []string{core.TpanBin, core.TpanBin + ".new"} {
		f.Handle([]string{p, "version"}, func(system.Cmd) system.Result {
			b, err := f.ReadFile(p)
			if err != nil {
				return system.Result{ExitCode: 127}
			}
			return system.Result{Stdout: string(b) + " (commit x, build y)\n"}
		})
	}
}

func TestSelfUpdateFlags(t *testing.T) {
	c := newCLI(t)
	code, _, errs := c.run("self-update", "--rollback", "--url", "https://example.com")
	if code != 1 || !strings.Contains(errs, "--rollback") || len(c.fake.Ran()) != 0 {
		t.Errorf("--rollback --url: mã %d, %q", code, errs)
	}
	c = newCLI(t)
	if code, _, errs := c.run("self-update", "--verbose"); code != 1 || !strings.Contains(errs, "verbose") {
		t.Errorf("--verbose không có ở self-update: mã %d, %q", code, errs)
	}
	for _, u := range []string{"http://example.com", "https://example.com/?$(id)"} {
		c := newCLI(t)
		code, out, _ := c.run("self-update", "--url", u, "--dry-run", "--json")
		if code != 1 || refusalCode(t, out) != "self_url_invalid" || len(c.fake.Ran()) != 0 {
			t.Errorf("%q: mã %d, %s", u, code, out)
		}
	}
	c = newCLI(t)
	code, out, _ := c.run("self-update", "--rollback", "--dry-run", "--json")
	if code != 1 || refusalCode(t, out) != "self_no_old" {
		t.Errorf("--rollback không có .old: mã %d, %s", code, out)
	}
}

func TestSelfUpdateDryRun(t *testing.T) {
	c := newCLI(t)
	c.selfRelease("v9.9.9")
	code, out, errs := c.run("self-update", "--dry-run", "--url", "https://example.com/rel")
	if code != 0 || !strings.Contains(out, "https://example.com/rel/tpan-linux-"+runtime.GOARCH) || !strings.Contains(out, "SHA256SUMS") {
		t.Errorf("mã %d\n%s\n%s", code, out, errs)
	}
	if len(c.fake.Ran()) != 0 || c.dbExists() {
		t.Errorf("--dry-run chạy %v hoặc tạo DB", c.fake.Ran())
	}
}

func TestSelfUpdateRun(t *testing.T) {
	c := newCLI(t)
	c.selfRelease("v9.9.9")
	code, out, errs := c.run("self-update", "--yes", "--url", "https://example.com/rel")
	if code != 0 || !strings.Contains(out, buildinfo.Version+" -> v9.9.9") {
		t.Fatalf("mã %d\n%s\n%s", code, out, errs)
	}
	if c.audits("self.update") != 1 {
		t.Error("không ghi audit self.update")
	}
	if b, _ := c.fake.ReadFile(core.TpanBin + ".old"); string(b) != "tpan "+buildinfo.Version {
		t.Errorf(".old = %q", b)
	}

	// Quay về: bản cũ trở lại TpanBin.
	code, out, errs = c.run("self-update", "--rollback", "--yes")
	if code != 0 || !strings.Contains(out, "đã quay về bản trước") || c.audits("self.rollback") != 1 {
		t.Fatalf("rollback: mã %d\n%s\n%s", code, out, errs)
	}
	if b, _ := c.fake.ReadFile(core.TpanBin); string(b) != "tpan "+buildinfo.Version {
		t.Errorf("sau quay về TpanBin = %q", b)
	}
}

func TestSelfUpdateSameVersion(t *testing.T) {
	c := newCLI(t)
	c.selfRelease(buildinfo.Version)
	code, out, errs := c.run("self-update", "--yes", "--url", "https://example.com/rel")
	if code != 0 || !strings.Contains(out, "Đang là bản mới nhất") {
		t.Errorf("mã %d\n%s\n%s", code, out, errs)
	}
	if _, err := c.fake.Stat(core.TpanBin + ".old"); err == nil {
		t.Error("trùng bản mà vẫn thay file")
	}
}
