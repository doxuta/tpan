package main

// Dòng lệnh của M20 (mục 3.9 và 11.9 của docs/thay-doi/M20-thiet-ke.md):
//
//	tpan user 2fa status|reset|policy   hai bước của user, chính sách bắt buộc
//	tpan token create|list|revoke       token API
//	tpan panel domain show|set          tên miền (và chứng chỉ) của panel
//
// Dòng lệnh chạy bằng root nên không bị chính sách hai bước và không bao giờ
// kiểm mã: không có cờ nào nhận mã hay token trên dòng lệnh (lọt vào ps và lịch
// sử shell). Token mới chỉ in một lần, ra stdout.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/apitoken"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/server"
	"github.com/doxuta/tpan/internal/store"
)

func init() { register(tokenCmd, panelCmd) }

// Khoá setting của M20. Server giữ cùng chuỗi (mục 3.6, 3.7).
const (
	settingRequire2FA   = "auth.require_2fa"
	settingTokenMaxDays = "auth.token_max_days"
)

// tokenIDRe: id token là 16 ký tự hex (mục 3.3).
var tokenIDRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// refusal dựng lời từ chối có mã ổn định, như core.refuse.
func refusal(code, vi, en string) *core.RefusedError {
	return &core.RefusedError{Code: code, Message: i18n.T(vi, en)}
}

// userNameArg kiểm tên user người dùng gõ. Khác parseUserArg ở chỗ nhận cả
// "admin": tên đó dành riêng cho hệ thống nên ParseUserName từ chối, mà đặt
// lại hai bước của admin bị khoá ngoài là việc chính của `user 2fa reset`.
func userNameArg(s string) (string, error) {
	if s == account.AdminName {
		return s, nil
	}
	u, err := parseUserArg(s)
	return u.String(), err
}

// invalidAsRefusal đổi lỗi của apitoken (tên, phạm vi, số ngày) thành lời từ chối.
func invalidAsRefusal(err error) error {
	var ie *apitoken.InvalidError
	if errors.As(err, &ie) {
		return refusal(ie.Code, ie.Vi, ie.En)
	}
	return err
}

// openM20Store đòi root rồi mở tpan.db có sẵn. Chưa có file thì từ chối mà
// không tạo: chưa có bảng user thì không có hai bước hay token nào để xem.
func openM20Store(cmd *cobra.Command, e *env, asJSON bool) (*store.Store, error) {
	if err := requireRoot(e, cmd); err != nil {
		return nil, fail(e, asJSON, err)
	}
	if !e.fileExists(e.dbPath) {
		return nil, fail(e, asJSON, refusal("setup_required",
			"máy chưa có tpan.db; chạy sudo tpan setup trước",
			"this machine has no tpan.db yet; run sudo tpan setup first"))
	}
	return openStore(cmd.Context(), e)
}

// m20Op là một thao tác chỉ ghi tpan.db của M20.
type m20Op struct {
	action, target string
	// prep chạy trước mọi thứ (cả --dry-run): kiểm điều kiện, chỉ đọc, và trả
	// câu "Sẽ: ..." in khi --dry-run hoặc trước câu hỏi xác nhận.
	prep func(ctx context.Context, st *store.Store) (what i18n.Text, err error)
	do   func(ctx context.Context, st *store.Store) error
	// done dựng kết quả sau lần chạy thật: thứ in khi --json, và hàm in cho người.
	done func(ctx context.Context, st *store.Store) (asJSON any, human func(), err error)
	// noConfirm: không hỏi lại (đặt chính sách là lối thoát khi bị khoá ngoài,
	// script e2e gọi không có terminal; tạo token in bí mật nên không có gì để
	// "huỷ" sau đó).
	noConfirm bool
}

