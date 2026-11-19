package ai

// Bộ che dữ liệu trước khi gửi cho AI (mục 8.1, nguyên tắc 4 và 5).
//
// Che làm hai lượt, mỗi lượt là MỘT lần quét bằng một regex gộp:
//
//  1. Bí mật (mọi mức): mật khẩu, khoá, salt bị thay bằng Removed.
//  2. Tên (chỉ LevelAll): tên đã Learn, email, IPv6, IPv4, domain được thay
//     bằng bí danh.
//
// Vì sao một lần quét chứ không thay lần lượt từng loại: bí danh trông y như
// dữ liệu thật (site-a.example là một domain, 203.0.113.1 là một IP). Thay
// lần lượt thì lượt sau sẽ che tiếp bí danh lượt trước sinh ra, và Unalias
// không còn đổi ngược đúng được. Cũng vì vậy, chuỗi đã là bí danh thì giữ
// nguyên: che lại một bản đã che (JSON, người gọi lỡ che hai lần) không đổi gì.

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"html"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"sync"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"

	"github.com/doxuta/tpan/internal/validate"
)

// redactState là phần ruột của Redactor. Mọi trường được mu bảo vệ.
type redactState struct {
	mu      sync.Mutex
	level   Level
	toReal  map[string]string // bí danh → giá trị thật (dạng chuẩn); đây là bảng lưu vào ai_sessions
	toAlias map[string]string // giá trị thật (dạng chuẩn) → bí danh
	next    map[aliasKind]int // số nhỏ nhất có thể còn trống của từng loại bí danh
	learned map[string]string // tên đã Learn, viết thường → khoá trong toAlias
	re      *regexp.Regexp    // regex gộp của lượt 2, dựng lại sau mỗi Learn
}

type aliasKind int

const (
	kindDomain aliasKind = iota
	kindSlug
	kindHost
	kindEmail
	kindIPv4
	kindIPv6
)

// NewRedactor dựng bộ che ở mức level. aliases là bảng bí danh → giá trị thật
// đã lưu của phiên (nil cho phiên mới): bí danh cũ được giữ nguyên, bí danh
// mới lấy số chưa dùng nên không trùng bí danh cũ.
func NewRedactor(level Level, aliases map[string]string) *Redactor {
	// Mức lạ (cấu hình gõ sai) thì che hết: thà che thừa còn hơn lọt.
	if level != LevelSecrets {
		level = LevelAll
	}
	r := &Redactor{st: redactState{
		level:   level,
		toReal:  make(map[string]string, len(aliases)),
		toAlias: make(map[string]string, len(aliases)),
		next:    map[aliasKind]int{},
		learned: map[string]string{},
	}}
	for a, real := range aliases {
		a = strings.ToLower(a)
		r.st.toReal[a] = real
		r.st.toAlias[real] = a
	}
	r.st.compile()
	return r
}

// Learn cho bộ che biết các tên thật cần che dù chúng nằm ở đâu: domain (cả
// punycode lẫn Unicode), slug của nó, hostname. Các tên suy từ slug (tp_<slug>,
// tpan-<slug>) chứa nguyên slug nên che slug là che luôn chúng: tp_site_a.
// Domain được sắp xếp trước khi gán bí danh để kết quả không phụ thuộc thứ tự
// truyền vào.
func (r *Redactor) Learn(domains []validate.Domain, hostname string) {
	st := &r.st
	st.mu.Lock()
	defer st.mu.Unlock()

	ds := make([]validate.Domain, 0, len(domains))
	for _, d := range domains {
		if !d.IsZero() {
			ds = append(ds, d)
		}
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i].String() < ds[j].String() })

	for _, d := range ds {
		ascii := d.String()
		da := st.alias(kindDomain, ascii)
		st.learned[ascii] = ascii
		if u, err := idna.Lookup.ToUnicode(ascii); err == nil && u != ascii {
			st.learned[strings.ToLower(u)] = ascii
		}
		slug := validate.SlugOf(d).String()
		if _, ok := st.toAlias[slug]; !ok {
			// Slug mang cùng chữ với domain (site-a.example → site_a) để người
			// đọc câu trả lời thấy ngay tp_site_a là user của site-a.example.
			sa := ""
			if x, ok := strings.CutPrefix(da, "site-"); ok {
				if x, ok = strings.CutSuffix(x, ".example"); ok {
					sa = "site_" + x
				}
			}
			if sa == "" || st.used(sa) {
				sa = st.allocate(kindSlug)
			}
			st.toAlias[slug] = sa
			st.toReal[sa] = slug
		}
		st.learned[slug] = slug
	}

	if h := strings.ToLower(strings.TrimSpace(hostname)); h != "" && h != "localhost" {
		st.alias(kindHost, h)
		st.learned[h] = h
	}
	st.compile()
}

