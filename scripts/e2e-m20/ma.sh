#!/usr/bin/env bash
# ĐỒ NGHỀ TEST của scripts/e2e-m20.sh, chỉ chạy TRONG máy ảo test (user ubuntu).
#
#   ma.sh dung <file bí mật> <lệch giây> <bước tối thiểu> <file ra>
#   ma.sh sai  <file bí mật> <file ra>
#
# dung: chờ tới khi bước TOTP của (giờ máy + lệch) >= bước tối thiểu và đã vào
#   bước đó ít nhất 2 giây, rồi tính mã của đúng thời điểm đó bằng oathtool (gói
#   Ubuntu, độc lập với tpan) từ cùng bí mật.
# sai: một mã 6 số chắc chắn sai: khác mã của mọi bước từ −3 tới +3 quanh giờ máy.
#
# Mã ghi vào <file ra> (0600) dạng {"code":"<mã>"} để script trên Mac gửi bằng
# curl --data @file. Bí mật chỉ đi từ file vào oathtool bên trong script này;
# script KHÔNG in mã hay bí mật, chỉ in số bước (không phải bí mật).
set -uo pipefail
umask 077

totp_at() { # totp_at <giây unix>
  oathtool --totp -b --now "$(date -u -d "@$1" '+%Y-%m-%d %H:%M:%S UTC')" "$(cat "$secret_file")"
}

case "${1:-}" in
dung)
  secret_file=$2 off=$3 min=$4 out=$5
  for _ in $(seq 1 120); do
    t=$(($(date +%s) + off))
    step=$((t / 30))
    [ "$step" -ge "$min" ] && [ $((t % 30)) -ge 2 ] && break
    sleep 1
  done
  code=$(totp_at "$t") || exit 1
  [[ $code =~ ^[0-9]{6}$ ]] || { echo "oathtool không ra mã 6 số"; exit 1; }
  printf '{"code":"%s"}' "$code" >"$out"
  printf 'bước %s (giờ máy %+d giây, cần bước >= %s); mã ghi vào file, không in\n' "$step" "$off" "$min"
  ;;
sai)
  secret_file=$2 out=$3
  now=$(date +%s)
  near=" "
  for d in -3 -2 -1 0 1 2 3; do near="$near$(totp_at $((now + d * 30))) "; done
  for c in 000000 111111 222222 333333 444444 555555 666666 777777; do
    case "$near" in *" $c "*) continue ;; esac
    printf '{"code":"%s"}' "$c" >"$out"
    echo "mã sai: khác mã của bảy bước quanh giờ máy (bước $((now / 30)) ±3); không in"
    exit 0
  done
  exit 1
  ;;
*)
  echo "cách dùng: ma.sh dung <bí mật> <lệch> <bước tối thiểu> <ra> | ma.sh sai <bí mật> <ra>" >&2
  exit 2
  ;;
esac
