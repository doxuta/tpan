# shellcheck shell=bash
# Phần dùng chung của các script e2e M28 đợt 2 chạy TRONG máy ảo bằng root. Mỗi
# script nạp: . "$(dirname "$0")/chung2.sh" (file này tự nạp chung.sh của đợt 1;
# driver chép cả hai thư mục vào cùng một chỗ trong máy ảo).
#
# Bản thiết kế đợt 2 chưa qua soát: mọi chuỗi có thể đổi (tên mã lỗi, dòng
# health, hằng số) gom thành biến ngay dưới đây.
# shellcheck source=../e2e-m28/chung.sh
. "$(dirname "${BASH_SOURCE[0]}")/chung.sh"

# Dòng [lỗi] của health nhắc tới public là symlink (phép kiểm site.docroot).
# Health in mô tả tiếng Việt, không in ID phép kiểm, nên khớp theo từ khoá.
RE_HEALTH_LOI='^\[lỗi\]'
RE_HEALTH_DOCROOT='public.*(symlink|liên kết)|(symlink|liên kết).*public'
# Trần kết nối DB của user site (steps.DBUserMaxConnections) và số kết nối thử.
DB_TRAN=20
DB_THU=160
# Panel thật. Request tấn công đi từ 127.0.0.<n> tới 127.0.0.1 (nguồn khác nhau,
# đích là cổng panel đã nghe sẵn).
PANEL=https://127.0.0.1:8443
LOGIN_PATH=/api/v1/login
TPAN_DB=/var/lib/tpan/tpan.db
# Nguồn riêng của từng phép thử, để khoá IP không chặn các bước khác (đi từ 127.0.0.1).
IP_P1A=127.0.0.31
IP_P1B=127.0.0.32
IP_P1C=127.0.0.33
IP_P2_PREFIX=127.0.0. # cộng 101..120

# Hậu tố ngẫu nhiên cho tên file và chuỗi đánh dấu (không phải bí mật thật).
rnd() { head -c 5 /dev/urandom | od -An -tx1 | tr -d ' \n'; }

# get <domain> <đường dẫn không có / đầu> <file thân>: GET qua 127.0.0.1 với
# tên miền đó, thân vào file (thư mục $W chỉ root đọc), mã vào GOT.
GOT=""
get() {
  GOT="$(curl -s -m 20 -o "$3" -w '%{http_code}' --resolve "$1:80:127.0.0.1" "http://$1/$2")"
  echo "  GET $1/$2: HTTP $GOT, $(wc -c <"$3") byte"
}

# khong_co <file> <chuỗi>: file không chứa chuỗi đó.
khong_co() { ! grep -qF -- "$2" "$1"; }
# co <file> <chuỗi>
co() { grep -qF -- "$2" "$1"; }
# bi_chan: GOT là 403 hoặc 404.
bi_chan() { [ "$GOT" = 403 ] || [ "$GOT" = 404 ]; }

# xoa_khoa_ip <ip>...: xoá dòng khoá đăng nhập của các IP thử (python3 có sẵn sqlite3).
xoa_khoa_ip() {
  python3 -I - "$TPAN_DB" "$@" <<'PY'
import sqlite3, sys
c = sqlite3.connect(sys.argv[1], timeout=10)
n = 0
for ip in sys.argv[2:]:
    n += c.execute("DELETE FROM login_failures WHERE ip = ?", (ip,)).rowcount
c.commit()
print("  dọn: xoá", n, "dòng login_failures của IP thử")
PY
}

# health_site <domain> <file>: các dòng health của một site vào file. Thử
# `tpan health <domain>`; nếu không ra dòng nào dạng [..] thì lấy phần "== Site
# <domain> ==" của health đầy đủ.
health_site() {
  tpan health "$1" </dev/null >"$2" 2>&1
  grep -q '^\[' "$2" && return 0
  tpan health </dev/null >"$2.full" 2>&1
  awk -v h="== Site $1 ==" 'index($0, h) {f = 1; next} /^== / {f = 0} f' "$2.full" >"$2"
  rm -f "$2.full"
}
