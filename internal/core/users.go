package core

// User, gói, hạn mức, tạm khoá user của M13 (mục 6.12 của docs/KE-HOACH.md).
//
// core không biết vai (docs/thay-doi/M13.md, quyết định 10): ai được gọi hàm
// nào là luật của route ở server. Ở đây chỉ có những luật đúng với mọi người
// gọi: không khoá hay xoá admin cuối cùng, không xoá user còn đồ, hạn mức gói.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

const (
	// userMinPassword khớp authMinPassword của server (đổi mật khẩu quản trị):
	// cùng một luật cho mọi mật khẩu đăng nhập panel.
	userMinPassword = 12
	// userMaxPassword chặn chuỗi khổng lồ trước khi băm argon2id.
	userMaxPassword = 1024
	// userMaxText: tên đầy đủ và lý do khoá (SuspendRequest nói tối đa 200).
	userMaxText = 200
	// mib: disk_mb của gói tính theo MiB, như dbHumanBytes in ra.
	mib = 1 << 20
)

// Chỗ nối với hai thợ khác của M13 (tạm khoá site ở suspend.go, DB rời ở
// dbs.go). Là biến để test của file này thay bằng bước giả: phần cần kiểm ở
// đây là chọn đúng site, DB nào và đánh dấu đúng gì, không phải bản thân bước.
var (
	userSiteSuspend   = (*Sites).suspendSteps
	userSiteUnsuspend = (*Sites).unsuspendSteps
	userDBLock        = dbLockSteps
	userDBUnlock      = dbUnlockSteps
	userDBTrash       = (*DBs).PrepareTrash
)

// ownerOf là chủ của site. Rỗng chỉ có ở bản ghi chưa qua store (store ghi
// rỗng thành admin), nên mọi chỗ so chủ đều đi qua đây.
func ownerOf(st site.Site) string {
	if st.Owner == "" {
		return account.AdminName
	}
	return st.Owner
}

func (a *Accounts) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// userNotFound: tên chỉ được in khi đúng dạng tên user, vì tên có thể đến
// thẳng từ đường dẫn của API (xuống dòng, mã thoát terminal).
func userNotFound(name string) *RefusedError {
	if _, err := validate.ParseUserName(name); err != nil && name != account.AdminName {
		return refuse(CodeUserNotFound, i18n.T("không có user này", "there is no such user"))
	}
	return refuse(CodeUserNotFound, i18n.T(
		"không có user "+name+"; xem danh sách bằng: sudo tpan user list",
		"there is no user "+name+"; list users with: sudo tpan user list"))
}

// lookupUser đọc user; không có thì từ chối user_not_found.
func lookupUser(ctx context.Context, as AccountStore, name string) (account.User, error) {
	u, ok, err := as.User(ctx, name)
	if err != nil {
		return account.User{}, fmt.Errorf("đọc user %q: %w", name, err)
	}
	if !ok {
		return account.User{}, userNotFound(name)
	}
	return u, nil
}

// ownerUser như lookupUser nhưng hiểu "admin" chưa có dòng (máy chưa chạy
// tpan setup: migration 1300 chỉ tạo dòng admin từ hash cũ) là admin active,
// gói default không giới hạn, như mọi thứ có từ trước M13. implicit=true khi
// đó, để người gọi bỏ qua phần cần dòng thật (Usage).
func ownerUser(ctx context.Context, as AccountStore, name string) (u account.User, implicit bool, err error) {
	u, ok, err := as.User(ctx, name)
	if err != nil {
		return account.User{}, false, fmt.Errorf("đọc user %q: %w", name, err)
	}
	if ok {
		return u, false, nil
	}
	if name == account.AdminName {
		return account.User{Name: name, Role: account.RoleAdmin, Package: account.DefaultPackage, Status: account.Active}, true, nil
	}
	return account.User{}, false, userNotFound(name)
}

// lookupPackage đọc gói theo tên đã kiểm; không có thì package_not_found.
func lookupPackage(ctx context.Context, as AccountStore, name string) (account.Package, error) {
	p, ok, err := as.Package(ctx, name)
	if err != nil {
		return account.Package{}, fmt.Errorf("đọc gói %s: %w", name, err)
	}
	if !ok {
		return account.Package{}, refuse(CodePackageNotFound, i18n.T(
			"không có gói "+name+"; xem danh sách bằng: sudo tpan package list",
			"there is no package "+name+"; list packages with: sudo tpan package list"))
	}
	return p, nil
}

// parsePackageRef kiểm tên gói người gọi đưa vào (rỗng là gói default) rồi
// đòi gói có thật.
func parsePackageRef(ctx context.Context, as AccountStore, name string) (string, error) {
	if name == "" {
		name = account.DefaultPackage
	}
	name, err := validate.ParsePackageName(name)
	if err != nil {
		return "", err
	}
	if _, err := lookupPackage(ctx, as, name); err != nil {
		return "", err
	}
	return name, nil
}

// checkUserText kiểm tên đầy đủ, lý do khoá: UTF-8, không ký tự điều khiển,
// tối đa userMaxText ký tự. Hai chuỗi này hiện trên giao diện và trong CLI.
func checkUserText(what i18n.Text, s string) error {
	if utf8.ValidString(s) && utf8.RuneCountInString(s) <= userMaxText &&
		!strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || (r >= 0x7f && r < 0xa0) }) {
		return nil
	}
	n := strconv.Itoa(userMaxText)
	return refuse("args_invalid", i18n.T(
		what.Vi+" phải là chữ UTF-8, tối đa "+n+" ký tự, không có xuống dòng hay ký tự điều khiển",
		what.En+" must be UTF-8 text of at most "+n+" characters, without newlines or control characters"))
}