// String che một chuỗi theo mức của bộ che.
func (r *Redactor) String(s string) string {
	s = removeSecrets(s)
	st := &r.st
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.level == LevelSecrets {
		return s
	}
	return replaceMatches(st.re, s, func(m []int) string {
		match := s[m[0]:m[1]]
		switch {
		case m[2] >= 0: // tên đã Learn
			if a, ok := st.toAlias[st.learned[strings.ToLower(match)]]; ok {
				return a
			}
			return st.domain(match)
		case m[4] >= 0:
			return st.email(match)
		case m[6] >= 0, m[8] >= 0:
			return st.ip(s, m[0], m[1])
		default:
			return st.domain(match)
		}
	})
}

// JSON mã hoá v thành JSON rồi che: giá trị dưới khoá có tên như mật khẩu bị
// thay cả khối bằng Removed; mọi chuỗi khác (kể cả tên khoá) qua String.
func (r *Redactor) JSON(v any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("che JSON: mã hoá %T: %w", v, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // giữ nguyên số (3306 không thành 3306.0, số lớn không mất chính xác)
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, fmt.Errorf("che JSON: đọc lại %T: %w", v, err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // output cho AI đọc, không nhúng vào HTML
	if err := enc.Encode(r.walk(tree)); err != nil {
		return nil, fmt.Errorf("che JSON: mã hoá lại %T: %w", v, err)
	}
	return json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n")), nil
}

func (r *Redactor) walk(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if sensitiveKey(k) {
				out[r.String(k)] = Removed
			} else {
				out[r.String(k)] = r.walk(val)
			}
		}
		return out
	case []any:
		for i := range t {
			t[i] = r.walk(t[i])
		}
		return t
	case string:
		return r.String(t)
	default:
		return v
	}
}

// sensitiveKeyParts: tên khoá chứa một trong các mảnh này thì giá trị là bí
// mật. Cố ý KHÔNG có "key" trần: "key", "cache_key", "primary_key" là trường
// thường, xoá đi thì AI mất dữ liệu chẩn đoán.
var sensitiveKeyParts = []string{
	"password", "passwd", "passphrase", "secret", "token", "salt",
	"private_key", "privatekey", "api_key", "apikey",
	"auth_key", "nonce_key", "logged_in_key", // khoá của wp-config.php (secure_auth_key chứa auth_key)
}

func sensitiveKey(k string) bool {
	k = strings.ReplaceAll(strings.ToLower(k), "-", "_")
	for _, p := range sensitiveKeyParts {
		if strings.Contains(k, p) {
			return true
		}
	}
	return false
}

// Unalias đổi mọi bí danh trong s về giá trị thật. Domain có dấu về dạng
// punycode (dạng chuẩn của validate.Domain).
func (r *Redactor) Unalias(s string) string {
	st := &r.st
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.toReal) == 0 {
		return s
	}
	// ponytail: dựng regex mỗi lần gọi; Unalias chỉ chạy cho đối số công cụ
	// và câu trả lời, bảng bí danh một phiên cỡ vài chục mục. Nếu chậm thì
	// lưu cache, xoá cache khi có bí danh mới.
	re := regexp.MustCompile("(?i)" + alternation(keys(st.toReal)))
	return replaceMatches(re, s, func(m []int) string {
		match := s[m[0]:m[1]]
		// Bí danh dính chữ hay số là một phần của chuỗi khác:
		// 203.0.113.1 trong 203.0.113.19, site_a trong site_ab.
		if !bounded(s, m[0], m[1]) {
			return match
		}
		if real, ok := st.toReal[strings.ToLower(match)]; ok {
			return real
		}
		return match
	})
}

// Aliases trả bản sao bảng bí danh → giá trị thật.
func (r *Redactor) Aliases() map[string]string {
	r.st.mu.Lock()
	defer r.st.mu.Unlock()
	out := make(map[string]string, len(r.st.toReal))
	for a, real := range r.st.toReal {
		out[a] = real
	}
	return out
}

