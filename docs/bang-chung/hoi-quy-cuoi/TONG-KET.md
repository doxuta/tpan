# Hồi quy cuối: chạy lại toàn bộ e2e trên bản cuối

Ngày chạy: 20/11/2026, khoảng 02:49 tới 03:45 (giờ máy Mac, +07). Máy ảo `tpan-test`
(Multipass, Ubuntu 24.04, arm64). Nhánh `hoi-quy-cuoi`, tách từ `dev` ở commit 31b21c7.

Mỗi kịch bản tự bắt đầu bằng `make vm-reset` (máy sạch) và `make vm-deploy`
(build từ cây hiện tại). Kịch bản chạy lần lượt, không chồng nhau. Log nằm cạnh
file này, tên `M<n>.log`; lần chạy đầu có khẳng định không đạt được giữ lại với
tên `M<n>-lan1.log`. Mỗi log tự tìm bí mật, IP, thư mục nhà trong chính nó ở
cuối lần chạy; sau đó một bước che thêm IP của mạng Multipass nếu kịch bản bỏ
sót (chỉ `M1.log` cần, hai dòng `curl http://<vm>/`).

Không có lỗi thật của tpan. Bốn khẳng định không đạt ở lần đầu đều là lỗi của
kịch bản: khẳng định viết trước khi một mốc sau đổi hành vi có chủ ý, hoặc
ngưỡng thời gian phụ thuộc mạng. Mã Go và giao diện không sửa gì. `make check`
xanh trên nhánh này sau khi sửa kịch bản.

## Kết quả

| Mốc | Lệnh | Đạt | Không đạt | Thời gian | Log |
|---|---|---|---|---|---|
| M1 | `scripts/e2e-m1.sh all` | 104 | 0 | 1 phút 44 giây | `M1.log` |
| M2 | `scripts/e2e-m2.sh all` | 276 | 0 | 3 phút 15 giây | `M2.log` |
| M3 | `scripts/e2e-m3.sh all` | 151 | 0 | 1 phút 37 giây | `M3.log` (lần đầu 150/1: `M3-lan1.log`) |
| M4 | `scripts/e2e-m4.sh all` | 313 | 0 | 7 phút 19 giây | `M4.log` |
| M5 | `scripts/e2e-m5.sh all` | 256 | 0 | 4 phút 18 giây | `M5.log` |
| M6 | `scripts/e2e-m6.sh all` | 167 | 0 | 4 phút 4 giây | `M6.log` |
| M7 | `scripts/e2e-m7.sh` (không có chế độ khác) | 99 | 0 | 1 phút 29 giây | `M7.log` (lần đầu 96/2: `M7-lan1.log`) |
| M8 | `scripts/e2e-m8.sh fake` (claude giả, mặc định) | 106 | 0 | 1 phút 20 giây | `M8.log` (lần đầu 105/1: `M8-lan1.log`) |
| M9 | `scripts/e2e-m9.sh all` | 128 | 0 | 4 phút 3 giây | `M9.log` |
| M10 | `scripts/e2e-m10.sh all` | 279 | 0 | 9 phút 43 giây | `M10.log` |
| M11 | `scripts/e2e-m11.sh all` | 127 | 0 | 2 phút 24 giây | `M11.log` |
| M12 | `scripts/e2e-m12.sh all` | 175 | 0 | 6 phút 35 giây | `M12.log` |

Tổng: 2181 khẳng định đạt, 0 không đạt trên lần chạy cuối của từng kịch bản.
`scripts/e2e-m8.sh real` (gọi `claude -p` thật) không chạy.

M1, M2 chạy trước ba lần sửa kịch bản dưới đây; ba lần sửa chỉ đụng
`e2e-m3.sh`, `e2e-m7.sh`, `e2e-m8.sh`, không đụng mã của tpan, nên file chạy
được build từ cùng một mã nguồn cho cả 12 kịch bản.

## Khẳng định đã sửa (lỗi của kịch bản, không phải của tpan)

