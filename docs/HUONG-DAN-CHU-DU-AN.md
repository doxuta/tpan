# Việc chủ dự án tự làm: hướng dẫn từng bước

Cả 14 mốc (M0 đến M13) đã chạy trọn trên máy ảo. Còn bốn nhóm việc agent không được làm thay, vì luật của repo (AGENTS.md) cấm hoặc vì cần mắt người:

| Nhóm | Vì sao agent không làm | Cần gì | Thời gian |
|---|---|---|---|
| 1. Nhìn giao diện bằng mắt | Agent chỉ đo được bằng máy; cảm nhận màu, chữ, VoiceOver cần người | Mac | 45 phút |
| 2. Claude Code thật (M7, M8) | Chạy `claude` tốn hạn mức gói của bạn (luật 9) | Mac, máy ảo `tpan-test`, `claude` đã đăng nhập | 30 phút |
| 3. VPS và tên miền thật | Agent không được chạy gì trên VPS (luật 1) | VPS Ubuntu 24.04 amd64 sạch, một tên miền | 2 giờ |
| 4. Phát hành và mở repo công khai | Cần bạn quyết (luật 8) | Tài khoản GitHub | 20 phút |

Nên làm theo thứ tự 1, 2, 3, 4: việc trước không cần máy chủ và dễ quay lại; việc 4 không đảo ngược được hoàn toàn (đã công khai thì có thể đã bị sao chép).

Quy ước trong file này:

- `<domain>` là tên miền thật của bạn, `<IP>` là IP của VPS, `<user>` là tài khoản SSH trên VPS. Chỉ gõ giá trị thật vào terminal, **không** chép vào repo.
- Mỗi khối lệnh có ghi nó chạy **ở đâu** (Mac, máy ảo, VPS) và **trong thư mục nào**.
- Lệnh trên Mac chạy trong thư mục repo: `cd ~/tpan`.

---

## 1. Nhìn giao diện bằng mắt (trên Mac, không cần máy chủ)

### 1.1. Chạy panel chế độ demo

Panel demo dùng máy giả và bộ não AI giả: không đụng gì thật, không tốn hạn mức Claude. Mở hai terminal, cả hai ở `~/tpan`:

```bash
make dev-api
```

```bash
make dev-web
```

Mở http://localhost:5173. Mật khẩu demo in ở terminal của `make dev-api`: của `admin`, và từ M13 của hai khách hosting `khach_a`, `khach_b`. Đăng nhập lần lượt bằng admin rồi bằng `khach_a` để thấy hai kiểu panel: khách chỉ thấy site, DB, job của mình và hạn mức gói thay cho số liệu máy. Với admin, thử màn **Người dùng** (nút "Xem như user này" hiện dải màu và mọi nút đổi máy bị từ chối), màn **Gói**, nút **Tạm khoá** ở Site và Cơ sở dữ liệu, và site `lab.example.org` đang tạm khoá (tuyến xám trên bản đồ).

Mở luôn các trang mẫu để so (bản đã chốt, giao diện phải giống):

```bash
open docs/mau-giao-dien/bang-dieu-khien-sang.html docs/mau-giao-dien/bang-dieu-khien-toi.html docs/mau-giao-dien/ngan-ai-sang.html
```

### 1.2. Đi một vòng, mỗi màn bốn kiểu

Mỗi màn xem ở: **sáng tiếng Việt**, **tối tiếng Việt**, **sáng tiếng Anh**, và **hẹp 390 px**. Nút Sáng/Tối và VI/EN ở thanh đầu trang. Thu về 390 px: trong Chrome bấm `Cmd+Option+I` (DevTools) rồi `Cmd+Shift+M` (chế độ thiết bị), chọn chiều rộng 390.

Danh sách màn nên xem:

