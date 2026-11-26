package steps

// M37 (v0.2.17): cho máy khác nối vào một DB (DB rời hay DB của site).
//
// Cách làm của MariaDB: mỗi máy được nối là một account riêng 'tên'@'IP', cùng
// tên với user 'tên'@'localhost' của DB. TPAN tạo account đó bằng chính hash mật
// khẩu của 'tên'@'localhost' (chép trong MariaDB, hash không bao giờ ra Go hay
// log), cấp cùng quyền ALL trên đúng DB đó, và khoá nếu account localhost đang
// khoá. Người dùng chỉ có một mật khẩu cho mọi chỗ.
//
// Giữ các account đó luôn đồng bộ với 'tên'@'localhost' mà không phải sửa từng
// bước: mọi câu SQL của tpan đi qua dbRun, và dbRun (dbWithRemote) chép mỗi câu
// ALTER USER, DROP USER IF EXISTS, KILL USER nhắm vào 'tên'@'localhost' sang mọi
// account 'tên'@'<host khác>' trong cùng lần chạy mariadb. Đổi mật khẩu, khoá,
// mở khoá, cắt kết nối, trần kết nối, xoá user: tự áp cho cả máy ngoài. Việc
// này chỉ chạy khi máy đang ở chế độ cho kết nối từ ngoài (có DBRemoteMarker):
// ngoài chế độ đó không có account máy ngoài nào (tắt chế độ là xoá hết chúng
// trước), và mọi thứ chạy y như trước M37.

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// DBRemoteMarker: có file này là máy đang cho kết nối DB từ ngoài (MariaDB nghe
// ở mọi địa chỉ, tường lửa chỉ mở cổng 3306 cho các IP đã khai).
const DBRemoteMarker = "/etc/tpan/db-remote"

// DBRemotePort là cổng MariaDB mở ra ngoài.
const DBRemotePort = "3306"

// DBRemoteUFWComment đánh dấu luật ufw do tpan thêm cho cổng 3306; tpan chỉ gỡ
// luật mang dấu này.
const DBRemoteUFWComment = "tpan-db"

// DBRemoteOn cho biết máy đang ở chế độ cho kết nối DB từ ngoài.
func DBRemoteOn(sys system.System) bool {
	_, err := sys.Stat(DBRemoteMarker)
	return err == nil
}

var (
	// dbLocalUserStmtRe: câu nhắm vào account localhost của một user DB, cần chép
	// sang account máy ngoài. CREATE USER và GRANT không ở đây: account máy ngoài
	// chỉ do DBRemoteHosts tạo.
	dbLocalUserStmtRe = regexp.MustCompile(`^(ALTER USER|DROP USER IF EXISTS|KILL USER) '([a-z0-9_]{1,64})'@'localhost'(.*)$`)
	// dbHostRe: giá trị cột Host được nhúng vào '...'. Account do tpan tạo luôn
	// là một IPv4; host khác (người quản trị tự tạo, ví dụ '%') vẫn nhận nếu
	// không có ký tự nào làm vỡ dấu nháy.
	dbHostRe = regexp.MustCompile(`^[0-9A-Za-z:._%-]{1,60}$`)
)

// dbWithRemote: xem đầu file. sql không đổi khi không ở chế độ máy ngoài hoặc
// không có câu nào nhắm vào account localhost.
func dbWithRemote(ctx context.Context, sys system.System, sql string) (string, error) {
	if !DBRemoteOn(sys) {
		return sql, nil
	}
	type stmt struct{ verb, name, rest string }
	var hits []stmt
	names := map[string]bool{}
	for line := range strings.SplitSeq(sql, "\n") {
		if m := dbLocalUserStmtRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			hits = append(hits, stmt{m[1], m[2], m[3]})
			names[m[2]] = true
		}
	}
	if len(hits) == 0 {
		return sql, nil
	}
	hosts, err := dbRemoteHostsOf(ctx, sys, slices.Collect(func(yield func(string) bool) {
		for n := range names {
			if !yield(n) {
				return
			}
		}
	}))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(sql)
	if !strings.HasSuffix(sql, "\n") {
		b.WriteString("\n")
	}
	for _, h := range hits {
		for _, host := range hosts[h.name] {
			b.WriteString(h.verb + " '" + h.name + "'@'" + host + "'" + h.rest + "\n")
		}
	}
	return b.String(), nil
}

