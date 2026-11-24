package main

// Phần trình duyệt của e2e bước 23 (mốc M20, scripts/e2e-m20.sh): Chrome không
// giao diện mở panel THẬT trên máy ảo test và bật đăng nhập hai bước như người
// dùng (T5, T20 ở mục 7.2 của docs/thay-doi/M20-thiet-ke.md).
//
//	TPAN_PANEL_PASSWORD=… TPAN_USER_PASSWORD=… go run ./scripts/anh-readme -bo m20-that \
//	    -panel https://<máy ảo>:8443 -buoc bat -ten admin2 -bi-mat-ra <file>
//	… -buoc bat-buoc -ten khach -bi-mat-ra <file>
//
// Bước bat (T5): user -ten (rỗng là admin) đăng nhập qua form, vào /account,
// bấm "Bật hai bước", đọc chuỗi bí mật trên trang, tính mã TOTP ngay trong
// chương trình này (RFC 6238, không gọi tpan), gõ mã và mật khẩu, thấy 10 mã dự
// phòng; đăng xuất; đăng nhập lại qua form: hiện chặng hai; mã sai thì báo lỗi;
// mã đúng thì vào (qua thẻ "Đã đăng nhập" khi máy chủ báo có lần nhập sai).
// Bước bat-buoc (T20): chính sách đã bắt user -ten bật hai bước; đăng nhập xong
// ở /bat-hai-buoc, mở /sites thì bị đưa về đó; bật ngay trên trang đó; bật xong
// thì /sites mở được.
//
// BÍ MẬT: chuỗi bí mật, mã, mã dự phòng và mật khẩu không bao giờ được in, kể
// cả khi khẳng định hỏng (in "<bí mật>"). Mật khẩu đi qua biến môi trường như
// bộ m16-that. -bi-mat-ra <file>: ghi chuỗi bí mật và 10 mã dự phòng vào file
// đó (0600, mỗi dòng "totp:<…>" hoặc "ma:<…>") để script e2e đưa chúng vào
// danh sách quét bí mật T60–T66; không có cờ thì không ghi đâu cả.
//
// Đồng hồ: mã tính theo giờ của MÁY CHỦ (header Date của panel), không theo giờ
// Mac: máy ảo vừa về snapshot có thể lệch giờ. KHÔNG chạy với panel thật.

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// m20SecretOut là cờ -bi-mat-ra (đăng ký ở main.go).
var m20SecretOut string

// Chữ trên giao diện mà bộ này bấm hay đọc (web/src/i18n/tai-khoan.ts,
// dang-nhap.ts). Đổi chữ trên giao diện thì đổi ở đây.
var m20Nhan = struct {
	batHaiBuoc, daLuuMa, taoLaiMa, maSai, daDangNhap, vaoTPAN, taoToken, daLuuToken string
}{
	batHaiBuoc: "Bật hai bước",
	daLuuMa:    "Đã lưu mã",
	taoLaiMa:   "Tạo lại mã dự phòng",
	maSai:      "Mã chưa đúng",
	daDangNhap: "Đã đăng nhập",
	vaoTPAN:    "Vào TPAN",
	taoToken:   "Tạo token",
	daLuuToken: "Đã lưu token",
}

// ---------------------------------------------------------------- TOTP

// totpPeriod, totpDigits: tham số chuẩn mà tpan dùng (mục 3.1 của thiết kế).
const (
	totpPeriod = 30
	totpDigits = 6
)

var secretRe = regexp.MustCompile(`^[A-Z2-7]{32}$`)

// hotp là RFC 4226 mục 5.3 (HMAC-SHA1, cắt động), 6 chữ số có số 0 đứng đầu.
func hotp(key []byte, counter uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, v%1_000_000)
}

// totpStep là bước thời gian của RFC 6238 (giây Unix chia 30).
func totpStep(t time.Time) uint64 { return uint64(t.Unix()) / totpPeriod }

// totpKey giải chuỗi base32 (chữ hoa, không đệm) thành khoá.
func totpKey(secret string) ([]byte, error) {
	k, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return nil, errors.New("chuỗi bí mật không phải base32")
	}
	return k, nil
}

