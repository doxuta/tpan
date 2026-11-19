# M11: cài đặt bằng một lệnh và gói phát hành

Đọc kèm: `01-m1-cai-stack.md` (engine, Step, Check rồi Apply rồi Check), `03-m3-giao-dien-va-api.md` (`tpan setup`, chứng chỉ tự ký, mật khẩu quản trị, `tpan.service`), `05-m5-ssl-va-bao-mat.md` (webroot ACME, HTTP-01), `12-m12-phien-ban-va-cap-nhat.md` (mục 3.12 `tpan self-update`, mục 4 "`rename` và file đang chạy"), `docs/KE-HOACH.md` mục 5 (bố trí trên máy chủ), 9 (CLI), 13 (dòng M11), 16 (dòng quyền sau `tpan setup`), và `docs/GIAO-DIEN.md` mục 8 (giọng chữ, bảng thuật ngữ).

Trong file này, tên tài khoản GitHub trong địa chỉ tải mặc định và trong đường dẫn module Go được viết là `<chủ repo>`. Mã và log ghi tên thật.

## 1. Mốc này làm được gì

- Trên Mac, `make release` đóng một gói phát hành ở `dist/release/<bản>/`: hai file chạy `tpan-linux-amd64`, `tpan-linux-arm64`, `install.sh` và `SHA256SUMS`. Gói này đúng định dạng mà `tpan self-update` (M12) tải về.
- Trên một máy Ubuntu 24.04 sạch, chép gói sang rồi gõ đúng một lệnh `sudo ./install.sh --web nginx`: tpan được đặt vào `/usr/local/bin/tpan`, panel chạy thành dịch vụ ở cổng 8443, mật khẩu quản trị in ra một lần, stack LEMP được cài. Gõ lại lệnh đó thì không có gì đổi.
- Gói bị hỏng hay bị sửa dù một byte thì `install.sh` dừng ở bước kiểm sha256, không đụng tới bản đang cài.
- Khi có chỗ để tải (bản phát hành trên GitHub, hoặc một máy chủ HTTP), cài được bằng `curl -fsSL <gốc>/install.sh | sudo bash -s -- --from <gốc> --web apache`.
- README có ảnh chụp giao diện thật (chụp tự động bằng `go run ./scripts/anh-readme`), và chữ trên mọi màn được rà lại theo một bảng thuật ngữ hai ngôn ngữ.

## 2. Chạy thử

Mọi output dưới đây lấy nguyên văn từ `docs/bang-chung/M11.log`: lần chạy `scripts/e2e-m11.sh all` ngày 20/11/2026, 02:35 tới 02:38, trên bản đã commit `52c14c3`, máy ảo `tpan-test` (Multipass, Ubuntu 24.04, arm64) bắt đầu từ snapshot sạch. Dòng cuối log là `TỔNG KẾT: 127 khẳng định đạt, 0 khẳng định không đạt`. Log chia thành phần 0 tới 6; mục 2.1 tới 2.7 đi theo đúng thứ tự đó.

Những điều cần biết khi đọc log:

- Mọi lệnh gõ trên Mac, trong thư mục repo. `scripts/vm.sh exec X` chạy X trong máy ảo; `scripts/vm.sh root X` chạy X trong máy ảo bằng `sudo`. Câu "chạy trong máy ảo" dưới đây nghĩa là lệnh nằm sau `scripts/vm.sh exec`.
- Kịch bản in lệnh bằng `$*` nên mất dấu nháy. Lệnh dưới đây mình viết lại có nháy đúng để bạn dán vào zsh được; output giữ nguyên.
- Kịch bản che IP máy ảo (`<vm>`), tên user Mac (`<user>`), mật khẩu (`<bí mật, đã che>`). `<IP của máy>` là chữ chính `tpan setup` in ra, không phải chỗ che. Phần cuối log tự kiểm: không mật khẩu, cookie, IP hay tên user Mac nào lọt vào log.
- Chỗ có `...` là mình cắt bớt dòng ở giữa; số dòng log ghi kèm để bạn mở xem đủ.

### 2.1 Đóng gói trên Mac (phần 0)

Chạy trên Mac:

```bash
make release VERSION=52c14c3 DATE=2026-11-19T19:35:55Z
```

Kịch bản tự tính `VERSION` một lần (cùng công thức `git describe` của Makefile) rồi đưa thẳng cho make, để thư mục gói chắc chắn là `dist/release/<bản đó>` (ghi chú trong `scripts/e2e-m11.sh`). `DATE` cũng tính một lần và dùng cho cả hai gói, nên hai bản ở 2.7 có cùng giờ build. Gõ `make release` trần thì Makefile tự tính cả hai. Output bắt đầu bằng phần build giao diện (`pnpm build`), rồi hai dòng `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ...` và `... GOARCH=arm64 ...`, rồi phần đóng gói (M11.log dòng 75 tới 89):

```
rm -rf dist/release/52c14c3
mkdir -p dist/release/52c14c3
cp bin/tpan-linux-amd64 bin/tpan-linux-arm64 scripts/install.sh dist/release/52c14c3/
chmod 0755 dist/release/52c14c3/tpan-linux-amd64 dist/release/52c14c3/tpan-linux-arm64 dist/release/52c14c3/install.sh
cd dist/release/52c14c3 && if command -v sha256sum >/dev/null 2>&1; then \
	  sha256sum tpan-linux-amd64 tpan-linux-arm64 install.sh; \
	else \
	  shasum -a 256 tpan-linux-amd64 tpan-linux-arm64 install.sh; \
	fi > SHA256SUMS
Gói phát hành: dist/release/52c14c3/
total 84880
-rw-r--r--@ 1 <user>  staff       243 Nov 20 02:35 SHA256SUMS
-rwxr-xr-x@ 1 <user>  staff     20019 Nov 20 02:35 install.sh
-rwxr-xr-x@ 1 <user>  staff  22585504 Nov 20 02:35 tpan-linux-amd64
-rwxr-xr-x@ 1 <user>  staff  20840608 Nov 20 02:35 tpan-linux-arm64
```

Kịch bản build thêm một gói thứ hai, `make release VERSION=e2e-m11-b`, từ cùng mã, để có "bản mới" cho self-update ở 2.7. So hai file `SHA256SUMS` (chạy trên Mac: `cat dist/release/52c14c3/SHA256SUMS dist/release/e2e-m11-b/SHA256SUMS`):

```
6735834992840ef189fab100d75ec0b5dc361f72b0702459b2d63f8247e90b7a  tpan-linux-amd64
2d10640407c5131992b4011ccbe8e74d317529d31ac2db65631c48c6e17e9b9c  tpan-linux-arm64
329de7b8a4ac21be8a378f873d27e7e50b6c2f997eb7523fce15ce5ac166c472  install.sh
d41e8c5323c80961b9157311c7bfa20ccc29c986d3fae5e67d9563f2d198b1ec  tpan-linux-amd64
2c8d9019e27096bce82a266d0dd8d8d0732ab4a3a8ec669e19f2ee193b38f67e  tpan-linux-arm64
329de7b8a4ac21be8a378f873d27e7e50b6c2f997eb7523fce15ce5ac166c472  install.sh
```

Mỗi dòng chỉ có tên file trần. `install.sh` giống hệt nhau ở hai gói; hai file chạy khác nhau dù cùng mã, vì chữ bản (`52c14c3` hay `e2e-m11-b`) được ghi vào file chạy lúc build. Sau phần 0, kịch bản chạy `make vm-reset` để máy ảo về sạch, chưa có tpan.

### 2.2 Cài bằng một lệnh từ gói (phần 1)

Chạy trên Mac: chép cả thư mục gói vào máy ảo, rồi gõ đúng một lệnh trong máy ảo.

```bash
multipass transfer -r dist/release/52c14c3 tpan-test:/home/ubuntu/
scripts/vm.sh exec bash -c 'cd /home/ubuntu/52c14c3 && sudo ./install.sh --web nginx'
```

Output (M11.log dòng 258 tới 360):

