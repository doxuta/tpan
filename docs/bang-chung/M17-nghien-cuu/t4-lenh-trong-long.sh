# t4: lệnh chạy thay cho site qua systemd-run phải giữ đúng nghĩa "argv, không shell".
#  a. systemd-run có sửa tham số không ($BIẾN, %specifier, rỗng, xuống dòng...).
#  b. Giá trị permalink mà t2 ghi qua systemd-run (có dấu %).
#  c. Lệnh trong lồng vượt trần RAM: mã thoát và bộ đếm.
#  d. Tiến trình gọi (tpan) bị giết giữa chừng: lệnh trong lồng có chạy tiếp không;
#     RuntimeMaxSec cắt được không.
#  e. Kiểm cấu hình FPM của lồng trước khi chạy (php-fpm -t) bằng user của site.
#  f. Dịch vụ hỏng liên tục: socket ra sao, gỡ thế nào.
S=$SA; SVC=tpan-cage-$S-php.service; SK=tpan-cage-$S-php.socket; CG=/sys/fs/cgroup/tpan.slice/tpan-$S.slice
long_go $A >/dev/null 2>&1; long_dung $A 200 0 0; long_vao $A >/dev/null; code_a '' >/dev/null
rin(){ systemd-run --quiet --wait --pipe --collect --expand-environment=no --unit="tpan-cage-$S-run-$RANDOM$RANDOM" "$@"; }

echo "## a. Tham số qua systemd-run"
args=('$HOME' '${PATH}' '%n %h %% 50%' '' 'a b' '"nhay kep"' "nhay'don" 'gach\cheo' 'cham;phay' $'xuong\ndong' '-dau-gach' 'tiếng Việt' '$(id)' '`id`')
printf '%s|' "${args[@]}" > /tmp/goc.txt
run_in $A printf '%s|' "${args[@]}" > /tmp/mac-dinh.txt 2>/dev/null
rin -- printf '%s|' "${args[@]}" > /tmp/khong-thay.txt 2>/dev/null
echo "gốc:                         $(tr '\n' '~' < /tmp/goc.txt)"
echo "systemd-run mặc định:        $(tr '\n' '~' < /tmp/mac-dinh.txt)"
echo "--expand-environment=no:     $(tr '\n' '~' < /tmp/khong-thay.txt)"
echo "mặc định giữ nguyên: $(cmp -s /tmp/goc.txt /tmp/mac-dinh.txt && echo CO || echo KHONG) | --expand-environment=no giữ nguyên: $(cmp -s /tmp/goc.txt /tmp/khong-thay.txt && echo CO || echo KHONG)"
echo "biến môi trường lệnh nhận được: $(rin -- env | sort | tr '\n' ' ' | cut -c1-300)"
echo "thêm biến bằng -E: $(rin -E LC_ALL=C -E X='a b $HOME' -- sh -c 'echo "LC_ALL=$LC_ALL X=$X"')"
echo "thư mục làm việc mặc định: $(rin -- pwd) | --working-directory: $(rin --working-directory=/var/www/$A/public -- pwd)"

echo; echo "## b. permalink đã ghi ở t2 (wp rewrite structure '/%postname%/' qua systemd-run)"
echo "permalink_structure = [$(rin -- wp option get permalink_structure --path=/var/www/$A/public 2>&1)]"

echo; echo "## c. Lệnh trong lồng vượt trần RAM 200M"
rin -- php -d memory_limit=-1 -r '$a=[]; for($i=0;$i<40;$i++){ $a[]=str_repeat("x",10*1024*1024);} echo "xong\n";' >/tmp/o.txt 2>/tmp/e.txt; rc=$?
echo "mã thoát $rc | stdout=[$(cat /tmp/o.txt)] stderr=[$(head -c 200 /tmp/e.txt)]"
echo "memory.events của slice: $(tr '\n' ' ' < $CG/memory.events)| dịch vụ PHP: $(systemctl is-active $SVC), NRestarts=$(systemctl show -p NRestarts --value $SVC) | A: HTTP $(code_a '')"