// serverClock là giờ của máy chủ: giờ Mac cộng độ lệch đo từ header Date.
type serverClock struct{ skew time.Duration }

func (k serverClock) now() time.Time { return time.Now().Add(k.skew) }

// measureSkew đọc header Date của panel (độ phân giải 1 giây) để biết giờ máy chủ.
func measureSkew(c *cdp) (serverClock, error) {
	before := time.Now()
	d := c.str(`fetch('/api/v1/health',{cache:'no-store'}).then(r=>r.headers.get('Date')||'')`)
	after := time.Now()
	t, err := http.ParseTime(d)
	if err != nil {
		return serverClock{}, fmt.Errorf("không đọc được giờ máy chủ từ header Date (%q)", d)
	}
	mid := before.Add(after.Sub(before) / 2)
	// Date bị cắt về đầu giây: cộng nửa giây để độ lệch không lệch hẳn về một phía.
	return serverClock{skew: t.Add(500 * time.Millisecond).Sub(mid).Round(time.Second)}, nil
}

// codeAfter chờ tới khi bước hiện tại của máy chủ lớn hơn used (0: không chờ)
// và đã vào bước được ít nhất 2 giây, rồi trả mã của bước đó và số bước.
func codeAfter(k serverClock, key []byte, used uint64) (string, uint64) {
	for {
		now := k.now()
		s := totpStep(now)
		if s > used && now.Unix()%totpPeriod >= 2 {
			return hotp(key, s), s
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// wrongCode trả một mã 6 số chắc chắn sai: khác mã của mọi bước từ −2 tới +2.
func wrongCode(k serverClock, key []byte) string {
	s := totpStep(k.now())
	near := map[string]bool{}
	for d := int64(-2); d <= 2; d++ {
		near[hotp(key, uint64(int64(s)+d))] = true
	}
	for i := 0; ; i++ {
		c := fmt.Sprintf("%06d", (i*7919)%1_000_000)
		if !near[c] {
			return c
		}
	}
}

// ---------------------------------------------------------------- bộ m20-that

func liveM20(c *cdp, base, adminPW string, o m16Opts) error {
	if err := c.watchConsole(); err != nil {
		return err
	}
	if _, err := c.call("Page.navigate", map[string]any{"url": base + "/dang-nhap"}); err != nil {
		return err
	}
	if err := c.waitTrue(`document.readyState==='complete'`, 20*time.Second); err != nil {
		return err
	}
	if _, err := c.eval(`localStorage.setItem('tpan-theme','light');localStorage.setItem('tpan-lang','vi')`); err != nil {
		return err
	}
	name, pw := o.userName, adminPW
	if name != "" {
		pw = os.Getenv("TPAN_USER_PASSWORD")
	}
	if pw == "" {
		return errors.New("-ten cần mật khẩu ở biến môi trường TPAN_USER_PASSWORD")
	}
	k, err := measureSkew(c)
	if err != nil {
		return err
	}
	fmt.Printf("giờ máy chủ lệch giờ Mac %v (mã TOTP tính theo giờ máy chủ)\n", k.skew)

	var l liveCheck
	switch o.step {
	case "bat":
		err = liveEnroll(c, &l, base, name, pw, k)
	case "bat-buoc":
		err = liveForced(c, &l, base, name, pw, k)
	default:
		return fmt.Errorf("-buoc của m20-that chỉ nhận bat, bat-buoc (đang là %q)", o.step)
	}
	if err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), base, "<panel>"))
	}
	if l.fails > 0 {
		return fmt.Errorf("bước %s: %d khẳng định không đạt", o.step, l.fails)
	}
	return nil
}