```

==> Kiểm máy
Ubuntu 24.04
Kiến trúc: arm64

==> Dùng file chạy /home/ubuntu/52c14c3/tpan-linux-arm64

==> Kiểm sha256
sha256 khớp: tpan-linux-arm64

==> Đặt /usr/local/bin/tpan
tpan 52c14c3 (commit 52c14c3, build 2026-11-19T19:35:55Z)
Đã đặt /usr/local/bin/tpan

==> Khởi tạo panel: tpan setup
Bắt đầu job b60ee4e2cab03750
- Tạo thư mục /etc/tpan ...
  xong
...
- Tạo thư mục /var/lib/tpan ...
  xong
- Ghi file dịch vụ systemd của tpan ...
  xong
- Chạy và bật tpan.service khi khởi động ...
  xong

Job b60ee4e2cab03750: thành công

Mật khẩu quản trị (chỉ hiện MỘT lần, hãy lưu lại ngay):
    <bí mật, đã che>
Dấu vân tay SHA-256 của chứng chỉ (đối chiếu với trình duyệt khi nó cảnh báo chứng chỉ tự ký):
    D5:9A:B2:3F:42:52:1C:E6:4E:DE:98:9D:76:F1:61:AE:82:EC:9D:EE:3A:2F:AA:BC:7D:91:2A:ED:70:D5:96:84
Mở: https://<IP của máy>:8443

==> Cài stack: tpan stack install --web nginx
Bắt đầu job 91b7d702b952f712
- Làm mới danh sách gói (apt-get update) ...
  xong
...
- Tạo thư mục /var/lib/tpan ...
  đã đúng, bỏ qua
- Tạo thư mục /var/lib/tpan/acme ...
  xong
...
Job 91b7d702b952f712: thành công
...
Xong. Bước tiếp:
  sudo tpan site add example.com --email admin@example.com
  sudo tpan health
```

Từ đầu tới cuối mất 52 giây (02:36:16 tới 02:37:08). Để ý dòng `Tạo thư mục /var/lib/tpan ... đã đúng, bỏ qua` trong job của stack: setup vừa đặt thư mục đó 0755, đúng quyền stack cần, nên stack không phải sửa lại (mục 5.2). Mật khẩu quản trị xuất hiện đúng 1 lần trong output (kịch bản đếm rồi che).

Kịch bản kiểm lại độc lập, không tin lời `install.sh` (chạy trên Mac):

```bash
scripts/vm.sh root sha256sum /usr/local/bin/tpan
scripts/vm.sh exec bash -c 'stat -c "%a %U:%G %n" /usr/local/bin/tpan; ls -la /usr/local/bin; tpan version'
scripts/vm.sh root bash -c 'p=$(systemctl show -p MainPID --value tpan); /proc/$p/exe version'
```

```
2d10640407c5131992b4011ccbe8e74d317529d31ac2db65631c48c6e17e9b9c  /usr/local/bin/tpan
```

```
755 root:root /usr/local/bin/tpan
total 27340
drwxr-xr-x  2 root root     4096 Nov 20 02:37 .
drwxr-xr-x 10 root root     4096 Oct 17 19:57 ..
-rwxr-xr-x  1 root root 20840608 Nov 20 02:36 tpan
-rwxr-xr-x  1 root root  7142777 Nov 20 02:37 wp
tpan 52c14c3 (commit 52c14c3, build 2026-11-19T19:35:55Z)
```

```
tpan 52c14c3 (commit 52c14c3, build 2026-11-19T19:35:55Z)
```

Dấu sha256 trùng dòng `tpan-linux-arm64` của `SHA256SUMS` ở 2.1. Không còn sót `tpan.install-tmp`. Tiến trình chính của `tpan.service` chạy đúng bản `52c14c3`. `ss -tlnp` có dòng `LISTEN 0      4096               *:8443            *:*    users:(("tpan",pid=1705,fd=8))`; `systemctl is-active tpan` ra `active`, `is-enabled` ra `enabled`. `tpan health --json` có 26 phép kiểm, chỉ một cái không pass:

```
tpan  tpan.backup:db  warn  -- chưa có bản sao lưu tpan.db nào
```

Đó là đúng như mong đợi: máy mới chưa sao lưu `tpan.db` lần nào (M9).

### 2.3 Tạo site và đăng nhập panel (phần 2)

Chạy trên Mac: `scripts/vm.sh root tpan site add a.example.com --email admin@example.com --yes`, rồi gọi site bằng `curl --resolve` trong máy ảo và từ Mac: cả hai ra `HTTP 200` và `<title>a.example.com</title>` (M11.log dòng 467 tới 545). Health của site: 6 phép kiểm, cả 6 pass.

Rồi kiểm panel từ Mac. Chứng chỉ `cert.pem` chép từ máy ảo và chứng chỉ panel đang phục vụ ở cổng 8443 cho cùng dấu vân tay, trùng dòng `tpan setup` in ra ở 2.2:

```
sha256 Fingerprint=D5:9A:B2:3F:42:52:1C:E6:4E:DE:98:9D:76:F1:61:AE:82:EC:9D:EE:3A:2F:AA:BC:7D:91:2A:ED:70:D5:96:84
```

Gọi API qua https với `--cacert` là `cert.pem` đó: `GET /api/v1/sites` khi chưa đăng nhập ra `-> HTTP 401`; `POST /api/v1/login` bằng mật khẩu `install.sh` in ra ra `-> HTTP 204`; rồi `GET /api/v1/sites` ra `-> HTTP 200` và:

```
[{"domain":"a.example.com","slug":"a_example_com_7653e4","status":"active","php_version":"8.3","ssl":false,"created":"2026-11-19T19:37:19.756904713Z"}]
```

### 2.4 Chạy lại đúng lệnh đó: không đổi gì (phần 3)

Trước và sau, kịch bản chụp một ảnh trạng thái: sha256 của file chạy, cert, key, file unit; PID và giờ khởi động của `tpan.service`; hash mật khẩu quản trị; một dấu chung cho mọi file trong `/etc/nginx`, `/etc/apache2`, `/etc/php`, `/etc/tpan`, `/etc/systemd/system`; danh sách gói đã cài; PID của nginx, php8.3-fpm, mariadb. Rồi chạy lại (trên Mac):

```bash
scripts/vm.sh exec bash -c 'cd /home/ubuntu/52c14c3 && sudo ./install.sh --web nginx'
```

```
==> Đặt /usr/local/bin/tpan
/usr/local/bin/tpan đã đúng bản này, giữ nguyên
tpan 52c14c3 (commit 52c14c3, build 2026-11-19T19:35:55Z)

==> Khởi tạo panel: tpan setup
Bắt đầu job 08c1f61fb657e0c9
- Tạo thư mục /etc/tpan ...
  đã đúng, bỏ qua
...
- Tạo thư mục /var/lib/tpan ...
  đã đúng, bỏ qua
...
Job 08c1f61fb657e0c9: thành công

Mật khẩu quản trị đã có từ trước, giữ nguyên. Quên thì chạy: sudo tpan admin passwd
```

(M11.log dòng 636 tới 659.) Job của stack sau đó cũng ra `đã đúng, bỏ qua` ở mọi bước. Ảnh trạng thái sau giống hệt ảnh trước, ví dụ:

```
2d10640407c5131992b4011ccbe8e74d317529d31ac2db65631c48c6e17e9b9c  /usr/local/bin/tpan
...
MainPID=1705
ExecMainStartTimestamp=Fri 2026-11-20 02:36:16 +07
...
cây cấu hình (nginx, apache2, php, tpan, systemd): 37c614a14869a8ca
gói đã cài: 8c86f3072810e823
```

PID 1705 là đúng PID của `tpan` nghe cổng 8443 ở 2.2: dịch vụ không bị khởi động lại. Không có dòng `Đã đặt`, không có dòng `xong` nào, mật khẩu xuất hiện 0 lần. Site `a.example.com` vẫn 200.

### 2.5 Gói hỏng một byte: dừng ở bước kiểm sha256 (phần 4)

Kịch bản chép gói sang `/tmp/pkg-bad` trong máy ảo và lật byte thứ 4097 của `tpan-linux-arm64` bằng một dòng Python, để nguyên `SHA256SUMS`. Trước và sau khi sửa:

```
2d10640407c5131992b4011ccbe8e74d317529d31ac2db65631c48c6e17e9b9c  /tmp/pkg-bad/tpan-linux-arm64
f2944984bde5082319013a1dcea463dd97aaf4b1d9e2534a18d7d2f299fe4fbe  /tmp/pkg-bad/tpan-linux-arm64
    4097 220 157
```

(`cmp -l` in vị trí byte theo hệ 10, đếm từ 1, nên 4097 là `b[4096]` mà dòng Python đã lật; rồi giá trị cũ và mới theo hệ 8: 220 là 0x90, 157 là 0x6F, đúng là 0x90 lật hết bit.) Chạy trên Mac:

