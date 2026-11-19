package main

// Lệnh `tpan update` (mục 6.11 và 9 của docs/KE-HOACH.md):
//   - `tpan update --check`: làm mới danh sách gói rồi in nội dung màn Cập nhật.
//   - `tpan update`: nâng các gói của stack đang chờ (Plan update.apply), rồi
//     chạy lại toàn bộ health như `tpan health` để mở sự cố cho site không còn
//     đạt. Site nào lỗi thì in rõ và mã thoát 1 (e2e bước 15).
//   - `tpan update auto`: bật bản vá bảo mật tự cài hằng ngày (Plan update.auto).

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/store"
)

func init() { register(updateCmd) }

// bindChangeFlags gắn bốn cờ chung của lệnh có thay đổi (mục 9) cho update,
// mariadb, self-update.
func bindChangeFlags(c *cobra.Command, f *planFlags) {
	fl := c.Flags()
	fl.BoolVar(&f.dryRun, "dry-run", false, "chỉ in kế hoạch, không thay đổi gì")
	fl.BoolVar(&f.yes, "yes", false, "không hỏi xác nhận")
	fl.BoolVar(&f.asJSON, "json", false, "in kết quả dạng JSON")
	fl.BoolVar(&f.verbose, "verbose", false, "in lệnh và output của mọi bước")
}

// openUpdates dựng core.Updates. readOnly (--dry-run, --check) mà chưa có file
// DB thì không tạo DB, đọc như bảng settings và bảng sites rỗng (giống stack
// install). st nil nghĩa là không có DB.
func openUpdates(ctx context.Context, e *env, readOnly bool) (u *core.Updates, st *store.Store, done func(), err error) {
	u = &core.Updates{Sys: e.sys, Settings: core.EmptySettings{}, Sites: core.EmptySiteStore{}, Version: buildinfo.Version}
	if readOnly && !e.fileExists(e.dbPath) {
		return u, nil, func() {}, nil
	}
	if st, err = openStore(ctx, e); err != nil {
		return nil, nil, nil, err
	}
	u.Settings, u.Sites, u.Backups = st, st, st
	if !readOnly {
		u.Journal = st
	}
	return u, st, func() { closeStore(st) }, nil
}

func updateCmd(e *env) *cobra.Command {
	var (
		f     planFlags
		check bool
	)
	c := &cobra.Command{
		Use:   "update [--check]",
		Short: "Xem (--check) hoặc cập nhật các gói của stack đang có bản mới",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// --check chỉ xem: --dry-run, --yes vô nghĩa, báo luôn để người gõ
			// không tưởng là đã cập nhật gì.
			if check && (f.dryRun || f.yes) {
				return errors.New("--check không đi cùng --dry-run hay --yes")
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, f.asJSON, err)
			}
			u, st, done, err := openUpdates(ctx, e, f.dryRun || check)
			if err != nil {
				return err
			}
			defer done()

			if check {
				status, err := u.Status(ctx, true)
				if err != nil {
					return fail(e, f.asJSON, err)
				}
				if f.asJSON {
					return writeJSON(e.stdout, status)
				}
				printUpdateStatus(e, status)
				return nil
			}

			p, err := u.PrepareApply(ctx)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			rep, err := runPlanned(ctx, e, f, u.Journal, "update.apply", "stack", p)
			if rep.Job.ID == "" {
				return err
			}
			return healthAfterJob(ctx, e, st, f.asJSON, err)
		},
	}
	c.Flags().BoolVar(&check, "check", false, "làm mới danh sách gói rồi chỉ in những gì đang chờ, không thay đổi gì")
	bindChangeFlags(c, &f)
	c.AddCommand(updateAutoCmd(e))
	return c
}

func updateAutoCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "auto",
		Short: "Bật bản vá bảo mật tự cài hằng ngày (unattended-upgrades), gồm cả kho ngoài đang dùng",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, f.asJSON, err)
			}
			u, _, done, err := openUpdates(ctx, e, f.dryRun)
			if err != nil {
				return err
			}
			defer done()
			p, err := u.PrepareAuto(ctx)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			_, err = runPlanned(ctx, e, f, u.Journal, "update.auto", "unattended-upgrades", p)
			return err
		},
	}
	bindChangeFlags(c, &f)
	return c
}

