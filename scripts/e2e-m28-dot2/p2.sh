#!/usr/bin/env bash
# e2e M28 đợt 2, P2: trần số lần argon2id chạy cùng lúc. N request đăng nhập
# song song (mặc định 20), mỗi cái từ một địa chỉ loopback riêng (127.0.0.101 trở
# lên) với mật khẩu sai; lấy mẫu VmRSS của tiến trình `tpan serve` mỗi 0,1 giây.
# Chạy bằng root trong máy ảo, sau dung-may.sh. Khẳng định của bản vá.
#
# Nguồn: địa chỉ 10.98.0.<n> gắn tạm vào lo (không phải loopback), mỗi request
# một địa chỉ, gọi thẳng tới chính địa chỉ đó: bản vá đếm kết nối loopback theo
# UID (mọi request của root chung một khoá, bị khoá sau 5 lần), nên muốn chạm
# tới semaphore argon2 thì mỗi request phải có khoá nguồn riêng.
#
# Tham số: N (mặc định: tính theo thời gian một lần argon2 đo trước, để tổng
# việc vượt hẳn 2 chỗ x 3 giây chờ; trong khoảng 20 đến 60).
. "$(dirname "$0")/chung2.sh"
N="${1:-}"
can_may
RND="$(rnd)"
CT='Content-Type: application/json'
RSS_TRAN_MIB=400
p0="$(pid)"
n0="$(nrestarts)"
[ -n "$p0" ] && [ "$p0" != 0 ] || bail "không có PID của tpan"
T="$W/p2"
mkdir -p "$T"

ip addr add 10.98.0.200/32 dev lo 2>/dev/null
t1="$(curl -sk -m 60 -o /dev/null -w "%{time_total}" --interface 10.98.0.200 -H "$CT" -d "{\"name\":\"admin\",\"password\":\"do-$RND\"}" https://10.98.0.200:8443$LOGIN_PATH)"
ip addr del 10.98.0.200/32 dev lo 2>/dev/null
xoa_khoa_ip 10.98.0.200 >/dev/null
if [ -z "$N" ]; then
  N="$(awk -v t="$t1" "BEGIN { n = int(8 / (t > 0.01 ? t : 0.01)) + 1; if (n < 20) n = 20; if (n > 60) n = 60; print n }")"
fi
[[ $N =~ ^[0-9]+$ ]] && [ "$N" -ge 1 ] && [ "$N" -le 150 ] || bail "N phải là số từ 1 đến 150"
echo "  một lần đăng nhập sai mất ${t1}s; N = $N"
IPS=()
for i in $(seq 1 "$N"); do IPS+=("10.98.0.$i"); ip addr add "10.98.0.$i/32" dev lo 2>/dev/null; done
cleanup() {
  : >"$T/stop"
  wait 2>/dev/null
  xoa_khoa_ip "${IPS[@]}"
  for ip in "${IPS[@]}"; do ip addr del "$ip/32" dev lo 2>/dev/null; done
  rm -rf "$T"
}
trap cleanup EXIT
xoa_khoa_ip "${IPS[@]}"

# rss_kib: VmRSS của tpan theo KiB (rỗng nếu tiến trình mất).
rss_kib() { awk '/^VmRSS:/ {print $2}' "/proc/$p0/status" 2>/dev/null; }

step "P2.0. mốc: tpan serve PID $p0"
base="$(rss_kib)"
[ -n "$base" ] || bail "không đọc được VmRSS của PID $p0"
echo "  RSS trước: $((base / 1024)) MiB; NRestarts $n0"
free -m | sed -n 1,2p | sed 's/^/  /'
oom0="$(dmesg 2>/dev/null | grep -ci 'out of memory')"

step "P2.1. $N request đăng nhập song song, mỗi cái từ một địa chỉ riêng; lấy mẫu RSS mỗi 0,1 giây"
(
  peak=0
  while [ ! -e "$T/stop" ]; do
    r="$(rss_kib)"
    [ -n "$r" ] && [ "$r" -gt "$peak" ] && peak="$r"
    echo "$peak" >"$T/peak"
    sleep 0.1
  done
) &
SAMPLER=$!
i=0
for ip in "${IPS[@]}"; do
  i=$((i + 1))
  curl -sk -m 60 -o /dev/null -w '%{http_code} %{time_total}\n' --interface "$ip" -H "$CT" \
    -d "{\"name\":\"admin\",\"password\":\"dos-$i-$RND\"}" "https://$ip:8443$LOGIN_PATH" >"$T/c.$i" 2>/dev/null &
done
# chỉ chờ các curl, không chờ vòng lấy mẫu
for j in $(jobs -p); do [ "$j" = "$SAMPLER" ] || wait "$j"; done
sleep 1
: >"$T/stop"
wait "$SAMPLER"
peak="$(cat "$T/peak")"
cat "$T"/c.* | awk '{print $1}' | sort | uniq -c | sed 's/^/  mã /'
echo "  thời gian: nhanh nhất $(awk '{print $2}' "$T"/c.* | sort -n | head -1)s, chậm nhất $(awk '{print $2}' "$T"/c.* | sort -n | tail -1)s"
n429="$(awk '$1 == 429' "$T"/c.* | wc -l | tr -d ' ')"
tang=$(((peak - base) / 1024))
echo "  RSS đỉnh $((peak / 1024)) MiB, tăng $tang MiB so với trước; 429: $n429; dòng OOM mới trong dmesg: $(($(dmesg 2>/dev/null | grep -ci 'out of memory') - oom0))"

step "P2.2. khẳng định"
ok "RSS đỉnh trừ RSS trước không quá $RSS_TRAN_MIB MiB (tăng $tang MiB)" test "$tang" -le "$RSS_TRAN_MIB"
ok "có ít nhất một câu 429 ($n429 trên $N)" test "$n429" -ge 1
ok "tpan: PID và NRestarts không đổi" song "$p0" "$n0" || doi_tpan
