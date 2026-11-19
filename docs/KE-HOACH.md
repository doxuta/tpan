# TPAN: kế hoạch và thiết kế

Tài liệu này là bản đặc tả để viết code. Ai viết code (người hay AI) đều làm theo nó; chỗ nào thực tế khác với tài liệu thì sửa tài liệu trong cùng commit.

Trạng thái: mốc M0 (khung dự án) đã xong ngày 19/11/2026. Các mốc sau chưa bắt đầu. Cùng ngày đã chốt hai việc: hướng giao diện (bản "Tuyến", có sáng và tối, đặc tả ở `docs/GIAO-DIEN.md`) và bộ não AI (Claude Code của người vận hành, mục 8.2).

## 1. TPAN là gì

TPAN là một panel quản trị máy chủ web, viết lại phần lõi của aaPanel ở quy mô nhỏ:

1. Cài mô hình **LEMP** (Nginx) hoặc **LAMP** (Apache) lên một VPS Ubuntu sạch.
2. Tạo **nhiều site WordPress**, mỗi site một tên miền, trên cùng một VPS.

Hai việc trên là bắt buộc. Mọi thứ khác trong tài liệu này là phần làm thêm, xếp theo mốc, và chỉ làm khi phần lõi đã có bằng chứng chạy đúng.

TPAN là **một file chạy duy nhất** (`tpan`) gồm bốn mặt cùng dùng chung một lõi:

- CLI: `tpan stack install`, `tpan site add shop.vn`, ...
- API HTTP: `/api/v1/...`
- Giao diện web (nhúng sẵn trong file chạy), song ngữ Việt và Anh.
- MCP server (`tpan mcp`): để Claude Code, Codex hoặc agent khác dùng TPAN qua đúng các thao tác trên.

Phần AI là tuỳ chọn và tắt mặc định. Không bật AI thì mọi chức năng khác vẫn đầy đủ; không có tính năng nào chỉ làm được bằng AI. Bộ não AI là Claude Code chạy trên máy của người vận hành; máy chủ không giữ khoá hay đăng nhập AI nào (mục 8).

### Ngoài phạm vi

Mail server, DNS server, FTP, Docker, nhiều người dùng với phân quyền, quản lý nhiều máy chủ, hệ điều hành ngoài Ubuntu 24.04. Lý do: không nằm trong đề, và mỗi thứ là một mảng lớn.

Cũng không làm trình quản lý file và công cụ gõ SQL trên web. Một panel chạy bằng quyền root mà cho tải lên, sửa file hay chạy SQL tuỳ ý là chỗ các panel dính lỗ hổng nhiều nhất, và hai thứ đó không giúp gì cho hai yêu cầu chính của đề.

### Các mục cơ bản của một panel, và TPAN có gì

Đối chiếu với thanh menu của HestiaCP và aaPanel:

| Mục | TPAN | Ở mốc |
|---|---|---|
| Web (domain, site) | có | M2 |
| Cơ sở dữ liệu | có: mỗi site một DB; màn danh sách, đổi mật khẩu, xuất, nhập (mục 6.9) | M3 (xem), M9 (thao tác) |
| SSL | có | M5 |
| Backup | có, bấm tay và theo lịch | M9 |
| Cron | có: cron thật cho WordPress và lịch backup; chưa cho gõ lệnh tuỳ ý (mục 6.10) | M9 |
| Phiên bản PHP, nginx, MariaDB | có (mục 6.11) | M12 |
| Cập nhật | có (mục 6.11) | M12 |
| Thống kê | một phần: CPU, RAM, đĩa, số request và lỗi 5xx theo site; không có biểu đồ dài ngày | M3, M4 |
| Thông báo | một phần: sự cố hiện trên menu và trên logo; chưa gửi ra ngoài | M4 |
| Tài khoản | một tài khoản quản trị: đổi mật khẩu, đăng xuất | M3 |
| Nhiều người dùng, gói, hạn mức | không làm | |
| Trình quản lý file | không làm; trang site cho xem các file TPAN quản lý kèm diff | |
| Mail, DNS, FTP | không làm | |

## 2. Khác gì các panel đang có

Panel kiểu aaPanel là "hộp đen": bấm nút, panel chạy lệnh bằng quyền root, người dùng không thấy nó sắp làm gì và không dễ biết nó đã làm đúng chưa. TPAN đi theo hướng "hộp kính":

| Vấn đề ở panel thường gặp | TPAN làm gì |
|---|---|
| Bấm là chạy, không biết sẽ đổi gì trên máy | **Xem trước** mọi thao tác: danh sách bước, lệnh sẽ chạy, file sẽ ghi (có diff), lý do của từng bước. Giống `terraform plan`. |
| Lỗi giữa chừng để lại máy ở trạng thái dở dang | Mỗi bước có **hoàn tác**. Bước N lỗi thì các bước N-1..1 được hoàn tác theo thứ tự ngược. Config luôn được kiểm (`nginx -t`, `apachectl configtest`) trước khi reload. |
| "Xong" nhưng không có gì chứng minh | Mỗi thao tác kết thúc bằng bước **kiểm chứng**, lưu lại bằng chứng: mã HTTP, socket, quyền file, ngày hết hạn chứng chỉ. |
| Mọi site chạy PHP chung một user, một site bị chiếm là lộ các site khác | **Cô lập từng site**: user Linux riêng, pool PHP-FPM riêng, DB và user DB riêng (mục 5). |
| Site lỗi thì chỉ thấy 502, phải tự SSH vào lần từng tầng | **Health theo đường đi request**: TPAN tự kiểm chính nó và mọi thứ nó tạo ra, theo từng chặng DNS → TLS → web server → pool PHP → WordPress → DB. Lỗi hiện đúng chặng hỏng kèm bằng chứng và ba cách sửa: tự động, sửa tay theo hướng dẫn, hỏi AI (mục 6.7). |
| Sửa tay file config thì panel không biết, lần sau ghi đè | **Phát hiện lệch** là một nhóm phép kiểm của health: so file thật với bản TPAN đã ghi, báo đúng file bị sửa. |
| Chọn LAMP hay LEMP là chọn một lần; muốn đổi phải cài lại | **Chuyển LEMP ⇄ LAMP tại chỗ**. Làm được vì cả hai đều chạy PHP qua PHP-FPM, chỉ khác lớp web server đứng trước (mục 6.6). |
| Tạo, xoá từng site một; xoá là mất | **Hàng loạt** (dán danh sách hoặc file `sites.yaml`) và **thùng rác**: xoá là chuyển vào thùng rác, khôi phục được trong 7 ngày. |
| Backup có thể làm đầy đĩa và kéo sập cả máy | **Chặn trước**: ước lượng dung lượng, thiếu chỗ thì từ chối backup. Log của từng site có logrotate, cấu hình logrotate được kiểm bằng `logrotate -d` trước khi ghi. |
| Panel là một tiến trình root mở cổng ra internet | Không ghép chuỗi lệnh shell, mọi đầu vào có kiểu và được kiểm, đăng nhập có giới hạn số lần, có chế độ chỉ nghe ở `127.0.0.1` và vào qua SSH tunnel (mục 11). |
| AI trong panel thường là ô chat được quyền chạy lệnh, khoá AI nằm ngay trên máy chủ | AI của TPAN **không có tay**: nó chỉ đọc qua các công cụ có giới hạn và chỉ được *đề xuất* một Plan. Người duyệt trên bản xem trước rồi engine mới chạy, có hoàn tác và kiểm chứng như mọi thao tác khác. Dữ liệu được che trước khi gửi cho AI và hiện ra để người dùng xem. **Bộ não ở máy người vận hành**: máy chủ không giữ khoá hay đăng nhập AI nào (mục 8). |

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
| Tên miền có dấu | `golang.org/x/net/idna` | Đổi tên miền có dấu sang punycode và kiểm theo đúng chuẩn IDNA; thư viện chuẩn không có. Gói `x/` do chính nhóm Go duy trì. | Tự viết bảng chuyển: dễ sai ở các trường hợp biên. |
| Đẩy tiến độ ra giao diện | Server-Sent Events | Một chiều từ server ra trình duyệt là đủ cho log của job; đơn giản hơn WebSocket. | WebSocket. |
| Frontend | React 19, TypeScript 7, Vite 8 | Phổ biến nhất; build ra file tĩnh rồi nhúng vào file chạy Go bằng `go:embed`. | Vue (1Panel, aaPanel dùng), Svelte, HTMX. |
| Dữ liệu phía trình duyệt | TanStack Query | Quản lý gọi API, cache, tải lại. | Tự viết bằng `fetch` + state. |
| Giao diện | Tailwind CSS 4 + thành phần tự viết; Radix cho hộp thoại, menu | Kiểm soát được từng chi tiết hình thức; Radix lo phần bàn phím và trợ năng. | Bộ thành phần dựng sẵn: nhanh nhưng ra giao diện giống mọi sản phẩm khác. |
| Chứng chỉ | `certbot certonly --webroot` | certbot chỉ đặt file thử thách, **không sửa config web server**; config do TPAN sinh từ template nên phải là một nguồn duy nhất. Cùng một cách cho cả Nginx và Apache. | `certbot --nginx`: tự sửa config. Thư viện ACME nhúng (lego): bớt một gói cài nhưng khó quan sát hơn. |
| WordPress | WP-CLI | Cài, cấu hình, kiểm checksum bằng lệnh, không cần trình duyệt. | Tự tải và tự sinh `wp-config.php`. |
| AI | Bộ não là **Claude Code của người vận hành**, chạy không giao diện (`claude -p`) trên máy của họ và chỉ được nạp công cụ MCP của TPAN. Trong mã: giao diện `Brain` với hai bản `claudecode` và `fake` | Dùng luôn gói Claude người vận hành đang có, không cần khoá API. Vòng lặp gọi công cụ do Claude Code lo, TPAN không viết lại. Một bộ công cụ MCP phục vụ cả AI trong panel lẫn agent bên ngoài. Máy chủ không giữ thông tin đăng nhập AI. | Tự gọi API của nhà cung cấp bằng khoá riêng (Anthropic, hoặc API kiểu OpenAI để trỏ vào model chạy tại chỗ): tốn tiền theo lượt, phải để khoá trên máy chủ, phải tự viết vòng lặp gọi công cụ; để ngỏ cho sau qua giao diện `Brain`. Chạy `claude` ngay trên máy chủ: phải để đăng nhập Claude trên máy chủ. |
| MCP | SDK Go chính thức `modelcontextprotocol/go-sdk`, transport stdio | Chạy qua SSH (`ssh <máy> tpan mcp`) nên không mở thêm cổng và không cần khoá AI trên máy chủ; xác thực chính là SSH. | Transport HTTP: phải mở cổng và tự lo xác thực. |
| Máy ảo test | Multipass, Ubuntu 24.04 | Giống hệ điều hành của VPS đích; có snapshot để quay về máy sạch trong vài giây. | Docker: không có systemd thật, không giống VPS. |