// healthAfterJob chạy lại toàn bộ health (như `tpan health`, gồm phép kiểm
// nặng) sau một job đổi gói: ghi kết quả, mở sự cố cho thứ không còn đạt, in
// tóm tắt. --json thì tóm tắt ra stderr, vì stdout đã là JSON của Report.
// jobErr là kết quả của runPlanned: job hỏng thì vẫn kiểm (gói đã nâng không
// hoàn tác được) nhưng mã thoát theo job.
func healthAfterJob(ctx context.Context, e *env, st *store.Store, asJSON bool, jobErr error) error {
	w := e.stdout
	if asJSON {
		w = e.stderr
	}
	rep, err := (&health.Runner{Sys: e.sys, Store: st, DBPath: e.dbPath}).Run(ctx, health.Options{Full: true})
	if err != nil {
		err = fmt.Errorf("chạy lại health sau job: %w", err)
		if jobErr != nil {
			printf(e.stderr, "tpan: %v\n", err)
			return jobErr
		}
		return err
	}
	printf(w, "\n%s\n", e.tr("Kiểm lại toàn bộ health sau khi đổi gói:", "Re-running the full health check after the package change:"))
	failed := 0
	for _, g := range rep.Groups {
		if g.Target == store.TargetTPAN {
			continue // chính tpan (DB, lịch chạy) không do gói của stack quyết định
		}
		var ids []string
		for _, r := range g.Results {
			if r.Status == checks.Fail {
				ids = append(ids, r.ID)
			}
		}
		if len(ids) > 0 {
			failed++
			printf(w, "  %s: %s %s\n", groupName(e, g.Target), e.tr("KHÔNG ĐẠT", "FAILING"), strings.Join(ids, ", "))
		}
	}
	if failed == 0 {
		printf(w, "  %s\n", e.tr("Stack và mọi site vẫn đạt.", "The stack and every site still pass."))
	} else {
		printf(w, "  %s\n", e.tr("Xem chi tiết và cách sửa: sudo tpan health", "Details and fixes: sudo tpan health"))
	}
	if n := len(rep.Changes.Opened); n > 0 {
		printf(w, "  %s %d\n", e.tr("Sự cố vừa mở:", "Incidents opened:"), n)
	}
	if jobErr != nil {
		return jobErr
	}
	if failed > 0 {
		return &exitCode{1}
	}
	return nil
}

// printUpdateStatus in nội dung màn Cập nhật cho `tpan update --check`.
func printUpdateStatus(e *env, s core.UpdateStatus) {
	w := e.stdout
	if len(s.Packages) == 0 {
		printf(w, "%s\n", e.tr("Không có gói nào của stack đang chờ cập nhật.", "No stack package is waiting for an update."))
	} else {
		printf(w, "%s (%d)\n", e.tr("Gói của stack có bản mới", "Stack packages with a new version"), len(s.Packages))
		width := 0
		for _, p := range s.Packages {
			width = max(width, len(p.Name))
		}
		for _, p := range s.Packages {
			from := p.From
			if from == "" {
				from = e.tr("(mới)", "(new)")
			}
			sec := ""
			if p.Security {
				sec = "  [" + e.tr("bảo mật", "security") + "]"
			}
			printf(w, "  %-*s  %s -> %s%s\n", width, p.Name, from, p.To, sec)
		}
		printf(w, "%s\n", e.tr("Cập nhật: sudo tpan update", "Update: sudo tpan update"))
	}
	printf(w, "%s %d (%s %d; %s)\n", e.tr("Gói khác của hệ điều hành đang chờ:", "Other OS packages waiting:"), s.Others,
		e.tr("bảo mật", "security"), s.OthersSecurity, e.tr("bản vá bảo mật do unattended-upgrades tự cài", "security fixes are installed by unattended-upgrades"))

	printf(w, "\n")
	printResult(w, e.lang, s.Auto, "")
	if len(s.AutoOrigins) > 0 {
		printf(w, "  %s %s\n", e.tr("Kho ngoài được tự vá:", "External repositories auto-patched:"), strings.Join(s.AutoOrigins, ", "))
	}

	m := s.MariaDB
	printf(w, "\nMariaDB: ")
	if m.Version == "" {
		printf(w, "%s\n", e.tr("chưa cài", "not installed"))
	} else {
		printf(w, "%s (%s %s, %s %s)\n", m.Version, e.tr("dòng", "series"), m.Line, e.tr("nguồn", "source"), m.Source)
		for _, l := range m.Upgrades {
			printf(w, "  %s %s (%s %s): sudo tpan mariadb upgrade --to %s\n", e.tr("Nâng lên được dòng LTS", "Can upgrade to LTS series"), l.Line, e.tr("hỗ trợ tới", "supported until"), l.EOL, l.Line)
		}
	}

	printf(w, "\ntpan %s (%s %s)\n", s.Self.Version, e.tr("gốc tải", "download from"), s.Self.URL)
	printf(w, "  %s: sudo tpan self-update\n", e.tr("Cập nhật", "Update"))
	if s.Self.HasOld {
		printf(w, "  %s: sudo tpan self-update --rollback\n", e.tr("Quay về bản trước", "Go back to the previous release"))
	}
}