// runM20Op: đòi root, kiểm (prep), --dry-run thì in việc sẽ làm, hỏi lại (trừ
// --yes hay noConfirm), chạy, ghi nhật ký (ip "cli"), in.
func runM20Op(cmd *cobra.Command, e *env, f planFlags, op m20Op) error {
	ctx := cmd.Context()
	st, err := openM20Store(cmd, e, f.asJSON)
	if err != nil {
		return err
	}
	defer closeStore(st)
	what, err := op.prep(ctx, st)
	if err != nil {
		return fail(e, f.asJSON, asRefusal(err))
	}
	text := what.In(e.lang)
	if f.dryRun {
		if f.asJSON {
			return writeJSON(e.stdout, map[string]any{"dry_run": true, "plan": text})
		}
		printf(e.stdout, "%s\n%s\n", text, e.tr("(--dry-run: chưa thay đổi gì)", "(--dry-run: nothing changed)"))
		return nil
	}
	if !f.yes && !op.noConfirm {
		if f.asJSON || !e.isTerminal() {
			return fail(e, f.asJSON, refusal("confirm_required",
				"không hỏi xác nhận được (không có terminal hoặc đang dùng --json); xem trước bằng --dry-run rồi chạy lại với --yes",
				"cannot ask for confirmation (no terminal, or --json is set); preview with --dry-run, then run again with --yes"))
		}
		printf(e.stdout, "%s\n", text)
		if !confirm(e) {
			printf(e.stdout, "%s\n", e.tr("Đã huỷ, không thay đổi gì.", "Cancelled, nothing changed."))
			return &exitCode{1}
		}
	}
	if err := op.do(ctx, st); err != nil {
		return fail(e, f.asJSON, asRefusal(err))
	}
	auditNoJob(ctx, e, st, op.action, op.target)
	v, human, err := op.done(ctx, st)
	if err != nil {
		return err
	}
	if f.asJSON {
		return writeJSON(e.stdout, v)
	}
	human()
	return nil
}

// requireUser đọc user (kể cả "admin"); không có thì user_not_found.
func requireUser(ctx context.Context, st *store.Store, name string) (account.User, error) {
	u, ok, err := st.User(ctx, name)
	if err != nil {
		return account.User{}, fmt.Errorf("đọc user %q: %w", name, err)
	}
	if !ok {
		return account.User{}, refusal(core.CodeUserNotFound,
			"không có user "+name+"; xem danh sách bằng: sudo tpan user list",
			"there is no user "+name+"; list users with: sudo tpan user list")
	}
	return u, nil
}

// readPolicy đọc auth.require_2fa; chưa đặt hay giá trị lạ (sửa tay) là off,
// như server.
func readPolicy(ctx context.Context, st *store.Store) (string, error) {
	v, ok, err := st.GetSetting(ctx, settingRequire2FA)
	if err != nil {
		return "", fmt.Errorf("đọc setting %s: %w", settingRequire2FA, err)
	}
	switch v {
	case server.PolicyAdmin, server.PolicyAll:
		if ok {
			return v, nil
		}
	}
	return server.PolicyOff, nil
}

// policyRequires: chính sách p có bắt user có vai role phải bật hai bước không.
func policyRequires(p string, role account.Role) bool {
	return p == server.PolicyAll || (p == server.PolicyAdmin && role == account.RoleAdmin)
}

// ---- tpan user 2fa ----

// user2FACmd là `tpan user 2fa`; user.go gắn nó vào `tpan user`.
func user2FACmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "2fa",
		Short: "Đăng nhập hai bước: xem tình trạng, gỡ khi user mất điện thoại, đặt chính sách bắt buộc",
	}
	c.AddCommand(user2FAStatusCmd(e), user2FAResetCmd(e), user2FAPolicyCmd(e))
	return c
}

// mfaRow là một dòng của `user 2fa status --json`.
type mfaRow struct {
	User         string       `json:"user"`
	Role         account.Role `json:"role"`
	Enabled      bool         `json:"enabled"`
	EnabledAt    time.Time    `json:"enabled_at,omitzero"`
	RecoveryLeft int          `json:"recovery_left"`
	// Required: chính sách hiện tại bắt user này phải bật.
	Required bool `json:"required"`
}

