# Kiểm thử toàn bộ trước khi phát hành

Ngày chạy: 20/11/2026, 09:43 tới 11:14 (giờ Mac, +07). Máy ảo `tpan-test`
(Multipass, Ubuntu 24.04, arm64). Nhánh `dev`.

Mỗi kịch bản tự bắt đầu bằng `make vm-reset` (máy sạch) và `make vm-deploy`
(build từ cây mã hiện tại), rồi chạy lệnh thật trong máy ảo và kiểm kết quả
bằng lệnh độc lập. Kịch bản chạy lần lượt bằng `scripts/hoi-quy.sh`; tóm tắt
từng lần chạy nằm ở `tom-tat.txt`, log đầy đủ ở `<tên>.log` cạnh file này. Mỗi
log tự quét ở cuối lần chạy: không chứa mật khẩu, IP máy ảo, IP của Mac, thư
mục nhà, cookie. Sau đó che thêm IP mạng Multipass mà vài kịch bản bỏ sót
(`M1.log`, `M4.log`: `<vm>`, `<vm-broadcast>`); `gitleaks dir` trên log và file
cấu hình không thấy bí mật nào.

## Kết quả cuối

| Kịch bản | Phần được kiểm | Đạt | Không đạt | Log |
|---|---|---|---|---|
| M1 | cài stack nginx, PHP, MariaDB; chạy lại không đổi gì | 104 | 0 | `M1.log` |
| M2 | tạo site WordPress, cô lập giữa các site, xoá site | 276 | 0 | `M2.log` |
| M3 | panel web: đăng nhập, CSRF, header bảo mật, tạo và xoá site qua API, log trực tiếp | 151 | 0 | `M3.log` |
| M4 | health theo đường đi request, sự cố, 9 kịch bản gây lỗi và sửa tự động, lệch file | 313 | 0 | `M4.log` |
| M5 | SSL với máy chủ ACME thử, gia hạn thử, ufw, fail2ban, trang bảo mật | 256 | 0 | `M5.log` |
| M6 | thùng rác (xoá, khôi phục, xoá hẳn), thao tác hàng loạt `apply -f` | 167 | 0 | `M6.log` |
| M7 | MCP qua SSH: 21 công cụ, Plan chờ duyệt, che tên miền | 99 | 0 | `M7.log` |
| M8 | AI trong panel, bộ não với claude giả | 106 | 0 | `M8.log` |
| M8 thật | AI trong panel với Claude Code thật trên Mac (`claude -p`) | 63 | 0 | `M8-real.log` |
| M9 | backup, khôi phục, cron, DB: đổi mật khẩu, xuất, nhập | 128 | 0 | `M9.log` |
| M10 | đổi nginx sang apache và ngược lại, gián đoạn đo được, khởi động lại máy | 279 | 0 | `M10.log` |
| M11 | cài bằng `install.sh`, gói hỏng thì dừng, `curl \| bash`, `self-update`, quay về | 127 | 0 | `M11.log` |
| M12 | nhiều bản PHP, cập nhật gói, cập nhật tự động, nâng MariaDB trên bản sao | 175 | 0 | `M12.log` |
| API | mọi route API giao diện dùng mà các kịch bản trên chưa gọi (mới, `scripts/e2e-api.sh`) | 402 | 0 | `API.log` |

**Tổng: 2646 khẳng định đạt, 0 không đạt.** `M8 thật` gọi Claude Code thật
(tốn hạn mức gói Claude của chủ dự án); lần hồi quy trước (cùng ngày, thư mục
`hoi-quy-cuoi/`) đã bỏ phần này.

## Độ phủ

- **CLI**: 45 lệnh con của `tpan`. Mỗi lệnh có ít nhất một kịch bản chạy thật
  trên máy ảo, trừ `tpan db list` (phần đọc đó được kiểm qua `GET /api/v1/databases`
  ở M3, cùng hàm lõi).
- **API**: 67 route trong `internal/server/server.go`. Trước đợt này 32 route
  chưa có kịch bản nào gọi (các nút trên giao diện: chạy lại health, sửa sự cố,
  duyệt và từ chối Plan, đổi web server, cài và gỡ PHP, đổi bản PHP của site,
  khôi phục backup, lịch backup, đổi mật khẩu, xuất và nhập DB, cập nhật, đổi
  mật khẩu quản trị, huỷ phiên, xem trước AI...). `scripts/e2e-api.sh` phủ hết:
  giờ 67/67 route được gọi thật. Mỗi route ghi: xem trước (`?dry_run=1`) không
  đổi máy, chạy thật, nghe job qua SSE tới `job.done`, rồi kiểm trạng thái máy
  bằng lệnh độc lập (`systemctl`, `dpkg`, `mariadb`, `stat`, `curl`).
- **Cấu hình thật**: `scripts/lay-cau-hinh.sh` chép nguyên văn các file tpan ghi
  lên máy (vhost nginx và apache, pool PHP-FPM, `wp-config.php` đã che bí mật,
  cron, logrotate, fail2ban, ufw, unit systemd, quyền thư mục) vào
  `docs/bang-chung/cau-hinh/` (25 file, máy ảo sạch, site `a.example.com`, nginx rồi
  apache; mật khẩu DB và mọi khoá, muối của WordPress đã che). Chạy lại được:
  `scripts/lay-cau-hinh.sh`.

## Lỗi thật tìm được và đã sửa

