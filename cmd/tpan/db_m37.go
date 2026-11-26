package main

// M37 (v0.2.17): `tpan db remote` — kết nối DB từ máy khác.
//
//   tpan db remote                         trạng thái: chế độ, ufw, máy ngoài của từng DB
//   tpan db remote on | off                bật, tắt chế độ của máy
//   tpan db remote set <domain|tên DB> [IP...]   đặt danh sách máy ngoài (không IP: chỉ máy chủ)
//   tpan db remote sync                    tính lại luật tường lửa cổng 3306

import (
	"context"
	"sort"
	"strings"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/spf13/cobra"
)

func dbRemoteCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "remote [on|off|set|sync]",
		Short: "Kết nối DB từ máy khác: xem trạng thái; on, off (cả máy, cần tường lửa ufw đang bật); set <domain|tên DB> [IP...]; sync",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			m, done, err := openM13(ctx, e, !e.fileExists(e.dbPath))
			if err != nil {
				return err
			}
			defer done()
			st, err := m.dbs.RemoteStatus(ctx)
			if err != nil {
				return fail(e, asJSON, err)
			}
			if asJSON {
				return writeJSON(e.stdout, map[string]any{"on": st.On, "ufw_active": st.UFWActive, "port": steps.DBRemotePort, "hosts": st.Hosts})
			}
			onOff := func(b bool, y, n string) string {
				if b {
					return y
				}
				return n
			}
			printf(e.stdout, "%s %s\n", e.tr("Kết nối DB từ máy khác:", "Database connections from other machines:"), onOff(st.On, e.tr("BẬT", "ON"), e.tr("tắt", "off")))
			printf(e.stdout, "%s %s\n", e.tr("Tường lửa ufw:", "ufw firewall:"), onOff(st.UFWActive, e.tr("đang bật", "active"), e.tr("đang tắt", "inactive")))
			if !st.On {
				printf(e.stdout, "%s\n", e.tr("Bật bằng: sudo tpan db remote on", "Turn on with: sudo tpan db remote on"))
				return nil
			}
			users := make([]string, 0, len(st.Hosts))
			for u := range st.Hosts {
				users = append(users, u)
			}
			sort.Strings(users)
			if len(users) == 0 {
				printf(e.stdout, "%s\n", e.tr("Chưa DB nào cho máy ngoài nối vào.", "No database allows outside machines yet."))
			}
			for _, u := range users {
				printf(e.stdout, "  %-32s %s\n", u, strings.Join(st.Hosts[u], ", "))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	c.AddCommand(dbRemoteModeCmd(e, true), dbRemoteModeCmd(e, false), dbRemoteSetCmd(e), dbRemoteSyncCmd(e))
	return c
}

func dbRemoteModeCmd(e *env, on bool) *cobra.Command {
	var f planFlags
	use, short := "on", "Bật: MariaDB nghe ở mọi địa chỉ IPv4 (khởi động lại MariaDB). Cần tường lửa ufw đang bật; cổng 3306 vẫn đóng cho tới khi một DB khai IP"
	if !on {
		use, short = "off", "Tắt: xoá mọi account máy ngoài và luật tường lửa của cổng 3306, MariaDB chỉ nghe 127.0.0.1 (khởi động lại MariaDB)"
	}
	c := &cobra.Command{
		Use: use, Short: short, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runM13Plan(cmd, e, f, false, func(ctx context.Context, m *m13Env) (*core.Planned, error) {
				return m.dbs.PrepareRemoteMode(ctx, on)
			}, nil, nil)
		},
	}
	bindChangeFlags(c, &f)
	return c
}

func dbRemoteSetCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "set <domain|tên DB> [IP...]",
		Short: "Đặt danh sách IPv4 được nối vào DB (thay cả danh sách cũ; không IP nào: chỉ máy chủ). Mật khẩu dùng chung với user DB trên máy chủ",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, name, err := parseDBArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			hosts := args[1:]
			return runM13Plan(cmd, e, f, false, func(ctx context.Context, m *m13Env) (*core.Planned, error) {
				if !d.IsZero() {
					return m.dbs.PrepareSiteRemoteHosts(ctx, d, hosts)
				}
				return m.dbs.PrepareRemoteHosts(ctx, name, hosts)
			}, nil, nil)
		},
	}
	bindChangeFlags(c, &f)
	return c
}

func dbRemoteSyncCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "sync",
		Short: "Tính lại luật tường lửa cổng 3306 theo các máy ngoài đang được nối vào DB (sau khi xoá hẳn một DB)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runM13Plan(cmd, e, f, false, func(ctx context.Context, m *m13Env) (*core.Planned, error) {
				return m.dbs.PrepareRemoteSync(ctx)
			}, nil, nil)
		},
	}
	bindChangeFlags(c, &f)
	return c
}