// password băm pw; pw rỗng thì sinh, trả mật khẩu đã sinh để hiện một lần.
func (a *Accounts) password(pw string) (hash, generated string, err error) {
	if a.Hash == nil || a.Generate == nil {
		return "", "", errors.New("Accounts thiếu Hash hoặc Generate (server, CLI gắn store.HashPassword và store.GeneratePassword)")
	}
	if pw == "" {
		pw = a.Generate()
		generated = pw
	}
	if n := utf8.RuneCountInString(pw); n < userMinPassword || len(pw) > userMaxPassword {
		m := strconv.Itoa(userMinPassword)
		return "", "", refuse("password_too_short", i18n.T(
			"mật khẩu phải có ít nhất "+m+" ký tự (và không quá "+strconv.Itoa(userMaxPassword)+" byte)",
			"the password must be at least "+m+" characters long (and at most "+strconv.Itoa(userMaxPassword)+" bytes)"))
	}
	hash, err = a.Hash(pw)
	if err != nil {
		return "", "", fmt.Errorf("băm mật khẩu: %w", err)
	}
	return hash, generated, nil
}

// isLastAdmin: u là admin đang hoạt động và không còn admin hoạt động nào
// khác. Admin bị khoá không đăng nhập được nên không tính.
func (a *Accounts) isLastAdmin(ctx context.Context, u account.User) (bool, error) {
	if u.Role != account.RoleAdmin || u.Status != account.Active {
		return false, nil
	}
	all, err := a.Store.Users(ctx)
	if err != nil {
		return false, fmt.Errorf("đếm admin: %w", err)
	}
	for _, o := range all {
		if o.Name != u.Name && o.Role == account.RoleAdmin && o.Status == account.Active {
			return false, nil
		}
	}
	return true, nil
}

func refuseLastAdmin(name string) *RefusedError {
	return refuse(CodeLastAdmin, i18n.T(
		name+" là admin cuối cùng đang hoạt động: khoá, xoá hay hạ vai thì không còn ai quản trị panel. Thêm hoặc mở khoá một admin khác trước",
		name+" is the last active admin: locking, deleting or demoting it leaves nobody to run the panel. Add or unlock another admin first"))
}

// ---------------------------------------------------------------- user

// AddUser thêm user (kiểm tên trùng, tên user Linux có sẵn, gói có thật, vai
// hợp lệ). generated là mật khẩu tpan sinh khi in.Password rỗng, hiện một lần.
func (a *Accounts) AddUser(ctx context.Context, in UserInput) (u account.User, generated string, err error) {
	if in.Name.IsZero() {
		return u, "", refuse(validate.CodeUserNameInvalid, i18n.T("thiếu tên đăng nhập", "the user name is missing"))
	}
	name := in.Name.String()
	role := in.Role
	if role == "" {
		role = account.RoleUser
	}
	if !role.Valid() {
		return u, "", refuse("args_invalid", i18n.T("vai phải là admin hoặc user", "the role must be admin or user"))
	}
	if err := checkUserText(i18n.T("tên đầy đủ", "the full name"), in.FullName); err != nil {
		return u, "", err
	}
	if _, ok, err := a.Store.User(ctx, name); err != nil {
		return u, "", fmt.Errorf("đọc user %s: %w", name, err)
	} else if ok {
		return u, "", refuse(CodeUserExists, i18n.T("đã có user "+name, "user "+name+" already exists"))
	}
	// Mục 6.12: tên trùng một user Linux có sẵn (ubuntu, syslog...) dễ bị
	// hiểu nhầm là tài khoản trên máy, và DB rời "<tên>_..." trông như của nó.
	linux, err := linuxUserExists(ctx, a.Sys, name)
	if err != nil {
		return u, "", err
	}
	if linux {
		return u, "", refuse(validate.CodeUserNameReserved, i18n.T(
			"máy đã có user Linux tên "+name+"; chọn tên khác để không nhầm với tài khoản trên máy",
			"the machine already has a Linux user named "+name+"; choose another name so it is not mistaken for a machine account"))
	}
	pkg, err := parsePackageRef(ctx, a.Store, in.Package)
	if err != nil {
		return u, "", err
	}
	hash, generated, err := a.password(in.Password)
	if err != nil {
		return u, "", err
	}
	u = account.User{
		Name: name, FullName: in.FullName, Email: in.Email.String(), Role: role,
		Package: pkg, Status: account.Active, Created: a.now().UTC(),
	}
	if err := a.Store.CreateUser(ctx, u, hash); err != nil {
		return account.User{}, "", fmt.Errorf("thêm user %s: %w", name, err)
	}
	return u, generated, nil
}