- Bảng điều khiển: so từng chi tiết với `bang-dieu-khien-sang.html` và `-toi.html` (hàng tuyến, kẻ, cỡ chữ, màu mã HTTP).
- Sites, trang một site, tạo site (xem trước Plan rồi chạy, theo dõi từng bước).
- Sự cố, Health, một job lỗi (nút "Giải thích").
- Plans: danh sách, một Plan ở ba trạng thái chờ, đã chạy, đã từ chối.
- Backup, Cron, Thùng rác, PHP, Cập nhật, Cài đặt (khối bộ não, Agent và MCP).
- Ngăn AI: so với `ngan-ai-sang.html`. Bỏ từng nhãn ngữ cảnh, bấm "xem trước thứ sẽ gửi", hỏi một câu, xem chữ hiện dần.
- Thanh lệnh: `Cmd+K`, gõ, mũi tên, Enter, Esc.
- Khối lệnh: bấm biểu tượng chép phải hiện dấu ✓; dán vào terminal không có dấu `$` ở đầu; lệnh cần quyền root có `sudo`.

Ở màn 390 px để ý: không có thanh cuộn ngang cả trang, lệnh dài xuống dòng, nút đủ to để bấm bằng ngón tay.

### 1.3. Chế độ xám (kiểm không dựa vào màu)

Người mù màu không phân biệt được đỏ và xanh. Trạng thái (đạt, lỗi, đang chạy) phải đọc được bằng **chữ hoặc ký hiệu**, không chỉ bằng màu.

1. Mở **System Settings → Accessibility → Display → Color Filters**.
2. Bật **Color filters**, chọn **Grayscale**.
3. Đi lại Bảng điều khiển, Health, một job lỗi, Sự cố. Câu hỏi cho mỗi màn: không nhìn màu, mình có biết cái nào đang lỗi không?
4. Tắt filter khi xong.

Mẹo: ở **Accessibility → Shortcut** chọn "Color Filters" để bật tắt bằng `Option+Cmd+F5`.

### 1.4. VoiceOver (trình đọc màn hình)

Dùng **Safari** (VoiceOver hợp Safari nhất).

1. Bật: `Cmd+F5`. Tắt cũng bằng `Cmd+F5`.
2. `VO` là `Control+Option`. Các phím chính:
   - `Tab` / `Shift+Tab`: đi qua nút, liên kết, ô nhập.
   - `VO+→` / `VO+←`: đọc từng phần tử.
   - `VO+U`: mở "rotor", chọn Headings, Links, Form Controls để xem cả trang có gì.
3. Thử:
   - Menu trái: mục đang mở được đọc là "current page".
   - Mỗi nút đọc được tên (không có nút nào chỉ là "button").
   - Bấm một chữ mono (domain, đường dẫn) để chép: VoiceOver đọc "Đã chép".
   - Chạy một job: trạng thái từng bước được đọc khi đổi.
   - Mở ngăn AI bằng bàn phím, đóng bằng `Esc`, focus quay về nút đã mở nó.
   - Hộp xác nhận: focus nằm trong hộp, `Esc` đóng.

### 1.5. Ghi lại

Chỗ nào lệch: chụp màn (`Cmd+Shift+4`, kéo chọn vùng), ghi màn nào, kiểu nào (sáng/tối, VI/EN, 390 px), rồi gửi mình. Mình sửa và chụp lại cho bạn so.

---

## 2. Claude Code thật trên Mac (M7 và M8, dùng máy ảo)

Cần: `claude` trên Mac đã đăng nhập (gõ `claude` một lần nếu chưa), máy ảo `tpan-test` đang chạy. Mỗi câu hỏi tốn một ít hạn mức gói Claude của bạn.

### 2.1. Chuẩn bị máy ảo (trên Mac, `~/tpan`)

```bash
scripts/e2e-m7.sh
```

Kịch bản này tự đưa máy ảo về sạch, cài stack và tạo site `a.example.com` trong máy ảo (chạy vài phút, cuối cùng in số khẳng định đạt). Sau đó cho khoá SSH của bạn vào máy ảo và lấy IP:

```bash
scripts/vm.sh ssh-key ~/.ssh/id_ed25519.pub
```

```bash
scripts/vm.sh ip
```

Lệnh này in IP của máy ảo, dạng `192.168.x.y`. Các lệnh dưới ghi `<IP máy ảo>`: gõ thẳng số đó vào. Đừng dùng biến shell kiểu `$IP`: biến chỉ sống trong đúng cửa sổ terminal đã gán nó, sang tab khác nó rỗng và lệnh thành `ssh ubuntu@` (lỗi `CONNECTION_CLOSED` trong Claude Code).

