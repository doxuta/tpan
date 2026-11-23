# M20: đăng nhập hai bước, token API có phạm vi, chứng chỉ panel được tin (thiết kế, đợt 0)

Đặc tả gốc: `agent-dev/TU-CHAY-M20.md`. Cách làm: `agent-dev/LO-TRINH.md` (tám phần, soát thiết kế, luật 8, luật 9). File này là bản thiết kế thợ viết mã làm theo; cuối mốc gộp vào mục 6.18 của `docs/KE-HOACH.md`. Quyết định tự chốt (không ai hỏi được) ghi kèm lý do ở mục 9 và ở `docs/thay-doi/M20.md`.

**Đọc mục 11 trước khi viết mã.** Mục 10 là phát hiện của thợ soát; mục 11 là bản sửa của phiên chính cho từng phát hiện. Chỗ nào mục 11 nói khác các mục 3 đến 8 thì **theo mục 11**.

Ngày làm: 21/11/2026. Máy ảo: `tpan-m20` (Ubuntu 24.04.5, arm64).

---

## 1. Vấn đề và phạm vi

Panel chạy bằng root. Hôm nay chỉ cần một mật khẩu là có root cả máy, và mọi công cụ tự động (script, CI, AI) phải dùng chung mật khẩu của người. Chứng chỉ của panel là tự ký 10 năm, dạy người dùng bấm "vẫn tiếp tục" (P8 của rà soát v0.2.0).

**Làm:**

1. Đăng nhập hai bước TOTP (RFC 6238) cho mọi user của TPAN, kèm 10 mã dự phòng dùng một lần.
2. Luồng đăng nhập hai chặng: mật khẩu đúng chỉ cho một "phiếu chờ bước hai" ngắn hạn, không vào được route nào khác.
3. Chính sách "bắt buộc hai bước" cho vai admin hoặc mọi vai; người bị bắt buộc mà chưa bật thì chỉ vào được trang bật.
4. Mất điện thoại: mã dự phòng; hết mã thì root gõ `tpan user 2fa reset <tên>` (có nhật ký).
5. Token API theo user: tên, phạm vi theo nhóm tài nguyên (đọc, ghi), hạn dùng bắt buộc (trần do admin đặt), thu hồi được, chỉ lưu bản băm, hiện một lần, có tiền tố và phần kiểm lỗi gõ, gọi bằng `Authorization: Bearer`.
6. Chứng chỉ panel được tin: admin đặt tên miền cho panel thì panel xin chứng chỉ ACME HTTP-01 (dùng lại certbot và webroot chung của site), tự gia hạn, health báo hạn. Chứng chỉ tự ký mới sinh có hạn ≤ 825 ngày.
7. CLI, API, phép kiểm health, giao diện bản 2 (trang Tài khoản, khối mới ở Cài đặt, màn đăng nhập bước hai, trang bật hai bước bắt buộc, ô mới ở trang Công cụ).

**Không làm (và vì sao):**

- WebAuthn / khoá phần cứng: cần thư viện lớn và trình duyệt thật để thử; TOTP đã đủ cho đề và chạy được với mọi app xác thực. Để mốc sau.
- Gửi mã qua SMS hay email: TPAN không có máy chủ mail (LO-TRINH: không làm mail), và SMS kém an toàn hơn TOTP.
- Gỡ hai bước qua web khi không có mã: đặc tả cấm. Đường duy nhất là root trên máy.
- Sửa logic khoá "sai 5 lần" và argon2: việc của M28 đợt 2 (P1, P2). M20 chỉ gọi vào ba hàm sẵn có (`authLocked`, `authCheckPassword`, `authFailed`) để bản vá của M28 tự phủ luôn đường của M20 (mục 3.4).
- Token cho MCP: MCP vẫn chạy qua SSH bằng root (mục 8.3), không đổi.
- Đổi chứng chỉ của panel khi vào bằng IP: vào bằng IP vẫn là chứng chỉ tự ký như cũ (CA không cấp chứng chỉ cho IP riêng).

---

## 2. Nghiên cứu

### 2.1 TOTP: tự viết, không thêm thư viện

- RFC 6238 (TOTP) = RFC 4226 (HOTP) với bộ đếm là `floor(unix_time / 30)`. HOTP là HMAC-SHA1 của bộ đếm 8 byte big-endian, cắt động (dynamic truncation) lấy 31 bit, chia dư 10^6. Toàn bộ là `crypto/hmac`, `crypto/sha1`, `encoding/binary`, `encoding/base32` của thư viện chuẩn: khoảng 40 dòng.
- Thư viện đã cân nhắc (tra 21/11/2026 bằng `go list -m -json <mô-đun>@latest`): `github.com/pquerna/otp` v1.5.0 (31/12/2024, Apache-2.0) kéo thêm `github.com/boombuler/barcode` để vẽ QR. Không chọn: phần TOTP chỉ vài chục dòng, có vector thử chính thức; thêm hai mô-đun cho chừng đó là trái mục 14 ("không thêm thư viện khi thư viện chuẩn làm được").
- Tham số chọn đúng mặc định mà các app xác thực phổ biến (Google Authenticator, Microsoft Authenticator, Aegis, 1Password, Bitwarden) đều nhận: SHA-1, 6 chữ số, bước 30 giây. Bí mật 20 byte (160 bit, đúng khuyến nghị ở RFC 4226 mục 4, R6). SHA-256 thì một số app bỏ qua tham số `algorithm` trong QR và tính bằng SHA-1, ra mã sai mà không báo; nên giữ SHA-1 (HMAC-SHA1 không bị ảnh hưởng bởi va chạm của SHA-1).
- Kiểm bằng vector thử của RFC 6238 phụ lục B (SHA-1, bí mật ASCII `12345678901234567890`, 8 chữ số, sáu mốc thời gian 59 đến 20000000000). Đối chiếu thêm với `oathtool` (gói Ubuntu `oathtool`) trên máy ảo: cùng bí mật, cùng giờ, cùng mã (làm ở e2e, mục 7.2, khẳng định T3).

### 2.2 QR: vẽ ma trận ở máy chủ bằng `rsc.io/qr`, trình duyệt vẽ SVG

- CSP của panel (đo trên máy ảo, `docs/bang-chung/M20-nghien-cuu/thu-vm.log` phần 5): `default-src 'self'; frame-ancestors 'none'`. `img-src` rơi về `default-src 'self'` nên ảnh `data:` bị chặn. Phần tử `<svg>`, `<rect>`, `<path>` do React tạo là DOM, CSP không chặn; thuộc tính `fill` không phải style nội tuyến.
- Thư viện chuẩn của Go không có QR. Đã cân nhắc (tra 21/11/2026):
  - `rsc.io/qr` v0.2.0 (05/06/2018), giấy phép BSD-3 (bản quyền "The Go Authors"), không phụ thuộc mô-đun nào, khoảng 2200 dòng (gói `qr`, `qr/coding`, `qr/gf256`). Gói con `libqrencode` dùng cgo: **không import** (chỉ import `rsc.io/qr`), `make build-linux` với `CGO_ENABLED=0` vẫn phải xanh. Chính tác giả dùng nó cho công cụ `rsc.io/2fa`. Luôn dùng mặt nạ 0 (một mặt nạ hợp lệ của chuẩn; máy quét đọc mọi mặt nạ).
  - `github.com/skip2/go-qrcode` (17/06/2020, MIT): không có bản gắn thẻ, lâu không bảo trì.
  - Thư viện JS (`qrcode`, `uqr`, `lean-qr`): thêm phụ thuộc cho `web/`, và logic mã hoá nằm ở nơi `web/` chưa có bộ chạy test đầy đủ.
  - Tự viết bộ mã hoá QR (Reed-Solomon, xếp module, mặt nạ): khoảng 500 dòng, rủi ro sai mà không có bộ giải mã để thử.
- Chọn `rsc.io/qr`: máy chủ trả ma trận (mảng chuỗi `"0101…"`), trình duyệt vẽ một `<path>` trong `<svg>`. Không có `innerHTML`, không ảnh `data:`, không thư viện JS mới. QR luôn là module tối trên nền sáng ở cả hai chế độ (nhiều app không đọc QR đảo màu). Kiểm QR thật: e2e giải mã ảnh QR bằng `zbarimg` (gói `zbar-tools`) trên máy ảo và so với URI `otpauth://` (khẳng định T4).

### 2.3 Đồng hồ và cửa sổ ±1 bước

- Máy ảo (`thu-vm.log` phần 1): `systemd-timesyncd` 255.4 đang chạy, `chrony` chưa cài (Ubuntu 24.04 dùng timesyncd mặc định; chrony chỉ thành mặc định từ 25.10). Ngay sau khi về snapshot, `NTPSynchronized=no` trong vài giây đầu.
- Lệch đồng hồ: mã của app ở bước `c = floor(Tc/30)`, máy chủ ở bước `s = floor((Tc+d)/30)`; chấp nhận khi `|s − c| ≤ 1`. Lệch `d` dưới 30 giây luôn qua; 30 đến dưới 60 giây qua theo xác suất (tuỳ vị trí trong bước); **lệch từ 60 giây trở lên luôn bị từ chối**. Test `TestTOTPLechDongHo` đo tỉ lệ chấp nhận cho `d` ∈ {±15, ±30, ±45, ±60, ±90} trên 3000 mốc thời gian, và e2e lặp lại với `oathtool --now` (T3).
- Vì vậy thêm phép kiểm `tpan.clock` (StageTPAN): `timedatectl show -p NTPSynchronized --value` là `yes` thì đạt; `no` thì cảnh báo **chỉ khi** có user đã bật hai bước (không bật thì đồng hồ lệch không ảnh hưởng đăng nhập). Không có `timedatectl` (máy dev, demo): chưa kiểm được.

### 2.4 Phiếu chờ bước hai: bảng riêng, cookie riêng

- Cách 1: thêm cột "đang chờ" vào bảng `sessions`. Mọi chỗ đọc phiên (guard, `writeSessionInfo`, các handler gọi `authSessionOf`) phải nhớ kiểm cột đó; quên một chỗ là lỗ vượt bước hai.
- Cách 2 (chọn): bảng `login_challenges` và cookie `__Host-tpan-2fa` riêng. `authResolve` (chỗ duy nhất dựng danh tính cho guard) chỉ đọc bảng `sessions` và cookie `__Host-tpan`, nên phiếu chờ **không bao giờ** thành phiên được, theo cấu trúc chứ không theo trí nhớ của người viết mã. Bất biến B1 đúng vì không có mã nào đọc phiếu ngoài hai handler bước hai và đăng xuất.
- CSRF và Fetch metadata: `POST /api/v1/login/2fa` đi qua `http.CrossOriginProtection` như mọi POST (đã gắn chung ở `secure`). Cookie phiếu: `HttpOnly`, `Secure`, `SameSite=Strict`, `Path=/`, `Max-Age=300`, tiền tố `__Host-` khi có TLS (như cookie phiên, mục 11).

### 2.5 ACME cho panel: qua vhost mặc định của tpan, lineage riêng

Đo trên máy ảo (`thu-vm.log` phần 2 đến 6), cổng 80 do web server của site giữ:

- Tên panel (`panel.m20.test`, không phải tên site nào) rơi vào vhost mặc định: nginx `tpan-default.conf` (`default_server`) và apache `000-tpan-default.conf` (vhost nạp đầu). Cả hai phục vụ `/.well-known/acme-challenge/` từ webroot chung `/var/lib/tpan/acme` (HTTP 200 đúng nội dung file thử), còn trang chủ thì nginx đóng kết nối (444, curl mã 52), apache 403. Tức là HTTP-01 cho tên panel **không đụng vhost của site nào**, không cần sửa cấu hình web server.
- `certbot certonly --webroot -w /var/lib/tpan/acme -d panel.m20.test --cert-name tpan-panel --server <pebble>` xin được chứng chỉ (pebble 2.4.0 của Ubuntu, hạn 5 năm); `certbot renew --cert-name tpan-panel --force-renewal` đổi serial (60978CC6536D21C4 → 25B3C03ACFF60665) trên cả nginx và apache; hook deploy nhận `RENEWED_LINEAGE=/etc/letsencrypt/live/tpan-panel`. `certbot.timer` có sẵn (gói certbot), gia hạn mọi lineage.
- Khoá riêng certbot ghi là PKCS#8 (`-----BEGIN PRIVATE KEY-----`), `crypto/tls.X509KeyPair` đọc được.
- Vhost của site **cũng** phục vụ thử thách từ cùng webroot (`location ^~ /.well-known/acme-challenge/`). Nên trùng tên site với tên panel không làm HTTP-01 hỏng; cái hỏng là **lineage**: nếu panel dùng `--cert-name <tên>` như site, hai bên chung một thư mục `/etc/letsencrypt/live/<tên>` và xoá hẳn site (`certbot delete --cert-name <tên>`) xoá luôn chứng chỉ của panel. Chọn: lineage cố định `tpan-panel` (không bao giờ trùng một tên miền, vì tên miền hợp lệ của tpan luôn có dấu chấm) **và** từ chối tên trùng ở cả hai chiều (đặc tả yêu cầu).

### 2.6 Panel chọn chứng chỉ theo SNI, nạp lại khi file đổi

- Hôm nay `server.Run` gọi `ListenAndServeTLS(certFile, keyFile)`: nạp một lần lúc khởi động. Gia hạn xong thì phải restart panel (cắt job đang chạy, mục 6.14).
- Chọn: `tls.Config.GetCertificate`. Bắt tay có SNI trùng một tên trong SAN của chứng chỉ ở `/etc/letsencrypt/live/tpan-panel/` (còn hạn, khớp khoá) thì trả chứng chỉ đó; còn lại (vào bằng IP, qua `ssh -L` tới 127.0.0.1, tên khác) trả chứng chỉ tự ký như cũ. Mỗi lần bắt tay `stat` hai file (rẻ; bắt tay chỉ có khi mở kết nối mới); `mtime` hoặc cỡ file đổi thì đọc lại. Đọc hỏng thì giữ bản cũ đang chạy và ghi log, không bao giờ làm panel mất HTTPS.
- Hệ quả hay: không đặt tên miền thì hành vi y hệt v0.2.0; có tên miền thì vào bằng IP vẫn được (cảnh báo tự ký như cũ), vào bằng tên thì trình duyệt tin. Gia hạn không cần restart: e2e chứng minh bằng serial mới được phục vụ trong khi `MainPID` của `tpan.service` không đổi (T40).

---

## 3. Thiết kế

### 3.1 Gói mới (lá, chỉ import thư viện chuẩn và `internal/i18n`)

**`internal/mfa`** (TOTP, mã dự phòng, niêm phong bí mật):