func user2FAStatusCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status [<tên>]",
		Short: "Bảng hai bước của user: vai, đã bật chưa, từ lúc nào, còn mấy mã dự phòng, có bị chính sách bắt không",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			var only string
			if len(args) == 1 {
				var err error
				if only, err = userNameArg(args[0]); err != nil {
					return fail(e, asJSON, err)
				}
			}
			st, err := openM20Store(cmd, e, asJSON)
			if err != nil {
				return err
			}
			defer closeStore(st)
			policy, err := readPolicy(ctx, st)
			if err != nil {
				return err
			}
			all, err := st.MFAStatuses(ctx)
			if err != nil {
				return err
			}
			rows := []mfaRow{}
			for _, s := range all {
				if only != "" && s.User != only {
					continue
				}
				rows = append(rows, mfaRow{User: s.User, Role: s.Role, Enabled: s.Enabled, EnabledAt: s.EnabledAt,
					RecoveryLeft: s.RecoveryLeft, Required: policyRequires(policy, s.Role)})
			}
			if only != "" && len(rows) == 0 {
				_, err := requireUser(ctx, st, only)
				return fail(e, asJSON, err)
			}
			if asJSON {
				return writeJSON(e.stdout, rows)
			}
			w := e.stdout
			printf(w, "%-20s %-6s %-6s %-17s %-10s %s\n", e.tr("TÊN", "NAME"), e.tr("VAI", "ROLE"), e.tr("BẬT", "ON"),
				e.tr("TỪ LÚC", "SINCE"), e.tr("MÃ DỰ PHÒNG", "RECOVERY"), e.tr("CHÍNH SÁCH", "POLICY"))
			for _, r := range rows {
				on, since, left, req := e.tr("không", "no"), "-", "-", "-"
				if r.Enabled {
					on, since, left = e.tr("có", "yes"), r.EnabledAt.Local().Format("2006-01-02 15:04"), strconv.Itoa(r.RecoveryLeft)
				}
				if r.Required {
					req = e.tr("bắt buộc", "required")
				}
				printf(w, "%-20s %-6s %-6s %-17s %-10s %s\n", r.User, r.Role, on, since, left, req)
			}
			printf(w, "\n%s %s\n", e.tr("Chính sách bắt buộc:", "Required by policy:"), policy)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

// liveTokens là các token còn sống (chưa thu hồi, chưa hết hạn) của user.
func liveTokens(ctx context.Context, st *store.Store, user string, now time.Time) ([]server.TokenRow, error) {
	all, err := st.Tokens(ctx, user)
	if err != nil {
		return nil, err
	}
	out := []server.TokenRow{}
	for _, t := range all {
		if r := tokenJSON(t, now); r.Status == "active" {
			out = append(out, r)
		}
	}
	return out, nil
}

func user2FAResetCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "reset <tên>",
		Short: "Gỡ hai bước của user (bí mật, mã dự phòng, phiếu chờ) và đăng xuất mọi phiên của họ",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := userNameArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			var hadTOTP bool
			return runM20Op(cmd, e, f, m20Op{
				action: "user.2fa_reset", target: name,
				prep: func(ctx context.Context, st *store.Store) (i18n.Text, error) {
					if _, err := requireUser(ctx, st, name); err != nil {
						return i18n.Text{}, err
					}
					return i18n.T(
						"Sẽ: gỡ hai bước của "+name+" (bí mật, mã dự phòng, phiếu chờ) và đăng xuất mọi phiên của họ. Token API giữ nguyên.",
						"Will: remove two-step sign-in of "+name+" (secret, recovery codes, pending sign-ins) and end every session of theirs. API tokens stay."), nil
				},
				do: func(ctx context.Context, st *store.Store) error {
					// Không có dòng hai bước: ResetTOTP vẫn đã xoá phiên, chỉ báo "chưa bật".
					err := st.ResetTOTP(ctx, name)
					hadTOTP = err == nil
					if errors.Is(err, store.ErrNotFound) {
						return nil
					}
					return err
				},
				done: func(ctx context.Context, st *store.Store) (any, func(), error) {
					toks, err := liveTokens(ctx, st, name, time.Now())
					if err != nil {
						return nil, nil, err
					}
					return map[string]any{"user": name, "was_enabled": hadTOTP, "active_tokens": toks}, func() {
						if hadTOTP {
							printf(e.stdout, "%s\n", e.tr("Đã gỡ hai bước của "+name+".", "Two-step sign-in of "+name+" removed."))
						} else {
							printf(e.stdout, "%s\n", e.tr(name+" chưa bật hai bước nên không có gì để gỡ.", name+" had no two-step sign-in, so there was nothing to remove."))
						}
						printf(e.stdout, "%s\n", e.tr("Mọi phiên của "+name+" đã bị đăng xuất.", "Every session of "+name+" has been ended."))
						if len(toks) == 0 {
							return
						}
						printf(e.stdout, "%s\n", e.tr(
							name+" còn token API đang sống (nếu nghi mật khẩu đã lộ, thu hồi chúng):",
							name+" still has live API tokens (if the password may be exposed, revoke them):"))
						for _, t := range toks {
							printf(e.stdout, "  %-16s %-24s %s    sudo tpan token revoke %s\n", t.ID, t.Name, t.Hint, t.ID)
						}
					}, nil
				},
			})
		},
	}
	bindChangeFlags(c, &f)
	return c
}