Nhận khoá máy ảo một lần. Claude Code mở ssh không có terminal; gặp máy lạ ssh sẽ dừng với "Host key verification failed", nên phải nhận trước. Gõ `yes` khi được hỏi:

```bash
ssh ubuntu@<IP máy ảo> true
```

### 2.2. M7: Claude Code gọi tpan qua MCP

Thêm tpan làm MCP server của Claude Code (trên Mac, `~/tpan`):

```bash
claude mcp add --transport stdio tpan -- ssh ubuntu@<IP máy ảo> sudo -n tpan mcp
```

```bash
claude mcp get tpan
```

Chờ thấy `Status: ✔ Connected`. `mcp add` mặc định ghi cho **thư mục đang đứng**, nên các lệnh `claude` dưới đây phải chạy cũng ở `~/tpan`; chạy ở thư mục khác thì Claude Code không thấy công cụ tpan.

Hỏi liệt kê site:

```bash
claude "Dùng công cụ tpan liệt kê các site trên máy chủ"
```

Chờ thấy: site hiện dưới bí danh `site-a.example` (tên miền thật không được gửi cho AI).

Gây lỗi trong máy ảo rồi để Claude chẩn đoán:

```bash
scripts/vm.sh root rm /etc/nginx/sites-enabled/tpan-a.example.com.conf
```

```bash
scripts/vm.sh root systemctl reload nginx
```

```bash
claude "Dùng công cụ tpan chẩn đoán vì sao site đầu tiên không chạy rồi đề xuất Plan sửa"
```

Chờ thấy: nó nói lỗi ở chặng web server (vhost chưa bật) và có một `plan_id`. **Máy chưa đổi gì**: AI chỉ đề xuất, bạn duyệt.

Duyệt Plan (thay `<id>` bằng số nó đưa):

```bash
scripts/vm.sh root tpan plan list --status pending
```

```bash
scripts/vm.sh root tpan plan approve <id> --yes
```

```bash
scripts/vm.sh root curl -s -o /dev/null -w '%{http_code}\n' --resolve a.example.com:80:127.0.0.1 http://a.example.com/
```

Chờ thấy: `200`. Gửi mình output của `claude --version`, hai câu trả lời của Claude và `scripts/vm.sh root tpan plan show <id>`; mình ghi vào `docs/bang-chung/M7-claude.log`.

Gỡ khi xong:

```bash
claude mcp remove tpan
```

### 2.3. M8: bộ não AI trong panel

Bật bộ não kiểu `bridge` trong máy ảo (trên Mac, `~/tpan`):

```bash
scripts/vm.sh root tpan mcp config --brain bridge
```

```bash
scripts/vm.sh root systemctl restart tpan
```

Máy ảo Multipass cho `ubuntu` dùng sudo không mật khẩu, nên bỏ qua bước sudoers (bước đó chỉ cần trên VPS, mục 3.9).

Build tpan cho macOS (bản trong `bin/tpan-linux-*` là bản Linux, không chạy được trên Mac) rồi nối bộ não. Để cửa sổ này mở:

```bash
make build
```

```bash
bin/tpan brain ubuntu@<IP máy ảo>
```

Terminal in "Bộ não: đã nối" là được. Mỗi câu hỏi từ panel hiện ở đây. `Ctrl+C` để ngắt.

Mở panel của máy ảo: lấy mật khẩu mới (terminal khác, `~/tpan`):

```bash
scripts/vm.sh root tpan admin passwd
```

Rồi mở `https://<IP máy ảo>:8443` (in bằng `scripts/vm.sh ip`), bỏ qua cảnh báo chứng chỉ tự ký, đăng nhập. Thử:

1. Mở site `a.example.com`, mở ngăn AI. Xem nhãn ngữ cảnh, rồi **bấm nút** "xem trước thứ sẽ gửi" (đó là nút, không phải câu để gõ vào ô hỏi): tên miền đã thành `site-a.example`.
2. Gõ vào ô hỏi: "Site này có ổn không?". Chữ hiện dần. Nếu AI đề xuất Plan, nó nằm ở "Chờ bạn duyệt"; máy không đổi tới khi bạn duyệt.
3. Màn **Phiên AI**: xem lại từng lượt, ngữ cảnh đã gửi, các lần gọi công cụ.
4. **Cài đặt**: khối bộ não báo đã nối, có tên Mac và bản `claude`.