Phiên bản thư viện: lấy bản stable mới nhất tại lúc thêm vào, ghi trong `go.mod` và `pnpm-lock.yaml`. Không chép số phiên bản từ trí nhớ.

Gói hệ điều hành trên Ubuntu 24.04 (dùng tên gói không kèm số phiên bản để lên 26.04 không phải sửa):

- Chung: `mariadb-server`, `php-fpm`, `php-cli`, `php-mysql`, `php-curl`, `php-gd`, `php-mbstring`, `php-xml`, `php-zip`, `php-intl`, `certbot`, `curl`.
- LEMP thêm `nginx`. LAMP thêm `apache2`.
- **Không bao giờ** cài gói `php`, `php<bản>` (ví dụ `php8.3`) hay bất kỳ gói `libapache2-mod-php*` nào: chúng kéo theo mod_php và buộc Apache về MPM prefork. Gói `php8.3` của Ubuntu 24.04 ghi `libapache2-mod-php8.3` là lựa chọn đầu trong Depends, nên apt sẽ chọn mod_php. `steps.AptInstall` từ chối các tên này.

## 4. Kiến trúc

```
                                           máy của người vận hành
                                  ┌───────────────────────────────────────┐
                                  │ Claude Code, Codex (gõ trực tiếp)     │
                                  │ tpan brain (AI trong panel)           │
                                  └───────────────────┬───────────────────┘
                                                      │ SSH, stdio
 CLI      API HTTP ◄── Giao diện web                 MCP
  │          │                                        │
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
  ai/                phiên hỏi đáp, che dữ liệu, giao diện Brain
  ai/claudecode/     dựng lệnh claude -p, đọc luồng stream-json, kiểm sự kiện init
  ai/playbooks/      bản hướng dẫn cho AI theo từng mục của giao diện
  ai/bridge/         nối bộ não ở máy người vận hành với panel (tpan brain)
  mcp/               MCP server (stdio)
  validate/          kiểu Domain, Slug, Email và hàm kiểm
web/                 giao diện React; web/embed.go nhúng web/dist
scripts/vm.sh        quản lý máy ảo test
scripts/faults/      kịch bản gây lỗi có chủ ý trên máy ảo
agents/              hướng dẫn cho agent: skill cho Claude Code, AGENTS.md cho Codex
docs/                tài liệu này, GIAO-DIEN.md, mau-giao-dien/, giai-thich/, bang-chung/
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
    Stat(path string) (FileInfo, error) // không đi theo symlink (như lstat)
    // ... MkdirAll, Symlink, Readlink, ReadDir, Chown, Chmod, StatFS (dung lượng đĩa)
    Lock(ctx context.Context) (unlock func(), err error) // khoá toàn cục giữa các tiến trình tpan
}
```

Bản đầy đủ, kèm quy ước lỗi, nằm ở `internal/system/types.go`. Hai điểm chốt lúc viết hợp đồng M1: `Run` trả `*ExitError` khi mã thoát khác 0 (kèm `Result` đầy đủ để làm bằng chứng); đường dẫn không tồn tại luôn cho lỗi thoả `errors.Is(err, fs.ErrNotExist)` ở cả `LocalSystem` lẫn `FakeSystem`. Khoá toàn cục là `flock` trên `/run/tpan/tpan.lock`, vì CLI và `tpan serve` là hai tiến trình khác nhau.

- `Run` nhận tên chương trình và mảng tham số, gọi `exec.CommandContext`. Không có hàm nào nhận một chuỗi lệnh shell. Biến môi trường cố định: `LC_ALL=C`, `DEBIAN_FRONTEND=noninteractive`.
- `Cmd` có trường `Stdin` (để đưa mật khẩu và câu SQL vào qua stdin, không để lộ trên dòng lệnh) và `AsUser` (chạy dưới user của site bằng `runuser -u`).
- `WriteFileAtomic` ghi ra file tạm cùng thư mục rồi `rename`, để không bao giờ có file config ghi dở.
- Giá trị bí mật dùng kiểu `Secret`; khi in ra log nó hiện `••••`.
- `FakeSystem` giữ một hệ thống file giả và danh sách lệnh đã "chạy" trong bộ nhớ. Nhờ nó unit test chạy trên Mac không cần root, và `tpan serve --demo` cho phép làm giao diện mà không đụng máy thật.

### 4.4 Trạng thái (SQLite, `/var/lib/tpan/tpan.db`)

| Bảng | Giữ gì |
|---|---|
| `settings` | web server đang dùng, nguồn gói của từng thành phần (mục 6.11), bản PHP mặc định, email cho Let's Encrypt, chế độ staging, số ngày giữ thùng rác |
| `sites` | domain, slug, trạng thái (`active`, `trashed`), ssl, bản PHP đang dùng, thời điểm tạo, thời điểm vào thùng rác |
| `jobs`, `job_events` | lịch sử thao tác và log từng bước |
| `managed_files` | đường dẫn + sha256 của mọi file TPAN đã ghi (để phát hiện sửa tay) |
| `backups` | site, đường dẫn file, kích thước, sha256, thời điểm |
| `plans` | Plan chờ duyệt: nội dung, nguồn gốc (người, AI, MCP), ai duyệt, lúc nào |
| `check_results`, `incidents` | kết quả gần nhất của từng phép kiểm trên từng đối tượng; sự cố mở khi một phép kiểm chuyển sang lỗi, đóng khi nó đạt lại |
| `ai_sessions` | phiên AI: lối vào (trong panel hay agent nối trực tiếp), câu hỏi nếu có, ngữ cảnh và đường đã đi, các lần gọi công cụ, dữ liệu đã trả về (bản đã che), Plan được đề xuất |
| `sessions`, `audit` | phiên đăng nhập (chỉ lưu hash của token), nhật ký ai làm gì lúc nào |

Không lưu mật khẩu DB của site (nó nằm trong `wp-config.php`) và không lưu mật khẩu quản trị WordPress (chỉ hiện một lần lúc tạo).

## 5. Bố trí trên máy chủ

```
/usr/local/bin/tpan                 file chạy
/etc/tpan/config.toml               địa chỉ nghe, chế độ, phần [ai] (không chứa khoá nào)
/etc/tpan/tls/                      chứng chỉ của chính panel
/var/lib/tpan/tpan.db               trạng thái
/var/lib/tpan/acme/                 webroot chung cho thử thách Let's Encrypt
/var/lib/tpan/trash/<id>/           thùng rác
/var/lib/tpan/backups/<domain>/     backup
/var/cache/tpan/                    WordPress và WP-CLI đã tải (kèm checksum)
/run/tpan/brain.sock                socket nội bộ cho tpan brain (quyền 600, chỉ root)

/var/www/<domain>/public/           mã nguồn site (document root)
/var/www/<domain>/tmp/              upload tạm, session PHP của riêng site
/var/log/tpan/sites/<domain>/       access.log, error.log, php-error.log (thư mục thuộc root:root, 0755,
                                    để logrotate không phải đổi tên file trong thư mục do user của site
                                    kiểm soát; php-error.log thuộc user của site để PHP ghi được)

/etc/nginx/sites-available/tpan-<domain>.conf    (+ symlink ở sites-enabled)
/etc/apache2/sites-available/tpan-<domain>.conf  (+ symlink ở sites-enabled)
/etc/php/<ver>/fpm/pool.d/tpan-<slug>.conf
/etc/logrotate.d/tpan-sites
```

Mọi file TPAN sinh ra đều có tiền tố `tpan-` (trừ site mặc định của Apache, `000-tpan-default.conf`, lý do ở mục 6.1) và dòng đầu `# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.`

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

