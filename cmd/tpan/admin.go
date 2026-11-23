package main

// tpan admin passwd: đặt lại mật khẩu quản trị khi quên (mục 11 của
// docs/KE-HOACH.md). Không có đường lấy lại qua web hay email: ai có root trên
// máy thì đặt lại được, ai không có thì không.

import (
	"strconv"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
)

func init() { register(adminCmd) }

func adminCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "admin",
		Short: "Quản lý tài khoản quản trị của panel",
	}
	c.AddCommand(adminPasswdCmd(e))
	return c
}

func adminPasswdCmd(e *env) *cobra.Command {
	var dryRun, yes, asJSON bool
	c := &cobra.Command{
		Use:   "passwd",
		Short: "Sinh mật khẩu quản trị mới, huỷ mọi phiên đăng nhập và gỡ mọi IP đang bị khoá",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			what := e.tr(
				"Sẽ: tạo mật khẩu quản trị mới (in ra một lần), huỷ MỌI phiên đang đăng nhập, gỡ mọi IP đang bị khoá, ghi nhật ký.",
				"Will: generate a new admin password (printed once), end EVERY logged-in session, unlock every locked IP, write the audit log.")
			if dryRun {
				if asJSON {
					return writeJSON(e.stdout, map[string]any{"dry_run": true, "plan": what})
				}
				printf(e.stdout, "%s\n", what)
				return nil
			}
			if !yes {
				if asJSON || !e.isTerminal() {
					return fail(e, asJSON, &core.RefusedError{Code: "confirm_required", Message: i18n.T(
						"không hỏi xác nhận được (không có terminal hoặc đang dùng --json); chạy lại với --yes",
						"cannot ask for confirmation (no terminal, or --json is set); run again with --yes")})
				}
				printf(e.stdout, "%s\n", what)
				if !confirm(e) {
					printf(e.stdout, "%s\n", e.tr("Đã huỷ, không thay đổi gì.", "Cancelled, nothing changed."))
					return &exitCode{1}
				}
			}

			st, err := openStore(ctx, e)
			if err != nil {
				return err
			}
			defer closeStore(st)
			// Đặt lại mật khẩu thu hồi mọi token của admin (mục 11.5): đếm trước.
			revoked, err := st.ActiveTokens(ctx, account.AdminName, setupNow())
			if err != nil {
				return err
			}
			pw := store.GeneratePassword()
			hash, err := store.HashPassword(pw)
			if err != nil {
				return err
			}
			// Hash, xoá phiên, gỡ khoá trong một transaction.
			if err := st.ResetAdminPassword(ctx, hash, setupNow()); err != nil {
				return err
			}
			if err := st.AddAudit(ctx, store.AuditEntry{
				Time: setupNow(), IP: "cli", Action: "admin.passwd", Result: "ok",
			}); err != nil {
				// Mật khẩu đã đổi rồi: vẫn phải in ra, nếu không thì không ai vào được.
				printf(e.stderr, "tpan: %v\n", err)
			}
			if asJSON {
				return writeJSON(e.stdout, map[string]string{"admin_password": pw})
			}
			printf(e.stdout, "%s\n    %s\n%s\n", e.tr(
				"Mật khẩu quản trị mới (chỉ hiện MỘT lần, hãy lưu lại ngay):",
				"New admin password (shown ONCE, save it now):"), pw, e.tr(
				"Mọi phiên đăng nhập đã bị huỷ và mọi IP bị khoá đã được gỡ.",
				"Every session has been ended and every locked IP unlocked."))
			if revoked > 0 {
				printf(e.stdout, "%s\n", e.tr("Đã thu hồi "+strconv.Itoa(revoked)+" token API của admin.", strconv.Itoa(revoked)+" API token(s) of admin revoked."))
			}
			return nil
		},
	}
	f := c.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "chỉ in việc sẽ làm, không thay đổi gì")
	f.BoolVar(&yes, "yes", false, "không hỏi xác nhận")
	f.BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}