```bash
scripts/vm.sh exec bash -c 'cd /tmp/pkg-bad && sudo ./install.sh --web nginx'
```

```

==> Kiểm máy
Ubuntu 24.04
Kiến trúc: arm64

==> Dùng file chạy /tmp/pkg-bad/tpan-linux-arm64

==> Kiểm sha256

LỖI: sha256 của /tmp/pkg-bad/tpan-linux-arm64 không khớp /tmp/pkg-bad/SHA256SUMS: file hỏng hoặc bị sửa giữa đường. Không cài.
Ở bước: Kiểm sha256
Làm gì tiếp: tải hoặc chép lại gói phát hành rồi chạy lại.
[2026-11-20 02:37:25] (mã thoát 1)
```

Ảnh trạng thái sau giống hệt trước: file đang cài vẫn có sha256 `2d1064...`, `tpan.service` vẫn `active`, không sót `tpan.install-tmp`.

### 2.6 Máy sạch lần hai: `curl | sudo bash` (phần 5)

Repo còn riêng tư nên chưa tải được từ GitHub (5.1). Kịch bản đưa máy ảo về sạch, chép gói vào `/home/ubuntu/52c14c3`, rồi dựng một máy chủ HTTP chỉ nghe 127.0.0.1 bên trong máy ảo để đóng vai trang phát hành (chạy trên Mac):

```bash
make vm-reset
multipass transfer -r dist/release/52c14c3 tpan-test:/home/ubuntu/
multipass transfer -r dist/release/e2e-m11-b tpan-test:/home/ubuntu/52c14c3/
scripts/vm.sh root systemd-run --unit=tpan-e2e-rel python3 -m http.server 18711 --bind 127.0.0.1 --directory /home/ubuntu/52c14c3
```

```
Running as unit: tpan-e2e-rel.service; invocation ID: 9b3d911a9c9b4b7f9a31bffbd1af3b04
```

Rồi đúng một lệnh, lần này chọn Apache:

```bash
scripts/vm.sh exec bash -c 'curl -fsSL http://127.0.0.1:18711/install.sh | sudo bash -s -- --from http://127.0.0.1:18711 --web apache'
```

```

==> Kiểm máy
Ubuntu 24.04
Kiến trúc: arm64

==> Tải tpan từ http://127.0.0.1:18711
Tải http://127.0.0.1:18711/SHA256SUMS
Tải http://127.0.0.1:18711/tpan-linux-arm64

==> Kiểm sha256
sha256 khớp: tpan-linux-arm64

==> Đặt /usr/local/bin/tpan
tpan 52c14c3 (commit 52c14c3, build 2026-11-19T19:35:55Z)
Đã đặt /usr/local/bin/tpan

==> Khởi tạo panel: tpan setup
...
==> Cài stack: tpan stack install --web apache
...
Job 51face4d2873eb4a: thành công
...
Xong. Bước tiếp:
  sudo tpan site add example.com --email admin@example.com
  sudo tpan health
```

(M11.log dòng 954 tới 1061.) Khác 2.2 ở chỗ có bước "Tải" thay cho "Dùng file chạy". Lý do là cờ `--from`: có nó thì script tải, không tìm thư mục gói nữa (3.5). Trường hợp `curl | bash` không cờ nào (khi đó `BASH_SOURCE` rỗng nên script tự chuyển sang tải từ `DEFAULT_BASE`) chưa chạy ở đâu cả: test `curl | bash` trong `install_test.sh` (2.8) cũng truyền `--from`. Kiểm độc lập sau đó: sha256 của `/usr/local/bin/tpan` vẫn là `2d1064...`; `apache2`, `php8.3-fpm`, `mariadb` active, `nginx` ra `inactive`; health như 2.2 (26 phép kiểm, chỉ `tpan.backup:db` warn); tạo `b.example.com` ra `HTTP 200`.

### 2.7 `tpan self-update` lên gói thứ hai, rồi `--rollback` (phần 6)

Cùng máy chủ HTTP, gói `e2e-m11-b` nằm ở `http://127.0.0.1:18711/e2e-m11-b`. Chạy trên Mac:

```bash
scripts/vm.sh root tpan self-update --url http://127.0.0.1:18711/e2e-m11-b --dry-run
scripts/vm.sh root tpan self-update --url http://127.0.0.1:18711/e2e-m11-b --yes
```

`--dry-run` liệt kê 3 bước sẽ chạy và không đổi file (sha256 vẫn `2d1064...`). Lệnh thật (M11.log dòng 1303 tới 1315):

```
Bắt đầu job ff4dc8d0adc51dba
- Tải bản tpan mới vào /usr/local/bin/tpan.new ...
  xong
- Thay /usr/local/bin/tpan bằng /usr/local/bin/tpan.new, giữ bản cũ ở /usr/local/bin/tpan.old ...
  xong
- Khởi động lại tpan ...
  xong
  [đạt] File chạy /usr/local/bin/tpan chạy được

Job ff4dc8d0adc51dba: thành công
Kiểm chứng:
  [đạt] File chạy /usr/local/bin/tpan chạy được: tpan e2e-m11-b (commit 52c14c3, build 2026-11-19T19:35:55Z)
tpan đã đổi: 52c14c3 -> e2e-m11-b
```

Đây là chứng cứ gói của `make release` đúng định dạng `tpan self-update` đọc: cùng `SHA256SUMS`, cùng tên `tpan-linux-arm64`. Sau đó sha256 của `/usr/local/bin/tpan` là `2c8d90...`, trùng dòng arm64 của gói `e2e-m11-b` ở 2.1; `tpan.old` là bản `52c14c3`; PID của dịch vụ đổi từ 1821 sang 16722 và `/proc/<pid>/exe version` ra `tpan e2e-m11-b`. Site `b.example.com` vẫn 200.

Quay về (chạy trên Mac):

```bash
scripts/vm.sh root tpan self-update --rollback --yes
```

```
Bắt đầu job cef21af0712c3ce1
- Thay /usr/local/bin/tpan bằng /usr/local/bin/tpan.old, giữ bản cũ ở /usr/local/bin/tpan.new ...
  xong
- Khởi động lại tpan ...
  xong
  [đạt] File chạy /usr/local/bin/tpan chạy được

Job cef21af0712c3ce1: thành công
Kiểm chứng:
  [đạt] File chạy /usr/local/bin/tpan chạy được: tpan 52c14c3 (commit 52c14c3, build 2026-11-19T19:35:55Z)
tpan đã quay về bản trước: e2e-m11-b -> 52c14c3
```

sha256 của file chạy về lại đúng `2d10640407c5131992b4011ccbe8e74d317529d31ac2db65631c48c6e17e9b9c`, dịch vụ chạy lại với PID mới 17395, health toàn máy 35 phép kiểm, chỉ `tpan.backup:db` warn.

### 2.8 Test của `install.sh` trên Mac

Phần không cần root (đọc cờ, tìm hoặc tải file, kiểm sha256, qua `--check-only`) có test riêng, nằm trong `make check`. Mình chạy lại trong phiên viết file này, trên Mac:

```bash
bash scripts/install_test.sh
```

29 dòng `ok`, kết thúc bằng:

```
ok    --from loopback đúng
ok    --from loopback file bị sửa
ok    --from loopback không có file
ok    curl | bash

29 đạt, 0 hỏng
```

### 2.9 Chưa chạy

- Tải thật từ GitHub (`DEFAULT_BASE`): chưa có bản phát hành nào, repo riêng tư. Lệnh để thử khi đã mở công khai: `curl -fsSL https://github.com/<chủ repo>/tpan/releases/latest/download/install.sh | sudo bash -s -- --web nginx`.
- `--private`, `--force`, `--binary` và `--check-only` trên máy ảo: chưa chạy trong e2e (`--binary`, `--check-only` có trong `install_test.sh` trên Mac).
- Cài trên máy amd64: chưa chạy, máy ảo là arm64. Gói có file amd64 và `SHA256SUMS` có dòng cho nó.
- Xem quyền `/var/lib/tpan` sau khi cài: e2e không in `stat` của thư mục này, chỉ có bước `đã đúng, bỏ qua` ở 2.2 và 2.4. Lệnh: `scripts/vm.sh root stat -c '%a %U:%G %n' /var/lib/tpan /var/lib/tpan/acme`.
- `go run ./scripts/anh-readme`: không chạy lại trong phiên này (nó ghi đè `docs/anh/`). Ảnh hiện có là của commit 9be99aa: 7 file PNG, ví dụ `docs/anh/bang-dieu-khien.png` 1440 x 900, 231954 byte (`ls -l docs/anh`, `file docs/anh/bang-dieu-khien.png` trên Mac).