// untrustedTag bắt mọi thẻ mở hay đóng "untrusted" nằm sẵn trong text, kể cả
// chữ hoa và khoảng trắng chen vào ("</ UNTRUSTED >").
var untrustedTag = regexp.MustCompile(`(?i)<(\s*/?\s*untrusted)`)

// Untrusted bọc text do người lạ gửi tới trong thẻ đánh dấu. Dấu "<" của mọi
// thẻ untrusted có sẵn trong text đổi thành "&lt;": kẻ cài câu lệnh vào log
// không đóng được thẻ sớm để phần sau trông như lời của TPAN.
func Untrusted(source, text string) string {
	text = untrustedTag.ReplaceAllString(text, "&lt;$1")
	return `<untrusted source="` + html.EscapeString(source) + "\">\n" + text + "\n</untrusted>"
}

// ------------------------------------------------------------ lượt 1: bí mật

// secretRule: regex có nhóm 2 là phần bí mật; chỉ nhóm 2 bị thay, phần còn lại
// (tên biến, dấu nháy) giữ nguyên để AI vẫn hiểu dòng đó nói gì.
type secretRule struct {
	re   *regexp.Regexp
	keep func(v string) bool // trả true thì giữ giá trị (không phải bí mật)
}

const quoted = `'(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"`

var secretRules = []secretRule{
	// Khoá PEM; thiếu dòng END (đuôi log bị cắt) thì loại tới hết chuỗi.
	{re: regexp.MustCompile(`()(-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----(?s:.*?-----END [A-Z0-9 ]*PRIVATE KEY-----|.*))`)},
	// define('DB_PASSWORD', '...'), các AUTH_KEY, ..._SALT của wp-config.php.
	{re: regexp.MustCompile(`(?i)(define\(\s*['"][a-z0-9_]*(?:password|passwd|_key|salt|secret|token)[a-z0-9_]*['"]\s*,\s*)(` + quoted + `)`)},
	// CREATE/ALTER USER ... IDENTIFIED [WITH plugin] BY|AS '...'.
	{re: regexp.MustCompile(`(?i)(identified\s+(?:with\s+\S+\s+)?(?:by|as)\s+(?:password\s+)?)(` + quoted + `|[^\s;]+)`)},
	// Mật khẩu trong URL: mysql://user:pass@host.
	{re: regexp.MustCompile(`(?i)([a-z][a-z0-9+.\-]*://[^\s:/@]+:)([^\s@/]+)@`)},
	// Authorization: Bearer ...
	{re: regexp.MustCompile(`(?i)(authorization[ \t]*:[ \t]*(?:bearer|basic|token)[ \t]+)(\S+)`)},
	// password=..., --password=..., DB_PASSWORD=..., "password": "...",
	// api_token: ..., 'password' => '...' (mảng PHP), $cfg[...]['password'] = '...',
	// X-Api-Key: ... Sau dấu = hay : chỉ nhận khoảng trắng cùng dòng, để
	// "Password:" cuối dòng không nuốt dòng sau.
	{
		re: regexp.MustCompile(`(?i)([a-z0-9_\-]*(?:password|passwd|passphrase|secret|token|api[_\-]?key|salt)["']?\]?[ \t]*(?:=>|[:=])[ \t]*)(` + quoted + `|[^\s"'&;,)]+)`),
		// MySQL ghi "(using password: YES)": đó là thông tin chẩn đoán, không phải mật khẩu.
		keep: func(v string) bool { return strings.EqualFold(v, "yes") || strings.EqualFold(v, "no") },
	},
}

func removeSecrets(s string) string {
	for _, rule := range secretRules {
		s = replaceMatches(rule.re, s, func(m []int) string {
			v := s[m[4]:m[5]]
			if rule.keep != nil && rule.keep(v) {
				return s[m[0]:m[1]]
			}
			return s[m[0]:m[4]] + mask(v) + s[m[5]:m[1]]
		})
	}
	return s
}

// mask thay giá trị bằng Removed, giữ cặp nháy bao ngoài nếu có.
func mask(v string) string {
	if len(v) >= 2 && (v[0] == '\'' || v[0] == '"') && v[len(v)-1] == v[0] {
		return v[:1] + Removed + v[:1]
	}
	return Removed
}

// ------------------------------------------------------------- lượt 2: tên

