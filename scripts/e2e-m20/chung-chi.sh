#!/usr/bin/env bash
# ĐỒ NGHỀ TEST của scripts/e2e-m20.sh (T50), chỉ chạy TRONG máy ảo test, bằng
# root. Đọc chứng chỉ panel ĐANG PHỤC VỤ bằng openssl s_client (độc lập với
# tpan) và so với hai file trên đĩa.
#
#   chung-chi.sh <tên SNI | ->     ("-": không gửi SNI, như vào bằng IP)
#
# In (không có bí mật):
#   phuc-vu: serial=<…> tu-ky=<co|khong> issuer=<…> san=<…>
#   tu-ky-tren-dia: serial=<…>                  (/etc/tpan/tls/cert.pem)
#   lineage-tpan-panel: serial=<…> san=<…>      (hoặc "lineage-tpan-panel: không có")
set -uo pipefail
sni=$1
args=(-connect 127.0.0.1:8443)
if [ "$sni" = - ]; then args+=(-noservername); else args+=(-servername "$sni"); fi
pem=$(openssl s_client "${args[@]}" </dev/null 2>/dev/null | openssl x509 2>/dev/null)
if [ -z "$pem" ]; then
  echo "phuc-vu: không đọc được chứng chỉ"
else
  serial=$(openssl x509 -noout -serial <<<"$pem" | cut -d= -f2)
  iss=$(openssl x509 -noout -issuer -nameopt RFC2253 <<<"$pem" | sed 's/^issuer=//')
  sub=$(openssl x509 -noout -subject -nameopt RFC2253 <<<"$pem" | sed 's/^subject=//')
  san=$(openssl x509 -noout -ext subjectAltName <<<"$pem" 2>/dev/null | sed -n '2p' | tr -d ' ')
  self=khong
  [ "$iss" = "$sub" ] && self=co
  echo "phuc-vu: serial=$serial tu-ky=$self issuer=$iss san=$san"
fi
echo "tu-ky-tren-dia: serial=$(openssl x509 -in /etc/tpan/tls/cert.pem -noout -serial | cut -d= -f2)"
f=/etc/letsencrypt/live/tpan-panel/fullchain.pem
if [ -f "$f" ]; then
  echo "lineage-tpan-panel: serial=$(openssl x509 -in "$f" -noout -serial | cut -d= -f2) san=$(openssl x509 -in "$f" -noout -ext subjectAltName 2>/dev/null | sed -n '2p' | tr -d ' ')"
else
  echo "lineage-tpan-panel: không có"
fi