## 3. Đi qua mã theo thứ tự chạy

Thứ tự: người phát hành gõ `make release` trên Mac (3.1). Người cài gõ `sudo ./install.sh ...` trên máy đích (3.2 tới 3.8). `install.sh` gọi `tpan setup` (3.9) rồi `tpan stack install` (3.10). Về sau người dùng gõ `tpan self-update`, đọc cùng gói đó (3.11). Hai việc còn lại của mốc không nằm trên đường cài: chụp ảnh README (3.12) và rà chữ (3.13).

### 3.1 `Makefile`: `build-linux` và `release`

```make
build-linux: web ## Build tpan cho Linux amd64 (VPS) và arm64 (máy ảo trên Mac M)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/tpan-linux-amd64 ./cmd/tpan
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/tpan-linux-arm64 ./cmd/tpan
```

```make
RELEASE_DIR := dist/release/$(VERSION)

release: build-linux ## Đóng gói phát hành vào dist/release/<version>/ (file chạy, install.sh, SHA256SUMS)
	rm -rf $(RELEASE_DIR)
	mkdir -p $(RELEASE_DIR)
	cp bin/tpan-linux-amd64 bin/tpan-linux-arm64 scripts/install.sh $(RELEASE_DIR)/
	chmod 0755 $(RELEASE_DIR)/tpan-linux-amd64 $(RELEASE_DIR)/tpan-linux-arm64 $(RELEASE_DIR)/install.sh
	cd $(RELEASE_DIR) && if command -v sha256sum >/dev/null 2>&1; then \
	  sha256sum tpan-linux-amd64 tpan-linux-arm64 install.sh; \
	else \
	  shasum -a 256 tpan-linux-amd64 tpan-linux-arm64 install.sh; \
	fi > SHA256SUMS
```

- `release` phụ thuộc `build-linux`, mà `build-linux` phụ thuộc `web`: giao diện được build vào `web/dist` trước, rồi mới nhúng vào file chạy Go (embed, xem `00-go-can-ban.md`).
- `VERSION` mặc định là `git describe --tags --always --dirty`. `LDFLAGS` có `-X .../internal/buildinfo.Version=$(VERSION)`: chữ bản được ghi thẳng vào file chạy lúc build, nên `tpan version` in đúng bản của gói.
- `RELEASE_DIR :=` (dấu `:=`, không phải `=`): Make tính giá trị một lần. Ghi chú trong Makefile nói lý do: tên thư mục gói phải khớp bản đã nhúng.
- `cd $(RELEASE_DIR) && sha256sum ...`: tính sha256 khi đang đứng trong thư mục gói, để mỗi dòng của `SHA256SUMS` chỉ có tên file trần (`tpan-linux-amd64`), không có `dist/release/...`. `install.sh` và `tpan self-update` đều tìm dòng theo đúng tên trần đó.
- Nhánh `shasum -a 256`: phòng máy dev không có lệnh `sha256sum`. Hai lệnh in cùng một dạng dòng.

### 3.2 `scripts/install.sh`: khung của script

```bash
set -Eeuo pipefail
...
main() {
  ...
}

main "$@"
```

Dòng 26 bật bốn chế độ (giải thích ở mục 4). Mọi việc nằm trong hàm `main`; dòng cuối cùng của file mới gọi `main "$@"`. Ghi chú ở đầu file nói vì sao: khi chạy kiểu `curl ... | bash`, bash đọc tới đâu chạy tới đó. Nếu mạng đứt giữa chừng, bash chỉ nhận được định nghĩa các hàm, chưa có dòng gọi `main`, nên không có nửa script nào chạy.

Các hàm phụ đứng trước `main`:

- `t` chọn câu tiếng Việt hay tiếng Anh theo `LANG_UI` (cờ `--lang`).
- `step` ghi bước đang làm vào `CUR_STEP` và in `==> <tên bước>`.
- `die` in `LỖI:`, `Ở bước:`, `Làm gì tiếp:` ra stderr rồi `exit 1`. Ba dòng này theo đúng luật "chuyện gì, ở đâu, làm gì tiếp" của `docs/GIAO-DIEN.md` mục 8.
- `on_err` là bẫy ERR: lệnh nào hỏng mà script không tự kiểm thì nó in lệnh, dòng, mã thoát qua `die`.

### 3.3 `main`: đọc và kiểm tham số trước khi đụng tới máy

```bash
  while [ $# -gt 0 ]; do
    case "$1" in
      --binary) [ $# -ge 2 ] || { usage >&2; exit 2; }; binary="$2"; shift 2 ;;
      --from) [ $# -ge 2 ] || { usage >&2; exit 2; }; from="$2"; shift 2 ;;
      ...
      -h | --help) usage; exit 0 ;;
      *) usage >&2; printf '\n%s %s\n' "$(t 'Cờ lạ:' 'Unknown flag:')" "$1" >&2; exit 2 ;;
    esac
  done
  trap 'on_err "$?" "$LINENO" "$BASH_COMMAND"' ERR
```

Vòng `while` đọc cờ; cờ lạ thì in cách dùng và thoát mã 2. Bẫy ERR chỉ được đặt sau vòng này, nên `--help` và cờ sai thoát gọn, không qua `on_err`.

Rồi kiểm từng giá trị: `--lang` chỉ nhận `vi`, `en`; `--web` chỉ nhận `nginx`, `apache` hoặc rỗng; không được dùng cùng lúc `--binary` và `--from`. `--from` bỏ dấu `/` ở cuối rồi qua `check_base`: phải là `https://...`, còn `http://` chỉ nhận khi máy là `127.0.0.1`, `localhost` hoặc `[::1]`, và không được có khoảng trắng, ký tự điều khiển, `@`, `?`, `#`. Gốc tải này được nối thành URL đưa cho `curl`; chữ lạ ở đây có thể làm curl tải từ chỗ khác.

### 3.4 Bước "Kiểm máy": root, hệ điều hành, kiến trúc

```bash
    while IFS='=' read -r k v || [ -n "$k" ]; do
      v="${v//\"/}"
      case "$k" in
        ID) os_id="$v" ;;
        VERSION_ID) os_ver="$v" ;;
      esac
    done < "$OS_RELEASE"
```

- `id -u` phải là 0 (root). Thiếu thì `die` kèm gợi ý chạy lại với `sudo`.
- Đọc `/etc/os-release` từng dòng `KEY=giá trị`. Ghi chú trong mã nói vì sao không `source` file đó: `source` là chạy nội dung file như mã. Cũng không dùng `grep | cut` trong `$(...)`, vì thiếu dòng thì bẫy ERR nổ trong tiến trình con và in một lỗi giả.
- Phải là `ubuntu` và `24.04`. Khác thì dừng, trừ khi có `--force`: khi đó in một cảnh báo dài rồi đi tiếp.
- `dpkg --print-architecture` cho `amd64` hoặc `arm64`; khác thì dừng. Tên file cần dùng là `tpan-linux-$arch`.
- Kiểm có lệnh `sha256sum`.
- Với `--check-only`, phần root và hệ điều hành được bỏ qua (để test chạy được trên Mac không cần root), nhưng phần kiến trúc và sha256 vẫn chạy.

### 3.5 Tìm file chạy: trong gói, `--binary`, hay tải

```bash
  tmp="$(mktemp -d)"
  trap "rm -rf '$tmp'" EXIT

  local here="" pkg=0
  if [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ]; then
    here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  fi
  if [ -z "$binary" ] && [ -z "$from" ]; then
    if [ -n "$here" ] && [ -f "$here/$name" ]; then
      binary="$here/$name"
      pkg=1
    else
      from="$DEFAULT_BASE"
    fi
  fi
```

- `mktemp -d` tạo thư mục tạm; `trap ... EXIT` xoá nó khi script thoát vì bất cứ lý do gì. Dấu nháy kép ở `trap "rm -rf '$tmp'"` là cố ý: `$tmp` được thay giá trị ngay lúc đặt bẫy (có dòng `shellcheck disable=SC2064` giải thích).
- `BASH_SOURCE[0]` là đường dẫn file script. Chạy `./install.sh` thì nó là file thật, `here` là thư mục gói. Chạy kiểu `curl | bash` thì không có file, `here` rỗng.
- Không có `--binary`, không có `--from`: nếu cạnh `install.sh` có `tpan-linux-<arch>` thì dùng nó và đánh dấu `pkg=1` (đang đứng trong gói); không thì tải từ `DEFAULT_BASE`, tức `https://github.com/<chủ repo>/tpan/releases/latest/download`.

