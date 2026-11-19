package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/core"
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

	root.AddCommand(versionCmd(e), serveCmd(), stackCmd(e), healthCmd(e))
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

func serveCmd() *cobra.Command {
	var listen string
	c := &cobra.Command{
		Use:   "serve",
		Short: "Chạy giao diện web và API",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return server.Run(cmd.Context(), listen)
		},
	}
	c.Flags().StringVar(&listen, "listen", "127.0.0.1:8080", "địa chỉ và cổng để nghe")
	return c
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