// dbRemoteHostsOf đọc host khác localhost của các user names (tên đã qua
// dbNameRe). Host lạ (có ký tự không nhúng được vào '...') là lỗi: tpan không
// bỏ qua nó, vì bỏ qua là để một account máy ngoài lệch mật khẩu hay khoá.
func dbRemoteHostsOf(ctx context.Context, sys system.System, names []string) (map[string][]string, error) {
	sort.Strings(names)
	var in []string
	for _, n := range names {
		if !dbNameRe.MatchString(n) {
			return nil, fmt.Errorf("tên user DB %q không hợp lệ", n)
		}
		in = append(in, "'"+n+"'")
	}
	q := "SELECT User, Host FROM mysql.global_priv WHERE Host<>'localhost' AND User IN (" + strings.Join(in, ",") + ") ORDER BY User, Host;\n"
	res, err := sys.Run(ctx, mariadbCmd(q))
	if err != nil {
		return nil, withStdout(fmt.Errorf("đọc các máy ngoài được nối vào user DB %s: %w", strings.Join(names, ", "), err), res)
	}
	out := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(res.Stdout), "\n") {
		if line == "" {
			continue
		}
		u, h, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, fmt.Errorf("đọc host của user DB: dòng lạ %q", line)
		}
		if !dbHostRe.MatchString(h) {
			return nil, fmt.Errorf("user DB %s có account với host lạ %q (không do tpan tạo); tpan không đụng tới user này cho tới khi người quản trị xoá account đó: mariadb -e \"SELECT User, Host FROM mysql.global_priv WHERE User='%s'\"", u, h, u)
		}
		out[u] = append(out[u], h)
	}
	return out, nil
}

// DBRemoteHostsOf trả các máy ngoài (host khác localhost) của user DB name.
func DBRemoteHostsOf(ctx context.Context, sys system.System, name string) ([]string, error) {
	m, err := dbRemoteHostsOf(ctx, sys, []string{name})
	return m[name], err
}

// DBRemoteAll trả mọi user DB có account máy ngoài và host của chúng (một câu SQL).
func DBRemoteAll(ctx context.Context, sys system.System) (map[string][]string, error) {
	res, err := sys.Run(ctx, mariadbCmd("SELECT User, Host FROM mysql.global_priv WHERE Host NOT IN ('localhost','127.0.0.1','::1') AND User<>'' AND User NOT IN ('root','mysql','mariadb.sys') ORDER BY User, Host;\n"))
	if err != nil {
		return nil, withStdout(fmt.Errorf("đọc các account máy ngoài của MariaDB: %w", err), res)
	}
	out := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(res.Stdout), "\n") {
		if u, h, ok := strings.Cut(line, "\t"); ok {
			out[u] = append(out[u], h)
		}
	}
	return out, nil
}

// CheckRemoteIP kiểm một địa chỉ máy ngoài: IPv4 đơn (không dải, không tên),
// không phải loopback, 0.0.0.0, multicast hay broadcast. Trả dạng chuẩn.
func CheckRemoteIP(s string) (string, error) {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	switch {
	case err != nil:
		return "", fmt.Errorf("%q không phải một địa chỉ IPv4", s)
	case !a.Is4():
		return "", fmt.Errorf("%q: chỉ nhận địa chỉ IPv4", s)
	case a.IsLoopback(), a.IsUnspecified(), a.IsMulticast(), a == netip.MustParseAddr("255.255.255.255"):
		return "", fmt.Errorf("%q không phải địa chỉ của một máy ngoài", s)
	}
	return a.String(), nil
}

// ================================================================ DBRemoteHosts

// DBRemoteHosts đặt đúng tập máy ngoài được nối vào DB Name (Hosts, IPv4 đã qua
// CheckRemoteIP): thêm account 'Name'@'IP' còn thiếu (hash mật khẩu, quyền và
// trạng thái khoá chép từ 'Name'@'localhost'), xoá account thừa. Check: tập
// host hiện có đúng bằng Hosts. Undo: đặt lại tập trước Apply (account đã xoá
// được tạo lại cùng cách, vẫn cùng mật khẩu với localhost).
type DBRemoteHosts struct {
	Name  string
	Hosts []string

	prev    []string
	applied bool
}