// EditUser sửa tên đầy đủ, email, vai, gói. by là người sửa: hạ vai admin cuối
// cùng (kể cả tự hạ mình khi là admin cuối) thì từ chối last_admin. Đổi vai
// thì huỷ mọi phiên của user đó: phiên cũ không được giữ quyền của vai cũ.
func (a *Accounts) EditUser(ctx context.Context, name string, ch UserChange, by account.Identity) (account.User, error) {
	u, err := lookupUser(ctx, a.Store, name)
	if err != nil {
		return account.User{}, err
	}
	if ch.FullName != nil {
		if err := checkUserText(i18n.T("tên đầy đủ", "the full name"), *ch.FullName); err != nil {
			return account.User{}, err
		}
		u.FullName = *ch.FullName
	}
	if ch.Email != nil {
		u.Email = ch.Email.String()
	}
	if ch.Package != nil {
		if u.Package, err = parsePackageRef(ctx, a.Store, *ch.Package); err != nil {
			return account.User{}, err
		}
	}
	roleChanged := false
	if ch.Role != nil && *ch.Role != u.Role {
		if !ch.Role.Valid() {
			return account.User{}, refuse("args_invalid", i18n.T("vai phải là admin hoặc user", "the role must be admin or user"))
		}
		last, err := a.isLastAdmin(ctx, u)
		if err != nil {
			return account.User{}, err
		}
		if last {
			return account.User{}, refuseLastAdmin(u.Name)
		}
		u.Role, roleChanged = *ch.Role, true
	}
	if err := a.Store.UpdateUser(ctx, u); err != nil {
		return account.User{}, fmt.Errorf("sửa user %s: %w", u.Name, err)
	}
	if roleChanged {
		if err := a.Store.DeleteSessionsOf(ctx, u.Name); err != nil {
			return u, fmt.Errorf("đã đổi vai của %s nhưng chưa huỷ được phiên đang mở: %w", u.Name, err)
		}
	}
	return u, nil
}

// SetPassword đặt mật khẩu mới (pw rỗng thì sinh, trả về một lần) và huỷ mọi
// phiên của user. Mật khẩu tối thiểu 12 ký tự như đổi mật khẩu quản trị.
func (a *Accounts) SetPassword(ctx context.Context, name, pw string) (generated string, err error) {
	u, err := lookupUser(ctx, a.Store, name)
	if err != nil {
		return "", err
	}
	hash, generated, err := a.password(pw)
	if err != nil {
		return "", err
	}
	if err := a.Store.SetUserPassword(ctx, u.Name, hash); err != nil {
		return "", fmt.Errorf("đổi mật khẩu của %s: %w", u.Name, err)
	}
	return generated, nil
}

// owned trả site và DB rời của user name. Cần Sites và DBs: thiếu một trong
// hai thì khoá user sẽ bỏ sót đồ của họ mà không ai biết, nên báo lỗi.
func (a *Accounts) owned(ctx context.Context, name string) ([]site.Site, []account.Database, error) {
	if a.Sites == nil || a.DBs == nil {
		return nil, nil, errors.New("Accounts thiếu Sites hoặc DBs")
	}
	all, err := a.Sites.Store.Sites(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách site: %w", err)
	}
	var sites []site.Site
	for _, st := range all {
		if ownerOf(st) == name {
			sites = append(sites, st)
		}
	}
	dbs, err := a.DBs.Store.StandaloneDBs(ctx, name)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc DB rời của %s: %w", name, err)
	}
	return sites, dbs, nil
}

// markSite đọc lại site (bản ghi có thể đã đổi từ lúc dựng Plan, ví dụ vừa
// có SSL) rồi ghi trạng thái khoá mới.
func (a *Accounts) markSite(ctx context.Context, d validate.Domain, how account.Suspension, at time.Time) error {
	st, ok, err := a.Sites.Store.Site(ctx, d)
	if err == nil && !ok {
		err = errors.New("không còn trong bảng sites")
	}
	if err == nil {
		st.Suspended, st.SuspendedAt = how, at
		err = a.Sites.Store.SaveSite(ctx, st)
	}
	if err != nil {
		return fmt.Errorf("ghi trạng thái khoá của site %s: %w", d, err)
	}
	return nil
}

// markDB như markSite cho DB rời.
func (a *Accounts) markDB(ctx context.Context, name string, how account.Suspension, at time.Time) error {
	d, ok, err := a.DBs.Store.StandaloneDB(ctx, name)
	if err == nil && !ok {
		err = errors.New("không còn trong bảng databases")
	}
	if err == nil {
		d.Suspended, d.SuspendedAt = how, at
		err = a.DBs.Store.SaveStandaloneDB(ctx, d)
	}
	if err != nil {
		return fmt.Errorf("ghi trạng thái khoá của DB %s: %w", name, err)
	}
	return nil
}

