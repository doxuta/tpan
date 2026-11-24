#!/usr/bin/env bash
# e2e M28 đợt 2, F4 (và D5): user Linux của site e.example.com (K1) đổi tên
# `public` rồi đặt symlink `public` sang thư mục của site b.example.com. Chạy
# bằng root trong máy ảo, sau dung-may.sh, cả nginx lẫn Apache.
#
# Tham số:
#   chan (mặc định)  khẳng định của bản vá: web server không trả file của B qua
#                    tên miền của e (403 hay 404, không có chuỗi đánh dấu), ca
#                    hợp lệ vẫn 200, health có dòng hỏng nhắc tới public là symlink.
#   lo               bản chưa vá (v0.2.0 ở phần nâng cấp): khẳng định bộ thử BẮT
#                    ĐƯỢC lỗ (thân trả về có chuỗi đánh dấu); ca hợp lệ vẫn 200.
#                    Không khẳng định health (bản chưa vá không có phép kiểm đó).
#
# Chuỗi đánh dấu là chuỗi ngẫu nhiên vô hại, không phải bí mật thật. File của B
# 640, chủ user của B, nhóm www-data (như file thường của site), để www-data
# (user của web server) đọc được: nếu không thì 403 sẽ đến từ quyền file chứ
# không từ bản vá. Điều kiện đó được khẳng định trước.
. "$(dirname "$0")/chung2.sh"
MODE="${1:-chan}"
can_may
UB="$(stat -c '%U' "/var/www/$OTHER")"
DE="/var/www/$DOM"
DB="/var/www/$OTHER"
RND="$(rnd)"
M1="M28F4-AN-$RND"
M2="M28F4-NGOAI-$RND"
M3="M28F4-HOPLE-$RND"
BAK="$DE/public.that-$RND"

# tra_f4: đưa `public` thật về chỗ cũ, xoá mọi file thử.
tra_f4() {
  if [ -d "$BAK" ]; then
    [ -L "$DE/public" ] && as rm -f "$DE/public"
    [ -e "$DE/public" ] || as mv "$BAK" "$DE/public"
  fi
  rm -rf "$DB/public/.bimat-$RND" "$DB/rieng-ngoai-$RND" "$DE/ban-moi-$RND" "$W/f4.body" "$W/f4.health"
}
trap tra_f4 EXIT

# tro <đích>: user của e đổi `public` thật thành public.that-<RND>, đặt symlink `public` sang đích.
tro() {
  [ -L "$DE/public" ] && as rm -f "$DE/public"
  [ -d "$DE/public" ] && as mv "$DE/public" "$BAK"
  as ln -s "$1" "$DE/public"
}
# khoi_phuc: đưa `public` thật về (giữa các ca).
khoi_phuc() {
  [ -L "$DE/public" ] && as rm -f "$DE/public"
  [ -e "$DE/public" ] || as mv "$BAK" "$DE/public"
}

step "F4.0. file của B (640, thư mục ẩn và ngoài docroot), user Linux của B là $UB"
mkdir "$DB/public/.bimat-$RND" "$DB/rieng-ngoai-$RND"
printf '%s\n' "$M1" >"$DB/public/.bimat-$RND/config"
printf '%s\n' "$M2" >"$DB/rieng-ngoai-$RND/dump.sql"
chown -R "$UB:www-data" "$DB/public/.bimat-$RND" "$DB/rieng-ngoai-$RND"
chmod 2750 "$DB/public/.bimat-$RND" "$DB/rieng-ngoai-$RND"
chmod 640 "$DB/public/.bimat-$RND/config" "$DB/rieng-ngoai-$RND/dump.sql"
stat -c '  %n: %U:%G %a' "$DB/public/.bimat-$RND/config" "$DB/rieng-ngoai-$RND/dump.sql"
ok "www-data đọc được hai file 640 của B (điều kiện để phép thử có nghĩa)" \
  bash -c 'runuser -u www-data -- cat "$1" "$2" >/dev/null' _ "$DB/public/.bimat-$RND/config" "$DB/rieng-ngoai-$RND/dump.sql"