```go
const (
    Digits      = 6
    Period      = 30 // giây
    Skew        = 1  // chấp nhận bước hiện tại ±1
    SecretBytes = 20
    RecoveryCount = 10
)
func NewSecret() []byte                              // 20 byte crypto/rand
func EncodeSecret(b []byte) string                   // base32 RFC 4648 chữ hoa, không đệm (32 ký tự)
func HOTP(secret []byte, counter uint64, digits int) string // RFC 4226 mục 5.3, HMAC-SHA1, có số 0 đứng đầu
func Step(t time.Time) uint64                        // floor(unix/30); t trước 1970 → 0
func Match(secret []byte, code string, now time.Time) (step uint64, ok bool)
    // code phải đúng 6 chữ số ASCII (người gọi đã bỏ khoảng trắng). Thử các bước
    // Step(now)-1, Step(now), Step(now)+1; so bằng subtle.ConstantTimeCompare,
    // KHÔNG dừng sớm (thời gian như nhau dù khớp bước nào). Trả bước khớp lớn nhất.
func IsCode(s string) bool                           // đúng 6 chữ số
func URI(issuer, account string, secret []byte) string
    // otpauth://totp/<issuer>:<account>?secret=<B32>&issuer=<issuer>&algorithm=SHA1&digits=6&period=30
    // nhãn qua url.PathEscape, tham số qua url.Values (Key URI Format của Google).
func QR(text string) ([]string, error)               // rsc.io/qr mức M; mỗi chuỗi một hàng '0'/'1'; KHÔNG kèm vùng trắng
func NewRecoveryCodes() []string                     // 10 mã "xxxx-xxxx-xxxx-xxxx": 16 ký tự a-z2-7 (80 bit) mỗi mã, không trùng nhau
func NormalizeRecovery(s string) string              // bỏ khoảng trắng và '-', chữ thường; không đúng 16 ký tự a-z2-7 thì ""
func HashRecovery(normalized string) string          // sha256 hex
type Key [32]byte
func Seal(k Key, user string, plain []byte) ([]byte, error) // AES-256-GCM, nonce 12 byte ngẫu nhiên đặt trước; AAD "tpan-totp-v1:"+user
func Open(k Key, user string, sealed []byte) ([]byte, error)
```

Vì sao mã dự phòng băm bằng sha256 trần (không argon2, không khoá bí mật): mỗi mã 80 bit ngẫu nhiên (như token phiên 256 bit chỉ lưu sha256): dò ngược 2^80 là không làm được; argon2 cho 10 mã mỗi lần thử là mở đường làm máy hết RAM (P2 của M28); còn trộn khoá bí mật thì mất file khoá là mất luôn mã dự phòng, đúng lúc người ta cần chúng nhất.

Vì sao bí mật TOTP phải mã hoá (không băm được): máy chủ cần bí mật thật để tính mã. Khoá AES nằm ở `/etc/tpan/secret.key` (32 byte ngẫu nhiên, 0600 root, thư mục `/etc/tpan` 0700), không nằm trong `tpan.db`: lộ `tpan.db` hay bản sao lưu của nó thì không lộ bí mật. AAD gắn tên user: chép bản mã của user A sang dòng của user B thì giải không ra.

**`internal/apitoken`** (dạng token, phạm vi):

```go
const Prefix = "tpan_"
func New() string            // "tpan_" + base32 chữ thường không đệm của 20 byte crypto/rand (32 ký tự) + kiểm (7 ký tự)
                             // kiểm = base32 chữ thường không đệm của crc32.ChecksumIEEE(32 ký tự giữa), 4 byte big-endian
                             // tổng 44 ký tự, ví dụ tpan_5x2q…(32)…abcdefg
func WellFormed(t string) bool // đúng 44 ký tự, đúng tiền tố, chỉ a-z2-7, kiểm khớp (bắt gõ sai một ký tự trước khi hỏi DB)
func Hash(t string) string     // sha256 hex
func Hint(t string) string     // "tpan_" + 6 ký tự đầu phần giữa + "…" để nhận ra token trong danh sách

type Group string
const ( GroupSites="sites"; GroupDatabases="databases"; GroupBackups="backups"; GroupCron="cron";
        GroupJobs="jobs"; GroupHealth="health"; GroupSSL="ssl"; GroupSystem="system" )
type Scope struct{ Group Group; Write bool }          // dạng chữ "sites:read", "sites:write"
type Scopes []Scope
func ParseScopes(list []string) (Scopes, error)       // bỏ trùng, sắp xếp; nhóm lạ, quyền lạ, rỗng → lỗi
func (s Scopes) String() string                      // "backups:read,sites:write" (để lưu)
func (s Scopes) Allows(g Group, write bool) bool      // ghi bao gồm đọc cùng nhóm
func AdminOnly(g Group) bool                         // chỉ GroupSystem
```

Vì sao token có tiền tố và phần kiểm: các công cụ quét bí mật (GitHub secret scanning, gitleaks, trufflehog) bắt chuỗi theo tiền tố cố định và kiểm được ngay mà không cần hỏi máy chủ; phần kiểm CRC32 bắt token chép thiếu hay gõ sai một ký tự, trả `token_malformed` mà không chạm DB. Entropy 160 bit; DB chỉ giữ sha256 (như phiên, mục 11).

### 3.2 Dữ liệu: migration `2000_hai_buoc_token.sql`

```sql
-- user_totp: một dòng mỗi user đã bắt đầu bật hai bước. enabled_at NULL = đang chờ xác nhận.
-- secret_enc = nonce(12) || AES-256-GCM(bí mật 20 byte), khoá ở /etc/tpan/secret.key.
-- last_step: bước thời gian của mã TOTP được nhận gần nhất (chống dùng lại).
-- failures, locked_until: đếm mã sai liên tiếp ở bước hai của user này (khoá theo user, mục 3.4).
-- lock_count: số lần bị khoá liên tiếp kể từ lần đúng gần nhất (khoá leo thang, cảnh báo health).
CREATE TABLE user_totp (
    user_name    TEXT PRIMARY KEY REFERENCES users (name) ON DELETE CASCADE,
    secret_enc   BLOB NOT NULL,
    created_at   TEXT NOT NULL,
    enabled_at   TEXT,
    last_step    INTEGER NOT NULL DEFAULT 0,
    failures     INTEGER NOT NULL DEFAULT 0,
    locked_until TEXT,
    lock_count   INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE user_recovery_codes (
    user_name   TEXT NOT NULL REFERENCES users (name) ON DELETE CASCADE,
    code_sha256 TEXT NOT NULL,
    used_at     TEXT,
    PRIMARY KEY (user_name, code_sha256)
) STRICT;

-- Phiếu chờ bước hai. Không phải phiên: authResolve không bao giờ đọc bảng này.
-- host: tên máy (Host bỏ cổng, chữ thường) lúc tạo phiếu; request khác host thì như không có phiếu.
CREATE TABLE login_challenges (
    token_sha256 TEXT PRIMARY KEY,
    user_name    TEXT NOT NULL REFERENCES users (name) ON DELETE CASCADE,
    ip           TEXT NOT NULL,
    host         TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    attempts     INTEGER NOT NULL DEFAULT 0
) STRICT;

-- Phiên gắn với tên máy đã tạo nó (S2 của mục 10): cookie host-only chỉ được
-- trình duyệt gửi lại đúng tên đó, nên request mang cookie mà Host khác là
-- cookie bị chép đi nơi khác. '' là phiên của bản cũ (nhận ở mọi host cho tới
-- khi hết hạn, để nâng cấp không đăng xuất ai).
ALTER TABLE sessions ADD COLUMN host TEXT NOT NULL DEFAULT '';

CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,          -- 16 hex ngẫu nhiên, để liệt kê và thu hồi
    token_sha256 TEXT NOT NULL UNIQUE,
    user_name    TEXT NOT NULL REFERENCES users (name) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    scopes       TEXT NOT NULL,             -- apitoken.Scopes.String()
    hint         TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    revoked_at   TEXT,
    last_used_at TEXT,
    last_used_ip TEXT NOT NULL DEFAULT ''
) STRICT;
CREATE INDEX api_tokens_user ON api_tokens (user_name);

-- Nhật ký ghi token nào đã làm (rỗng: phiên đăng nhập, dòng lệnh, bộ hẹn giờ).
ALTER TABLE audit ADD COLUMN token_id TEXT NOT NULL DEFAULT '';
```

- `PRAGMA foreign_keys` đã bật cho mọi kết nối (`store.go`, DSN có `_pragma=foreign_keys(1)`), nên xoá user (`DeleteUser`) tự xoá TOTP, mã dự phòng, phiếu chờ và token của user đó **trong cùng transaction**. Không có thì tạo lại user cùng tên sẽ thừa hưởng token và hai bước của người cũ (bất biến B7).
- Khoá `settings` mới: `auth.require_2fa` (`""`/`off` | `admin` | `all`; chưa ghi là tắt), `auth.token_max_days` (số nguyên 1–365; chưa ghi là 90), `panel.domain` (tên miền hoặc chưa ghi).
- Mọi bảng mới chỉ thêm, không sửa bảng cũ ngoài một cột có DEFAULT ở `audit`: dữ liệu của v0.2.0 không đổi nghĩa.

### 3.3 Store (`internal/store/m20.go`)

```go
type TOTPRow struct {
    User string; SecretEnc []byte; Created time.Time
    Enabled time.Time // zero: đang chờ xác nhận
    LastStep uint64; Failures int; LockedUntil time.Time
}
func (s *Store) TOTP(ctx, user string) (TOTPRow, bool, error)
func (s *Store) TOTPEnabled(ctx, user string) (bool, error)
func (s *Store) SetPendingTOTP(ctx, user string, secretEnc []byte, now time.Time) error
    // đã bật → ErrTOTPEnabled; chưa có hoặc đang chờ → ghi đè bí mật chờ
func (s *Store) EnableTOTP(ctx, user string, step uint64, recoveryHashes []string, keepSession string, now time.Time) error
    // MỘT transaction: chỉ khi đang chờ (không thì ErrTOTPNotPending); enabled_at=now, last_step=step,
    // failures=0; xoá mã dự phòng cũ, thêm mã mới; xoá mọi phiên của user (kể cả phiên admin "xem như" họ)
    // trừ keepSession; xoá phiếu chờ của user
func (s *Store) DisableTOTP(ctx, user string) error      // xoá dòng TOTP, mã dự phòng, phiếu chờ của user; không có → ErrNotFound
func (s *Store) ResetTOTP(ctx, user string) error        // DisableTOTP + xoá mọi phiên của user (tpan user 2fa reset)
func (s *Store) UseTOTPStep(ctx, user string, step uint64) (bool, error)
    // UPDATE user_totp SET last_step=?, failures=0, locked_until=NULL
    //  WHERE user_name=? AND enabled_at IS NOT NULL AND last_step < ?   → true khi đúng 1 dòng
func (s *Store) UseRecoveryCode(ctx, user, codeSHA256 string, now time.Time) (bool, error)
    // UPDATE ... SET used_at=? WHERE user_name=? AND code_sha256=? AND used_at IS NULL → true khi 1 dòng
    // thành công cũng đặt failures=0, locked_until=NULL ở user_totp
func (s *Store) ReplaceRecoveryCodes(ctx, user string, hashes []string) error // một transaction
func (s *Store) RecoveryLeft(ctx, user string) (int, error)
func (s *Store) RecordTOTPFailure(ctx, user string, now time.Time, max int, lock time.Duration) (locked bool, until time.Time, err error)
    // failures++; failures >= max → locked_until = now+lock, failures = 0
type MFAStatus struct{ User string; Role account.Role; Status account.Status; Enabled bool; EnabledAt time.Time; RecoveryLeft int }
func (s *Store) MFAStatuses(ctx) ([]MFAStatus, error)    // mọi user, sắp theo tên

type Challenge struct{ TokenSHA256, User, IP string; Created, Expires time.Time; Attempts int }
func (s *Store) CreateChallenge(ctx, c Challenge) error
func (s *Store) ChallengeByToken(ctx, sha string) (Challenge, error)  // ErrNotFound
func (s *Store) BumpChallenge(ctx, sha string) (int, error)            // attempts++ trả số mới
func (s *Store) ConsumeChallenge(ctx, sha string) (bool, error)        // DELETE, true khi đúng 1 dòng
func (s *Store) DeleteChallengesOf(ctx, user string) error
func (s *Store) DeleteExpiredChallenges(ctx, now time.Time) error

type TokenRow struct{
    ID, User, Name, Scopes, Hint string
    Created, Expires, Revoked, LastUsed time.Time; LastIP string
}
func (s *Store) CreateToken(ctx, t TokenRow, tokenSHA256 string) error
func (s *Store) TokenByHash(ctx, sha string) (TokenRow, error)          // ErrNotFound
func (s *Store) Tokens(ctx, user string) ([]TokenRow, error)            // user "" = mọi user; mới nhất trước
func (s *Store) RevokeToken(ctx, id, user string, now time.Time) error  // user "" = admin thu hồi bất kỳ;
                                                                        // không có / không của user / đã thu hồi → ErrNotFound
func (s *Store) TouchToken(ctx, id, ip string, now time.Time) error
var ErrTOTPEnabled, ErrTOTPNotPending error
```

`AuditEntry` thêm trường `TokenID string` (cột `token_id`).

### 3.4 Luồng đăng nhập (máy trạng thái)

```
                 POST /login (tên, mật khẩu)
                           │  authLocked → authCheckPassword → (sai) authFailed
                           ▼
              user active? ──không──▶ 403 user_suspended (như cũ)
                           │
           đã bật hai bước? ──không──▶ ClearLoginFailures, tạo phiên, 204 (như cũ)
                           │ có
                           ▼
     tạo phiếu (5 phút, gắn IP), cookie __Host-tpan-2fa, 200 {"mfa_required":true}
     KHÔNG ClearLoginFailures (điểm ghép với M28, mục 3.4.1)
                           │
                 POST /login/2fa (mã)
                           │  authLocked; phiếu có, chưa hết hạn, đúng IP; user còn active
                           │  và còn bật hai bước; user không đang bị khoá bước hai
                           ▼
        mã 6 số → mfa.Match + UseTOTPStep │ mã 16 ký tự → UseRecoveryCode
                           │
            sai ◀──────────┴──────────▶ đúng
   BumpChallenge (5 lần → xoá phiếu)      ConsumeChallenge (đua: chỉ một bên thắng)
   RecordTOTPFailure (10 lần → khoá       ClearLoginFailures(ip)
     bước hai của user 15 phút)           tạo phiên MỚI (token mới), xoá cookie phiếu
   authFailed(ip, "login.2fa")            204, hoặc 200 {"recovery_left":n} khi dùng mã dự phòng
```

Quy tắc:

1. **Sai ở bước hai luôn đi qua `authFailed`** (đếm theo IP, khoá 15 phút ở lần thứ 5, đúng hàm của M13 và bản vá của M28 sau này). `authFailed` viết câu "Sai mật khẩu" với mã `wrong_password`; bước hai cần mã `mfa_code_wrong`. Không sửa `authFailed` (M28 đang sửa nó): gọi nó với một `ResponseWriter` thu tạm (`failRecorder`, vài chục dòng trong `auth_m20.go`); nó trả 401 thì handler bước hai tự viết 401 `mfa_code_wrong`; nó trả 423 thì chép nguyên header và thân 423 ra.
2. **Mật khẩu đúng ở bước một không xoá bộ đếm IP khi user có hai bước.** Không vậy thì ai có mật khẩu cứ đăng nhập lại để xoá bộ đếm giữa các lần đoán mã (10^6 mã, 3 mã đúng mỗi lúc: đoán không giới hạn thì trung bình vài giờ là trúng). Chỉ bước hai đúng mới xoá.
3. **Khoá theo user ở bước hai** (10 lần sai liên tiếp → 15 phút, đếm trong `user_totp`): kẻ có mật khẩu và nhiều IP không đoán mã song song được. Chỉ người đã qua được mật khẩu mới chạm tới bộ đếm này, nên nó không thành đường khoá người khác ra ngoài cho người lạ.
4. **Mỗi phiếu tối đa 5 lần nhập**; quá thì xoá phiếu, phải nhập lại mật khẩu (tốn argon2 cho kẻ tấn công).
5. **Phiếu gắn IP**: IP khác thì như không có phiếu (401 `mfa_challenge_invalid`).
6. **Mã đã dùng bị từ chối**: `last_step < step` trong một câu `UPDATE` có điều kiện (SQLite khoá ghi ngay từ đầu transaction, `_txlock=immediate`): hai request cùng mã đua nhau thì đúng một bên nhận. Mã của một bước cũ hơn bước đã dùng cũng bị từ chối. Mã dự phòng: `used_at IS NULL` trong `UPDATE` có điều kiện.
7. **Phiên mới, token mới** ở bước hai (không nâng phiếu thành phiên): chống cố định phiên.
8. **Không đọc được bí mật** (file khoá mất, hỏng): mã 6 số trả 503 `mfa_secret_unavailable` (không tính là sai), câu nói dùng mã dự phòng hoặc `sudo tpan user 2fa reset <tên>`. Mã dự phòng vẫn dùng được (chỉ cần sha256).
9. Đổi mật khẩu (`POST /account/password`, `POST /users/{name}/password`, `tpan admin passwd`, `tpan user passwd`) thì xoá phiếu chờ của user đó: phiếu chứng minh mật khẩu cũ.
10. `GET /api/v1/session`: chưa có phiên mà có phiếu hợp lệ thì `{"logged_in":false,"mfa_pending":true}` (tải lại trang ở bước hai vẫn ở bước hai). `POST /logout` xoá cả phiếu.