func user2FAPolicyCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "policy [off|admin|all]",
		Short: "Xem hoặc đặt chính sách bắt buộc hai bước: off (không), admin (chỉ admin), all (mọi user). Lối thoát khi bị khoá ngoài panel",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				st, err := openM20Store(cmd, e, f.asJSON)
				if err != nil {
					return err
				}
				defer closeStore(st)
				p, err := readPolicy(cmd.Context(), st)
				if err != nil {
					return err
				}
				if f.asJSON {
					return writeJSON(e.stdout, map[string]string{"policy": p})
				}
				printf(e.stdout, "%s %s\n", e.tr("Chính sách bắt buộc hai bước:", "Two-step sign-in policy:"), p)
				return nil
			}
			p := args[0]
			if p != server.PolicyOff && p != server.PolicyAdmin && p != server.PolicyAll {
				return fail(e, f.asJSON, refusal("policy_invalid",
					"chính sách phải là off, admin hoặc all (nhận được "+quote(p)+")",
					"the policy must be off, admin or all (got "+quote(p)+")"))
			}
			return runM20Op(cmd, e, f, m20Op{
				action: "auth.require_2fa", target: p, noConfirm: true,
				prep: func(ctx context.Context, st *store.Store) (i18n.Text, error) {
					return i18n.T("Sẽ: đặt chính sách bắt buộc hai bước là "+p+".", "Will: set the two-step sign-in policy to "+p+"."), nil
				},
				do: func(ctx context.Context, st *store.Store) error {
					return st.SetSetting(ctx, settingRequire2FA, p)
				},
				done: func(context.Context, *store.Store) (any, func(), error) {
					return map[string]string{"policy": p}, func() {
						printf(e.stdout, "%s %s\n", e.tr("Đã đặt chính sách bắt buộc hai bước:", "Two-step sign-in policy set to:"), p)
						if p != server.PolicyOff {
							printf(e.stdout, "%s\n", e.tr(
								"User thuộc diện bắt buộc mà chưa bật sẽ bị chuyển sang trang bật hai bước ở request kế tiếp.",
								"Users covered by the policy who have not turned it on are sent to the enrolment page on their next request."))
						}
					}, nil
				},
			})
		},
	}
	bindChangeFlags(c, &f)
	return c
}

// ---- tpan token ----

// tokenJSON đổi một dòng token của store sang dạng của API, tính trạng thái
// lúc now: thu hồi thắng hết hạn.
func tokenJSON(t store.TokenRow, now time.Time) server.TokenRow {
	status := "active"
	switch {
	case !t.Revoked.IsZero():
		status = "revoked"
	case !now.Before(t.Expires):
		status = "expired"
	}
	scopes := []string{}
	if t.Scopes != "" {
		scopes = strings.Split(t.Scopes, ",")
	}
	return server.TokenRow{ID: t.ID, User: t.User, Name: t.Name, Scopes: scopes, Hint: t.Hint, Created: t.Created,
		Expires: t.Expires, Revoked: t.Revoked, LastUsed: t.LastUsed, LastIP: t.LastIP, Status: status}
}

func tokenCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "token",
		Short: "Token API: tạo, xem, thu hồi",
	}
	c.AddCommand(tokenCreateCmd(e), tokenListCmd(e), tokenRevokeCmd(e))
	return c
}

