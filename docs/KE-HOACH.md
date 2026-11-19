# tpan: kế hoạch và thiết kế

Tài liệu này là bản đặc tả để viết code. Ai viết code (người hay AI) đều làm theo nó; chỗ nào thực tế khác với tài liệu thì sửa tài liệu trong cùng commit.

Trạng thái: mốc M0 (khung dự án) đã xong ngày 19/11/2026. Các mốc sau chưa bắt đầu.

## 1. tpan là gì

tpan là một panel quản trị máy chủ web, viết lại phần lõi của aaPanel ở quy mô nhỏ:

1. Cài mô hình **LEMP** (Nginx) hoặc **LAMP** (Apache) lên một VPS Ubuntu sạch.
2. Tạo **nhiều site WordPress**, mỗi site một tên miền, trên cùng một VPS.

Hai việc trên là bắt buộc. Mọi thứ khác trong tài liệu này là phần làm thêm, xếp theo mốc, và chỉ làm khi phần lõi đã có bằng chứng chạy đúng.

tpan là **một file chạy duy nhất** (`tpan`) gồm bốn mặt cùng dùng chung một lõi:

- CLI: `tpan stack install`, `tpan site add shop.vn`, ...
- API HTTP: `/api/v1/...`
- Giao diện web (nhúng sẵn trong file chạy), song ngữ Việt và Anh.
- MCP server (`tpan mcp`): để Claude Code, Codex hoặc agent khác dùng tpan qua đúng các thao tác trên.

Phần AI là tuỳ chọn và tắt mặc định. Không bật AI thì mọi chức năng khác vẫn đầy đủ; không có tính năng nào chỉ làm được bằng AI.

### Ngoài phạm vi

Mail server, DNS server, FTP, Docker, nhiều người dùng với phân quyền, quản lý nhiều máy chủ, hệ điều hành ngoài Ubuntu 24.04. Lý do: không nằm trong đề, và mỗi thứ là một mảng lớn.

## 2. Khác gì các panel đang có

Panel kiểu aaPanel là "hộp đen": bấm nút, panel chạy lệnh bằng quyền root, người dùng không thấy nó sắp làm gì và không dễ biết nó đã làm đúng chưa. tpan đi theo hướng "hộp kính":

| Vấn đề ở panel thường gặp | tpan làm gì |
|---|---|
| Bấm là chạy, không biết sẽ đổi gì trên máy | **Xem trước** mọi thao tác: danh sách bước, lệnh sẽ chạy, file sẽ ghi (có diff), lý do của từng bước. Giống `terraform plan`. |
| Lỗi giữa chừng để lại máy ở trạng thái dở dang | Mỗi bước có **hoàn tác**. Bước N lỗi thì các bước N-1..1 được hoàn tác theo thứ tự ngược. Config luôn được kiểm (`nginx -t`, `apachectl configtest`) trước khi reload. |
| "Xong" nhưng không có gì chứng minh | Mỗi thao tác kết thúc bằng bước **kiểm chứng**, lưu lại bằng chứng: mã HTTP, socket, quyền file, ngày hết hạn chứng chỉ. |
| Mọi site chạy PHP chung một user, một site bị chiếm là lộ các site khác | **Cô lập từng site**: user Linux riêng, pool PHP-FPM riêng, DB và user DB riêng (mục 5). |
| Site lỗi thì chỉ thấy 502, phải tự SSH vào lần từng tầng | **Health theo đường đi request**: tpan tự kiểm chính nó và mọi thứ nó tạo ra, theo từng chặng DNS → TLS → web server → pool PHP → WordPress → DB. Lỗi hiện đúng chặng hỏng kèm bằng chứng và ba cách sửa: tự động, sửa tay theo hướng dẫn, hỏi AI (mục 6.7). |
| Sửa tay file config thì panel không biết, lần sau ghi đè | **Phát hiện lệch** là một nhóm phép kiểm của health: so file thật với bản tpan đã ghi, báo đúng file bị sửa. |
| Chọn LAMP hay LEMP là chọn một lần; muốn đổi phải cài lại | **Chuyển LEMP ⇄ LAMP tại chỗ**. Làm được vì cả hai đều chạy PHP qua PHP-FPM, chỉ khác lớp web server đứng trước (mục 6.6). |
| Tạo, xoá từng site một; xoá là mất | **Hàng loạt** (dán danh sách hoặc file `sites.yaml`) và **thùng rác**: xoá là chuyển vào thùng rác, khôi phục được trong 7 ngày. |
| Backup có thể làm đầy đĩa và kéo sập cả máy | **Chặn trước**: ước lượng dung lượng, thiếu chỗ thì từ chối backup. Log của từng site có logrotate, cấu hình logrotate được kiểm bằng `logrotate -d` trước khi ghi. |
| Panel là một tiến trình root mở cổng ra internet | Không ghép chuỗi lệnh shell, mọi đầu vào có kiểu và được kiểm, đăng nhập có giới hạn số lần, có chế độ chỉ nghe ở `127.0.0.1` và vào qua SSH tunnel (mục 11). |
| AI trong panel thường là ô chat được quyền chạy lệnh | AI của tpan **không có tay**: nó chỉ đọc qua các công cụ có giới hạn và chỉ được *đề xuất* một Plan. Người duyệt trên bản xem trước rồi engine mới chạy, có hoàn tác và kiểm chứng như mọi thao tác khác. Dữ liệu được che trước khi gửi cho AI và hiện ra để người dùng xem (mục 8). |

Hai điểm cần tự kiểm trên máy đang cài aaPanel trước khi nói với người khác, vì mới dựa trên tài liệu và diễn đàn:

- aaPanel chạy PHP của mọi site dưới một user: `ps -eo user:20,cmd | grep [p]hp-fpm`.
- aaPanel giới hạn thư mục bằng `open_basedir` trong `.user.ini`: `cat /www/wwwroot/<site>/.user.ini`.

## 3. Công nghệ

| Phần | Chọn | Vì sao | Đã cân nhắc |
|---|---|---|---|
| Ngôn ngữ backend | **Go 1.27** | Ra một file chạy tĩnh, không cần cài runtime trên VPS; build chéo từ Mac sang Linux amd64 và arm64 bằng một lệnh; Docker, Kubernetes, Terraform, 1Panel đều viết bằng Go. | Python (aaPanel dùng): phải mang theo môi trường Python lên máy đích. Bash (HestiaCP dùng): khó test, khó hoàn tác. |
| CLI | `spf13/cobra` | Chuẩn phổ biến của CLI viết bằng Go (kubectl, docker, gh); có sẵn help và gợi ý lệnh. | Gói `flag` chuẩn: đủ dùng cho khung hiện tại, nhưng lệnh con lồng nhau phải tự viết. |
| HTTP | `net/http` chuẩn | Từ Go 1.22 router chuẩn đã hỗ trợ `GET /api/v1/sites/{domain}`. Không cần framework. | chi, gin, echo. |
| Lưu trạng thái | SQLite qua `modernc.org/sqlite` | Một file, không cần dịch vụ DB riêng cho panel. Bản này viết thuần Go nên giữ được `CGO_ENABLED=0` (file chạy tĩnh, build chéo dễ). | `mattn/go-sqlite3`: cần CGO. Dùng luôn MariaDB: panel sẽ chết theo MariaDB. |
| Đẩy tiến độ ra giao diện | Server-Sent Events | Một chiều từ server ra trình duyệt là đủ cho log của job; đơn giản hơn WebSocket. | WebSocket. |
| Frontend | React 19, TypeScript 7, Vite 8 | Phổ biến nhất; build ra file tĩnh rồi nhúng vào file chạy Go bằng `go:embed`. | Vue (1Panel, aaPanel dùng), Svelte, HTMX. |
| Dữ liệu phía trình duyệt | TanStack Query | Quản lý gọi API, cache, tải lại. | Tự viết bằng `fetch` + state. |
| Giao diện | Tailwind CSS 4 + thành phần tự viết; Radix cho hộp thoại, menu | Kiểm soát được từng chi tiết hình thức; Radix lo phần bàn phím và trợ năng. | Bộ thành phần dựng sẵn: nhanh nhưng ra giao diện giống mọi sản phẩm khác. |
| Chứng chỉ | `certbot certonly --webroot` | certbot chỉ đặt file thử thách, **không sửa config web server**; config do tpan sinh từ template nên phải là một nguồn duy nhất. Cùng một cách cho cả Nginx và Apache. | `certbot --nginx`: tự sửa config. Thư viện ACME nhúng (lego): bớt một gói cài nhưng khó quan sát hơn. |
| WordPress | WP-CLI | Cài, cấu hình, kiểm checksum bằng lệnh, không cần trình duyệt. | Tự tải và tự sinh `wp-config.php`. |
| AI | Giao diện `Provider` với hai bản: Anthropic API và API kiểu OpenAI | Không khoá vào một nhà cung cấp. Bản thứ hai trỏ được vào model chạy tại chỗ, dùng cho nơi không được gửi dữ liệu ra ngoài. Dùng SDK chính thức. | Gắn cứng một nhà cung cấp. Gọi CLI của agent ngay trên máy chủ: phải để thông tin đăng nhập AI trên máy chủ và trao shell cho AI. |
| MCP | SDK Go chính thức `modelcontextprotocol/go-sdk`, transport stdio | Chạy qua SSH (`ssh <máy> tpan mcp`) nên không mở thêm cổng và không cần khoá AI trên máy chủ; xác thực chính là SSH. | Transport HTTP: phải mở cổng và tự lo xác thực. |
| Máy ảo test | Multipass, Ubuntu 24.04 | Giống hệ điều hành của VPS đích; có snapshot để quay về máy sạch trong vài giây. | Docker: không có systemd thật, không giống VPS. |