// PrepareSuspendUser dựng Plan khoá user: tạm khoá mọi site (Sites.suspendSteps)
// và DB rời (dbLockSteps) đang chưa khoá của user. Sau job: đánh dấu từng site,
// DB là SuspendByUser, user thành Suspended, huỷ mọi phiên. Từ chối khoá chính
// mình (by.Name) và admin cuối cùng (last_admin).
func (a *Accounts) PrepareSuspendUser(ctx context.Context, name, reason string, by account.Identity) (*Planned, error) {
	u, err := lookupUser(ctx, a.Store, name)
	if err != nil {
		return nil, err
	}
	if by.Name != "" && by.Name == u.Name {
		return nil, refuse(CodeLastAdmin, i18n.T(
			"không tự khoá chính mình được: khoá xong là mất phiên đang dùng và không đăng nhập lại được",
			"you cannot lock yourself: you would lose this session and could not log in again"))
	}
	if u.Status == account.Suspended {
		return nil, refuse(CodeUserSuspended, i18n.T("user "+u.Name+" đang bị khoá rồi", "user "+u.Name+" is already locked"))
	}
	if last, err := a.isLastAdmin(ctx, u); err != nil {
		return nil, err
	} else if last {
		return nil, refuseLastAdmin(u.Name)
	}
	if err := checkUserText(i18n.T("lý do khoá", "the reason"), reason); err != nil {
		return nil, err
	}
	sites, dbs, err := a.owned(ctx, u.Name)
	if err != nil {
		return nil, err
	}
	var ss []engine.Step
	var verify []checks.Check
	var locked []site.Site
	for _, st := range sites {
		// Đã khoá riêng thì giữ nguyên nguồn "self": mở khoá user sau này
		// không được mở nó.
		if st.Status != site.Active || st.Suspended != account.NotSuspended {
			continue
		}
		steps, err := userSiteSuspend(a.Sites, ctx, st)
		if err != nil {
			return nil, fmt.Errorf("dựng bước khoá site %s: %w", st.Domain, err)
		}
		ss = append(ss, steps...)
		verify = append(verify, suspendedChecks(st)...)
		locked = append(locked, st)
	}
	var lockedDBs []string
	for _, d := range dbs {
		if d.Status != account.DBActive || d.Suspended != account.NotSuspended {
			continue
		}
		ss = append(ss, userDBLock(d.Name)...)
		lockedDBs = append(lockedDBs, d.Name)
	}
	plan := engine.Plan{
		Kind: "user.suspend", Target: u.Name, Steps: ss, Verify: verify,
		Title: i18n.T("Khoá user "+u.Name+" cùng mọi site và DB rời của họ", "Lock user "+u.Name+" and all their sites and standalone databases"),
	}
	return newPlanned(a.Sys, a.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		at := a.now().UTC()
		var errs []error
		for _, st := range locked {
			errs = append(errs, a.markSite(ctx, st.Domain, account.SuspendByUser, at))
		}
		for _, n := range lockedDBs {
			errs = append(errs, a.markDB(ctx, n, account.SuspendByUser, at))
		}
		// User và phiên vẫn được ghi khi một dòng ở trên lỗi: chặn đăng nhập
		// quan trọng hơn, và chạy lại thì các site đã khoá được bỏ qua.
		cur, err := lookupUser(ctx, a.Store, u.Name)
		if err == nil {
			cur.Status, cur.SuspendedAt, cur.SuspendReason = account.Suspended, at, reason
			err = a.Store.UpdateUser(ctx, cur)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("ghi user %s là đã khoá: %w", u.Name, err))
		}
		if err := a.Store.DeleteSessionsOf(ctx, u.Name); err != nil {
			errs = append(errs, fmt.Errorf("huỷ phiên của %s: %w", u.Name, err))
		}
		return errors.Join(errs...)
	}), nil
}

// PrepareUnsuspendUser mở khoá user và mở lại đúng những site, DB rời có
// Suspended == SuspendByUser; thứ bị khoá riêng (SuspendSelf) giữ khoá.
// Chạy được cả khi user đã active: dọn nốt thứ còn khoá vì user sau một lần
// mở khoá hỏng giữa chừng.
func (a *Accounts) PrepareUnsuspendUser(ctx context.Context, name string) (*Planned, error) {
	u, err := lookupUser(ctx, a.Store, name)
	if err != nil {
		return nil, err
	}
	sites, dbs, err := a.owned(ctx, u.Name)
	if err != nil {
		return nil, err
	}
	var ss []engine.Step
	var verify []checks.Check
	var opened []site.Site
	var cfg StackConfig
	for _, st := range sites {
		if st.Status != site.Active || st.Suspended != account.SuspendByUser {
			continue
		}
		if cfg.Web == "" {
			if cfg, err = a.Sites.stack(ctx); err != nil {
				return nil, err
			}
		}
		steps, err := userSiteUnsuspend(a.Sites, ctx, st)
		if err != nil {
			return nil, fmt.Errorf("dựng bước mở khoá site %s: %w", st.Domain, err)
		}
		ss = append(ss, steps...)
		// Kiểm chứng như lúc tạo site: trang trả 200 qua PHP và DB. Không kiểm
		// cô lập với site khác ở đây (site khác có thể đang khoá, pool tắt).
		// DB của site đang khoá riêng thì WordPress báo lỗi kết nối DB: kiểm
		// chứng đó chắc chắn Fail và engine hoàn tác cả lần mở khoá user, nên bỏ.
		if st.DBSuspended == account.NotSuspended {
			verify = append(verify, siteChecks(cfg.Web, sitePHP(st, cfg), st.Layout(), nil, st.SSL)...)
		}
		opened = append(opened, st)
	}
	var openedDBs []string
	for _, d := range dbs {
		if d.Status != account.DBActive || d.Suspended != account.SuspendByUser {
			continue
		}
		ss = append(ss, userDBUnlock(d.Name)...)
		openedDBs = append(openedDBs, d.Name)
	}
	plan := engine.Plan{
		Kind: "user.unsuspend", Target: u.Name, Steps: ss, Verify: verify,
		Title: i18n.T("Mở khoá user "+u.Name+" và những gì bị khoá cùng họ", "Unlock user "+u.Name+" and what was locked with them"),
	}
	return newPlanned(a.Sys, a.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		var errs []error
		for _, st := range opened {
			errs = append(errs, a.markSite(ctx, st.Domain, account.NotSuspended, time.Time{}))
		}
		for _, n := range openedDBs {
			errs = append(errs, a.markDB(ctx, n, account.NotSuspended, time.Time{}))
		}
		cur, err := lookupUser(ctx, a.Store, u.Name)
		if err == nil {
			cur.Status, cur.SuspendedAt, cur.SuspendReason = account.Active, time.Time{}, ""
			err = a.Store.UpdateUser(ctx, cur)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("ghi user %s là đã mở khoá: %w", u.Name, err))
		}
		return errors.Join(errs...)
	}), nil
}

