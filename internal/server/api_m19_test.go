package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// m19Foreign: thứ của user khác mà lời từ chối gửi khach_a không được nhắc.
var m19Foreign = []string{m14Wiki, m14Lab, m14UserB, m14Blog, "old.example.net", "hoc-truc-tuyen.lop-hoc-a.example.net"}

func m19NoLeak(t *testing.T, what, body string) {
	t.Helper()
	for _, s := range m19Foreign {
		if strings.Contains(body, s) {
			t.Errorf("%s: lời trả lộ %q: %s", what, s, body)
		}
	}
}

// TestNameRefusalDich: logic thuần của nameRefusal (không cần thân core).
func TestNameRefusalDich(t *testing.T) {
	name := apiDomain(t, "www.example.org")
	other := apiDomain(t, m14Wiki)
	leaky := &core.RefusedError{Code: site.CodeNameTaken, Message: i18n.T(
		"www.example.org là tên của "+m14Wiki+" ("+m14UserB+")", "taken by "+m14Wiki)}
	for _, err := range []error{
		&site.NameError{Code: site.CodeNameRootForeign, Name: name, Other: other},
		&site.NameError{Code: site.CodeNameTaken, Name: name, Other: other},
		leaky,
	} {
		var re *core.RefusedError
		if got := nameRefusal(err, false); !errors.As(got, &re) || re.Code != codeNameUnavailable {
			t.Fatalf("user thường, %v: %v", err, got)
		}
		m19NoLeak(t, "user thường", re.Message.Vi+re.Message.En)
	}
	// Admin thấy mã thật, nhưng lời dựng từ NameError không chép Other.
	var re *core.RefusedError
	if got := nameRefusal(&site.NameError{Code: site.CodeNameRootForeign, Name: name, Other: other}, true); !errors.As(got, &re) || re.Code != site.CodeNameRootForeign {
		t.Fatalf("admin: %v", got)
	}
	m19NoLeak(t, "admin", re.Message.Vi)
	// Public suffix là lỗi đầu vào (400), không phải 500.
	var ve *validate.Error
	if got := nameRefusal(&site.NameError{Code: site.CodeNamePublicSuffix, Name: apiDomain(t, "co.uk")}, false); !errors.As(got, &ve) {
		t.Fatalf("public suffix: %v", got)
	}
	if st, _ := apiErrorStatus(&validate.Error{Code: ve.Code, Message: ve.Message}); st != 400 {
		t.Errorf("public suffix: mã %d", st)
	}
	// Tên trùng site khác cũng của mình: nêu được site kia.
	if got := nameRefusal(&site.NameError{Code: site.CodeNameTakenSelf, Name: name, Other: apiDomain(t, m14Shop)}, false); !strings.Contains(got.Error(), site.CodeNameTakenSelf) {
		t.Errorf("name_taken_self: %v", got)
	}
	if nameRefusal(nil, false) != nil || nameRefusal(context.Canceled, false) != context.Canceled {
		t.Error("lỗi không phải lời từ chối phải đi nguyên")
	}
}

// TestAliasAPIDauVao: thân và đường dẫn sai bị chặn trước core, không in lại
// chuỗi lạ.
func TestAliasAPIDauVao(t *testing.T) {
	e := newM14Env(t)
	base := "/api/v1/sites/" + m14Shop + "/names"
	for _, body := range []string{`{"name":"www.` + m14Shop + `","mode":"<script>"}`} {
		rec := e.call("POST", base+"?dry_run=1", body, m14UserA)
		if code := apiErrCode(t, rec, 400); code != codeAliasModeBad || strings.Contains(rec.Body.String(), "script") {
			t.Errorf("mode lạ: %s %s", code, rec.Body)
		}
	}
	if code := apiErrCode(t, e.call("POST", base+"/www."+m14Shop+"/mode", `{}`, m14UserA), 400); code != codeAliasModeBad {
		t.Errorf("đổi kiểu không nêu mode: %s", code)
	}
	if code := apiErrCode(t, e.call("POST", base+"?dry_run=1", `{"name":"a.com; rm -rf /"}`, m14UserA), 400); code != validate.CodeDomainInvalid {
		t.Errorf("tên tấn công: %s", code)
	}
	if code := apiErrCode(t, e.call("POST", base+"?dry_run=1", `{"name":"www.`+m14Shop+`","x":1}`, m14UserA), 400); code != "bad_request" {
		t.Errorf("trường lạ: %s", code)
	}
	rec := e.call("DELETE", base+"/%24%28id%29", "", m14UserA)
	if code := apiErrCode(t, rec, 404); code != codeAliasNotFound || strings.Contains(rec.Body.String(), "$(id)") {
		t.Errorf("tên lạ trên đường dẫn: %s %s", code, rec.Body)
	}
}