Phiên bản thư viện: lấy bản stable mới nhất tại lúc thêm vào, ghi trong `go.mod` và `pnpm-lock.yaml`. Không chép số phiên bản từ trí nhớ.

Gói hệ điều hành trên Ubuntu 24.04 (dùng tên gói không kèm số phiên bản để lên 26.04 không phải sửa):

- Chung: `mariadb-server`, `php-fpm`, `php-cli`, `php-mysql`, `php-curl`, `php-gd`, `php-mbstring`, `php-xml`, `php-zip`, `php-intl`, `certbot`, `curl`.
- LEMP thêm `nginx`. LAMP thêm `apache2`.
- **Không bao giờ** cài gói `php` hay `libapache2-mod-php`: chúng kéo theo mod_php và buộc Apache về MPM prefork.

## 4. Kiến trúc

```
 CLI      API HTTP ◄── Giao diện web      AI trong panel     MCP ◄── Claude Code, Codex
  │          │                                 │              │
  │          │                                 └──────┬───────┘
  │          │                                        ▼
  │          │                         tools: danh mục công cụ có kiểu
  │          │                         (loại "đọc" và loại "lập Plan")
  └──────────┴───────────────┬────────────────────────┘
                             ▼
          core: các ca sử dụng (cài stack, tạo site, xoá, backup...)
                             │  mỗi ca sử dụng dựng một Plan = danh sách Step
                             ▼
          engine: chạy Plan ở chế độ xem trước hoặc thực thi,
                  ghi log từng bước, hoàn tác khi lỗi
                             │
              ┌──────────────┼────────────────┐
              ▼              ▼                ▼
           System          store           checks
   ├─ LocalSystem         (SQLite)    phép kiểm dùng chung cho
   └─ FakeSystem                      kiểm chứng, health và e2e
```

`LocalSystem` chạy lệnh thật và ghi file thật. `FakeSystem` chỉ ghi nhớ thao tác trong bộ nhớ, dùng cho unit test và chế độ `--demo`.

### 4.1 Thư mục mã nguồn

```
cmd/tpan/            điểm vào, khai báo lệnh CLI
internal/
  buildinfo/         phiên bản gán lúc build
  core/              ca sử dụng: stack, site, ssl, trash, backup
  engine/            Plan, Step, chạy, hoàn tác, sự kiện
  system/            giao diện System, LocalSystem, FakeSystem
  steps/             các Step cụ thể: apt, service, file, user, mariadb, phpfpm,
                     nginx, apache, wordpress, certbot, ufw
  templates/         template config (nginx, apache, pool, logrotate) + golden test
  store/             SQLite: schema, migration, truy vấn
  server/            HTTP: router, đăng nhập, SSE, phục vụ giao diện
  checks/            các phép kiểm (một bộ dùng cho kiểm chứng, health và e2e)
  health/            chạy phép kiểm định kỳ, sự cố, cách sửa
  monitor/           đọc /proc, trạng thái dịch vụ, log
  tools/             danh mục công cụ có kiểu cho AI và MCP
  ai/                Provider, vòng lặp gọi công cụ, che dữ liệu, phiên hỏi đáp
  mcp/               MCP server (stdio)
  validate/          kiểu Domain, Slug, Email và hàm kiểm
web/                 giao diện React; web/embed.go nhúng web/dist
scripts/vm.sh        quản lý máy ảo test
scripts/faults/      kịch bản gây lỗi có chủ ý trên máy ảo
agents/              hướng dẫn cho agent: skill cho Claude Code, AGENTS.md cho Codex
docs/                tài liệu này và docs/giai-thich/
```

### 4.2 Engine: Step và Plan

Đây là phần quan trọng nhất của dự án. Mọi thay đổi trên máy chủ đều đi qua nó.

```go
// Step là một thay đổi nhỏ, tự kiểm được, hoàn tác được.
type Step interface {
    // Info mô tả bước cho người đọc: tiêu đề, lý do, lệnh sẽ chạy, file sẽ ghi.
    Info() StepInfo
    // Check trả về true nếu máy đã ở trạng thái mong muốn; khi đó bước được bỏ qua.
    Check(ctx context.Context, sys system.System) (done bool, err error)
    // Apply thực hiện thay đổi.
    Apply(ctx context.Context, sys system.System) error
    // Undo đưa máy về trước khi Apply. Chỉ được gọi cho bước đã Apply thành công.
    Undo(ctx context.Context, sys system.System) error
}

type StepInfo struct {
    ID       string   // "nginx.vhost:shop.vn"
    Title    Text     // {vi, en}
    Why      Text     // vì sao cần bước này
    Commands []string // lệnh sẽ chạy, chỉ để hiển thị
    Files    []FileChange // đường dẫn, nội dung cũ, nội dung mới (để vẽ diff)
    Danger   bool     // bước có thể gây gián đoạn dịch vụ
}
```

Quy tắc của engine:

- **Xem trước** (`dry-run`): gọi `Check` và `Info` cho từng bước, không gọi `Apply`. Kết quả là thứ giao diện hiển thị trước khi người dùng bấm xác nhận.
- **Thực thi**: với từng bước, `Check` → đã đúng thì bỏ qua → `Apply` → `Check` lại; sai thì coi là lỗi.
- **Lỗi**: dừng, gọi `Undo` cho các bước đã `Apply` theo thứ tự ngược. Lỗi khi hoàn tác được ghi lại và báo rõ, không nuốt.
- Chạy lại một Plan đã xong thì mọi bước đều được bỏ qua (idempotent). "Sửa chỗ lệch" chính là chạy lại Plan.
- Bước kiểm chứng gọi các phép kiểm trong `internal/checks` (mục 6.7); kết quả lưu thành bằng chứng của job. Health về sau chạy lại đúng những phép kiểm đó, nên "lúc tạo đã đúng" và "bây giờ còn đúng" dùng chung một định nghĩa.
- Mỗi lần chạy là một **job** có id. Sự kiện (bước bắt đầu, dòng output, bước xong, lỗi) được ghi vào SQLite và phát qua SSE.
- Cùng lúc chỉ một job được thay đổi hệ thống (khoá toàn cục). Job hàng loạt tự chia việc bên trong (mục 7).
- Plan có thể được lưu ở trạng thái **chờ duyệt** (bảng `plans`) kèm nguồn gốc: người, AI, hay agent qua MCP. Plan do AI đề xuất luôn dừng ở đây cho tới khi có người duyệt. Ngay trước khi chạy, engine dựng lại Plan trên trạng thái hiện tại của máy và so với bản đã duyệt; khác nhau thì yêu cầu duyệt lại.

