package server

// Chứng chỉ của panel theo SNI (M20, docs/thay-doi/M20-thiet-ke.md mục 2.6,
// 11.2 ý 5, 6). Chỉ trả chứng chỉ Let's Encrypt của lineage tpan-panel khi SNI
// BẰNG setting panel.domain VÀ chứng chỉ có SAN chứa tên đó, còn hạn, khớp
// khoá; còn lại (vào bằng IP, qua ssh -L, tên khác, không đặt tên) là chứng
// chỉ tự ký như v0.2.0 (B9). Quyết theo setting, không theo file: bỏ tên hay
// job hỏng (Undo trả setting cũ) thì chứng chỉ trên đĩa không được phục vụ.
//
// Mỗi lần bắt tay stat hai file (rẻ; chỉ có khi mở kết nối mới), mtime hoặc
// cỡ đổi thì nạp lại: gia hạn không cần restart panel. Nạp hỏng thì nhớ
// (mtime, cỡ) của lần hỏng, ghi log một lần, giữ bản đang chạy.
//
// Đọc file bằng os (như ListenAndServeTLS trước đây): đây là chứng chỉ của
// chính panel, không phải thao tác lên máy.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/core"
)

// panelDomainTTL: setting panel.domain được đệm 2 giây (bắt tay không hỏi DB
// mỗi lần); API đổi tên thì xoá đệm ngay (panelPlan, api_m20.go).
const panelDomainTTL = 2 * time.Second

type panelDomainCache struct {
	mu  sync.Mutex
	at  time.Time
	ok  bool
	val string
}

func (c *panelDomainCache) invalidate() {
	c.mu.Lock()
	c.ok = false
	c.mu.Unlock()
}

// panelDomain là tên miền của panel (chữ thường), "" khi chưa đặt.
func (s *Server) panelDomain() string {
	if s.deps.Store == nil {
		return ""
	}
	c := &s.panelDom
	c.mu.Lock()
	defer c.mu.Unlock()
	now := s.now()
	if c.ok && now.Sub(c.at) < panelDomainTTL && !now.Before(c.at) {
		return c.val
	}
	v, _, err := s.deps.Store.GetSetting(context.Background(), core.SettingPanelDomain)
	if err != nil {
		slog.Error("đọc tên miền của panel", "err", err)
		return c.val
	}
	c.val, c.at, c.ok = strings.ToLower(v), now, true
	return c.val
}

// fileStamp nhận ra một cặp file đã đổi chưa: mtime (ns) và cỡ của cả hai.
type fileStamp struct{ certMod, certSize, keyMod, keySize int64 }

func stampOf(certPath, keyPath string) (fileStamp, error) {
	c, err := os.Stat(certPath) // đi theo symlink: live/ của certbot là symlink vào archive/
	if err != nil {
		return fileStamp{}, err
	}
	k, err := os.Stat(keyPath)
	if err != nil {
		return fileStamp{}, err
	}
	return fileStamp{c.ModTime().UnixNano(), c.Size(), k.ModTime().UnixNano(), k.Size()}, nil
}

// certSlot là một cặp chứng chỉ, khoá đã nạp.
type certSlot struct {
	certPath, keyPath string
	// keepOnMissing: file mất thì vẫn phục vụ bản đã nạp (chứng chỉ tự ký:
	// panel không bao giờ mất HTTPS). Lineage ACME thì không: file mất (bỏ tên,
	// xin lại hỏng sau khi xoá) là về tự ký.
	keepOnMissing bool
	stamp, bad    fileStamp
	cert          *tls.Certificate
}

// load trả chứng chỉ hiện tại (nil khi chưa có bản nào dùng được).
func (c *certSlot) load() *tls.Certificate {
	st, err := stampOf(c.certPath, c.keyPath)
	if err != nil {
		if !c.keepOnMissing {
			c.cert, c.stamp = nil, fileStamp{}
		}
		return c.cert
	}
	if (c.cert != nil && st == c.stamp) || st == c.bad {
		return c.cert
	}
	cert, err := tls.LoadX509KeyPair(c.certPath, c.keyPath) // kiểm luôn khoá khớp chứng chỉ
	if err == nil && cert.Leaf == nil {
		cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	}
	if err != nil {
		c.bad = st
		slog.Warn("không nạp được chứng chỉ của panel; giữ bản đang dùng", "cert", c.certPath, "err", err)
		return c.cert
	}
	c.cert, c.stamp = &cert, st
	return c.cert
}

// panelCerts chọn chứng chỉ cho từng lần bắt tay TLS.
type panelCerts struct {
	domain func() string
	now    func() time.Time
	mu     sync.Mutex
	self   certSlot
	acme   certSlot
}

// newPanelCerts: acmeDir rỗng là core.PanelCertDir.
func (s *Server) newPanelCerts(certFile, keyFile, acmeDir string) *panelCerts {
	if acmeDir == "" {
		acmeDir = core.PanelCertDir
	}
	return &panelCerts{
		domain: s.panelDomain, now: s.now,
		self: certSlot{certPath: certFile, keyPath: keyFile, keepOnMissing: true},
		acme: certSlot{certPath: filepath.Join(acmeDir, "fullchain.pem"), keyPath: filepath.Join(acmeDir, "privkey.pem")},
	}
}

func (p *panelCerts) selfSigned() (*tls.Certificate, error) {
	p.mu.Lock()
	c := p.self.load()
	p.mu.Unlock()
	if c == nil {
		return nil, errors.New("không nạp được chứng chỉ tự ký của panel " + p.self.certPath)
	}
	return c, nil
}

// GetCertificate là tls.Config.GetCertificate.
func (p *panelCerts) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	sni := strings.TrimSuffix(strings.ToLower(hello.ServerName), ".")
	if sni != "" {
		if d := p.domain(); d != "" && d == sni {
			p.mu.Lock()
			c := p.acme.load()
			p.mu.Unlock()
			if c != nil {
				now := p.now()
				if now.After(c.Leaf.NotBefore) && now.Before(c.Leaf.NotAfter) && c.Leaf.VerifyHostname(sni) == nil {
					return c, nil
				}
			}
		}
	}
	return p.selfSigned()
}
