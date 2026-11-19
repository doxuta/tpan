package core

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// sim là FakeSystem cộng các handler giả lập một máy Ubuntu 24.04: dpkg, apt,
// systemd, a2query, MariaDB, ss, curl. Không có chúng thì mọi lệnh trên
// FakeSystem trả mã 0 với output rỗng, và một Plan "chạy xong" mà không kiểm gì.
// Viết lại ở đây (không import machine_test.go của steps, vì file test không
// import được) và chỉ giả lập đủ cho Plan cài stack.
type sim struct {
	*system.FakeSystem

	mu      sync.Mutex
	pkgs    map[string]bool
	units   map[string]unitState
	mods    map[string]bool
	ss      string // output của `ss -tlnH`/`ss -tlnpH` ngoài cổng 3306
	updates int
	// onUpdate khác nil thì được gọi lúc `apt-get update` chạy; trả mã thoát.
	onUpdate func() int
}

type unitState struct{ active, enabled bool }

// fakePhar là nội dung curl giả "tải" về cho WP-CLI; test dùng sha512 của nó.
var fakePhar = []byte("<?php // wp-cli giả\n")

func fakePharSHA() string {
	sum := sha512.Sum512(fakePhar)
	return hex.EncodeToString(sum[:])
}

func newSim(t *testing.T) *sim {
	t.Helper()
	m := &sim{
		FakeSystem: system.NewFake(),
		pkgs:       map[string]bool{},
		units:      map[string]unitState{},
		mods:       map[string]bool{},
	}
	m.SetFile("/etc/os-release", []byte("NAME=\"Ubuntu\"\nID=ubuntu\nVERSION_ID=\"24.04\"\n"), 0o644, system.Root)
	m.SetDisk("/", system.DiskUsage{Total: 100 << 30, Free: 60 << 30})
	m.SetDir("/usr/local/bin", 0o755, system.Root) // Ubuntu có sẵn

	ok := system.Result{}
	exit := func(code int, stdout, stderr string) system.Result {
		return system.Result{ExitCode: code, Stdout: stdout, Stderr: stderr}
	}

	// dpkg-query: steps dùng ${Status} ("install ok installed"), checks dùng
	// ${db:Status-Status} ("installed"). Trả đúng dạng theo format được hỏi.
	m.Handle([]string{"dpkg-query"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		status := "install ok installed"
		if strings.Contains(strings.Join(c.Args, " "), "db:Status-Status") {
			status = "installed"
		}
		var out, errs strings.Builder
		for _, p := range c.Args {
			if strings.HasPrefix(p, "-") || strings.Contains(p, "$") {
				continue
			}
			if m.pkgs[p] {
				fmt.Fprintf(&out, "%s\t%s\n", p, status)
			} else {
				fmt.Fprintf(&errs, "dpkg-query: no packages found matching %s\n", p)
			}
		}
		if errs.Len() > 0 {
			return exit(1, out.String(), errs.String())
		}
		return exit(0, out.String(), "")
	})

	m.Handle([]string{"apt-get", "update"}, func(system.Cmd) system.Result {
		m.mu.Lock()
		m.updates++
		fn := m.onUpdate
		m.mu.Unlock()
		if fn != nil {
			if code := fn(); code != 0 {
				return exit(code, "", "E: giả lập apt-get update lỗi\n")
			}
		}
		return ok
	})
	m.Handle([]string{"apt-get", "install", "-y"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, p := range c.Args[2:] {
			m.install(p)
		}
		return ok
	})

	m.Handle([]string{"systemctl"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		unit := c.Args[len(c.Args)-1]
		u := m.units[unit]
		switch c.Args[0] {
		case "is-active":
			if u.active {
				return exit(0, "active\n", "")
			}
			return exit(3, "inactive\n", "")
		case "is-enabled":
			if u.enabled {
				return exit(0, "enabled\n", "")
			}
			return exit(1, "disabled\n", "")
		case "enable":
			u.enabled, u.active = true, true
		case "disable":
			u.enabled = false
		case "stop":
			u.active = false
		default: // reload, restart
			return ok
		}
		m.units[unit] = u
		return ok
	})

	m.Handle([]string{"a2query", "-m"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.mods[c.Args[1]] {
			return exit(0, c.Args[1]+" (enabled by site administrator)\n", "")
		}
		return exit(32, "", "No module matches "+c.Args[1]+"\n")
	})
	setMods := func(on bool) func(system.Cmd) system.Result {
		return func(c system.Cmd) system.Result {
			m.mu.Lock()
			defer m.mu.Unlock()
			for _, mod := range c.Args[1:] { // bỏ "-q"
				if on {
					m.mods[mod] = true
				} else {
					delete(m.mods, mod)
				}
			}
			return ok
		}
	}
	m.Handle([]string{"a2enmod"}, setMods(true))
	m.Handle([]string{"a2dismod"}, setMods(false))

	// MariaDB của Ubuntu 24.04 mới cài đã đạt cả ba điều (root unix_socket,
	// không user ẩn danh, không DB test): bước siết được bỏ qua, phép kiểm đạt.
	m.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		sql := string(c.Stdin)
		switch {
		case strings.Contains(sql, "'root_socket'"): // câu của checks.MariaDBSecure
			return exit(0, "root_socket\t1\nanonymous\t0\ntest_db\t0\n", "")
		case strings.HasPrefix(sql, "SELECT (SELECT COUNT"): // câu trạng thái của steps.MariaDBSecure
			return exit(0, "1\t0\t0\n", "")
		}
		return ok
	})

	m.Handle([]string{"ss"}, func(system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		out := m.ss
		if m.units["mariadb"].active {
			out += "LISTEN 0 80 127.0.0.1:3306 0.0.0.0:*\n"
		}
		return exit(0, out, "")
	})

	m.Handle([]string{"curl"}, func(c system.Cmd) system.Result {
		if slices.Contains(c.Args, "-w") { // checks.HTTPDefault
			m.mu.Lock()
			defer m.mu.Unlock()
			switch {
			case m.units["nginx"].active:
				return exit(52, "000", "curl: (52) Empty reply from server\n")
			case m.units["apache2"].active:
				return exit(0, "403", "")
			}
			return exit(7, "000", "curl: (7) Failed to connect\n")
		}
		i := slices.Index(c.Args, "-o")
		m.SetFile(c.Args[i+1], fakePhar, 0o644, system.Root)
		return ok
	})
	return m
}