Pool dùng `pm = ondemand` (site không có khách thì không tốn RAM), `pm.max_children = 5`, `pm.process_idle_timeout = 10s`, `pm.max_requests = 500`. Bảng điều khiển tính RAM xấu nhất = tổng `max_children` × 80 MB và cảnh báo khi vượt RAM dành cho PHP. Con số 80 MB là ước lượng cho WordPress; phải đo lại trên máy thật bằng `ps -o rss`.

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
   - Apache: `a2dismod mpm_prefork`, bật `mpm_event proxy_fcgi setenvif rewrite ssl headers http2`, gỡ `000-default`, ghi `000-tpan-default.conf`. Tên file này là ngoại lệ duy nhất của luật tiền tố `tpan-`: Apache coi vhost được nạp **đầu tiên** (theo thứ tự tên file trong `sites-enabled`) là site mặc định, nên nếu đặt `tpan-default.conf` thì site `tpan-abc.com.conf` sẽ xếp trước và hứng mọi request lạ. Nginx không bị vì dùng `default_server`.
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

Giới hạn của Let's Encrypt cần nhớ: 50 chứng chỉ cho mỗi tên miền đăng ký trong 7 ngày (tính chung cho mọi tên miền con và mọi người dùng tên miền đó), 5 chứng chỉ cho cùng một bộ tên trong 7 ngày, 300 đơn mới cho một tài khoản trong 3 giờ. **Trong lúc phát triển luôn bật staging.** Chỉ tắt staging cho buổi demo, và khi đó chỉ xin vài chứng chỉ: hạn mức 50 là của chung mọi người dùng cùng tên miền gốc, dùng hết thì người khác cũng không xin được.

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

Phép kiểm của **chính TPAN**: `PRAGMA integrity_check` của SQLite, chỗ trống ở `/var/lib/tpan`, hạn chứng chỉ của panel, bộ hẹn giờ còn chạy, có job nào treo, quyền của file config, tuổi bản sao lưu gần nhất của `tpan.db`.

Lịch chạy: ngay sau mỗi job (cho các đối tượng job đó chạm vào); 5 phút một lần cho phép kiểm nhẹ; mỗi giờ cho phép kiểm nặng (checksum, DNS). Chạy tay bằng `tpan health [<domain>] [--json]`.

Khi một phép kiểm chuyển từ đạt sang lỗi, TPAN mở một **sự cố**; khi đạt lại thì đóng. Sự cố giữ bằng chứng lúc mở, lúc đóng và những việc đã làm ở giữa.

Mỗi phép kiểm không đạt đưa ra tối đa ba cách sửa:

1. **Sửa tự động**: một Plan dựng từ các Step có sẵn (ví dụ ghi lại vhost từ template, khởi động lại pool). Vẫn qua xem trước, xác nhận và kiểm chứng.
2. **Sửa tay**: các lệnh cụ thể để người vận hành tự gõ, kèm cách kiểm lại. Cách này luôn có, kể cả khi không có cách sửa tự động.
3. **Hỏi AI** (khi đã bật AI): gửi phát hiện và bằng chứng đã che cho bộ não để chẩn đoán. Bộ não chưa nối thì nút này đưa ra câu lệnh để dán vào Claude Code (mục 8.3 và 8.4).

File bị sửa tay: hiện diff giữa bản đang có và bản TPAN sẽ sinh, cho chọn ghi lại bản của TPAN hoặc nhận bản sửa tay làm chuẩn mới.

Để kiểm chính hệ thống health, `scripts/faults/` có các kịch bản gây lỗi có chủ ý trên máy ảo: dừng PHP-FPM, xoá socket của một pool, sửa hỏng một vhost, đổi quyền `wp-config.php`, khoá user DB, làm đầy đĩa bằng file giả. Mỗi kịch bản phải làm đúng một chặng chuyển sang lỗi, và cách sửa tự động phải đưa nó về đạt.

### 6.8 Bảo mật máy

Trang chỉ đọc là chính, mỗi mục một dòng trạng thái kèm lệnh đã dùng để kiểm:

- ufw: bật chưa, đang mở cổng nào. Nút bật: mở 22, 80, 443 và cổng panel **trước** rồi mới `ufw --force enable`.
- fail2ban cho sshd: nút cài và bật.
- SSH: root có đăng nhập được không, còn cho mật khẩu không. **Chỉ cảnh báo**, không tự sửa, vì sửa sai là mất đường vào máy.
- Có swap không, có gói bảo mật chờ cập nhật không, cổng panel có mở ra internet không.

### 6.9 Cơ sở dữ liệu

Mỗi site có đúng một DB và một user DB, tạo cùng lúc với site (mục 6.2). Màn "Cơ sở dữ liệu" liệt kê chúng: tên DB, user, site, dung lượng, số bảng. Thao tác:

- **Đổi mật khẩu user DB**: sinh mật khẩu mới, đổi trong MariaDB, ghi vào `wp-config.php`, rồi kiểm site còn trả 200. Hoàn tác là đặt lại mật khẩu cũ. Mật khẩu không được xuất hiện trên dòng lệnh hay trong log.
- **Xuất file `.sql`**: `mariadb-dump --single-transaction` vào thư mục backup của site, có chặn đầy đĩa như mục 6.5.
- **Nhập file `.sql`** từ một bản đã xuất: luôn tự xuất bản hiện tại trước, để hoàn tác được.

Không có DB rời không gắn với site nào, và không có ô gõ SQL trên web (mục 1).

### 6.10 Cron

WordPress mặc định chạy việc định kỳ của nó (đăng bài hẹn giờ, kiểm cập nhật) mỗi khi có khách vào trang. Site vắng khách thì việc bị trễ; site đông khách thì mỗi lượt xem trang phải gánh thêm việc. TPAN thay bằng cron thật:

- Mỗi site có một lịch chạy `wp cron event run --due-now` dưới user của site, mặc định 5 phút một lần, và `DISABLE_WP_CRON` được bật trong `wp-config.php`.
- Lịch nằm trong một file `/etc/cron.d/tpan-<slug>` cho mỗi site, do TPAN quản lý như mọi file khác: có kiểm lệch, xoá site thì file đi theo.
- Màn "Cron" liệt kê lịch của mọi site: việc gì, bao lâu một lần, lần chạy gần nhất và kết quả. Lịch backup (mục 6.5) cũng hiện ở đây.
- Chỉ có các việc TPAN định nghĩa sẵn. Chưa cho gõ lệnh tuỳ ý vào cron: đó là một đường chạy lệnh tự do, để sau.

### 6.11 Phiên bản và cập nhật

Ba thành phần của stack khác nhau về chuyện "nhiều phiên bản":

| Thành phần | Trên một máy | Chọn ở đâu | Đổi về sau |
|---|---|---|---|
| PHP | nhiều bản chạy song song, mỗi bản là một dịch vụ `php<bản>-fpm` | theo từng site | đổi qua lại tự do, có xem trước và hoàn tác |
| Web server | một bản, vì chỉ một tiến trình giữ được cổng 80 và 443 | lúc cài stack: gói của Ubuntu, hoặc bản mới từ kho của nhà phát hành | đổi nguồn là một Plan riêng, có kiểm config và hoàn tác |
| MariaDB | một bản | lúc cài stack: dòng của Ubuntu, hoặc một dòng LTS từ kho của MariaDB | chỉ nâng lên, không hạ xuống |

**Danh sách phiên bản không ghi cứng trong mã.** TPAN hỏi kho gói đang cấu hình (`apt-cache`) có những bản nào rồi hiện đúng danh sách đó. Kho ra bản mới thì TPAN tự thấy sau lần `apt update` kế tiếp, không phải sửa mã. Bên cạnh mỗi bản PHP có hai ghi chú, cũng hỏi lúc chạy chứ không ghi cứng: hạn hỗ trợ của PHP, và bản WordPress khuyên dùng (API của WordPress.org). Không có mạng thì chỉ thiếu ghi chú, danh sách vẫn đủ.

**PHP theo từng site.**

1. Kho PHP nhiều phiên bản cho Ubuntu là kho của Ondřej Surý, người duy trì gói PHP của Debian. Thêm kho này là thêm một nguồn gói ngoài Ubuntu, nên nó là một bước có `Danger`, hiện rõ trong bản xem trước, và chỉ chạy khi người dùng chọn một bản PHP khác bản của Ubuntu.
2. Cài một bản: `php<bản>-fpm` cùng đúng bộ extension ở mục 3.
3. Đổi bản cho một site mà không gián đoạn: ghi pool của site vào bản mới với một socket mang số phiên bản; reload bản mới; kiểm socket; sửa vhost trỏ sang socket mới; kiểm config; reload web server; kiểm site trả 200; cuối cùng mới gỡ pool ở bản cũ. Lỗi ở bước nào thì các bước trước được hoàn tác và site vẫn chạy trên bản cũ.
4. Bảng `sites` lưu bản PHP của từng site. Bản PHP nào không còn site dùng thì TPAN đề nghị gỡ.

**Nguồn của web server và MariaDB.** `settings` lưu nguồn đang dùng cho từng thành phần. Mặc định là gói của Ubuntu: được Ubuntu vá bảo mật và ít bất ngờ nhất. Nguồn khác (nginx.org bản stable hoặc mainline; kho của MariaDB với dòng LTS được chọn) là lựa chọn lúc cài; bản xem trước hiện rõ kho và khoá ký sẽ thêm vào máy.