| Lỗi | Thấy ở đâu | Sửa | Commit |
|---|---|---|---|
| `tpan brain` không chuyển `SSH_AUTH_SOCK` cho ssh và claude: khoá SSH có passphrase (chỉ nằm trong ssh-agent) bị từ chối "Permission denied (publickey)" dù gõ `ssh` tay vào được | chủ dự án chạy M8 với Claude Code thật (`docs/bang-chung/M8-claude-mac.log`) | thêm `SSH_AUTH_SOCK` vào danh sách biến chuyển tiếp, test `TestBrainEnvSSHAgent` | 27b29d5 |
| Ga DNS và TLS trên bản đồ tuyến luôn "chưa kiểm được": site chưa xin SSL không có phép kiểm DNS hay TLS nào (việc M3 để lại) | chủ dự án nhìn giao diện | health thêm `dns.a` (phép kiểm nặng) và `tls.cert` cho mọi site; site chưa SSL là cảnh báo kèm cách sửa (bản ghi A trỏ về IP nào; lệnh và liên kết xin chứng chỉ); site SSL mất chứng chỉ có sửa tự động `ssl.issue` | 40e65c2 |
| `tpan stack install` lỗi ngay "Could not get lock /var/lib/dpkg/lock-frontend" khi máy vừa khởi động (unattended-upgrades của Ubuntu đang giữ khoá dpkg): bước cài gói của M1 thiếu `-o DPkg::Lock::Timeout=300` mà các bước apt từ M10, M12 đều có. Trên VPS vừa tạo, `install.sh --web nginx` gặp đúng lỗi này. Các kịch bản e2e không gặp vì chờ máy ổn định trước | `scripts/lay-cau-hinh.sh` chạy `stack install` ngay sau khi máy ảo khởi động | bước `AptInstall` chờ khoá dpkg tối đa 5 phút như các bước apt khác | b51ac5a |
| Bản đầu của sửa trên cho site SSL thiếu bản ghi A là lỗi: `tpan stack switch` thành công vẫn thoát 1 vì bước "kiểm lại toàn bộ health sau job" thấy lỗi DNS | e2e M10 lần đầu | DNS luôn là cảnh báo (tpan không sửa được DNS); chứng chỉ không gia hạn được thì `tls.cert` báo lỗi đúng lúc | a2dc540 |

## Khẳng định của kịch bản đã sửa (không phải lỗi của tpan)

Lần chạy đầu của các kịch bản này có khẳng định không đạt; log đầu đã bị lần
chạy lại ghi đè, tóm tắt còn ở `tom-tat.txt`.

| Kịch bản | Lần đầu | Vì sao | Sửa |
|---|---|---|---|
| M3, M10, M11 | 1, 12, 5 không đạt | Khẳng định "mọi phép kiểm pass"; sau sửa DNS/TLS, site trên máy ảo có đúng hai cảnh báo có chủ ý (tên miền thử không có bản ghi A thật, site chưa xin SSL) | Nhận đúng hai cảnh báo `dns.a`, `tls.cert`; mọi phép kiểm khác vẫn phải pass |
| M8 | 1 không đạt | claude giả báo bản 2.1.328 trong khi bản đã kiểm vừa nâng lên 2.1.329 | claude giả lấy bản từ `claudecode.TestedVersion`, không còn lệch khi nâng |
| API | 1 không đạt (lần chạy đầu tiên của kịch bản) | Kịch bản chờ giờ cron `3`, tpan ghi `03` (cron hiểu như nhau) | Nhận cả hai |
| M8 thật | dừng có chủ ý sau 39 giây | Kịch bản bị sửa trong lúc đang chạy (bash đọc kịch bản dần) | Chạy lại từ đầu |

## Ghi chú khi đọc kết quả

- **CSRF**: request ghi thiếu cả `Origin` lẫn `Sec-Fetch-Site` được cho qua; đó
  là cách `http.CrossOriginProtection` của Go coi request không phải từ trình
  duyệt (vẫn cần cookie đăng nhập). Request cross-site, same-site, Origin lạ đều
  403 (`API.log`, phần 4).
- **Route không có bước xem trước**: duyệt, từ chối Plan, đặt cấu hình SSL và
  thùng rác, chạy health, tài khoản, xem trước AI làm ngay khi gọi (không đi qua
  Plan). Kịch bản kiểm kết quả sau khi làm.
- **`/mariadb/upgrade` qua API chỉ chạy xem trước**: nâng là một chiều và mất
  nhiều phút; lần nâng thật trên bản sao nằm ở M12.
- M1 tới M7 và M9 chạy trước commit a2dc540 (DNS luôn là cảnh báo). Commit đó
  chỉ nới DNS từ lỗi xuống cảnh báo cho site SSL, nên các kịch bản đã đạt khi
  còn chặt vẫn đạt; M8, M10, M11, M12, API chạy trên bản cuối.

## Chưa kiểm được ở đây

- Phần cần VPS và tên miền thật: chứng chỉ Let's Encrypt staging cho một tên
  miền thật, `install.sh` trên máy amd64 (máy ảo là arm64). Hướng dẫn từng bước
  ở `docs/HUONG-DAN-CHU-DU-AN.md` mục 3.
- Giao diện: chủ dự án đã nhìn bằng mắt (20/11/2026); rà tự động 24 màn ở bốn
  tổ hợp nằm ở `docs/bang-chung/ra-giao-dien.md`.