// install giả lập những gì apt để lại trên máy sau khi cài một gói. Gọi khi
// đang giữ m.mu.
func (m *sim) install(p string) {
	m.pkgs[p] = true
	on := unitState{active: true, enabled: true}
	switch p {
	case "php-fpm":
		m.SetDir("/etc/php/8.3/fpm/pool.d", 0o755, system.Root)
		m.units["php8.3-fpm"] = on
	case "mariadb-server":
		m.SetDir("/etc/mysql/mariadb.conf.d", 0o755, system.Root)
		m.units["mariadb"] = on
	case "nginx":
		m.SetFile("/etc/nginx/sites-available/default", []byte("server {}\n"), 0o644, system.Root)
		m.SetSymlink("/etc/nginx/sites-available/default", "/etc/nginx/sites-enabled/default")
		m.SetDir("/etc/logrotate.d", 0o755, system.Root)
		m.units["nginx"] = on
	case "apache2":
		m.SetFile("/etc/apache2/sites-available/000-default.conf", []byte("<VirtualHost *:80>\n</VirtualHost>\n"), 0o644, system.Root)
		m.SetSymlink("../sites-available/000-default.conf", "/etc/apache2/sites-enabled/000-default.conf")
		m.SetDir("/etc/logrotate.d", 0o755, system.Root)
		m.units["apache2"] = on
		m.mods["mpm_prefork"] = true
	}
}

// readOnly: lệnh chỉ hỏi trạng thái máy.
func readOnly(c system.Cmd) bool {
	switch c.Name {
	case "dpkg-query", "a2query", "ss":
		return true
	case "systemctl":
		return c.Args[0] == "is-active" || c.Args[0] == "is-enabled"
	case "mariadb":
		return strings.HasPrefix(strings.TrimSpace(string(c.Stdin)), "SELECT")
	}
	return false
}

// newTestStack dựng Stack trên một sim và một store thật trong thư mục tạm.
func newTestStack(t *testing.T) (*Stack, *sim, *store.Store) {
	t.Helper()
	m := newSim(t)
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tpan.db"))
	if err != nil {
		t.Fatalf("mở store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &Stack{
		Sys:          m,
		Settings:     st,
		Journal:      st,
		WPCLIVersion: "2.12.0",
		WPCLISHA512:  fakePharSHA(),
	}, m, st
}
