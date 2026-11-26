package core

// M37 (v0.2.17): cho máy khác nối vào một DB. Hai tầng:
//
//  1. Chế độ của cả máy (chỉ admin): MariaDB nghe ở mọi địa chỉ IPv4
//     (bind-address = 0.0.0.0 trong 99-tpan.cnf, khởi động lại MariaDB) và
//     DBRemoteMarker có mặt. Chỉ bật được khi tường lửa ufw đang bật: tường lửa
//     là lớp chặn đầu, cổng 3306 chỉ mở cho IP đã khai của từng DB.
//  2. Từng DB (chủ DB hoặc admin): danh sách IPv4 được nối vào. Mỗi IP là một
//     account 'user'@'IP' cùng mật khẩu và quyền với user của DB trên máy chủ
//     (steps.DBRemoteHosts), cộng một luật ufw cho IP đó (steps.UFWDBFrom; luật
//     của mọi DB tính lại mỗi lần).
//
// Tắt chế độ: xoá mọi account máy ngoài và mọi luật ufw tpan-db trước, rồi đưa
// MariaDB về chỉ nghe 127.0.0.1. Nhờ vậy ngoài chế độ này không có account máy
// ngoài nào, và đồng bộ mật khẩu, khoá của account máy ngoài (steps.dbWithRemote)
// chỉ phải chạy khi chế độ bật. Chi tiết: docs/thay-doi/M37.md.

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

const (
	// CodeDBRemoteOff: khai máy ngoài cho một DB khi chế độ của máy đang tắt.
	CodeDBRemoteOff = "db_remote_off"
	// CodeDBRemoteNeedsFirewall: bật chế độ khi ufw chưa bật.
	CodeDBRemoteNeedsFirewall = "db_remote_needs_firewall"
	// CodeDBRemoteInvalid: danh sách IP sai (không phải IPv4 đơn, lặp, quá 10).
	CodeDBRemoteInvalid = "db_remote_invalid"
	// DBRemoteMax là số máy ngoài tối đa của một DB.
	DBRemoteMax = 10
)

var mariadbRestart = &system.Cmd{Name: "systemctl", Args: []string{"restart", "mariadb"}}

// DBRemoteStatus là trạng thái chế độ kết nối từ xa của máy.
type DBRemoteStatus struct {
	On        bool
	UFWActive bool
	// Hosts: user DB (trùng tên DB) → các IP được nối vào. Chỉ đọc khi On.
	Hosts map[string][]string
}

// RemoteStatus đọc chế độ, ufw và các account máy ngoài.
func (d *DBs) RemoteStatus(ctx context.Context) (DBRemoteStatus, error) {
	st := DBRemoteStatus{On: steps.DBRemoteOn(d.Sys)}
	var err error
	if st.UFWActive, err = steps.UFWActive(ctx, d.Sys); err != nil {
		return st, err
	}
	if st.On {
		if st.Hosts, err = steps.DBRemoteAll(ctx, d.Sys); err != nil {
			return st, err
		}
	}
	return st, nil
}