var _ engine.Step = (*DBRemoteHosts)(nil)

func (s *DBRemoteHosts) validate() error {
	if !dbNameRe.MatchString(s.Name) {
		return fmt.Errorf("tên user DB %q không hợp lệ", s.Name)
	}
	for i, h := range s.Hosts {
		c, err := CheckRemoteIP(h)
		if err != nil {
			return err
		}
		if c != h {
			return fmt.Errorf("địa chỉ %q chưa ở dạng chuẩn (%s)", h, c)
		}
		if slices.Contains(s.Hosts[:i], h) {
			return fmt.Errorf("địa chỉ %s bị lặp", h)
		}
	}
	return nil
}

// dbGrantTo là câu GRANT của user name trên DB cùng tên, cho host.
func dbGrantTo(name, host string) string {
	return "GRANT ALL PRIVILEGES ON `" + strings.ReplaceAll(name, "_", `\_`) + "`.* TO '" + name + "'@'" + host + "';\n"
}

// dbRemoteAddSQL tạo account 'name'@'host' theo account localhost. Hash đọc vào
// biến của phiên MariaDB, không ra output. User localhost không có thì @a là
// NULL, CONCAT ra NULL và EXECUTE IMMEDIATE báo lỗi: không tạo account mồ côi.
func dbRemoteAddSQL(name, host string) string {
	q := "''"
	return "SET @a := (SELECT JSON_VALUE(Priv, '$.authentication_string') FROM mysql.global_priv WHERE User='" + name + "' AND Host='localhost');\n" +
		"SET @l := (SELECT JSON_VALUE(Priv, '$.account_locked') FROM mysql.global_priv WHERE User='" + name + "' AND Host='localhost');\n" +
		"EXECUTE IMMEDIATE CONCAT('CREATE USER " + q + name + q + "@" + q + host + q + " IDENTIFIED BY PASSWORD " + q + "', @a, '" + q + " WITH MAX_USER_CONNECTIONS 20');\n" +
		dbGrantTo(name, host) +
		"EXECUTE IMMEDIATE IF(@l IN ('1', 'true'), 'ALTER USER " + q + name + q + "@" + q + host + q + " ACCOUNT LOCK', 'DO 0');\n"
}

func dbRemoteDropSQL(name, host string) string {
	return "DROP USER IF EXISTS '" + name + "'@'" + host + "';\n"
}

func (s *DBRemoteHosts) Info() engine.StepInfo {
	var cmds []string
	for _, h := range s.Hosts {
		cmds = append(cmds, dbShow(dbRemoteAddSQL(s.Name, h)))
	}
	return engine.StepInfo{
		ID: "mariadb.remote-hosts:" + s.Name,
		Title: i18n.T(
			"Đặt các máy ngoài được nối vào DB "+s.Name+": "+orNone(s.Hosts, "không máy nào"),
			"Set the outside machines allowed to connect to database "+s.Name+": "+orNone(s.Hosts, "no machine")),
		Why: i18n.T(
			"MariaDB cho đăng nhập theo cặp user và địa chỉ máy: mỗi máy ngoài cần một account riêng 'user'@'IP'. TPAN tạo account đó bằng đúng mật khẩu và quyền của user DB trên máy chủ (chỉ trên DB này), khoá nếu DB đang tạm khoá, và xoá account của máy không còn trong danh sách. Máy không có trong danh sách thì không đăng nhập được dù biết mật khẩu.",
			"MariaDB logs in by user and machine address: each outside machine needs its own 'user'@'IP' account. TPAN creates it with the same password and privileges as the database user on the server (this database only), locks it if the database is suspended, and drops the accounts of machines no longer listed. A machine not on the list cannot log in even with the password."),
		Commands: cmds,
		Danger:   true,
	}
}

func (s *DBRemoteHosts) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	cur, err := DBRemoteHostsOf(ctx, sys, s.Name)
	if err != nil {
		return false, err
	}
	return sameSet(cur, s.Hosts), nil
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

