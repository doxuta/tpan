package core

// Ca sử dụng của màn Cập nhật (mục 6.11 của docs/KE-HOACH.md): gói của stack
// có bản mới, bản vá bảo mật tự động, nâng MariaDB. Hợp đồng M12 đợt 0; thân
// do thợ đợt 2 viết.
//
// Status: `apt-get -s -o Debug::NoLocking=1 dist-upgrade` (chỉ đọc), tách gói
// của stack (tên là hoặc bắt đầu bằng: nginx, apache2, mariadb-, galera-,
// php, certbot, python3-certbot, curl, libcurl) khỏi phần còn lại. refresh=true (chỉ CLI `tpan update --check`) thì lấy khoá
// toàn cục và chạy `apt-get update` trước. MariaDB.Upgrades và ghi chú hỏi mạng qua curl
// của Sys với timeout ngắn; không mạng thì để trống, không lỗi.
//
// Plan "update.apply" (tpan update): AptUpdate luôn chạy (lists mới nhất), rồi
// steps.AptUpgrade{gói của stack đang chờ} (giữ file cấu hình cũ:
// --force-confdef --force-confold), kiểm config (nginx -t | apachectl
// configtest, php-fpm<bản> -t cho mỗi bản đã cài), steps.ServiceRestart cho
// dịch vụ có gói vừa nâng (web server, php<bản>-fpm, mariadb). Kiểm chứng:
// stackChecks (site do health chạy ngay sau job lo, xem verifyAll). Không có gói nào chờ thì từ
// chối nothing_to_update. CLI và server chạy lại toàn bộ health (health.Runner)
// sau job để mở sự cố cho site không còn đạt.
//
// Plan "update.auto" (bật bản vá tự động): AptInstall unattended-upgrades; File
// /etc/apt/apt.conf.d/52tpan-unattended-upgrades (templates.UnattendedUpgrades
// với origin của các kho ngoài đang dùng: "origin=nginx", "origin=MariaDB",
// "origin=deb.sury.org"). Kiểm chứng: checks.UnattendedUpgrades.
//
// Plan "mariadb.upgrade" (tpan mariadb upgrade --to <dòng>), một chiều, không
// hoàn tác tự động (mục 6.11): steps.MariaDBDump vào
// /var/lib/tpan/backups/mariadb-upgrade-<thời điểm>.sql (kiểm file xuất);
// steps.ServiceStop mariadb; steps.AptRepo của MariaDB dòng mới; AptUpgrade các
// gói mariadb đang cài; Service mariadb; steps.MariaDBUpgradeRun; kiểm mọi site.
// Plan.Title và Why của bước đầu nói rõ đường lùi: cài lại dòng cũ rồi nhập lại
// từ file xuất. Từ chối: mariadb_line_invalid, mariadb_not_newer (chỉ nâng lên).
// Thành công thì after ghi stack.db_source = mariadb.org, stack.db_line.
//
// Ghi chú khi viết thân (đợt 2):
//   - ListsUpdated để trống: system.FileInfo chưa có thời điểm sửa file (mtime),
//     và lời giao việc bảo không thêm lệnh `stat` chỉ để đọc nó.
//   - templates.UnattendedUpgrades tự thêm "origin=" trước mỗi giá trị, nên ở
//     đây truyền origin trần (nginx, MariaDB, deb.sury.org).
//   - Kiểm config nằm trong lệnh Test của từng bước khởi động lại (sai thì
//     không khởi động lại), và `php-fpm<bản> -t` của mọi bản đã cài nằm trong
//     kiểm chứng.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// ---------------------------------------------------------------- đọc apt

// aptSimulateCmd hỏi apt "nâng hết thì sẽ cài gì" mà không đổi gì.
// Debug::NoLocking để chạy được cả khi một job khác đang giữ khoá dpkg.
var aptSimulateCmd = system.Cmd{Name: "apt-get", Args: []string{"-s", "-o", "Debug::NoLocking=1", "dist-upgrade"}}

// aptUpdateCmd làm mới danh sách gói. Chờ khoá dpkg tối đa 5 phút như mọi lệnh
// apt-get của M12 (apt-daily, unattended-upgrades chạy nền giữ khoá).
var aptUpdateCmd = system.Cmd{Name: "apt-get", Args: []string{"update", "-o", "DPkg::Lock::Timeout=300"}}