Tắt khi xong: `Ctrl+C` ở cửa sổ bộ não, rồi:

```bash
scripts/vm.sh root tpan mcp config --brain off
```

---

## 3. VPS và tên miền thật

### Cách nhanh: một lệnh, không cần tên miền

Trên VPS Ubuntu 24.04 sạch (SSH vào bằng user lúc cài Ubuntu):

```bash
curl -fsSL https://raw.githubusercontent.com/doxuta/tpan/main/scripts/thu-tren-vps.sh | sudo bash
```

`scripts/thu-tren-vps.sh` cài tpan từ bản phát hành mới nhất rồi thử lần lượt: health, tạo site `thu.example.com` (gọi bằng `curl --resolve` ngay trên máy), thùng rác, fail2ban, ufw (tpan tự mở cổng SSH đang dùng), backup và lịch backup, đổi nginx sang apache rồi về, PHP, cập nhật, self-update (xem trước). Cuối cùng in bảng đạt/hỏng. Mật khẩu panel in một lần ở phần cài: lưu lại. Log đã che mật khẩu và mọi IP nằm ở `/root/tpan-thu-che.log`. Đã chạy thử trên máy ảo sạch ngày 20/11/2026: 29 đạt, 0 hỏng. Không làm SSL (cần tên miền thật trỏ về máy): phần đó theo 3.3 bên dưới khi có tên miền.

### 3.0. Chuẩn bị

- Một VPS **Ubuntu 24.04 amd64 sạch** (không có panel khác, không có web đang chạy). Nếu nhà cung cấp có snapshot, chụp một bản ngay sau khi tạo để quay về được.
- Vào được bằng SSH dùng khoá. **Mở sẵn console web của nhà cung cấp** ở một tab: nếu bật tường lửa sai, đó là đường vào duy nhất còn lại.
- Một tên miền (hoặc tên miền con) có bản ghi **A** trỏ về IP của VPS, **không** có bản ghi **AAAA** trỏ chỗ khác. Kiểm trên Mac:

```bash
dig +short A <domain>; dig +short AAAA <domain>
```

Dòng đầu phải là IP của VPS; không có dòng thứ hai (hoặc là IPv6 của chính VPS).

- Thêm một tên miền con để thử thùng rác, ví dụ `thu.<domain>`, cũng trỏ về VPS.

**Ghi lại toàn bộ phiên để gửi mình.** Trên VPS, ngay sau khi SSH vào:

```bash
script -q ~/tpan-vps.log
```

Mọi thứ bạn gõ và output được ghi vào file đó. Gõ `exit` để dừng ghi. Trước khi gửi, che tên miền và IP (trên VPS, thay giá trị thật vào):

```bash
sed -e 's/<domain>/example.com/g' -e 's/<IP>/203.0.113.10/g' ~/tpan-vps.log > ~/tpan-vps-che.log
```

### 3.1. Cài tpan bằng `install.sh` (M11)

Repo đã công khai và có bản phát hành, nên cách gọn nhất là lệnh một dòng ở README (`curl … install.sh | sudo bash -s -- --web nginx`). Dưới đây là cách không cần GitHub: đóng gói trên Mac rồi chép sang.

Trên Mac, `~/tpan`:

```bash
make release
```

```bash
ls dist/release/
```

Lệnh thứ hai in tên bản, gọi là `<bản>`. Chép sang VPS:

```bash
scp -r dist/release/<bản> <user>@<IP>:tpan-release
```

Trên VPS, thư mục nhà:

```bash
cd ~/tpan-release && sudo ./install.sh --web nginx
```

Script tự chọn `tpan-linux-amd64`, kiểm sha256, chạy `tpan setup` rồi cài stack nginx. Cuối cùng nó in:

- địa chỉ panel `https://<IP>:8443`,
- dấu vân tay SHA-256 của chứng chỉ,
- **mật khẩu quản trị, chỉ hiện một lần**: lưu vào trình quản lý mật khẩu ngay.

Mở panel trên trình duyệt. Trình duyệt cảnh báo vì chứng chỉ tự ký: xem chi tiết chứng chỉ, so dấu vân tay SHA-256 với dòng `tpan setup` đã in. Khớp mới bỏ qua cảnh báo và đăng nhập.