**Nâng MariaDB lên dòng mới** là thao tác một chiều: dữ liệu đã nâng thì bản cũ không đọc lại được. Plan của nó bắt buộc có: xuất toàn bộ DB và kiểm file xuất, dừng dịch vụ, đổi kho, nâng gói, chạy `mariadb-upgrade`, kiểm mọi site. Không có hoàn tác tự động; đường lùi là cài lại dòng cũ rồi nhập lại từ file xuất, và bản xem trước nói rõ điều đó trước khi người dùng bấm.

**Cập nhật.**

- Màn "Cập nhật" hiện các gói của stack đang có bản mới: từ bản nào lên bản nào, có phải bản vá bảo mật không. Bấm cập nhật là một Plan như mọi thao tác khác: xem trước, chạy, kiểm config, khởi động lại dịch vụ cần thiết, rồi chạy lại toàn bộ health. Site nào đang đạt mà sau cập nhật không đạt thì mở sự cố ngay.
- **Bản vá bảo mật tự cài hằng ngày** bằng `unattended-upgrades` của Ubuntu. TPAN kiểm nó đang bật, và thêm các kho ngoài đã chọn vào danh sách được tự vá.
- **Bản lớn không bao giờ tự nhảy.** PHP 8.4 lên 8.5 hay MariaDB 11.8 lên 12.3 là do người bấm, vì plugin WordPress hay vỡ ở bản PHP mới. Với PHP, cách an toàn là cài bản mới song song rồi chuyển từng site.
- TPAN cập nhật chính nó bằng `tpan self-update`: tải bản phát hành mới, kiểm checksum, thay file chạy, khởi động lại dịch vụ, giữ lại bản cũ để quay về. Chỉ chạy khi người dùng gọi.

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
   - công cụ **đọc**: danh sách site, kết quả health, số liệu máy, đuôi log, nội dung file do TPAN quản lý, mục đang mở trên panel;
   - công cụ **lập Plan**: tạo site, xoá site, xin SSL, sửa một phát hiện của health. Gọi công cụ loại này chỉ sinh ra một Plan ở trạng thái chờ duyệt.

   Plan chờ duyệt hiện lên đúng màn xem trước của mọi thao tác khác, có nhãn "AI đề xuất". Người bấm duyệt thì engine mới chạy.
2. **Không có công cụ shell.** Việc nào không diễn đạt được bằng các Step có sẵn thì AI chỉ được trả lời bằng chữ: lệnh gợi ý để người tự gõ, ghi rõ "AI đề xuất, chưa được kiểm".
3. **Thấy được AI thấy gì.** Mỗi lần hỏi, giao diện hiện đúng dữ liệu sẽ gửi đi (bản đã che) trước khi gửi, và hiện từng lần AI gọi công cụ cùng kết quả.
4. **Che dữ liệu trước khi gửi.** Mật khẩu, khoá, salt bị loại hẳn. IP, email, tên miền được thay bằng bí danh ổn định trong phiên (`site-a.example`, `203.0.113.7`); TPAN giữ bảng đối chiếu tại chỗ và đổi ngược lại khi hiển thị câu trả lời. Mức che chỉnh được; mặc định là che hết.
5. **Thứ AI đọc được là dữ liệu, không phải mệnh lệnh.** Log truy cập chứa chuỗi do người lạ gửi tới (URL, User-Agent), nên kẻ xấu có thể cài vào đó câu lệnh nhắm tới AI. Nghiên cứu năm 2026 cho thấy các biện pháp chống ở tầng prompt chỉ giảm chứ không loại được kiểu tấn công này, nên lớp chặn chính của TPAN là nguyên tắc 1 và 2: dù AI bị dẫn dắt, việc tệ nhất nó làm được là đề xuất một Plan mà người sẽ đọc trước khi duyệt. Thêm vào đó, nội dung không tin cậy được bọc trong thẻ đánh dấu, và Plan nào được đề xuất sau khi AI đã đọc log sẽ mang cảnh báo trên màn xem trước.
6. **Tuỳ chọn và có giới hạn.** Tắt mặc định. Mỗi phiên tối đa 12 lượt, có thời gian chờ, có trần số phiên mỗi ngày. Mọi phiên được ghi vào nhật ký.
7. **Bộ não ở máy người vận hành, không ở máy chủ.** Máy chủ không giữ khoá hay đăng nhập AI nào. TPAN không bao giờ nhận, lưu hay chuyển tiếp thông tin đăng nhập Claude: không có ô nào để dán token, không đọc `~/.claude` hay Keychain. Người vận hành tự đăng nhập Claude Code trên máy của họ theo cách của Anthropic; TPAN chỉ gọi lệnh `claude` nguyên bản đã đăng nhập sẵn.

### 8.2 Bộ não: Claude Code của người vận hành

Quyết định ngày 19/11/2026: TPAN không gọi API của nhà cung cấp AI nào và không cần khoá API. Bộ não là **Claude Code** (lệnh `claude`) mà người vận hành đã cài và đăng nhập trên máy cá nhân bằng gói Claude của chính họ. Vòng lặp "hỏi model, model gọi công cụ, nhận kết quả, hỏi tiếp" do Claude Code lo; TPAN chỉ cung cấp công cụ qua MCP (mục 8.3) và hiển thị kết quả.

Có hai lối dùng bộ não, cùng đi qua một bộ công cụ:

| Lối | Người vận hành làm gì | Bộ não chạy thế nào | Hợp với |
|---|---|---|---|
| **Trực tiếp** | gõ trong Claude Code (hoặc Codex) ở terminal máy cá nhân | phiên Claude Code bình thường, đã thêm MCP server `tpan` (mục 8.3) | việc dài, nhiều bước, hỏi đáp tự do |
| **Trong panel** | bấm "Hỏi AI", "Giải thích", "Chẩn đoán" hoặc gõ vào `⌘K` trên giao diện web | `tpan brain` trên máy cá nhân nhận câu hỏi từ panel rồi chạy `claude -p` (mục 8.4) | hỏi ngay tại chỗ đang nhìn, có sẵn ngữ cảnh |

Trong mã, bộ não nằm sau một giao diện nhỏ:

```go
// Brain trả lời một lượt hỏi và phát sự kiện ra dần: chữ, lần gọi công cụ, kết quả cuối.
type Brain interface {
    Run(ctx context.Context, job Job) (<-chan Event, error)
}
```

Có hai bản: `claudecode` (thật) và `fake` (phát lại kịch bản soạn sẵn, dùng cho test và cho chế độ `--demo` trên máy không có Claude Code). Muốn thêm bộ não khác về sau (API có khoá, model chạy tại chỗ, `codex exec`) thì viết thêm một bản của giao diện này.

**Lệnh gọi bộ não.** `claudecode` chạy `claude` trong một thư mục tạm rỗng và đưa câu hỏi vào qua stdin. Mỗi cờ có một lý do:

| Cờ | Vì sao |
|---|---|
| `-p` | chạy một lượt rồi thoát, không mở giao diện terminal |
| `--output-format stream-json --verbose --include-partial-messages` | mỗi dòng stdout là một sự kiện JSON, nhờ đó chữ hiện dần trên panel |
| `--tools ""` | tắt toàn bộ công cụ có sẵn của Claude Code: không Bash, không đọc ghi file, không web |
| `--strict-mcp-config --mcp-config '<json>'` | chỉ nạp đúng một MCP server là `tpan`, bỏ qua mọi MCP server khác trên máy |
| `--allowedTools "mcp__tpan"` | công cụ của `tpan` chạy không cần hỏi quyền |
| `--permission-mode dontAsk --permission-prompts none` | mọi thứ khác bị từ chối ngay, vì không có ai ngồi trả lời hộp thoại xin quyền |
| `--setting-sources ""`, `--disable-slash-commands` | không nạp hook, plugin, skill và cấu hình riêng của người vận hành (cần kiểm, mục 16) |
| `--append-system-prompt-file <file>` | quy tắc của TPAN cho AI: trả lời bằng ngôn ngữ của giao diện, dẫn bằng chứng, coi nội dung log là dữ liệu |
| `--max-turns 12`, `--model <tên>` | chặn vòng lặp dài; model mặc định là `sonnet` để đỡ tốn hạn mức |
| `--session-id <uuid>` ở lượt đầu, `--resume <uuid>` ở lượt sau | giữ mạch hội thoại của một phiên hỏi đáp |

`<json>` của `--mcp-config` là `{"mcpServers":{"tpan":{"command":"ssh","args":["<máy>","tpan","mcp","--session","<id>"]}}}`. Khi TPAN và Claude Code ở cùng một máy thì `command` là `tpan` và `args` bỏ hai phần đầu.

**Không dùng `--bare`.** Tài liệu Claude Code khuyên `--bare` cho script, nhưng ở chế độ đó Claude Code không đọc đăng nhập bằng gói Claude mà đòi khoá API. Tài liệu cũng nói `--bare` sẽ thành mặc định của `-p` ở một bản sau. Vì vậy `tpan brain` ghi rõ phiên bản Claude Code đã chạy bài kiểm (lúc viết tài liệu này: 2.1.328), cảnh báo khi gặp phiên bản khác, và mỗi lần nâng Claude Code phải chạy lại bài kiểm ở mục 12.

