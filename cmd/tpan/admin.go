package main

// tpan admin passwd: đặt lại mật khẩu quản trị khi quên (mục 11 của
// docs/KE-HOACH.md). Không có đường lấy lại qua web hay email: ai có root trên
// máy thì đặt lại được, ai không có thì không. tpan admin unlock (M28 đợt 2):
// gỡ khoá đăng nhập của một nguồn mà không đổi mật khẩu.

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"

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
	c.AddCommand(adminPasswdCmd(e), adminUnlockCmd(e))
	return c
}

// adminUnlockCmd: tpan admin unlock [<khoá nguồn>] (M28 đợt 2, S10). Gỡ khoá
// "sai mật khẩu quá nhiều lần" mà không đổi mật khẩu, không huỷ phiên: lối ra
// khi admin dùng chung nguồn với người đã làm khoá (cùng NAT, hay một site trên
// máy). Khoá nguồn in trong lời 423 của panel: địa chỉ IPv4, dải /64 của IPv6,
// uid:<n> hay loopback. Không đối số thì gỡ mọi khoá.
func adminUnlockCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "unlock [<khoá nguồn>]",
		Short: "Gỡ khoá đăng nhập của một nguồn (hoặc mọi nguồn), không đổi mật khẩu",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			key := ""
			if len(args) == 1 {
				key = args[0]
				// Khoá nguồn chỉ là địa chỉ, dải /64 hay uid:<n>: chặn chuỗi lạ
				// trước khi nó vào nhật ký.
				if key == "" || len(key) > 64 || strings.ContainsFunc(key, func(r rune) bool { return r <= ' ' || r > '~' }) {
					return fail(e, asJSON, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
						"khoá nguồn không hợp lệ; chép đúng chuỗi trong lời báo khoá của panel (ví dụ 203.0.113.5, 2001:db8:1:2::/64, uid:1001)",
						"invalid source key; copy it exactly from the panel's lock message (e.g. 203.0.113.5, 2001:db8:1:2::/64, uid:1001)")})
				}
			}
			st, err := openStore(ctx, e)
			if err != nil {
				return err
			}
			defer closeStore(st)
			var n int64
			if key == "" {
				n, err = st.ClearAllLoginFailures(ctx)
			} else {
				var found bool
				found, err = st.ClearLoginFailures(ctx, key)
				if found {
					n = 1
				}
			}
			if err != nil {
				return err
			}
			if err := st.AddAudit(ctx, store.AuditEntry{
				Time: setupNow(), IP: "cli", Action: "admin.unlock", Target: key, Result: "ok",
			}); err != nil {
				printf(e.stderr, "tpan: %v\n", err)
			}
			if asJSON {
				return writeJSON(e.stdout, map[string]any{"unlocked": n})
			}
			switch {
			case key != "" && n == 0:
				printf(e.stdout, "%s\n", e.tr("Nguồn "+key+" không bị khoá (hoặc đã hết khoá); không có gì để gỡ.",
					"Source "+key+" is not locked (or the lock already expired); nothing to remove."))
			case key != "":
				printf(e.stdout, "%s\n", e.tr("Đã gỡ khoá đăng nhập của "+key+".", "Unlocked logins from "+key+"."))
			default:
				printf(e.stdout, "%s\n", e.tr("Đã gỡ mọi khoá đăng nhập ("+strconv.FormatInt(n, 10)+" nguồn).",
					"Removed every login lock ("+strconv.FormatInt(n, 10)+" sources)."))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
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
			pw := store.GeneratePassword()
			// Tiến trình CLI riêng: chờ chỗ argon2 không hạn, không bao giờ "bận".
			hash, err := store.HashPasswordWait(pw)
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
			return nil
		},
	}
	f := c.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "chỉ in việc sẽ làm, không thay đổi gì")
	f.BoolVar(&yes, "yes", false, "không hỏi xác nhận")
	f.BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}