// setTo đưa tập host của user về want. Thêm trước, xoá sau.
func (s *DBRemoteHosts) setTo(ctx context.Context, sys system.System, cur, want []string, what string) error {
	var sql strings.Builder
	for _, h := range want {
		if !slices.Contains(cur, h) {
			sql.WriteString(dbRemoteAddSQL(s.Name, h))
		}
	}
	for _, h := range cur {
		if !slices.Contains(want, h) {
			sql.WriteString(dbRemoteDropSQL(s.Name, h))
		}
	}
	if sql.Len() == 0 {
		return nil
	}
	// Không qua dbRun: các câu ở đây nhắm vào account máy ngoài, không cần chép.
	res, err := sys.Run(ctx, mariadbCmd(sql.String()))
	if err != nil {
		return withStdout(fmt.Errorf("%s: %w", what, err), res)
	}
	return nil
}

func (s *DBRemoteHosts) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	cur, err := DBRemoteHostsOf(ctx, sys, s.Name)
	if err != nil {
		return err
	}
	for _, h := range cur {
		if _, err := CheckRemoteIP(h); err != nil {
			return fmt.Errorf("user DB %s có account máy ngoài %q không do tpan tạo; tpan không sửa danh sách này cho tới khi người quản trị xoá account đó", s.Name, h)
		}
	}
	if err := s.setTo(ctx, sys, cur, s.Hosts, "đặt các máy ngoài của user DB "+s.Name); err != nil {
		// Trả lại như cũ trước khi báo lỗi: engine không Undo bước có Apply lỗi.
		now, rerr := DBRemoteHostsOf(ctx, sys, s.Name)
		if rerr == nil {
			rerr = s.setTo(ctx, sys, now, cur, "trả lại các máy ngoài của user DB "+s.Name)
		}
		return errors.Join(err, rerr)
	}
	s.prev, s.applied = cur, true
	return nil
}

func (s *DBRemoteHosts) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("máy ngoài của user DB %s: %w", s.Name, errNotApplied)
	}
	cur, err := DBRemoteHostsOf(ctx, sys, s.Name)
	if err != nil {
		return err
	}
	return s.setTo(ctx, sys, cur, s.prev, "trả lại các máy ngoài của user DB "+s.Name)
}

// =================================================================== UFWDBFrom

// UFWDBFrom đặt đúng tập IP được qua tường lửa tới cổng 3306: mỗi IP một luật
// "allow from IP to any port 3306 proto tcp" mang chú thích tpan-db. Chỉ đụng
// luật mang chú thích đó; luật người quản trị tự thêm giữ nguyên. Check: tập
// luật tpan-db đúng bằng IPs. Undo: đặt lại tập trước Apply.
type UFWDBFrom struct {
	IPs []string

	prev    []string
	applied bool
}

var _ engine.Step = (*UFWDBFrom)(nil)

func ufwDBRule(ip string) []string {
	return []string{"from", ip, "to", "any", "port", DBRemotePort, "proto", "tcp"}
}

func (s *UFWDBFrom) validate() error {
	for i, ip := range s.IPs {
		c, err := CheckRemoteIP(ip)
		if err != nil {
			return err
		}
		if c != ip || slices.Contains(s.IPs[:i], ip) {
			return fmt.Errorf("địa chỉ %q bị lặp hoặc chưa ở dạng chuẩn", ip)
		}
	}
	return nil
}

func (s *UFWDBFrom) Info() engine.StepInfo {
	var cmds []string
	for _, ip := range s.IPs {
		cmds = append(cmds, ufwRun(append(append([]string{"allow"}, ufwDBRule(ip)...), "comment", DBRemoteUFWComment)...).String())
	}
	return engine.StepInfo{
		ID: "ufw.db-from",
		Title: i18n.T(
			"Tường lửa: chỉ mở cổng 3306 cho "+orNone(s.IPs, "không máy nào"),
			"Firewall: open port 3306 only to "+orNone(s.IPs, "no machine")),
		Why: i18n.T(
			"MariaDB nghe ở mọi địa chỉ khi bật kết nối từ xa. Tường lửa là lớp chặn đầu: chỉ các IP đã khai mới chạm được cổng 3306, máy khác không thấy cổng này để dò mật khẩu.",
			"MariaDB listens on every address once remote connections are on. The firewall is the first barrier: only the listed IPs reach port 3306, other machines cannot see the port to guess passwords."),
		Commands: cmds,
	}
}