// tokenMaxDays đọc trần số ngày; chưa đặt hay hỏng là mặc định.
func tokenMaxDays(ctx context.Context, st *store.Store) (int, error) {
	v, ok, err := st.GetSetting(ctx, settingTokenMaxDays)
	if err != nil {
		return 0, fmt.Errorf("đọc setting %s: %w", settingTokenMaxDays, err)
	}
	if n, perr := strconv.Atoi(v); ok && perr == nil && n >= 1 {
		return n, nil
	}
	return apitoken.DefaultMaxDays, nil
}

// newTokenID là 16 ký tự hex ngẫu nhiên.
func newTokenID() string {
	var b [8]byte
	_, _ = rand.Read(b[:]) // từ Go 1.24 Read không bao giờ trả lỗi
	return hex.EncodeToString(b[:])
}

func tokenCreateCmd(e *env) *cobra.Command {
	var (
		f      planFlags
		name   string
		scopes []string
		days   int
	)
	c := &cobra.Command{
		Use:   "create <user> --name N --scope s[,s...] --days D",
		Short: "Tạo token API cho user (in ra MỘT lần). Phạm vi dạng sites:read, backups:write...",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			user, err := userNameArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			sc, err := apitoken.ParseScopes(scopes)
			if err != nil {
				return fail(e, f.asJSON, invalidAsRefusal(err))
			}
			var (
				tok string
				row store.TokenRow
			)
			return runM20Op(cmd, e, f, m20Op{
				action: "token.create", noConfirm: true,
				prep: func(ctx context.Context, st *store.Store) (i18n.Text, error) {
					u, err := requireUser(ctx, st, user)
					if err != nil {
						return i18n.Text{}, err
					}
					if u.Status != account.Active {
						return i18n.Text{}, refusal("user_suspended",
							"user "+user+" đang tạm khoá nên không tạo token được",
							"user "+user+" is suspended, so no token can be created")
					}
					maxDays, err := tokenMaxDays(ctx, st)
					if err != nil {
						return i18n.Text{}, err
					}
					if err := apitoken.CheckNew(u.Role == account.RoleAdmin, name, sc, days, maxDays); err != nil {
						return i18n.Text{}, invalidAsRefusal(err)
					}
					return i18n.T(
						fmt.Sprintf("Sẽ: tạo token API %q cho %s, phạm vi %s, sống %d ngày; token in ra một lần.", name, user, sc, days),
						fmt.Sprintf("Will: create API token %q for %s, scopes %s, valid %d days; the token is printed once.", name, user, sc, days)), nil
				},
				do: func(ctx context.Context, st *store.Store) error {
					now := time.Now()
					tok = apitoken.New()
					row = store.TokenRow{ID: newTokenID(), User: user, Name: name, Scopes: sc.String(), Hint: apitoken.Hint(tok),
						Created: now, Expires: now.AddDate(0, 0, days)}
					return st.CreateToken(ctx, row, apitoken.Hash(tok))
				},
				done: func(context.Context, *store.Store) (any, func(), error) {
					return server.TokenCreated{Token: tok, Row: tokenJSON(row, time.Now())}, func() {
						printf(e.stdout, "%s\n", e.tr("Đã tạo token "+row.ID+" cho "+user+".", "Token "+row.ID+" created for "+user+"."))
						printOncePassword(e, e.tr("Token API", "API token"), tok)
						printf(e.stdout, "%s\n", e.tr("Dùng: header Authorization: Bearer <token>. Thu hồi bằng: sudo tpan token revoke "+row.ID,
							"Use it as the header Authorization: Bearer <token>. Revoke it with: sudo tpan token revoke "+row.ID))
					}, nil
				},
			})
		},
	}
	fl := c.Flags()
	fl.StringVar(&name, "name", "", "tên gợi nhớ của token (1-64 ký tự)")
	fl.StringSliceVar(&scopes, "scope", nil, "phạm vi, cách nhau bằng dấu phẩy: sites:read,jobs:read (nhóm system chỉ cho admin)")
	fl.IntVar(&days, "days", 0, "số ngày token còn dùng được (tối đa theo auth.token_max_days, mặc định 90)")
	bindChangeFlags(c, &f)
	return c
}

