package core

// Test của users.go (M13). Fake có tiền tố usr để không trùng tên với test của
// hai thợ cùng gói (suspend, DB rời). Không dùng *store.Store làm AccountStore:
// phần users của store được viết song song.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// ---------------------------------------------------------------- giả lập

// usrStore là AccountStore trong bộ nhớ. Usage đếm site từ memSites và DB rời
// từ usrDBs như store thật (mọi trạng thái).
type usrStore struct {
	mu       sync.Mutex
	users    map[string]account.User
	hashes   map[string]string
	pkgs     map[string]account.Package
	disk     map[string]int64
	measured map[string]time.Time
	killed   []string // DeleteSessionsOf, SetUserPassword đã huỷ phiên của ai
	running  map[string]int
	sites    *memSites
	dbs      *usrDBs
}

func newUsrStore(sites *memSites, dbs *usrDBs) *usrStore {
	return &usrStore{
		users: map[string]account.User{}, hashes: map[string]string{},
		pkgs: map[string]account.Package{account.DefaultPackage: {Name: account.DefaultPackage}},
		disk: map[string]int64{}, measured: map[string]time.Time{},
		running: map[string]int{}, sites: sites, dbs: dbs,
	}
}

func (s *usrStore) put(u account.User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u.Package == "" {
		u.Package = account.DefaultPackage
	}
	if u.Status == "" {
		u.Status = account.Active
	}
	s.users[u.Name] = u
}

func (s *usrStore) get(name string) account.User {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.users[name]
}

func (s *usrStore) User(_ context.Context, name string) (account.User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[name]
	return u, ok, nil
}

func (s *usrStore) Users(context.Context) ([]account.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []account.User
	for _, u := range s.users {
		out = append(out, u)
	}
	slices.SortFunc(out, func(a, b account.User) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (s *usrStore) CreateUser(_ context.Context, u account.User, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[u.Name], s.hashes[u.Name] = u, hash
	return nil
}

func (s *usrStore) UpdateUser(_ context.Context, u account.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[u.Name]; !ok {
		return errors.New("không có")
	}
	s.users[u.Name] = u
	return nil
}

func (s *usrStore) SetUserPassword(_ context.Context, name, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hashes[name] = hash
	s.killed = append(s.killed, name)
	return nil
}

func (s *usrStore) DeleteSessionsOf(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.killed = append(s.killed, name)
	return nil
}

func (s *usrStore) DeleteUser(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, name)
	return nil
}

func (s *usrStore) RunningJobsOf(_ context.Context, name string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[name], nil
}

func (s *usrStore) Package(_ context.Context, name string) (account.Package, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pkgs[name]
	return p, ok, nil
}

func (s *usrStore) Packages(context.Context) ([]account.Package, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []account.Package
	for _, p := range s.pkgs {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b account.Package) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (s *usrStore) SavePackage(_ context.Context, p account.Package) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pkgs[p.Name] = p
	return nil
}

func (s *usrStore) DeletePackage(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pkgs, name)
	return nil
}

func (s *usrStore) PackageUsers(_ context.Context, name string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, u := range s.users {
		if u.Package == name {
			n++
		}
	}
	return n, nil
}