#### 3.4.1 Điểm ghép với M28 (ghi ở `M20.md`)

`handleLogin` chỉ thêm một nhánh sau khi kiểm user active và **trước** `ClearLoginFailures`: user có hai bước thì sang `s.startChallenge` rồi `return`. M28 sửa `authFailed`, `authLocked`, `ClearLoginFailures` thì M20 tự hưởng. Lúc `git merge origin/dev`, nếu M28 đổi chữ ký ba hàm đó thì sửa nơi gọi trong `auth_m20.go`, `api_m20.go`; nếu M28 đổi cách xoá bộ đếm (ví dụ xoá theo user thay vì theo IP) thì giữ quy tắc 2: bước một của user có hai bước không xoá gì.

### 3.5 Bật, tắt hai bước, mã dự phòng (API, chỉ phiên)

Mọi route `/account/2fa…` gắn với **user thật của phiên** (`store.Session.UserName`, không phải `who(r).Name`: lúc admin "xem như" user khác hai tên này khác nhau; POST lúc "xem như" đã bị guard chặn).

| Route | Thân | Làm gì | Lỗi |
|---|---|---|---|
| `GET /api/v1/account/2fa` | | `MFAState{enabled, enabled_at, pending, recovery_left, required, policy}` | |
| `POST /api/v1/account/2fa/setup` | `{}` | sinh bí mật mới, niêm phong, `SetPendingTOTP`; trả `MFASetup{secret, uri, qr}` một lần, `Cache-Control: no-store` | 409 `mfa_already_enabled` |
| `POST /api/v1/account/2fa/enable` | `{password, code}` | mật khẩu qua `authLocked`+`authCheckPassword`+`authFailed`; mã khớp bí mật chờ; `EnableTOTP` (đăng xuất mọi phiên khác của user); trả `{recovery_codes}` một lần, no-store | 409 `mfa_not_pending`, 400 `mfa_code_wrong`, 401/423 như mật khẩu |
| `POST /api/v1/account/2fa/disable` | `{password, code}` | bị chính sách bắt buộc thì từ chối; mật khẩu; mã TOTP (qua `UseTOTPStep`) hoặc mã dự phòng; `DisableTOTP` | 409 `mfa_required_by_policy`, 409 `mfa_not_enabled`, 400 `mfa_code_wrong` |
| `POST /api/v1/account/2fa/recovery` | `{password, code}` | như tắt nhưng tạo lại 10 mã (bộ cũ mất hiệu lực); trả mã một lần, no-store | như trên |

- Mã sai ở `disable` và `recovery` cũng đi qua `authFailed` (dò mã bằng một phiên bị chiếm); ở `enable` thì không (người đang bật nhập nhầm mã mới là chuyện thường, và mật khẩu đã được đếm).
- Issuer trong QR là `TPAN`, tên tài khoản là `<user>@<tên máy>` (người có nhiều máy phân biệt được trong app).
- Bí mật chờ xác nhận bị ghi đè mỗi lần gọi `setup`; chưa xác nhận thì đăng nhập không đổi gì.

### 3.6 Chính sách bắt buộc hai bước

- `auth.require_2fa`: `off` (mặc định), `admin`, `all`. Đặt bằng `POST /api/v1/settings/auth` (admin, chỉ phiên). Bật `admin` hay `all` khi chính người bật chưa bật hai bước thì từ chối 409 `mfa_enroll_self_first` (không tự nhốt mình).
- Guard kiểm **mỗi request** (không chụp lúc đăng nhập, để bật chính sách là áp ngay cho phiên đang mở): user thật của phiên (hoặc chủ token) thuộc diện bắt buộc mà `TOTPEnabled` sai → mọi route không có cờ `allowEnroll` trả 403 `mfa_enroll_required`. Có cờ `allowEnroll`: `GET /account/2fa`, `POST /account/2fa/setup`, `POST /account/2fa/enable` (cùng các route public: `/session`, `/logout`, `/login`, `/login/2fa`, `/health`).
- **Token của user thuộc diện bắt buộc mà chưa bật cũng bị 403 `mfa_enroll_required`**: chính sách áp cho mọi loại thông tin đăng nhập, không có đường vòng chỉ cần mật khẩu.
- `SessionInfo` thêm `mfa_enroll_required` (giao diện đưa sang trang bật hai bước) và `mfa_pending`.
- CLI và MCP chạy bằng root: không bị chính sách (như mục 6.12).

### 3.7 Token API

**Tạo** (`POST /api/v1/account/tokens`, chỉ phiên; CLI `tpan token create`): thân `{name, scopes:[...], days, password}`.

- `name`: 1–64 ký tự, không ký tự điều khiển (`token_name_invalid`).
- `scopes`: `apitoken.ParseScopes`, ít nhất một (`token_scope_invalid`); user vai `user` chọn nhóm `system` → 400 `token_scope_invalid` ("vượt quyền của bạn").
- `days`: 1 ≤ days ≤ `auth.token_max_days` (`token_days_invalid`). Hạn = lúc tạo + days × 24 giờ.
- `password`: qua `authLocked`+`authCheckPassword`+`authFailed` (tạo một thông tin đăng nhập mới sống lâu hơn phiên: phiên bị chiếm không được tự in token cho mình).
- Trả 201 `TokenCreated{token, row}` một lần, `Cache-Control: no-store`. Nhật ký `token.create`, đối tượng là id.

**Dùng**: header `Authorization: Bearer <token>`.

- Có header `Authorization` (kể cả sai dạng) thì **chỉ** xác thực bằng token, bỏ qua cookie. Thiếu chữ `Bearer ` → 401 `token_invalid`.
- `WellFormed` sai → 401 `token_malformed`. Không có hash trong DB → 401 `token_invalid`. Đã thu hồi → 401 `token_revoked`. Quá hạn → 401 `token_expired`.
- Chủ token phải còn trong `users` và `active` (khoá user → token 401 `token_invalid`); danh tính là **vai hiện tại** của chủ (`account.Identity{Name, Role}` đọc mỗi request, không có `ViewedBy`).
- Route không có nhóm token → 403 `token_forbidden`. Có nhóm mà phạm vi thiếu (`GET`/`HEAD` cần đọc, còn lại cần ghi) → 403 `token_scope`.
- Rồi mới tới luật vai của route như phiên (admin, owner:X, any). Quyền thật = phạm vi ∩ quyền hiện tại của chủ: hạ chủ từ admin xuống user thì route admin trả 403 dù token có `system:write` (bất biến B4).
- Không qua `CrossOriginProtection`? Vẫn qua: `secure` bọc mọi request; công cụ dòng lệnh không gửi `Sec-Fetch-Site`, `Origin` nên được cho qua (đúng tài liệu của `http.CrossOriginProtection`); trình duyệt không tự gửi header `Authorization`, nên token không có CSRF.
- Ghi `last_used_at`, `last_used_ip` tối đa mỗi phút một lần (như phiên). Nhật ký và job ghi chủ token làm actor, `audit.token_id` là id token.

**Bảng nhóm của từng route** (trường `tok` trong bảng route; **giá trị rỗng là chỉ phiên**, quên khai là đóng, không mở):

| Nhóm | Route |
|---|---|
| `sites` | `GET`,`POST /sites`; `GET`,`DELETE /sites/{d}`; `POST /sites/{d}/ssl`, `/restore`, `/php`, `/htaccess`, `/suspend`, `/unsuspend`; `GET /sites/{d}/traffic`, `/log`, `/route`; `GET /trash`; `DELETE /trash/{d}`; `POST /apply`; `GET /export`; `GET /php` |
| `databases` | `GET /databases`; `POST /databases/{d}/password`, `/dump`, `/import`, `/charset`, `/suspend`, `/unsuspend`; `GET /collations`; `POST /dbs`; `POST /dbs/{n}`, `/dbs/{n}/password`, `/dbs/{n}/suspend`, `/dbs/{n}/unsuspend`, `/dbs/{n}/restore`; `DELETE /dbs/{n}`; `DELETE /trash/dbs/{n}` |
| `backups` | `GET /backups`; `POST /sites/{d}/backups`; `POST /backups/{id}/restore`; `POST /backups/settings` |
| `cron` | `GET /cron` |
| `jobs` | `GET /jobs`; `GET /jobs/{id}`; `GET /jobs/{id}/events` |
| `health` | `GET /overview`; `GET /checks`; `GET /incidents`; `GET /incidents/{id}`; `POST /fixes`; `POST /files/accept`; `POST /health/run` |
| `ssl` | `GET /ssl`; `POST /ssl/settings` |
| `system` | `GET /machine`; `GET /stack`; `POST /stack/switch`; `GET /security`; `POST /security/ufw`, `/fail2ban`, `/harden`; `GET /security/isolation`; `POST /php/install`, `/php/remove`; `GET`,`POST /updates`; `POST /updates/auto`; `POST /mariadb/upgrade`; `POST /trash/settings`; `GET /self`; `POST /self/update`, `/self/rollback`; `GET /panel`; `POST /panel/domain` |
| *(chỉ phiên)* | mọi `/account/...` (mật khẩu, phiên, hai bước, token của tôi); mọi `/users...`, `/packages...`; `POST /session/view-as/exit`; mọi `/plans...`, `GET /agent`, mọi `/ai/...`; `GET`,`POST /settings/auth`; `GET /tokens`, `DELETE /tokens/{id}` |

Vì sao các route "chỉ phiên": quản lý token, mật khẩu, hai bước là đặc tả (bất biến B5). `/users` cho đặt lại mật khẩu và đổi vai người khác: cũng là quản lý mật khẩu, và là đường biến một token sống ngắn thành một tài khoản admin sống mãi. `/plans` (duyệt) và `/ai`: mục 8.1 nói Plan do AI đề xuất phải có **người** duyệt; token là thứ máy dùng, cho nó duyệt thì một agent tự duyệt Plan của chính nó.

**Liệt kê, thu hồi**: `GET /api/v1/account/tokens` (của mình), `DELETE /api/v1/account/tokens/{id}` (của mình; của người khác ra 404 như không có); admin: `GET /api/v1/tokens` (mọi user, có cột chủ), `DELETE /api/v1/tokens/{id}`. Danh sách không bao giờ có token, chỉ `hint`. Trạng thái tính lúc trả: `active`, `expired`, `revoked`.

**Trần hạn dùng**: `auth.token_max_days` (1–365, mặc định 90), đặt ở `POST /api/v1/settings/auth`. Hạ trần không rút ngắn token đã tạo (ghi rõ trên giao diện).

### 3.8 Chứng chỉ panel (Plan `panel.domain`)

Ca sử dụng `core.Panel` (file mới `internal/core/panel_m20.go`):

```go
const SettingPanelDomain = "panel.domain"
const PanelLineage = "tpan-panel"
const PanelCertDir = "/etc/letsencrypt/live/tpan-panel"
type Panel struct{ Sys system.System; Settings Settings; Store SiteStore; Journal engine.Journal; Now func() time.Time }
func (p *Panel) Domain(ctx context.Context) (validate.Domain, bool, error)
func (p *Panel) PrepareDomain(ctx context.Context, d validate.Domain) (*Planned, error) // Planned của M12 (types_m12.go); after ghi setting
func (p *Panel) PrepareClear(ctx context.Context) (*Planned, error)
func RefusePanelDomain(ctx context.Context, settings Settings, d validate.Domain) error // dùng ở PrepareAdd
```

Điều kiện đầu (chỉ đọc), từ chối bằng `RefusedError`:

1. `validate.ParseDomain`.
2. Không có site nào (mọi trạng thái, kể cả thùng rác) tên `d` → `panel_domain_is_site`.
3. Stack đã cài (cổng 80 do vhost mặc định của tpan hứng thử thách) → `stack_not_installed`.
4. Có `ssl.email` → `acme_email_missing` (dùng lại câu của SSL).
5. `checks.DNSPointsHere` không Fail → `precondition_failed` kèm kết quả.

Các bước (dùng lại Step có sẵn):

1. `steps.Dir` `/var/lib/tpan/acme/.well-known/acme-challenge` (0755 root).
2. `steps.ACMEProbe{Layout: site.LayoutOf(d), Webroot, Cert}`.
3. `steps.CertbotCert{Layout, Webroot, Email, Server: SSLConfig.ServerURL(), CertName: "tpan-panel"}`. **Trường mới `CertName`**: rỗng là tên miền (hành vi cũ của site, không đổi); có giá trị thì dùng cho `--cert-name` và cho đường dẫn `live/<CertName>/fullchain.pem`, `renewal/<CertName>.conf` trong `Check`, `Apply`. Hoàn tác không xoá chứng chỉ (như site).
4. Kiểm chứng: `checks.PanelACMECert(d, now)` (dưới) và `checks.PanelServes(d)`: `openssl s_client -connect 127.0.0.1:<cổng panel> -servername <d>` lấy chứng chỉ panel đang phục vụ, so serial với file của lineage (chứng minh GetCertificate đã chọn đúng mà không cần tin CA thử).

Job thành công thì ghi `panel.domain = d` (hàm `after` của `Planned`).

**Đổi sang tên khác**: `PrepareDomain` đọc chứng chỉ đang có của lineage (nếu có); SAN không chứa `d` thì Plan thêm bước `steps.CertbotDelete{CertName: "tpan-panel"}` **trước** bước xin, để certbot không phải sửa bộ tên của một lineage có sẵn (không dựa vào hành vi của `certonly --cert-name` khi bộ tên đổi ở chế độ không hỏi). Bước xin hỏng sau khi đã xoá thì panel về chứng chỉ tự ký (GetCertificate không thấy file): ghi trong `Why` của bước xoá. `CertbotCert.Check` khi có `CertName` còn đòi SAN của fullchain chứa tên miền (không thì coi là chưa đúng).

**Xoá tên** (`PrepareClear`): bước `steps.CertbotDelete{CertName}` (`certbot delete --cert-name tpan-panel`), rồi `after` xoá setting. Panel thấy file mất thì về chứng chỉ tự ký (GetCertificate).