### 4.3 System: chỗ duy nhất chạm vào máy

```go
type System interface {
    Run(ctx context.Context, cmd Cmd) (Result, error) // exec với argv, KHÔNG qua shell
    ReadFile(path string) ([]byte, error)
    WriteFileAtomic(path string, data []byte, perm fs.FileMode, owner Owner) error
    Remove(path string) error
    Rename(oldPath, newPath string) error
    Stat(path string) (FileInfo, error)
    // ... MkdirAll, Symlink, Chown, StatFS (dung lượng đĩa)
}
```

- `Run` nhận tên chương trình và mảng tham số, gọi `exec.CommandContext`. Không có hàm nào nhận một chuỗi lệnh shell. Biến môi trường cố định: `LC_ALL=C`, `DEBIAN_FRONTEND=noninteractive`.
- `Cmd` có trường `Stdin` (để đưa mật khẩu và câu SQL vào qua stdin, không để lộ trên dòng lệnh) và `AsUser` (chạy dưới user của site bằng `runuser -u`).
- `WriteFileAtomic` ghi ra file tạm cùng thư mục rồi `rename`, để không bao giờ có file config ghi dở.
- Giá trị bí mật dùng kiểu `Secret`; khi in ra log nó hiện `••••`.
- `FakeSystem` giữ một hệ thống file giả và danh sách lệnh đã "chạy" trong bộ nhớ. Nhờ nó unit test chạy trên Mac không cần root, và `tpan serve --demo` cho phép làm giao diện mà không đụng máy thật.

### 4.4 Trạng thái (SQLite, `/var/lib/tpan/tpan.db`)

| Bảng | Giữ gì |
|---|---|
| `settings` | web server đang dùng, phiên bản PHP, email cho Let's Encrypt, chế độ staging, số ngày giữ thùng rác |
| `sites` | domain, slug, trạng thái (`active`, `trashed`), ssl, thời điểm tạo, thời điểm vào thùng rác |
| `jobs`, `job_events` | lịch sử thao tác và log từng bước |
| `managed_files` | đường dẫn + sha256 của mọi file tpan đã ghi (để phát hiện sửa tay) |
| `backups` | site, đường dẫn file, kích thước, sha256, thời điểm |
| `plans` | Plan chờ duyệt: nội dung, nguồn gốc (người, AI, MCP), ai duyệt, lúc nào |
| `check_results`, `incidents` | kết quả gần nhất của từng phép kiểm trên từng đối tượng; sự cố mở khi một phép kiểm chuyển sang lỗi, đóng khi nó đạt lại |
| `ai_sessions` | phiên hỏi AI: câu hỏi, các lần gọi công cụ, dữ liệu đã gửi (bản đã che), Plan được đề xuất |
| `sessions`, `audit` | phiên đăng nhập (chỉ lưu hash của token), nhật ký ai làm gì lúc nào |

Không lưu mật khẩu DB của site (nó nằm trong `wp-config.php`) và không lưu mật khẩu quản trị WordPress (chỉ hiện một lần lúc tạo).

## 5. Bố trí trên máy chủ

```
/usr/local/bin/tpan                 file chạy
/etc/tpan/config.toml               địa chỉ nghe, chế độ
/etc/tpan/tls/                      chứng chỉ của chính panel
/var/lib/tpan/tpan.db               trạng thái
/var/lib/tpan/acme/                 webroot chung cho thử thách Let's Encrypt
/var/lib/tpan/trash/<id>/           thùng rác
/var/lib/tpan/backups/<domain>/     backup
/var/cache/tpan/                    WordPress và WP-CLI đã tải (kèm checksum)

/var/www/<domain>/public/           mã nguồn site (document root)
/var/www/<domain>/tmp/              upload tạm, session PHP của riêng site
/var/log/tpan/sites/<domain>/       access.log, error.log, php-error.log

/etc/nginx/sites-available/tpan-<domain>.conf    (+ symlink ở sites-enabled)
/etc/apache2/sites-available/tpan-<domain>.conf  (+ symlink ở sites-enabled)
/etc/php/<ver>/fpm/pool.d/tpan-<slug>.conf
/etc/logrotate.d/tpan-sites
```

Mọi file tpan sinh ra đều có tiền tố `tpan-` và dòng đầu `# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.`

### Cô lập từng site

Với site `shop.vn`, `slug` = tối đa 16 ký tự đầu của domain (ký tự ngoài `a-z0-9` đổi thành `_`) + `_` + 6 ký tự hex đầu của sha256(domain). Ví dụ `shop_vn_3fa1c2`. Từ slug suy ra:

| Thứ | Tên | Ghi chú |
|---|---|---|
| User Linux | `tp_<slug>` | không có shell đăng nhập (`/usr/sbin/nologin`), không mật khẩu |
| Thư mục site | `/var/www/shop.vn` | chủ `tp_<slug>`, nhóm `www-data`, thư mục `2750`, file `640` |
| `wp-config.php` | | `600`, chỉ user của site đọc được |
| Pool PHP-FPM | `tpan-<slug>` | chạy dưới `tp_<slug>`, socket `/run/php/tpan-<slug>.sock` (`www-data`, `0660`) |
| DB và user DB | `tp_<slug>` | user `@localhost`, chỉ có quyền trên đúng DB đó |

Kết quả: web server (`www-data`) đọc được file tĩnh qua quyền nhóm; PHP của site A chạy dưới user A, không thuộc nhóm `www-data`, nên không đọc được file của site B. Pool còn đặt `open_basedir` về thư mục site, `upload_tmp_dir` và `session.save_path` về `tmp/` riêng.

Pool dùng `pm = ondemand` (site không có khách thì không tốn RAM), `pm.max_children = 5`, `pm.process_idle_timeout = 10s`, `pm.max_requests = 500`. Trang tổng quan tính RAM xấu nhất = tổng `max_children` × 80 MB và cảnh báo khi vượt RAM dành cho PHP. Con số 80 MB là ước lượng cho WordPress; phải đo lại trên máy thật bằng `ps -o rss`.

## 6. Các luồng chính

Mỗi luồng dưới đây là một Plan. Liệt kê theo đúng thứ tự bước.

### 6.1 Cài stack

Điều kiện đầu: Ubuntu 24.04; cổng 80 và 443 đang trống; chưa có nginx, apache2, mariadb, mysql. Sai điều kiện thì từ chối và nói rõ lý do (giống các panel yêu cầu máy sạch).

1. `apt-get update`.
2. Cài gói chung và gói web server đã chọn (mục 3).
3. MariaDB: kiểm root dùng `unix_socket`, không có user ẩn danh, không có DB `test`, chỉ nghe `127.0.0.1`. Thiếu gì sửa nấy bằng SQL.
4. Dò phiên bản PHP đã cài (đọc `/etc/php/*/fpm`), lưu vào `settings`.
5. Web server:
   - Nginx: gỡ site `default` của distro, ghi `tpan-default.conf`.
   - Apache: `a2dismod mpm_prefork`, bật `mpm_event proxy_fcgi setenvif rewrite ssl headers http2`, gỡ `000-default`, ghi `tpan-default.conf`.
   - `tpan-default` là site hứng mọi request không khớp domain nào: trả 444 (Nginx) hoặc 403 (Apache), chỉ phục vụ `/.well-known/acme-challenge/` từ `/var/lib/tpan/acme/`.