func (s *usrStore) Usage(ctx context.Context, name string) (account.Usage, error) {
	use := account.Usage{DiskBytes: -1}
	if s.sites != nil {
		all, _ := s.sites.Sites(ctx)
		for _, st := range all {
			if ownerOf(st) == name {
				use.Sites++
			}
		}
	}
	if s.dbs != nil {
		dbs, _ := s.dbs.StandaloneDBs(ctx, name)
		use.DBs = len(dbs)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n, ok := s.disk[name]; ok {
		use.DiskBytes, use.DiskMeasured = n, s.measured[name]
	}
	return use, nil
}

func (s *usrStore) SetDiskUsage(_ context.Context, name string, n int64, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disk[name], s.measured[name] = n, at
	return nil
}

// usrDBs là DBStore trong bộ nhớ.
type usrDBs struct {
	mu  sync.Mutex
	dbs map[string]account.Database
}

func newUsrDBs() *usrDBs { return &usrDBs{dbs: map[string]account.Database{}} }

func (m *usrDBs) StandaloneDB(_ context.Context, name string) (account.Database, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.dbs[name]
	return d, ok, nil
}

func (m *usrDBs) StandaloneDBs(_ context.Context, owner string) ([]account.Database, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []account.Database
	for _, d := range m.dbs {
		if owner == "" || d.Owner == owner {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b account.Database) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (m *usrDBs) SaveStandaloneDB(_ context.Context, d account.Database) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dbs[d.Name] = d
	return nil
}

func (m *usrDBs) DeleteStandaloneDB(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.dbs, name)
	return nil
}

// usrStep là bước giả thay cho bước của thợ khác: chỉ có ID để kiểm Plan.
type usrStep struct {
	id   string
	done bool
}

func (s *usrStep) Info() engine.StepInfo {
	return engine.StepInfo{ID: s.id, Title: i18n.T(s.id, s.id), Why: i18n.T(s.id, s.id)}
}
func (s *usrStep) Check(context.Context, system.System) (bool, error) { return s.done, nil }
func (s *usrStep) Apply(context.Context, system.System) error         { s.done = true; return nil }
func (s *usrStep) Undo(context.Context, system.System) error          { s.done = false; return nil }

// usrHooks thay bốn chỗ nối với thợ khác bằng bước giả, trả lại khi test xong.
func usrHooks(t *testing.T) {
	t.Helper()
	ss, su, dl, du, dt := userSiteSuspend, userSiteUnsuspend, userDBLock, userDBUnlock, userDBTrash
	t.Cleanup(func() {
		userSiteSuspend, userSiteUnsuspend, userDBLock, userDBUnlock, userDBTrash = ss, su, dl, du, dt
	})
	userSiteSuspend = func(_ *Sites, _ context.Context, st site.Site) ([]engine.Step, error) {
		return []engine.Step{&usrStep{id: "suspend:" + st.Domain.String()}}, nil
	}
	userSiteUnsuspend = func(_ *Sites, _ context.Context, st site.Site) ([]engine.Step, error) {
		return []engine.Step{&usrStep{id: "unsuspend:" + st.Domain.String()}}, nil
	}
	userDBLock = func(u string) []engine.Step { return []engine.Step{&usrStep{id: "dblock:" + u}} }
	userDBUnlock = func(u string) []engine.Step { return []engine.Step{&usrStep{id: "dbunlock:" + u}} }
}

type usrEnv struct {
	*siteEnv
	a   *Accounts
	as  *usrStore
	dbs *usrDBs
	now time.Time
}

// newUsrEnv: stack nginx đã cài, user admin (vai admin) và bob (vai user).
// getent trả "không có" cho mọi tên trừ "ubuntu".
func newUsrEnv(t *testing.T) *usrEnv {
	t.Helper()
	se := newSiteEnv(t, Nginx)
	se.m.Handle([]string{"getent", "passwd", "ubuntu"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "ubuntu:x:1000:1000::/home/ubuntu:/bin/bash\n"}
	})
	dbs := newUsrDBs()
	as := newUsrStore(se.sites, dbs)
	as.put(account.User{Name: account.AdminName, Role: account.RoleAdmin})
	as.put(account.User{Name: "bob", Role: account.RoleUser})
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	se.s.Accounts = as
	se.s.Now = func() time.Time { return now }
	a := &Accounts{
		Sys: se.m, Store: as, Sites: se.s, Journal: se.st,
		DBs:      &DBs{Sys: se.m, Store: dbs, Sites: se.sites, Accounts: as, Journal: se.st},
		Now:      func() time.Time { return now },
		Hash:     func(pw string) (string, error) { return "hash:" + pw, nil },
		Generate: func() string { return "sinh-ra-mat-khau-26-ky-tu" },
	}
	return &usrEnv{siteEnv: se, a: a, as: as, dbs: dbs, now: now}
}