`core.Sites.PrepareAdd` gọi `RefusePanelDomain` ở nhánh site mới: tên trùng `panel.domain` → `domain_is_panel`. `apply` và công cụ MCP `propose_site_add` đi qua `PrepareAdd` nên tự có.

**Server** (`internal/server/tls_m20.go`): `panelCerts{selfCert, selfKey string; acmeDir string; mu; cache}` với `GetCertificate(*tls.ClientHelloInfo)`. `server.Run` dựng `tls.Config{GetCertificate, MinVersion: tls.VersionTLS12}`, gọi `ListenAndServeTLS("", "")`. Chế độ không TLS giữ nguyên.

**Setup**: `newSelfSignedCert` đặt `NotAfter = NotBefore + 825 ngày` (chứng chỉ mới sinh). Chứng chỉ đang có giữ nguyên (không bắt ai chấp nhận lại cảnh báo của trình duyệt sau khi nâng cấp).

### 3.9 CLI

```
tpan user 2fa status [<tên>] [--json]            bảng: user, vai, hai bước bật chưa, từ lúc nào, còn mấy mã dự phòng, có bị chính sách bắt buộc không
tpan user 2fa reset <tên> [--dry-run] [--yes]   gỡ hai bước (bí mật, mã dự phòng, phiếu chờ) và đăng xuất mọi phiên của user; audit ip "cli", action "user.2fa_reset"
tpan token create <user> --name N --scope s[,s...] --days D [--json]   in token MỘT lần
tpan token list [--user U] [--json]
tpan token revoke <id> [--yes]
tpan panel domain show [--json]
tpan panel domain set <domain> [--dry-run] [--yes]
tpan panel domain set --clear [--dry-run] [--yes]
```

`tpan admin passwd` và `tpan user passwd` thêm việc xoá phiếu chờ của user (quy tắc 9 ở 3.4). Không có cờ nào nhận mã hay token trên dòng lệnh (lọt vào `ps`, lịch sử shell).

### 3.10 Phép kiểm health (StageTPAN, trong `health.selfChecks`)

| ID | Đạt | Cảnh báo | Lỗi | Không kiểm được |
|---|---|---|---|---|
| `tpan.2fa:admins` | mọi admin active đã bật hai bước | có admin chưa bật (liệt kê tên, cách bật) | | lỗi đọc DB |
| `tpan.clock` | `NTPSynchronized=yes` | `no` và có user đã bật hai bước | | không có `timedatectl` |
| `tpan.secretkey` | có user bật hai bước và file khoá đọc được, 32 byte, 0600 root, file thường | | có user bật hai bước mà file khoá mất hay hỏng (họ chỉ còn mã dự phòng) | không ai bật hai bước (bỏ khỏi danh sách) |
| `tpan.panelcert:<d>` (chỉ khi có `panel.domain`) | còn ≥ 14 ngày, SAN có `<d>` | dưới 14 ngày (certbot đáng lẽ đã gia hạn ở mốc 30 ngày) | hết hạn, mất file, sai tên | |

Cảnh báo không đổi mã thoát của `tpan health` (`healthExit` chỉ tính Fail), không mở sự cố (M4). Máy nâng từ v0.2.0 sẽ có cảnh báo vàng `tpan.2fa:admins`: đó đúng là ý của đặc tả. Đã soát: e2e M6, M13, M15, M16 không khẳng định số cảnh báo.

### 3.11 Giao diện (bản 2, `docs/GIAO-DIEN.md`)

- **Màn Đăng nhập**: thêm chặng hai trong cùng thẻ: ô "Mã 6 số" (`inputmode="numeric"`, `autocomplete="one-time-code"`), liên kết "Dùng mã dự phòng" đổi ô sang mã 16 ký tự, nút "Xác nhận", nút "Quay lại" (gọi `/logout` để xoá phiếu). Lỗi theo mã: `mfa_code_wrong`, `mfa_code_used`, `mfa_challenge_invalid` (về chặng một), `mfa_locked`, `login_locked`, `mfa_secret_unavailable`. Đăng nhập bằng mã dự phòng xong thì báo "còn N mã".
- **Trang bật hai bước bắt buộc** `/bat-hai-buoc` (bare, như đăng nhập): khi `SessionInfo.mfa_enroll_required`, mọi màn chuyển tới đây; chỉ có luồng bật và nút đăng xuất.
- **Trang Tài khoản** `/account` (menu nhóm Tài khoản, mọi vai): thẻ "Đăng nhập hai bước" (trạng thái; bật: QR + chuỗi bí mật chia nhóm 4 ký tự bấm là chép, ô mã, ô mật khẩu; xong thì hiện 10 mã dự phòng một lần với nút chép và tải `.txt`; tạo lại mã; tắt); thẻ "Token API" (bảng: tên, phạm vi, hint mono, hạn, lần dùng cuối và IP, trạng thái; nút Tạo token mở hộp: tên, ô chọn phạm vi theo nhóm × đọc/ghi (nhóm `system` chỉ admin thấy), số ngày ≤ trần, mật khẩu; tạo xong hiện token một lần; Thu hồi trong menu ba chấm, có hỏi lại).
- **Màn Cài đặt** (admin, thêm thẻ): "Đăng nhập hai bước" (chính sách off/admin/all); "Token API" (trần ngày; bảng token của mọi user với nút thu hồi); "Tên miền của panel" (tên đang dùng, hạn chứng chỉ, ô tên miền → xem trước Plan → chạy; nút xoá tên; lời nhắc tên panel sẽ hiện công khai trong nhật ký Certificate Transparency).
- **Trang Công cụ**: ô "Hai bước" (`/account`, neo `#hai-buoc`) và "Token API" (`/account`, neo `#token`) ở nhóm Tài khoản, mọi vai.
- QR: `<svg>` một `<path>`; màu qua hai biến mới trong `styles.css` (`--qr-fg`, `--qr-bg`, sáng tối như nhau) để `kieu_dang_test` không gặp mã màu trong màn.
- Biểu tượng mới trong `Icon.tsx`: `key` (token), `smartphone` (hai bước).
- Demo (`tpan serve --demo`): user `khach_a` đã bật hai bước (bí mật in lúc khởi động như mật khẩu demo), admin có hai token (một đang dùng, một đã thu hồi), `panel.domain` = `panel.example.com` với chứng chỉ giả còn 60 ngày (FakeSystem).

---

## 4. Bất biến (mỗi câu một test có tên)

| | Câu | Test |
|---|---|---|
| B1 | Không route nào ngoài bước hai và đăng xuất dùng được bằng phiếu chờ bước hai. | `TestPhieuChoKhongVaoRouteNao` (duyệt mọi route của `routes()`, chỉ mang cookie phiếu: route không public → 401; `/login/2fa`, `/logout` dùng được) |
| B2 | Một mã TOTP hoặc mã dự phòng không bao giờ được chấp nhận hai lần. | `TestMaTOTPKhongDungLai`, `TestMaDuPhongMotLan`, `TestHaiRequestCungMaChiMotThang` (song song 20 goroutine) |
| B3 | Bí mật TOTP, mã dự phòng, token không bao giờ xuất hiện dạng rõ trong `tpan.db`, log, bằng chứng, câu trả lời API sau lần hiện đầu, công cụ MCP, bản sao lưu panel. | `TestKhongLoBiMatTrongDB` (đọc mọi bảng và file WAL sau một luồng đầy đủ), `TestAPIKhongTraLaiBiMat` (quét mọi câu trả lời JSON của một luồng), e2e T60–T66 |
| B4 | Token không bao giờ làm được việc mà người tạo không làm được, kể cả khi người tạo bị hạ vai hay bị khoá sau khi tạo token. | `TestTokenTheoVaiHienTai` (hạ vai → route admin 403; khoá user → 401; xoá user → 401), `TestTokenUserKhongChonDuocSystem` |
| B5 | Token không quản lý được token, mật khẩu hay hai bước. | `TestPhanQuyenMoiRoute` mở rộng (mọi route chỉ phiên trả 403 `token_forbidden` với token đủ mọi phạm vi) |
| B6 | Nâng từ v0.2.0: không ai bị khoá ngoài; hai bước mặc định tắt; đăng nhập cũ vẫn chạy. | `TestNangCapM20GiuDangNhap` (DB của bản 1400 có phiên và admin: sau migration phiên dùng được, `/login` trả 204), e2e T80–T85 |
| B7 | Xoá user thì token, hai bước, phiếu chờ của user đó mất cùng lúc; tạo lại user cùng tên không thừa hưởng gì. | `TestXoaUserXoaTokenVaHaiBuoc` |
| B8 | Người thuộc diện bắt buộc mà chưa bật chỉ gọi được các route bật hai bước, bằng phiên hay bằng token. | `TestBatBuocChiVaoTrangBat` (duyệt mọi route) |
| B9 | Panel không có `panel.domain` phục vụ đúng chứng chỉ tự ký như v0.2.0, với mọi SNI. | `TestChungChiTheoSNI`, e2e T44 |
| B10 | Mọi route không public có quyết định token rõ ràng: nhóm hợp lệ, hoặc rỗng (chỉ phiên); không route nào nhận token mà vắng trong bảng 3.7. | `TestNhomTokenCuaRoute` (so bảng route với bảng mong đợi viết tay trong test) |

---

## 5. Mô hình đe doạ

| Kẻ tấn công | Đường | Chặn bằng | Chứng minh |
|---|---|---|---|
| Có mật khẩu (lộ, đoán), không có điện thoại | đăng nhập | bước hai | e2e T10–T14 |
| Có mật khẩu, dò mã 6 số | lặp đăng nhập để reset bộ đếm | bước một không xoá bộ đếm IP; 5 lần/phiếu; 10 lần/user → khoá 15 phút | `TestDoMaKhongResetBoDem`, `TestKhoaBuocHaiTheoUser` |
| Có mật khẩu và nhiều IP | đoán song song | khoá theo user ở bước hai | `TestKhoaBuocHaiTheoUser` |
| Lấy được cookie phiếu | gọi API | phiếu không phải phiên (B1), gắn IP, 5 phút | B1, `TestPhieuSaiIP` |
| Nghe lén một mã đúng | dùng lại mã | `last_step`, `used_at` có điều kiện | B2 |
| Chiếm được một phiên (XSS, máy bỏ quên) | bật hai bước bằng app của mình để giữ tài khoản; tắt hai bước; in token sống lâu | bật/tắt/tạo mã/tạo token đều đòi mật khẩu; tắt đòi thêm mã; bật xong đăng xuất mọi phiên khác | `TestBatHaiBuocCanMatKhau`, `TestTaoTokenCanMatKhau` |
| Đọc được `tpan.db` hoặc bản sao lưu | lấy bí mật | bí mật mã hoá bằng khoá ngoài DB; mã và token chỉ có sha256 | B3 |
| Lộ token (CI, log) | gọi API | phạm vi, hạn bắt buộc, thu hồi, chỉ dùng được với route có nhóm, IP lần dùng cuối hiện trên giao diện, tiền tố cho máy quét | `TestPhanQuyenMoiRoute` mở rộng |
| Token của admin | nâng quyền vĩnh viễn (tạo admin mới, đổi mật khẩu admin khác, duyệt Plan AI) | `/users`, `/plans`, `/ai`, `/account`, token là chỉ phiên | B5, B10 |
| Chủ token bị hạ vai hay khoá | dùng tiếp token cũ | vai đọc lại mỗi request, user phải active | B4 |
| User mới trùng tên user đã xoá | thừa hưởng | ON DELETE CASCADE | B7 |
| Trang lạ (CSRF) | POST bước hai, POST tạo token | `CrossOriginProtection`, SameSite=Strict; Bearer không do trình duyệt tự gửi | `TestBuocHaiChongCSRF` |
| Đo thời gian | đoán token, đoán mã | token tra theo sha256; so mã hằng thời gian, thử đủ 3 bước | review |
| Làm máy hết RAM | spam bước hai | bước hai chỉ có HMAC và sha256 (không argon2) | |
| Root trên máy | mọi thứ | ngoài mô hình: root đặt lại được (đúng ý đặc tả, có nhật ký) | e2e T30 |
| Người đặt tên panel | lộ tên panel | Certificate Transparency công khai mọi tên được cấp chứng chỉ; giao diện nói rõ, khuyên dùng tên khó đoán hoặc chế độ riêng tư | |

**Panel chạy bằng root đụng vào thứ do site kiểm soát.** Luồng chứng chỉ panel chỉ đọc, ghi dưới `/var/lib/tpan/acme` (root 0755, user của site không ghi được) và `/etc/letsencrypt` (root). Panel đọc `/etc/letsencrypt/live/tpan-panel/*.pem` là symlink do certbot tạo, nằm trong thư mục root 0700: user của site không đặt được symlink ở đó. Site không thể giành tên panel: tạo site tên trùng `panel.domain` bị từ chối; đặt `panel.domain` trùng site bị từ chối; lineage tên `tpan-panel` không bao giờ trùng lineage của site (tên miền của site luôn có dấu chấm), nên xoá hẳn site không xoá chứng chỉ panel. Không có đường đua thời điểm nào đáng kể: điều kiện đầu đọc trước khoá toàn cục, nhưng hai thao tác (đặt tên panel, tạo site) cùng lúc với đúng một tên chỉ làm hai bên cùng tồn tại, mà nhờ lineage riêng thì không bên nào hỏng.

---

## 6. Nâng cấp và lùi

- **Từ v0.2.0 lên**: migration 2000 chỉ thêm bảng và một cột có DEFAULT. Không ai có hai bước, chính sách tắt, không có token, không có `panel.domain`: đăng nhập y như cũ (`POST /login` vẫn trả 204), cookie cũ dùng tiếp, `tpan admin passwd` y như cũ. File khoá `/etc/tpan/secret.key` chỉ được tạo lần đầu có người bật hai bước. Thứ duy nhất khác: health có cảnh báo vàng "admin chưa bật hai bước" và chứng chỉ của panel giữ nguyên.
- **Lùi về v0.2.0**: không lùi được sau khi đã nâng (v0.2.0 từ chối mở DB có migration 2000, như mọi mốc từ M1). `tpan self-update --rollback` và nút "Quay về bản trước" đã chặn sẵn (`self_rollback_db`, M15). Đường lùi là bản sao `tpan.db` từ trước khi nâng; khi đó hai bước và token biến mất cùng DB (đúng kỳ vọng). Nếu cố chép đè file chạy cũ: DB không mở, panel không chạy, không có trạng thái nào mà bản cũ bỏ qua hai bước.
- **Mất file khoá**: user có hai bước dùng mã dự phòng hoặc root reset; health báo lỗi `tpan.secretkey`. Không tự sinh khoá mới đè lên khi đã có bí mật trong DB (bí mật cũ vẫn không đọc được, sinh khoá mới chỉ che lỗi).

---

## 7. Kế hoạch kiểm thử

### 7.1 Unit (Mac, `make check`)