### 3.6 Tải và kiểm sha256: `fetch`, `sum_line`, `verify_sha256`

Nhánh tải (`--from`, hoặc mặc định khi không có gói):

```bash
    fetch "$from/SHA256SUMS" "$tmp/SHA256SUMS"
    fetch "$from/$name" "$tmp/$name"
    src="$tmp/$name"
    step "$(t 'Kiểm sha256' 'Verify sha256')"
    verify_sha256 "$src" "$tmp/SHA256SUMS"
```

`fetch` gọi `curl -fsSL --proto "$proto" --proto-redir '=https' --connect-timeout 20 --retry 2 -o "$out" "$url"`. `--proto =https` bắt curl chỉ dùng https; `--proto-redir =https` bắt cả các lần chuyển hướng phải là https (GitHub chuyển hướng file phát hành sang máy chủ lưu trữ khác). Chỉ khi gốc là `http://` loopback (đã qua `check_base`) thì `proto` là `=http`.

`sum_line` đọc `SHA256SUMS` từng dòng, tách số hex và tên file, bỏ dấu `*` (chế độ nhị phân) nếu có, chỉ nhận dòng có đúng 64 chữ số hex và tên khớp đúng (`tpan-linux-amd64.sig` không tính). Không có dòng nào: mã 1. Hai dòng cùng tên mà khác số: mã 2, không đoán dòng nào đúng. Kết quả trả qua biến `SUM_LINE`, không qua `$(...)`; ghi chú nói vì sao: hàm trả mã khác 0 bên trong `$(...)` làm bẫy ERR nổ trong tiến trình con.

`verify_sha256` đưa đúng một dòng đó cho `sha256sum -c --strict --status -` (đọc từ stdin), chạy trong thư mục chứa file vì `sha256sum` đọc tên file tương đối. Không khớp thì `die` với câu "file hỏng hoặc bị sửa giữa đường. Không cài."

Nhánh có file trên máy (`--binary` hoặc gói) kiểm `SHA256SUMS` nằm cạnh file:

- Đang đứng trong gói (`pkg=1`) mà thiếu `SHA256SUMS`: dừng.
- Đang trong gói, hoặc `SHA256SUMS` có dòng cho tên file, hoặc file mang đúng tên `tpan-linux-<arch>`: luôn kiểm, thiếu dòng là lỗi.
- Còn lại (file tự build, tên khác, không có `SHA256SUMS` ghi tên nó): in `CẢNH BÁO: ... KHÔNG kiểm sha256` rồi đi tiếp. Đây là đường cho người tự build.

`--check-only` dừng ở đây với dòng `Xong (--check-only): ... hợp lệ, không cài gì.` Đường này là thứ `scripts/install_test.sh` (chạy trong `make check`) kiểm, vì nó chạy được trên Mac không cần root.

### 3.7 Đặt `/usr/local/bin/tpan`: chép, thử chạy, rồi `mv`

```bash
  new_sum="$(file_sha "$src")"
  if [ -f "$BIN_DST" ]; then
    old_sum="$(file_sha "$BIN_DST")"
  fi
  if [ "$new_sum" = "$old_sum" ]; then
    say "$(t "$BIN_DST đã đúng bản này, giữ nguyên" "$BIN_DST already has this build, kept")"
    ...
    probe="$BIN_DST"
  else
    probe="$BIN_DST.install-tmp"
    cp "$src" "$probe"
    chown root:root "$probe"
    chmod 0755 "$probe"
  fi
```

- So sha256 của file mới với file đang cài. Giống nhau thì giữ nguyên: đây là lý do chạy lại `install.sh` không đổi file chạy và không khởi động lại dịch vụ.
- Khác thì chép sang `/usr/local/bin/tpan.install-tmp` (cùng thư mục với đích), đặt chủ `root:root`, quyền 0755.
- Chạy `"$probe" version`. Dòng đầu phải bắt đầu bằng `tpan `. File hỏng hoặc sai kiến trúc thì xoá file tạm và dừng; bản đang cài vẫn nguyên.
- Rồi mới `mv -f "$probe" "$BIN_DST"`.

Ghi chú trong mã nói ba lý do cho cách làm này. Thử chạy bản đã chép chứ không chạy file nguồn: file tải bằng curl hay chép bằng scp không có quyền chạy, và có máy gắn `/tmp` với `noexec`. `mv` trong cùng thư mục là một lần `rename`: thay nguyên tử, và không ghi đè lên file đang chạy (ghi đè thì Linux báo "Text file busy" khi `tpan.service` đang chạy nó). Không dùng tên `.new`, `.old` vì đó là chỗ của `tpan self-update`.

### 3.8 Khởi động lại dịch vụ nếu file chạy vừa đổi

```bash
  if [ "$changed" -eq 1 ] && systemctl is-active --quiet tpan 2>/dev/null; then
    say "$(t 'Dịch vụ tpan đang chạy bản cũ, khởi động lại' 'The tpan service runs the old build, restarting it')"
    systemctl restart tpan
  fi
```

`rename` không làm tiến trình đang chạy đổi sang bản mới: nó vẫn giữ file cũ (mục 4). `tpan setup` cũng không làm việc đó: bước `steps.Service` (`internal/steps/apt.go`) thấy dịch vụ đang chạy và đã bật là "đã đúng", không bao giờ khởi động lại một dịch vụ đang chạy; khi file unit đổi, setup chỉ `daemon-reload` rồi in lời nhắc `systemctl restart tpan` (3.9). Ghi chú trong `install.sh` ngay trên khối này viết "tpan setup chỉ khởi động lại dịch vụ khi file unit đổi"; theo mã thì setup không khởi động lại cả lúc đó. Nên `install.sh` tự `systemctl restart tpan` khi file chạy vừa thay và dịch vụ đang chạy. Lần cài đầu dịch vụ chưa có, `is-active` trả khác 0, bỏ qua.

### 3.9 `tpan setup --yes`: `cmd/tpan/setup.go`

```bash
  local setup_args=(setup --yes --lang "$LANG_UI")
  if [ "$private" -eq 1 ]; then
    setup_args+=(--private)
  fi
  "$BIN_DST" "${setup_args[@]}" || die ...
```

Tham số đặt trong mảng bash, không nối chuỗi. `--yes` bắt buộc: chạy kiểu `curl | bash` thì stdin là ống dẫn, không phải terminal, `tpan setup` không hỏi được nên sẽ từ chối. Output không bị bắt lại: mật khẩu quản trị chỉ in ra đúng một lần, ngay trên màn hình người cài.

Bên trong, `setupCmd` gọi `requireRoot`, rồi `buildSetupPlan` dựng Plan `panel.setup` từ các bước có sẵn của M1: `secretDir("/etc/tpan")`, `secretDir("/etc/tpan/tls")` (0700), chứng chỉ và khoá (0600, khoá bọc trong `setupKeyFile` để không lộ vào bản xem trước), thư mục `/var/lib/tpan`, file unit `tpan.service`, rồi `steps.Service`. Mốc này đổi đúng một dòng:

```go
			// Không phải secretDir: stack install đặt /var/lib/tpan 0755 (xem
			// core.StateDirPerm); tpan.db bên trong vẫn 0600.
			&steps.Dir{Path: setupStateDir, Perm: core.StateDirPerm, Owner: system.Root},
```

`core.StateDirPerm` là hằng `0o755` ở `internal/core/stack.go`, dùng chung cho `tpan setup` và `tpan stack install`. Mục 5.2 kể lỗi đã dẫn tới dòng này.

Thứ tự thật trên máy mới: sau khi người dùng đồng ý (ở đây là `--yes`), `openStore` mở DB trước khi chạy Plan. `store.Open` (`internal/store/store.go`) gọi `os.MkdirAll` với 0700 cho thư mục chứa DB (mặc định `/var/lib/tpan/tpan.db`, cờ `--db` ở `cmd/tpan/root.go`), nên `/var/lib/tpan` ra đời với 0700. Rồi bước `dir:/var/lib/tpan` của Plan thấy quyền chưa đúng và đặt lại 0755. Ghi chú ở `setup.go` (dòng "store.Open tự tạo /var/lib/tpan với 0700, đúng như bước Dir") là ghi chú cũ, từ trước khi đổi sang 0755.