func (ue *usrEnv) site(t *testing.T, dom, owner string, mod func(*site.Site)) site.Site {
	t.Helper()
	st := site.Site{Domain: mustDomain(t, dom), Status: site.Active, PHPVersion: "8.3", Owner: owner}
	if mod != nil {
		mod(&st)
	}
	if err := ue.sites.SaveSite(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	return st
}

func (ue *usrEnv) db(name, owner string, mod func(*account.Database)) {
	d := account.Database{Name: name, Owner: owner, Status: account.DBActive, Charset: "utf8mb4"}
	if mod != nil {
		mod(&d)
	}
	_ = ue.dbs.SaveStandaloneDB(context.Background(), d)
}

func mustUserName(t *testing.T, s string) validate.UserName {
	t.Helper()
	n, err := validate.ParseUserName(s)
	if err != nil {
		t.Fatalf("ParseUserName(%q): %v", s, err)
	}
	return n
}

// ---------------------------------------------------------------- user

func TestUserAdd(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	em, _ := validate.ParseEmail("carol@example.com")
	u, gen, err := ue.a.AddUser(ctx, UserInput{Name: mustUserName(t, "carol"), FullName: "Carol Ng", Email: em})
	if err != nil {
		t.Fatal(err)
	}
	if gen != "sinh-ra-mat-khau-26-ky-tu" || ue.as.hashes["carol"] != "hash:"+gen {
		t.Errorf("mật khẩu sinh = %q, hash = %q", gen, ue.as.hashes["carol"])
	}
	if u.Role != account.RoleUser || u.Package != account.DefaultPackage || u.Status != account.Active ||
		u.Email != "carol@example.com" || !u.Created.Equal(ue.now) {
		t.Errorf("user = %+v", u)
	}
	// Có mật khẩu thì không sinh, không trả lại.
	if _, gen, err := ue.a.AddUser(ctx, UserInput{Name: mustUserName(t, "dave"), Password: "mat-khau-du-dai-12", Role: account.RoleAdmin}); err != nil || gen != "" {
		t.Errorf("dave: gen=%q err=%v", gen, err)
	}

	for _, c := range []struct {
		name string
		in   UserInput
		code string
	}{
		{"thiếu tên", UserInput{}, validate.CodeUserNameInvalid},
		{"trùng", UserInput{Name: mustUserName(t, "bob")}, CodeUserExists},
		{"user Linux có sẵn", UserInput{Name: mustUserName(t, "ubuntu")}, validate.CodeUserNameReserved},
		{"gói không có", UserInput{Name: mustUserName(t, "erin"), Package: "pro"}, CodePackageNotFound},
		{"vai lạ", UserInput{Name: mustUserName(t, "erin"), Role: "root"}, "args_invalid"},
		{"mật khẩu ngắn", UserInput{Name: mustUserName(t, "erin"), Password: "ngắn-quá"}, "password_too_short"},
		{"tên đầy đủ xuống dòng", UserInput{Name: mustUserName(t, "erin"), FullName: "a\nb"}, "args_invalid"},
	} {
		_, _, err := ue.a.AddUser(ctx, c.in)
		if got := refusedCode(t, err); got != c.code {
			t.Errorf("%s: mã %s, muốn %s", c.name, got, c.code)
		}
	}
	// Gói sai dạng: lỗi của validate, không chép lại chuỗi người dùng gõ.
	if _, _, err := ue.a.AddUser(ctx, UserInput{Name: mustUserName(t, "erin"), Package: "$(id)"}); err == nil || strings.Contains(err.Error(), "$(id)") {
		t.Errorf("gói tấn công: %v", err)
	}
	if _, ok, _ := ue.as.User(ctx, "erin"); ok {
		t.Error("lời từ chối mà vẫn tạo erin")
	}
	ue.a.Hash = nil
	if _, _, err := ue.a.AddUser(ctx, UserInput{Name: mustUserName(t, "frank")}); err == nil {
		t.Error("thiếu Hash mà không lỗi")
	}
}

func TestUserEditLastAdmin(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	user := account.RoleUser
	_, err := ue.a.EditUser(ctx, account.AdminName, UserChange{Role: &user}, account.Identity{Name: account.AdminName, Role: account.RoleAdmin})
	if got := refusedCode(t, err); got != CodeLastAdmin {
		t.Fatalf("hạ admin cuối: %s", got)
	}
	// Có admin thứ hai thì hạ được, và phiên của người bị hạ bị huỷ.
	ue.as.put(account.User{Name: "root2", Role: account.RoleAdmin})
	u, err := ue.a.EditUser(ctx, account.AdminName, UserChange{Role: &user}, account.Root)
	if err != nil || u.Role != account.RoleUser || !slices.Contains(ue.as.killed, account.AdminName) {
		t.Fatalf("hạ admin: %+v %v killed=%v", u, err, ue.as.killed)
	}
	// Admin bị khoá không tính là admin còn hoạt động.
	ue.as.put(account.User{Name: "root2", Role: account.RoleAdmin, Status: account.Suspended})
	admin := account.RoleAdmin
	if _, err := ue.a.EditUser(ctx, account.AdminName, UserChange{Role: &admin}, account.Root); err != nil {
		t.Fatal(err)
	}
	if _, err := ue.a.EditUser(ctx, account.AdminName, UserChange{Role: &user}, account.Root); refusedCode(t, err) != CodeLastAdmin {
		t.Error("admin còn lại duy nhất (root2 bị khoá) mà hạ được")
	}
}

func TestUserEditFields(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	ue.as.pkgs["pro"] = account.Package{Name: "pro", MaxSites: 5}
	name, pkg := "Bob B", "pro"
	em, _ := validate.ParseEmail("bob@example.com")
	u, err := ue.a.EditUser(ctx, "bob", UserChange{FullName: &name, Package: &pkg, Email: &em}, account.Root)
	if err != nil || u.FullName != name || u.Package != "pro" || u.Email != "bob@example.com" {
		t.Fatalf("%+v %v", u, err)
	}
	if len(ue.as.killed) != 0 {
		t.Errorf("không đổi vai mà huỷ phiên: %v", ue.as.killed)
	}
	bad := "basic"
	if _, err := ue.a.EditUser(ctx, "bob", UserChange{Package: &bad}, account.Root); refusedCode(t, err) != CodePackageNotFound {
		t.Error("gói không có mà sửa được")
	}
	if _, err := ue.a.EditUser(ctx, "nobody_here", UserChange{}, account.Root); refusedCode(t, err) != CodeUserNotFound {
		t.Error("user không có")
	}
	// Tên lấy từ đường dẫn API: không in lại chuỗi lạ.
	_, err = ue.a.EditUser(ctx, "x\n$(id)", UserChange{}, account.Root)
	if refusedCode(t, err) != CodeUserNotFound || strings.Contains(err.Error(), "$(id)") {
		t.Errorf("tên tấn công: %v", err)
	}
}

func TestUserSetPassword(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	gen, err := ue.a.SetPassword(ctx, "bob", "")
	if err != nil || gen == "" || ue.as.hashes["bob"] != "hash:"+gen || !slices.Contains(ue.as.killed, "bob") {
		t.Fatalf("gen=%q err=%v hash=%q killed=%v", gen, err, ue.as.hashes["bob"], ue.as.killed)
	}
	if gen, err := ue.a.SetPassword(ctx, "bob", "đủ-mười-hai-ký"); err != nil || gen != "" {
		t.Errorf("mật khẩu có dấu đủ 12 ký tự: gen=%q err=%v", gen, err)
	}
	if _, err := ue.a.SetPassword(ctx, "bob", "short"); refusedCode(t, err) != "password_too_short" {
		t.Error("mật khẩu ngắn")
	}
	if _, err := ue.a.SetPassword(ctx, "ghost", ""); refusedCode(t, err) != CodeUserNotFound {
		t.Error("user không có")
	}
}

// ---------------------------------------------------------------- khoá user

func TestUserSuspendPicksAndMarks(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	usrHooks(t)
	ue.site(t, "a.example.com", "bob", nil)
	ue.site(t, "b.example.com", "bob", func(s *site.Site) { s.Suspended = account.SuspendSelf })
	ue.site(t, "c.example.com", "bob", func(s *site.Site) { s.Status = site.Trashed })
	ue.site(t, "d.example.com", "", nil) // của admin
	ue.db("bob_shop", "bob", nil)
	ue.db("bob_old", "bob", func(d *account.Database) { d.Suspended = account.SuspendSelf })
	ue.db("bob_bin", "bob", func(d *account.Database) { d.Status = account.DBTrashed })
	ue.db("admin_x", account.AdminName, nil)
	ue.as.put(account.User{Name: "bob", Role: account.RoleUser, Status: account.Active})

	p, err := ue.a.PrepareSuspendUser(ctx, "bob", "nợ phí", account.Identity{Name: account.AdminName, Role: account.RoleAdmin})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stepIDs(p.Plan), []string{"suspend:a.example.com", "dblock:bob_shop"}; !slices.Equal(got, want) {
		t.Fatalf("bước = %v, muốn %v", got, want)
	}
	if p.Plan.Kind != "user.suspend" || p.Plan.Target != "bob" {
		t.Errorf("Plan %s %s", p.Plan.Kind, p.Plan.Target)
	}
	// after chạy thẳng: kiểm chứng của site tạm khoá thuộc thợ khác.
	if err := p.after(ctx, engine.Report{}); err != nil {
		t.Fatal(err)
	}
	a, _, _ := ue.sites.Site(ctx, mustDomain(t, "a.example.com"))
	b, _, _ := ue.sites.Site(ctx, mustDomain(t, "b.example.com"))
	if a.Suspended != account.SuspendByUser || !a.SuspendedAt.Equal(ue.now) || b.Suspended != account.SuspendSelf {
		t.Errorf("a=%q %v, b=%q", a.Suspended, a.SuspendedAt, b.Suspended)
	}
	shop, _, _ := ue.dbs.StandaloneDB(ctx, "bob_shop")
	old, _, _ := ue.dbs.StandaloneDB(ctx, "bob_old")
	if shop.Suspended != account.SuspendByUser || old.Suspended != account.SuspendSelf {
		t.Errorf("shop=%q old=%q", shop.Suspended, old.Suspended)
	}
	u := ue.as.get("bob")
	if u.Status != account.Suspended || u.SuspendReason != "nợ phí" || !u.SuspendedAt.Equal(ue.now) || !slices.Contains(ue.as.killed, "bob") {
		t.Errorf("user = %+v, killed = %v", u, ue.as.killed)
	}
	if _, err := ue.a.PrepareSuspendUser(ctx, "bob", "", account.Root); refusedCode(t, err) != CodeUserSuspended {
		t.Error("khoá lần hai")
	}

	// Mở khoá: chỉ thứ bị khoá vì user.
	p, err = ue.a.PrepareUnsuspendUser(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stepIDs(p.Plan), []string{"unsuspend:a.example.com", "dbunlock:bob_shop"}; !slices.Equal(got, want) {
		t.Fatalf("bước mở = %v, muốn %v", got, want)
	}
	if len(p.Plan.Verify) == 0 {
		t.Error("mở khoá site mà không có kiểm chứng")
	}
	if err := p.after(ctx, engine.Report{}); err != nil {
		t.Fatal(err)
	}
	a, _, _ = ue.sites.Site(ctx, mustDomain(t, "a.example.com"))
	b, _, _ = ue.sites.Site(ctx, mustDomain(t, "b.example.com"))
	shop, _, _ = ue.dbs.StandaloneDB(ctx, "bob_shop")
	old, _, _ = ue.dbs.StandaloneDB(ctx, "bob_old")
	if a.Suspended != "" || !a.SuspendedAt.IsZero() || b.Suspended != account.SuspendSelf || shop.Suspended != "" || old.Suspended != account.SuspendSelf {
		t.Errorf("sau mở: a=%q b=%q shop=%q old=%q", a.Suspended, b.Suspended, shop.Suspended, old.Suspended)
	}
	if u := ue.as.get("bob"); u.Status != account.Active || u.SuspendReason != "" || !u.SuspendedAt.IsZero() {
		t.Errorf("user sau mở = %+v", u)
	}

	// Site có DB khoá riêng: vẫn mở site, nhưng không kiểm chứng qua DB
	// (chắc chắn Fail và hoàn tác cả lần mở khoá user).
	ue.site(t, "a.example.com", "bob", func(s *site.Site) {
		s.Suspended, s.DBSuspended = account.SuspendByUser, account.SuspendSelf
	})
	p, err = ue.a.PrepareUnsuspendUser(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if got := stepIDs(p.Plan); !slices.Equal(got, []string{"unsuspend:a.example.com"}) || len(p.Plan.Verify) != 0 {
		t.Errorf("bước = %v, kiểm chứng = %d; muốn mở a và không kiểm chứng", got, len(p.Plan.Verify))
	}
}

func TestUserSuspendRefusals(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	usrHooks(t)
	me := account.Identity{Name: account.AdminName, Role: account.RoleAdmin}
	if _, err := ue.a.PrepareSuspendUser(ctx, account.AdminName, "", me); refusedCode(t, err) != CodeLastAdmin {
		t.Error("tự khoá mình")
	}
	if _, err := ue.a.PrepareSuspendUser(ctx, account.AdminName, "", account.Root); refusedCode(t, err) != CodeLastAdmin {
		t.Error("khoá admin cuối từ dòng lệnh")
	}
	if _, err := ue.a.PrepareSuspendUser(ctx, "ghost", "", account.Root); refusedCode(t, err) != CodeUserNotFound {
		t.Error("user không có")
	}
	if _, err := ue.a.PrepareSuspendUser(ctx, "bob", strings.Repeat("x", 201), account.Root); refusedCode(t, err) != "args_invalid" {
		t.Error("lý do quá dài")
	}
}

// Không có gì để khoá: Plan rỗng vẫn chạy qua engine, after vẫn khoá user.
func TestUserSuspendEmptyPlanRuns(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	p, err := ue.a.PrepareSuspendUser(ctx, "bob", "", account.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Preview(ctx); err != nil {
		t.Fatal(err)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("%v %+v", err, rep.Job)
	}
	if ue.as.get("bob").Status != account.Suspended || !slices.Contains(ue.as.killed, "bob") {
		t.Error("Plan rỗng mà user chưa bị khoá")
	}
}

func TestUserTrashAll(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	usrHooks(t)
	dbAfter := 0
	userDBTrash = func(d *DBs, _ context.Context, name string) (*Planned, error) {
		plan := engine.Plan{Steps: []engine.Step{&usrStep{id: "dbtrash:" + name}}}
		return newPlanned(d.Sys, d.Journal, plan, func(context.Context, engine.Report) error { dbAfter++; return nil }), nil
	}
	if _, err := ue.a.PrepareTrashAll(ctx, "bob"); refusedCode(t, err) != "nothing_to_do" {
		t.Error("không có gì mà vẫn dựng")
	}
	ue.site(t, "a.example.com", "bob", nil)
	ue.site(t, "z.example.com", "bob", func(s *site.Site) { s.Status = site.Trashed })
	ue.site(t, "d.example.com", "", nil)
	ue.db("bob_shop", "bob", nil)

	p, err := ue.a.PrepareTrashAll(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	ids := stepIDs(p.Plan)
	if !slices.Contains(ids, "dbtrash:bob_shop") || slices.ContainsFunc(ids, func(s string) bool {
		return strings.Contains(s, "d.example.com") || strings.Contains(s, "z.example.com")
	}) {
		t.Errorf("bước = %v", ids)
	}
	if !slices.ContainsFunc(ids, func(s string) bool { return strings.Contains(s, "a.example.com") }) || len(p.Plan.Verify) == 0 {
		t.Errorf("thiếu bước hoặc kiểm chứng của a.example.com: %v", ids)
	}
	if err := p.after(ctx, engine.Report{}); err != nil {
		t.Fatal(err)
	}
	if a, _, _ := ue.sites.Site(ctx, mustDomain(t, "a.example.com")); a.Status != site.Trashed || dbAfter != 1 {
		t.Errorf("sau job: a=%s dbAfter=%d", a.Status, dbAfter)
	}

	// Site hay DB đang khoá: từ chối cả lô (quyết định 7).
	ue.site(t, "a.example.com", "bob", func(s *site.Site) { s.Suspended = account.SuspendByUser })
	if _, err := ue.a.PrepareTrashAll(ctx, "bob"); refusedCode(t, err) != CodeSiteSuspended {
		t.Error("site đang khoá")
	}
	ue.site(t, "a.example.com", "bob", func(s *site.Site) { s.DBSuspended = account.SuspendSelf })
	if _, err := ue.a.PrepareTrashAll(ctx, "bob"); refusedCode(t, err) != CodeDBSuspended {
		t.Error("DB của site đang khoá")
	}
	ue.site(t, "a.example.com", "bob", nil)
	ue.db("bob_shop", "bob", func(d *account.Database) { d.Suspended = account.SuspendSelf })
	if _, err := ue.a.PrepareTrashAll(ctx, "bob"); refusedCode(t, err) != CodeDBSuspended {
		t.Error("DB rời đang khoá")
	}
}

func TestUserDelete(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	me := account.Identity{Name: "bob", Role: account.RoleUser}
	if err := ue.a.DeleteUser(ctx, "bob", me); refusedCode(t, err) != CodeLastAdmin {
		t.Error("tự xoá mình")
	}
	ue.as.put(account.User{Name: "root2", Role: account.RoleAdmin})
	if err := ue.a.DeleteUser(ctx, account.AdminName, account.Root); refusedCode(t, err) != CodeLastAdmin {
		t.Error("xoá user admin gốc")
	}
	ue.site(t, "a.example.com", "bob", func(s *site.Site) { s.Status = site.Trashed })
	if err := ue.a.DeleteUser(ctx, "bob", account.Root); refusedCode(t, err) != CodeUserNotEmpty {
		t.Error("còn site trong thùng rác")
	}
	_ = ue.sites.DeleteSite(ctx, mustDomain(t, "a.example.com"))
	ue.db("bob_x", "bob", nil)
	if err := ue.a.DeleteUser(ctx, "bob", account.Root); refusedCode(t, err) != CodeUserNotEmpty {
		t.Error("còn DB rời")
	}
	_ = ue.dbs.DeleteStandaloneDB(ctx, "bob_x")
	ue.as.running["bob"] = 1
	if err := ue.a.DeleteUser(ctx, "bob", account.Root); refusedCode(t, err) != CodeUserNotEmpty {
		t.Error("đang có job")
	}
	ue.as.running["bob"] = 0
	if err := ue.a.DeleteUser(ctx, "bob", account.Root); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := ue.as.User(ctx, "bob"); ok {
		t.Error("bob vẫn còn")
	}
	// root2 là admin cuối (admin gốc chặn riêng ở trên).
	ue.as.put(account.User{Name: account.AdminName, Role: account.RoleUser})
	if err := ue.a.DeleteUser(ctx, "root2", account.Root); refusedCode(t, err) != CodeLastAdmin {
		t.Error("xoá admin cuối")
	}
}

// ---------------------------------------------------------------- gói, xem

func TestUserPackages(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	if err := ue.a.AddPackage(ctx, account.Package{Name: "pro", MaxSites: 3}); err != nil {
		t.Fatal(err)
	}
	if err := ue.a.AddPackage(ctx, account.Package{Name: "pro"}); refusedCode(t, err) != CodePackageExists {
		t.Error("trùng gói")
	}
	if err := ue.a.AddPackage(ctx, account.Package{Name: "neg", MaxDBs: -1}); refusedCode(t, err) != "args_invalid" {
		t.Error("giới hạn âm")
	}
	var ve *validate.Error
	if err := ue.a.AddPackage(ctx, account.Package{Name: "../x"}); !errors.As(err, &ve) || ve.Code != validate.CodePackageNameInvalid {
		t.Errorf("tên gói tấn công: %v", err)
	}
	created := ue.as.pkgs["pro"].Created
	ue.a.Now = func() time.Time { return ue.now.Add(time.Hour) }
	if err := ue.a.EditPackage(ctx, account.Package{Name: "pro", MaxSites: 1}); err != nil {
		t.Fatal(err)
	}
	if p := ue.as.pkgs["pro"]; p.MaxSites != 1 || !p.Created.Equal(created) {
		t.Errorf("sửa gói: %+v", p)
	}
	if err := ue.a.EditPackage(ctx, account.Package{Name: "ghost"}); refusedCode(t, err) != CodePackageNotFound {
		t.Error("sửa gói không có")
	}
	ue.as.put(account.User{Name: "bob", Role: account.RoleUser, Package: "pro"})
	if err := ue.a.DeletePackage(ctx, "pro"); refusedCode(t, err) != CodePackageInUse {
		t.Error("xoá gói còn user")
	}
	if err := ue.a.DeletePackage(ctx, account.DefaultPackage); refusedCode(t, err) != CodePackageInUse {
		t.Error("xoá gói default")
	}
	ps, err := ue.a.Packages(ctx)
	if err != nil || len(ps) != 2 || ps[0].Package.Name != "default" || ps[0].Users != 1 || ps[1].Users != 1 {
		t.Errorf("Packages = %+v %v", ps, err)
	}
	ue.as.put(account.User{Name: "bob", Role: account.RoleUser})
	if err := ue.a.DeletePackage(ctx, "pro"); err != nil {
		t.Fatal(err)
	}
}

func TestUserSummaryOverQuota(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	ue.as.pkgs["s1"] = account.Package{Name: "s1", MaxSites: 1, DiskMB: 1}
	ue.as.put(account.User{Name: "bob", Role: account.RoleUser, Package: "s1"})
	ue.site(t, "a.example.com", "bob", nil)
	s, err := ue.a.Summary(ctx, "bob")
	if err != nil || s.OverQuota || s.Usage.Sites != 1 || s.Limits.Name != "s1" {
		t.Fatalf("đúng giới hạn chưa phải vượt: %+v %v", s, err)
	}
	ue.site(t, "b.example.com", "bob", nil)
	if s, _ := ue.a.Summary(ctx, "bob"); !s.OverQuota {
		t.Error("2 site trên giới hạn 1 mà không vượt")
	}
	all, err := ue.a.Summaries(ctx)
	if err != nil || len(all) != 2 || all[0].User.Name != "admin" || all[0].OverQuota {
		t.Errorf("Summaries = %+v %v", all, err)
	}
}

// ---------------------------------------------------------------- hạn mức

func TestUserQuota(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	ue.as.pkgs["s2"] = account.Package{Name: "s2", MaxSites: 2, MaxDBs: 1, DiskMB: 10, MaxBackups: 2}
	ue.as.put(account.User{Name: "bob", Role: account.RoleUser, Package: "s2"})
	var d validate.Domain

	if err := checkQuota(ctx, nil, "bob", QuotaSites, d); err != nil {
		t.Errorf("as nil: %v", err)
	}
	if err := checkQuota(ctx, ue.as, "", QuotaSites, d); err != nil {
		t.Errorf("admin, gói default: %v", err)
	}
	if err := checkQuota(ctx, ue.as, "ghost", QuotaSites, d); refusedCode(t, err) != CodeUserNotFound {
		t.Error("chủ không có")
	}
	ue.site(t, "a.example.com", "bob", nil)
	if err := checkQuota(ctx, ue.as, "bob", QuotaSites, d); err != nil {
		t.Errorf("1/2 site: %v", err)
	}
	if err := quotaCheck(ctx, ue.as, "bob", QuotaSites, d, 1); refusedCode(t, err) != CodeQuotaSites {
		t.Error("1 site + 1 trong lô = 2/2")
	}
	ue.site(t, "b.example.com", "bob", func(s *site.Site) { s.Status = site.Trashed })
	err := checkQuota(ctx, ue.as, "bob", QuotaSites, d)
	if refusedCode(t, err) != CodeQuotaSites {
		t.Fatal("2/2 site (một ở thùng rác)")
	}
	var re *RefusedError
	errors.As(err, &re)
	if !strings.Contains(re.Message.Vi, "2 trên 2") || !strings.Contains(re.Message.Vi, "gói s2") || !strings.Contains(re.Message.En, "2 of 2") {
		t.Errorf("lời từ chối không nói số và gói: %+v", re.Message)
	}
	ue.db("bob_a", "bob", nil)
	if err := checkQuota(ctx, ue.as, "bob", QuotaDBs, d); refusedCode(t, err) != CodeQuotaDBs {
		t.Error("1/1 DB rời")
	}
	dom := mustDomain(t, "a.example.com")
	if err := quotaCheck(ctx, ue.as, "bob", QuotaBackups, dom, 1); err != nil {
		t.Errorf("1/2 backup: %v", err)
	}
	if err := quotaCheck(ctx, ue.as, "bob", QuotaBackups, dom, 2); refusedCode(t, err) != CodeQuotaBackups {
		t.Error("2/2 backup")
	}
	// Dung lượng: đúng bằng giới hạn thì qua, vượt thì chặn mọi loại.
	ue.as.disk["bob"], ue.as.measured["bob"] = 10*mib, ue.now
	if err := quotaCheck(ctx, ue.as, "bob", QuotaBackups, dom, 0); err != nil {
		t.Errorf("đúng 10 MiB: %v", err)
	}
	ue.as.disk["bob"] = 10*mib + 1
	for _, k := range []QuotaKind{QuotaSites, QuotaDBs, QuotaBackups} {
		if err := checkQuota(ctx, ue.as, "bob", k, dom); refusedCode(t, err) != CodeQuotaDisk {
			t.Errorf("%s khi vượt dung lượng", k)
		}
	}
}

// ---------------------------------------------------------------- dung lượng

func TestUserMeasureDisk(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	ue.as.put(account.User{Name: "carol", Role: account.RoleUser})
	a := ue.site(t, "a.example.com", "bob", nil)
	b := ue.site(t, "b.example.com", "bob", func(s *site.Site) { s.Status = site.Trashed })
	c := ue.site(t, "c.example.com", "carol", nil)
	ue.db("bob_shop", "bob", nil)
	ue.db("bob_gone", "bob", func(d *account.Database) { d.Status = account.DBTrashed })
	ue.m.SetDir(a.Layout().Dir, 0o750, system.Root)
	ue.m.SetDir(site.TrashDir(b.Layout()), 0o750, system.Root)
	ue.m.SetDir(c.Layout().Dir, 0o750, system.Root)
	sizes := map[string]string{a.Layout().Dir: "100", site.TrashDir(b.Layout()): "20", c.Layout().Dir: "lỗi"}
	ue.m.Handle([]string{"du"}, func(cmd system.Cmd) system.Result {
		v := sizes[cmd.Args[len(cmd.Args)-1]]
		if v == "lỗi" {
			return system.Result{ExitCode: 1, Stderr: "du: cannot read"}
		}
		return system.Result{Stdout: v + "\t" + cmd.Args[len(cmd.Args)-1] + "\n"}
	})
	var sql string
	ue.m.Handle([]string{"mariadb", "-N", "-B"}, func(cmd system.Cmd) system.Result {
		sql = string(cmd.Stdin)
		return system.Result{Stdout: a.Layout().DB + "\t3\t1000\nbob_shop\t1\t5\n"}
	})
	ue.as.disk["carol"] = 777 // lần đo cũ phải được giữ

	err := ue.a.MeasureDisk(ctx)
	if err == nil || !strings.Contains(err.Error(), "c.example.com") {
		t.Errorf("lỗi đo c phải được báo: %v", err)
	}
	if got := ue.as.disk["bob"]; got != 100+20+1000+5 {
		t.Errorf("bob = %d", got)
	}
	if ue.as.disk["carol"] != 777 {
		t.Errorf("carol bị ghi đè bằng số thiếu: %d", ue.as.disk["carol"])
	}
	if got := ue.as.disk["admin"]; got != 0 || !ue.as.measured["admin"].Equal(ue.now) {
		t.Errorf("admin không có gì phải là 0 đã đo: %d", got)
	}
	if strings.Contains(sql, "bob_gone") || !strings.Contains(sql, "'bob_shop'") || !strings.Contains(sql, "'"+b.Layout().DB+"'") {
		t.Errorf("SQL đo DB: %s", sql)
	}
	for _, r := range ue.m.Ran() {
		if r.Name == "du" && !slices.Equal(r.Args[:3], []string{"-sb", "--one-file-system", "--"}) {
			t.Errorf("argv của du: %v", r.Args)
		}
	}
}

func TestUserMeasureDiskNoDB(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	if err := ue.a.MeasureDisk(ctx); err != nil {
		t.Fatal(err)
	}
	for _, r := range ue.m.Ran() {
		if r.Name == "mariadb" {
			t.Errorf("không có DB nào mà vẫn hỏi MariaDB (IN () là lỗi cú pháp): %v", r)
		}
	}
	if ue.as.disk["bob"] != 0 {
		t.Error("bob chưa được ghi 0")
	}
}