- `internal/mfa`: vector RFC 6238 phụ lục B (8 chữ số, sáu mốc), mã 6 số có số 0 đứng đầu, `Match` ở bước −2, −1, 0, +1, +2, `TestTOTPLechDongHo` (in bảng tỉ lệ), URI đúng Key URI Format, QR trả ma trận vuông có ba ô định vị, mã dự phòng đúng dạng, không trùng, chuẩn hoá nhận chữ hoa và dấu cách, Seal/Open (đúng, sai user, sửa một byte, khoá khác).
- `internal/apitoken`: dạng, độ dài, đổi một ký tự bất kỳ thì `WellFormed` sai, phạm vi (parse, trùng, lạ, ghi bao gồm đọc).
- `internal/store`: mọi hàm 3.3, đua `UseTOTPStep` và `UseRecoveryCode` và `ConsumeChallenge` (20 goroutine, đúng một thắng), cascade khi xoá user (B7), không có chuỗi bí mật trong DB (B3), nâng cấp từ DB bản 1400 (B6).
- `internal/server`: luồng đăng nhập (không hai bước 204; có hai bước 200 + phiếu; bước hai đúng, sai, dùng lại, cũ, dự phòng một lần, phiếu hết hạn, sai IP, quá 5 lần, khoá user, file khoá mất), B1, B5, B8, B10, `TestPhanQuyenMoiRoute` mở rộng theo năm trạng thái (phiên; token đủ phạm vi; token thiếu phạm vi; token hết hạn; token thu hồi) cho **mọi** route, chính sách, tạo token (vai, ngày, mật khẩu), `GetCertificate` theo SNI và nạp lại khi file đổi.
- `internal/core`: Plan `panel.domain` (điều kiện đầu, bước, CertName), `PrepareAdd` từ chối tên panel.
- `internal/checks`, `internal/health`: bốn phép kiểm mới.
- `cmd/tpan`: `user 2fa status|reset`, `token create|list|revoke`, `panel domain show|set` (dry-run, JSON, audit).

### 7.2 E2E bước 23 trên `tpan-m20` (`scripts/e2e-m20.sh`, log `docs/bang-chung/M20.log`)

Từ máy sạch, bản của nhánh. Mỗi dòng là một khẳng định có kết quả mong đợi.

- T1 cài stack nginx, `tpan setup`, tạo user `khach` (vai user), một site; admin đăng nhập bằng API trả 204 (chưa ai bật hai bước).
- T2 `GET /account/2fa`: `enabled=false`; health có `tpan.2fa:admins` cảnh báo.
- T3 `oathtool` (gói Ubuntu) với cùng bí mật và cùng giờ cho cùng mã như máy chủ; lệch `--now` ±60 giây bị máy chủ từ chối, ±15 giây được nhận.
- T4 QR: ma trận từ `setup` vẽ ra PNG, `zbarimg` đọc ra đúng URI `otpauth://`.
- T5 (trình duyệt thật, `go run ./scripts/anh-readme -bo m20-that`): admin vào `/account`, bấm bật, đọc chuỗi bí mật trên trang, tính mã, nhập mã và mật khẩu, thấy 10 mã dự phòng; đăng xuất; đăng nhập qua form: hiện chặng hai; mã sai → lỗi; mã đúng → vào Bảng điều khiển.
- T10 mã sai 401 `mfa_code_wrong`; T11 mã của bước −2 (cũ) bị từ chối; T12 mã vừa dùng bị từ chối `mfa_code_used`; T13 mã dự phòng dùng được, `recovery_left=9`; T14 cùng mã dự phòng lần hai bị từ chối.
- T15 mang cookie phiếu gọi `GET /sites`, `GET /overview`, `POST /sites`, `GET /account/tokens` → 401 (hoặc 403 `csrf` khi thiếu Origin cho POST từ trình duyệt; script gửi đúng Origin để thấy 401).
- T16 phiếu quá 5 lần sai bị xoá; T17 sai đủ 5 lần ở bước hai thì IP bị khoá 423 (dùng IP thứ hai của máy ảo để không tự khoá mình).
- T20 admin đặt chính sách `all`: `khach` (chưa bật) đăng nhập xong `GET /sites` → 403 `mfa_enroll_required`, `GET /account/2fa` → 200; trình duyệt mở `/sites` bị đưa tới `/bat-hai-buoc`; `khach` bật xong thì vào được.
- T21 bật `all` khi admin chưa bật hai bước → 409 `mfa_enroll_self_first` (chạy trước T5 trên một admin thứ hai).
- T30 `tpan user 2fa reset khach --yes` trên máy: `khach` đăng nhập lại bằng mật khẩu (chính sách đã về `off`) → 204; `audit` có dòng `user.2fa_reset` ip `cli`.
- T40 token: bảng route × {đủ phạm vi, thiếu phạm vi, hết hạn (ghi thẳng `expires_at` vào DB), thu hồi, không có} cho một route của mỗi nhóm và mọi route chỉ phiên; kết quả đúng bảng 3.7.
- T41 hạ `admin2` xuống vai user: token `system:read` của họ gọi `GET /stack` → 403; khoá `admin2` → 401.
- T42 token dạng sai một ký tự → 401 `token_malformed`.
- T50 chứng chỉ panel: pebble, `/etc/hosts` có `panel.m20.test`; trước khi đặt tên: `openssl s_client -servername panel.m20.test` thấy chứng chỉ tự ký; `tpan panel domain set panel.m20.test --yes` thành công; s_client thấy chứng chỉ do pebble ký, SAN đúng; vào bằng IP vẫn là tự ký; health `tpan.panelcert:panel.m20.test` đạt và nói số ngày; `certbot renew --cert-name tpan-panel --force-renewal` → serial mới được phục vụ, `MainPID` của `tpan.service` không đổi; đặt tên trùng site → `panel_domain_is_site`; tạo site tên `panel.m20.test` → `domain_is_panel`.
- T60–T66 quét bí mật: bí mật TOTP (base32 và hex), 20 mã dự phòng, mọi token đã tạo **không** có trong: `tpan.db` + `-wal`, `journalctl -u tpan`, `/var/log/`, sự kiện và bằng chứng của mọi job (`GET /jobs/{id}`), mọi câu trả lời API đã ghi trong e2e trừ đúng câu trả lời lần đầu, output công cụ MCP (`list_sites`, `get_job`, `list_jobs` qua client thử của M7), bản sao `tpan.db` do `tpan backup create --all` tạo.
- T80–T85 nâng từ v0.2.0: cài gói v0.2.0, `setup`, đăng nhập lấy cookie, tạo site; lên bản nhánh bằng `tpan self-update` của v0.2.0; cookie cũ còn dùng; đăng nhập bằng mật khẩu cũ 204; `tpan admin passwd` chạy; site 200; không có `/etc/tpan/secret.key`; health có cảnh báo vàng và không có lỗi mới.

### 7.3 Hồi quy

E2E M6, M13, M15, M16 chạy lại trên bản cuối, mỗi kịch bản từ máy sạch, trên `tpan-m20`; log `docs/bang-chung/M20-hoi-quy-m<n>.log`.

### 7.4 Ảnh

`make anh-m20`: màn Đăng nhập chặng hai, trang bật hai bước, trang Tài khoản (bật, mã dự phòng, token, hộp tạo token, token vừa tạo), thẻ mới ở Cài đặt, trang Công cụ; sáng, tối, 390 px; vào `docs/bang-chung/M20/`.

---

## 8. Bảng chia việc

Mỗi đợt tối đa 4 thợ, mỗi thợ một vùng file (không hai thợ chung file). Loại theo luật 8: **N** = nhạy cảm (`opus`, effort high), **T** = thường (`sonnet`). Không dùng `fable`.

| Đợt | Phần | Vùng file | Thợ | Loại |
|---|---|---|---|---|
| 0 | Nghiên cứu, thử trên máy ảo, thiết kế, hợp đồng (kiểu API Go và TS, chữ ký gói mới, `go.mod`) | file này, `internal/server/types_m20.go`, `web/src/api/m20-types.ts`, `go.mod`, `go.sum` | phiên chính | |
| 1 | `internal/mfa`, `internal/apitoken` (thân hàm + test) | `internal/mfa/`, `internal/apitoken/` | tho-loi | N |
| 1 | Store: migration 2000, `m20.go`, `AuditEntry.TokenID`, test | `internal/store/migrations/2000_*.sql`, `internal/store/m20*.go`, `internal/store/audit.go` | tho-loi | N |
| 1 | Chứng chỉ panel: `CertbotCert.CertName`, `CertbotDelete.CertName`, `core/panel_m20.go`, `RefusePanelDomain` trong `PrepareAdd`, bốn phép kiểm, `health.selfChecks`, `server/tls_m20.go` + `server.Run`, setup 825 ngày | `internal/steps/ssl.go`, `internal/core/panel_m20*.go`, `internal/core/site.go` (một dòng), `internal/checks/m20*.go`, `internal/health/collect.go`, `internal/server/tls_m20*.go`, `internal/server/server.go`, `cmd/tpan/setup.go` | tho-loi | N |
| 1 | Giao diện theo hợp đồng TS (chạy với demo ở đợt 2): Đăng nhập chặng hai, `/bat-hai-buoc`, `/account`, thẻ Cài đặt, ô Công cụ, Icon, QR | `web/src/` (màn mới, `dang-nhap`, `cai-dat`, `cong-cu/tools.ts`, `routes.tsx`, `components/Icon.tsx`, `i18n/*`, `styles.css`, `main.tsx` cho chuyển hướng) | tho-giao-dien | T |
| 2 | Server: luồng đăng nhập, phiếu, chính sách, token trong guard, route mới, bảng nhóm token, test phân quyền mở rộng | `internal/server/auth.go`, `internal/server/routes.go`, `internal/server/auth_m20*.go`, `internal/server/api_m20*.go`, `internal/server/token_m20*.go`, `internal/server/authz_test.go`, `internal/server/types.go` (SessionInfo) | tho-loi | N |
| 2 | CLI: `user 2fa`, `token`, `panel domain`; `admin passwd`, `user passwd` xoá phiếu | `cmd/tpan/m20*.go`, `cmd/tpan/admin.go`, `cmd/tpan/user.go` | tho-phu | T |
| 2 | Demo: dữ liệu M20 | `internal/demo/m20*.go`, `internal/demo/demo.go` (một dòng) | tho-phu | T |
| 2 | Script e2e bước 23 và bộ ảnh M20 (chưa chạy) | `scripts/e2e-m20.sh`, `scripts/e2e-m20/`, `scripts/anh-readme/m20*.go`, `scripts/anh-readme/main.go` (đăng ký `-bo`), `Makefile` (target) | tho-may-ao | N (chạy root trên máy ảo) |
| 3 | Chạy e2e bước 23, hồi quy M6, M13, M15, M16, ảnh; sửa script; báo lỗi mã cho phiên chính | máy ảo `tpan-m20`, `docs/bang-chung/` | tho-may-ao | N |
| 4 | Soát cuối: thợ chưa đọc mã mốc này, thử phá trên máy ảo: vượt bước hai, nâng quyền bằng token, lộ bí mật | chỉ đọc + máy ảo | opus effort max | |
| 5 | Giải thích `docs/giai-thich/20-m20-hai-buoc-va-token.md` | `docs/giai-thich/` | tho-viet | T |

Phiên chính sau mỗi đợt: đọc diff (nhất là của thợ `sonnet`), `make check`, commit mỗi gói một commit, `git merge origin/dev` nếu `dev` có mới, `git push origin moc/M20`.

---

## 9. Quyết định tự chốt (không hỏi được ai)

1. Lineage `tpan-panel` riêng **và** từ chối trùng tên ở cả hai chiều (mục 2.5).
2. Chọn chứng chỉ theo SNI thay vì chép chứng chỉ ACME đè `/etc/tpan/tls/cert.pem` (mục 2.6): không đổi file mà `managed_files` và phép kiểm quyền đang theo dõi, vào bằng IP vẫn chạy, gia hạn không cần restart.
3. Mã dự phòng 80 bit, sha256 trần (mục 3.1).
4. Khoá bước hai theo user 10 lần / 15 phút, ngoài khoá theo IP của M13 (mục 3.4).
5. Bật, tắt hai bước, tạo lại mã, tạo token đều đòi mật khẩu (mục 5).
6. `/users`, `/packages`, `/plans`, `/ai`, `/settings/auth`, `/tokens` là chỉ phiên (mục 3.7).
7. Token của người thuộc diện bắt buộc mà chưa bật hai bước bị chặn như phiên (mục 3.6).
8. Thêm phép kiểm `tpan.clock` và `tpan.secretkey` (đặc tả chỉ nói hai phép kiểm; hai cái này giữ cho hai bước không hỏng lặng lẽ).
9. `rsc.io/qr` là thư viện mới duy nhất (mục 2.2).

---

## 10. Kết quả soát thiết kế

Thợ soát (opus, effort max), 21/11/2026. Đã đọc: file này, `agent-dev/TU-CHAY-M20.md`, mã ở `moc/M20` (20edc7f, gồm hợp đồng đợt 0 `internal/server/types_m20.go`, `internal/store/m20.go`), `docs/bang-chung/M20-nghien-cuu/thu-vm.log`. Kết quả: 3 NẶNG, 6 VỪA, 4 NHẸ (NHẸ gộp theo chủ đề). Mọi con số xác suất dưới đây là ước lượng trên giấy, chưa đo.

**S1. NẶNG. Mục 3.4 (sơ đồ, quy tắc 3, 4), 3.3 (`CreateChallenge`, `RecordTOTPFailure`, `UseTOTPStep`), 5 (hàng "dò mã 6 số", "nhiều IP", "Làm máy hết RAM"). Đoán mã song song vượt mọi trần chống dò.**

- Chỗ sai: mọi trần ở bước hai đều là "kiểm trước, ghi sau". Request đọc "user không đang bị khoá bước hai", `authLocked`, phiếu, rồi mới chạy `mfa.Match`; chỉ khi sai mới `BumpChallenge`, `RecordTOTPFailure`, `authFailed` (sơ đồ, dòng 270–280). Không có gì giới hạn số phiếu đang sống của một user (`CreateChallenge`, dòng 234). Dòng 288 "tốn argon2 cho kẻ tấn công" sai: argon2 chạy ở máy chủ, người có mật khẩu chỉ tốn một request cho mỗi phiếu. Bước hai rẻ (dòng 479), panel nói HTTP/2 (thu-vm.log:388), `http.Server` không đặt trần nào (server.go:67–71). `SetMaxOpenConns(1)` (store.go:109) chỉ xếp hàng từng câu SQL, không xếp hàng cả handler, nên câu đọc kiểm của hàng nghìn request xen vào trước câu ghi lần sai đầu tiên. Thêm: `UseTOTPStep` đặt `locked_until=NULL` (dòng 221–222), nên một mã trúng lọt vào giữa đợt bắn vừa thắng vừa mở khoá. Lớp khoá theo IP không cứu được: phiếu gắn IP nhưng IPv6 cho vô số địa chỉ, và lớp đó có lỗi đua riêng (M28 P1). Bảo đảm của M20 không được dựa vào lớp IP.
- Kịch bản: kẻ có mật khẩu của admin (đã bật hai bước) gửi `POST /login` liên tục trong 5 phút, mỗi lần được một phiếu (vài trăm tới vài nghìn phiếu, tuỳ tốc độ argon2 của máy và bản vá P2), rồi bắn cùng lúc 5 mã cho mỗi phiếu. Mọi request qua kiểm "chưa khoá" trước khi bộ đếm của user kịp tới 10: vài nghìn tới vài chục nghìn lần đoán một đợt, mỗi đợt trúng cỡ 1–10% (3 mã đúng trên 10^6). Hết khoá 15 phút thì bắn tiếp; trong một tới vài ngày gần như chắc trúng → phiên admin → root.
- Sửa: (a) giành lượt trước khi so mã, trong một câu `UPDATE` có điều kiện trên `user_totp` (chỉ tăng khi chưa khoá và chưa đủ 10; lượt thứ 10 đặt `locked_until`; khoá hết hạn thì đếm lại), đọc kết quả bằng `RETURNING` hoặc `RowsAffected`; không giành được thì 423 `mfa_locked` mà không chạy `Match`; đúng mã mới đặt lại bộ đếm. Phiếu cũng vậy: `UPDATE login_challenges SET attempts = attempts + 1 WHERE token_sha256 = ? AND attempts < 5 AND expires_at > ?`. Cách khác tương đương: một `sync.Mutex` theo user bao trọn kiểm, so, ghi trong `tpan serve` (tiến trình duy nhất xử lý bước hai). (b) `RecordTOTPFailure` không được viết theo mẫu `RecordLoginFailure` (ở đó lần sai ghi lúc đang khoá thì đếm lại từ 1 và xoá khoá, store/auth.go:435–449). (c) Mã sai ở `/account/2fa/disable`, `/recovery` (dòng 312) vào cùng bộ đếm user. (d) Mã dự phòng (80 bit, không dò được) đi qua khoá user, để người có mật khẩu không nhốt chủ tài khoản mãi (quy tắc 3, dòng 287, chỉ xét người lạ). (e) Tuỳ chọn: mỗi user một phiếu sống (phiếu mới xoá phiếu cũ); đổi lại, kẻ có mật khẩu đăng nhập liên tục sẽ làm phiếu của chủ mất hiệu lực. Test có tên: 2000 request song song trên 400 phiếu của một user, `Match` chạy không quá 10 lần.

