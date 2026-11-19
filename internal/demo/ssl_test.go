package demo

import (
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
)

// TestSSLSeed: blog có SSL (chứng chỉ thật về cấu trúc, còn khoảng 60 ngày,
// staging), shop chưa có; email đã đặt; phép kiểm có SSL của blog đều đạt.
func TestSSLSeed(t *testing.T) {
	ctx, sys, st := newDemo(t)

	for dom, want := range map[string]bool{siteBlog: true, siteShop: false, siteWiki: false} {
		s, ok, err := st.Site(ctx, domain(t, dom))
		if err != nil || !ok || s.SSL != want {
			t.Errorf("%s: SSL = %v (có=%v, %v), muốn %v", dom, s.SSL, ok, err, want)
		}
	}
	all, err := st.Settings(ctx)
	if err != nil || all[core.SettingSSLEmail] != demoEmail {
		t.Fatalf("ssl.email = %q, %v", all[core.SettingSSLEmail], err)
	}

	l := site.LayoutOf(domain(t, siteBlog))
	data, err := sys.ReadFile(l.CertFullchain())
	if err != nil {
		t.Fatal(err)
	}
	blk, _ := pem.Decode(data)
	if blk == nil {
		t.Fatal("fullchain.pem không phải PEM")
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := cert.VerifyHostname(siteBlog); err != nil {
		t.Errorf("chứng chỉ không khớp domain: %v", err)
	}
	if left := time.Until(cert.NotAfter); left < 59*24*time.Hour || left > 61*24*time.Hour {
		t.Errorf("chứng chỉ còn %v, muốn khoảng 60 ngày", left)
	}
	if !strings.Contains(cert.Issuer.CommonName, "(STAGING)") {
		t.Errorf("Issuer = %q, muốn chứng chỉ staging", cert.Issuer.CommonName)
	}

	tls := checks.TLSCert(l).Run(ctx, sys)
	if tls.Status != checks.Pass {
		t.Errorf("TLSCert = %s: %s", tls.Status, tls.Summary.Vi)
	}
	h, err := NewSites(sys, st).Health(ctx, domain(t, siteBlog))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"tls.cert:", "http.redirect:", "https.site:"} {
		if got := statusOf(h.Results, id); got != checks.Pass {
			t.Errorf("%s%s = %q, muốn pass (mọi phép kiểm không đạt: %v)", id, siteBlog, got, notPass(h.Results))
		}
	}
	// Site chưa có SSL: cổng 443 không nối được, nên https.site của shop (nếu ai
	// gọi) là Fail chứ không lỡ đạt.
	if r := checks.HTTPSSite(site.LayoutOf(domain(t, siteShop))).Run(ctx, sys); r.Status == checks.Pass {
		t.Errorf("shop chưa có SSL mà https.site đạt: %s", r.Summary.Vi)
	}
}

// TestSSLIssueOnDemoMachine: xin SSL cho shop trên giao diện demo chạy được trọn
// Plan trên máy giả (DNS, thử ACME, certbot, vhost, URL WordPress, kiểm chứng).
func TestSSLIssueOnDemoMachine(t *testing.T) {
	ctx, sys, st := newDemo(t)
	ssl := &core.SSL{Sys: sys, Settings: st, Store: st, Journal: st}
	d := domain(t, siteShop)
	is, err := ssl.PrepareIssue(ctx, d)
	if err != nil {
		t.Fatalf("PrepareIssue: %v", err)
	}
	rep, err := is.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s", rep.Job.Status, rep.Job.Error)
	}
	if s, _, _ := st.Site(ctx, d); !s.SSL {
		t.Fatal("job xong mà site chưa ghi SSL")
	}
	h, err := NewSites(sys, st).Health(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if bad := notPass(h.Results); len(bad) > 0 {
		t.Errorf("sau khi có SSL, shop còn: %v", bad)
	}
}