func tokenListCmd(e *env) *cobra.Command {
	var (
		asJSON bool
		user   string
	)
	c := &cobra.Command{
		Use:   "list [--user U]",
		Short: "Liệt kê token (chỉ hint, không bao giờ in token): người chủ, phạm vi, hạn, lần dùng cuối, trạng thái",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("user") {
				var err error
				if user, err = userNameArg(user); err != nil {
					return fail(e, asJSON, err)
				}
			}
			st, err := openM20Store(cmd, e, asJSON)
			if err != nil {
				return err
			}
			defer closeStore(st)
			ts, err := st.Tokens(cmd.Context(), user)
			if err != nil {
				return err
			}
			now := time.Now()
			rows := make([]server.TokenRow, len(ts))
			for i, t := range ts {
				rows[i] = tokenJSON(t, now)
			}
			if asJSON {
				return writeJSON(e.stdout, rows)
			}
			if len(rows) == 0 {
				printf(e.stdout, "%s\n", e.tr("Chưa có token nào. Tạo bằng: sudo tpan token create <user> --name N --scope sites:read --days 30",
					"No tokens yet. Create one with: sudo tpan token create <user> --name N --scope sites:read --days 30"))
				return nil
			}
			printf(e.stdout, "%-16s %-14s %-20s %-30s %-14s %-17s %-17s %s\n", "ID", e.tr("CHỦ", "OWNER"), e.tr("TÊN", "NAME"),
				e.tr("PHẠM VI", "SCOPES"), "HINT", e.tr("HẾT HẠN", "EXPIRES"), e.tr("DÙNG CUỐI", "LAST USED"), e.tr("TRẠNG THÁI", "STATUS"))
			for _, r := range rows {
				used := "-"
				if !r.LastUsed.IsZero() {
					used = r.LastUsed.Local().Format("2006-01-02 15:04")
					if r.LastIP != "" {
						used += " " + r.LastIP
					}
				}
				printf(e.stdout, "%-16s %-14s %-20s %-30s %-14s %-17s %-17s %s\n", r.ID, r.User, clipText(r.Name, 20),
					clipText(strings.Join(r.Scopes, ","), 30), r.Hint, r.Expires.Local().Format("2006-01-02 15:04"), used, tokenStatusText(e, r.Status))
			}
			return nil
		},
	}
	c.Flags().StringVar(&user, "user", "", "chỉ token của user này")
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

// clipText cắt s còn n ký tự (tính theo rune) cho cột của bảng.
func clipText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func tokenStatusText(e *env, s string) string {
	switch s {
	case "active":
		return e.tr("đang dùng", "active")
	case "expired":
		return e.tr("hết hạn", "expired")
	}
	return e.tr("đã thu hồi", "revoked")
}

func tokenRevokeCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Thu hồi một token (id lấy từ: sudo tpan token list)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if !tokenIDRe.MatchString(id) {
				return fail(e, f.asJSON, refusal("token_not_found",
					"không có token nào có id "+quote(id)+"; id gồm 16 ký tự a-f, 0-9 (xem bằng: sudo tpan token list)",
					"there is no token with id "+quote(id)+"; an id is 16 characters a-f, 0-9 (see: sudo tpan token list)"))
			}
			return runM20Op(cmd, e, f, m20Op{
				action: "token.revoke", target: id,
				prep: func(ctx context.Context, st *store.Store) (i18n.Text, error) {
					all, err := st.Tokens(ctx, "")
					if err != nil {
						return i18n.Text{}, err
					}
					for _, t := range all {
						if t.ID != id {
							continue
						}
						if !t.Revoked.IsZero() {
							return i18n.Text{}, refusal("token_not_found", "token "+id+" đã được thu hồi từ trước", "token "+id+" was already revoked")
						}
						return i18n.T(
							fmt.Sprintf("Sẽ: thu hồi token %s (%q của %s). Công cụ đang dùng nó bị 401 ngay.", id, t.Name, t.User),
							fmt.Sprintf("Will: revoke token %s (%q of %s). Tools using it get 401 at once.", id, t.Name, t.User)), nil
					}
					return i18n.Text{}, refusal("token_not_found", "không có token "+id+" (xem bằng: sudo tpan token list)", "there is no token "+id+" (see: sudo tpan token list)")
				},
				do: func(ctx context.Context, st *store.Store) error {
					err := st.RevokeToken(ctx, id, "", time.Now())
					if errors.Is(err, store.ErrNotFound) {
						return refusal("token_not_found", "token "+id+" đã được thu hồi hay không còn", "token "+id+" is already revoked or gone")
					}
					return err
				},
				done: func(context.Context, *store.Store) (any, func(), error) {
					return map[string]string{"revoked": id}, func() {
						printf(e.stdout, "%s\n", e.tr("Đã thu hồi token "+id+".", "Token "+id+" revoked."))
					}, nil
				},
			})
		},
	}
	bindChangeFlags(c, &f)
	return c
}

