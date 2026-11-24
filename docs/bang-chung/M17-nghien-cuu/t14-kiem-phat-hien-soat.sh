# t14: thử các điểm "cần thử" của đợt soát thiết kế (cuối M17-thiet-ke.md).
#  P4. Tiến trình cùng uid với site chạy NGOÀI lồng (runuser) có làm lộ gốc thật
#      của máy qua /proc/<pid>/root cho mã trong lồng không; fd giữ lại có dùng
#      được sau khi tiến trình ngoài lồng đã thoát không.
#  P5. /proc/self/net/unix, /proc/self/net/tcp trong lồng lộ gì.
#  P1. Hạ trần RAM xuống 8M khi đang chạy, chờ, trả trần 200M: site có tự sống lại
#      không, hay socket và dịch vụ kẹt ở failed cho tới khi reset-failed.
#  P11. cpu.max và io.max khi không đặt trần CPU và khi có trần đĩa (mẫu cho test).
S=$SA; SVC=tpan-cage-$S-php.service; SK=tpan-cage-$S-php.socket; CG=/sys/fs/cgroup/tpan.slice/tpan-$S.slice
long_go $A >/dev/null 2>&1; long_go $B >/dev/null 2>&1
long_dung $A 200 0 40; long_vao $A >/dev/null; echo "A trong lồng: HTTP $(code_a '')"

echo; echo "## P4. /proc/<pid>/root của tiến trình cùng uid ngoài lồng"
runuser -u $UA -- sleep 25 &
OUT_PID=$!; sleep 0.5
systemd-run --quiet --wait --pipe --collect --unit=tpan-cage-$S-run-p4 -- sh -c '
  p=$(pgrep -u '"$UA"' -x sleep | head -1)
  echo "  trong lồng thấy pid của sleep ngoài lồng: [$p]"
  [ -n "$p" ] || exit 0
  echo "  ls /proc/$p/root/var/www: $(ls /proc/$p/root/var/www 2>&1 | tr "\n" " ")"
  echo "  ls /proc/$p/root/etc/nginx/sites-enabled: $(ls /proc/$p/root/etc/nginx/sites-enabled 2>&1 | tr "\n" " ")"
  exec 3</proc/$p/root
  echo "  đã giữ fd 3; chờ sleep ngoài lồng thoát..."
  while kill -0 $p 2>/dev/null; do sleep 1; done
  echo "  sleep đã thoát; ls /proc/self/fd/3/var/www: $(ls /proc/self/fd/3/var/www 2>&1 | tr "\n" " ")"
  echo "  ghi được vào /proc/self/fd/3/tmp: $(touch /proc/self/fd/3/tmp/tpan-p4-thu 2>&1 && echo CO || echo KHONG)"
' 2>&1
wait $OUT_PID 2>/dev/null
echo "  file /tmp/tpan-p4-thu ở máy thật: $(test -e /tmp/tpan-p4-thu && echo CO || echo KHONG)"; rm -f /tmp/tpan-p4-thu
echo "  kernel.yama.ptrace_scope=$(sysctl -n kernel.yama.ptrace_scope)"

echo; echo "## P5. /proc/self/net trong lồng"
systemd-run --quiet --wait --pipe --collect --unit=tpan-cage-$S-run-p5 -- sh -c '
  echo "  số dòng có \"tpan\" trong /proc/self/net/unix: $(grep -c tpan /proc/self/net/unix)"
  grep -o "/run/[^ ]*tpan[^ ]*" /proc/self/net/unix | sort -u | head -8 | sed "s/^/    /"
  echo "  số dòng trong /proc/self/net/tcp: $(wc -l < /proc/self/net/tcp) | tcp6: $(wc -l < /proc/self/net/tcp6)"
' 2>&1

echo; echo "## P1. Hạ trần RAM xuống 8M khi đang chạy rồi trả lại"
code_a '' >/dev/null
sed -i 's/^MemoryMax=.*/MemoryMax=8M/' /etc/systemd/system/tpan-$S.slice; systemctl daemon-reload
for i in 1 2 3 4 5 6; do code_a '' >/dev/null; sleep 2; done
echo "  sau 12 giây ở 8M: dịch vụ $(systemctl is-active $SVC) ($(systemctl show -p Result --value $SVC), NRestarts=$(systemctl show -p NRestarts --value $SVC)) | socket $(systemctl is-active $SK) ($(systemctl show -p Result --value $SK)) | file socket $(test -S $(sock_of $A) && echo CO || echo MAT)"
sed -i 's/^MemoryMax=.*/MemoryMax=200M/' /etc/systemd/system/tpan-$S.slice; systemctl daemon-reload; sleep 1
echo "  trả 200M + daemon-reload: A HTTP $(code_a '') | dịch vụ $(systemctl is-active $SVC) | socket $(systemctl is-active $SK)"
systemctl try-restart $SVC; echo "  thêm try-restart: A HTTP $(code_a '')"
systemctl reset-failed $SVC $SK; systemctl start $SK; sleep 0.5
echo "  reset-failed + start socket: A HTTP $(code_a '') | dịch vụ $(systemctl is-active $SVC) | socket $(systemctl is-active $SK)"

echo; echo "## P11. cpu.max, io.max theo trần"
long_go $A >/dev/null 2>&1; long_dung $A 200 0 40; long_vao $A >/dev/null; code_a '' >/dev/null
echo "  không CPUQuota: cpu.max $(cat $CG/cpu.max 2>&1 | head -1) | io.max [$(cat $CG/io.max 2>&1)] | memory.max $(cat $CG/memory.max)"
mkdir -p /root/cgroup-mau2/khong-cpu; for f in cpu.max cpu.stat io.max memory.max pids.max; do cat $CG/$f > /root/cgroup-mau2/khong-cpu/$f 2>&1; done
sed -i 's|^\[Slice\]|[Slice]\nIOReadBandwidthMax=/var/www 4M\nIOWriteBandwidthMax=/var/www 5M|' /etc/systemd/system/tpan-$S.slice; systemctl daemon-reload
echo "  có trần đĩa: io.max [$(cat $CG/io.max 2>&1)] | thiết bị của /var/www: $(stat -c '%d' /var/www) $(findmnt -no MAJ:MIN -T /var/www)"
mkdir -p /root/cgroup-mau2/co-io; for f in cpu.max io.max memory.max pids.max; do cat $CG/$f > /root/cgroup-mau2/co-io/$f 2>&1; done
sed -i '/^MemoryMax=/d; /^MemorySwapMax=/d' /etc/systemd/system/tpan-$S.slice; systemctl daemon-reload
echo "  bỏ MemoryMax: memory.max $(cat $CG/memory.max) | memory.swap.max $(cat $CG/memory.swap.max)"
cat $CG/memory.max > /root/cgroup-mau2/co-io/memory.max.khong-tran; cat $CG/memory.swap.max > /root/cgroup-mau2/co-io/memory.swap.max.khong-tran

echo; echo "## Dọn"
long_go $A
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '')"