const (
	labelPat  = `[\p{L}\p{N}](?:[\p{L}\p{M}\p{N}\-]*[\p{L}\p{M}\p{N}])?`
	domainPat = `(?:` + labelPat + `\.)+(?:xn--[a-z0-9\-]+|[\p{L}\p{M}]{2,63})`
	emailPat  = `[a-z0-9._%+\-]+@` + domainPat
	ipv6Pat   = `(?:[0-9a-f]{0,4}:){2,7}(?:\d{1,3}(?:\.\d{1,3}){3}|[0-9a-f]{0,4})`
	ipv4Pat   = `\d{1,3}(?:\.\d{1,3}){3}`
)

// compile dựng regex gộp. Thứ tự nhóm là thứ tự ưu tiên khi hai nhóm khớp từ
// cùng một vị trí: tên đã Learn trước (dài trước ngắn), để slug và domain của
// site luôn ra đúng bí danh đã gán chứ không bị bắt như domain lạ.
func (st *redactState) compile() {
	lits := `[^\s\S]` // lớp rỗng: chưa Learn gì thì nhóm 1 không bao giờ khớp
	if len(st.learned) > 0 {
		lits = alternation(keys(st.learned))
	}
	st.re = regexp.MustCompile(`(?i)(` + lits + `)|(` + emailPat + `)|(` + ipv6Pat + `)|(` + ipv4Pat + `)|(` + domainPat + `)`)
}

// fileExts: đuôi tên file hay gặp trong log và config. Chuỗi kết thúc bằng
// các đuôi này là tên file (wp-config.php, nginx.conf), không phải domain.
// Giá phải trả: domain thật có TLD trùng đuôi (.sh, .md, .py, .zip) mà chưa
// Learn sẽ không bị che. Domain của site luôn được Learn nên vẫn che.
var fileExts = toSet("php phtml conf cnf ini log sock socket sql json service timer target mount " +
	"html htm js mjs css txt md go py sh bash yml yaml toml xml pem crt key csr pub asc gpg " +
	"gz tgz bz2 xz zst zip tar bak tmp pid lock db sqlite png jpg jpeg gif svg webp ico " +
	"woff woff2 ttf map env so old orig swp csv list sources rules out err journal")

// domain đổi một chuỗi trông như domain thành bí danh. Regex chỉ bắt hình
// dạng "chữ.chữ", nên chuỗi tới đây có thể là domain, tên file (wp-config.php),
// hay mã của chính tpan (site.add, step.done, nginx.vhost). Thứ tự xét:
//
//  1. Tách đuôi file ra và giữ lại: other.example.org.conf → site-c.example.conf.
//  2. Phần còn lại kết thúc bằng một domain đã Learn thì thay đúng phần đó
//     bằng bí danh của site, giữ tiền tố: tpan-shop.example.com.conf →
//     tpan-site-a.example.conf. Regex chọn chỗ khớp bắt đầu sớm nhất, nên nó
//     bắt cả "tpan-shop.example.com.conf" từ chữ t chứ không bắt nhóm tên đã
//     Learn; không có bước này file vhost mang bí danh khác bí danh của site.
//     Bước này đứng trước bước 3: domain đã Learn luôn bị che dù đuôi gì.
//  3. Còn lại chỉ che khi nhãn cuối là đuôi tên miền thật (isDomain).
func (st *redactState) domain(m string) string {
	base, ext := m, ""
	for {
		i := strings.LastIndexByte(base, '.')
		if i < 0 {
			return m // chỉ còn tên file trần như wp-config.php
		}
		if !fileExts[strings.ToLower(base[i+1:])] {
			break
		}
		ext = base[i:] + ext
		base = base[:i]
	}
	if prefix, a, ok := st.learnedTail(base); ok {
		return prefix + a + ext
	}
	key, err := idna.Lookup.ToASCII(base)
	if err != nil {
		key = strings.ToLower(base) // không chuẩn hoá được thì vẫn xét, không bỏ qua
	}
	if _, isAlias := st.toReal[key]; isAlias {
		return m
	}
	if !isDomain(base, key) {
		return m
	}
	return st.alias(kindDomain, key) + ext
}