// stackPrefixes: gói "của stack" là gói có tên bằng hoặc bắt đầu bằng một
// trong các chuỗi này. Gói khác do unattended-upgrades lo (bản vá bảo mật).
var stackPrefixes = []string{"nginx", "apache2", "mariadb-", "galera-", "php", "certbot", "python3-certbot", "curl", "libcurl"}

func stackPackage(name string) bool {
	return slices.ContainsFunc(stackPrefixes, func(p string) bool { return strings.HasPrefix(name, p) })
}

// parseAptInst đọc các dòng "Inst" của `apt-get -s dist-upgrade`:
//
//	Inst libc6 [2.39-0ubuntu8.9] (2.39-0ubuntu8.12 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [arm64])
//	Inst linux-image-6.8.0-90 (6.8.0-90.91 Ubuntu:24.04/noble-updates [arm64])   (gói mới, chưa có [bản cũ])
//	Inst libperl5.38t64 [5.38.2-3.2build2] (5.38.2-3.2ubuntu0.2 ... [arm64]) [perl:arm64 ]
//
// Mọi thứ sau dấu ")" đầu tiên bị bỏ (apt ghi chú gói phụ thuộc ở đó). Tên có
// đuôi ":<arch>" (gói nhiều kiến trúc) được cắt đuôi và gộp một dòng. Dòng
// không đúng dạng bị bỏ qua: đây là ghi chú cho người đọc, không phải điều
// kiện an toàn.
func parseAptInst(out string) []PendingPackage {
	var pkgs []PendingPackage
	seen := map[string]bool{}
	for line := range strings.Lines(out) {
		rest, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "Inst ")
		if !ok {
			continue
		}
		name, rest, _ := strings.Cut(rest, " ")
		name, _, _ = strings.Cut(name, ":")
		var from string
		if r, ok := strings.CutPrefix(rest, "["); ok {
			if from, rest, ok = strings.Cut(r, "]"); !ok {
				continue
			}
			rest = strings.TrimSpace(rest)
		}
		inner, ok := strings.CutPrefix(rest, "(")
		if !ok {
			continue
		}
		if inner, _, ok = strings.Cut(inner, ")"); !ok {
			continue
		}
		// Bỏ "[arch]" ở cuối ngoặc.
		if i := strings.LastIndex(inner, " ["); i >= 0 && strings.HasSuffix(inner, "]") {
			inner = inner[:i]
		}
		to, kho, _ := strings.Cut(inner, " ")
		if name == "" || to == "" || seen[name] {
			continue
		}
		seen[name] = true
		p := PendingPackage{Name: name, From: from, To: to, Origins: []string{}}
		for o := range strings.SplitSeq(kho, ",") {
			if o = strings.TrimSpace(o); o != "" {
				p.Origins = append(p.Origins, o)
				p.Security = p.Security || strings.HasSuffix(o, "-security")
			}
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

// pending đọc gói đang chờ, tách gói của stack (sắp theo tên) khỏi phần còn lại.
func (u *Updates) pending(ctx context.Context) (stack, others []PendingPackage, err error) {
	res, err := u.Sys.Run(ctx, aptSimulateCmd)
	if err != nil {
		return nil, nil, fmt.Errorf("hỏi apt gói nào có bản mới: %w", err)
	}
	stack = []PendingPackage{}
	for _, p := range parseAptInst(res.Stdout) {
		if stackPackage(p.Name) {
			stack = append(stack, p)
		} else {
			others = append(others, p)
		}
	}
	slices.SortFunc(stack, func(a, b PendingPackage) int { return strings.Compare(a.Name, b.Name) })
	return stack, others, nil
}

// ---------------------------------------------------------------- Status

// Status đọc nội dung màn Cập nhật. refresh chạy apt-get update trước (lấy khoá).
func (u *Updates) Status(ctx context.Context, refresh bool) (UpdateStatus, error) {
	if refresh {
		if err := u.refreshLists(ctx); err != nil {
			return UpdateStatus{}, err
		}
	}
	stack, others, err := u.pending(ctx)
	if err != nil {
		return UpdateStatus{}, err
	}
	st := UpdateStatus{Packages: stack, Others: len(others)}
	for _, p := range others {
		if p.Security {
			st.OthersSecurity++
		}
	}
	st.Auto = checks.UnattendedUpgrades().Run(ctx, u.Sys)
	if st.AutoOrigins, err = u.externalOrigins(); err != nil {
		return UpdateStatus{}, err
	}
	if st.MariaDB, err = u.mariadbStatus(ctx); err != nil {
		return UpdateStatus{}, err
	}
	if st.Self, err = (&SelfUpdate{Sys: u.Sys, Settings: u.Settings, Version: u.Version}).Status(ctx); err != nil {
		return UpdateStatus{}, err
	}
	return st, nil
}

// refreshLists chạy apt-get update dưới khoá toàn cục: không chen vào giữa một
// job đang cài gói.
func (u *Updates) refreshLists(ctx context.Context) error {
	unlock, err := u.Sys.Lock(ctx)
	if err != nil {
		return fmt.Errorf("chờ khoá toàn cục để làm mới danh sách gói: %w", err)
	}
	defer unlock()
	if _, err := steps.RunAptUpdate(ctx, u.Sys, aptUpdateCmd); err != nil {
		return fmt.Errorf("làm mới danh sách gói: %w", err)
	}
	return nil
}

// externalOrigins là origin của các kho ngoài Ubuntu đang được thêm (có file
// sources của tpan), theo thứ tự cố định. Đây cũng là danh sách Plan
// update.auto ghi vào file tự vá.
func (u *Updates) externalOrigins() ([]string, error) {
	out := []string{}
	for _, r := range [][2]string{{RepoNginx, OriginNginx}, {RepoMariaDB, OriginMariaDB}, {RepoSury, OriginSury}} {
		ok, err := repoEnabled(u.Sys, r[0])
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, r[1])
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- MariaDB

// mariadbLineRE lấy "11.8" từ bản gói đã bỏ epoch: "11.8.5+maria~ubu2404",
// "10.11.14-0ubuntu0.24.04.1".
var mariadbLineRE = regexp.MustCompile(`^([0-9]+)\.([0-9]+)`)

// dbLineOfVersion suy dòng MariaDB từ bản của gói mariadb-server
// ("1:10.11.14-0ubuntu0.24.04.1" ra "10.11"). Không đọc được thì "".
func dbLineOfVersion(v string) string {
	if _, after, ok := strings.Cut(v, ":"); ok {
		v = after
	}
	m := mariadbLineRE.FindStringSubmatch(v)
	if m == nil {
		return ""
	}
	return m[1] + "." + m[2]
}

// cmpDBLine so hai dòng dạng "11.8" theo số (10.11 lớn hơn 10.6). Cả hai phải
// khớp dbLineRE.
func cmpDBLine(a, b string) int {
	num := func(s string) (int, int) {
		x, y, _ := strings.Cut(s, ".")
		maj, _ := strconv.Atoi(x)
		mnr, _ := strconv.Atoi(y)
		return maj, mnr
	}
	am, an := num(a)
	bm, bn := num(b)
	if am != bm {
		return am - bm
	}
	return an - bn
}

// mariadbVersion trả bản của gói mariadb-server đang cài; chưa cài thì "".
// Lấy thêm trạng thái gói: dpkg vẫn nhớ bản của gói đã gỡ mà còn file cấu hình.
func mariadbVersion(ctx context.Context, sys system.System) (string, error) {
	res, err := sys.Run(ctx, system.Cmd{Name: "dpkg-query", Args: []string{"-W", "-f", `${db:Status-Status}\t${Version}`, "mariadb-server"}})
	if err != nil {
		// Mã 1: dpkg chưa từng biết gói này, tức là chưa cài.
		if ee := (*system.ExitError)(nil); errors.As(err, &ee) && ee.Code == 1 {
			return "", nil
		}
		return "", fmt.Errorf("hỏi dpkg bản mariadb-server: %w", err)
	}
	status, v, _ := strings.Cut(strings.TrimSpace(res.Stdout), "\t")
	if status != "installed" {
		return "", nil
	}
	return v, nil
}

// mariadbStatus là phần MariaDB của màn Cập nhật. Dòng là dòng đang chạy thật
// (suy từ bản gói); settings chỉ là dự phòng khi không đọc được gói.
func (u *Updates) mariadbStatus(ctx context.Context) (MariaDBStatus, error) {
	cfg, err := (&Stack{Settings: u.Settings}).Config(ctx)
	if err != nil {
		return MariaDBStatus{}, err
	}
	ms := MariaDBStatus{Source: cfg.DBSource, Upgrades: []MariaDBLine{}}
	if ms.Version, err = mariadbVersion(ctx, u.Sys); err != nil {
		return MariaDBStatus{}, err
	}
	if ms.Line = dbLineOfVersion(ms.Version); ms.Line == "" {
		ms.Line = cfg.DBLine
	}
	if ms.Line != "" && ms.Version != "" {
		ms.Upgrades = u.mariadbUpgrades(ctx, ms.Line)
	}
	return ms, nil
}

// mariadbReleasesURL trả danh sách dòng MariaDB (đã kiểm trên máy ảo, docs/thay-doi/M12.md).
const mariadbReleasesURL = "https://downloads.mariadb.org/rest-api/mariadb/"

// mariadbUpgrades hỏi MariaDB các dòng LTS ổn định cao hơn current, sắp tăng
// dần. Không hỏi được (không mạng, JSON lạ) thì trả rỗng: màn Cập nhật vẫn đủ,
// chỉ thiếu gợi ý.
func (u *Updates) mariadbUpgrades(ctx context.Context, current string) []MariaDBLine {
	out := []MariaDBLine{}
	res, err := u.Sys.Run(ctx, system.Cmd{Name: "curl", Args: []string{"-fsS", "--max-time", "5", mariadbReleasesURL}})
	if err != nil {
		return out
	}
	var body struct {
		MajorReleases []struct {
			ID      string `json:"release_id"`
			Status  string `json:"release_status"`
			Support string `json:"release_support_type"`
			EOL     string `json:"release_eol_date"`
		} `json:"major_releases"`
	}
	if json.Unmarshal([]byte(res.Stdout), &body) != nil {
		return out
	}
	for _, r := range body.MajorReleases {
		if r.Support != "Long Term Support" || r.Status != "Stable" || !dbLineRE.MatchString(r.ID) || cmpDBLine(r.ID, current) <= 0 {
			continue
		}
		out = append(out, MariaDBLine{Line: r.ID, EOL: r.EOL})
	}
	slices.SortFunc(out, func(a, b MariaDBLine) int { return cmpDBLine(a.Line, b.Line) })
	return out
}

// mariadbPackages trả các gói mariadb-* và galera-* đang cài. dpkg-query thoát
// mã 1 khi một mẫu không khớp gói nào (máy không có galera) mà vẫn in các gói
// khớp mẫu kia, nên mã 1 không phải lỗi.
func mariadbPackages(ctx context.Context, sys system.System) ([]string, error) {
	res, err := sys.Run(ctx, system.Cmd{Name: "dpkg-query", Args: []string{"-W", "-f", `${Package}\t${db:Status-Status}\n`, "mariadb-*", "galera-*"}})
	if err != nil {
		if ee := (*system.ExitError)(nil); !errors.As(err, &ee) || ee.Code != 1 {
			return nil, fmt.Errorf("liệt kê gói MariaDB đang cài: %w", err)
		}
	}
	var pkgs []string
	for line := range strings.Lines(res.Stdout) {
		name, status, _ := strings.Cut(strings.TrimSpace(line), "\t")
		if status == "installed" && !slices.Contains(pkgs, name) {
			pkgs = append(pkgs, name)
		}
	}
	slices.Sort(pkgs)
	return pkgs, nil
}

// ---------------------------------------------------------------- update.apply

// aptRefresh chạy `apt-get update` mỗi lần job chạy. steps.AptUpdate không
// dùng được ở đây: nó bỏ qua khi mọi gói đã cài, mà gói cần nâng thì luôn đã cài.
type aptRefresh struct{ done bool }

func (s *aptRefresh) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "apt.update:lists",
		Title: i18n.T("Làm mới danh sách gói (apt-get update)", "Refresh the package lists (apt-get update)"),
		Why: i18n.T(
			"Danh sách gói trên máy có thể đã cũ từ lần làm mới trước. Bỏ bước này thì bước nâng gói cài bản apt biết lúc đó, có thể chưa phải bản vá mới nhất.",
			"The package lists on the machine may be stale since the last refresh. Without this step the upgrade installs whatever version apt knew then, which may not be the latest fix.",
		),
		Commands: []string{aptUpdateCmd.String()},
		NoUndo: i18n.T(
			"apt-get update chỉ làm mới danh sách gói, không cài hay gỡ gì, nên không có gì để hoàn tác.",
			"apt-get update only refreshes the package lists; it installs or removes nothing, so there is nothing to undo.",
		),
	}
}

// Check: không có trạng thái nào trên máy nói "danh sách đủ mới", nên bước nhớ
// lần chạy của chính nó.
func (s *aptRefresh) Check(context.Context, system.System) (bool, error) { return s.done, nil }

func (s *aptRefresh) Apply(ctx context.Context, sys system.System) error {
	if _, err := steps.RunAptUpdate(ctx, sys, aptUpdateCmd); err != nil {
		return fmt.Errorf("làm mới danh sách gói: %w", err)
	}
	s.done = true
	return nil
}

func (s *aptRefresh) Undo(context.Context, system.System) error { return nil }

var phpPackageRE = regexp.MustCompile(`^php([0-9]+\.[0-9]+)-`)

// restartsFor là các bước khởi động lại cho dịch vụ có gói vừa nâng, theo thứ
// tự web server, PHP-FPM từng bản, MariaDB. Mỗi bước kiểm config trước (sai
// thì không khởi động lại) và chỉ khởi động lại dịch vụ đang chạy: dịch vụ
// đang tắt (web server không dùng sau khi chuyển stack) mà bật lên là tranh
// cổng 80 với web server đang dùng.
func restartsFor(pkgs []string) []engine.Step {
	var nginx, apache, mariadb bool
	var php []string
	for _, p := range pkgs {
		switch {
		case strings.HasPrefix(p, "nginx"):
			nginx = true
		case strings.HasPrefix(p, "apache2"):
			apache = true
		case strings.HasPrefix(p, "mariadb-"):
			mariadb = true
		default:
			if m := phpPackageRE.FindStringSubmatch(p); m != nil && !slices.Contains(php, m[1]) {
				php = append(php, m[1])
			}
		}
	}
	var ss []engine.Step
	if nginx {
		ss = append(ss, &steps.ServiceRestart{Unit: "nginx", Test: &system.Cmd{Name: "nginx", Args: []string{"-t"}}, OnlyIfActive: true})
	}
	if apache {
		ss = append(ss, &steps.ServiceRestart{Unit: "apache2", Test: &system.Cmd{Name: "apachectl", Args: []string{"configtest"}}, OnlyIfActive: true})
	}
	slices.SortFunc(php, cmpDBLine) // cùng dạng số.số như dòng MariaDB
	for _, v := range php {
		ss = append(ss, &steps.ServiceRestart{Unit: "php" + v + "-fpm", Test: &system.Cmd{Name: "php-fpm" + v, Args: []string{"-t"}}, OnlyIfActive: true})
	}
	if mariadb {
		ss = append(ss, &steps.ServiceRestart{Unit: "mariadb", OnlyIfActive: true})
	}
	return ss
}

// verifyAll là kiểm chứng sau khi đổi gói: phép kiểm của stack và `php-fpm<bản>
// -t` của mọi bản PHP khác bản mặc định. Không có phép kiểm của từng site: một
// site hỏng từ trước không được làm job "lỗi" (gói đã nâng không hạ về được);
// CLI và API chạy lại toàn bộ health ngay sau job, site nào đang đạt mà giờ
// không đạt thì mở sự cố (mục 6.11). Stack chưa cài thì không có gì để kiểm.
func (u *Updates) verifyAll(ctx context.Context, cfg StackConfig) ([]checks.Check, error) {
	if cfg.Web == "" || cfg.PHPVersion == "" {
		return nil, nil
	}
	_, sha := (&Stack{WPCLISHA512: u.WPCLISHA512}).wpcli()
	cs := stackChecks(cfg.Web, func() string { return cfg.PHPVersion }, sha)
	// phpInstalled, không chỉ thư mục /etc/php/<bản>/fpm: `tpan php remove`
	// giữ file cấu hình nên thư mục còn sau khi gỡ gói. Thấy trên máy ảo M12:
	// sau khi gỡ 8.4, kiểm chứng của update.apply và mariadb.upgrade có dòng
	// "[chưa rõ] Cấu hình PHP-FPM 8.4" vì php-fpm8.4 không còn.
	vers, err := phpInstalled(u.Sys)
	if err != nil {
		return nil, err
	}
	for _, v := range vers {
		if v != cfg.PHPVersion {
			cs = append(cs, checks.PHPFPMConfig(v))
		}
	}
	return cs, nil
}

// PrepareApply dựng Plan "update.apply". Chỉ đọc máy.
func (u *Updates) PrepareApply(ctx context.Context) (*Planned, error) {
	stack, _, err := u.pending(ctx)
	if err != nil {
		return nil, err
	}
	// Chỉ nâng gói đã cài; gói mới (chưa có bản cũ) được apt kéo theo như phụ thuộc.
	var pkgs []string
	for _, p := range stack {
		if p.From != "" {
			pkgs = append(pkgs, p.Name)
		}
	}
	if len(pkgs) == 0 {
		return nil, refuse("nothing_to_update", i18n.T(
			"không có gói nào của stack đang chờ cập nhật; xem lại bằng: sudo tpan update --check",
			"no stack package is waiting for an update; check again with: sudo tpan update --check"))
	}
	cfg, err := (&Stack{Settings: u.Settings}).Config(ctx)
	if err != nil {
		return nil, err
	}
	verify, err := u.verifyAll(ctx, cfg)
	if err != nil {
		return nil, err
	}
	n := strconv.Itoa(len(pkgs))
	plan := engine.Plan{
		Kind:   "update.apply",
		Target: "stack",
		Title:  i18n.T("Cập nhật "+n+" gói của stack", "Update "+n+" stack packages"),
		Steps:  append([]engine.Step{&aptRefresh{}, &steps.AptUpgrade{Packages: pkgs}}, restartsFor(pkgs)...),
		Verify: verify,
	}
	return newPlanned(u.Sys, u.Journal, plan, nil), nil
}

// ---------------------------------------------------------------- update.auto

// unattendedConf đứng sau 20auto-upgrades và 50unattended-upgrades của Ubuntu
// nên giá trị của nó thắng.
const unattendedConf = "/etc/apt/apt.conf.d/52tpan-unattended-upgrades"

// PrepareAuto dựng Plan "update.auto". Chỉ đọc máy.
func (u *Updates) PrepareAuto(ctx context.Context) (*Planned, error) {
	origins, err := u.externalOrigins()
	if err != nil {
		return nil, err
	}
	conf, err := templates.UnattendedUpgrades(origins)
	if err != nil {
		return nil, fmt.Errorf("dựng Plan bản vá tự động: %w", err)
	}
	extra := i18n.T("", "")
	if len(origins) > 0 {
		list := strings.Join(origins, ", ")
		extra = i18n.T(" Kho ngoài đang dùng ("+list+") cũng được thêm vào danh sách tự vá.", " The external repositories in use ("+list+") are added to the auto-patch list too.")
	}
	plan := engine.Plan{
		Kind:   "update.auto",
		Target: "unattended-upgrades",
		Title:  i18n.T("Bật bản vá bảo mật tự cài hằng ngày", "Turn on daily automatic security patches"),
		Steps: []engine.Step{
			&steps.AptInstall{Packages: []string{"unattended-upgrades"}},
			&steps.File{
				Path: unattendedConf, Content: conf, Perm: 0o644, Owner: system.Root,
				// apt-config đọc lại toàn bộ apt.conf.d: file sai cú pháp thì nó lỗi
				// và File trả nội dung cũ về.
				Test:  &system.Cmd{Name: "apt-config", Args: []string{"dump"}},
				Title: i18n.T("Bật tự vá trong "+unattendedConf, "Turn on auto-patching in "+unattendedConf),
				Why: i18n.T(
					"Không có file này thì máy chỉ được vá khi có người đăng nhập chạy cập nhật, trong khi lỗ hổng đã công bố bị khai thác ngay."+extra.Vi,
					"Without this file the machine is only patched when someone logs in and runs the update, while published holes are exploited right away."+extra.En),
			},
		},
		Verify: []checks.Check{checks.UnattendedUpgrades()},
	}
	return newPlanned(u.Sys, u.Journal, plan, nil), nil
}

// ---------------------------------------------------------------- mariadb.upgrade

// mariadbDumpDir là nơi giữ file xuất trước khi nâng dòng (mục 5: /var/lib/tpan/backups).
const mariadbDumpDir = "/var/lib/tpan/backups"

// PrepareMariaDB dựng Plan "mariadb.upgrade" lên dòng line. Chỉ đọc máy.
func (u *Updates) PrepareMariaDB(ctx context.Context, line string) (*Planned, error) {
	// Dòng sai thì từ chối trước khi đọc máy; repoMariaDB giữ lời từ chối
	// mariadb_line_invalid (nó kiểm dòng trước khi dùng codename).
	if !dbLineRE.MatchString(line) {
		_, err := repoMariaDB("", line)
		return nil, err
	}
	version, err := mariadbVersion(ctx, u.Sys)
	if err != nil {
		return nil, err
	}
	current := dbLineOfVersion(version)
	if current == "" {
		return nil, refuse("stack_not_installed", i18n.T(
			"máy chưa cài mariadb-server nên không có gì để nâng; cài stack trước bằng: sudo tpan stack install --web nginx|apache",
			"mariadb-server is not installed, so there is nothing to upgrade; install the stack first with: sudo tpan stack install --web nginx|apache"))
	}
	if cmpDBLine(line, current) <= 0 {
		return nil, refuse("mariadb_not_newer", i18n.T(
			fmt.Sprintf("MariaDB đang chạy dòng %s; chỉ nâng lên được dòng mới hơn (nhận được %s), không hạ xuống", current, line),
			fmt.Sprintf("MariaDB is running series %s; it can only move up to a newer series (got %s), never down", current, line)))
	}
	codename, err := osCodename(u.Sys)
	if err != nil {
		return nil, err
	}
	repo, err := repoMariaDB(codename, line)
	if err != nil {
		return nil, err
	}
	pkgs, err := mariadbPackages(ctx, u.Sys)
	if err != nil {
		return nil, err
	}
	// Tên file theo giờ lúc dựng Plan: mỗi lần nâng một file, không đè file
	// xuất của lần trước (có thể vẫn là đường lùi của lần trước).
	dump := mariadbDumpDir + "/mariadb-upgrade-" + time.Now().Format("20060102-150405") + ".sql"
	plan := engine.Plan{
		Kind:   "mariadb.upgrade",
		Target: line,
		Title: i18n.T(
			"Nâng MariaDB từ dòng "+current+" lên "+line+". Một chiều, không tự hoàn tác: đường lùi là cài lại dòng "+current+" rồi nhập lại "+dump,
			"Upgrade MariaDB from series "+current+" to "+line+". One-way, not undone automatically: the way back is to reinstall series "+current+" and import "+dump),
		// Kho mới không hoàn tác và settings ghi ngay sau khi nâng gói (oneway.go):
		// bước nào sau AptUpgrade lỗi thì máy vẫn đang chạy dòng mới, kho và
		// settings phải nói đúng như vậy.
		Steps: []engine.Step{
			&steps.MariaDBDump{Path: dump},
			&steps.ServiceStop{Unit: "mariadb"},
			&keepOnUndo{Step: repo, why: i18n.T(
				"Không gỡ kho khi hoàn tác: nếu gói đã kịp nâng thì máy cần kho này để nhận bản vá của dòng "+line+"; gỡ thì apt không còn biết gói đang chạy từ đâu ra.",
				"The repository is not removed on undo: if the packages were already upgraded the machine needs it for series "+line+" patches; removing it leaves apt not knowing where the running packages come from.")},
			&afterStep{Step: &steps.AptUpgrade{Packages: pkgs}, dep: repo},
			&settingsStep{s: u.Settings, kv: [][2]string{{SettingDBSource, string(SourceMariaDB)}, {SettingDBLine, line}},
				title: i18n.T("Ghi nguồn MariaDB mới vào settings (mariadb.org, dòng "+line+")", "Record the new MariaDB source in settings (mariadb.org, series "+line+")"),
				why: i18n.T("tpan đọc settings để biết máy đang dùng kho nào; ghi ngay sau khi nâng gói để dù bước sau lỗi, tpan vẫn nói đúng dòng đang chạy.",
					"tpan reads settings to know which repository the machine uses; it is written right after the upgrade so that even if a later step fails, tpan still reports the running series correctly.")},
			&steps.Service{Unit: "mariadb"},
			&steps.MariaDBUpgradeRun{},
		},
		// Chỉ kiểm MariaDB ở đây. Phép kiểm của từng site chạy bằng health ngay
		// sau job (CLI và API): một site hỏng từ trước không được làm job "lỗi"
		// rồi kéo hoàn tác những bước trước điểm không quay lại.
		Verify: []checks.Check{checks.Service("mariadb", checks.StageDB), checks.MariaDBSecure()},
	}
	return newPlanned(u.Sys, u.Journal, plan, nil), nil
}