// PrepareTrashAll đưa mọi site active và DB rời active của user vào thùng rác
// (nút trước khi xoá user). Từ chối khi có site hay DB đang tạm khoá. Một job
// cho cả lô: một bước lỗi thì engine trả mọi thứ về như cũ.
func (a *Accounts) PrepareTrashAll(ctx context.Context, name string) (*Planned, error) {
	u, err := lookupUser(ctx, a.Store, name)
	if err != nil {
		return nil, err
	}
	sites, dbs, err := a.owned(ctx, u.Name)
	if err != nil {
		return nil, err
	}
	// Kiểm hết trước khi dựng gì: khôi phục từ thùng rác mở khoá lén
	// (docs/thay-doi/M13.md, quyết định 7).
	for _, st := range sites {
		if st.Status == site.Active && (st.Suspended != account.NotSuspended || st.DBSuspended != account.NotSuspended) {
			return nil, refuseSuspendedTrash(st)
		}
	}
	for _, d := range dbs {
		if d.Status == account.DBActive && d.Suspended != account.NotSuspended {
			return nil, refuse(CodeDBSuspended, i18n.T(
				"DB "+d.Name+" đang tạm khoá; mở khoá trước rồi mới đưa vào thùng rác (khôi phục từ thùng rác sẽ mở khoá nó)",
				"database "+d.Name+" is locked; unlock it first, then move it to the trash (restoring from the trash would unlock it)"))
		}
	}
	plan := engine.Plan{
		Kind: "user.trash_all", Target: u.Name,
		Title: i18n.T("Đưa mọi site và DB rời của "+u.Name+" vào thùng rác", "Move every site and standalone database of "+u.Name+" to the trash"),
	}
	var afters []func(context.Context, engine.Report) error
	for _, st := range sites {
		if st.Status != site.Active {
			continue
		}
		rm, err := a.Sites.PrepareRemove(ctx, st.Domain)
		if err != nil {
			return nil, err
		}
		plan.Steps = append(plan.Steps, rm.Plan.Steps...)
		plan.Verify = append(plan.Verify, rm.Plan.Verify...)
		afters = append(afters, func(ctx context.Context, _ engine.Report) error { return a.Sites.markTrashed(ctx, rm.Site) })
	}
	for _, d := range dbs {
		if d.Status != account.DBActive {
			continue
		}
		p, err := userDBTrash(a.DBs, ctx, d.Name)
		if err != nil {
			return nil, err
		}
		plan.Steps = append(plan.Steps, p.Plan.Steps...)
		plan.Verify = append(plan.Verify, p.Plan.Verify...)
		if p.after != nil {
			afters = append(afters, p.after)
		}
	}
	if len(afters) == 0 && len(plan.Steps) == 0 {
		return nil, refuse("nothing_to_do", i18n.T(
			"user "+u.Name+" không có site hay DB rời nào đang chạy",
			"user "+u.Name+" has no running site or standalone database"))
	}
	return newPlanned(a.Sys, a.Journal, plan, func(ctx context.Context, rep engine.Report) error {
		var errs []error
		for _, f := range afters {
			errs = append(errs, f(ctx, rep))
		}
		return errors.Join(errs...)
	}), nil
}

// refuseSuspendedTrash là lời từ chối đưa một site đang khoá (site hoặc DB
// của nó) vào thùng rác (quyết định 7 của docs/thay-doi/M13.md).
func refuseSuspendedTrash(st site.Site) *RefusedError {
	d := st.Domain.String()
	if st.Suspended != account.NotSuspended {
		return refuse(CodeSiteSuspended, i18n.T(
			"site "+d+" đang tạm khoá; mở khoá trước (sudo tpan site unsuspend "+d+") rồi mới đưa vào thùng rác, vì khôi phục từ thùng rác sẽ mở khoá nó",
			"site "+d+" is suspended; unsuspend it first (sudo tpan site unsuspend "+d+") before moving it to the trash, because restoring from the trash would unlock it"))
	}
	return refuse(CodeDBSuspended, i18n.T(
		"DB của site "+d+" đang tạm khoá; mở khoá trước rồi mới đưa site vào thùng rác, vì khôi phục từ thùng rác sẽ mở khoá user DB",
		"the database of site "+d+" is locked; unlock it first before moving the site to the trash, because restoring from the trash would unlock the database user"))
}

