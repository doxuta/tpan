package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/demo"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/server"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// env là mọi thứ CLI lấy từ bên ngoài. Test thay bằng FakeSystem, thư mục tạm
// và bộ đệm, nên chạy được trên Mac mà không cần root.
type env struct {
	sys        system.System
	isRoot     func() bool
	isTerminal func() bool
	fileExists func(path string) bool
	openStore  func(ctx context.Context, path string) (*store.Store, error)
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer

	// Cờ chung, cobra ghi vào đây.
	dbPath string
	lang   string
}

// tr chọn câu theo --lang.
func (e *env) tr(vi, en string) string { return i18n.T(vi, en).In(e.lang) }

func newRoot(e *env) *cobra.Command {
	root := &cobra.Command{
		Use:   "tpan",
		Short: "Panel tự động hoá LAMP/LEMP và WordPress",
		// Lỗi do run() in, một dòng; không in usage mỗi lần lệnh lỗi.
		SilenceErrors:     true,
		SilenceUsage:      true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		PersistentPreRunE: func(*cobra.Command, []string) error {
			if e.lang != "vi" && e.lang != "en" {
				return fmt.Errorf("--lang phải là vi hoặc en (nhận được %q)", e.lang)
			}
			return nil
		},
	}
	root.SetIn(e.stdin)
	root.SetOut(e.stdout)
	root.SetErr(e.stderr)

	pf := root.PersistentFlags()
	pf.StringVar(&e.dbPath, "db", "/var/lib/tpan/tpan.db", "đường dẫn file SQLite")
	// Ẩn khỏi help: chỉ dùng cho test và gỡ lỗi; người dùng không cần đổi.
	_ = pf.MarkHidden("db")
	pf.StringVar(&e.lang, "lang", "vi", "ngôn ngữ của output: vi hoặc en")

	root.AddCommand(versionCmd(e), serveCmd(e), stackCmd(e), siteCmd(e), healthCmd(e))
	root.AddCommand(setupCmd(e), adminCmd(e))
	return root
}

func versionCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "In phiên bản",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			printf(e.stdout, "%s\n", buildinfo.String())
			return nil
		},
	}
}

func serveCmd(e *env) *cobra.Command {
	var (
		listen, certFile, keyFile string
		demoMode                  bool
	)
	c := &cobra.Command{
		Use:   "serve",
		Short: "Chạy giao diện web và API",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if (certFile == "") != (keyFile == "") {
				return fmt.Errorf("--tls-cert và --tls-key phải đi cùng nhau")
			}
			// Không TLS thì mật khẩu đi trên mạng dạng chữ thường (mục 11): chỉ
			// cho nghe ở loopback (máy dev, --demo).
			if certFile == "" && !isLoopbackListen(listen) {
				return fmt.Errorf("không có --tls-cert/--tls-key thì chỉ được nghe 127.0.0.1 hoặc localhost (đang là %q)", listen)
			}
			deps := server.Deps{Sys: e.sys}
			if demoMode {
				sys, st, err := demo.New(ctx, "")
				if err != nil {
					return fmt.Errorf("dựng máy demo: %w", err)
				}
				defer closeStore(st)
				// Mật khẩu demo mới mỗi lần chạy, in ra terminal: chế độ demo
				// vẫn bắt đăng nhập để giao diện đi đúng luồng như máy thật.
				pw := store.GeneratePassword()
				hash, err := store.HashPassword(pw)
				if err != nil {
					return err
				}
				if err := st.SetAdminPasswordHash(ctx, hash); err != nil {
					return err
				}
				printf(e.stdout, "%s %s\n", e.tr("Mật khẩu đăng nhập demo:", "Demo login password:"), pw)
				wpHash, wpcliHash := demo.Hashes()
				deps = server.Deps{Sys: sys, Store: st, Demo: true, Hostname: "demo", WPHash: wpHash, WPCLISHA512: wpcliHash}
			} else {
				st, err := openStore(ctx, e)
				if err != nil {
					return err
				}
				defer closeStore(st)
				host, err := setupHostname()
				if err != nil {
					return fmt.Errorf("đọc hostname: %w", err)
				}
				deps.Store, deps.Hostname = st, host
			}
			return server.Run(ctx, server.RunOptions{Listen: listen, CertFile: certFile, KeyFile: keyFile, Deps: deps})
		},
	}
	f := c.Flags()
	f.StringVar(&listen, "listen", "127.0.0.1:8080", "địa chỉ và cổng để nghe")
	f.StringVar(&certFile, "tls-cert", "", "file chứng chỉ PEM (tpan setup sinh ở /etc/tpan/tls/cert.pem)")
	f.StringVar(&keyFile, "tls-key", "", "file khoá riêng PEM của chứng chỉ")
	f.BoolVar(&demoMode, "demo", false, "chạy trên máy giả có sẵn dữ liệu mẫu, không đụng máy thật")
	return c
}