Sau khi Plan chạy xong, `setupAdminPassword` sinh mật khẩu nếu DB chưa có hash. Đã có thì không đụng: lần chạy lại in "Mật khẩu quản trị đã có từ trước, giữ nguyên." `printSetupResult` in thêm dấu vân tay SHA-256 của chứng chỉ, để người dùng đối chiếu khi trình duyệt cảnh báo chứng chỉ tự ký.

### 3.10 `tpan stack install --web <W> --yes`

Có `--web` thì `install.sh` gọi tiếp `"$BIN_DST" stack install --web "$web" --yes --lang "$LANG_UI"`. Đây là lệnh của M1 (Plan cài stack, xem `01-m1-cai-stack.md`), không đổi gì ở mốc này, trừ bước thư mục trạng thái:

```go
	ss = append(ss,
		&steps.Dir{Path: stateDir, Perm: StateDirPerm, Owner: system.Root},
		&steps.Dir{Path: acmeRoot, Perm: 0o755, Owner: system.Root},
	)
```

`acmeRoot` là `/var/lib/tpan/acme`, webroot mà certbot ghi file thử thách HTTP-01 vào, và nginx hay Apache (chạy dưới `www-data`) phải đọc được (M5). Không có `--web` thì `install.sh` in gợi ý `sudo tpan stack install --web nginx` ở cuối.

### 3.11 Cùng định dạng với `tpan self-update`: `internal/core/selfupdate.go`

`install.sh` và `tpan self-update` đọc cùng một gói. `SelfUpdate.Prepare` dựng Plan `self.update` với `steps.SelfDownload{Base: baseURL, Arch: s.Arch, Dest: tpanNew, ...}`; `SelfDownload` (`internal/steps/self.go`) tải `<gốc>/SHA256SUMS` và `<gốc>/tpan-linux-<arch>` (hàm `asset()`), tìm dòng bằng `parseSums`, kiểm gốc tải bằng `checkSelfBase`. Gốc mặc định `DefaultSelfURL` (`internal/core/types_m12.go`) trùng với `DEFAULT_BASE` của `install.sh`.

Hai bên viết lại cùng luật bằng hai ngôn ngữ, không dùng chung mã:

| Việc | `install.sh` | Go |
|---|---|---|
| Kiểm gốc tải | `check_base` (khớp mẫu `case`) | `checkSelfBase` (`url.Parse`) |
| Tìm dòng trong `SHA256SUMS` | `sum_line` | `parseSums` (regex `selfSumLine`) |
| So sha256 | `sha256sum -c` | `sha256Hex` trong Go |
| Đặt file | `tpan.install-tmp` rồi `mv` | `tpan.new`, `tpan.old`, hai lần `rename` (`SelfInstall`) |

Luật chính giống nhau: https bắt buộc, http chỉ cho loopback, tên phải khớp đúng, hai dòng khác số là lỗi. Chi tiết nhỏ có khác: regex Go cho phép 1 hoặc 2 dấu cách giữa số và tên và bắt tên không có khoảng trắng, còn `sum_line` cắt ở dấu cách đầu tiên rồi bỏ thêm một dấu cách. Với file do `make release` tạo thì hai bên cho cùng kết quả.

### 3.12 `scripts/anh-readme/main.go`: chụp ảnh README

Đây là đồ nghề chạy trên Mac, không nằm trong file chạy tpan (ghi chú đầu file nói `scripts/` được phép dùng `os/exec`, khác với phần còn lại của repo phải chạy lệnh qua `system.System`). `run` làm theo thứ tự:

1. `go build` tpan ra thư mục tạm rồi chạy `tpan serve --demo --listen 127.0.0.1:18141` (máy giả, dữ liệu mẫu). Build ra file riêng thay vì `go run` để lúc tắt thì tắt đúng tiến trình tpan.
2. Đọc stdout tới khi gặp dòng có chữ "demo" và dấu hai chấm; từ cuối dòng là mật khẩu demo. Rồi `waitHTTP` chờ `/api/v1/session` trả lời.
3. Mở Chrome với `--headless=new --remote-debugging-port=18142 --remote-allow-origins=http://127.0.0.1:18142`. `pageSocket` hỏi `http://127.0.0.1:18142/json/list` để lấy `webSocketDebuggerUrl` của tab đầu tiên, `dial` nối websocket tới đó bằng `golang.org/x/net/websocket`.
4. `Emulation.setDeviceMetricsOverride` đặt khung 1440 x 900. Đăng nhập bằng `fetch('/api/v1/login', ...)` chạy trong trang, để cookie HttpOnly nằm trong Chrome.
5. Mỗi ảnh trong `shots()`: `capture` đặt sáng hoặc tối, `goTo` tới đường dẫn, chạy `before` nếu có (điền form tạo site bằng `typeInto`), chờ `ready` đúng, chờ font, rồi `Page.captureScreenshot`. Ảnh lớn hơn 600 KB thì nén lại bằng `png.Encoder` mức cao nhất.

Lõi là `cdp.call`:

```go
func (c *cdp) call(method string, params any) (json.RawMessage, error) {
	c.next++
	id := c.next
	if err := websocket.JSON.Send(c.ws, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		...
		if err := websocket.JSON.Receive(c.ws, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", method, err)
		}
		if m.ID != id {
			continue
		}
		... // có Error thì trả lỗi, không thì trả m.Result
```

Gửi một thông điệp JSON có `id`, rồi đọc cho tới khi gặp trả lời mang đúng `id` đó. Các thông điệp khác (sự kiện của Chrome) bị bỏ qua. Ghi chú nói vì sao không cần luồng đọc riêng: chụp ảnh chỉ gọi tuần tự. `stop` tắt cả nhóm tiến trình (`syscall.Kill(-pid, SIGTERM)`, sau 5 giây thì `SIGKILL`), vì Chrome đẻ nhiều tiến trình con; muốn vậy thì lúc chạy phải đặt `Setpgid: true`.

Ghi chú đầu file nói vì sao chụp bằng chương trình: ảnh phải chụp lại được mỗi lần giao diện đổi, và luôn bằng dữ liệu demo, không lọt thông tin máy thật. Kết quả là 7 ảnh trong `docs/anh/` (commit 9be99aa), README trỏ tới chúng.

### 3.13 Rà chữ hai ngôn ngữ

Không có mã mới. Commit 6fb93ae sửa chuỗi trong 18 file `web/src/i18n/*.ts` và thêm mục "Thuật ngữ đã thống nhất" vào `docs/GIAO-DIEN.md` mục 8: mỗi khái niệm một từ tiếng Việt, một từ tiếng Anh (ví dụ Hoàn tác / Roll back, Phép kiểm / Check, Màn / screen, Bấm / Press). Ví dụ trong `web/src/i18n/lich-su.ts`, mọi chỗ "tpan đã hoàn tác" thành "TPAN đã hoàn tác": bảng quy định `TPAN` viết hoa là tên sản phẩm, `tpan` chữ thường chỉ dùng cho tên lệnh, đường dẫn và tên dịch vụ. Thêm chuỗi mới thì tra bảng trước.

## 4. Kiến thức nền

### sha256sum và chuỗi tin cậy của gói

`sha256sum` tính một dấu 64 chữ số hex cho nội dung file; đổi một byte thì dấu đổi hẳn. `sha256sum -c SHA256SUMS` đọc từng dòng "dấu, tên" và so. Nó chỉ chứng minh file khớp với `SHA256SUMS`, không chứng minh `SHA256SUMS` là thật: ai sửa được cả hai thì vẫn qua. Nên độ tin cậy đi theo chuỗi: bạn tin nơi lấy gói (máy bạn tự build, hoặc trang phát hành qua https), `SHA256SUMS` đến từ đó, file chạy khớp `SHA256SUMS`. Trong mã: `verify_sha256` ở `scripts/install.sh`. Tự xem trên máy ảo: `scripts/vm.sh exec bash -c 'cd /home/ubuntu/<gói> && sha256sum -c SHA256SUMS'`.

### `curl | bash` và vì sao thân script nằm trong `main`