Kiểm chạy lại không đổi gì:

```bash
sudo ./install.sh --web nginx
```

Chờ thấy: các bước báo đã đúng, không cài lại.

### 3.2. Tạo site và kiểm sức khoẻ (M4)

Trên VPS:

```bash
sudo tpan site add <domain> --dry-run
```

```bash
sudo tpan site add <domain> --yes
```

```bash
sudo tpan health
```

Chờ thấy: mọi chặng đạt. Gây lỗi rồi để tpan tự sửa:

```bash
sudo systemctl stop php8.3-fpm
```

```bash
sudo tpan health
```

```bash
sudo tpan health --fix
```

```bash
sudo tpan health
```

Chờ thấy: lần thứ hai chặng PHP lỗi và có sự cố; `--fix` cho xem trước rồi hỏi xác nhận; lần cuối đạt lại, sự cố đóng.

### 3.3. Chứng chỉ SSL staging (M5)

Staging là máy chủ thử của Let's Encrypt: cấp chứng chỉ không được trình duyệt tin, nhưng không bị giới hạn số lần. Dùng nó để thử.

```bash
sudo tpan ssl settings --email <email của bạn>
```

```bash
sudo tpan ssl settings
```

Dòng `ACME:` phải là `https://acme-staging-v02.api.letsencrypt.org/directory`.

```bash
sudo tpan ssl issue <domain> --dry-run
```

```bash
sudo tpan ssl issue <domain> --yes
```

Chờ thấy: `Job ...: thành công`, mọi dòng dưới "Kiểm chứng:" là `[đạt]`, kể cả gia hạn thử. Kiểm độc lập:

```bash
sudo certbot certificates
```

```bash
sudo certbot renew --dry-run --no-random-sleep-on-renew --cert-name <domain>
```

```bash
curl -sI http://<domain>/ | head -3; curl -skI https://<domain>/ | head -3
```

Chờ thấy: nơi cấp có chữ "(STAGING)"; `Congratulations, all simulated renewals succeeded`; http trả 301, https trả 200.

### 3.4. Tường lửa ufw và fail2ban (M5)

**Đọc kỹ trước khi làm.** Bật tường lửa mà không mở cổng SSH là tự khoá mình ngoài. Giữ console web của nhà cung cấp mở sẵn.

```bash
sudo sshd -T | grep -i '^port'
```

```bash
sudo tpan security ufw --dry-run
```

Cổng ở dòng "Mở cổng trên tường lửa" phải chứa đúng cổng SSH ở lệnh đầu. Đúng thì:

```bash
sudo tpan security ufw --yes
```

**Đừng đóng phiên SSH đang mở.** Mở một terminal mới trên Mac, SSH vào lại. Vào được mới yên tâm.

```bash
sudo tpan security fail2ban --yes
```

```bash
sudo fail2ban-client status sshd
```

### 3.5. Thùng rác (M6)

Thùng rác chỉ tức thì khi `/var/www` và `/var/lib/tpan` cùng một phân vùng:

```bash
df /var/www /var/lib
```

Hai dòng phải cùng cột `Filesystem`. Khác nhau thì dừng, báo mình.

Thử với site thử, **không phải site có dữ liệu thật**:

```bash
sudo tpan site add thu.<domain> --yes
```

```bash
sudo tpan site rm thu.<domain> --dry-run
```

```bash
sudo tpan site rm thu.<domain> --yes
```

```bash
sudo tpan trash list
```

```bash
sudo tpan site restore thu.<domain> --yes
```

```bash
curl -sI http://thu.<domain>/ | head -1
```

Chờ thấy: sau `rm` site nằm trong `trash list`; sau `restore` trả 200.

### 3.6. Backup và cron (M9)

```bash
df -h /var/www /var/lib/tpan
```

```bash
sudo tpan cron list
```

```bash
sudo tpan backup create --all --yes
```

```bash
sudo tpan backup list
```

```bash
sudo tpan backup settings --keep 5 --at 03:30 --dry-run
```

```bash
sudo tpan backup settings --keep 5 --at 03:30 --yes
```

```bash
sudo tpan health | grep tpan.backup
```

Hôm sau, kiểm lịch đã chạy: `sudo tpan backup list` có bản loại `schedule` mới.