// formLoginAny đăng nhập qua form (gõ phím thật) và trả đường dẫn nơi giao
// diện đưa tới (có h1). name rỗng là admin.
func formLoginAny(c *cdp, base, name, pw string) (string, error) {
	if err := c.goTo(base+"/dang-nhap", "/dang-nhap"); err != nil {
		return "", err
	}
	if name != "" {
		if err := c.typeInto("#ten-dang-nhap", name); err != nil {
			return "", err
		}
	}
	if err := c.typeInto("#mat-khau", pw); err != nil {
		return "", err
	}
	if err := c.keyRaw("Enter"); err != nil {
		return "", err
	}
	if err := c.waitTrue(`location.pathname!=='/dang-nhap' && !!document.querySelector('main h1')`, 30*time.Second); err != nil {
		return "", fmt.Errorf("sau khi gửi form đăng nhập vẫn ở %s: %w", c.str(`location.pathname`), err)
	}
	return c.str(`location.pathname`), nil
}

// readSecret đọc chuỗi bí mật đang hiện trên trang (nút bấm-là-chép, chữ chia
// nhóm 4 ký tự): bỏ dấu cách, phải đúng 32 ký tự base32. Không in.
func readSecret(c *cdp) string {
	return c.str(`(()=>{for(const e of document.querySelectorAll('button')){const s=(e.textContent||'').replace(/\s+/g,''); if(/^[A-Z2-7]{32}$/.test(s)) return s} return ''})()`)
}

// enroll chạy luồng bật trên trang đang mở (trang Tài khoản hoặc /bat-hai-buoc):
// bấm "Bật hai bước", đọc bí mật, gõ mã và mật khẩu, chờ 10 mã dự phòng. Trả
// bí mật và bước của mã đã dùng.
func enroll(c *cdp, l *liveCheck, pw string, k serverClock) (string, uint64, error) {
	if err := c.pressText("button", m20Nhan.batHaiBuoc); err != nil {
		return "", 0, err
	}
	if err := c.waitTrue(`(()=>{for(const e of document.querySelectorAll('button')){if(/^[A-Z2-7]{32}$/.test((e.textContent||'').replace(/\s+/g,''))) return true} return false})()`, 20*time.Second); err != nil {
		return "", 0, fmt.Errorf("bấm %q xong không thấy chuỗi bí mật: %w", m20Nhan.batHaiBuoc, err)
	}
	secret := readSecret(c)
	qr := c.str(`document.querySelector('svg[role=img] path')?'có':''`)
	l.ok(secretRe.MatchString(secret) && qr != "", "bấm %q: trang hiện mã QR (svg một path) và chuỗi bí mật 32 ký tự base32 (<bí mật>, không in)", m20Nhan.batHaiBuoc)
	key, err := totpKey(secret)
	if err != nil {
		return "", 0, err
	}
	code, step := codeAfter(k, key, 0)
	if err := c.typeInto("#hb-ma", code); err != nil {
		return "", 0, err
	}
	if err := c.typeInto("#hb-mat-khau", pw); err != nil {
		return "", 0, err
	}
	if err := c.keyRaw("Enter"); err != nil {
		return "", 0, err
	}
	const list = `[...document.querySelectorAll('ul[aria-label] li')].map(e=>e.textContent.trim())`
	if err := c.waitTrue(list+`.length===10`, 20*time.Second); err != nil {
		alert := c.str(`[...document.querySelectorAll('[role=alert]')].map(e=>e.textContent.trim()).join(' | ')`)
		return "", 0, fmt.Errorf("gõ mã (bước %d) và mật khẩu xong không thấy 10 mã dự phòng (thông báo trên trang: %s): %w", step, alert, err)
	}
	v, err := c.eval(list)
	if err != nil {
		return "", 0, err
	}
	var codes []string
	good := 0
	codeRe := regexp.MustCompile(`^[a-z2-7]{4}(-[a-z2-7]{4}){3}$`)
	for _, x := range v.([]any) {
		s, _ := x.(string)
		codes = append(codes, s)
		if codeRe.MatchString(s) {
			good++
		}
	}
	l.ok(len(codes) == 10 && good == 10, "mã tính trong chương trình (bước %d) và mật khẩu được nhận: thấy %d mã dự phòng, %d mã đúng dạng xxxx-xxxx-xxxx-xxxx (<bí mật>)", step, len(codes), good)
	if err := saveSecrets(secret, codes); err != nil {
		return "", 0, err
	}
	return secret, step, nil
}