`curl -fsSL URL | sudo bash -s -- --web nginx`: curl in script ra ống dẫn, bash đọc từ ống dẫn (`-s` là đọc lệnh từ stdin, `--` ngăn cách phần còn lại thành tham số `$1`, `$2` cho script). Bash chạy từng lệnh ngay khi đọc đủ, không chờ hết file. Nếu file bị cắt giữa chừng, phần đầu vẫn chạy, và dòng cuối bị cắt dở cũng chạy (ví dụ giả định: `rm -rf /var/lib/tpan/tmp` cụt thành `rm -rf /var`). Gói mọi thứ trong `main` và chỉ gọi ở dòng cuối thì file bị cắt chỉ định nghĩa hàm, không chạy gì. Trong mã: dòng `main "$@"` cuối `install.sh`. Cũng vì không có file, `BASH_SOURCE[0]` rỗng (3.5).

### `set -Eeuo pipefail` và bẫy ERR

`-e`: lệnh nào trả mã khác 0 thì script thoát. `-u`: dùng biến chưa đặt là lỗi. `-o pipefail`: ống dẫn `a | b` hỏng nếu `a` hỏng, không chỉ khi `b` hỏng. `-E`: bẫy ERR được kế thừa vào hàm, `$(...)` và tiến trình con; thiếu nó thì lệnh hỏng trong hàm làm script thoát im lặng (ghi chú dòng 20 của `install.sh`). Chính vì `-E` mà `sum_line` không trả kết quả qua `$(...)`. Bẫy `trap 'on_err "$?" "$LINENO" "$BASH_COMMAND"' ERR` in lệnh và dòng hỏng. Tự xem (trên Mac hay máy ảo đều được): `bash -c 'set -Ee; trap "echo hỏng ở dòng \$LINENO" ERR; f(){ false; }; f'`.

### `mktemp` và `trap ... EXIT`

`mktemp -d` tạo một thư mục tên ngẫu nhiên trong `/tmp`, chỉ chủ của nó vào được, không đụng tên của ai. `trap "rm -rf '$tmp'" EXIT` chạy khi script thoát, kể cả thoát vì `die` hay vì `set -e`. Trong mã: 3.5. Tự xem: `scripts/vm.sh exec bash -c 'd=$(mktemp -d); stat -c "%a %n" "$d"; rmdir "$d"'`.

### Thay file chạy đang chạy: "Text file busy" và `rename`

Linux không cho mở để ghi một file đang được một tiến trình chạy: `cp` đè lên `/usr/local/bin/tpan` trong lúc `tpan.service` chạy sẽ lỗi `Text file busy`. `mv` trong cùng phân vùng là `rename`: chỉ đổi mục tên trong thư mục cho trỏ sang inode mới. Tiến trình cũ vẫn giữ inode cũ và chạy tiếp bản cũ, tới khi khởi động lại. Nên `install.sh` chép sang `tpan.install-tmp` rồi `mv` (3.7), rồi `systemctl restart tpan` (3.8). Tự xem tiến trình đang chạy file nào: `scripts/vm.sh root bash -c 'p=$(systemctl show -p MainPID --value tpan); readlink /proc/$p/exe'`. Câu lỗi "Text file busy" là theo ghi chú trong mã; trong mốc này chưa chạy thử `cp` đè lên file đang chạy.

### `systemctl restart` và `daemon-reload`

`systemctl restart tpan` dừng tiến trình chính của dịch vụ rồi chạy lại theo `ExecStart`, lúc đó mới nạp file chạy mới. `systemctl daemon-reload` chỉ bảo systemd đọc lại file unit, không đụng tới tiến trình đang chạy. `tpan setup` gọi `daemon-reload` khi file unit đổi (trường `Reload` của bước File); `install.sh` gọi `restart` khi file chạy đổi. Trong mã: 3.8, và `printSetupResult` nhắc `systemctl restart tpan` khi unit đổi mà dịch vụ vốn đang chạy. Tự xem: `scripts/vm.sh exec systemctl show -p MainPID,ActiveEnterTimestamp tpan`, chạy trước và sau một lần restart.

### Quyền thư mục, bit x, và đường đi của `www-data` tới ACME

Với thư mục, bit `r` cho liệt kê tên bên trong, bit `x` cho đi qua (mở file bên trong khi đã biết tên). Muốn đọc `/var/lib/tpan/acme/.well-known/acme-challenge/<token>`, tiến trình `www-data` cần `x` trên mọi thư mục dọc đường. `/var/lib/tpan` là 0700 thì người ngoài root không có `x`, nginx trả 403 và certbot không lấy được chứng chỉ. 0755 cho mọi người `r` và `x`, nhưng file bên trong vẫn tự giữ kín: `tpan.db` 0600, `backups/` và `trash/` 0700. Trong mã: `core.StateDirPerm`. Tự xem: `scripts/vm.sh root stat -c '%a %U:%G %n' /var/lib/tpan /var/lib/tpan/acme /var/lib/tpan/tpan.db` và `scripts/vm.sh root sudo -u www-data ls /var/lib/tpan/acme`.

### Build chéo trong Go

Go build được file chạy cho hệ khác ngay trên Mac: `GOOS=linux GOARCH=amd64` (máy chủ Intel/AMD) hay `GOARCH=arm64` (máy ảo trên Mac M). `CGO_ENABLED=0` bắt Go không dùng mã C, nên file chạy là một khối tự đủ, không cần thư viện C của máy đích. `-trimpath` bỏ đường dẫn thư mục trên máy build khỏi file chạy; `-s -w` bỏ bảng ký hiệu để file nhỏ hơn. Trong mã: `build-linux` ở `Makefile`. Tự xem: `file bin/tpan-linux-arm64` trên Mac, rồi `scripts/vm.sh exec dpkg --print-architecture` để biết máy ảo cần file nào.

### Chrome không giao diện và DevTools Protocol

`--headless=new` chạy Chrome không mở cửa sổ. `--remote-debugging-port` mở một cổng HTTP: `/json/list` liệt kê các tab, mỗi tab có một địa chỉ websocket. Qua websocket đó, chương trình gửi lệnh JSON dạng `{"id": 1, "method": "Page.navigate", "params": {...}}` và nhận trả lời cùng `id`; đó là Chrome DevTools Protocol (CDP), chính thứ mà công cụ dev của Chrome dùng. `--remote-allow-origins` là bắt buộc từ Chrome 111, vì Chrome từ chối websocket có header Origin lạ (ghi chú trong mã). Trong mã: `pageSocket`, `dial`, `cdp.call` ở `scripts/anh-readme/main.go`. Tự xem trên Mac khi đang chạy chương trình: `curl -s http://127.0.0.1:18142/json/list`.

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

### 5.1 Bản phát hành trên GitHub: chưa làm

Dòng M11 ở mục 13 của KE-HOACH ghi "bản phát hành trên GitHub". Mốc này chưa tạo bản phát hành nào: repo đang để riêng tư, và luật của repo (AGENTS.md, luật 8) cấm tạo release hay mở công khai khi chủ dự án chưa bảo. Repo riêng tư thì `curl` không tải được file phát hành, nên `DEFAULT_BASE` hiện chưa dùng được; `fetch` in sẵn gợi ý "với GitHub, repo phải công khai và đã có bản phát hành".

Thay vào đó, mốc này làm hai đường chạy được ngay: chép gói sang máy rồi `sudo ./install.sh` tại chỗ (phần 1 của e2e), và `--from` trỏ tới một máy chủ HTTP bất kỳ. Phần 5 của e2e dựng `python3 -m http.server` nghe 127.0.0.1 trong máy ảo để đóng vai GitHub. Vì thế `check_base` mới nhận `http://` cho loopback. Khi mở công khai, chỉ cần đưa 4 file của `dist/release/<bản>/` lên một bản phát hành GitHub; đường `curl | bash` không qua `--from` khi đó chưa chạy.

### 5.2 `/var/lib/tpan` 0700: lỗi e2e bắt được

Trước commit 1739fd2, `tpan setup` đặt `/var/lib/tpan` bằng `secretDir` (0700), còn `tpan stack install` đặt cùng thư mục đó 0755 cho ACME. Mỗi lệnh một mình thì đúng; chạy chung thì không. `install.sh` chạy cả hai, nên mỗi lần chạy lại: setup đổi về 0700, stack install đổi lên 0755, bước `dir:/var/lib/tpan` "xong" ở cả hai thay vì "đã đúng". Tệ hơn, chạy riêng `sudo tpan setup` trên máy đã có stack để lại 0700, `www-data` không đi qua được tới thư mục ACME, và lần lấy chứng chỉ HTTP-01 sau đó hỏng.