### 3.7. PHP và cập nhật (M12, tuỳ chọn)

Chỉ đọc hoặc có xem trước, an toàn:

```bash
ps -eo user,cmd | grep [p]hp-fpm
```

```bash
sudo tpan php list
```

```bash
sudo tpan update --check
```

```bash
sudo tpan update --dry-run
```

Thử thêm PHP 8.4 (thêm kho sury; tpan đã chặn gửi thống kê về `telemetry.sury.org`):

```bash
sudo tpan php install 8.4 --yes
```

```bash
time php-fpm8.4 -t
```

Trên 2 giây là có chuyện với DNS, báo mình. Đổi site qua lại rồi gỡ:

```bash
sudo tpan php use <domain> 8.4 --yes
```

```bash
sudo tpan php use <domain> 8.3 --yes
```

```bash
sudo tpan php remove 8.4 --yes
```

**Không** nâng MariaDB trên VPS có dữ liệu thật khi chưa có backup trong 24 giờ (tpan tự từ chối).

### 3.8. Đổi web server (M10, tuỳ chọn)

Mỗi lần đổi, mọi site ngừng trả lời khoảng 0,1 đến 0,2 giây.

```bash
sudo tpan stack switch --to apache --dry-run
```

```bash
sudo tpan stack switch --to apache --yes
```

```bash
curl -skI https://<domain>/ | head -1; sudo tpan health
```

```bash
sudo reboot
```

Vào lại, kiểm chỉ apache2 chạy và site vẫn 200, rồi đổi về:

```bash
systemctl is-active apache2 nginx
```

```bash
sudo tpan stack switch --to nginx --yes
```

### 3.9. Bộ não AI với VPS (M8, tuỳ chọn)

Giống mục 2.3, khác hai chỗ. Thứ nhất, trên VPS cho tài khoản SSH chạy đúng ba lệnh tpan bằng sudo không mật khẩu (bỏ qua nếu SSH bằng root):

```bash
sudo visudo -f /etc/sudoers.d/tpan-mcp
```

Ghi đúng một dòng, thay `<user>`:

```
<user> ALL=(root) NOPASSWD: /usr/local/bin/tpan mcp, /usr/local/bin/tpan mcp --session *, /usr/local/bin/tpan brain-link
```

`visudo` kiểm cú pháp trước khi lưu; đừng sửa file sudoers bằng trình soạn khác (sai cú pháp có thể khoá luôn sudo). Dòng này không cho chạy `tpan plan approve`: AI chỉ đề xuất, bạn duyệt.

Thứ hai, trên VPS bật bộ não rồi trên Mac nối tới VPS:

```bash
sudo tpan mcp config --brain bridge && sudo systemctl restart tpan
```

```bash
bin/tpan brain <user>@<IP>
```

### 3.10. Nâng VPS lên bản có nhiều người dùng (M13, khi đã phát hành v0.2.0)

VPS của bạn đang chạy v0.1.1. Bản M13 thêm bảng `users` và chuyển mật khẩu quản trị sang đó; đăng nhập bằng mật khẩu `admin` cũ vẫn được, phiên đang mở vẫn sống (đã chạy thật trên máy ảo từ gói v0.1.1, `docs/bang-chung/M13-nang-cap.log`).

**Lùi về v0.1.1 sau khi đã nâng thì không được.** Trên máy ảo, `tpan self-update --rollback` báo thành công nhưng panel không lên lại, journal ghi `DB ở bản 1300, tpan này chỉ biết tới bản 900: hãy dùng tpan mới hơn`. Vì vậy sao lưu `tpan.db` trước:

```bash
sudo cp -a /var/lib/tpan/tpan.db /var/lib/tpan/tpan.db.v011
```

Nâng (đường đã chạy thật trên máy ảo là `self-update`; chạy lại `install.sh` của gói mới cũng được):

```bash
sudo tpan self-update --dry-run
```

```bash
sudo tpan self-update --yes
```

Kiểm: đăng nhập panel với ô tên để trống (hiểu là `admin`) và mật khẩu cũ; `sudo tpan user list` có `admin`; `sudo tpan site list` mọi site có chủ `admin`. Lỡ chạy `--rollback` mà panel chết thì cài lại bản mới bằng `install.sh` của gói v0.2.0 (DB còn nguyên).