6. Tải WP-CLI vào `/usr/local/bin/wp`, kiểm sha512.
7. Ghi `/etc/logrotate.d/tpan-sites`, kiểm bằng `logrotate -d`.
8. Kiểm chứng: `systemctl is-active` và `is-enabled` cho web server, PHP-FPM, MariaDB; `ss -tlnp` cho thấy 3306 chỉ ở `127.0.0.1`; `curl` vào IP trả đúng mã của site mặc định.

### 6.2 Tạo site WordPress

Đầu vào: domain, tiêu đề, email quản trị, có xin SSL không.

1. Kiểm domain (mục 11), kiểm chưa tồn tại (kể cả trong thùng rác).
2. Tạo user `tp_<slug>`.
3. Tạo thư mục site và thư mục log với quyền ở mục 5.
4. Tạo DB, user DB, mật khẩu ngẫu nhiên 32 ký tự (`crypto/rand`). SQL đưa vào `mariadb` qua stdin.
5. Ghi pool PHP-FPM; `php-fpm<ver> -t`; reload PHP-FPM.
6. Giải nén WordPress từ `/var/cache/tpan/` (tải một lần, kiểm checksum) vào `public/`.
7. `wp config create` (mật khẩu DB qua stdin), thêm `DISALLOW_FILE_EDIT`; `chmod 600 wp-config.php`.
8. Ghi vhost từ template; kiểm config; reload web server.
9. `wp core install` với mật khẩu quản trị ngẫu nhiên (qua stdin); đặt permalink `/%postname%/`.
10. Kiểm chứng:
    - Gọi `http://127.0.0.1/` với header `Host: <domain>` → 200 và có dấu hiệu WordPress.
    - `wp core verify-checksums` → mã nguồn nguyên vẹn.
    - Socket của pool tồn tại, đúng chủ và quyền.
    - Đăng nhập DB bằng user của site được; user đó không thấy DB của site khác.
    - Dưới user của site, đọc `wp-config.php` của một site khác phải bị từ chối.
11. Nếu chọn SSL: chạy luồng 6.3.

Hoàn tác đi ngược: gỡ vhost, gỡ pool, xoá DB và user DB, xoá thư mục, xoá user.

Template vhost của cả hai web server đều có: chặn file ẩn (trừ `.well-known`), chặn chạy PHP trong `wp-content/uploads`, giới hạn upload 64 MB, log riêng từng site. Bản Apache đặt `AllowOverride All` và ghi `.htaccess` chuẩn của WordPress; bản Nginx dùng `try_files $uri $uri/ /index.php?$args`.

### 6.3 SSL

1. Kiểm trước, để không tốn lượt của Let's Encrypt vào lỗi biết trước:
   - DNS: bản ghi A của domain có trỏ về IP public của máy không.
   - HTTP: đặt một file thử vào `/var/lib/tpan/acme/` rồi tự gọi lại qua `http://<domain>/.well-known/acme-challenge/<file>`.
2. `certbot certonly --webroot -w /var/lib/tpan/acme -d <domain> --non-interactive --agree-tos -m <email>`, thêm `--staging` khi `settings.acme_staging` bật.
3. Ghi lại vhost bản có 443 và chuyển hướng 80 → 443; kiểm config; reload.
4. Hook gia hạn: `/etc/letsencrypt/renewal-hooks/deploy/tpan` gọi `tpan internal reload-web` (reload đúng web server đang dùng).
5. Kiểm chứng: `https://<domain>/` trả 200, chứng chỉ khớp tên miền, còn bao nhiêu ngày; `certbot renew --dry-run`.

Giới hạn của Let's Encrypt cần nhớ: 50 chứng chỉ cho mỗi tên miền đăng ký trong 7 ngày (tính chung cho mọi tên miền con và mọi người dùng tên miền đó), 5 chứng chỉ cho cùng một bộ tên trong 7 ngày, 300 đơn mới cho một tài khoản trong 3 giờ. **Trong lúc phát triển luôn bật staging.** Chỉ tắt staging cho buổi demo.

### 6.4 Xoá, thùng rác, khôi phục

Xoá một site là đưa vào thùng rác, không mất dữ liệu:

1. Gỡ symlink vhost, gỡ pool; kiểm config; reload.
2. Khoá user DB (`ALTER USER ... ACCOUNT LOCK`); DB giữ nguyên.
3. Khoá user Linux (`usermod -L`).
4. Chuyển `/var/www/<domain>` vào `/var/lib/tpan/trash/<id>/` bằng `rename` (cùng phân vùng thì xong tức thì; khác phân vùng thì chép rồi xoá).
5. Đánh dấu `trashed`, ghi hạn xoá hẳn (mặc định 7 ngày).

Khôi phục: làm ngược lại rồi chạy bước kiểm chứng của 6.2. Từ chối nếu domain đã được tạo lại.

Xoá hẳn (bấm tay hoặc hết hạn): xoá DB và user DB, xoá file, `userdel`, `certbot delete`. Việc dọn thùng rác hết hạn do một bộ hẹn giờ trong `tpan serve` chạy mỗi giờ; cũng gọi tay được bằng `tpan trash purge --expired`.

### 6.5 Backup và khôi phục

- Backup = một file `.tar.gz` gồm thư mục site, bản dump DB (`mariadb-dump --single-transaction --quick`) và `manifest.json` (domain, phiên bản WordPress và PHP, sha256). Nén bằng thư viện chuẩn của Go, không cần gói ngoài.
- **Chặn đầy đĩa**: trước khi backup, tính kích thước site + DB; chỉ chạy khi chỗ trống ≥ kích thước × 1,2 và sau đó vẫn còn ≥ 10% đĩa.
- Giữ N bản gần nhất cho mỗi site (mặc định 3).
- Khôi phục vào chính site đó: tự backup hiện trạng trước rồi mới ghi đè.
- Tự động backup trước các thao tác rủi ro: xoá hẳn, khôi phục.

### 6.6 Chuyển LEMP ⇄ LAMP

Làm được vì PHP luôn chạy trong PHP-FPM; Nginx và Apache chỉ là lớp nhận request rồi chuyển file `.php` sang socket của pool. Đổi web server không đụng tới pool, DB hay mã nguồn.

1. Cài gói web server mới mà **không cho nó tự khởi động** (đặt tạm `/usr/sbin/policy-rc.d` trả mã 101 trong lúc `apt-get install`, xong thì gỡ).
2. Sinh vhost của mọi site theo template của web server mới; kiểm config.
3. Dừng web server cũ, khởi động web server mới.
4. Chạy kiểm chứng HTTP cho **mọi** site.
5. Có site nào hỏng: dừng cái mới, bật lại cái cũ, báo lỗi (hoàn tác). Tất cả đúng: `disable` cái cũ, cập nhật `settings`.

Thời gian gián đoạn là khoảng giữa bước 3 và lúc web server mới nhận request, cần đo trên máy ảo và ghi con số vào tài liệu giải thích.

### 6.7 Health: tự kiểm và sửa

Một **phép kiểm** (check) là một hàm nhỏ trả về: trạng thái (`đạt`, `cảnh báo`, `lỗi`), bằng chứng (lệnh đã chạy và output), và nếu không đạt thì kèm nguyên nhân có thể và cách sửa. Tất cả nằm trong `internal/checks` và được dùng ở bốn chỗ: kiểm chứng sau mỗi thao tác, health định kỳ, lệnh `tpan health`, và các khẳng định trong e2e.

Phép kiểm của một site xếp theo **đường đi của request**, để nhìn là biết hỏng ở chặng nào:

| Chặng | Kiểm gì | Khi hỏng thì người dùng thấy |
|---|---|---|
| DNS | bản ghi A trỏ về IP của máy | không tới được máy |
| TLS | có chứng chỉ, khớp tên miền, còn hạn (cảnh báo khi dưới 14 ngày) | trình duyệt báo không an toàn |
| Web server | dịch vụ `active`, config hợp lệ, vhost của site đang bật, file không bị sửa tay | lỗi kết nối, hoặc rơi vào site mặc định |
| Pool PHP | file pool có, socket tồn tại và đúng quyền, log không có "max_children reached" | 502 (web server không nói chuyện được với pool) hoặc 504 (pool quá tải) |
| WordPress | `wp core verify-checksums`, quyền file đúng, `wp-config.php` là 600 | lỗi 500, hoặc mã nguồn đã bị sửa |
| DB | MariaDB `active`, user của site đăng nhập được, DB tồn tại | "Error establishing a database connection" |
| Đầu cuối | gọi `http(s)://<domain>/` ngay trên máy → 200 và đúng là WordPress | bất kỳ lỗi nào ở trên |

Phép kiểm chung của máy: đĩa (cảnh báo 80%, lỗi 90%), RAM và swap, ba dịch vụ, cổng đang mở, log có được rotate không, tường lửa.

Phép kiểm của **chính tpan**: `PRAGMA integrity_check` của SQLite, chỗ trống ở `/var/lib/tpan`, hạn chứng chỉ của panel, bộ hẹn giờ còn chạy, có job nào treo, quyền của file config, tuổi bản sao lưu gần nhất của `tpan.db`.

Lịch chạy: ngay sau mỗi job (cho các đối tượng job đó chạm vào); 5 phút một lần cho phép kiểm nhẹ; mỗi giờ cho phép kiểm nặng (checksum, DNS). Chạy tay bằng `tpan health [<domain>] [--json]`.

Khi một phép kiểm chuyển từ đạt sang lỗi, tpan mở một **sự cố**; khi đạt lại thì đóng. Sự cố giữ bằng chứng lúc mở, lúc đóng và những việc đã làm ở giữa.

Mỗi phép kiểm không đạt đưa ra tối đa ba cách sửa:

1. **Sửa tự động**: một Plan dựng từ các Step có sẵn (ví dụ ghi lại vhost từ template, khởi động lại pool). Vẫn qua xem trước, xác nhận và kiểm chứng.
2. **Sửa tay**: các lệnh cụ thể để người vận hành tự gõ, kèm cách kiểm lại. Cách này luôn có, kể cả khi không có cách sửa tự động.
3. **Hỏi AI** (khi đã bật AI): gửi phát hiện và bằng chứng đã che cho AI để chẩn đoán (mục 8).

File bị sửa tay: hiện diff giữa bản đang có và bản tpan sẽ sinh, cho chọn ghi lại bản của tpan hoặc nhận bản sửa tay làm chuẩn mới.

Để kiểm chính hệ thống health, `scripts/faults/` có các kịch bản gây lỗi có chủ ý trên máy ảo: dừng PHP-FPM, xoá socket của một pool, sửa hỏng một vhost, đổi quyền `wp-config.php`, khoá user DB, làm đầy đĩa bằng file giả. Mỗi kịch bản phải làm đúng một chặng chuyển sang lỗi, và cách sửa tự động phải đưa nó về đạt.

### 6.8 Bảo mật máy

Trang chỉ đọc là chính, mỗi mục một dòng trạng thái kèm lệnh đã dùng để kiểm:

- ufw: bật chưa, đang mở cổng nào. Nút bật: mở 22, 80, 443 và cổng panel **trước** rồi mới `ufw --force enable`.
- fail2ban cho sshd: nút cài và bật.
- SSH: root có đăng nhập được không, còn cho mật khẩu không. **Chỉ cảnh báo**, không tự sửa, vì sửa sai là mất đường vào máy.
- Có swap không, có gói bảo mật chờ cập nhật không, cổng panel có mở ra internet không.

## 7. Thao tác hàng loạt

Đầu vào: dán danh sách domain (mỗi dòng một cái) trên giao diện, hoặc file:

```yaml
# sites.yaml
defaults:
  admin_email: admin@example.com
  ssl: false
sites:
  - domain: shop.example.com
    title: Cửa hàng
  - domain: blog.example.com
    ssl: true
```

`tpan apply -f sites.yaml` đưa máy về đúng danh sách trong file: site thiếu thì tạo, site đã có thì bỏ qua. Mặc định không xoá site nằm ngoài file; thêm `--prune` thì các site đó vào thùng rác. `tpan export` in ra file từ trạng thái hiện tại.

Một job hàng loạt chạy theo pha để chỉ reload một lần:

| Pha | Việc | Cách chạy |
|---|---|---|
| A | user, thư mục, DB, file pool, giải nén WordPress, `wp config create` | song song, tối đa 3 site một lúc |
| B | kiểm config PHP-FPM và web server, reload **một lần** | một lần cho cả lô |
| C | `wp core install`, kiểm chứng | song song |
| D | SSL | lần lượt từng site, có kiểm trước |

Site nào lỗi thì hoàn tác riêng site đó, các site khác đi tiếp. Kết quả là bảng từng site: xong, lỗi ở bước nào, vì sao. Xoá và khôi phục hàng loạt theo cùng cách.

## 8. AI và agent

### 8.1 Nguyên tắc

1. **AI không có tay.** AI không chạy lệnh và không ghi file. Nó chỉ gọi được các *công cụ* trong `internal/tools`, gồm hai loại:
   - công cụ **đọc**: danh sách site, kết quả health, số liệu máy, đuôi log, nội dung file do tpan quản lý;
   - công cụ **lập Plan**: tạo site, xoá site, xin SSL, sửa một phát hiện của health. Gọi công cụ loại này chỉ sinh ra một Plan ở trạng thái chờ duyệt.

   Plan chờ duyệt hiện lên đúng màn xem trước của mọi thao tác khác, có nhãn "AI đề xuất". Người bấm duyệt thì engine mới chạy.
2. **Không có công cụ shell.** Việc nào không diễn đạt được bằng các Step có sẵn thì AI chỉ được trả lời bằng chữ: lệnh gợi ý để người tự gõ, ghi rõ "AI đề xuất, chưa được kiểm".
3. **Thấy được AI thấy gì.** Mỗi lần hỏi, giao diện hiện đúng dữ liệu sẽ gửi đi (bản đã che) trước khi gửi, và hiện từng lần AI gọi công cụ cùng kết quả.
4. **Che dữ liệu trước khi gửi.** Mật khẩu, khoá, salt bị loại hẳn. IP, email, tên miền được thay bằng bí danh ổn định trong phiên (`site-a.example`, `203.0.113.7`); tpan giữ bảng đối chiếu tại chỗ và đổi ngược lại khi hiển thị câu trả lời. Mức che chỉnh được; mặc định là che hết.
5. **Thứ AI đọc được là dữ liệu, không phải mệnh lệnh.** Log truy cập chứa chuỗi do người lạ gửi tới (URL, User-Agent), nên kẻ xấu có thể cài vào đó câu lệnh nhắm tới AI. Nghiên cứu năm 2026 cho thấy các biện pháp chống ở tầng prompt chỉ giảm chứ không loại được kiểu tấn công này, nên lớp chặn chính của tpan là nguyên tắc 1 và 2: dù AI bị dẫn dắt, việc tệ nhất nó làm được là đề xuất một Plan mà người sẽ đọc trước khi duyệt. Thêm vào đó, nội dung không tin cậy được bọc trong thẻ đánh dấu, và Plan nào được đề xuất sau khi AI đã đọc log sẽ mang cảnh báo trên màn xem trước.
6. **Tuỳ chọn và có giới hạn.** Tắt mặc định. Có trần số token mỗi ngày, tối đa 8 vòng gọi công cụ mỗi phiên, có thời gian chờ. Mọi phiên được ghi vào nhật ký.

### 8.2 AI trong panel

Một lõi (`internal/ai`), nhiều lối vào:

| Lối vào | Làm gì |
|---|---|
| Thanh lệnh `⌘K` | Gõ tên lệnh thì mở đúng form; gõ một câu thì AI chuyển thành Plan. Ví dụ: "tạo 3 site cho a.example, b.example, c.example, có SSL". |
| "Giải thích" trên mọi lỗi | Bước job lỗi, phép kiểm không đạt, một dòng log: AI giải thích bằng ngôn ngữ đang dùng và dẫn lại đúng bằng chứng. |
| "Chẩn đoán" trên sự cố | AI đọc thêm (log, config, trạng thái dịch vụ) qua công cụ đọc, nêu nguyên nhân khả dĩ nhất, rồi đề xuất Plan sửa, hoặc lệnh sửa tay nếu không có Step phù hợp. |
| Hỏi số liệu | "Site nào nhiều lỗi 5xx nhất trong 1 giờ qua?" AI gọi công cụ thống kê, trả lời kèm con số và lần gọi công cụ đã dùng. |
| Tóm tắt | Tóm tắt các job và sự cố trong một khoảng thời gian. |
| CLI | `tpan ask "<câu>"`: in Plan đề xuất rồi hỏi xác nhận như mọi lệnh khác. |

Cấu hình nằm trong `/etc/tpan/ai.toml` (quyền 600, không vào DB): nhà cung cấp, địa chỉ API, tên model, khoá. Có hai bản `Provider`: Anthropic API và API kiểu OpenAI. Bản thứ hai trỏ được vào model chạy tại chỗ; khi đó không dữ liệu nào rời khỏi máy. Test dùng `FakeProvider` trả lời theo kịch bản soạn sẵn, nên không cần mạng và không cần khoá.

Không làm: cho tpan gọi CLI của một agent (như `claude -p`) ngay trên máy chủ để tự sửa. Cách đó để thông tin đăng nhập AI trên máy chủ và trao cho AI một shell root, đi ngược nguyên tắc 1 và 2.

### 8.3 MCP: để agent bên ngoài dùng tpan

`tpan mcp` chạy một MCP server qua stdio, đưa ra đúng danh mục công cụ ở 8.1. Kết nối đi qua SSH, nên không mở thêm cổng nào và không có khoá AI nào nằm trên máy chủ; AI chạy ở máy của người vận hành.

```bash
# Claude Code, chạy trên máy cá nhân
claude mcp add --transport stdio tpan -- ssh <máy> tpan mcp

# Codex CLI
codex mcp add tpan -- ssh <máy> tpan mcp
```

Công cụ lập Plan trả về bản xem trước và một `plan_id`. Plan đó được chạy theo một trong hai chế độ duyệt, đặt trong config:

- `panel` (mặc định): agent không tự chạy được. Người duyệt trên giao diện web hoặc bằng `tpan plan approve <id>`.
- `client`: tin vào bước hỏi quyền của chính agent; `apply_plan(plan_id)` chạy ngay.

Thư mục `agents/` trong repo có hướng dẫn cho agent: một skill cho Claude Code (`agents/skills/tpan/SKILL.md`) và `AGENTS.md` cho Codex, mô tả các quy trình như "chẩn đoán site trả 502" theo đúng thứ tự chặng ở mục 6.7.

## 9. CLI và API

```
tpan setup                         khởi tạo panel: thư mục, DB, chứng chỉ, mật khẩu, systemd
tpan serve [--listen A] [--demo]   chạy giao diện và API
tpan stack install --web nginx|apache
tpan stack status
tpan stack switch --to nginx|apache
tpan site add <domain> [--title T] [--email E] [--ssl]
tpan site list | info <domain> | rm <domain> | restore <domain>
tpan ssl issue <domain>
tpan apply -f sites.yaml [--prune]
tpan export
tpan trash list | purge [--expired | <domain>]
tpan backup create|list|restore <domain>
tpan health [<domain>] [--fix]
tpan plan list | show <id> | approve <id> | reject <id>
tpan ask "<câu hỏi hoặc yêu cầu>"
tpan mcp                           chạy MCP server qua stdio
tpan version
```

Mọi lệnh có thay đổi đều nhận `--dry-run` (chỉ in kế hoạch) và `--yes` (bỏ qua câu hỏi xác nhận). Thêm `--json` để in kết quả cho script đọc.

API dưới `/api/v1`, JSON. Mỗi lệnh ở trên có một endpoint tương ứng. Endpoint có thay đổi nhận `?dry_run=1` và trả về Plan; chạy thật thì trả `job_id`, tiến độ lấy ở `GET /api/v1/jobs/{id}/events` (SSE). Lỗi có dạng `{"error": {"code": "domain_invalid", "message": {"vi": "...", "en": "..."}}}`.

## 10. Giao diện

Bốn ý định hình giao diện:

1. **Ma trận sức khoẻ là màn chính.** Hàng là site, cột là các chặng của đường đi request (DNS, TLS, web, pool, WordPress, DB). Mỗi ô là kết quả phép kiểm của chặng đó. Lỗi ở tầng dùng chung (ví dụ PHP-FPM chết) hiện thành một **cột**; lỗi của riêng một site hiện thành một **ô**. Nhìn hình dạng vùng lỗi là biết loại sự cố trước khi đọc chữ nào.
2. **Màu chỉ dành cho bất thường.** Khi mọi thứ ổn, màn hình gần như một màu; màu chỉ xuất hiện ở chỗ cần chú ý. Đây là nguyên tắc của giao diện phòng điều khiển công nghiệp (ISA-101). Trạng thái luôn có thêm hình dạng hoặc chữ, không chỉ dựa vào màu.
3. **Thay đổi máy chủ được duyệt như duyệt pull request.** Bản xem trước của một Plan trình bày như một PR: tóm tắt, phạm vi ảnh hưởng (dịch vụ nào reload, bao nhiêu site dùng chung), file thay đổi dạng diff, lệnh sẽ chạy. Lịch sử job đọc như `git log` của cái máy: ai đổi gì, lúc nào, bằng chứng, và nút hoàn tác.
4. **Trạng thái nào cũng mở ra được bằng chứng.** Bấm vào bất kỳ dấu trạng thái nào là thấy lệnh đã chạy và output thật. Giao diện không khẳng định điều gì mà nó không chỉ ra được căn cứ.

Màn hình:

1. **Đăng nhập.**
2. **Tổng quan**: ma trận sức khoẻ; sự cố đang mở; CPU, RAM, đĩa kèm đường 1 giờ gần nhất; dịch vụ; chứng chỉ sắp hết hạn; Plan đang chờ duyệt.
3. **Site**: đường đi request của site đó vẽ theo chiều ngang, mỗi chặng bấm được; bên dưới là bằng chứng, log trực tiếp, backup, thao tác.
4. **Tạo site**: một site hoặc dán danh sách → bản xem trước → chạy.
5. **Duyệt Plan**: màn xem trước dùng chung cho mọi thao tác, kể cả Plan do AI hoặc agent đề xuất (có nhãn nguồn gốc).
6. **Lịch sử**: các job theo thời gian; trang một job có từng bước, output chạy trực tiếp, bằng chứng; khi lỗi thì thấy các bước hoàn tác chạy ngược.
7. **Sự cố**: đang mở và đã đóng; mỗi sự cố có bằng chứng và ba cách sửa.
8. **Thùng rác**, **Stack** (kèm nút chuyển LEMP ⇄ LAMP), **Bảo mật**, **Cài đặt** (AI, MCP, ngôn ngữ).
9. **Thanh lệnh `⌘K`**: mở được từ mọi màn, dùng hoàn toàn bằng bàn phím.
10. **Phiên AI**: câu hỏi, dữ liệu đã gửi (bản đã che), từng lần gọi công cụ, câu trả lời, Plan đề xuất.

Nguyên tắc:

- Thao tác nào cũng đi qua ba chặng: nhập → xem trước → chạy và xem bằng chứng. Không có nút nào thay đổi máy ngay lập tức.
- **Chế độ học**: một công tắc bật phần "vì sao" bên cạnh từng bước, từng dòng config, từng phép kiểm. Người mới bật, người quen tắt.
- Dữ liệu kỹ thuật (đường dẫn, lệnh, domain, mã lỗi) dùng chữ mono, bấm là chép.
- Song ngữ: chuỗi giao diện nằm trong `web/src/i18n/vi.ts` và `en.ts` (cùng một kiểu TypeScript nên thiếu khoá là lỗi biên dịch). Thuật ngữ kỹ thuật giữ nguyên ở cả hai bản: vhost, pool, socket, reload.
- Font tự chứa trong file chạy, không gọi CDN (panel phải chạy được trong mạng kín). Font phải có đủ dấu tiếng Việt.
- Dùng được bằng bàn phím; tương phản đạt WCAG AA; có chế độ sáng và tối.

Hướng hình thức chốt sau khi chọn bản mẫu; ghi vào `docs/GIAO-DIEN.md`.

## 11. Bảo mật của chính tpan

Panel chạy bằng quyền root nên lỗi ở panel là lỗi chiếm máy. Năm 2026 có nhiều lỗ hổng nghiêm trọng ở các panel phổ biến (chèn lệnh ở aaPanel, chuỗi lỗi thực thi lệnh không cần đăng nhập ở CyberPanel, vượt đăng nhập ở cPanel); phần lớn bắt nguồn từ việc ghép dữ liệu người dùng vào lệnh shell hoặc kiểm đăng nhập thiếu.

- **Không shell**: mọi lệnh chạy bằng argv (mục 4.3). Có test tĩnh cấm `sh -c`, `bash -c` trong mã.
- **Đầu vào có kiểu**: `validate.Domain` chỉ nhận tên miền hợp lệ (chuyển về chữ thường, punycode, mỗi nhãn `[a-z0-9-]`, không bắt đầu hay kết thúc bằng `-`, tổng ≤ 253 ký tự). Tên user, DB, pool không lấy từ người dùng mà sinh từ slug. Template chỉ nhận các kiểu đã kiểm.
- **Đăng nhập**: một tài khoản quản trị; mật khẩu băm bằng argon2id; `tpan setup` sinh mật khẩu lần đầu và in ra đúng một lần. Sai 5 lần thì khoá IP đó 15 phút.
- **Phiên**: token ngẫu nhiên 32 byte, DB chỉ lưu sha256 của nó; cookie `__Host-tpan`, `Secure`, `HttpOnly`, `SameSite=Strict`; hết hạn sau 12 giờ không dùng.
- **CSRF**: kiểm `Sec-Fetch-Site` và `Origin` cho mọi request có thay đổi (Go có sẵn `http.CrossOriginProtection`; đọc tài liệu của đúng phiên bản Go đang dùng trước khi áp dụng).
- **TLS**: panel nghe HTTPS ở cổng 8443 với chứng chỉ tự ký sinh lúc `setup` (in fingerprint để đối chiếu). Có chế độ riêng tư: chỉ nghe `127.0.0.1`, vào bằng `ssh -L 8443:127.0.0.1:8443 <máy>`.
- **Header**: `Content-Security-Policy: default-src 'self'`, `X-Content-Type-Options: nosniff`, `frame-ancestors 'none'`.
- **Bí mật**: không ghi mật khẩu vào log, DB hay repo. Mật khẩu quản trị WordPress hiện một lần trong kết quả job rồi bỏ.
- **Nhật ký**: mọi thao tác có thay đổi ghi vào `audit` (thời điểm, IP, thao tác, kết quả).
- **AI và MCP**: theo sáu nguyên tắc ở mục 8.1. Khoá API nằm trong `/etc/tpan/ai.toml` quyền 600, không vào DB, log hay repo. MCP chỉ chạy qua stdio; không có cổng MCP nào mở ra mạng.

Để sau (không làm trong đợt này): tách đặc quyền, tức tiến trình web chạy dưới user thường và chỉ nói chuyện với một tiến trình root nhỏ qua Unix socket với danh sách thao tác cố định. Ranh giới `System` ở mục 4.3 được vẽ sẵn để làm việc này không phải viết lại lõi.

## 12. Kiểm thử và bằng chứng

| Tầng | Chạy ở đâu | Kiểm gì |
|---|---|---|
| Unit | Mac, `make test` | kiểm đầu vào (kèm các chuỗi cố tình chèn lệnh), sinh slug, template so với file mẫu (golden), engine với `FakeSystem`: bỏ qua bước đã đúng, hoàn tác đúng thứ tự, lỗi khi hoàn tác; che dữ liệu (không lọt mật khẩu, IP, email, tên miền thật); danh mục công cụ (AI không gọi được gì ngoài danh mục); log có cài câu lệnh, chạy với `FakeProvider`: máy không đổi gì khi chưa có người duyệt |
| Tích hợp | máy ảo `tpan-test`, `make e2e` | kịch bản đầy đủ trên Ubuntu 24.04 sạch (dưới đây) |
| Tay | trình duyệt | giao diện, hai ngôn ngữ, bàn phím |

Kịch bản `make e2e` (bắt đầu bằng `scripts/vm.sh reset`):

1. `stack install --web nginx` → 3 dịch vụ `active`.
2. Tạo 3 site → cả 3 trả 200 đúng nội dung của mình.
3. Kiểm cô lập: user của site 1 không đọc được `wp-config.php` của site 2.
4. Chạy lại bước 2 → không có gì thay đổi (idempotent).
5. Cố tạo site với domain `a.com; rm -rf /` → bị từ chối ở bước kiểm, không lệnh nào chạy.
6. Cho bước ghi vhost lỗi có chủ ý → site đó được hoàn tác sạch (không còn user, DB, thư mục).
7. Xoá site 2 → 404/444; khôi phục → 200, dữ liệu còn nguyên.
8. `stack switch --to apache` → cả 3 site vẫn 200; chuyển ngược lại → vẫn 200.
9. Sửa tay một vhost → `tpan health` báo lệch đúng file đó.
10. Backup site 1, sửa nội dung, restore → nội dung về như lúc backup.
11. Chạy lần lượt các kịch bản trong `scripts/faults/` → health báo lỗi ở đúng chặng; sửa tự động đưa về đạt.
12. Nối một MCP client thử vào `tpan mcp`: liệt kê được công cụ; công cụ lập Plan trả `plan_id` và máy **không** đổi gì khi chưa duyệt.

Thứ tự bắt buộc: unit test xanh → e2e xanh trên máy ảo → mới chạy trên VPS. **Không chạy thử lần đầu trên VPS.**

## 13. Các mốc

Mỗi mốc kết thúc bằng ba thứ: test xanh, bằng chứng chạy trên máy ảo, và một file trong `docs/giai-thich/`. Thiếu một thứ thì chưa sang mốc sau. Trước khi đem dự án cho người khác xem, người chịu trách nhiệm phải tự giải thích lại được ít nhất M1 đến M3 mà không nhìn tài liệu.