// saveSecrets ghi bí mật vào file -bi-mat-ra (0600), nếu có cờ.
func saveSecrets(secret string, codes []string) error {
	if m20SecretOut == "" {
		return nil
	}
	f, err := os.OpenFile(m20SecretOut, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("mở -bi-mat-ra: %w", err)
	}
	var b strings.Builder
	b.WriteString("totp:" + secret + "\n")
	for _, s := range codes {
		b.WriteString("ma:" + s + "\n")
	}
	if _, err := f.WriteString(b.String()); err != nil {
		_ = f.Close()
		return fmt.Errorf("ghi -bi-mat-ra: %w", err)
	}
	return f.Close()
}

// liveEnroll là bước bat (T5).
func liveEnroll(c *cdp, l *liveCheck, base, name, pw string, k serverClock) error {
	who := name
	if who == "" {
		who = "admin"
	}
	home, err := formLoginAny(c, base, name, pw)
	if err != nil {
		return err
	}
	l.ok(true, "%s đăng nhập qua form bằng mật khẩu (chưa có hai bước): tới %s", who, home)
	if err := c.goTo(base+"/account", "/account"); err != nil {
		return err
	}
	if err := settle(c); err != nil {
		return err
	}
	l.ok(c.str(`document.getElementById('hai-buoc')?'có':''`) != "" && c.str(`document.getElementById('token')?'có':''`) != "",
		"/account có thẻ Đăng nhập hai bước (#hai-buoc) và thẻ Token API (#token); tiêu đề «%s»", c.str(`document.querySelector('main h1')?.innerText.trim()||''`))
	secret, step, err := enroll(c, l, pw, k)
	if err != nil {
		return err
	}
	if err := c.pressText("button", m20Nhan.daLuuMa); err != nil {
		return err
	}
	on := c.waitTrue(fmt.Sprintf(`(document.getElementById('hai-buoc')?.innerText||'').includes(%q)`, m20Nhan.taoLaiMa), 15*time.Second)
	l.ok(on == nil, "bấm %q: thẻ hai bước chuyển sang trạng thái đã bật (có nút %q)", m20Nhan.daLuuMa, m20Nhan.taoLaiMa)

	// Đăng xuất rồi đăng nhập lại qua form: phải có chặng hai.
	if err := c.logout(); err != nil {
		return err
	}
	if err := c.goTo(base+"/dang-nhap", "/dang-nhap"); err != nil {
		return err
	}
	if name != "" {
		if err := c.typeInto("#ten-dang-nhap", name); err != nil {
			return err
		}
	}
	if err := c.typeInto("#mat-khau", pw); err != nil {
		return err
	}
	if err := c.keyRaw("Enter"); err != nil {
		return err
	}
	stage2 := c.waitTrue(`!!document.getElementById('ma-hai-buoc')`, 20*time.Second)
	l.ok(stage2 == nil && c.str(`location.pathname`) == "/dang-nhap", "đăng xuất, đăng nhập lại bằng mật khẩu: vẫn ở /dang-nhap, hiện chặng hai (ô #ma-hai-buoc)")
	if stage2 != nil {
		return errors.New("không có chặng hai, dừng bước bat")
	}
	return liveStageTwo(c, l, secret, k, step)
}

