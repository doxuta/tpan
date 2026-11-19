package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/store"
)

func (c *cli) setting(key string) string {
	c.t.Helper()
	st, err := store.Open(context.Background(), c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	v, _, err := st.GetSetting(context.Background(), key)
	if err != nil {
		c.t.Fatal(err)
	}
	return v
}

func TestMCPConfig(t *testing.T) {
	c := newCLI(t)

	// Chưa đặt: mặc định panel, all; không ghi gì.
	code, out, _ := c.run("mcp", "config", "--json")
	var cfg core.AgentSettings
	if err := json.Unmarshal([]byte(out), &cfg); err != nil || code != 0 || cfg.Approval != core.ApprovalPanel || cfg.Redact != "all" {
		t.Fatalf("mặc định: mã %d, %+v (%v)\n%s", code, cfg, err, out)
	}
	if c.audits("mcp.config") != 0 || c.setting(core.SettingMCPApproval) != "" {
		t.Error("chỉ đọc cấu hình mà ghi setting hoặc audit")
	}

	// Bật client: ghi setting, cảnh báo, nhật ký.
	code, out, errs := c.run("mcp", "config", "--approval", "client")
	if code != 0 || !strings.Contains(out, "CẢNH BÁO") || !strings.Contains(out, "client") {
		t.Errorf("--approval client: mã %d, stdout %q, stderr %q", code, out, errs)
	}
	if v := c.setting(core.SettingMCPApproval); v != "client" {
		t.Errorf("mcp.approval = %q", v)
	}
	if n := c.audits("mcp.config"); n != 1 {
		t.Errorf("audit mcp.config: %d dòng", n)
	}

	// --json: stdout chỉ có JSON, cảnh báo ra stderr.
	code, out, errs = c.run("mcp", "config", "--approval", "client", "--redact", "secrets", "--json")
	if err := json.Unmarshal([]byte(out), &cfg); err != nil || code != 0 || cfg.Approval != core.ApprovalClient || cfg.Redact != "secrets" {
		t.Errorf("--json: mã %d, %+v (%v)\n%s", code, cfg, err, out)
	}
	if !strings.Contains(errs, "CẢNH BÁO") {
		t.Errorf("--json: cảnh báo phải ra stderr: %q", errs)
	}

	// Về panel: không cảnh báo.
	code, out, _ = c.run("mcp", "config", "--approval", "panel")
	if code != 0 || strings.Contains(out, "CẢNH BÁO") || c.setting(core.SettingMCPApproval) != "panel" {
		t.Errorf("--approval panel: mã %d, %q", code, out)
	}
	if n := c.audits("mcp.config"); n != 3 {
		t.Errorf("audit mcp.config: %d dòng, muốn 3", n)
	}
}

func TestMCPConfigRejectsBadValues(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"--approval", "auto"}, "approval_invalid"},
		{[]string{"--approval", ""}, "approval_invalid"},
		{[]string{"--approval", "client; id"}, "approval_invalid"},
		{[]string{"--redact", "none"}, "redact_invalid"},
		{[]string{"--approval", "client", "--redact", "ALL"}, "redact_invalid"},
		{[]string{"--brain", "local"}, "brain_invalid"},
		{[]string{"--brain", "BRIDGE"}, "brain_invalid"},
		{[]string{"--brain", ""}, "brain_invalid"},
		{[]string{"--approval", "panel", "--brain", "bridge; id"}, "brain_invalid"},
	} {
		c := newCLI(t)
		code, out, _ := c.run(append([]string{"mcp", "config", "--json"}, tc.args...)...)
		if code != 1 || refusalCode(t, out) != tc.code {
			t.Errorf("%q: mã %d, %q", tc.args, code, out)
		}
		// Từ chối trước khi mở DB: không ghi nửa chừng (approval đúng, redact sai).
		if c.dbExists() {
			t.Errorf("%q: giá trị sai mà vẫn mở DB", tc.args)
		}
	}
}

func TestMCPNeedsRoot(t *testing.T) {
	for _, args := range [][]string{
		{"mcp", "config"},
		{"mcp", "config", "--approval", "client"},
		{"mcp"},
	} {
		c := newCLI(t)
		c.root = false
		code, out, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "sudo") {
			t.Errorf("%q không root: mã %d, stderr %q", args, code, errs)
		}
		if out != "" || c.dbExists() {
			t.Errorf("%q không root mà in ra stdout hoặc mở DB: %q", args, out)
		}
	}
}