**S2. NẶNG. Mục 2.5, 2.6, 3.8 ("Đổi sang tên khác", "Xoá tên", `RefusePanelDomain`), 5 (đoạn cuối, dòng 483), 9.1. Site và panel cùng tên máy làm lộ cookie phiên admin cho chủ site.**

- Chỗ sai: mục 5 chỉ xét hại ở lineage ("nhờ lineage riêng thì không bên nào hỏng"). Hại thật là cookie: cookie không tách theo cổng (RFC 6265 mục 8.5); `__Host-tpan` gắn với tên máy, không với cổng; cùng tên máy là cùng site nên `SameSite=Strict` không chặn. Khi một site và panel cùng tên X, trình duyệt gửi cookie phiên panel (đặt bởi `https://X:8443`) tới `https://X/` cổng 443, nơi PHP của chủ site (user thường) đọc được. Phiên không gắn IP (`authCurrent`, auth.go:149–180), nên phát lại được ngay.
- Các đường để hai tên cùng tồn tại mà thiết kế để hở:
  1. Đổi hoặc xoá tên panel (dòng 396, 398): tên cũ hết được giữ chỗ; user thường tạo site X (DNS vẫn trỏ về máy) và xin SSL (luật owner:site). Trình duyệt admin còn cookie của X, và panel vẫn trả lời ở X bằng chứng chỉ tự ký (2.6), nên admin quen tay vẫn đăng nhập qua tên cũ.
  2. Đua giữa hai tiến trình: `claim()` chỉ chặn trong một tiến trình (api_job.go:269–273). `tpan panel domain set X` ở CLI đọc bảng sites khi job tạo site X của user đang chạy (site chỉ được ghi lúc job xong, core/site.go:647), rồi chờ flock không hạn và chạy sau. Chiều ngược lại: `after` ghi `panel.domain` sau khi engine đã nhả khoá (core/types_m12.go:245–252).
  3. Job `panel.domain` hỏng ở bước kiểm chứng sau khi certbot đã cấp (Undo giữ chứng chỉ): file lineage còn, `panel.domain` không được ghi; GetCertificate chọn theo file (2.6) nên vẫn phục vụ chứng chỉ tin được cho X, admin dùng X, mà X không được giữ chỗ.
- Kịch bản: admin đổi tên panel từ `panel.example.com` sang `admin.example.com`; user `khach` tạo site `panel.example.com`, bật SSL, ghi `$_COOKIE` ra file; admin gõ `panel.example.com` không kèm cổng (hoặc bấm liên kết từ một trang cùng site) → PHP của `khach` nhận `__Host-tpan` → gửi tới `:8443` → phiên admin → root.
- Sửa: (1) trong `secure`: Host (bỏ cổng) trùng tên một site ở mọi trạng thái thì trả 421, không đặt và không nhận cookie; chặt hơn là chỉ nhận Host thuộc {`panel.domain`, địa chỉ IP, `localhost`, hostname của máy}. (2) `after` của `PrepareDomain` và `PrepareClear` xoá mọi phiên. (3) Bước đầu của Plan `panel.domain` và Plan `site.add` kiểm lại trùng tên dưới flock; `after` kiểm lại trước khi ghi setting. (4) GetCertificate chỉ trả chứng chỉ ACME khi SNI bằng `panel.domain` (đệm theo setting), để B9 đúng theo setting chứ không theo file. Sửa câu ở mục 5 và quyết định 9.1.

**S3. NẶNG. Mục 3.7 (bảng nhóm: `jobs` gồm `GET /jobs/{id}`, dòng 352), B4, B10. Token chỉ đọc lấy được mật khẩu hiện một lần.**

- Chỗ sai: `GET /jobs/{id}` không chỉ đọc. Job đã xong và danh tính không ở chế độ "xem như" thì handler lấy rồi xoá mật khẩu hiện một lần của job: mật khẩu quản trị WordPress, `Secrets` của `POST /apply`, `DBSecret` (api.go:334–336). Danh tính dựng từ token không có `ViewedBy` (dòng 338), nên `ReadOnly()` sai (account/types.go:112) và `takeSecret` chạy.
- Kịch bản: admin cấp token `jobs:read,health:read` cho một dashboard; token lộ qua log CI. Kẻ giữ token gọi `GET /jobs?limit=5` rồi `GET /jobs/{id}` mỗi giây: mỗi site mới của mọi user cho kẻ đó mật khẩu quản trị WordPress (chạy PHP bằng user của site), mỗi DB rời mới hay vừa đổi mật khẩu cho mật khẩu DB; người thật mở job thì không còn thấy mật khẩu. Phạm vi "đọc" thành quyền ghi trên site và DB.
- Sửa: chỉ giao mật khẩu hiện một lần cho đúng thông tin đăng nhập đã khởi job (phiên của chủ; hoặc đúng token id đã gọi POST, ghi id đó vào `jobRun`/`heldSecret`); token khác, nhất là token chỉ đọc, không bao giờ chạy `takeSecret`. Thêm vào bảng test của B4/B10: `GET /jobs/{id}` bằng token `jobs:read` không có `secret`, `secrets`, `db_secret` và không làm mất chúng.

**S4. VỪA. Mục 3.8 (`Server: SSLConfig.ServerURL()`), 3.10 (`tpan.panelcert`), 7.2 T50. Cài mặc định thì panel nhận chứng chỉ staging mà mọi phép kiểm vẫn đạt.**

- Chỗ sai: `ssl.staging` chưa ghi là BẬT (core/ssl.go:131–139); khi đó `ServerURL()` là staging của Let's Encrypt (core/ssl.go:75–83). Chứng chỉ "(STAGING)…" thì trình duyệt không tin, nên P8 không được giải; `PanelServes` chỉ so serial, `tpan.panelcert` chỉ xét ngày và SAN, nên đều báo đạt. E2E đặt `ssl.server` (pebble) nên không bao giờ đi nhánh này.
- Kịch bản: VPS mới, admin đặt email rồi đặt tên panel → job xanh, health xanh, trình duyệt vẫn cảnh báo → người dùng tiếp tục bấm "vẫn tiếp tục", đúng thói quen P8 muốn bỏ.
- Sửa: khi `ServerURL()` là staging, điều kiện đầu từ chối (hoặc bản xem trước nói rõ và đòi xác nhận); `tpan.panelcert` báo Warn khi issuer là staging; thẻ "Tên miền của panel" nói rõ. Tắt staging rồi thì chạy lại `panel domain set` cùng tên là đủ (`CertbotCert.Apply` tự ép khi server đổi, steps/ssl.go:160).

**S5. VỪA. Mục 3.3 (`EnableTOTP`), 3.5 (`setup`, `enable`, dòng 314). Bật hai bước có thể bật một bí mật khác với bí mật vừa so mã.**

- Chỗ sai: `enable` đọc bí mật chờ, so mã, rồi gọi `EnableTOTP(user, step, …)`; hàm này bật "dòng đang chờ lúc đó" chứ không bật "đúng bí mật vừa so" (chữ ký ở `internal/store/m20.go` không có `secretEnc`). `setup` không đòi mật khẩu và ghi đè bí mật chờ. Nếu bí mật chờ được đọc trước bước kiểm mật khẩu thì khe hở dài cả một lần argon2.
- Kịch bản: kẻ có cookie phiên bị lấy (không cần mật khẩu) gọi `POST /account/2fa/setup` đúng lúc nạn nhân bấm "Xác nhận": dòng chờ thành S2 (kẻ đó vừa nhận S2), nạn nhân bật S2 trong khi app giữ S1 → mã của nạn nhân không bao giờ đúng, còn kẻ đó nắm yếu tố thứ hai.
- Sửa: `EnableTOTP` nhận `secretEnc` đã dùng để so và chỉ `UPDATE … WHERE user_name = ? AND enabled_at IS NULL AND secret_enc = ?`; không khớp thì `ErrTOTPNotPending`. Đọc bí mật chờ sau bước kiểm mật khẩu. Test đua `setup` với `enable`.

**S6. VỪA. Mục 3.1 (đoạn "Vì sao bí mật TOTP phải mã hoá"), 6 (dòng 489, 491), B3. Chưa nói khoá `/etc/tpan/secret.key` được ghi bằng đường nào, và đường mặc định của repo làm lộ nó.**

- Chỗ sai: ghi file trong tpan mặc định là một `steps.File` trong Plan. Đi đường đó thì file vào `managed_files`, nội dung vào bản xem trước (in ra màn hình, ra JSON), và công cụ MCP `managed_file` trả nguyên nội dung của mọi file có trong `managed_files` (tools/catalog.go:183, tools/read.go:232–257). Khoá TLS của panel đã phải né đúng chỗ này bằng `setupKeyFile` (cmd/tpan/setup.go:274–284).
- Kịch bản: thợ viết bước "tạo khoá" bằng `steps.File` → agent gọi `managed_file` với `/etc/tpan/secret.key` → khoá ra tới nhà cung cấp AI; ghép với một bản sao `tpan.db` là có bí mật TOTP của mọi người.
- Sửa: ghi vào thiết kế: khoá sinh bằng `sys` ngoài engine (`O_CREAT|O_EXCL`, 0600, root, để hai lần bật đầu tiên cùng lúc không ghi đè nhau), hoặc qua một Step có `Info().Files` rỗng như `setupKeyFile`; không bao giờ vào `managed_files`, bản xem trước, sự kiện job, bằng chứng của `tpan.secretkey`. Thêm khoá (dạng hex và base64) vào danh sách quét T60–T66.

**S7. VỪA. Mục 3.4 quy tắc 3, 5 (hàng "dò mã 6 số"), 3.10. Khoá bước hai không leo thang và không báo cho ai.**

- Chỗ sai: kể cả khi S1 đã vá, 10 lần mỗi 15 phút là khoảng 960 lần đoán mỗi ngày: cỡ 8% mỗi tháng, khoảng 50% sau 8 tháng (ước lượng, 3×10^-6 mỗi lần). Lần sai bước hai chỉ để lại dòng audit `wrong_password`. Sai mã bước hai lặp lại nghĩa là mật khẩu đã lộ: đó là tin chủ tài khoản cần nhất, mà thiết kế không đưa tới ai.
- Kịch bản: kẻ có mật khẩu đoán đều 10 mã mỗi 15 phút từ nhiều IP trong nhiều tuần; admin không thấy gì ngoài các dòng nhật ký giống hệt sai mật khẩu.
- Sửa: khoá leo thang (gấp đôi mỗi lần khoá liên tiếp, có trần; hoặc sau 3 lần khoá thì chỉ còn mã dự phòng hay `tpan user 2fa reset`); mỗi lần khoá ghi audit riêng (ví dụ `login.2fa_locked`) và có phép kiểm health Warn "có người biết mật khẩu của <user>, hãy đổi mật khẩu".

**S8. VỪA. Mục 3.7 (Tạo token), 3.4 quy tắc 9, 5 (hàng "Chiếm được một phiên"). Tạo token chỉ đòi mật khẩu, và token sống qua mọi bước "đuổi kẻ lạ".**

- Chỗ sai: tắt hai bước đòi mật khẩu và mã, nhưng tạo token (sống tới `auth.token_max_days`, tối đa 365 ngày, dùng ngoài trình duyệt) chỉ đòi mật khẩu. Đổi mật khẩu, tự đổi hay do admin đặt lại, chỉ huỷ phiên (store/auth.go:370–373, store/users.go:138); `tpan user 2fa reset` cũng không đụng token.
- Kịch bản: user đã bật hai bước; kẻ có phiên (máy bỏ quên, cookie bị lấy) và mật khẩu đã lộ (đúng tình huống hai bước sinh ra để chặn) tạo token đủ phạm vi của vai, hạn 90 ngày; chủ phát hiện, đổi mật khẩu, tin là đã đuổi được kẻ đó; token vẫn chạy.
- Sửa: user đã bật hai bước thì `POST /account/tokens` (và `POST /account/password`) đòi thêm mã như `disable`. Admin đặt lại mật khẩu (`POST /users/{name}/password`, `tpan user passwd`, `tpan admin passwd`) và `tpan user 2fa reset` thu hồi mọi token của user (hoặc có cờ, và luôn in danh sách token còn sống); tự đổi mật khẩu thì câu trả lời và trang Tài khoản nhắc số token đang sống.

**S9. VỪA. Mục 3.8 ("Đổi sang tên khác", "Xoá tên", dòng 396, 398), 8 (dòng 548). Giả định sai về `steps.CertbotDelete`: thêm trường `CertName` thôi thì không đủ.**

- Chỗ sai: thiết kế chỉ tả nghĩa của `CertName` cho `CertbotCert` (dòng 391: `--cert-name`, `live/<CertName>/fullchain.pem`, `renewal/<CertName>.conf` trong Check, Apply), còn `CertbotDelete{CertName: "tpan-panel"}` thì không. `CertbotDelete` hôm nay lấy mọi thứ từ `Layout`: `--cert-name` là `Layout.Domain` (steps/ssl.go:336); Check và Apply đều gọi `webCheckLayout(s.Layout)` (steps/ssl.go:358, 374), hàm này lỗi với Layout rỗng (steps/site_web.go:28–30); Check stat `Layout.CertRenewal()` và `Layout.CertDir()`, tức `renewal/<tên miền>.conf` và `live/<tên miền>` (steps/ssl.go:361–369, site/ssl.go:9, 20).
- Kịch bản: (a) viết đúng như dòng 396, 398 (không có Layout) thì bước hỏng ở `webCheckLayout`: đổi tên và xoá tên luôn lỗi. (b) Thêm `Layout: site.LayoutOf(d)` cho qua kiểm thì Check không thấy `live/<tên miền>` (lineage nằm ở `live/tpan-panel`), trả true, engine bỏ qua Apply, `certbot delete --cert-name tpan-panel` không bao giờ chạy. Xoá tên: lineage còn, GetCertificate (chọn theo file) vẫn phục vụ chứng chỉ tin được khi `panel.domain` đã rỗng, trái B9 và mở thêm một đường vào S2. Đổi tên: `certonly --cert-name tpan-panel -d <tên mới>` chạy trên lineage đang mang bộ tên khác, đúng điều dòng 396 nói là tránh.
- Sửa: ghi vào 3.8: khi `CertName` có giá trị, `CertbotDelete` dùng `--cert-name <CertName>`, Check stat `live/<CertName>` và `renewal/<CertName>.conf`, không cần Layout của site (thay `webCheckLayout` bằng kiểm `CertName` theo một mẫu riêng), ID bước là `ssl.delete:<CertName>`. Test: có `live/tpan-panel` mà không có `live/<tên miền>` thì Check trả false.