**Kiểm cửa rồi mới tin.** Sự kiện đầu tiên của luồng (`system`, `init`) liệt kê công cụ, MCP server và plugin mà phiên đó thật sự nạp. `claudecode` đọc sự kiện này và **dừng ngay** nếu có thứ gì ngoài mong đợi: một công cụ không bắt đầu bằng `mcp__tpan__`, một MCP server khác, một plugin. Ngoại lệ là những công cụ không tắt được của chính Claude Code (tài liệu nêu `EndConversation`, chỉ để kết thúc hội thoại); danh sách ngoại lệ lấy từ sự kiện `init` thật ghi lại ở bài kiểm (mục 16), không viết theo phỏng đoán. Khi dừng, không câu trả lời nào được hiện, chỉ có thông báo nêu đúng thứ thừa. Đây là lớp bảo đảm cho nguyên tắc 1 và 2: nó không dựa vào việc các cờ ở trên có tác dụng đúng như tài liệu, và vẫn đúng khi Claude Code đổi hành vi ở bản mới.

**Hạn mức và điều khoản** (tra ngày 19/11/2026; kiểm lại ở hai trang ghi trong mục 17 trước khi dựa vào):

- Theo trang trợ giúp của Anthropic, `claude -p` khi đăng nhập bằng gói Claude vẫn tính vào hạn mức của gói, tức là chung với lúc dùng Claude Code để viết code. Chính sách này đã đổi qua lại trong năm 2026.
- Trang điều khoản của Claude Code nói: đăng nhập bằng gói Claude là để dùng Claude Code và ứng dụng của Anthropic theo cách thông thường; người làm sản phẩm không được nhận, lưu hay chuyển tiếp đăng nhập Claude của người dùng, và làm sản phẩm cho người khác dùng thì phải dùng khoá API. Trang đó cũng nói người dùng vẫn được đăng nhập gói của mình vào bản Claude Code nguyên gốc. Nguyên tắc 7 bám đúng ranh giới này: mỗi người dùng bộ não của chính mình, trên máy của chính mình, và TPAN không chạm vào đăng nhập.
- Nếu sau này đưa TPAN cho người khác dùng thì đọc lại trang điều khoản trước.

**Bộ não đặt ở đâu.** Khoá `brain` trong phần `[ai]` của `config.toml` nhận ba giá trị:

| Giá trị | Nghĩa | Dùng khi |
|---|---|---|
| `off` (mặc định) | không có AI | |
| `bridge` | bộ não ở máy người vận hành, nối vào bằng `tpan brain <máy>` | TPAN chạy trên VPS |
| `local` | TPAN tự gọi `claude` trên chính máy đang chạy TPAN | TPAN và Claude Code cùng ở máy cá nhân, ví dụ `tpan serve --demo` lúc làm giao diện |

Không dùng `local` trên VPS: muốn vậy phải để đăng nhập Claude trên máy chủ, trái nguyên tắc 7.

### 8.3 MCP: cổng chung cho mọi agent

`tpan mcp` chạy một MCP server qua stdio, đưa ra đúng danh mục công cụ ở 8.1. Kết nối đi qua SSH, nên không mở thêm cổng nào; xác thực chính là SSH. Người chạy lệnh phải có quyền root trên máy chủ.

```bash
# Claude Code, chạy trên máy cá nhân
claude mcp add --transport stdio tpan -- ssh <máy> tpan mcp

# Codex CLI
codex mcp add tpan -- ssh <máy> tpan mcp
```

Mỗi lần `tpan mcp` chạy là một phiên trong `ai_sessions`: mọi lần gọi công cụ và dữ liệu đã trả về (bản đã che) được ghi lại ở phía máy chủ, không dựa vào lời kể của agent. `--session <id>` gắn phiên đó với một câu hỏi gửi từ panel (mục 8.4).

Công cụ lập Plan trả về bản xem trước và một `plan_id`. Plan đó được chạy theo một trong hai chế độ duyệt, đặt trong config:

- `panel` (mặc định): agent không tự chạy được. Người duyệt trên giao diện web hoặc bằng `tpan plan approve <id>`.
- `client`: tin vào bước hỏi quyền của chính agent; `apply_plan(plan_id)` chạy ngay. Chỉ có ở lối trực tiếp; phiên đi từ panel luôn là `panel`.

Thư mục `agents/` trong repo có hướng dẫn cho agent: một skill cho Claude Code (`agents/skills/tpan/SKILL.md`) và `AGENTS.md` cho Codex, mô tả các quy trình như "chẩn đoán site trả 502" theo đúng thứ tự chặng ở mục 6.7.

**Nút chuyển sang Claude Code.** Ngay khi có MCP, mọi chỗ có "Hỏi AI" trên giao diện đều có thêm "Chép lệnh cho Claude Code": một câu lệnh soạn sẵn, ví dụ `claude "Dùng công cụ tpan chẩn đoán sự cố #12 rồi đề xuất cách sửa"`. Dán vào terminal máy cá nhân là Claude Code làm việc qua MCP; Plan nó đề xuất hiện trên panel chờ duyệt. Cách này không cần `tpan brain`, và là đường lùi khi bộ não chưa nối.

### 8.4 AI trong panel

Một câu hỏi đi một vòng như sau:

```
trình duyệt ──► TPAN (VPS) ──► tpan brain (máy cá nhân) ──► claude -p ──► Anthropic
                    ▲                                           │
                    └─────────── tpan mcp, qua SSH ◄────────────┘
                        (đọc dữ liệu đã che, đề xuất Plan)
```

1. Người vận hành chạy `tpan brain <máy>` trong một terminal trên máy cá nhân. Lệnh này kiểm `claude --version` và `claude auth status` (chưa đăng nhập thì dừng; đăng nhập bằng khoá API thì cảnh báo vì mỗi lượt hỏi sẽ tính tiền API), rồi mở `ssh <máy> tpan brain-link` và giữ kết nối đó. Panel hiện "Bộ não: đã nối" kèm tên máy, phiên bản Claude Code và model.
2. Trên panel, người dùng bấm "Hỏi AI". Giao diện hiện đúng nội dung sẽ gửi (câu hỏi và bằng chứng đã che); người dùng bấm gửi.
3. TPAN đẩy câu hỏi xuống `tpan brain` qua kết nối ở bước 1. `tpan brain` chạy `claude -p` như mục 8.2. Terminal đang chạy `tpan brain` in ra từng câu hỏi và từng lần gọi công cụ, để người vận hành thấy bộ não của mình đang được dùng vào việc gì.
4. Claude Code gọi công cụ của TPAN qua `ssh <máy> tpan mcp --session <id>`. Công cụ đọc trả dữ liệu đã che; công cụ lập Plan tạo một Plan chờ duyệt.
5. Các sự kiện (chữ hiện dần, lần gọi công cụ) chạy ngược về TPAN rồi ra trình duyệt qua SSE. TPAN đổi bí danh về tên thật ngay trước khi hiển thị.
6. Plan được đề xuất hiện ở màn duyệt với nhãn "AI đề xuất". Người duyệt thì engine mới chạy.

**Ngữ cảnh làm việc.** AI trả lời tốt khi biết người dùng đang đứng ở mục nào và vừa đi qua những mục nào. Vì vậy mỗi câu hỏi mang theo một khối ngữ cảnh do giao diện dựng sẵn; người dùng không phải kể lại.

| Phần | Ví dụ | Lấy từ đâu |
|---|---|---|
| Đang ở mục nào | Bảng điều khiển; trang site `xuongmoc.example`; Sự cố #41; màn duyệt Plan #12 | đường dẫn của màn đang mở |
| Đang chỉ vào cái gì | chặng Pool PHP của site đó; bước 3 của job #128; một dòng log | thứ người dùng vừa bấm "Hỏi AI" trên đó |
| Vừa đi qua những đâu | Bảng điều khiển → trang site → Bảng điều khiển | 10 lần chuyển mục gần nhất của phiên đăng nhập |
| Bằng chứng đi kèm | kết quả phép kiểm và output của nó | đối tượng đang chỉ vào |
| Bối cảnh máy | LEMP hay LAMP, phiên bản, số site, ngôn ngữ giao diện | bảng `settings` |

- Khối ngữ cảnh hiện thành các nhãn ở đầu ngăn AI. Người dùng bỏ được từng nhãn trước khi gửi. Đây cũng là cách làm đúng nguyên tắc 3.
- **Ngăn AI đi theo người dùng.** Ngăn nằm bên phải và giữ nguyên khi chuyển mục; cuộc trò chuyện không mất. Sang mục khác thì nhãn "Đang ở" đổi theo, và câu hỏi kế tiếp mang vị trí mới cùng đường vừa đi. Nhờ vậy mở một site khác rồi hỏi "còn site này thì sao?" vẫn được hiểu đúng.
- **Mỗi mục có một bản hướng dẫn riêng cho AI.** `internal/ai/playbooks/` có một file cho mỗi mục của menu (bảng điều khiển, site, cơ sở dữ liệu, SSL, backup, cron, thùng rác, sự cố, lịch sử, stack, cập nhật, bảo mật) và một file cho màn duyệt Plan: mục này có dữ liệu gì, nên đọc gì trước, chẩn đoán theo thứ tự nào, Plan nào hợp lệ ở đây. Bản của mục đang mở được gửi kèm câu hỏi đầu tiên ở mục đó. Nó không nằm trong system prompt, vì Claude Code giữ nguyên system prompt suốt một phiên.
- **AI chỉ đường được.** Trong câu trả lời AI có thể đưa liên kết tới một mục, dạng `tpan:site/<domain>/log` hoặc `tpan:incident/41`. Giao diện chỉ biến liên kết thành nút khi nó trỏ tới một màn có thật. Bấm nút thì chuyển tới đó và ngăn AI đi theo. AI không tự chuyển màn của người dùng.
- Ngữ cảnh đi qua bước che dữ liệu như mọi thứ khác: AI thấy bí danh, người dùng thấy tên thật.
- Lối trực tiếp cũng có ngữ cảnh. Giao diện báo cho TPAN mỗi lần người dùng chuyển mục; công cụ đọc `current_view` trả về mục đang mở trên panel và đường vừa đi. Nhờ đó trong Claude Code chỉ cần gõ "xem giúp cái đang mở trên panel".