// ---- tpan panel domain ----

func panelCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "panel",
		Short: "Cài đặt của panel: tên miền và chứng chỉ Let's Encrypt",
	}
	d := &cobra.Command{Use: "domain", Short: "Tên miền của panel: xem, đặt, bỏ"}
	d.AddCommand(panelDomainShowCmd(e), panelDomainSetCmd(e))
	c.AddCommand(d)
	return c
}

// newPanel dựng core.Panel trên cùng DB với m. Sessions chỉ gán khi có store
// thật (nil trần bỏ vào interface thì khác nil): sau khi đổi hay bỏ tên, core
// xoá phiên của tên cũ và báo lỗi nếu thiếu chỗ xoá.
func newPanel(m *m13Env) *core.Panel {
	p := &core.Panel{Sys: m.sites.Sys, Settings: m.sites.Settings, Store: m.sites.Store, Journal: m.sites.Journal}
	if m.st != nil {
		p.Sessions = m.st
	}
	return p
}

func panelDomainShowCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show",
		Short: "Tên miền đang đặt cho panel và tình trạng chứng chỉ của nó",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			m, done, err := openM13(ctx, e, true) // chỉ đọc: chưa có tpan.db thì không tạo
			if err != nil {
				return err
			}
			defer done()
			d, ok, err := newPanel(m).Domain(ctx)
			if err != nil {
				return err
			}
			state := server.PanelState{}
			if ok {
				state.Domain = d.String()
				r := checks.PanelACMECert(d, time.Now).Run(ctx, e.sys)
				state.Cert = &r
			}
			if asJSON {
				return writeJSON(e.stdout, state)
			}
			if !ok {
				printf(e.stdout, "%s\n", e.tr(
					"Panel chưa có tên miền (dùng chứng chỉ tự ký). Đặt bằng: sudo tpan panel domain set <tên miền>",
					"The panel has no domain (it uses the self-signed certificate). Set one with: sudo tpan panel domain set <domain>"))
				return nil
			}
			printf(e.stdout, "%-12s %s\n", e.tr("Tên miền:", "Domain:"), state.Domain)
			printf(e.stdout, "%-12s [%s] %s\n", e.tr("Chứng chỉ:", "Certificate:"), statusWord(e.lang, state.Cert.Status), state.Cert.Summary.In(e.lang))
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func panelDomainSetCmd(e *env) *cobra.Command {
	var (
		f     planFlags
		clear bool
	)
	c := &cobra.Command{
		Use:   "set <domain> | --clear",
		Short: "Đặt tên miền cho panel và xin chứng chỉ Let's Encrypt cho nó (--clear: bỏ tên, về chứng chỉ tự ký)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if clear == (len(args) == 1) {
				return fail(e, f.asJSON, refusal("args_invalid",
					"đưa đúng một trong hai: <domain> để đặt tên, hoặc --clear để bỏ tên",
					"give exactly one of: <domain> to set the name, or --clear to drop it"))
			}
			if clear {
				return runM13Plan(cmd, e, f, false, func(ctx context.Context, m *m13Env) (*core.Planned, error) {
					return newPanel(m).PrepareClear(ctx)
				}, nil, nil)
			}
			d, err := parseDomainArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			return runM13Plan(cmd, e, f, false, func(ctx context.Context, m *m13Env) (*core.Planned, error) {
				return newPanel(m).PrepareDomain(ctx, d)
			}, nil, nil)
		},
	}
	c.Flags().BoolVar(&clear, "clear", false, "bỏ tên miền của panel (xoá chứng chỉ của nó)")
	bindChangeFlags(c, &f)
	return c
}
