package core

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

func TestDBRemoteMarkerTrungNhau(t *testing.T) {
	if checks.DBRemoteMarker != steps.DBRemoteMarker {
		t.Fatalf("checks %q khác steps %q", checks.DBRemoteMarker, steps.DBRemoteMarker)
	}
}

// remoteSys: ufw bật hay tắt; MariaDB giả trả DB và user có, máy ngoài theo hosts.
func remoteSys(ufwOn, modeOn bool, hosts string) *system.FakeSystem {
	f := system.NewFake()
	if modeOn {
		f.SetFile(steps.DBRemoteMarker, []byte("on\n"), 0o644, system.Root)
	}
	f.Handle([]string{"ufw", "status"}, func(system.Cmd) system.Result {
		if ufwOn {
			return system.Result{Stdout: "Status: active\n"}
		}
		return system.Result{Stdout: "Status: inactive\n"}
	})
	f.Handle([]string{"mariadb"}, func(c system.Cmd) system.Result {
		q := string(c.Stdin)
		switch {
		case strings.Contains(q, "Host NOT IN"):
			return system.Result{Stdout: hosts}
		case strings.Contains(q, "information_schema.SCHEMATA") || strings.Contains(q, "SCHEMA_NAME"):
			return system.Result{Stdout: "1\t1\n"}
		}
		return system.Result{Stdout: "1\t1\n"}
	})
	return f
}

// Bật chế độ khi tường lửa tắt: từ chối, chỉ đường bật tường lửa.
func TestBatKetNoiTuXaCanTuongLua(t *testing.T) {
	d := &DBs{Sys: remoteSys(false, false, "")}
	_, err := d.PrepareRemoteMode(ctx, true)
	if dbsCode(err) != CodeDBRemoteNeedsFirewall {
		t.Fatalf("lỗi %v; muốn %s", err, CodeDBRemoteNeedsFirewall)
	}
	d = &DBs{Sys: remoteSys(true, false, "")}
	p, err := d.PrepareRemoteMode(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := stepIDs(p.Plan); len(got) != 2 || !strings.Contains(strings.Join(got, " "), steps.MariaDBCnfPath) {
		t.Fatalf("bước = %v", got)
	}
}

// Tắt chế độ: xoá máy ngoài của từng user trước, đóng 3306, rồi mới đưa
// MariaDB về 127.0.0.1 và gỡ dấu.
func TestTatKetNoiTuXaXoaMayNgoaiTruoc(t *testing.T) {
	d := &DBs{Sys: remoteSys(true, true, "alice_shop\t203.0.113.5\nbob_blog\t198.51.100.7\n")}
	p, err := d.PrepareRemoteMode(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(stepIDs(p.Plan), " | ")
	want := "mariadb.remote-hosts:alice_shop | mariadb.remote-hosts:bob_blog | ufw.db-from | "
	if !strings.HasPrefix(got, want) || !strings.HasSuffix(got, steps.DBRemoteMarker) {
		t.Fatalf("thứ tự bước = %s", got)
	}
	for _, s := range p.Plan.Steps[:2] {
		if h := s.(*steps.DBRemoteHosts).Hosts; len(h) != 0 {
			t.Fatalf("tắt chế độ mà còn giữ máy ngoài %v", h)
		}
	}
}

// Khai máy ngoài cho DB rời: chế độ tắt thì từ chối; IP sai, quá 10 thì từ chối;
// luật tường lửa là hợp của mọi DB.
func TestKhaiMayNgoaiChoDB(t *testing.T) {
	e := newDBsEnv(t)
	e.add(t)
	e.d.Sys = remoteSys(true, false, "")
	if _, err := e.d.PrepareRemoteHosts(ctx, "alice_shop", []string{"203.0.113.5"}); dbsCode(err) != CodeDBRemoteOff {
		t.Fatalf("chế độ tắt: %v", err)
	}
	e.d.Sys = remoteSys(true, true, "bob_blog\t198.51.100.7\n")
	for _, bad := range [][]string{{"127.0.0.1"}, {"10.0.0.0/8"}, {"2001:db8::1"},
		{"1.1.1.1", "1.1.1.2", "1.1.1.3", "1.1.1.4", "1.1.1.5", "1.1.1.6", "1.1.1.7", "1.1.1.8", "1.1.1.9", "1.1.1.10", "1.1.1.11"}} {
		if _, err := e.d.PrepareRemoteHosts(ctx, "alice_shop", bad); dbsCode(err) != CodeDBRemoteInvalid {
			t.Errorf("%v: %v", bad, err)
		}
	}
	p, err := e.d.PrepareRemoteHosts(ctx, "alice_shop", []string{" 203.0.113.5", "203.0.113.5"})
	if err != nil {
		t.Fatal(err)
	}
	h := p.Plan.Steps[0].(*steps.DBRemoteHosts)
	u := p.Plan.Steps[1].(*steps.UFWDBFrom)
	if strings.Join(h.Hosts, ",") != "203.0.113.5" || strings.Join(u.IPs, ",") != "198.51.100.7,203.0.113.5" {
		t.Fatalf("máy ngoài %v, tường lửa %v", h.Hosts, u.IPs)
	}
	if p.Plan.Target != "alice_shop" {
		t.Errorf("Target = %s", p.Plan.Target)
	}
}