// PrepareRemoteMode bật hay tắt chế độ của máy (admin; quyền do server, CLI kiểm).
func (d *DBs) PrepareRemoteMode(ctx context.Context, on bool) (*Planned, error) {
	if on {
		active, err := steps.UFWActive(ctx, d.Sys)
		if err != nil {
			return nil, err
		}
		if !active {
			return nil, refuse(CodeDBRemoteNeedsFirewall, i18n.T(
				"tường lửa ufw đang tắt: bật nó trước ở màn Bảo mật (hoặc sudo tpan security ufw), vì khi cho kết nối từ xa tường lửa là thứ chỉ để các IP đã khai chạm được cổng 3306",
				"the ufw firewall is off: turn it on first on the Security screen (or sudo tpan security ufw), because with remote connections the firewall is what lets only the listed IPs reach port 3306"))
		}
		return newPlanned(d.Sys, d.Journal, engine.Plan{
			Kind:   "db.remote-mode",
			Target: "mariadb",
			Title: i18n.T(
				"Bật kết nối DB từ máy khác: MariaDB nghe ở mọi địa chỉ, tường lửa vẫn đóng cổng 3306 cho tới khi một DB khai IP được nối vào",
				"Turn on database connections from other machines: MariaDB listens on every address, the firewall keeps port 3306 closed until a database lists an allowed IP"),
			Steps: []engine.Step{
				&steps.File{
					Path:    steps.DBRemoteMarker,
					Content: []byte("# tpan: cho kết nối DB từ máy khác (M37). Tắt bằng: sudo tpan db remote off\n"),
					Perm:    0o644, Owner: system.Root,
					Title: i18n.T("Ghi dấu chế độ kết nối DB từ xa", "Mark remote database connections as on"),
					Why: i18n.T(
						"Có file này thì mỗi lần đổi mật khẩu, khoá, mở khoá hay xoá một user DB, tpan làm cùng việc đó cho các account máy ngoài của user đó, để không account nào giữ mật khẩu cũ hay còn mở khi DB đã khoá.",
						"With this file, every password change, lock, unlock or drop of a database user is applied to that user's outside-machine accounts too, so none keeps an old password or stays open when the database is locked."),
				},
				&steps.File{
					Path: steps.MariaDBCnfPath, Content: steps.MariaDBCnf(templates.Header, true),
					Perm: 0o644, Owner: system.Root, Reload: mariadbRestart, Danger: true,
					Title: i18n.T("MariaDB nghe ở mọi địa chỉ IPv4 (bind-address = 0.0.0.0), khởi động lại MariaDB", "MariaDB listens on every IPv4 address (bind-address = 0.0.0.0), restart MariaDB"),
					Why: i18n.T(
						"Mặc định MariaDB chỉ nghe 127.0.0.1 nên máy khác không nối vào được. Khởi động lại làm mọi site mất kết nối DB vài giây.",
						"By default MariaDB listens on 127.0.0.1 only, so no other machine can connect. Restarting drops every site's database connection for a few seconds."),
				},
			},
		}, nil), nil
	}
	all, err := steps.DBRemoteAll(ctx, d.Sys)
	if err != nil {
		return nil, err
	}
	var ss []engine.Step
	users := make([]string, 0, len(all))
	for u := range all {
		users = append(users, u)
	}
	slices.Sort(users)
	for _, u := range users {
		ss = append(ss, &steps.DBRemoteHosts{Name: u})
	}
	ss = append(ss,
		&steps.UFWDBFrom{},
		&steps.File{
			Path: steps.MariaDBCnfPath, Content: steps.MariaDBCnf(templates.Header, false),
			Perm: 0o644, Owner: system.Root, Reload: mariadbRestart, Danger: true,
			Title: i18n.T("MariaDB chỉ nghe 127.0.0.1, khởi động lại MariaDB", "MariaDB listens on 127.0.0.1 only, restart MariaDB"),
		},
		&steps.Absent{Path: steps.DBRemoteMarker,
			Title: i18n.T("Gỡ dấu chế độ kết nối DB từ xa", "Remove the remote database connections mark"),
			Why:   i18n.T("Chế độ đã tắt và không còn account máy ngoài nào.", "The mode is off and no outside-machine account is left.")},
	)
	return newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind:   "db.remote-mode",
		Target: "mariadb",
		Title: i18n.T(
			"Tắt kết nối DB từ máy khác: xoá "+fmt.Sprint(len(users))+" user DB khỏi danh sách máy ngoài, đóng cổng 3306, MariaDB chỉ nghe 127.0.0.1",
			"Turn off database connections from other machines: clear the outside machines of "+fmt.Sprint(len(users))+" database users, close port 3306, MariaDB listens on 127.0.0.1 only"),
		Steps: ss,
	}, nil), nil
}