`tpan brain-link` chỉ là ống nối: nó chuyển stdin và stdout sang socket nội bộ `/run/tpan/brain.sock` (quyền 600, chỉ root). Cùng lúc chỉ nhận một bộ não; bộ não thứ hai nối vào thì bị từ chối kèm thông báo máy nào đang nối.

Các lối vào trên giao diện dùng chung một lõi (`internal/ai`):

| Lối vào | Làm gì |
|---|---|
| Thanh lệnh `⌘K` | Gõ tên lệnh thì mở đúng form; gõ một câu thì AI chuyển thành Plan. Ví dụ: "tạo 3 site cho a.example, b.example, c.example, có SSL". |
| "Giải thích" trên mọi lỗi | Bước job lỗi, phép kiểm không đạt, một dòng log: AI giải thích bằng ngôn ngữ đang dùng và dẫn lại đúng bằng chứng. |
| "Chẩn đoán" trên sự cố | AI đọc thêm (log, config, trạng thái dịch vụ) qua công cụ đọc, nêu nguyên nhân khả dĩ nhất, rồi đề xuất Plan sửa, hoặc lệnh sửa tay nếu không có Step phù hợp. |
| Hỏi số liệu | "Site nào nhiều lỗi 5xx nhất trong 1 giờ qua?" AI gọi công cụ thống kê, trả lời kèm con số và lần gọi công cụ đã dùng. |
| Tóm tắt | Tóm tắt các job và sự cố trong một khoảng thời gian. |

Khi bộ não chưa nối, các nút trên không biến mất: chúng hiện cách nối (`tpan brain <máy>`) và nút "Chép lệnh cho Claude Code" ở mục 8.3. Thanh `⌘K` vẫn tìm và mở lệnh như thường.