echo; echo "## d. Tiến trình gọi bị giết giữa chừng"
systemd-run --quiet --wait --pipe --collect --expand-environment=no --unit=tpan-cage-$S-run-giet -- sleep 300 &
P=$!; sleep 1
echo "trước khi giết: unit $(systemctl is-active tpan-cage-$S-run-giet.service)"
kill -9 $P; sleep 1
echo "sau kill -9 systemd-run: unit $(systemctl is-active tpan-cage-$S-run-giet.service) (sleep còn chạy: $(pgrep -u $UA -c sleep))"
systemctl stop tpan-cage-$S-run-giet.service 2>/dev/null
systemd-run --quiet --wait --pipe --collect --expand-environment=no --unit=tpan-cage-$S-run-term -- sleep 300 &
P=$!; sleep 1; kill -TERM $P; sleep 1
echo "sau kill -TERM systemd-run: unit $(systemctl is-active tpan-cage-$S-run-term.service) (sleep còn chạy: $(pgrep -u $UA -c sleep))"
systemctl stop tpan-cage-$S-run-term.service 2>/dev/null
t0=$(date +%s.%N); rin -p RuntimeMaxSec=2 -- sleep 300; rc=$?; t1=$(date +%s.%N)
echo "RuntimeMaxSec=2 với sleep 300: mã $rc sau $(echo "$t1 - $t0" | bc) giây (sleep còn chạy: $(pgrep -u $UA -c sleep))"

echo; echo "## e. php-fpm -t cho cấu hình của lồng"
echo "bằng root:            $(php-fpm$V -t --fpm-config /etc/tpan/cage/$S/php-fpm.conf 2>&1 | tail -1) (mã ${PIPESTATUS[0]})"
echo "bằng user của site (trong lồng): $(rin -- php-fpm$V -t --fpm-config /etc/tpan/cage/$S/php-fpm.conf 2>&1 | tail -1)"
rin -- php-fpm$V -t --fpm-config /etc/tpan/cage/$S/php-fpm.conf >/dev/null 2>&1; echo "   mã thoát: $?"
cp -p /etc/tpan/cage/$S/php-fpm.conf /root/fpm-goc.conf; echo "pm.max_children = khong-phai-so" >> /etc/tpan/cage/$S/php-fpm.conf
rin -- php-fpm$V -t --fpm-config /etc/tpan/cage/$S/php-fpm.conf >/tmp/o.txt 2>&1; echo "cấu hình sai: mã $? | $(tail -2 /tmp/o.txt | tr '\n' ' ' | cut -c1-200)"

echo; echo "## f. Dịch vụ hỏng liên tục (cấu hình sai còn nguyên)"
systemctl restart $SVC 2>/dev/null; for i in 1 2 3 4 5 6 7 8; do code_a '' >/dev/null; sleep 0.3; done
echo "dịch vụ: $(systemctl is-active $SVC) ($(systemctl show -p Result --value $SVC)) | socket: $(systemctl is-active $SK) ($(systemctl show -p Result --value $SK)) | file socket: $(test -S $(sock_of $A) && echo CO || echo MAT) | A: HTTP $(code_a '')"
cp -p /root/fpm-goc.conf /etc/tpan/cage/$S/php-fpm.conf; rm -f /root/fpm-goc.conf
echo "sửa cấu hình, chỉ start lại socket: $(systemctl start $SK 2>&1 | tail -1) -> socket $(systemctl is-active $SK) | A: HTTP $(code_a '')"
systemctl reset-failed $SVC $SK 2>/dev/null; systemctl start $SK
echo "reset-failed cả hai rồi start socket: socket $(systemctl is-active $SK) | A: HTTP $(code_a '') | dịch vụ $(systemctl is-active $SVC)"

echo; echo "## Dọn"
long_go $A
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '')"