**S10. NHẸ. Mục 2.4, 3.4 (quy tắc 1, 9, 10), 8. Luồng đăng nhập và phiên: chỗ nhỏ.**

- 2.4 (dòng 62) nói không mã nào đọc phiếu ngoài bước hai và đăng xuất, nhưng quy tắc 10 (dòng 294) cho `GET /session` đọc phiếu. Sửa câu; test B1 thêm `GET /session` với phiếu sai IP hay hết hạn phải ra `mfa_pending:false`.
- `failRecorder` (quy tắc 1): `authFailed` còn đường 500 (`authInternal`, auth.go:273–275) chưa được nói là chép ra; nhật ký ghi `wrong_password` cho lần sai mã (auth.go:282), người đọc nhật ký sẽ hiểu là sai mật khẩu; khi khoá IP (423 `login_locked`) và khoá user (423 `mfa_locked`) cùng xảy ra thì chưa định mã nào thắng.
- Quy tắc 9 đặt việc xoá phiếu ở từng nơi gọi, nhưng `POST /users/{name}/password` đi `api_m13.go` → `core.Accounts.SetPassword` → `store.SetUserPassword` (users.go:129–143), không nằm trong vùng file của thợ nào (mục 8, dòng 547, 550, 551). Sửa một lần ở hàm chung: thêm `DELETE FROM login_challenges WHERE user_name = ?` vào `endSessionsOf` (users.go:150), `changePassword` (store/auth.go:349), `ResetAdminPassword` (store/auth.go:133), và giao các file đó cho thợ store. `tpan admin passwd` cũng nên gỡ `user_totp.locked_until` của admin (hiện chỉ `DELETE FROM login_failures`, store/auth.go:147).
- Phòng thủ chiều sâu cho token: các handler chỉ phiên bỏ qua `ok` của `authSessionOf` (auth.go:460, 500, 522; api_m13.go:451, 461). Với request token, `cur` là Session rỗng, và `SessionsOf(ctx, "")` trả phiên của mọi user (store/auth.go:249). Hôm nay kín nhờ bảng nhóm (B10); thêm một lớp: handler trả 401 khi `!ok`, hoặc guard panic khi route chỉ phiên mà request không có phiên.

**S11. NHẸ. Mục 3.6, 3.7, 6, 9.7. Chính sách bắt buộc và hợp đồng token: chỗ nhỏ.**

- Admin đang "xem như" khi chính sách bắt đầu áp cho họ: `POST /account/2fa/setup` bị `read_only_view` (routes.go:261), còn `POST /session/view-as/exit` không có `allowEnroll`; chỉ còn cách đăng xuất. Cho route thoát cờ `allowEnroll`.
- Câu "không có đường vòng chỉ cần mật khẩu" (dòng 320, quyết định 9.7) không đúng với user chưa bật: kẻ có mật khẩu đăng nhập, tự bật hai bước bằng app của mình, và `EnableTOTP` đăng xuất chủ thật. Đó là bản chất của lần bật đầu; ghi vào mô hình đe doạ, audit `mfa.enable` kèm IP, trang Tài khoản và health cho thấy lần bật mới.
- Không có lối thoát bằng CLI cho `auth.require_2fa`: mất file khoá khi DB còn bí mật thì `setup` không niêm phong được (mục 6, dòng 491, cấm sinh khoá mới), người chưa bật bị nhốt ở `/bat-hai-buoc` mà không bật nổi. T30 (dòng 521) cũng giả định chính sách "đã về off" mà không nói bằng cách nào. Thêm `tpan user 2fa policy off|admin|all` (root, có audit).
- User vai `user` cần `token_max_days` và danh sách nhóm để vẽ hộp tạo token, nhưng hai thứ đó chỉ có ở `GET /settings/auth` (chỉ admin; `AuthSettings` trong types_m20.go). Đưa chúng vào một route `any` (ví dụ câu trả lời của `GET /account/tokens`). `tpan token create` chưa nói có áp trần ngày và luật "vai user không chọn system" không.

**S12. NHẸ. Mục 2.6, 3.8. Chứng chỉ panel: chỗ nhỏ.**

- "Đọc hỏng thì giữ bản cũ" (dòng 77) mà không nói nhớ lần hỏng: mỗi bắt tay có SNI đó lại đọc, parse, ghi log (tốn CPU, ngập log khi bị dội bắt tay). Nhớ (mtime, size) của lần hỏng, log một lần.
- "Đổi sang tên khác" (dòng 396) chỉ nói `CertbotDelete` đứng "trước bước xin", không nói phải sau `ACMEProbe`: đứng trước bước thử thì một tên mới sai DNS cũng làm panel mất chứng chỉ cũ. Đặt sau `ACMEProbe`.
- Kiểm chứng của Plan site có `certbot.timer` và `CertRenewDryRun` (core/ssl.go:334–342); Plan panel thì không, nên "tự gia hạn" của đặc tả chưa được kiểm. `PanelServes` cần `tpan.service` đang chạy: chạy từ CLI lúc dịch vụ dừng thì job hỏng sau khi đã có chứng chỉ, ra đúng đường 3 của S2.
- `tpan setup --private` (panel chỉ nghe 127.0.0.1, setup.go:231–233): vào qua `ssh -L` tới 127.0.0.1 thì không có SNI, nên tên miền panel gần như vô dụng, mà tên vẫn bị công khai qua Certificate Transparency. Điều kiện đầu từ chối hoặc cảnh báo.

**S13. NHẸ. Mục 7.2, B3, "Xong khi" của đặc tả. Kế hoạch kiểm thử.**

- E2E sẽ ghi bí mật vào chính bằng chứng: `oathtool` nhận bí mật ở argv (T3, T5, T10–T13), curl gửi `Authorization: Bearer tpan_…` ở argv (T40–T42), mà log e2e in lệnh đã chạy, nên `docs/bang-chung/M20.log` (được commit) chứa bí mật. Trái B3 ("bằng chứng"), và T60–T66 sẽ tự bắt chính file đó. Truyền bí mật qua file 0600 hoặc stdin (`curl -H @file`), không qua argv, và che trong log.
- "Xong khi" đòi e2e có "bảng route × trạng thái token đạt hết"; T40 (dòng 522) chỉ lấy một route mỗi nhóm. Hoặc e2e duyệt đủ bảng (sinh từ `routes()`), hoặc ghi vào `M20.md` là khác đặc tả và phần đủ bảng nằm ở unit test.
- T15 (dòng 517) "403 csrf khi thiếu Origin" sai: `http.CrossOriginProtection` cho qua request không có cả `Sec-Fetch-Site` lẫn `Origin`; 403 chỉ khi `Origin` lệch Host hoặc `Sec-Fetch-Site` là cross-site.

---

## 11. Sửa sau soát (bản cuối; đè lên mục 3 đến 8 khi khác nhau)

Phiên chính nhận cả 13 phát hiện. Từng cái được sửa như dưới; S1, S2, S3 là điểm nặng, đã đóng bằng thiết kế mới, không còn điểm nặng mở.

### 11.1 (S1, S7) Bước hai: tuần tự theo user, khoá leo thang, báo động

- **Mọi lần kiểm mã của một user đi qua một khoá trong tiến trình** (`sync.Mutex` theo tên user, giữ trong `Server`): kiểm khoá user → `mfa.Match`/`UseTOTPStep` hoặc `UseRecoveryCode` → ghi lần sai (`RecordTOTPFailure`, `BumpChallenge`, `authFailed`) → nhả. Chỉ `tpan serve` kiểm mã (CLI không bao giờ kiểm mã), nên khoá trong tiến trình là đủ và bộ đếm đếm đúng từng lần đoán: không còn "kiểm trước, ghi sau" cho hàng nghìn request song song. Hàm chung: `s.checkSecondFactor(ctx, w, r, user, code, purpose) (ok bool, method string, done bool)`, dùng cho bước hai, `disable`, `recovery`, tạo token, đổi mật khẩu (11.5).
- **Đang bị khoá user**: mã dạng 6 số trả 423 `mfa_locked` (kèm `Retry-After`) **mà không chạy `Match`**; mã dạng dự phòng (80 bit, không dò được) vẫn được kiểm, đúng thì vào và gỡ khoá (người có mật khẩu không nhốt được chủ tài khoản mãi).
- **Khoá leo thang**: 10 lần sai liên tiếp thì khoá; lần khoá thứ n liên tiếp (cột `lock_count`) dài `15 phút × 2^(n−1)`, trần 24 giờ. Đúng mã thì `failures = 0`, `locked_until = NULL`, `lock_count = 0`. `RecordTOTPFailure(ctx, user, now, max, base, cap)` trả `(locked, until, lockCount)`; **không** viết theo mẫu `RecordLoginFailure` (lần sai lúc đang khoá không được đếm lại từ 1 hay gỡ khoá).
- **Mỗi lần khoá ghi một dòng audit riêng** `login.2fa_locked` (đối tượng là tên user, kết quả `locked`). Lần sai mã ghi `login.2fa` kết quả `wrong_code` (không phải `wrong_password`, xem 11.9).
- **Phép kiểm mới `tpan.2fa:attempts`** (StageTPAN, Warn): có user mà `lock_count > 0` thì cảnh báo "Có người nhập sai mã hai bước nhiều lần cho <tên>: mật khẩu của họ có thể đã lộ. Đổi mật khẩu." Hết cảnh báo khi user đó đăng nhập đúng.
- Mã sai ở `disable`, `recovery`, tạo token, đổi mật khẩu vào **cùng** bộ đếm user (và `authFailed` theo IP). Mã sai ở `enable` (bí mật mới, user đang bật) không đếm.
- Test có tên: `TestBuocHaiSongSongKhongVuotTran` (2000 request song song trên 400 phiếu của một user, `Match` chạy không quá 10 lần trước khi khoá), `TestKhoaLeoThang`, `TestMaDuPhongQuaKhoaUser`.

### 11.2 (S2, S12) Cookie và tên máy; chứng chỉ panel theo setting

1. **Phiên và phiếu gắn tên máy.** `sessions.host`, `login_challenges.host` (migration 2000). Tên máy của request: `requestHost(r)` = phần tên của `r.Host` (bỏ cổng, bỏ dấu chấm cuối, chữ thường; IPv6 trong ngoặc giữ nguyên dạng địa chỉ). Tạo phiên (cả `handleLogin` và bước hai) ghi `host`. `authCurrent`: phiên có `host` khác rỗng mà khác `requestHost(r)` → xoá phiên, ghi audit `session.host_mismatch`, coi như chưa đăng nhập. Phiếu: host khác → như không có phiếu. Trình duyệt chỉ gửi cookie host-only về đúng tên đã đặt nó, nên lệch host chỉ xảy ra khi cookie bị chép đi (ví dụ PHP của một site cùng tên máy đọc được rồi phát lại qua IP). Phiên của bản cũ (`host = ''`) được nhận ở mọi host tới khi hết hạn (12 giờ không dùng): nâng cấp không đăng xuất ai.
2. **Panel không phục vụ ở tên của site thuộc user thường.** Trong `secure`: `requestHost(r)` không phải địa chỉ IP và trùng domain của một site (mọi trạng thái) mà chủ của site đó không phải admin → 421 `host_is_site` (API: JSON; đường dẫn giao diện: trang HTML ngắn nói vào bằng IP hoặc tên miền riêng của panel). Đệm (tên → chủ có phải admin) 5 giây. Site của admin thì vẫn phục vụ (máy nâng cấp mà admin quen vào panel bằng tên site của mình không bị khoá ngoài), nhưng `GET /session` trả `host_is_site: true` và màn Đăng nhập hiện cảnh báo vàng: mã PHP của site đó (nếu bị chiếm) đọc được cookie của panel; nên vào bằng IP hoặc đặt tên miền cho panel.
3. **Đổi hoặc xoá `panel.domain`** thì xoá mọi phiên có `host` bằng tên cũ (`DeleteSessionsByHost`): cookie đặt dưới tên cũ chết trước khi tên đó có thể thành tên site.
4. **Kiểm lại trùng tên dưới khoá toàn cục.** Plan `panel.domain` có bước đầu `panelNameFree` (Step "chỉ kiểm": `Check` trả lỗi nếu bảng `sites` có tên đó ở mọi trạng thái, hoặc trên máy đã có `/var/www/<tên>`, user hay vhost của nó; không có gì thì trả `true`, không bao giờ `Apply`). Plan `site.add` (site mới) có bước đầu `siteNotPanel` (đọc `panel.domain` dưới khoá; trùng thì lỗi). Setting `panel.domain` ghi bằng một Step trong job (`panelSetting`, có `Undo` trả giá trị cũ), không ghi ở `after` sau khi khoá đã nhả.
5. **GetCertificate chọn theo setting, không theo file**: chỉ trả chứng chỉ ACME khi SNI (chữ thường) **bằng** `panel.domain` (đọc từ DB, đệm 2 giây; API đổi tên thì xoá đệm ngay) **và** chứng chỉ của lineage có SAN chứa tên đó, còn hạn, khớp khoá. B9 đúng theo setting: xoá tên hay job hỏng (Undo trả setting cũ) thì chứng chỉ trên đĩa không được phục vụ.
6. Nạp hỏng thì nhớ `(mtime, size)` của lần hỏng, ghi log một lần, không đọc lại cho tới khi file đổi.
7. **Kiểm chứng của Plan panel**: `checks.PanelACMECert(d)`; `checks.Service("certbot.timer")`; `CertRenewDryRun` cho lineage `tpan-panel` (thêm tham số tên lineage); `checks.PanelServes(d)` thử lại tới 10 giây (đệm 2 giây của setting), và trả "chưa kiểm được" (không phải lỗi) khi `tpan.service` không active (chạy từ CLI lúc dịch vụ dừng).
8. **Đổi tên**: thứ tự bước là `panelNameFree` → `Dir` → `ACMEProbe` (tên mới) → `CertbotDelete{CertName}` (chỉ khi lineage đang mang bộ tên khác) → `CertbotCert{CertName}` → `panelSetting` → kiểm chứng. Thử đường HTTP trước khi xoá: tên mới sai DNS không làm mất chứng chỉ cũ.
9. **Chế độ riêng tư** (`tpan setup --private`, unit có `--listen 127.0.0.1:`): điều kiện đầu từ chối `panel_private` (tên miền không dùng được qua `ssh -L`, mà vẫn bị công khai qua Certificate Transparency).
10. Test: `TestPhienLechHostBiXoa`, `TestPhienCuKhongHostVanDung`, `TestHostLaSiteCuaUser421`, `TestHostLaSiteCuaAdminCanhBao`, `TestChungChiChiTheoSetting`, `TestDoiTenPanelXoaPhienTenCu`, `TestSiteAddDuoiKhoaTuChoiTenPanel`.

### 11.3 (S3) Mật khẩu hiện một lần của job chỉ cho đúng người đã chạy job

`heldSecret` ghi thêm thông tin đăng nhập đã khởi job: `starterSession` (sha256 token phiên) hoặc `starterToken` (id token). `GET /jobs/{id}`:

- request bằng **token**: chỉ lấy mật khẩu khi id token trùng `starterToken`; không thì không có `secret`, `secrets`, `db_secret` và **không làm mất** chúng.
- request bằng **phiên**: như cũ (phiên không "xem như" lấy được), trừ khi job do một token khởi (`starterToken` khác rỗng): khi đó phiên không lấy (mật khẩu thuộc về công cụ đã gọi).
- Test: `TestTokenChiDocKhongLayMatKhauJob`, `TestTokenTaoSiteLayDuocMatKhauCuaMinh`.

### 11.4 (S4) Không nhận chứng chỉ staging cho panel

`PrepareDomain`: `ssl.server` rỗng và `ssl.staging` đang bật (mặc định) → từ chối `panel_staging` ("chứng chỉ staging của Let's Encrypt không được trình duyệt tin; tắt staging: `sudo tpan ssl settings --staging=false`"). `ssl.server` có giá trị (máy chủ ACME thử, e2e) thì cho qua. `tpan.panelcert` Warn khi issuer có chữ `STAGING` hoặc `Fake LE`. Thẻ "Tên miền của panel" nói rõ.

### 11.5 (S5, S8) Bật đúng bí mật đã so; tạo token và đổi mật khẩu đòi mã; đặt lại mật khẩu thu hồi token

- `EnableTOTP(ctx, user, secretEnc, step, hashes, keepSession, now)`: thêm `secretEnc` (đúng bản đã dùng để so mã); câu `UPDATE … WHERE user_name = ? AND enabled_at IS NULL AND secret_enc = ?`; không khớp → `ErrTOTPNotPending`. Handler đọc bí mật chờ **sau** khi đã kiểm mật khẩu.
- `POST /account/tokens` và `POST /account/password`: user đã bật hai bước thì thân phải có thêm `code` (TOTP hoặc mã dự phòng), qua `checkSecondFactor`. `TokenCreate.Code`, `PasswordChange.Code` (`json:"code,omitempty"`).
- **Admin đặt lại mật khẩu** của user (`POST /users/{name}/password` → `store.SetUserPassword`, `tpan user passwd`, `tpan admin passwd` → `store.ResetAdminPassword`) thì **thu hồi mọi token** của user đó trong cùng transaction. Tự đổi mật khẩu thì giữ token, và câu trả lời 204 thành 200 `{"active_tokens": n}` khi n > 0 để giao diện nhắc. `tpan user 2fa reset` giữ token nhưng in danh sách token còn sống kèm lệnh thu hồi.
- Test: `TestDuaSetupVoiEnable`, `TestTaoTokenCanMaKhiCoHaiBuoc`, `TestDatLaiMatKhauThuHoiToken`.

### 11.6 (S6) File khoá không bao giờ đi qua engine

`/etc/tpan/secret.key` chỉ được tạo trong `tpan serve` (tiến trình duy nhất niêm phong bí mật), lần đầu có người gọi `setup`, dưới một `sync.Mutex`: `sys.Stat` → chưa có thì `sys.WriteFileAtomic(path, key, 0o600, system.Root)`; có rồi thì đọc, đòi đúng 32 byte, file thường (không symlink), chủ root, quyền 0600 (sai thì lỗi `mfa_secret_unavailable`, không sinh khoá mới). **Không** qua `steps.File`, không vào `managed_files`, bản xem trước, sự kiện job; bằng chứng của `tpan.secretkey` chỉ có quyền, chủ, cỡ file. Quét bí mật T60–T66 có thêm khoá (hex và base64).

### 11.7 (S9) `CertbotDelete.CertName`

`CertbotDelete{CertName string}` (Layout bỏ trống được khi có CertName): kiểm `CertName` khớp `^[a-z0-9-]{1,64}$` thay cho `webCheckLayout`; lệnh `certbot delete --cert-name <CertName> --non-interactive`; `Check` stat `/etc/letsencrypt/live/<CertName>` và `/etc/letsencrypt/renewal/<CertName>.conf`; ID `ssl.delete:<CertName>`. `CertbotCert.CertName` tương tự cho đường dẫn và `--cert-name`, và `Check` còn đòi SAN của fullchain chứa tên miền. Không có CertName thì hành vi cũ, từng byte (test cũ giữ nguyên). Test: `TestCertbotDeleteTheoCertName` (có `live/tpan-panel` mà không có `live/<tên miền>` thì Check trả false).

### 11.8 (S10) Phiếu, failRecorder, phiếu khi đổi mật khẩu, handler chỉ phiên

- `GET /session` đọc phiếu (sửa câu ở 2.4): phiếu hết hạn, sai IP, sai host thì `mfa_pending` là false (test B1 có thêm ca này).
- `failRecorder` chép ra nguyên vẹn mọi mã khác 401 (423, 500…); 401 thì handler viết `mfa_code_wrong`. Khoá IP và khoá user cùng lúc: kiểm `authLocked` (IP, 423 `login_locked`) trước, rồi khoá user (423 `mfa_locked`).
- Xoá phiếu chờ ở **hàm chung của store**: `endSessionsOf` (users.go), `changePassword`, `ResetAdminPassword` (store/auth.go), `SetUserPassword` (users.go) đều xoá `login_challenges` của user trong cùng transaction. `ResetAdminPassword` gỡ thêm `user_totp.failures`, `locked_until`, `lock_count` của admin. Các file đó giao cho thợ store (mục 11.10).
- Handler chỉ phiên kiểm `ok` của `authSessionOf`: `!ok` → 401 (phòng thủ thêm ngoài bảng nhóm token): `handlePasswordChange`, `handleSessions`, `handleSessionDelete` (auth.go), hai chỗ ở `api_m13.go` (451, 461), mọi handler mới của M20.

### 11.9 (S11) Chính sách, lối thoát, hợp đồng token

- `POST /session/view-as/exit` có cờ `allowEnroll`.
- Lần bật đầu tiên của user chưa có hai bước là chỗ người có mật khẩu bật được bằng app của mình (bản chất của lần bật đầu): ghi vào mô hình đe doạ; audit `account.2fa.enable` kèm IP; trang Tài khoản hiện "bật lúc … từ IP …"; bật xong đăng xuất mọi phiên khác nên chủ thật thấy ngay.
- **Lối thoát bằng CLI**: `tpan user 2fa policy [off|admin|all]` (root, audit `auth.require_2fa` ip `cli`; không đối số thì in chính sách đang có). E2E T30 dùng lệnh này.
- `GET /api/v1/account/tokens` trả `MyTokens{tokens, max_days, groups}` (groups: nhóm người gọi được chọn theo vai), không phải mảng trần. `tpan token create` áp cùng luật (vai, trần ngày) qua hàm chung `apitoken.CheckNew(isAdmin bool, scopes Scopes, days, maxDays int) error`.

### 11.10 (S13) Kiểm thử

- E2E không bao giờ đưa bí mật lên argv hay vào log: bí mật TOTP, mã, token đi qua file 0600 trong `tmp-phien`/thư mục tạm của máy ảo hoặc stdin (`curl -H @file`, `oathtool` đọc bí mật từ file bằng `$(<file)` chỉ trong shell của máy ảo, không in lệnh đã thay giá trị); log in `<bí mật>`. T60–T66 quét cả `docs/bang-chung/M20.log`.
- Bảng route × trạng thái token trong e2e: **mọi route** (chương trình Go trong `scripts/e2e-m20/` đọc `server.RouteTable()` — hàm xuất mới trả `[]RouteInfo{Pattern, Rule, TokenGroup}` — rồi thay placeholder như `authz_test.go`) với bốn trạng thái bị chặn ở lớp token (thiếu phạm vi, hết hạn, thu hồi, route chỉ phiên) — các request đó không bao giờ tới handler nên an toàn trên máy thật; trạng thái "đủ phạm vi" chỉ chạy cho route `GET` (và vài POST có `?dry_run=1`), vì POST thật tới handler sẽ đổi máy. Unit test vẫn phủ đủ năm trạng thái cho mọi route.
- T15: script gửi đúng `Origin`/`Sec-Fetch-Site` cùng origin để thấy 401 (không phải 403 csrf).

### 11.11 Bảng chia việc sửa lại (thay mục 8)

| Đợt | Phần | Vùng file | Thợ | Loại |
|---|---|---|---|---|
| 1 | `internal/mfa`, `internal/apitoken` (thân hàm + test; thêm `apitoken.CheckNew`) | `internal/mfa/`, `internal/apitoken/` | tho-loi | N |
| 1 | Store: migration 2000 (có `lock_count`, `login_challenges.host`, `sessions.host`), `m20.go` theo chữ ký mới (11.1, 11.5), `Session.Host` trong `auth.go` của store, xoá phiếu và thu hồi token ở hàm chung (11.8, 11.5), `DeleteSessionsByHost`, `AuditEntry.TokenID`, test | `internal/store/migrations/2000_*.sql`, `internal/store/m20*.go`, `internal/store/auth.go`, `internal/store/users.go`, `internal/store/audit.go` | tho-loi | N |
| 1 | Chứng chỉ panel: `CertbotCert.CertName`, `CertbotDelete.CertName`, `core/panel_m20.go` (Plan, bước `panelNameFree`, `panelSetting`, từ chối staging, private), bước `siteNotPanel` trong Plan site mới, các phép kiểm M20 (`tpan.2fa:admins`, `tpan.2fa:attempts`, `tpan.clock`, `tpan.secretkey`, `tpan.panelcert`, `PanelServes`, `CertRenewDryRun` có tên lineage), `health.selfChecks(ctx)`, setup 825 ngày | `internal/steps/ssl.go`, `internal/core/panel_m20*.go`, `internal/core/site.go`, `internal/checks/m20*.go`, `internal/checks/ssl.go` (chỉ `CertRenewDryRun`), `internal/health/collect.go`, `cmd/tpan/setup.go` | tho-loi | N |
| 1 | Giao diện (chạy trên demo khi đợt 2 xong) | `web/src/` | tho-giao-dien | T |
| 2 | Server: mọi thứ của 11.1–11.3, 11.5, 11.8, 11.9 trong server, TLS theo SNI (`tls_m20.go`, `server.go`), `RouteTable()`, bảng nhóm token, test | `internal/server/` (trừ `types_m20.go` đã có, sửa được nếu cần) | tho-loi | N |
| 2 | CLI: `user 2fa status|reset|policy`, `token create|list|revoke`, `panel domain show|set` | `cmd/tpan/m20*.go` | tho-phu | T |
| 2 | Demo M20 | `internal/demo/m20*.go`, `internal/demo/demo.go` | tho-phu | T |
| 2 | Script e2e bước 23, chương trình `scripts/e2e-m20/`, bộ trình duyệt `-bo m20-that`, `-bo m20` (ảnh), target Makefile (chưa chạy máy ảo) | `scripts/e2e-m20.sh`, `scripts/e2e-m20/`, `scripts/anh-readme/m20*.go`, `scripts/anh-readme/main.go`, `Makefile` | tho-may-ao | N |
| 3–5 | như mục 8 | | | |

### 11.12 Soát lượt hai (thợ opus, 23/11/2026): không có điểm nặng; cách xử lý từng điểm

Đè lên 11.1 đến 11.11 khi khác nhau.

- **R1 (phiên của bản cũ `host=''`)**: lần đầu một phiên `host=''` được dùng thì ghi `host = requestHost(r)` cho nó (từ đó gắn tên máy như phiên mới). Nhánh 421 (dưới): request mang cookie phiên thì xoá phiên đó và gửi cookie hết hạn. Nâng cấp vẫn không đăng xuất ai (B6).
- **R2 (miễn 421 cho site của admin)**: bỏ miễn. Mọi tên trùng domain của một site ở mọi trạng thái, của bất kỳ ai, đều 421 `host_is_site`. Vào bằng IP hoặc `panel.domain` luôn được, nên không ai bị khoá ngoài. Bỏ `SessionInfo.host_is_site` và cảnh báo vàng ở màn Đăng nhập. Trang 421 (HTML với đường dẫn giao diện, JSON với `/api/`) in sẵn cách vào: `https://<IP của máy>:8443` hoặc tên miền của panel.
- **R12**: request có header `Authorization` (token) được miễn 421 (không dùng cookie nên không có gì để lộ).
- **R3**: guard chỉ gắn `authCtxKey` (store.Session) khi request dùng phiên cookie; request bằng token gắn khoá context riêng (`tokenCtxKey`, giữ `store.TokenRow`). Handler chỉ phiên kiểm `cur, ok := authSessionOf(r); !ok || cur.TokenSHA256 == ""` → 401.
- **R4**: file khoá chỉ được tạo khi bảng `user_totp` không có dòng nào; có dòng mà file mất hay hỏng thì `setup` và mọi lần mở bí mật trả 503 `mfa_secret_unavailable`.
- **R5**: cột `user_totp.wrong_since_ok` (mỗi lần sai +1, không reset khi khoá, chỉ đọc-và-về-0 bằng `TakeWrongSinceOK` khi bước hai đúng). `POST /login/2fa` đúng mà số đó > 0 thì 200 `LoginResult{wrong_codes}`; giao diện báo một lần "có N lần nhập sai mã kể từ lần đăng nhập trước, nếu không phải bạn thì đổi mật khẩu". `MFAState` không cần thêm trường. Phép kiểm `tpan.2fa:attempts` tính theo audit `login.2fa_locked` (kết quả `locked`) trong 7 ngày (`store.CountAuditSince`), không theo `lock_count`.
- **R6**: `checkSecondFactor` luôn chạy SAU khi mật khẩu đã đúng (thao tác đòi cả hai); argon2 chạy ngoài khoá theo user; khoá theo user là kênh 1 chỗ chờ được theo `ctx` (`select` với `ctx.Done()`), hết ctx thì 503 không chạy `Match`. Câu 423 `mfa_locked` nói: dùng mã dự phòng (vẫn được nhận) rồi đổi mật khẩu.
- **R7**: đệm cho 421 là cả danh sách domain của bảng `sites` (mọi trạng thái), nạp lại khi cũ hơn 5 giây; không đệm theo Host lạ. `requestHost` chuẩn hoá: bỏ cổng, bỏ một dấu chấm cuối, chữ thường; test `X.`, `X:443`, chữ hoa, `[::1]:8443`.
- **R8**: demo: `tpan serve --demo` gọi `SetUserPassword` cho user mẫu sau `demo.New` (thu hồi token): token mẫu phải tạo SAU bước đó (hàm demo M20 riêng gọi từ root.go sau vòng đặt mật khẩu, hoặc tạo token mẫu cho admin chỉ khi đặt mật khẩu admin bằng `SetAdminPasswordHash` không thu hồi).
- **R9**: bỏ `starterSession`. Nhánh phiên giữ như cũ (phiên không "xem như" lấy được). Chỉ thêm `starterToken`: request bằng token chỉ lấy khi id trùng; request bằng phiên không lấy được mật khẩu của job do token khởi.
- **R10**: `requestHost` rỗng (HTTP/1.0 không Host) thì `POST /login` trả 400 `host_missing`; không bao giờ ghi phiên `host=''` mới.
- **R11**: mục 3.8 "`after` ghi/xoá setting" và mục 5 "không có đường đua đáng kể" đã thay bằng 11.2 ý 4 (bước `panelSetting`, kiểm dưới khoá). `PrepareClear`: `panelSetting` về rỗng TRƯỚC rồi `CertbotDelete`.