// DeleteUser xoá user. Từ chối khi còn site (kể cả thùng rác), DB rời, hoặc
// job đang chạy (user_not_empty); khi là admin cuối cùng hoặc chính by (last_admin).
// User "admin" cũng không xoá được: `tpan admin passwd` (đường cứu khi quên
// mật khẩu) đặt lại đúng user này, và dòng cũ không có chủ được hiểu là của nó.
func (a *Accounts) DeleteUser(ctx context.Context, name string, by account.Identity) error {
	u, err := lookupUser(ctx, a.Store, name)
	if err != nil {
		return err
	}
	if by.Name != "" && by.Name == u.Name {
		return refuse(CodeLastAdmin, i18n.T("không tự xoá chính mình được", "you cannot delete yourself"))
	}
	if u.Name == account.AdminName {
		return refuse(CodeLastAdmin, i18n.T(
			"user admin là tài khoản quản trị gốc (sudo tpan admin passwd đặt lại mật khẩu của nó); không xoá được",
			"user admin is the built-in admin account (sudo tpan admin passwd resets its password); it cannot be deleted"))
	}
	if last, err := a.isLastAdmin(ctx, u); err != nil {
		return err
	} else if last {
		return refuseLastAdmin(u.Name)
	}
	use, err := a.Store.Usage(ctx, u.Name)
	if err != nil {
		return fmt.Errorf("đếm site, DB của %s: %w", u.Name, err)
	}
	if use.Sites > 0 || use.DBs > 0 {
		return refuse(CodeUserNotEmpty, i18n.T(
			fmt.Sprintf("user %s còn %d site và %d DB rời (kể cả trong thùng rác); đưa chúng vào thùng rác rồi xoá hẳn trước", u.Name, use.Sites, use.DBs),
			fmt.Sprintf("user %s still has %d sites and %d standalone databases (the trash included); move them to the trash and purge them first", u.Name, use.Sites, use.DBs)))
	}
	running, err := a.Store.RunningJobsOf(ctx, u.Name)
	if err != nil {
		return fmt.Errorf("đếm job đang chạy của %s: %w", u.Name, err)
	}
	if running > 0 {
		return refuse(CodeUserNotEmpty, i18n.T(
			"user "+u.Name+" đang có job chạy; đợi job xong rồi xoá",
			"user "+u.Name+" has a job running; wait for it to finish, then delete"))
	}
	if err := a.Store.DeleteUser(ctx, u.Name); err != nil {
		return fmt.Errorf("xoá user %s: %w", u.Name, err)
	}
	return nil
}

// ---------------------------------------------------------------- xem

// overQuota: đang vượt (lớn hơn, không phải bằng) ít nhất một giới hạn.
func overQuota(p account.Package, use account.Usage) bool {
	return (p.MaxSites > 0 && use.Sites > p.MaxSites) ||
		(p.MaxDBs > 0 && use.DBs > p.MaxDBs) ||
		diskOver(p, use)
}

// diskOver: lần đo gần nhất vượt disk_mb. Chưa đo (-1) thì không tính.
func diskOver(p account.Package, use account.Usage) bool {
	return p.DiskMB > 0 && use.DiskBytes > int64(p.DiskMB)*mib
}

// Summary là một user kèm gói và phần đã dùng.
func (a *Accounts) Summary(ctx context.Context, name string) (UserSummary, error) {
	u, err := lookupUser(ctx, a.Store, name)
	if err != nil {
		return UserSummary{}, err
	}
	return a.summary(ctx, u)
}

func (a *Accounts) summary(ctx context.Context, u account.User) (UserSummary, error) {
	p, ok, err := a.Store.Package(ctx, u.Package)
	if err != nil {
		return UserSummary{}, fmt.Errorf("đọc gói %s của %s: %w", u.Package, u.Name, err)
	}
	if !ok {
		return UserSummary{}, fmt.Errorf("user %s dùng gói %s không có trong bảng packages", u.Name, u.Package)
	}
	use, err := a.Store.Usage(ctx, u.Name)
	if err != nil {
		return UserSummary{}, fmt.Errorf("đọc phần đã dùng của %s: %w", u.Name, err)
	}
	return UserSummary{User: u, Limits: p, Usage: use, OverQuota: overQuota(p, use)}, nil
}

