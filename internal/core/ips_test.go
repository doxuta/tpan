package core

import (
	"context"
	"reflect"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

const ipOut = `2: eth0    inet 203.0.113.74/24 brd 203.0.113.255 scope global eth0\       valid_lft forever preferred_lft forever
2: eth0    inet 10.0.0.5/8 scope global secondary eth0\       valid_lft forever preferred_lft forever
3: eth1    inet 100.64.1.2/10 scope global eth1\       valid_lft forever preferred_lft forever
2: eth0    inet6 2001:db8::5/64 scope global \       valid_lft forever preferred_lft forever
2: eth0    inet6 fd00::1/64 scope global \       valid_lft forever preferred_lft forever
`

func TestParseIPAddrs(t *testing.T) {
	got := parseIPAddrs(ipOut)
	want := []IPAddr{
		{Iface: "eth0", IP: "203.0.113.74", Prefix: 24, Family: "ipv4", Public: true},
		{Iface: "eth0", IP: "10.0.0.5", Prefix: 8, Family: "ipv4", Public: false},
		{Iface: "eth1", IP: "100.64.1.2", Prefix: 10, Family: "ipv4", Public: false},
		{Iface: "eth0", IP: "2001:db8::5", Prefix: 64, Family: "ipv6", Public: true},
		{Iface: "eth0", IP: "fd00::1", Prefix: 64, Family: "ipv6", Public: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseIPAddrs\n got %+v\nwant %+v", got, want)
	}
}

func TestReadMachineIPs(t *testing.T) {
	f := system.NewFake()
	f.SetFile("/etc/os-release", []byte("NAME=\"Ubuntu\"\nPRETTY_NAME=\"Ubuntu 24.04 LTS\"\n"), 0o644, system.Root)
	f.Handle([]string{"ip", "-o", "addr"}, func(system.Cmd) system.Result { return system.Result{Stdout: ipOut} })
	// Tên trỏ về máy: ahostsv6 trả IPv4 dạng ::ffff: (không phải AAAA thật).
	f.Handle([]string{"getent", "ahostsv4", "a.example"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "203.0.113.74   STREAM a.example\n203.0.113.74   DGRAM\n"}
	})
	f.Handle([]string{"getent", "ahostsv6", "a.example"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "::ffff:203.0.113.74 STREAM a.example\n"}
	})
	// Tên trỏ máy khác.
	f.Handle([]string{"getent", "ahostsv4", "b.example"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "203.0.113.9 STREAM b.example\n"}
	})
	// Tên chưa có bản ghi: getent mã 2.
	f.Handle([]string{"getent", "ahostsv4", "c.example"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	f.Handle([]string{"getent", "ahostsv6", "c.example"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	f.Handle([]string{"getent", "ahostsv6"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })

	got, err := ReadMachineIPs(context.Background(), f, "may-thu-1", []string{"a.example", "b.example", "c.example"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Hostname != "may-thu-1" || got.OS != "Ubuntu 24.04 LTS" {
		t.Fatalf("hostname %q os %q", got.Hostname, got.OS)
	}
	if len(got.Addrs) != 5 || got.PanelPort != "8443" || got.PanelPrivate {
		t.Fatalf("addrs %d, panel %s private=%v", len(got.Addrs), got.PanelPort, got.PanelPrivate)
	}
	want := []SiteDNS{
		{Domain: "a.example", A: []string{"203.0.113.74"}, AAAA: []string{}, Here: true},
		{Domain: "b.example", A: []string{"203.0.113.9"}, AAAA: []string{}, Here: false},
		{Domain: "c.example", A: []string{}, AAAA: []string{}, Here: false},
	}
	if !reflect.DeepEqual(got.Sites, want) {
		t.Fatalf("sites\n got %+v\nwant %+v", got.Sites, want)
	}

	// Panel ở chế độ riêng tư: đọc từ file dịch vụ.
	f.SetFile(panelUnitPath, []byte("[Service]\nExecStart=/usr/local/bin/tpan serve --listen 127.0.0.1:8443 --tls-cert x --tls-key y\n"), 0o644, system.Root)
	got, err = ReadMachineIPs(context.Background(), f, "may-thu-1", nil)
	if err != nil || !got.PanelPrivate || len(got.Sites) != 0 {
		t.Fatalf("private: %+v %v", got, err)
	}
}