// isLoopbackListen: địa chỉ nghe chỉ nhận kết nối từ chính máy này.
func isLoopbackListen(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// requireRoot từ chối sớm khi không chạy bằng root: cài gói, đọc dịch vụ và
// ghi /var/lib/tpan đều cần root, và lỗi "permission denied" giữa chừng khó
// hiểu hơn nhiều so với một câu nói rõ ngay từ đầu.
func requireRoot(e *env, cmd *cobra.Command) error {
	if e.isRoot() {
		return nil
	}
	// CommandPath là "tpan stack install": gợi ý đúng lệnh người dùng vừa gõ.
	return &core.RefusedError{Code: "root_required", Message: i18n.T(
		"lệnh này cần quyền root; chạy lại bằng sudo: sudo "+cmd.CommandPath()+" ...",
		"this command needs root; run it again with sudo: sudo "+cmd.CommandPath()+" ...")}
}

// openStore mở DB (tạo nếu chưa có). Người gọi đóng bằng closeStore.
func openStore(ctx context.Context, e *env) (*store.Store, error) {
	st, err := e.openStore(ctx, e.dbPath)
	if err != nil {
		return nil, fmt.Errorf("mở DB %s: %w", e.dbPath, err)
	}
	return st, nil
}

// closeStore đóng DB khi lệnh xong. Lỗi lúc đóng bị bỏ qua: mỗi lần ghi đã
// commit riêng, đóng lỗi không làm mất dữ liệu đã ghi.
func closeStore(st *store.Store) { _ = st.Close() }

// fail in một lời từ chối (thân thiện, hoặc JSON khi --json) rồi trả mã thoát
// 1. Lỗi khác trả nguyên để run() in một dòng.
func fail(e *env, asJSON bool, err error) error {
	var re *core.RefusedError
	if !errors.As(err, &re) {
		return err
	}
	if asJSON {
		_ = writeJSON(e.stdout, map[string]any{"error": re})
		return &exitCode{1}
	}
	printRefusal(e, re)
	return &exitCode{1}
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// auditWriter là phần của store ghi được nhật ký. Nhận kiểu chung vì nơi gọi
// đang giữ store dưới dạng giao diện của core (Journal, SiteStore).
type auditWriter interface {
	AddAudit(ctx context.Context, a store.AuditEntry) error
}

// cliAudit ghi nhật ký cho thao tác chạy ở dòng lệnh (mục 11: mọi thao tác có
// thay đổi đều vào audit, kể cả không qua giao diện). IP là "cli". Job chưa
// tạo được (lỗi trước khi chạy) thì không ghi. Ghi lỗi chỉ in ra stderr: thao
// tác trên máy đã xong, không được báo hỏng vì nhật ký.
func cliAudit(ctx context.Context, e *env, w any, action, target string, job engine.Job) {
	aw, ok := w.(auditWriter)
	if !ok || job.ID == "" {
		return
	}
	result := "ok"
	if job.Status != engine.JobSucceeded {
		result = string(job.Status)
	}
	if err := aw.AddAudit(ctx, store.AuditEntry{Time: time.Now(), IP: "cli", Action: action, Target: target, Result: result, JobID: job.ID}); err != nil {
		printf(e.stderr, "tpan: %v\n", err)
	}
}