// Summaries là Summary của mọi user, sắp theo tên.
func (a *Accounts) Summaries(ctx context.Context) ([]UserSummary, error) {
	all, err := a.Store.Users(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách user: %w", err)
	}
	out := make([]UserSummary, 0, len(all))
	for _, u := range all {
		s, err := a.summary(ctx, u)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// ---------------------------------------------------------------- gói

// checkPackageLimits: mọi giới hạn ≥ 0 (0 là không giới hạn). DB cũng có
// CHECK nhưng lời từ chối ở đây đọc được.
func checkPackageLimits(p account.Package) error {
	if p.MaxSites < 0 || p.MaxDBs < 0 || p.DiskMB < 0 || p.MaxBackups < 0 {
		return refuse("args_invalid", i18n.T(
			"giới hạn của gói phải là số nguyên ≥ 0 (0 là không giới hạn)",
			"package limits must be whole numbers ≥ 0 (0 means unlimited)"))
	}
	return nil
}

// AddPackage thêm gói mới (package_exists nếu trùng tên).
func (a *Accounts) AddPackage(ctx context.Context, p account.Package) error {
	name, err := validate.ParsePackageName(p.Name)
	if err != nil {
		return err
	}
	if err := checkPackageLimits(p); err != nil {
		return err
	}
	if _, ok, err := a.Store.Package(ctx, name); err != nil {
		return fmt.Errorf("đọc gói %s: %w", name, err)
	} else if ok {
		return refuse(CodePackageExists, i18n.T("đã có gói "+name, "package "+name+" already exists"))
	}
	p.Name, p.Created = name, a.now().UTC()
	if err := a.Store.SavePackage(ctx, p); err != nil {
		return fmt.Errorf("thêm gói %s: %w", name, err)
	}
	return nil
}

// EditPackage sửa giới hạn của gói đã có. Hạ giới hạn xuống dưới phần user
// đang dùng vẫn được: chỉ chặn tạo mới, không xoá gì.
func (a *Accounts) EditPackage(ctx context.Context, p account.Package) error {
	name, err := validate.ParsePackageName(p.Name)
	if err != nil {
		return err
	}
	if err := checkPackageLimits(p); err != nil {
		return err
	}
	old, err := lookupPackage(ctx, a.Store, name)
	if err != nil {
		return err
	}
	p.Name, p.Created = name, old.Created
	if err := a.Store.SavePackage(ctx, p); err != nil {
		return fmt.Errorf("sửa gói %s: %w", name, err)
	}
	return nil
}

// DeletePackage xoá gói; từ chối với "default" và gói còn user dùng.
func (a *Accounts) DeletePackage(ctx context.Context, name string) error {
	name, err := validate.ParsePackageName(name)
	if err != nil {
		return err
	}
	if name == account.DefaultPackage {
		return refuse(CodePackageInUse, i18n.T(
			"gói default có sẵn và là gói của user không chọn gói; không xoá được",
			"the default package is built in and used by users without a package; it cannot be deleted"))
	}
	if _, err := lookupPackage(ctx, a.Store, name); err != nil {
		return err
	}
	n, err := a.Store.PackageUsers(ctx, name)
	if err != nil {
		return fmt.Errorf("đếm user của gói %s: %w", name, err)
	}
	if n > 0 {
		return refuse(CodePackageInUse, i18n.T(
			fmt.Sprintf("gói %s còn %d user dùng; chuyển họ sang gói khác trước", name, n),
			fmt.Sprintf("package %s is still used by %d users; move them to another package first", name, n)))
	}
	if err := a.Store.DeletePackage(ctx, name); err != nil {
		return fmt.Errorf("xoá gói %s: %w", name, err)
	}
	return nil
}

// Packages là mọi gói kèm số user.
func (a *Accounts) Packages(ctx context.Context) ([]PackageSummary, error) {
	all, err := a.Store.Packages(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách gói: %w", err)
	}
	out := make([]PackageSummary, 0, len(all))
	for _, p := range all {
		n, err := a.Store.PackageUsers(ctx, p.Name)
		if err != nil {
			return nil, fmt.Errorf("đếm user của gói %s: %w", p.Name, err)
		}
		out = append(out, PackageSummary{Package: p, Users: n})
	}
	return out, nil
}

// ---------------------------------------------------------------- dung lượng

// duBytes đo một thư mục bằng `du -sb` (byte thật, không làm tròn khối).
// Thư mục không có thì 0: site lệch không làm hỏng cả lần đo của chủ nó.
func duBytes(ctx context.Context, sys system.System, dir string) (int64, error) {
	ok, err := pathExists(sys, dir)
	if err != nil || !ok {
		return 0, err
	}
	res, err := sys.Run(ctx, system.Cmd{Name: "du", Args: []string{"-sb", "--one-file-system", "--", dir}})
	if err != nil {
		return 0, fmt.Errorf("đo %s: %w", dir, err)
	}
	f := strings.Fields(res.Stdout)
	if len(f) == 0 {
		return 0, fmt.Errorf("đo %s: kết quả lạ %q", dir, res.Stdout)
	}
	n, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("đo %s: kết quả lạ %q", dir, res.Stdout)
	}
	return n, nil
}

// MeasureDisk đo dung lượng của mọi user (du thư mục site + kích thước DB của
// site và DB rời) rồi ghi SetDiskUsage. Chạy trong phép kiểm nặng mỗi giờ.
//
// User có một phần đo lỗi thì giữ lần đo cũ, không ghi số thiếu: số nhỏ hơn
// thật có thể gỡ chặn disk_mb sai. Các user khác vẫn được ghi; lỗi gom lại trả về.
func (a *Accounts) MeasureDisk(ctx context.Context) error {
	users, err := a.Store.Users(ctx)
	if err != nil {
		return fmt.Errorf("đo dung lượng: đọc user: %w", err)
	}
	if a.Sites == nil {
		return errors.New("đo dung lượng: Accounts thiếu Sites")
	}
	sites, err := a.Sites.Store.Sites(ctx)
	if err != nil {
		return fmt.Errorf("đo dung lượng: đọc site: %w", err)
	}
	var dbs []account.Database
	if a.DBs != nil {
		if dbs, err = a.DBs.Store.StandaloneDBs(ctx, ""); err != nil {
			return fmt.Errorf("đo dung lượng: đọc DB rời: %w", err)
		}
	}

	total := map[string]int64{}
	failed := map[string]bool{}
	var errs []error
	dbOwner := map[string]string{} // tên DB → chủ
	for _, st := range sites {
		owner, l := ownerOf(st), st.Layout()
		dir := l.Dir
		if st.Status == site.Trashed {
			dir = site.TrashDir(l)
		}
		n, err := duBytes(ctx, a.Sys, dir)
		if err != nil {
			failed[owner] = true
			errs = append(errs, fmt.Errorf("site %s: %w", st.Domain, err))
		}
		total[owner] += n
		dbOwner[l.DB] = owner
	}
	for _, d := range dbs {
		// DB rời trong thùng rác đã DROP; bản .sql của nó chưa tính (nhỏ, và
		// tự xoá khi hết hạn thùng rác).
		if d.Status == account.DBActive {
			dbOwner[d.Name] = d.Owner
		}
	}
	var names []string
	for n := range dbOwner {
		if databaseNameRe.MatchString(n) {
			names = append(names, n)
		}
	}
	if len(names) > 0 {
		res, err := a.Sys.Run(ctx, system.Cmd{Name: "mariadb", Args: []string{"-N", "-B"}, Stdin: []byte(databaseSizeSQL(names))})
		if err != nil {
			errs = append(errs, fmt.Errorf("đo kích thước DB: %w", err))
			for _, o := range dbOwner {
				failed[o] = true
			}
		} else {
			// DB không có trong kết quả (đã mất) thì không cộng gì.
			for n, s := range parseDatabaseStats(res.Stdout) {
				if o, ok := dbOwner[n]; ok {
					total[o] += s.size
				}
			}
		}
	}

	at := a.now().UTC()
	for _, u := range users {
		if failed[u.Name] {
			continue
		}
		if err := a.Store.SetDiskUsage(ctx, u.Name, total[u.Name], at); err != nil {
			errs = append(errs, fmt.Errorf("ghi dung lượng của %s: %w", u.Name, err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("đo dung lượng: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------- hạn mức

// checkQuota kiểm hạn mức gói của owner trước khi tạo mới một thứ loại k
// (cho QuotaBackups thì d là site sẽ backup). Vượt thì *RefusedError
// (CodeQuota*); dung lượng đo gần nhất đã vượt disk_mb cũng chặn mọi loại tạo
// mới (CodeQuotaDisk). as nil thì luôn qua. Dùng ở Sites.PrepareAdd,
// DBs.PrepareAdd, Backups.PrepareCreate, Sites.PrepareApply.
//
// AccountStore không đọc được bảng backups, nên checkQuota(QuotaBackups) chỉ
// kiểm dung lượng; Backups.PrepareCreate tự đếm bản backup rồi gọi quotaCheck.
func checkQuota(ctx context.Context, as AccountStore, owner string, k QuotaKind, d validate.Domain) error {
	return quotaCheck(ctx, as, owner, k, d, 0)
}

// quotaCheck là checkQuota cộng thêm extra: với site và DB rời là số thứ cùng
// loại đã nhận trong cùng một lô (apply) mà Usage chưa thấy; với backup là số
// bản backup đầy đủ site d đang có.
func quotaCheck(ctx context.Context, as AccountStore, owner string, k QuotaKind, d validate.Domain, extra int) error {
	if as == nil {
		return nil
	}
	if owner == "" {
		owner = account.AdminName
	}
	u, implicit, err := ownerUser(ctx, as, owner)
	if err != nil {
		return err
	}
	if implicit {
		return nil // admin chưa có dòng: gói default, không giới hạn
	}
	p, ok, err := as.Package(ctx, u.Package)
	if err != nil {
		return fmt.Errorf("đọc gói %s của %s: %w", u.Package, owner, err)
	}
	if !ok {
		return fmt.Errorf("user %s dùng gói %s không có trong bảng packages", owner, u.Package)
	}
	use, err := as.Usage(ctx, owner)
	if err != nil {
		return fmt.Errorf("đọc phần đã dùng của %s: %w", owner, err)
	}
	who := "user " + owner + " (gói " + p.Name + ")"
	whoEn := "user " + owner + " (package " + p.Name + ")"
	if diskOver(p, use) {
		used := dbHumanBytes(uint64(use.DiskBytes))
		at := use.DiskMeasured.UTC().Format("2006-01-02 15:04 UTC")
		lim := strconv.Itoa(p.DiskMB)
		return refuse(CodeQuotaDisk, i18n.T(
			who+" đang dùng "+used+" (đo lúc "+at+"), quá giới hạn "+lim+" MiB của gói; chưa tạo thêm được gì cho tới khi dọn bớt (số đo cập nhật mỗi giờ) hoặc đổi sang gói lớn hơn",
			whoEn+" uses "+used+" (measured "+at+"), over the package limit of "+lim+" MiB; nothing new can be created until some space is freed (measured every hour) or the user moves to a bigger package"))
	}
	limit := func(code string, have, most int, vi, en string) error {
		if most == 0 || have < most {
			return nil
		}
		return refuse(code, i18n.T(
			fmt.Sprintf("%s đã dùng %d trên %d %s", who, have, most, vi),
			fmt.Sprintf("%s already uses %d of %d %s", whoEn, have, most, en)))
	}
	switch k {
	case QuotaSites:
		return limit(CodeQuotaSites, use.Sites+extra, p.MaxSites,
			"site (site trong thùng rác vẫn tính, và cả site mới trong cùng danh sách); xoá hẳn bớt site hoặc đổi sang gói lớn hơn",
			"sites (sites in the trash count, and so do new sites in the same list); purge some sites or move to a bigger package")
	case QuotaDBs:
		return limit(CodeQuotaDBs, use.DBs+extra, p.MaxDBs,
			"DB rời (DB trong thùng rác vẫn tính); xoá hẳn bớt DB hoặc đổi sang gói lớn hơn",
			"standalone databases (databases in the trash count); purge some or move to a bigger package")
	case QuotaBackups:
		return limit(CodeQuotaBackups, extra, p.MaxBackups,
			"bản backup đầy đủ cho site "+d.String()+"; bản cũ nhất được dọn ở lần backup theo lịch, hoặc đổi sang gói lớn hơn",
			"full backups for site "+d.String()+"; the oldest are pruned by the scheduled backup, or move to a bigger package")
	}
	return fmt.Errorf("hạn mức: loại %q không biết", k)
}