Thử nhiều người dùng (an toàn, chỉ đụng site thử):

```bash
sudo tpan package add thu --max-sites 1 --max-dbs 1
```

```bash
sudo tpan user add khach_thu --package thu
```

Lệnh in mật khẩu một lần. Đăng nhập panel bằng `khach_thu` trong một cửa sổ ẩn danh: chỉ thấy panel trống của khách. Tạo một site thử cho khách (cần một tên miền con trỏ về VPS, hoặc dùng tên bất kỳ rồi xem bằng `curl --resolve`):

```bash
sudo tpan site add thu.<domain> --email <email> --owner khach_thu --yes
```

Tạm khoá site rồi mở lại:

```bash
sudo tpan site suspend thu.<domain> --yes
```

```bash
curl -s -o /dev/null -w '%{http_code}\n' --resolve thu.<domain>:80:127.0.0.1 http://thu.<domain>/
```

Ra `403`. Mở lại bằng `sudo tpan site unsuspend thu.<domain> --yes`, chạy lại `curl` ra `200`. Khoá cả user: `sudo tpan user suspend khach_thu --yes` (cửa sổ ẩn danh bị đăng xuất, site ra 403), `sudo tpan user unsuspend khach_thu --yes`. Dọn: `sudo tpan user trash-all khach_thu --yes`, `sudo tpan trash purge thu.<domain> --yes`, `sudo tpan user rm khach_thu --yes`, `sudo tpan package rm thu --yes`.

### 3.10a. Sau khi nâng lên bản có M14: áp bản vá cô lập

Bản M14 siết cô lập giữa các site (mục 6.13 của `docs/KE-HOACH.md`). Site tạo bằng bản mới có sẵn mọi thứ. Site có từ trước thì chưa: vhost của nó thiếu dòng chặn, thư mục log còn mở, PHP-FPM chưa có file ini và drop-in mới. Vì vậy **việc đầu tiên sau `tpan self-update` là chạy harden**. Cho tới lúc đó, `tpan health` báo lỗi `php.opcache`, `log.perm`, `web.hardened`, và mọi thao tác trên site cũ (đổi bản PHP, khôi phục backup, tạo site mới cạnh site cũ) bị hoàn tác ở bước kiểm chứng; đó là thiết kế, không phải hỏng. Đường này đã chạy thật trên máy ảo từ v0.1.2, cả nginx lẫn Apache (`docs/bang-chung/M14-nang-cap.log`).

Xem trước: lệnh in từng bước, và với stack Apache thì in site nào sẽ chuyển sang chế độ `.htaccess` `an-toan`, site nào bị giữ ở `day-du` kèm đúng dòng `.htaccess` gây vướng.

```bash
sudo tpan security harden --dry-run
```

Có một bước ghi "NGUY HIỂM": khởi động lại PHP-FPM, request PHP đang chạy bị ngắt khoảng một giây. Chạy thật:

```bash
sudo tpan security harden --yes
```

Kiểm:

```bash
sudo tpan health
```

Không còn dòng `[lỗi]` nào của ba loại trên. Trên Apache, site bị giữ `day-du` còn một dòng `[cảnh báo]` về `.htaccess`: site đó vẫn ghi được cấu hình Apache, nên nếu bị chiếm nó còn lấn được sang site khác. Muốn đóng: sửa dòng `.htaccess` mà bản xem trước đã chỉ, rồi

```bash
sudo tpan site set <domain> --htaccess an-toan
```

Xem một request tới site đi đường nào, kèm lệnh và output của từng chặng (dùng được khi người review hỏi "sao request không đi nhầm site"):

```bash
sudo tpan route explain <domain> --evidence
```

Trên panel: màn Bảo mật có khối "Cô lập giữa các site" (ma trận site × site, nút "Áp bản vá cô lập"); trang site có nút "Request tới site này đi đường nào". Hai chỗ này chưa ai xem bằng mắt: xem theo mục 1 (`make dev-api` + `make dev-web`).

Không chạy `scripts/e2e-m14/tan-cong.sh` trên VPS: bộ phép thử tấn công chỉ dành cho máy ảo test.

### 3.11. Gửi lại cho mình