// liveStageTwo: đang ở chặng hai của màn Đăng nhập. Gõ mã sai, thấy lỗi; chờ
// bước mới (mã của bước used đã dùng lúc bật), gõ mã đúng, vào được (qua thẻ
// "Đã đăng nhập" nếu máy chủ báo có lần nhập sai).
func liveStageTwo(c *cdp, l *liveCheck, secret string, k serverClock, used uint64) error {
	key, err := totpKey(secret)
	if err != nil {
		return err
	}
	if err := c.typeInto("#ma-hai-buoc", wrongCode(k, key)); err != nil {
		return err
	}
	if err := c.keyRaw("Enter"); err != nil {
		return err
	}
	bad := c.waitTrue(fmt.Sprintf(`[...document.querySelectorAll('[role=alert]')].some(e=>e.textContent.includes(%q))`, m20Nhan.maSai), 15*time.Second)
	l.ok(bad == nil && c.str(`document.getElementById('ma-hai-buoc')?'có':''`) != "", "mã sai: báo «%s…», vẫn ở chặng hai", m20Nhan.maSai)

	code, step := codeAfter(k, key, used)
	if err := c.typeInto("#ma-hai-buoc", code); err != nil {
		return err
	}
	if err := c.keyRaw("Enter"); err != nil {
		return err
	}
	// Máy chủ báo có lần nhập sai (R5): thẻ "Đã đăng nhập" giữ lại để người
	// dùng đọc, bấm "Vào TPAN" mới đi tiếp.
	if err := c.waitTrue(fmt.Sprintf(`location.pathname!=='/dang-nhap' || (document.querySelector('main h1')?.innerText||'').includes(%q)`, m20Nhan.daDangNhap), 20*time.Second); err != nil {
		alert := c.str(`[...document.querySelectorAll('[role=alert]')].map(e=>e.textContent.trim()).join(' | ')`)
		return fmt.Errorf("mã đúng (bước %d) không vào được (thông báo: %s): %w", step, alert, err)
	}
	if c.str(`location.pathname`) == "/dang-nhap" {
		note := c.str(`[...document.querySelectorAll('[role=status]')].map(e=>e.textContent.trim()).join(' | ')`)
		l.ok(strings.Contains(note, "1 lần"), "mã đúng sau một mã sai: thẻ «%s» báo có 1 lần nhập sai mã («%s»)", m20Nhan.daDangNhap, note)
		if err := c.pressText("button", m20Nhan.vaoTPAN); err != nil {
			return err
		}
	}
	in := c.waitTrue(`location.pathname!=='/dang-nhap' && !!document.querySelector('main h1') && !!document.querySelector('nav#menu-chinh')`, 20*time.Second)
	h1 := c.str(`document.querySelector('main h1')?.innerText.trim()||''`)
	l.ok(in == nil, "mã đúng (bước %d): vào panel, tới %s, thấy «%s»", step, c.str(`location.pathname`), h1)
	return nil
}

// liveForced là bước bat-buoc (T20): chính sách bắt user này bật hai bước.
func liveForced(c *cdp, l *liveCheck, base, name, pw string, k serverClock) error {
	if name == "" {
		return errors.New("-buoc bat-buoc cần -ten <user thường>")
	}
	if _, err := formLoginAny(c, base, name, pw); err != nil {
		return err
	}
	// Giao diện đi "/" rồi Layout mới chuyển sang /bat-hai-buoc: chờ chỗ dừng cuối.
	at := c.waitTrue(`location.pathname==='/bat-hai-buoc' && !!document.querySelector('main h1')`, 15*time.Second)
	l.ok(at == nil, "%s (chưa bật, bị chính sách bắt) đăng nhập qua form: tới %s (mong /bat-hai-buoc)", name, c.str(`location.pathname`))
	if _, err := c.call("Page.navigate", map[string]any{"url": base + "/sites"}); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	moved := c.waitTrue(`location.pathname==='/bat-hai-buoc' && !!document.querySelector('main h1')`, 20*time.Second)
	l.ok(moved == nil, "gõ thẳng /sites: bị đưa về /bat-hai-buoc (đang ở %s)", c.str(`location.pathname`))
	if moved != nil {
		return errors.New("không ở /bat-hai-buoc, dừng bước bat-buoc")
	}
	if _, _, err := enroll(c, l, pw, k); err != nil {
		return err
	}
	if err := c.pressText("button", m20Nhan.daLuuMa); err != nil {
		return err
	}
	left := c.waitTrue(`location.pathname!=='/bat-hai-buoc' && !!document.querySelector('main h1')`, 20*time.Second)
	l.ok(left == nil, "bấm %q: rời /bat-hai-buoc, tới %s", m20Nhan.daLuuMa, c.str(`location.pathname`))
	_ = c.consoleErrors() // các request 403 mfa_enroll_required lúc trước khi bật: không tính
	visit(c, l, base, "/sites")
	l.ok(c.str(`location.pathname`) == "/sites", "bật xong: /sites mở được (không bị đưa về /bat-hai-buoc)")
	return nil
}