// TestDomainsPhamVi: GET /domains chỉ có site trong phạm vi người xem.
func TestDomainsPhamVi(t *testing.T) {
	e := newM14Env(t)
	sitesOf := func(who string) []string {
		var out []string
		for _, r := range apiDecode[[]DomainRow](t, e.call("GET", "/api/v1/domains", "", who), 200) {
			if r.Names == nil {
				t.Errorf("%s: names null thay vì []", r.Site)
			}
			out = append(out, r.Site)
		}
		return out
	}
	gotA := strings.Join(sitesOf(m14UserA), " ")
	if !strings.Contains(gotA, m14Shop) {
		t.Errorf("khach_a không thấy site của mình: %s", gotA)
	}
	m19NoLeak(t, "khach_a GET /domains", gotA)
	got := sitesOf(account.AdminName)
	for _, d := range []string{m14Blog, m14Shop, m14Wiki, m14Lab} {
		if !strings.Contains(strings.Join(got, " "), d) {
			t.Errorf("admin không thấy %s: %v", d, got)
		}
	}
}

// --- Hai test dưới cần thân core của thợ 1c (PrepareAliasAdd, NamesOp): đỏ
// cho tới khi ghép.

// TestNameUnavailableNoLeak (S1): user thường xin tên chung tên miền gốc với
// site của người khác thì nhận 409 name_unavailable, lời không nêu site hay
// chủ khác; cả xem trước lẫn chạy thật. Admin nhận mã thật.
func TestNameUnavailableNoLeak(t *testing.T) {
	e := newM14Env(t)
	path := "/api/v1/sites/" + m14Shop + "/names"
	body := `{"name":"www.example.org","mode":"serve"}`
	for _, q := range []string{"?dry_run=1", ""} {
		rec := e.call("POST", path+q, body, m14UserA)
		if code := apiErrCode(t, rec, 409); code != codeNameUnavailable {
			t.Errorf("khach_a%s: mã %s, muốn name_unavailable: %s", q, code, rec.Body)
		}
		m19NoLeak(t, "khach_a"+q, rec.Body.String())
	}
	// Tên chính của site người khác: cũng một câu chung.
	rec := e.call("POST", path+"?dry_run=1", `{"name":"`+m14Wiki+`"}`, m14UserA)
	if code := apiErrCode(t, rec, 409); code != codeNameUnavailable {
		t.Errorf("tên chính của người khác: %s", code)
	}
	if strings.Contains(rec.Body.String(), m14UserB) {
		t.Errorf("lộ chủ: %s", rec.Body)
	}
	// Admin xin tên chính của site khác: mã thật name_taken.
	rec = e.call("POST", "/api/v1/sites/"+m14Shop+"/names?dry_run=1", `{"name":"`+m14Wiki+`"}`, account.AdminName)
	if code := apiErrCode(t, rec, 409); code != site.CodeNameTaken {
		t.Errorf("admin: %s", code)
	}
}

// TestAliasAPIDryRunNoClaim: xem trước trả 200 và bản xem trước, không job,
// không nhật ký, không giữ chỗ tên trong site_names.
func TestAliasAPIDryRunNoClaim(t *testing.T) {
	e := newM14Env(t)
	// Admin trên site của mình: site của khach_a chung tên miền gốc
	// example.net với site của admin trên máy demo (P4 từ chối khach_a).
	alias := "www." + m14Blog
	pv := apiDecode[engine.Preview](t, e.call("POST", "/api/v1/sites/"+m14Blog+"/names?dry_run=1",
		`{"name":"`+alias+`"}`, account.AdminName), 200)
	if pv.Kind != "site.alias.add" || len(pv.Steps) == 0 {
		t.Errorf("Preview = %+v", pv)
	}
	if a := e.audit(t, "site.alias.add"); len(a) != 0 {
		t.Errorf("xem trước mà có nhật ký: %+v", a)
	}
	ns, ok := any(e.st).(core.NameStore)
	if !ok {
		t.Fatal("store chưa cài core.NameStore")
	}
	if _, found, err := ns.NameSite(context.Background(), apiDomain(t, alias)); err != nil || found {
		t.Errorf("xem trước đã giữ chỗ %s (found=%v, err=%v)", alias, found, err)
	}
}

// TestAliasAPIChayThat: chạy thật đi qua namesPlan.Execute và ra job
// site.alias.add (202). ponytail: máy demo chưa phục vụ tên phụ nên bước
// name.probe hỏng và job lùi (rolled_back); kiểm tên phụ hiện ở GET names và
// /domains là việc của e2e trên máy ảo (scripts/e2e-m19.sh).
func TestAliasAPIChayThat(t *testing.T) {
	e := newM14Env(t)
	j := e.job(t, "/api/v1/sites/"+m14Blog+"/names", `{"name":"www.`+m14Blog+`"}`, account.AdminName)
	if j.Kind != "site.alias.add" || j.Target != m14Blog || j.Actor != account.AdminName || j.Status == engine.JobRunning {
		t.Fatalf("job = %+v", j)
	}
}