// learnedTail tìm tên đã Learn (domain, hostname) nằm ở cuối base, ngay sau
// đầu chuỗi, dấu chấm hay gạch nối. Trả phần đứng trước nó và bí danh của nó.
// Tên dài nhất thắng: Learn cả example.com lẫn shop.example.com thì
// www.shop.example.com ra www.<bí danh của shop>.
//
// Cái giá: một domain lạ như my-shop.example.com hiện ra là my-site-a.example,
// lộ tiền tố "my-". Đổi lại file tpan-<domain>.conf, pool, thư mục con
// (www.<domain>) đều nối được với đúng site.
func (st *redactState) learnedTail(base string) (prefix, alias string, ok bool) {
	best := -1
	for name, key := range st.learned {
		if !strings.Contains(name, ".") || len(name) <= best {
			continue // slug không có dấu chấm, regex domain không bắt nó tới đây
		}
		n := len(base) - len(name)
		if n < 0 || !strings.EqualFold(base[n:], name) {
			continue
		}
		if n > 0 && base[n-1] != '.' && base[n-1] != '-' {
			continue // myshop.example.com không phải shop.example.com
		}
		if a, found := st.toAlias[key]; found {
			best, prefix, alias, ok = len(name), base[:n], a, true
		}
	}
	return prefix, alias, ok
}

// isDomain: chuỗi base (key là dạng punycode viết thường của nó) là domain khi
// nhãn cuối là đuôi tên miền do ICANN quản lý, xét bằng danh sách Public
// Suffix (com, vn, org, xn--p1ai...). Nhờ vậy mã của tpan như site.add,
// job.start, nginx.vhost giữ nguyên: add, start, vhost không phải đuôi thật.
//
// Hai ngoại lệ:
//   - Nhãn cuối viết hoa thường lẫn lộn (fmt.Errorf, os.Open) là tên hàm Go
//     trong thông báo lỗi, dù "open" là một đuôi thật.
//   - Chuỗi đúng hai nhãn mà nhãn đầu là tên nhóm mã của tpan (codeSpaces):
//     step.fail, site.repair, http.site, db.secure, listen.group. Các chữ
//     fail, repair, site, secure, group tình cờ cũng là đuôi tên miền thật.
func isDomain(base, key string) bool {
	t := base[strings.LastIndexByte(base, '.')+1:]
	if t != strings.ToLower(t) && t != strings.ToUpper(t) {
		return false
	}
	// Chỉ dựa vào icann, không so phần đuôi trả về với key: với đuôi có luật
	// ký tự đại diện (*.ck) hàm trả cả "x.ck" làm đuôi.
	if _, icann := publicsuffix.PublicSuffix(key); !icann {
		return false
	}
	first, rest, _ := strings.Cut(key, ".")
	return strings.Contains(rest, ".") || !codeSpaces[first]
}

// codeSpaces: nhãn đầu của mã tpan (loại job, loại sự kiện, mã phép kiểm,
// hành động trong nhật ký) và của khoá cấu hình hay gặp (listen.group của
// php-fpm, mysql.global_priv). Chỉ cần những nhóm có mã mà chữ thứ hai trùng
// một đuôi tên miền thật; thêm nhóm khác cũng vô hại.
// Danh sách lấy từ mọi chuỗi "a.b" trong mã tpan (grep internal, cmd) cộng
// khoá cấu hình của php-fpm, MySQL (performance_schema.events,
// session.save_path: regex dừng ở dấu _, nên chỉ thấy schema.events,
// session.save). Thêm nhóm mã mới mà chữ thứ hai là đuôi thật thì thêm vào đây.
// Giá phải trả: domain lạ đúng hai nhãn có nhãn đầu trùng (site.com, test.run,
// php.net) không bị che. Đó gần như toàn là trang công khai, không lộ gì về
// máy chủ; domain của site luôn được Learn nên vẫn che.
var codeSpaces = toSet("account admin ai apache apt check config db disk http job listen login " +
	"mariadb mcp mysql nginx os packages panel php plan pm port ports schema service session " +
	"site stack step test undo web wp wpcli")

func (st *redactState) email(m string) string {
	at := strings.LastIndexByte(m, '@')
	local, host := m[:at], m[at+1:]
	if h, err := idna.Lookup.ToASCII(host); err == nil {
		host = h
	} else {
		host = strings.ToLower(host)
	}
	key := local + "@" + host
	if _, isAlias := st.toReal[key]; isAlias {
		return m
	}
	return st.alias(kindEmail, key)
}