ok "user $SU của e không đọc được file của B (đúng cô lập file)" \
  bash -c '! runuser -u "$1" -- cat "$2" >/dev/null 2>&1' _ "$SU" "$DB/rieng-ngoai-$RND/dump.sql"
ok "$DOM trả 200 trước khi tráo public" web200 "$DOM"

step "F4.1 (D1). public của $DOM -> symlink sang thư mục ẩn của B (file 640): $MODE"
tro "$DB/public/.bimat-$RND"
ls -l "$DE/public" | sed 's/^/  /'
get "$DOM" config "$W/f4.body"
if [ "$MODE" = lo ]; then
  ok "bản chưa vá, D1: thân trả về CÓ chuỗi đánh dấu của B (bộ thử bắt được lỗ)" co "$W/f4.body" "$M1"
else
  ok "D1: không có chuỗi đánh dấu của B trong thân" khong_co "$W/f4.body" "$M1"
  ok "D1: mã là 403 hay 404 (HTTP $GOT)" bi_chan
fi

step "F4.2 (D5). trong lúc lỗ mở: tpan health $DOM"
health_site "$DOM" "$W/f4.health"
echo "  số dòng health: $(grep -c . "$W/f4.health"); dòng [lỗi]: $(grep -cE "$RE_HEALTH_LOI" "$W/f4.health")"
grep -E "$RE_HEALTH_LOI" "$W/f4.health" | grep -iE "$RE_HEALTH_DOCROOT" | cut -c1-300 | sed 's/^/  khớp: /'
if [ "$MODE" = lo ]; then
  note "bản chưa vá: không khẳng định health (phép kiểm site.docroot chưa có)"
else
  ok "health có một dòng [lỗi] nhắc tới public là symlink" \
    bash -c 'grep -E "$1" "$3" | grep -qiE "$2"' _ "$RE_HEALTH_LOI" "$RE_HEALTH_DOCROOT" "$W/f4.health"
fi

step "F4.3 (D2). public của $DOM -> symlink sang thư mục ngoài docroot của B (file 640): $MODE"
tro "$DB/rieng-ngoai-$RND"
get "$DOM" dump.sql "$W/f4.body"
if [ "$MODE" = lo ]; then
  ok "bản chưa vá, D2: thân trả về CÓ chuỗi đánh dấu của B (bộ thử bắt được lỗ)" co "$W/f4.body" "$M2"
else
  ok "D2: không có chuỗi đánh dấu của B trong thân" khong_co "$W/f4.body" "$M2"
  ok "D2: mã là 403 hay 404 (HTTP $GOT)" bi_chan
fi
khoi_phuc
ok "public của $DOM là thư mục thật trở lại" test -d "$DE/public" -a ! -L "$DE/public"

step "F4.4. ca hợp lệ: symlink BÊN TRONG public (public/m28-hople) sang thư mục khác của CHÍNH $DOM"
# public là symlink thì bản vá coi là không hỗ trợ (site.docroot hỏng, backup từ
# chối); symlink bên trong public trỏ vào thứ của chính site thì phải chạy.
mkdir "$DE/ban-moi-$RND"
printf '%s\n' "$M3" >"$DE/ban-moi-$RND/ok.txt"
chown -R "$SU:www-data" "$DE/ban-moi-$RND"
chmod 2750 "$DE/ban-moi-$RND"
chmod 640 "$DE/ban-moi-$RND/ok.txt"
as ln -s "$DE/ban-moi-$RND" "$DE/public/m28-hople"
ls -l "$DE/public/m28-hople" | sed 's/^/  /'
get "$DOM" m28-hople/ok.txt "$W/f4.body"
ok "ca hợp lệ: HTTP 200 (chủ link và chủ đích cùng là $SU)" test "$GOT" = 200
ok "ca hợp lệ: thân đúng nội dung thư mục mới" co "$W/f4.body" "$M3"
as rm -f "$DE/public/m28-hople"

step "F4.5. kết thúc: public thật ở chỗ cũ, site e và site b trả 200"
ok "public của $DOM là thư mục thật" test -d "$DE/public" -a ! -L "$DE/public"
ok "$DOM trả 200 sau khi trả public" web200 "$DOM"
ok "$OTHER trả 200" web200 "$OTHER"
