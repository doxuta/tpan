# t15: RestartSec=5s có thật sự giữ dịch vụ và socket khỏi "failed" khi đang có
# request không (request tới socket có thể đánh thức dịch vụ ngay, bỏ qua
# RestartSec). Hai cấu hình, mỗi cấu hình: hạ trần RAM xuống 8M, gọi A mỗi 0,5
# giây trong 40 giây, xem trạng thái; rồi trả 200M (daemon-reload) và chờ xem
# A có tự sống lại không (không reset-failed, không start tay).
#   (1) RestartSec=5s, StartLimitIntervalSec=30, StartLimitBurst=10
#   (2) RestartSec=5s, StartLimitIntervalSec=0 (tắt giới hạn khởi động)
S=$SA; SVC=tpan-cage-$S-php.service; SK=tpan-cage-$S-php.socket
luot(){ # nhãn, dòng thêm vào [Unit] (giới hạn khởi động)
  long_go $A >/dev/null 2>&1; long_dung $A 200 0 40
  sed -i 's/^StartLimitIntervalSec=.*/'"$2"'/' /etc/systemd/system/$SVC
  sed -i 's/^Restart=on-failure/Restart=on-failure\nRestartSec=5s/' /etc/systemd/system/$SVC
  systemctl daemon-reload; long_vao $A >/dev/null; code_a '' >/dev/null
  echo "## $1: $(grep -E '^(StartLimit|RestartSec)' /etc/systemd/system/$SVC | tr '\n' ' ')"
  sed -i 's/^MemoryMax=.*/MemoryMax=8M/' /etc/systemd/system/tpan-$S.slice; systemctl daemon-reload
  t_end=$(( $(date +%s) + 40 )); n=0; ok=0
  while [ $(date +%s) -lt $t_end ]; do c=$(ca '' --max-time 3 -o /dev/null -w '%{http_code}'); n=$((n+1)); [ "$c" = 200 ] && ok=$((ok+1)); sleep 0.5; done
  echo "   40 giây ở 8M, $n request ($ok trả 200): dịch vụ $(systemctl is-active $SVC) ($(systemctl show -p Result --value $SVC), NRestarts=$(systemctl show -p NRestarts --value $SVC)) | socket $(systemctl is-active $SK) ($(systemctl show -p Result --value $SK)) | file socket $(test -S $(sock_of $A) && echo CO || echo MAT)"
  sed -i 's/^MemoryMax=.*/MemoryMax=200M/' /etc/systemd/system/tpan-$S.slice; systemctl daemon-reload
  t0=$(date +%s.%N); got=0
  for i in $(seq 1 30); do [ "$(code_a '' --max-time 3)" = 200 ] && { got=1; break; }; sleep 0.5; done
  t1=$(date +%s.%N)
  echo "   trả 200M, không lệnh nào khác: A $( [ $got = 1 ] && echo "trả 200 sau $(echo "$t1 - $t0" | bc) giây" || echo "VẪN HỎNG sau 15 giây") | dịch vụ $(systemctl is-active $SVC) | socket $(systemctl is-active $SK)"
  echo "   tiến trình php-fpm của lồng lúc 8M ăn CPU bao nhiêu (cpu.stat usage_usec của slice): $(awk '/^usage_usec/{print $2}' /sys/fs/cgroup/tpan.slice/tpan-$S.slice/cpu.stat 2>/dev/null)"
}
luot "(1) RestartSec=5s, giới hạn 10 lần/30 giây" "StartLimitIntervalSec=30"
luot "(2) RestartSec=5s, StartLimitIntervalSec=0" "StartLimitIntervalSec=0"
echo; echo "## Dọn"
long_go $A
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '')"