// ip che một IPv4 hay IPv6 nằm ở s[i:j]. Regex chỉ bắt hình dạng; netip mới
// quyết định có phải IP thật không (12:30:45 thì không).
func (st *redactState) ip(s string, i, j int) string {
	m := s[i:j]
	// Dính chữ hay số hai bên là một phần của chuỗi khác (std::vector).
	if !bounded(s, i, j) {
		return m
	}
	a, err := netip.ParseAddr(m)
	if err != nil || a.IsLoopback() || a.IsUnspecified() {
		return m
	}
	key := a.String()
	if _, isAlias := st.toReal[key]; isAlias {
		return m
	}
	if a.Is4() {
		return st.alias(kindIPv4, key)
	}
	return st.alias(kindIPv6, key)
}

// ------------------------------------------------------------- bí danh

func (st *redactState) alias(kind aliasKind, key string) string {
	if a, ok := st.toAlias[key]; ok {
		return a
	}
	a := st.allocate(kind)
	st.toAlias[key] = a
	st.toReal[a] = key
	return a
}

// used: chuỗi đã là bí danh, hoặc là một giá trị thật đã gặp (dùng nó làm bí
// danh cho thứ khác thì Unalias sẽ nhầm).
func (st *redactState) used(a string) bool {
	_, isAlias := st.toReal[a]
	_, isReal := st.toAlias[a]
	return isAlias || isReal
}

// allocate lấy bí danh nhỏ nhất chưa dùng của loại kind. Bảng nạp lại từ phiên
// cũ đã chiếm các số đầu, nên bí danh mới đánh số tiếp sau chúng.
func (st *redactState) allocate(kind aliasKind) string {
	for n := st.next[kind]; ; n++ {
		if a := aliasName(kind, n); !st.used(a) {
			st.next[kind] = n + 1
			return a
		}
	}
}

// ipv4Nets: ba dải dành cho tài liệu (RFC 5737), không trùng IP thật nào.
var ipv4Nets = []string{"203.0.113", "198.51.100", "192.0.2"}

func aliasName(kind aliasKind, n int) string {
	switch kind {
	case kindDomain:
		return "site-" + letters(n) + ".example"
	case kindSlug:
		return "site_" + letters(n)
	case kindHost:
		return "server-" + letters(n) + ".example"
	case kindEmail:
		return fmt.Sprintf("user-%d@mail.example", n+1)
	case kindIPv4:
		if n < 254*len(ipv4Nets) {
			return fmt.Sprintf("%s.%d", ipv4Nets[n/254], n%254+1)
		}
		// ponytail: quá 762 IP trong một phiên thì dùng tên kiểu domain
		// (.invalid không bao giờ là domain thật).
		return fmt.Sprintf("ipv4-%d.invalid", n+1)
	default: // kindIPv6: 2001:db8::/32 là dải dành cho tài liệu (RFC 3849)
		var b [16]byte
		b[0], b[1], b[2], b[3] = 0x20, 0x01, 0x0d, 0xb8
		binary.BigEndian.PutUint64(b[8:], uint64(n)+1)
		return netip.AddrFrom16(b).String()
	}
}

// letters: 0 → a, 25 → z, 26 → aa (đếm cơ số 26 không có chữ số 0).
func letters(n int) string {
	s := ""
	for n++; n > 0; n = (n - 1) / 26 {
		s = string(rune('a'+(n-1)%26)) + s
	}
	return s
}

// ------------------------------------------------------------- tiện ích

// replaceMatches thay từng chỗ khớp của re trong s bằng f(chỉ số nhóm), trong
// một lần quét: phần f trả về không bị quét lại.
func replaceMatches(re *regexp.Regexp, s string, f func(m []int) string) string {
	ms := re.FindAllStringSubmatchIndex(s, -1)
	if ms == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, m := range ms {
		b.WriteString(s[last:m[0]])
		b.WriteString(f(m))
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// alternation ghép các chuỗi thành "a|b|c", dài trước ngắn: regex của Go
// chọn nhánh đầu tiên khớp, nên chuỗi dài phải đứng trước chuỗi là tiền tố
// của nó (site_aa trước site_a).
func alternation(ss []string) string {
	sort.Slice(ss, func(i, j int) bool {
		if len(ss[i]) != len(ss[j]) {
			return len(ss[i]) > len(ss[j])
		}
		return ss[i] < ss[j]
	})
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = regexp.QuoteMeta(s)
	}
	return strings.Join(q, "|")
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func toSet(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}

// bounded: s[i:j] không dính chữ hay số ASCII ở hai bên.
func bounded(s string, i, j int) bool {
	return (i == 0 || !isAlnum(s[i-1])) && (j == len(s) || !isAlnum(s[j]))
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
