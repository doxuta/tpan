#!/usr/bin/env bash
# M18, đợt 0, câu "file 2 GB: tải lên và tải về tốn bao nhiêu RAM của panel
# (phải truyền theo dòng); đứt mạng giữa chừng thì dọn phần tải dở thế nào" và
# "kho trả lỗi giữa lúc tải nhiều phần". Chạy TRONG máy ảo bằng root:
#
#   scripts/vm.sh root bash -s < docs/bang-chung/M18-nghien-cuu/07-file-2gb.sh
#
# Cần s3thu (bản thử) và hai máy chủ thử của 04 và 06 đang chạy.
set -u
step() { echo; echo "--- $*"; }
f=/root/thu-2g.bin

step "0. tạo file 2 GiB ngẫu nhiên (không nén được) và tính sha256 bằng sha256sum"
if [ ! -f "$f" ]; then head -c $((2 * 1024 * 1024 * 1024)) /dev/urandom >"$f"; fi
ls -l "$f"
t0=$(date +%s.%N); want="$(sha256sum "$f" | cut -d' ' -f1)"; t1=$(date +%s.%N)
echo "sha256sum: $want ($(echo "$t1 - $t0" | bc) giây)"
free -m | sed -n 2p

for envf in /root/s3thu.env /root/garage.env; do
  # shellcheck disable=SC1090
  . "$envf"
  for mib in 8 16; do
    step "1. $S3_ENDPOINT, phần $mib MiB: tải lên 2 GiB"
    S3_PART_MIB=$mib s3thu put "$f" "thu/2g-$mib.bin"
    step "2. $S3_ENDPOINT: tải về, so sha256"
    s3thu get "thu/2g-$mib.bin" /root/thu-2g.out | tee /root/thu-get.txt
    grep -q "$want" /root/thu-get.txt && echo "ĐẠT: sha256 tải về khớp" || echo "KHÔNG ĐẠT: sha256 tải về khác $want"
    cmp -s "$f" /root/thu-2g.out && echo "ĐẠT: cmp hai file giống hệt" || echo "KHÔNG ĐẠT: cmp khác"
    rm -f /root/thu-2g.out
    s3thu head "thu/2g-$mib.bin"
    s3thu rm "thu/2g-$mib.bin"
  done
done

# shellcheck disable=SC1091
. /root/s3thu.env

step "3. đứt giữa chừng (versitygw): dừng máy chủ sau 1 giây tải lên"
du -sh /var/lib/s3thu
(sleep 1; systemctl stop versitygw@tpan) &
S3_PART_MIB=8 s3thu put "$f" thu/2g-dut.bin; echo "mã thoát của s3thu: $?"
wait
systemctl start versitygw@tpan; sleep 1
echo "sau khi máy chủ chạy lại:"
s3thu list thu/
s3thu mpu ""
du -sh /var/lib/s3thu
find /var/lib/s3thu -maxdepth 3 | head -20
step "3b. dọn phần tải dở bằng AbortMultipartUpload"
s3thu mpu-abort ""
s3thu mpu ""
du -sh /var/lib/s3thu

step "4. kho trả 503 từ phần thứ 5 (proxy gây lỗi ở 127.0.0.1:7071)"
s3thu proxy 127.0.0.1:7071 http://127.0.0.1:7070 5 503 &
ppid=$!
sleep 1
S3_ENDPOINT=http://127.0.0.1:7071 S3_PART_MIB=8 s3thu put "$f" thu/2g-503.bin; echo "mã thoát của s3thu: $?"
kill "$ppid" 2>/dev/null
echo "sau khi client đã tự huỷ:"
s3thu mpu ""
s3thu list thu/
du -sh /var/lib/s3thu

step "5. sửa hỏng một byte của object trên đĩa của kho (versitygw lưu object thành file thường)"
head -c $((3 * 1024 * 1024)) /dev/urandom >/root/thu-3m.bin
want3="$(sha256sum /root/thu-3m.bin | cut -d' ' -f1)"
s3thu put /root/thu-3m.bin thu/hong.bin
s3thu head thu/hong.bin
obj=/var/lib/s3thu/tpan-thu/thu/hong.bin
ls -l "$obj"
getfattr -d -m - "$obj" 2>/dev/null | cut -c1-120 || echo "(không có getfattr)"
printf '\377' | dd of="$obj" bs=1 seek=100000 conv=notrunc 2>/dev/null
echo "đã ghi đè 1 byte ở vị trí 100000"
s3thu head thu/hong.bin
s3thu get thu/hong.bin /root/thu-3m.out | tee /root/thu-get.txt
grep -q "$want3" /root/thu-get.txt && echo "KHÔNG ĐẠT: sha256 vẫn khớp (kho không trả byte hỏng?)" || echo "ĐẠT: sha256 đọc lại KHÁC bản gốc $want3, trong khi ETag của kho không đổi: phải tự tính sha256, không tin ETag"
s3thu rm thu/hong.bin
rm -f /root/thu-3m.bin /root/thu-3m.out /root/thu-get.txt

step "6. dọn"
rm -f "$f"
df -h / | tail -1
