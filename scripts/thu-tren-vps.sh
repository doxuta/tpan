#!/usr/bin/env bash
# Thử TPAN trên một VPS Ubuntu 24.04 sạch bằng một lệnh, cho chủ dự án tự chạy
# (agent không chạy gì trên VPS: luật 1 của AGENTS.md). Không cần tên miền: site
# thử là thu.example.com, gọi bằng curl --resolve ngay trên máy. Không xin SSL
# (cần tên miền thật trỏ về máy, xem docs/HUONG-DAN-CHU-DU-AN.md mục 3.3).
#
#   curl -fsSL https://raw.githubusercontent.com/doxuta/tpan/main/scripts/thu-tren-vps.sh | sudo bash
#
# Làm lần lượt: cài tpan từ bản phát hành mới nhất (install.sh --web nginx),
# health, tạo site, thùng rác (xoá rồi khôi phục), fail2ban, ufw (tpan tự mở cổng
# SSH đang dùng trước khi bật), backup và lịch backup, đổi nginx sang apache rồi
# về, PHP, cập nhật, self-update (chỉ xem trước). Cuối cùng in bảng đạt/hỏng.
#
# Log đầy đủ: /root/tpan-thu.log. Bản đã che mật khẩu quản trị và mọi địa chỉ
# IPv4 (trừ 127.x): /root/tpan-thu-che.log; gửi bản đó, không gửi bản gốc.
set -u

LOG=/root/tpan-thu.log
CHE=/root/tpan-thu-che.log
D=thu.example.com
BASE=https://github.com/doxuta/tpan/releases/latest/download
PASS=0
FAIL=0
KQ=()


buoc() { printf '\n######## %s\n' "$*"; }

# kiem <tên> <lệnh...>: chạy lệnh, in output, ghi đạt khi mã thoát 0.
kiem() {
  local ten=$1
  shift
  printf '\n$ %s\n' "$*"
  if "$@"; then
    PASS=$((PASS + 1)); KQ+=("đạt   $ten")
  else
    FAIL=$((FAIL + 1)); KQ+=("HỎNG  $ten (mã thoát $?)")
  fi
}

# site200: thu.example.com trả 200 khi gọi ngay trên máy.
site200() {
  local c
  c="$(curl -s -o /dev/null -w '%{http_code}' -m 20 --resolve "$D:80:127.0.0.1" "http://$D/")"
  echo "HTTP $c"
  [ "$c" = 200 ]
}

# khong_loi: tpan health không có phép kiểm nào lỗi (cảnh báo thì được: site thử
# không có bản ghi DNS, chưa có SSL, chưa sao lưu tpan.db lần nào).
khong_loi() { tpan health; [ "$?" = 0 ]; }

# Toàn bộ thân nằm trong main, dòng cuối mới gọi: chạy kiểu `curl | bash` thì
# bash đọc script từ stdin, nên main chuyển stdin sang /dev/null để không lệnh
# con nào nuốt mất phần script còn lại; tải đứt giữa chừng thì không có nửa
# script nào được chạy.
main() {
[ "$(id -u)" = 0 ] || { echo "Chạy bằng root: ... | sudo bash" >&2; exit 1; }
exec </dev/null > >(tee "$LOG") 2>&1

buoc "Máy"
kiem "Ubuntu 24.04" grep -q 'VERSION_ID="24.04"' /etc/os-release
uname -m
nproc
free -h
df -h /

buoc "Cài tpan từ bản phát hành mới nhất trên GitHub"
kiem "cài bằng install.sh --web nginx" bash -c "curl -fsSL $BASE/install.sh | bash -s -- --web nginx"
kiem "tpan version" tpan version
kiem "dịch vụ tpan, nginx, php8.3-fpm, mariadb đang chạy" systemctl is-active tpan nginx php8.3-fpm mariadb
kiem "chạy lại install.sh không lỗi (không đổi gì)" bash -c "curl -fsSL $BASE/install.sh | bash -s -- --web nginx >/dev/null"
kiem "health không có lỗi" khong_loi

buoc "Site thử $D"
kiem "tạo site" tpan site add "$D" --email admin@example.com --yes
kiem "site trả 200" site200
kiem "health của site không có lỗi" tpan health "$D"
kiem "chạy lại tạo site không lỗi" tpan site add "$D" --email admin@example.com --yes

buoc "Thùng rác"
kiem "xoá site vào thùng rác" tpan site rm "$D" --yes
kiem "thùng rác có site" bash -c "tpan trash list | grep -q '$D'"
kiem "khôi phục site" tpan site restore "$D" --yes
kiem "site trả 200 sau khôi phục" site200

buoc "Bảo mật"
sshd -T | grep -i '^port'
kiem "fail2ban" tpan security fail2ban --yes
kiem "ufw (xem trước)" tpan security ufw --dry-run
kiem "ufw" tpan security ufw --yes
ufw status verbose
kiem "site vẫn 200 sau ufw" site200

buoc "Backup"
kiem "backup mọi site" tpan backup create --all --yes
kiem "danh sách backup" tpan backup list
kiem "lịch backup 03:30, giữ 5 bản" tpan backup settings --keep 5 --at 03:30 --yes

buoc "Đổi web server"
kiem "nginx sang apache" tpan stack switch --to apache --yes
kiem "site 200 trên apache" site200
kiem "apache về nginx" tpan stack switch --to nginx --yes
kiem "site 200 trên nginx" site200

buoc "PHP và cập nhật"
kiem "danh sách PHP" tpan php list
kiem "xem gói chờ cập nhật" tpan update --check
kiem "self-update (xem trước)" tpan self-update --dry-run

buoc "Kiểm lại cuối"
kiem "health cuối không có lỗi" khong_loi

buoc "KẾT QUẢ"
printf '%s\n' "${KQ[@]}"
printf '\n%s đạt, %s hỏng\n' "$PASS" "$FAIL"
printf 'Panel: https://<IP của máy>:8443 (mật khẩu quản trị in ở phần cài, chỉ một lần)\n'
printf 'Log đã che để gửi: %s\n' "$CHE"

# Che mật khẩu quản trị (dòng ngay sau tiêu đề) và mọi địa chỉ IPv4.
awk '/Mật khẩu quản trị|admin password/ {print; getline; print "  <đã che>"; next} {print}' "$LOG" |
  sed -E 's/\b([0-9]{1,3}\.){3}[0-9]{1,3}\b/<ip>/g' >"$CHE"
chmod 600 "$LOG" "$CHE"
}

main "$@"