// normRemoteHosts chuẩn hoá danh sách IP (bỏ trùng giữ thứ tự, tối đa DBRemoteMax).
func normRemoteHosts(in []string) ([]string, error) {
	out := []string{}
	for _, h := range in {
		if strings.TrimSpace(h) == "" {
			continue
		}
		c, err := steps.CheckRemoteIP(h)
		if err != nil {
			return nil, refuse(CodeDBRemoteInvalid, i18n.T(err.Error(), err.Error()))
		}
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	if len(out) > DBRemoteMax {
		return nil, refuse(CodeDBRemoteInvalid, i18n.T(
			fmt.Sprintf("tối đa %d máy ngoài cho một DB", DBRemoteMax),
			fmt.Sprintf("at most %d outside machines per database", DBRemoteMax)))
	}
	return out, nil
}

// remotePlan dựng Plan đặt danh sách máy ngoài của user DB name; target là
// tên DB rời hoặc domain của site (để job thuộc đúng chủ).
func (d *DBs) remotePlan(ctx context.Context, name, target string, hosts []string) (*Planned, error) {
	if !steps.DBRemoteOn(d.Sys) {
		return nil, refuse(CodeDBRemoteOff, i18n.T(
			"máy chủ chưa bật kết nối DB từ máy khác; quản trị viên bật ở màn Cơ sở dữ liệu (hoặc sudo tpan db remote on)",
			"the server has not turned on database connections from other machines; an administrator turns it on on the Databases screen (or sudo tpan db remote on)"))
	}
	norm, err := normRemoteHosts(hosts)
	if err != nil {
		return nil, err
	}
	dbOK, userOK, err := steps.DBNameState(ctx, d.Sys, name)
	if err != nil {
		return nil, err
	}
	if !dbOK || !userOK {
		return nil, refuse(CodeDBNotFound, i18n.T(
			"DB hoặc user DB "+name+" không còn trong MariaDB; có lại chúng trước (xem phép kiểm của DB ở màn Sự cố)",
			"the database or database user "+name+" is no longer in MariaDB; get them back first (see the database check on the Incidents screen)"))
	}
	all, err := steps.DBRemoteAll(ctx, d.Sys)
	if err != nil {
		return nil, err
	}
	// Luật tường lửa = mọi IP của mọi DB sau thay đổi này.
	var ufw []string
	add := func(ip string) {
		if _, err := steps.CheckRemoteIP(ip); err == nil && !slices.Contains(ufw, ip) {
			ufw = append(ufw, ip)
		}
	}
	for u, hs := range all {
		if u == name {
			continue
		}
		for _, h := range hs {
			add(h)
		}
	}
	for _, h := range norm {
		add(h)
	}
	slices.Sort(ufw)
	return newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind:   "db.remote",
		Target: target,
		Title: i18n.T(
			"Máy ngoài được nối vào DB "+name+": "+orNoneText(norm, "không máy nào (chỉ máy chủ)"),
			"Outside machines allowed to connect to database "+name+": "+orNoneText(norm, "none (the server only)")),
		Steps: []engine.Step{
			&steps.DBRemoteHosts{Name: name, Hosts: norm},
			&steps.UFWDBFrom{IPs: ufw},
		},
	}, nil), nil
}

func orNoneText(xs []string, none string) string {
	if len(xs) == 0 {
		return none
	}
	return strings.Join(xs, ", ")
}

// PrepareRemoteHosts đặt danh sách máy ngoài của DB rời name.
func (d *DBs) PrepareRemoteHosts(ctx context.Context, name string, hosts []string) (*Planned, error) {
	db, err := d.dbsActive(ctx, name)
	if err != nil {
		return nil, err
	}
	return d.remotePlan(ctx, db.Name, db.Name, hosts)
}

// PrepareSiteRemoteHosts đặt danh sách máy ngoài của DB của site d. Site trong
// thùng rác thì từ chối (user DB của nó đang khoá, sắp bị xoá).
func (d *DBs) PrepareSiteRemoteHosts(ctx context.Context, dom validate.Domain, hosts []string) (*Planned, error) {
	if d.Sites == nil {
		return nil, fmt.Errorf("thiếu danh sách site")
	}
	st, ok, err := d.Sites.Site(ctx, dom)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, refuse("site_not_found", i18n.T("không có site "+dom.String(), "no site "+dom.String()))
	}
	if st.Status == site.Trashed {
		return nil, refuse("site_trashed", i18n.T("site "+dom.String()+" đang ở thùng rác", dom.String()+" is in the trash"))
	}
	return d.remotePlan(ctx, st.Layout().DB, dom.String(), hosts)
}

// PrepareRemoteSync tính lại luật tường lửa theo các account máy ngoài đang có
// (sau khi một DB bị xoá hẳn, luật của IP chỉ DB đó dùng thì thừa).
func (d *DBs) PrepareRemoteSync(ctx context.Context) (*Planned, error) {
	all, err := steps.DBRemoteAll(ctx, d.Sys)
	if err != nil {
		return nil, err
	}
	var ufw []string
	for _, hs := range all {
		for _, h := range hs {
			if _, err := steps.CheckRemoteIP(h); err == nil && !slices.Contains(ufw, h) {
				ufw = append(ufw, h)
			}
		}
	}
	slices.Sort(ufw)
	return newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind: "db.remote-sync", Target: "mariadb",
		Title: i18n.T("Tính lại luật tường lửa cổng 3306 theo các máy ngoài đang được nối vào DB", "Recompute the port 3306 firewall rules from the outside machines currently allowed"),
		Steps: []engine.Step{&steps.UFWDBFrom{IPs: ufw}},
	}, nil), nil
}