| Kịch bản | Khẳng định cũ | Vì sao không còn đúng | Sửa | Commit |
|---|---|---|---|---|
| `scripts/e2e-m3.sh` | "SSE: log trực tiếp: luồng kéo dài trên 10 giây" | Job tạo site lần này xong trong 9,8 giây (lần 19/11: 15 giây); thời gian phụ thuộc mạng lúc tải WordPress, không phải hành vi của tpan. Dấu hiệu luồng là trực tiếp đã có ở khẳng định kế bên: hơn 20 sự kiện tới trước `job.done` ít nhất 2 giây (lần này 128 sự kiện) | Ngưỡng hạ xuống 5 giây, kèm chú thích lý do | 6404a28 |
| `scripts/e2e-m7.sh` | "đủ 18 công cụ đúng tên của danh mục (15 của M7, 3 của M8)" | M9 thêm `list_backups`, `propose_backup_create`, `propose_backup_restore` vào danh mục (commit e51d621); khẳng định viết trước khi ghép M9 | Danh sách 21 tên | a37555e |
| `scripts/e2e-m7.sh` | "không còn gì của b.example.com" sau `propose_site_remove` rồi `apply_plan` | Từ M6, xoá site là đưa vào thùng rác (mục 6.4): user Linux, DB, user DB (bị khoá), file vhost ở `sites-available`, dòng `sites` còn lại có chủ ý. Khẳng định viết trước khi ghép M6 | Hai khẳng định mới: `/var/www/<d>`, pool, socket, link `sites-enabled` không còn; thư mục nằm ở `/var/lib/tpan/trash/<slug>` và dòng `sites` có `status trashed` | a37555e |
| `scripts/e2e-m8.sh` | "claude giả thấy 18 công cụ mcp__tpan__*" | Như trên, M9 thêm 3 công cụ | 21 công cụ | 0735b82 |

Các thay đổi có chủ ý mà lời giao việc nêu (M6 thùng rác, M12 tên socket theo bản
PHP, M11 quyền `/var/lib/tpan` 0755, M4 thêm phép kiểm) đã được các kịch bản
M1, M2, M3, M4 theo kịp từ trước: chúng đạt hết ngay lần đầu.

## Sửa mã

Không có. Không thấy lỗi thật nào của tpan trên bản cuối.

## Một số con số đo lại được (mục 16 của KE-HOACH.md)

Các phép đo dưới đây nằm trong chính các kịch bản; lần này cho cùng kết luận với
lần đo của từng mốc.

- M3: khoá toàn cục khi CLI đang chạy job và API cũng gọi: API chờ khoá khoảng
  2 giây rồi trả 409 `job_running`, xem trước và đọc vẫn 200; `GET /api/v1/overview` lần đầu với 2 site (bộ nhớ đệm rỗng):
  0,22 giây.
- M4: `ACCOUNT LOCK` đọc qua `JSON_VALUE(... account_locked)` là 1, sau sửa tự
  động là 0; `scripts/vm.sh root` giữ nguyên đối số có dấu cách; một lần
  `tpan health` đầy đủ với 2 site: 3,5 giây; socket pool bị xoá, chữ báo lỗi
  của `tail`, bộ hẹn giờ bỏ vòng khi CLI giữ khoá: đạt như M4 đã chốt.
- M6: root vẫn `runuser` được dưới user site đã hết hạn (user bị khoá ở thùng rác).
- M7: `sudo -n` qua ssh không tty chạy được; stdout của `tpan mcp` chỉ có
  JSON-RPC, stderr 0 byte; bắt tay mặc định 2026-07-28, ép 2025-06-18 cũng
  chạy; `list_sites` qua SSH cả tiến trình 0,22 tới 0,32 giây.
- M8 (claude giả): từ lúc gửi câu hỏi tới chữ đầu tiên trên SSE 1,34 giây (gồm
  1 giây claude giả cố ý chờ); mở ssh + `tpan mcp --session` + liệt kê công cụ 256 ms.
- M9: `wp cron event run --due-now` khi không có việc thoát 0; cron đọc
  file mới trong `/etc/cron.d` không cần reload; opcache sau đổi mật khẩu DB.
- M10: bước `web.swap` 95 ms (nginx sang apache) và 107 ms (apache sang nginx);
  mỗi cặp site và scheme mất 1 tới 3 request (curl mã 7), gián đoạn dài nhất
  98 tới 176 ms; `policy-rc.d` khi cài lại gói nginx của Ubuntu và của
  nginx.org, `nginx -t` và `apachectl configtest` khi web server kia giữ cổng.
- M12: đổi bản PHP của site 1 trong lúc vòng curl gọi liên tục: 0 request lỗi
  (hai lần); nâng MariaDB 10.11 lên 11.8 trên bản sao của máy ảo: mariadb dừng
  23,0 giây, dữ liệu các site còn nguyên.

## Việc còn dở

- `scripts/e2e-m8.sh real` (Claude Code thật) không chạy: tốn hạn mức gói Claude,
  cần chủ dự án cho phép.
- Kiểm bằng trình duyệt (bước 4 của M3, giao diện hai ngôn ngữ) không nằm trong
  lần hồi quy này.
- Các phần chỉ chạy được trên VPS (mục 18) vẫn là việc của chủ dự án.

Máy ảo `tpan-test` đã được đưa về snapshot `clean` sau lần chạy cuối.