func orNone(xs []string, none string) string {
	if len(xs) == 0 {
		return none
	}
	return strings.Join(xs, ", ")
}

// UFWDBCurrent đọc các IP đang có luật tpan-db cho cổng 3306 (`ufw show added`,
// đọc được cả khi ufw tắt): dòng dạng
// "ufw allow from 1.2.3.4 to any port 3306 proto tcp comment 'tpan-db'".
func UFWDBCurrent(ctx context.Context, sys system.System) ([]string, error) {
	res, err := sys.Run(ctx, ufwRun("show", "added"))
	if err != nil {
		return nil, withStdout(fmt.Errorf("đọc luật ufw đã thêm: %w", err), res)
	}
	var out []string
	for line := range strings.SplitSeq(res.Stdout, "\n") {
		f := strings.Fields(line)
		if len(f) >= 11 && f[0] == "ufw" && f[1] == "allow" && slices.Equal(f[2:10], ufwDBRule(f[3])) && strings.Contains(line, "comment '"+DBRemoteUFWComment+"'") {
			out = append(out, f[3])
		}
	}
	return out, nil
}

func (s *UFWDBFrom) setTo(ctx context.Context, sys system.System, cur, want []string) error {
	for _, ip := range want {
		if slices.Contains(cur, ip) {
			continue
		}
		args := append(append([]string{"allow"}, ufwDBRule(ip)...), "comment", DBRemoteUFWComment)
		if res, err := sys.Run(ctx, ufwRun(args...)); err != nil {
			return withStdout(fmt.Errorf("mở cổng 3306 cho %s trên ufw: %w", ip, err), res)
		}
	}
	for _, ip := range cur {
		if slices.Contains(want, ip) {
			continue
		}
		args := append([]string{"delete", "allow"}, ufwDBRule(ip)...)
		if res, err := sys.Run(ctx, ufwRun(args...)); err != nil {
			return withStdout(fmt.Errorf("đóng cổng 3306 với %s trên ufw: %w", ip, err), res)
		}
	}
	return nil
}

func (s *UFWDBFrom) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	cur, err := UFWDBCurrent(ctx, sys)
	return err == nil && sameSet(cur, s.IPs), err
}

func (s *UFWDBFrom) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	cur, err := UFWDBCurrent(ctx, sys)
	if err != nil {
		return err
	}
	if err := s.setTo(ctx, sys, cur, s.IPs); err != nil {
		now, rerr := UFWDBCurrent(ctx, sys)
		if rerr == nil {
			rerr = s.setTo(ctx, sys, now, cur)
		}
		return errors.Join(err, rerr)
	}
	s.prev, s.applied = cur, true
	return nil
}

func (s *UFWDBFrom) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("luật ufw cổng 3306: %w", errNotApplied)
	}
	cur, err := UFWDBCurrent(ctx, sys)
	if err != nil {
		return err
	}
	return s.setTo(ctx, sys, cur, s.prev)
}

// UFWActive cho biết ufw đang bật (`ufw status`: dòng đầu "Status: active").
func UFWActive(ctx context.Context, sys system.System) (bool, error) {
	res, err := sys.Run(ctx, ufwRun("status"))
	if err != nil {
		return false, withStdout(fmt.Errorf("hỏi trạng thái ufw: %w", err), res)
	}
	return strings.Contains(res.Stdout, "Status: active"), nil
}

// MariaDBCnfPath là file cấu hình tpan ghi cho MariaDB (bind-address).
const MariaDBCnfPath = "/etc/mysql/mariadb.conf.d/99-tpan.cnf"

// MariaDBCnf là nội dung file đó: chỉ nghe 127.0.0.1, hoặc nghe mọi địa chỉ
// IPv4 khi bật kết nối từ xa.
func MariaDBCnf(header string, remote bool) []byte {
	bind := "127.0.0.1"
	if remote {
		bind = "0.0.0.0"
	}
	return []byte(header + "\n[mysqld]\nbind-address = " + bind + "\n")
}
