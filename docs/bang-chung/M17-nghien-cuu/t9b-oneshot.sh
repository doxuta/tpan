# t9b: t9 cho thấy lệnh chết vì SIGTERM được systemd coi là THÀNH CÔNG (mã 0) khi
# unit tạm là loại mặc định. Thử --service-type=oneshot (tài liệu systemd: với
# oneshot chỉ mã 0 mới là thành công) và hạn giờ bằng TimeoutStartSec.
S=$SA
long_go $A >/dev/null 2>&1; long_dung $A 200 0 0
rin(){ local u=$1; shift; systemd-run --quiet --wait --pipe --expand-environment=no --service-type=oneshot -E LC_ALL=C --unit="$u" "$@"; }
hoi(){ local u=$1; echo "   LoadState=$(systemctl show -p LoadState --value $u) Result=$(systemctl show -p Result --value $u) ExecMainCode=$(systemctl show -p ExecMainCode --value $u) ExecMainStatus=$(systemctl show -p ExecMainStatus --value $u)"; systemctl reset-failed $u 2>/dev/null; }
rin tpan-cage-$S-run-a -- true; echo "true: mã $?"; hoi tpan-cage-$S-run-a.service
rin tpan-cage-$S-run-b -- sh -c 'exit 3'; echo "exit 3: mã $?"; hoi tpan-cage-$S-run-b.service
rin tpan-cage-$S-run-c -- sh -c 'kill -TERM $$'; echo "tự chết vì SIGTERM: mã $?"; hoi tpan-cage-$S-run-c.service
rin tpan-cage-$S-run-d -- sh -c 'kill -PIPE $$'; echo "tự chết vì SIGPIPE: mã $?"; hoi tpan-cage-$S-run-d.service
t0=$(date +%s.%N); rin tpan-cage-$S-run-e -p TimeoutStartSec=2 -- sleep 30; rc=$?; t1=$(date +%s.%N); echo "TimeoutStartSec=2 với sleep 30: mã $rc sau $(echo "$t1 - $t0" | bc) giây (sleep còn chạy: $(pgrep -u $UA -c sleep))"; hoi tpan-cage-$S-run-e.service
rin tpan-cage-$S-run-f -- php -d memory_limit=-1 -r '$a=[]; for($i=0;$i<40;$i++){ $a[]=str_repeat("x",10*1024*1024);} echo "xong\n";'; echo "vượt RAM: mã $?"; hoi tpan-cage-$S-run-f.service
echo "stdin + stdout + stderr với oneshot: $(echo vao | rin tpan-cage-$S-run-g -- sh -c 'cat; echo loi >&2' 2>/tmp/e.txt) / $(cat /tmp/e.txt)"
echo "wp trong lồng với oneshot: $(rin tpan-cage-$S-run-h -- wp option get siteurl --path=/var/www/$A/public 2>&1 | tail -1)"
t0=$(date +%s.%N); rin tpan-cage-$S-run-i -- true; t1=$(date +%s.%N); echo "thời gian true: $(echo "$t1 - $t0" | bc) giây"
long_go $A
echo "sau khi dọn: A HTTP $(code_a '')"