Dừng ghi (`exit`), che tên miền và IP như ở 3.0, rồi gửi file `~/tpan-vps-che.log` (lấy về Mac bằng `scp <user>@<IP>:tpan-vps-che.log .`). Mình đọc, đối chiếu với kết quả máy ảo, ghi vào `docs/bang-chung/` và đánh dấu các tiêu chí còn treo (chứng chỉ staging M5, `install.sh` trên amd64 M11).

---

## 4. Phát hành và mở repo công khai

**Đã làm 20/11/2026**: `v0.1.0` đã phát hành, repo đã công khai, lịch sử git đã dùng email ẩn, giấy phép MIT. Phần dưới giữ lại làm mẫu cho bản sau (bỏ bước viết lại lịch sử).

### 4.1. Biết trước khi quyết

Mình đã rà repo (20/11/2026):

- **Bí mật trong lịch sử git**: `gitleaks` quét 277 commit, báo 8 chỗ, cả 8 là giá trị giả trong file test (`*_test.go`: mật khẩu mẫu, khoá RSA cụt để thử chức năng che bí mật). Không có khoá hay mật khẩu thật.
- **IP**: không có IP công khai nào; chỉ có IP nội bộ của máy ảo (192.168...) và IP dành cho tài liệu.
- **Email cá nhân**: cả 288 commit mang email cá nhân của bạn trong phần tác giả. Mở công khai là ai cũng đọc được. Có hai cách:
  1. Chấp nhận (nhiều người làm vậy).
  2. Viết lại lịch sử để thay bằng email ẩn của GitHub (`<số>+<tên>@users.noreply.github.com`, xem ở GitHub → Settings → Emails). Việc này đổi mã mọi commit, phải đẩy đè (`--force`) lên `dev` và `main`. Muốn làm thì bảo mình, mình làm và kiểm trước khi đẩy.
  Dù chọn cách nào, từ nay nên bật "Keep my email addresses private" và đặt `git config user.email` thành email ẩn.
- **Giấy phép**: đã chọn MIT (file `LICENSE` ở gốc repo). Ai cũng được dùng, sửa, bán lại, miễn giữ dòng bản quyền; tác giả không chịu trách nhiệm nếu phần mềm gây hỏng hóc.

### 4.2. Tạo bản phát hành

Trên Mac, `~/tpan`, nhánh `main` đã có bản mới nhất:

```bash
git checkout main && git pull
```

Gắn tag phiên bản (đặt trước `make release` để số bản nhúng vào file chạy là `v0.1.0` chứ không phải mã commit):

```bash
git tag -a v0.1.0 -m "TPAN v0.1.0"
```

```bash
git push origin v0.1.0
```

```bash
make release
```

```bash
ls dist/release/v0.1.0/
```

Chờ thấy đủ bốn file: `tpan-linux-amd64`, `tpan-linux-arm64`, `install.sh`, `SHA256SUMS`. Tạo release trên GitHub, đính kèm cả bốn:

```bash
gh release create v0.1.0 dist/release/v0.1.0/* --title "TPAN v0.1.0" --notes "Bản đầu tiên. Cài: xem README."
```

Không đánh dấu prerelease: đường tải `releases/latest/download` mà `install.sh` và `tpan self-update` dùng bỏ qua bản prerelease.

Quay về nhánh làm việc:

```bash
git checkout dev
```

### 4.3. Mở công khai

Không đảo ngược được hoàn toàn: đổi lại riêng tư thì người đã sao chép vẫn giữ bản của họ.

```bash
gh repo edit --visibility public --accept-visibility-change-consequences
```

Hoặc trên web: repo → Settings → General → Danger Zone → Change visibility.

### 4.4. Thử cài một lệnh

Trên một VPS sạch (hoặc VPS ở mục 3 sau khi đã khôi phục snapshot):

```bash
curl -fsSL https://github.com/<chủ repo>/tpan/releases/latest/download/install.sh | sudo bash -s -- --web nginx
```

Trên VPS đã cài từ trước, thử tự cập nhật:

```bash
sudo tpan self-update --dry-run
```

```bash
sudo tpan self-update --yes && tpan version
```

Gửi mình output; mình ghi vào `docs/bang-chung/M11.log` và cập nhật README (bỏ câu "repo đang riêng tư, dùng cách 1").
