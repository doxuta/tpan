#!/usr/bin/env bash
# e2e M28, khẳng định 1 và 3: bản xem trước của khach1 (POST
# /api/v1/sites?dry_run=1 cho site e.example.com của chính nó). Chạy bằng root
# trong máy ảo, sau dung-may.sh.
#
# Apache: 1 (.htaccess thường), 3 (thư mục cha là symlink, hardlink, tên lạ).
# nginx: bản xem trước không có file nào dưới /var/www/, và phần tên lạ của 3.
. "$(dirname "$0")/chung.sh"
can_may
V=/srv/m28/nan-nhan
TEN_LA=()

don() {
  sysctl -q -w fs.protected_hardlinks=1
  if [ -L "$PUB" ] && [ -d "$PUB.that" ]; then
    as rm -f "$PUB"
    as mv "$PUB.that" "$PUB"
  fi
  if [ ${#TEN_LA[@]} -gt 0 ]; then
    as python3 -c 'import os,sys
for n in sys.argv[2:]:
    try: os.unlink(os.path.join(sys.argv[1], n))
    except FileNotFoundError: pass' "$PUB" "${TEN_LA[@]}"
  fi
  tra_lai
  rm -rf /srv/m28
}
trap don EXIT
login khach1 /root/m28-khach1.pw || bail "khach1 không đăng nhập được"

# thuong_ok: khẳng định 1 trên câu trả lời $W/xt.resp.
thuong_ok() {
  ok "$1: HTTP 200, mục .htaccess có old_exists true, old đúng bằng file trên đĩa, new khác rỗng" \
    bash -c '[ "$1" = 200 ] && python3 "$2" thuong "$3" "$4" "$4"' _ "$HTTP" "$D/phan-tich.py" "$W/xt.resp" "$HT"
}

if [ "$WEB" = apache ]; then
  step "1. .htaccess là file thường (hành vi đúng phải giữ)"
  tra_lai
  stat -c '  %n: %F, %U:%G %a, %s byte' "$HT"
  dry "$W/xt.resp" 60
  thuong_ok ".htaccess thường"
  le "$TIME" 5 && echo "  (bản xem trước mất ${TIME}s)"

  step "3a. thư mục cha: public -> symlink tới thư mục chỉ root đọc được"
  mkdir -p "$V"
  chmod 700 /srv/m28 "$V"
  head -c 600 /dev/urandom | base64 >"$V/.htaccess"
  # Có sẵn wp-config.php ở thư mục nạn nhân (như public của một site khác):
  # không có thì core từ chối sớm vì "site lệch", chưa tới chỗ đọc .htaccess.
  cp "/var/www/$VICTIM/public/wp-config.php" "$V/wp-config.php"
  as mv "$PUB" "$PUB.that"
  as ln -s "$V" "$PUB"
  p0="$(pid)"
  n0="$(nrestarts)"
  dry "$W/xt.resp" 60
  ok "cha là symlink: câu trả lời không chứa file nào của thư mục nạn nhân" pt khong-lo "$W/xt.resp" "$V/.htaccess" "$V/wp-config.php"
  as rm "$PUB"
  as mv "$PUB.that" "$PUB"
  song "$p0" "$n0" || doi_tpan

  step "3b. hardlink tới /etc/shadow"
  sysctl fs.protected_hardlinks
  as rm -f "$HT"
  if as ln /etc/shadow "$HT" 2>"$W/ln.err"; then
    echo "  ln thành công dù fs.protected_hardlinks=$(sysctl -n fs.protected_hardlinks)"
    lnrc=0
  else
    echo "  ln: $(cat "$W/ln.err")"
    lnrc=1
  fi
  ok "fs.protected_hardlinks=1: kernel từ chối user site tạo hardlink tới /etc/shadow" test "$lnrc" -eq 1
  echo "  tắt fs.protected_hardlinks (máy bị cấu hình lỏng) rồi thử lại"
  sysctl -q -w fs.protected_hardlinks=0
  as rm -f "$HT"
  if as ln /etc/shadow "$HT" 2>"$W/ln.err"; then
    stat -c '  %n: %F, chủ %U:%G, quyền %a, %h tên' "$HT"
    p0="$(pid)"
    n0="$(nrestarts)"
    dry "$W/xt.resp" 60
    ok "hardlink tới /etc/shadow: câu trả lời không chứa file đích" pt khong-lo "$W/xt.resp" /etc/shadow
    ok "hardlink tới /etc/shadow: HTTP 409, error.code site_path_unsafe" \
      bash -c '[ "$1" = 409 ] && python3 "$2" loi "$3" site_path_unsafe' _ "$HTTP" "$D/phan-tich.py" "$W/xt.resp"
    song "$p0" "$n0" || doi_tpan
  else
    ok "tắt fs.protected_hardlinks thì user site tạo được hardlink (điều kiện của phép thử)" false
    echo "  ln: $(cat "$W/ln.err")"
  fi
  sysctl -q -w fs.protected_hardlinks=1
  sysctl fs.protected_hardlinks
  tra_lai
else
  step "nginx: bản xem trước của khach1 không có file nào trong vùng site"
  dry "$W/xt.resp" 60
  ok "nginx: HTTP 200, không file nào trong bản xem trước có đường dẫn bắt đầu bằng /var/www/" \
    bash -c '[ "$1" = 200 ] && python3 "$2" khong-var-www "$3"' _ "$HTTP" "$D/phan-tich.py" "$W/xt.resp"
fi

step "3c. tên lạ trong public do user site tạo (xuống dòng, -rf, 255 ký tự)"
TEN_LA=($'m28-xuong\ndong.txt' '-rf' "$(printf 'm%.0s' $(seq 1 255))")
as python3 -c 'import os,sys
for n in sys.argv[2:]:
    fd = os.open(os.path.join(sys.argv[1], n), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o640)
    os.write(fd, b"m28\n"); os.close(fd)
print("  đã tạo", len(sys.argv) - 2, "file, độ dài tên:", [len(n) for n in sys.argv[2:]])' "$PUB" "${TEN_LA[@]}"
p0="$(pid)"
n0="$(nrestarts)"
dry "$W/xt.resp" 60
if [ "$WEB" = apache ]; then
  thuong_ok "tên lạ trong public"
else
  ok "tên lạ trong public: HTTP 200, bản xem trước không có file dưới /var/www/" \
    bash -c '[ "$1" = 200 ] && python3 "$2" khong-var-www "$3"' _ "$HTTP" "$D/phan-tich.py" "$W/xt.resp"
fi
ok "tên lạ: tpan còn sống, PID không đổi" song "$p0" "$n0"
rm -f "$W/xt.resp" "$W/ln.err"