Không có lệnh `tpan ask`: hỏi bằng dòng lệnh chính là gõ trong Claude Code đã thêm MCP server `tpan`.

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
tpan db list | dump <domain> | passwd <domain>
tpan cron list
tpan php list | install <bản> | use <domain> <bản>
tpan update [--check]              gói của stack: xem có gì mới, hoặc cập nhật
tpan self-update                   cập nhật chính tpan
tpan health [<domain>] [--fix]
tpan plan list | show <id> | approve <id> | reject <id>
tpan mcp [--session ID]            chạy MCP server qua stdio (gọi qua SSH từ máy cá nhân)
tpan brain <máy> [--model M]       chạy trên máy cá nhân: nối Claude Code vào panel của <máy>
tpan version
```

Mọi lệnh có thay đổi đều nhận `--dry-run` (chỉ in kế hoạch) và `--yes` (bỏ qua câu hỏi xác nhận). Thêm `--json` để in kết quả cho script đọc.

API dưới `/api/v1`, JSON. Mỗi lệnh ở trên có một endpoint tương ứng. Endpoint có thay đổi nhận `?dry_run=1` và trả về Plan; chạy thật thì trả `job_id`, tiến độ lấy ở `GET /api/v1/jobs/{id}/events` (SSE). Lỗi có dạng `{"error": {"code": "domain_invalid", "message": {"vi": "...", "en": "..."}}}`.

## 10. Giao diện

Bốn ý định hình giao diện:

1. **Bản đồ tuyến là màn chính.** Mỗi site là một tuyến chạy ngang; mỗi chặng của đường đi request (DNS, TLS, web server, pool PHP, WordPress, DB) là một ga trên tuyến. Các tuyến xếp chồng nên ga cùng loại thẳng cột với nhau. Tuyến liền nét tới ga cuối cùng còn đạt và đứt nét từ ga hỏng trở đi: nhìn là biết request đi được tới đâu. Lỗi ở tầng dùng chung (ví dụ PHP-FPM chết) hiện thành một **cột** ga đỏ; lỗi của riêng một site hiện thành một ga. Nhìn hình dạng vùng lỗi là biết loại sự cố trước khi đọc chữ nào.
2. **Trạng thái có cả hình lẫn màu.** Đạt là chấm tròn xanh lá, cảnh báo là hình thoi vàng, lỗi là ô vuông đỏ có dấu ×, chưa kiểm được là vòng rỗng xám. Bốn hình khác nhau nên người mù màu và bản in đen trắng vẫn đọc được. Màu chỉ dùng cho trạng thái; nút, khung và chữ đều đen trắng, nên chỗ nào có màu thì chỗ đó đang nói về tình trạng của máy.
3. **Thay đổi máy chủ được duyệt như duyệt pull request.** Bản xem trước của một Plan trình bày như một PR: tóm tắt, phạm vi ảnh hưởng (dịch vụ nào reload, bao nhiêu site dùng chung), file thay đổi dạng diff, lệnh sẽ chạy. Lịch sử job đọc như `git log` của cái máy: ai đổi gì, lúc nào, bằng chứng, và nút hoàn tác.
4. **Trạng thái nào cũng mở ra được bằng chứng.** Bấm vào bất kỳ dấu trạng thái nào là thấy lệnh đã chạy và output thật. Giao diện không khẳng định điều gì mà nó không chỉ ra được căn cứ.

**Menu chính** nằm bên trái ở màn rộng và xếp lên trên nội dung ở màn hẹp. Nó là danh sách các mục dưới đây theo đúng thứ tự, chia nhóm: nhóm đầu là việc hằng ngày (Bảng điều khiển, Site, Cơ sở dữ liệu, SSL, Backup, Cron, Thùng rác); nhóm "Máy" (Sự cố, Lịch sử, Stack, Cập nhật, Bảo mật); rồi Phiên AI; cuối cùng là Cài đặt và Đăng xuất. Mục nào đếm được thì có con số bên cạnh (số site, số DB, số thứ trong thùng rác, số bản cập nhật đang chờ). "Sự cố" hiện số trên nền đỏ khi có sự cố đang mở. Thanh đầu trang giữ logo, tên máy, các nhãn stack, ô lệnh `⌘K`, nút ngôn ngữ, Sáng/Tối và Gợi ý.

Màn hình:

1. **Đăng nhập.**
2. **Bảng điều khiển** (trang chủ): bản đồ tuyến; sự cố đang mở; cảnh báo (ví dụ chứng chỉ sắp hết hạn); Plan đang chờ duyệt; số liệu máy (CPU, RAM, đĩa, swap, mỗi số một thanh có vạch ngưỡng); lịch sử máy.
3. **Site**: đường đi request của site đó vẽ theo chiều ngang, mỗi chặng bấm được; bên dưới là bằng chứng, log trực tiếp, backup, thao tác.
4. **Tạo site**: một site hoặc dán danh sách → bản xem trước → chạy.
5. **Duyệt Plan**: màn xem trước dùng chung cho mọi thao tác, kể cả Plan do AI hoặc agent đề xuất (có nhãn nguồn gốc).
6. **Lịch sử**: các job theo thời gian; trang một job có từng bước, output chạy trực tiếp, bằng chứng; khi lỗi thì thấy các bước hoàn tác chạy ngược.
7. **Sự cố**: đang mở và đã đóng; mỗi sự cố có bằng chứng và ba cách sửa.
8. **Cơ sở dữ liệu** (mục 6.9), **Cron** (mục 6.10), **Backup**, **SSL**, **Thùng rác**.
9. **Stack** (kèm nút chuyển LEMP ⇄ LAMP và các bản PHP đang cài), **Cập nhật** (mục 6.11), **Bảo mật**.
10. **Cài đặt**: tài khoản quản trị (đổi mật khẩu, các phiên đang đăng nhập), AI và tình trạng bộ não, MCP, ngôn ngữ, sáng hoặc tối.
11. **Thanh lệnh `⌘K`**: mở được từ mọi màn, dùng hoàn toàn bằng bàn phím.
12. **Ngăn AI**: mở được từ mọi màn, nằm bên phải, đi theo khi chuyển mục; trên cùng là ngữ cảnh sẽ gửi kèm (mục 8.4). Trang **Phiên AI** liệt kê các phiên; mỗi phiên có câu hỏi, ngữ cảnh và đường đã đi, dữ liệu đã gửi (bản đã che), từng lần gọi công cụ, câu trả lời, Plan đề xuất. Phiên của agent nối trực tiếp qua MCP cũng nằm ở đây.

Nguyên tắc:

- Thao tác nào cũng đi qua ba chặng: nhập → xem trước → chạy và xem bằng chứng. Không có nút nào thay đổi máy ngay lập tức.
- **Gợi ý** (bản tiếng Anh: Tips): một công tắc trên thanh đầu trang. Bật thì bên cạnh từng bước, từng dòng config, từng phép kiểm có thêm một câu nói vì sao làm vậy. Câu đó viết sẵn trong mã (trường `Why` của Step), không do AI sinh ra. Người mới bật, người quen tắt.
- Chữ trên giao diện viết như người nói với người: ngắn, dùng từ thường ngày, nói thẳng việc sắp xảy ra. Quy tắc và ví dụ ở mục "Giọng chữ" của `docs/GIAO-DIEN.md`.
- Dữ liệu kỹ thuật (đường dẫn, lệnh, domain, mã lỗi) dùng chữ mono, bấm là chép.
- Song ngữ: chuỗi giao diện nằm trong `web/src/i18n/vi.ts` và `en.ts` (cùng một kiểu TypeScript nên thiếu khoá là lỗi biên dịch). Thuật ngữ kỹ thuật giữ nguyên ở cả hai bản: vhost, pool, socket, reload.
- Font tự chứa trong file chạy, không gọi CDN (panel phải chạy được trong mạng kín). Font phải có đủ dấu tiếng Việt.
- Dùng được bằng bàn phím; tương phản đạt WCAG AA ở cả hai chế độ sáng và tối. Lần đầu mở, giao diện theo cài đặt của hệ điều hành; nút Sáng/Tối trên thanh đầu trang đổi ngay và nhớ lựa chọn.

Hướng hình thức đã chốt ngày 19/11/2026: bản "Tuyến". Màu, chữ, thành phần và cách đổi sáng tối nằm ở `docs/GIAO-DIEN.md`; hai trang mẫu mở được bằng trình duyệt nằm ở `docs/mau-giao-dien/`. Làm giao diện thì theo hai thứ đó, không tự chọn màu hay font khác.

## 11. Bảo mật của chính TPAN

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
- **AI và MCP**: theo bảy nguyên tắc ở mục 8.1. Máy chủ không giữ khoá hay đăng nhập AI nào. MCP chỉ chạy qua stdio; không có cổng MCP nào mở ra mạng. Socket nội bộ `/run/tpan/brain.sock` có quyền 600, chỉ root dùng được.

Để sau (không làm trong đợt này): tách đặc quyền, tức tiến trình web chạy dưới user thường và chỉ nói chuyện với một tiến trình root nhỏ qua Unix socket với danh sách thao tác cố định. Ranh giới `System` ở mục 4.3 được vẽ sẵn để làm việc này không phải viết lại lõi.

## 12. Kiểm thử và bằng chứng

| Tầng | Chạy ở đâu | Kiểm gì |
|---|---|---|
| Unit | Mac, `make test` | kiểm đầu vào (kèm các chuỗi cố tình chèn lệnh), sinh slug, template so với file mẫu (golden), engine với `FakeSystem`: bỏ qua bước đã đúng, hoàn tác đúng thứ tự, lỗi khi hoàn tác; che dữ liệu (không lọt mật khẩu, IP, email, tên miền thật); danh mục công cụ (AI không gọi được gì ngoài danh mục); log có cài câu lệnh, chạy với bộ não `fake`: máy không đổi gì khi chưa có người duyệt; `claudecode` chạy với một lệnh `claude` giả: đọc đúng luồng sự kiện, và từ chối phiên khi sự kiện `init` khai thừa một công cụ |
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
13. Chạy `tpan brain` với một lệnh `claude` giả: câu hỏi gửi từ API của panel đi hết một vòng, Plan đề xuất nằm ở trạng thái chờ duyệt, máy **không** đổi gì. Cho `claude` giả khai thêm công cụ `Bash` ở sự kiện `init`: phiên bị từ chối.
14. Cài thêm một bản PHP, chuyển site 1 sang bản đó trong lúc một vòng lặp `curl` gọi site liên tục → không request nào lỗi; site 2 vẫn ở bản cũ.
15. `tpan update` trên máy ảo có gói đang chờ → sau cập nhật toàn bộ health vẫn đạt.

Thứ tự bắt buộc: unit test xanh → e2e xanh trên máy ảo → mới chạy trên VPS. **Không chạy thử lần đầu trên VPS.**

## 13. Các mốc

Mỗi mốc kết thúc bằng ba thứ: test xanh, bằng chứng chạy trên máy ảo, và một file trong `docs/giai-thich/`. Thiếu một thứ thì chưa sang mốc sau. Trước khi đem dự án cho người khác xem, người chịu trách nhiệm phải tự giải thích lại được ít nhất M1 đến M3 mà không nhìn tài liệu.

| Mốc | Nội dung | Xong khi |
|---|---|---|
| **M0** ✅ | Khung: repo, build chéo, nhúng giao diện, CI, máy ảo test | `make check` xanh; `tpan version` chạy trong máy ảo |
| **M1** | `validate`, `system`, `engine`, `store`, `checks`; `tpan stack install` cho cả Nginx và Apache; `--dry-run`; `tpan health` cho stack. Nguồn gói và bản PHP là tham số đọc từ `settings`, không ghi cứng ở đâu, để M12 không phải viết lại | e2e bước 1 cho cả hai web server; chạy lại không đổi gì |
| **M2** | `tpan site add/list/info/rm` với cô lập đầy đủ và hoàn tác; phép kiểm theo đường đi request | e2e bước 2 đến 6 |
| **M3** | `tpan setup`, API, đăng nhập, SSE; giao diện theo `docs/GIAO-DIEN.md`, sáng và tối: menu chính, bảng điều khiển với bản đồ tuyến và số liệu máy, màn cơ sở dữ liệu (chỉ xem), đổi mật khẩu quản trị, trang site với đường đi request, tạo site có xem trước, lịch sử và trang job; chế độ `--demo`; hai ngôn ngữ | tạo site từ trình duyệt trên máy ảo, thấy log chạy trực tiếp và bằng chứng |
| **M4** | Health đầy đủ: chạy định kỳ, sự cố, sửa tự động và sửa tay, tự kiểm TPAN, phát hiện lệch, log trực tiếp, số request và lỗi 5xx theo site, `scripts/faults/` | e2e bước 9 và 11 |
| **M5** | SSL (staging), trang bảo mật, ufw, fail2ban | chứng chỉ staging cho một domain thật; `certbot renew --dry-run` đạt |
| **M6** | Hàng loạt (dán danh sách, `apply -f`, `export`), thùng rác, khôi phục, dọn tự động | e2e bước 7; tạo 10 site một lượt chỉ reload một lần |
| **M7** | Cổng cho agent: danh mục công cụ, che dữ liệu, `tpan mcp`, hai chế độ duyệt, Plan chờ duyệt, thư mục `agents/`, nút "Chép lệnh cho Claude Code" | e2e bước 12; test che dữ liệu và test log cài câu lệnh đạt; Claude Code trên máy cá nhân liệt kê được site của máy ảo qua SSH, chẩn đoán đúng chặng một lỗi từ `scripts/faults/`, và Plan nó đề xuất sửa được lỗi đó |
| **M8** | AI trong panel: `Brain`, `claudecode`, `tpan brain`, kiểm sự kiện `init`, ngăn AI có ngữ cảnh làm việc và bản hướng dẫn theo mục, thanh lệnh, giải thích, chẩn đoán, trang phiên AI | e2e bước 13; với Claude Code thật: hỏi từ panel của máy ảo, chữ hiện dần, Plan đề xuất hiện ở màn duyệt; chuyển mục rồi hỏi tiếp thì ngữ cảnh gửi đi đổi theo, xem được ở trang phiên AI |
| **M9** | Backup, khôi phục, chặn đầy đĩa, backup theo lịch; cron thật cho WordPress và màn Cron; đổi mật khẩu, xuất và nhập DB | e2e bước 10; backup bị từ chối khi đĩa giả lập gần đầy; một bài hẹn giờ của WordPress được đăng đúng giờ khi không có khách nào vào site |
| **M10** | Chuyển LEMP ⇄ LAMP | e2e bước 8; đo được thời gian gián đoạn |
| **M11** | `install.sh` (nhận file chạy có sẵn trên máy, hoặc tải từ bản phát hành), bản phát hành trên GitHub, README có ảnh, rà hai ngôn ngữ | cài lên máy ảo sạch bằng một lệnh rồi tạo được site. Repo đang để riêng tư nên lệnh tải thẳng từ GitHub chỉ chạy được sau khi repo mở công khai; trước đó chép file chạy sang máy đích rồi chạy `install.sh` tại chỗ |
| **M12** | Phiên bản và cập nhật: nhiều bản PHP theo site, chọn nguồn nginx và dòng MariaDB lúc cài, màn Cập nhật, bản vá bảo mật tự động, nâng MariaDB, `tpan self-update` | e2e bước 14 và 15; nâng MariaDB trên một bản sao của máy ảo, dữ liệu của các site còn nguyên |

M1 đến M3 là phần bắt buộc của đề. Từ M4 trở đi các mốc độc lập với nhau (riêng M8 cần cổng MCP của M7, và M12 cần health của M4), nên thứ tự trên đổi được. Thiếu thời gian thì bỏ bớt mốc; không làm dở nhiều mốc cùng lúc.

Để sau: tách đặc quyền, clone site sang staging, đọc trạng thái pool PHP-FPM qua FastCGI, quản lý nhiều máy, gửi thông báo ra ngoài (Discord, email), cron chạy lệnh tuỳ ý, biểu đồ thống kê dài ngày.

## 14. Quy ước làm việc

- Tên sản phẩm viết là **TPAN**. Tên lệnh, file chạy, thư mục, gói Go và repo viết thường: `tpan`.
- Nhánh: phát triển trên `dev` hoặc `feature/<tên>`; chỉ merge vào `main` khi `make check` và e2e xanh.
- Commit nhỏ, một ý, tiếng Việt, dạng `engine: hoàn tác theo thứ tự ngược khi bước lỗi`.
- Chú thích trong mã bằng tiếng Việt, nói **vì sao**, không nhắc lại mã làm gì.
- Lỗi luôn kèm ngữ cảnh: `fmt.Errorf("ghi vhost %s: %w", domain, err)`.
- Không thêm thư viện khi thư viện chuẩn làm được. Thêm thư viện nào thì ghi lý do vào bảng ở mục 3.
- Không có mật khẩu, IP thật, tên miền nội bộ, tên công ty hay tên người trong repo. Ví dụ dùng `example.com`.
- Lệnh nguy hiểm (xoá, dừng dịch vụ, đổi SSH, tường lửa) phải có `Danger: true` và giao diện phải hỏi lại.
- Mọi thứ chạy trên máy ảo trước. Lệnh trên VPS do người chịu trách nhiệm tự gõ sau khi đã thấy nó chạy đúng trên máy ảo.
- Repo để riêng tư (private) từ ngày 19/11/2026. Người review xem bằng cách được mời làm collaborator, hoặc chủ dự án mở công khai trước buổi review.

## 15. Tài liệu giải thích

`docs/giai-thich/` có một file cho mỗi mốc, viết cho người chưa biết Go và mới học Linux. Mỗi file theo đúng khung:

1. Mốc này làm được gì (3 đến 5 dòng).
2. Chạy thử: lệnh, kèm output thật đã chạy trên máy ảo.
3. Đi qua mã theo thứ tự chạy: file nào, hàm nào, vì sao viết vậy.
4. Kiến thức nền cần có (Linux, web server, Go), mỗi khái niệm gắn với đúng một chỗ trong mã.
5. Đã thử cách nào khác và vì sao không chọn.
6. Câu hỏi tự kiểm, có đáp án ở cuối file.

Thêm `docs/giai-thich/00-go-can-ban.md`: những phần của Go mà TPAN dùng (package, struct, interface, error, context, goroutine, embed), mỗi phần minh hoạ bằng mã lấy từ chính repo.

Output dán vào tài liệu phải là output thật. Chưa chạy thì ghi "chưa chạy", không viết output theo dự đoán. Log đầy đủ của lần chạy e2e của từng mốc lưu ở `docs/bang-chung/M<n>.log`.

## 16. Điều chưa chắc, phải kiểm trên máy ảo

- Gói `apache2` có tự khởi động khi cài không, và `policy-rc.d` có chặn được không.
- WP-CLI nhận mật khẩu qua stdin với `--prompt=dbpass` và `--prompt=admin_password` như thế nào.
- Bản Ubuntu 24.04 của Multipass khác bản "minimized" trên VPS ở những gói nào (`dpkg -l | wc -l` hai bên, so danh sách).
- MariaDB bản của Ubuntu 24.04 có sẵn user ẩn danh và DB `test` không.
- Phiên bản và cập nhật (mục 6.11), kiểm trên máy ảo trước khi viết M12:
  - Kho PHP của Ondřej Surý cho Ubuntu 24.04 hiện nằm ở đâu và ký bằng khoá nào. Tra ngày 19/11/2026 thấy kho trên Launchpad (`ppa:ondrej/php`) đang được chuyển về `packages.sury.org`; đọc hướng dẫn hiện hành của chính kho đó.
  - Gói nginx từ nginx.org khác gói của Ubuntu ở bố cục thư mục (`conf.d` thay cho `sites-enabled`) và ở user chạy tiến trình (`nginx` thay cho `www-data`). Quyền thư mục site ở mục 5 dựa vào nhóm `www-data`, nên phải thử kỹ trước khi cho chọn nguồn này.
  - Nâng MariaDB từ dòng của Ubuntu (10.11) lên một dòng LTS mới hơn bằng kho của MariaDB: các bước thật, thời gian dừng, và `mariadb-upgrade` báo gì.
  - Đọc danh sách bản đang chờ cập nhật bằng lệnh nào để tách được bản vá bảo mật (`apt-get -s`, `apt list --upgradable`), và dịch vụ nào phải khởi động lại sau khi cập nhật.
  - API của WordPress.org về bản PHP khuyên dùng và nguồn của PHP về hạn hỗ trợ trả về gì; panel vẫn phải chạy đủ khi không gọi được hai nơi này.
  - `wp config set` có cách nào nhận giá trị mà không để lộ trên dòng lệnh không (dùng cho đổi mật khẩu DB ở mục 6.9).
- 80 MB cho một worker PHP của WordPress có sát thực tế không.
- `rename` giữa `/var/www` và `/var/lib` có cùng phân vùng trên VPS đích không (`df /var/www /var/lib`).
- Máy ảo test là arm64, VPS là amd64: gói cài giống nhau, nhưng phải chạy lại e2e trên VPS ở lần triển khai đầu.
- API của SDK MCP Go: đọc tài liệu của đúng phiên bản thêm vào, không viết theo trí nhớ.
- Che dữ liệu có làm AI chẩn đoán kém đi không: đo bằng các kịch bản trong `scripts/faults/`, có che và không che.
- Quy định của nơi triển khai về việc gửi dữ liệu máy chủ cho AI bên ngoài: phải được đồng ý trước khi bật AI trên máy có dữ liệu thật.
- Claude Code chạy không giao diện: phải thử trên máy cá nhân trước khi viết `claudecode`, và ghi nguyên văn sự kiện `init` của từng lần thử vào `docs/bang-chung/`:
  - `--setting-sources ""` có được chấp nhận không. Nếu không thì giữa `--safe-mode` và `--restricted`, cờ nào vừa bỏ được hook, plugin, skill, CLAUDE.md và bộ nhớ riêng của người dùng, vừa giữ đăng nhập bằng gói Claude, vừa vẫn nạp MCP server từ `--mcp-config`.
  - Với `--tools ""` và một MCP server, danh sách công cụ trong sự kiện `init` còn những gì ngoài công cụ của `tpan`. Danh sách ngoại lệ ở bước kiểm cửa (mục 8.2) lấy từ kết quả này.
  - `claude auth status` in ra gì trên máy đã đăng nhập bằng gói Claude (trường `authMethod`).
  - Một lượt `claude -p` có MCP qua SSH mất bao lâu từ lúc gọi tới chữ đầu tiên. Nếu chậm vì mỗi lượt mở thêm kết nối SSH thì bật `ControlMaster` trong cấu hình SSH của máy cá nhân.
  - Khi một bản Claude Code mới đặt `--bare` làm mặc định cho `-p`: có cờ nào tắt được không. Nếu không, `tpan brain` ghim phiên bản và báo rõ.
- Hạn mức của gói Claude khi dùng `claude -p` và điều khoản dùng đăng nhập gói (mục 8.2): đã đổi vài lần trong năm 2026, kiểm lại ở hai trang ghi trong mục 17 trước buổi demo.
- SSH từ máy cá nhân vào máy ảo Multipass (cần cho `tpan mcp` và `tpan brain` lúc test): thêm khoá công khai của máy cá nhân vào máy ảo bằng một lệnh con mới của `scripts/vm.sh`.

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
- Các bản PHP đang được hỗ trợ: https://www.php.net/supported-versions.php
- Kho PHP nhiều phiên bản của Ondřej Surý: https://deb.sury.org/
- Gói nginx chính thức cho Linux (stable và mainline): https://nginx.org/en/linux_packages.html
- Kho gói của MariaDB và cách chọn dòng: https://mariadb.com/docs/server/server-management/install-and-upgrade-mariadb/mariadb-package-repository-setup-and-usage
- Chính sách bảo trì các dòng MariaDB: https://mariadb.org/about/#maintenance-policy
- Yêu cầu máy chủ của WordPress theo từng phiên bản: https://make.wordpress.org/hosting/handbook/compatibility/
- Phụ thuộc của gói `php8.3` trên Ubuntu 24.04: https://packages.ubuntu.com/noble/php8.3
- Các câu SQL của `mysql_secure_installation` (MariaDB 10.11), dùng cho bước bảo vệ MariaDB: https://github.com/MariaDB/server/blob/10.11/scripts/mysql_secure_installation.sh
- Bản phát hành và file sha512 của WP-CLI: https://github.com/wp-cli/wp-cli/releases
- SDK Go chính thức của MCP: https://github.com/modelcontextprotocol/go-sdk
- Thêm MCP server vào Claude Code: https://code.claude.com/docs/en/mcp
- Thêm MCP server vào Codex: https://developers.openai.com/codex/mcp
- Cài câu lệnh vào log để dẫn dắt AI agent: https://www.eye.security/blog/log-poisoning-openclaw-ai-agent-injection-risk
- Đo hiệu quả các biện pháp chống tấn công qua log: https://www.alphaxiv.org/abs/2605.24421
- Chạy Claude Code không giao diện (`claude -p`, `--bare`, luồng stream-json): https://code.claude.com/docs/en/headless
- Các cờ dòng lệnh của Claude Code: https://code.claude.com/docs/en/cli-reference
- Đăng nhập Claude Code và thứ tự ưu tiên giữa các cách đăng nhập: https://code.claude.com/docs/en/authentication
- Điều khoản dùng đăng nhập gói Claude: https://code.claude.com/docs/en/legal-and-compliance
- Hạn mức của gói Claude với `claude -p`: https://support.claude.com/en/articles/15036540-use-the-claude-agent-sdk-with-your-claude-plan