| Mốc | Nội dung | Xong khi |
|---|---|---|
| **M0** ✅ | Khung: repo, build chéo, nhúng giao diện, CI, máy ảo test | `make check` xanh; `tpan version` chạy trong máy ảo |
| **M1** | `validate`, `system`, `engine`, `store`, `checks`; `tpan stack install` cho cả Nginx và Apache; `--dry-run`; `tpan health` cho stack | e2e bước 1 cho cả hai web server; chạy lại không đổi gì |
| **M2** | `tpan site add/list/info/rm` với cô lập đầy đủ và hoàn tác; phép kiểm theo đường đi request | e2e bước 2 đến 6 |
| **M3** | `tpan setup`, API, đăng nhập, SSE; giao diện theo bản mẫu đã chọn: tổng quan với ma trận sức khoẻ và số liệu máy, trang site với đường đi request, tạo site có xem trước, lịch sử và trang job; chế độ `--demo`; hai ngôn ngữ | tạo site từ trình duyệt trên máy ảo, thấy log chạy trực tiếp và bằng chứng |
| **M4** | Health đầy đủ: chạy định kỳ, sự cố, sửa tự động và sửa tay, tự kiểm tpan, phát hiện lệch, log trực tiếp, số request và lỗi 5xx theo site, `scripts/faults/` | e2e bước 9 và 11 |
| **M5** | SSL (staging), trang bảo mật, ufw, fail2ban | chứng chỉ staging cho một domain thật; `certbot renew --dry-run` đạt |
| **M6** | Hàng loạt (dán danh sách, `apply -f`, `export`), thùng rác, khôi phục, dọn tự động | e2e bước 7; tạo 10 site một lượt chỉ reload một lần |
| **M7** | AI trong panel: `Provider`, che dữ liệu, danh mục công cụ, Plan chờ duyệt, thanh lệnh, giải thích, chẩn đoán, `tpan ask` | với `FakeProvider`: test che dữ liệu và test log cài câu lệnh đạt. Với provider thật trên máy ảo: một lỗi từ `scripts/faults/` được chẩn đoán đúng chặng và Plan đề xuất sửa được nó |
| **M8** | `tpan mcp`, hai chế độ duyệt, thư mục `agents/` | e2e bước 12; Claude Code trên máy cá nhân liệt kê được site của máy ảo qua SSH |
| **M9** | Backup, khôi phục, chặn đầy đĩa | e2e bước 10; backup bị từ chối khi đĩa giả lập gần đầy |
| **M10** | Chuyển LEMP ⇄ LAMP | e2e bước 8; đo được thời gian gián đoạn |
| **M11** | `install.sh`, bản phát hành trên GitHub, README có ảnh, rà hai ngôn ngữ | cài từ một lệnh lên máy ảo sạch rồi tạo được site |

M1 đến M3 là phần bắt buộc của đề. Từ M4 trở đi các mốc độc lập với nhau (riêng M8 cần danh mục công cụ của M7), nên thứ tự trên đổi được. Thiếu thời gian thì bỏ bớt mốc; không làm dở nhiều mốc cùng lúc.

Để sau: tách đặc quyền, clone site sang staging, đọc trạng thái pool PHP-FPM qua FastCGI, backup theo lịch, nhiều phiên bản PHP, quản lý nhiều máy.

## 14. Quy ước làm việc

- Nhánh: phát triển trên `dev` hoặc `feature/<tên>`; chỉ merge vào `main` khi `make check` và e2e xanh.
- Commit nhỏ, một ý, tiếng Việt, dạng `engine: hoàn tác theo thứ tự ngược khi bước lỗi`.
- Chú thích trong mã bằng tiếng Việt, nói **vì sao**, không nhắc lại mã làm gì.
- Lỗi luôn kèm ngữ cảnh: `fmt.Errorf("ghi vhost %s: %w", domain, err)`.
- Không thêm thư viện khi thư viện chuẩn làm được. Thêm thư viện nào thì ghi lý do vào bảng ở mục 3.
- Không có mật khẩu, IP thật, tên miền nội bộ, tên công ty hay tên người trong repo. Ví dụ dùng `example.com`.
- Lệnh nguy hiểm (xoá, dừng dịch vụ, đổi SSH, tường lửa) phải có `Danger: true` và giao diện phải hỏi lại.
- Mọi thứ chạy trên máy ảo trước. Lệnh trên VPS do người chịu trách nhiệm tự gõ sau khi đã thấy nó chạy đúng trên máy ảo.

## 15. Tài liệu giải thích

`docs/giai-thich/` có một file cho mỗi mốc, viết cho người chưa biết Go và mới học Linux. Mỗi file theo đúng khung:

1. Mốc này làm được gì (3 đến 5 dòng).
2. Chạy thử: lệnh, kèm output thật đã chạy trên máy ảo.
3. Đi qua mã theo thứ tự chạy: file nào, hàm nào, vì sao viết vậy.
4. Kiến thức nền cần có (Linux, web server, Go), mỗi khái niệm gắn với đúng một chỗ trong mã.
5. Đã thử cách nào khác và vì sao không chọn.
6. Câu hỏi tự kiểm, có đáp án ở cuối file.

Thêm `docs/giai-thich/00-go-can-ban.md`: những phần của Go mà tpan dùng (package, struct, interface, error, context, goroutine, embed), mỗi phần minh hoạ bằng mã lấy từ chính repo.

Output dán vào tài liệu phải là output thật. Chưa chạy thì ghi "chưa chạy", không viết output theo dự đoán.

## 16. Điều chưa chắc, phải kiểm trên máy ảo

- Gói `apache2` có tự khởi động khi cài không, và `policy-rc.d` có chặn được không.
- WP-CLI nhận mật khẩu qua stdin với `--prompt=dbpass` và `--prompt=admin_password` như thế nào.
- Bản Ubuntu 24.04 của Multipass khác bản "minimized" trên VPS ở những gói nào (`dpkg -l | wc -l` hai bên, so danh sách).
- MariaDB bản của Ubuntu 24.04 có sẵn user ẩn danh và DB `test` không.
- 80 MB cho một worker PHP của WordPress có sát thực tế không.
- `rename` giữa `/var/www` và `/var/lib` có cùng phân vùng trên VPS đích không (`df /var/www /var/lib`).
- Máy ảo test là arm64, VPS là amd64: gói cài giống nhau, nhưng phải chạy lại e2e trên VPS ở lần triển khai đầu.
- API của SDK MCP Go và SDK của các nhà cung cấp AI: đọc tài liệu của đúng phiên bản thêm vào, không viết theo trí nhớ.
- Che dữ liệu có làm AI chẩn đoán kém đi không: đo bằng các kịch bản trong `scripts/faults/`, có che và không che.
- Quy định của nơi triển khai về việc gửi dữ liệu máy chủ cho AI bên ngoài: phải được đồng ý trước khi bật AI trên máy có dữ liệu thật.

## 17. Nguồn đã tra (19/11/2026)

- Giới hạn của Let's Encrypt: https://letsencrypt.org/docs/rate-limits/
- certbot, chế độ webroot và hook: https://eff-certbot.readthedocs.io/en/latest/using.html
- Kiến trúc 1Panel (Go, giao diện nhúng, SQLite): https://deepwiki.com/1Panel-dev/1Panel/2-core-architecture
- Kiến trúc HestiaCP (giao diện gọi xuống bộ lệnh CLI): https://deepwiki.com/coriaweb/hestiacp/1.1-architecture
- Apache với MPM event và PHP-FPM, pool riêng từng site: https://oneuptime.com/blog/post/2026-03-02-setup-apache-php-fpm-ubuntu/view
- User và pool PHP riêng cho từng site: https://spinupwp.com/doc/understanding-php-pools/
- PHP-FPM `ondemand` cho máy nhiều site nhỏ: https://ma.ttias.be/a-better-way-to-run-php-fpm/
- aaPanel chạy PHP các site chung một user (câu hỏi trên diễn đàn): https://aapanel.com/forum/d/23589-how-to-assign-a-unique-process-user-for-each-site-in-aapanel
- Lỗi chèn lệnh ở aaPanel: https://codeant.ai/vulnerability-database/cve-2026-113506
- Chuỗi lỗi ở CyberPanel: https://pentera.io/?p=5646
- Lịch phát hành Go: https://go.dev/doc/devel/release
- SDK Go chính thức của MCP: https://github.com/modelcontextprotocol/go-sdk
- Thêm MCP server vào Claude Code: https://code.claude.com/docs/en/mcp
- Thêm MCP server vào Codex: https://developers.openai.com/codex/mcp
- Cài câu lệnh vào log để dẫn dắt AI agent: https://www.eye.security/blog/log-poisoning-openclaw-ai-agent-injection-risk
- Đo hiệu quả các biện pháp chống tấn công qua log: https://www.alphaxiv.org/abs/2605.24421
- Giao diện phòng điều khiển, màu chỉ cho bất thường (ISA-101): https://control.com/technical-articles/going-gray/