e2e M11 bắt được lỗi này (KE-HOACH mục 16 ghi vậy); triệu chứng chính là thứ phần 3 kiểm: chạy lại `install.sh` thì không được có bước nào chạy. Log của lần chạy đó đã bị ghi đè bằng lần chạy lại từ máy sạch ở mục 2, nên ở đây chỉ còn dấu vết trong commit 1739fd2, dòng quyền ở mục 16 của KE-HOACH, và test `TestSetupGiuQuyenThuMucTrangThaiCuaStack` ở `cmd/tpan/setup_test.go`. Cách sửa: một hằng `core.StateDirPerm = 0o755` cho cả hai lệnh; những thứ cần kín bên trong đã có quyền riêng (`tpan.db` 0600, `backups/` và `trash/` 0700). Ở lần chạy e2e trong mục 2, job stack của lần cài đầu ra `Tạo thư mục /var/lib/tpan ... đã đúng, bỏ qua` (2.2), và lần chạy lại không có bước nào `xong` (2.4). Giải thích M3 (`03-m3-giao-dien-va-api.md`) đã thêm một dòng ghi chú về quyền mới.

### 5.3 Những cách viết shell bị bỏ, có ghi trong mã

- `source /etc/os-release`: ngắn hơn nhưng là chạy nội dung file như mã bằng quyền root. Thay bằng vòng `read` (3.4).
- `os_id=$(grep ^ID= /etc/os-release | cut -d= -f2)` và `SUM_LINE=$(sum_line ...)`: với `set -E`, lệnh trả mã khác 0 bên trong `$(...)` làm bẫy ERR nổ trong tiến trình con, in một lỗi giả dù script vẫn xử lý được trường hợp đó. Thay bằng vòng `read` và trả kết quả qua biến toàn cục.
- Đè thẳng bằng `cp` lên `/usr/local/bin/tpan`, hoặc dùng tên `tpan.new`: cái đầu dính "Text file busy", cái sau đụng file của `tpan self-update` (3.7).
- Bắt output của `tpan setup` để in đẹp hơn: không làm, vì mật khẩu quản trị khi đó sẽ đi qua một biến của script; để `tpan setup` in thẳng ra màn hình.

## 6. Tự kiểm

1. Người review hỏi "sao không cứ `curl ... | bash` một script bình thường, cần gì hàm `main`?". Bạn trả lời gì, và dòng nào trong `install.sh` làm việc đó?
2. Trong `release` của Makefile, bỏ `cd $(RELEASE_DIR) &&` và cho `sha256sum` chạy từ gốc repo với đường dẫn đầy đủ `dist/release/<bản>/tpan-linux-amd64 ...`, vẫn ghi vào `$(RELEASE_DIR)/SHA256SUMS`. Gói vẫn đủ 4 file. Cài bằng gói đó thì hỏng ở đâu, với thông báo nào?
3. `SHA256SUMS` chứng minh được điều gì và không chứng minh được điều gì? Muốn chắc gói không bị thay trên đường tới máy, bạn phải tin cái gì?
4. Vì sao `install.sh` chạy `tpan.install-tmp version` chứ không chạy `$src version` cho nhanh?
5. Xoá khối `systemctl restart tpan` ở 3.8. Cài lần đầu có sao không? Nâng từ bản A lên bản B thì sao?
6. Vì sao `install.sh` phải truyền `--yes` cho `tpan setup` và `tpan stack install`?
7. (Cần máy ảo) Trên máy đã cài xong bằng `install.sh --web nginx`, chứng minh `www-data` đi qua được `/var/lib/tpan` tới thư mục ACME nhưng không đọc được `tpan.db`.
8. (Cần máy ảo) Chứng minh rằng sau khi `install.sh` thay file chạy, tiến trình `tpan serve` đang chạy đúng file mới chứ không phải bản cũ đã bị `mv` đè.
9. `tpan setup` lần đầu trên máy mới: `/var/lib/tpan` được tạo với quyền gì trước, rồi thành quyền gì, do hàm nào?
10. `scripts/anh-readme` gửi `Page.captureScreenshot` rồi nhận về vài sự kiện của Chrome trước khi có trả lời. Vì sao `cdp.call` không lẫn sự kiện với trả lời?

## Đáp án

1. Bash đọc script từ ống dẫn tới đâu chạy tới đó. Mạng đứt giữa chừng thì phần đã nhận vẫn chạy, có khi dừng ngay giữa một lệnh bị cắt dở. Thân nằm trong `main`, và dòng cuối `main "$@"` là thứ duy nhất chạy việc; file bị cắt thì dòng đó chưa tới, bash chỉ định nghĩa hàm rồi hết.
2. Mỗi dòng sẽ ghi `dist/release/<bản>/tpan-linux-amd64` thay vì tên trần. `sum_line` tìm đúng tên `tpan-linux-<arch>` nên không thấy dòng nào (mã 1). Vì đang trong gói (`pkg=1`), `verify_sha256` dừng với `SHA256SUMS ... không có dòng nào cho tpan-linux-<arch>`. `tpan self-update` cũng hỏng tương tự ở `parseSums`.
3. Chứng minh file chạy khớp từng byte với dấu ghi trong `SHA256SUMS`, tức không hỏng khi tải hay chép. Không chứng minh `SHA256SUMS` là của bạn: ai thay được cả hai file thì vẫn qua. Phải tin nơi lấy `SHA256SUMS` (máy bạn tự `make release`, hoặc trang phát hành qua https; `fetch` bắt https cả khi chuyển hướng).
4. File tải bằng curl hay chép bằng scp thường không có bit x, và `/tmp` có máy gắn `noexec`, nên chạy `$src` có thể hỏng dù file tốt. Bản đã chép sang `/usr/local/bin/tpan.install-tmp` có chủ root, quyền 0755, nằm trên phân vùng chạy được: thử nó là thử đúng thứ sắp đặt vào chỗ.
5. Lần đầu không sao: dịch vụ chưa có, `is-active` trả khác 0, khối đó vốn bỏ qua. Nâng A lên B: file trên đĩa là B nhưng tiến trình `tpan serve` vẫn chạy A, vì `rename` không đụng tiến trình cũ và `tpan setup` không bao giờ restart một dịch vụ đang chạy (`steps.Service` thấy `active` và `enabled` là đã đúng). Panel chạy bản cũ tới lần khởi động lại sau.
6. Hai lệnh đó hỏi xác nhận trên terminal. Chạy kiểu `curl | bash` thì stdin là ống dẫn chứa chính script, không phải bàn phím, nên chúng không hỏi được và từ chối. `--yes` là người gõ `install.sh` đã đồng ý trước.
7. `scripts/vm.sh root stat -c '%a %U:%G %n' /var/lib/tpan /var/lib/tpan/acme /var/lib/tpan/tpan.db` cho 755, 755, 600 (chưa chạy trong mốc này; con số theo mã). Rồi `scripts/vm.sh root sudo -u www-data ls /var/lib/tpan/acme` phải chạy được, còn `scripts/vm.sh root sudo -u www-data cat /var/lib/tpan/tpan.db` phải báo `Permission denied`.
8. `scripts/vm.sh root bash -c 'p=$(systemctl show -p MainPID --value tpan); readlink /proc/$p/exe; /proc/$p/exe version; sha256sum /proc/$p/exe /usr/local/bin/tpan'`. Đúng bản mới thì `readlink` ra `/usr/local/bin/tpan` (không có chữ `(deleted)`) và hai dấu sha256 bằng nhau. Nếu tiến trình còn chạy bản cũ, `readlink` in `/usr/local/bin/tpan (deleted)` vì inode cũ đã mất tên. Phần `readlink` chưa chạy trong mốc này (output trên là theo cách Linux hoạt động); e2e chỉ chạy `/proc/$p/exe version` (2.2, 2.7).
9. Trước 0700, do `store.Open` (`os.MkdirAll(..., 0o700)`) khi `openStore` mở DB trước lúc chạy Plan. Rồi 0755, do bước `steps.Dir{Path: setupStateDir, Perm: core.StateDirPerm}` trong Plan của `buildSetupPlan`.
10. Mỗi lệnh gửi đi mang một `id` tăng dần (`c.next++`). Trả lời của Chrome mang lại đúng `id` đó; sự kiện thì không có `id` (đọc ra là 0). Vòng `for` trong `call` bỏ qua mọi thông điệp có `id` khác cho tới khi gặp đúng `id` của mình. Gọi tuần tự từng lệnh nên không cần luồng đọc riêng.
