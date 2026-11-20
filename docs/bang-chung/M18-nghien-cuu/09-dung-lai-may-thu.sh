#!/usr/bin/env bash
# M18, đợt 0, câu "dựng lại máy: thứ tự nào đúng; chỗ nào phụ thuộc tên máy và
# IP cũ" và quyết định làm hay cắt phần dựng lại cả máy. Chạy trên Mac:
#
#   make m18-nc S=09-dung-lai-may-thu
#
# Làm BẰNG TAY, với bản tpan hiện có (chưa có dòng mã nào của M18), đúng chuỗi
# việc mà lệnh dựng lại máy sẽ tự làm, để biết các Plan sẵn có (cài stack, tạo
# site, khôi phục backup) có ghép lại được trên một máy trắng hay không:
#
#   máy cũ (máy ảo chính): đã có 2 site a.m18.example, b.m18.example, mỗi site
#     một bài viết và một file dau.txt, mỗi site một bản backup đầy đủ.
#   máy mới (máy ảo phụ -ci): tạo từ user-data, cài tpan, rồi:
#     1. thay tpan.db bằng bản sao của máy cũ
#     2. tpan stack install (web server đọc từ tpan.db cũ)
#     3. với từng site: tpan site add (chạy lại trên dòng đã có), chép file
#        backup về đúng đường dẫn cũ, tpan backup restore
#
# Tiện thể thử câu còn mở của bản thử 08: lệnh CUỐI của runcmd lỗi thì
# cloud-init có báo lỗi không.
set -uo pipefail
cd "$(dirname "$0")/../../.."
main="$(tr -d '[:space:]' <.tpan-vm)"
ci="$main-ci"
tmp="$(mktemp -d)"
old() { scripts/vm.sh "$@" </dev/null; }
new() { scripts/vm.sh on "$ci" "$@" </dev/null; }
step() { echo; echo "=== $* ($(date +%H:%M:%S))"; }

step "0. máy cũ: chép tpan.db (backup create --all) và gom file cần mang đi"
old root bash -c 'tpan backup create --all --yes >/root/bk-all.out 2>&1; echo "backup --all rc=$?"; rm -rf /tmp/mang-di; mkdir -p /tmp/mang-di; snap=$(ls /var/lib/tpan/backups/_tpan/tpan-*.db | tail -1); cp "$snap" /tmp/mang-di/tpan.db; for d in a.m18.example b.m18.example; do f=$(ls /var/lib/tpan/backups/$d/*.tar.gz | tail -1); cp "$f" /tmp/mang-di/; echo "$d $f $(sha256sum "$f" | cut -d" " -f1)"; done > /tmp/mang-di/danh-sach.txt; chmod -R a+rX /tmp/mang-di; cat /tmp/mang-di/danh-sach.txt; ls -l /tmp/mang-di; tpan site list; hostname; hostname -I'
for f in tpan.db danh-sach.txt; do scripts/vm.sh pull "/tmp/mang-di/$f" "$tmp/$f"; done
while read -r _ path _; do scripts/vm.sh pull "/tmp/mang-di/$(basename "$path")" "$tmp/$(basename "$path")"; done <"$tmp/danh-sach.txt"
ls -l "$tmp"

step "1. máy mới: tạo từ user-data có lệnh cuối lỗi (thử cách cloud-init báo lỗi)"
cat >"$tmp/user-data.yaml" <<'EOF'
#cloud-config
runcmd:
  - [ sh, -c, 'echo "lenh dau chay"; exit 0' ]
  - [ sh, -c, 'echo "lenh CUOI loi co chu y (ma 9)"; exit 9' ]
EOF
scripts/vm.sh delete "$ci" >/dev/null 2>&1
t0=$(date +%s)
scripts/vm.sh cloud-init "$tmp/user-data.yaml" "$ci" >/dev/null 2>&1
echo "multipass launch trả về mã $? sau $(($(date +%s) - t0)) giây"
new root bash -c 'cloud-init status --long; echo "ma thoat cua cloud-init status: $?"; cat /run/cloud-init/result.json; grep -n "Failed\|WARNING" /var/log/cloud-init.log | tail -5; hostname; hostname -I'

step "2. máy mới: cài tpan, tpan setup"
scripts/vm.sh on "$ci" deploy 2>&1 | tail -1
new root bash -c 'tpan setup --yes --json >/root/setup.json 2>/root/setup.err; echo "setup rc=$?: $(jq -r .report.job.status /root/setup.json)"; rm -f /root/setup.json'

step "3. máy mới: thay tpan.db bằng bản sao của máy cũ"
scripts/vm.sh on "$ci" push "$tmp/tpan.db" /tmp/tpan.db
new root bash -c 'systemctl stop tpan; mv /var/lib/tpan/tpan.db /var/lib/tpan/tpan.db.moi; rm -f /var/lib/tpan/tpan.db-wal /var/lib/tpan/tpan.db-shm; install -m 0600 -o root -g root /tmp/tpan.db /var/lib/tpan/tpan.db; rm -f /tmp/tpan.db; systemctl start tpan; sleep 1; systemctl is-active tpan; tpan site list; tpan stack status 2>&1 | head -8'

step "4. máy mới: tpan stack install (web lấy từ settings của tpan.db cũ)"
new root bash -c 't0=$(date +%s); tpan stack install --web nginx --yes >/root/stack.out 2>&1; echo "stack rc=$? sau $(( $(date +%s) - t0 )) giay"; tail -4 /root/stack.out'

step "5. máy mới: tạo lại từng site trên dòng đã có trong tpan.db (tpan site add chạy lại)"
new root bash -c 'for d in a.m18.example b.m18.example; do t0=$(date +%s); tpan site add $d --email admin@example.com --yes >/root/add-$d.out 2>&1; echo "$d: site add rc=$? sau $(( $(date +%s) - t0 )) giay"; tail -3 /root/add-$d.out | sed "s/\(Mật khẩu: *\).*/\1•••• (đã che khi lưu bằng chứng)/" | cut -c1-300; curl -s -o /dev/null -w "$d: HTTP %{http_code}\n" --resolve $d:80:127.0.0.1 http://$d/; done'

step "6. máy mới: chép file backup về đúng đường dẫn ghi trong tpan.db, rồi tpan backup restore"
while read -r dom path sum; do
  scripts/vm.sh on "$ci" push "$tmp/$(basename "$path")" "/tmp/$(basename "$path")"
  new root bash -c "install -d -m 0700 /var/lib/tpan/backups \"$(dirname "$path")\"; install -m 0600 -o root -g root \"/tmp/$(basename "$path")\" \"$path\"; rm -f \"/tmp/$(basename "$path")\"; echo \"$sum  $path\" | sha256sum -c -"
done <"$tmp/danh-sach.txt"
new root bash -c 'tpan backup list; for d in a.m18.example b.m18.example; do t0=$(date +%s); tpan backup restore $d --yes >/root/restore-$d.out 2>&1; echo "$d: backup restore rc=$? sau $(( $(date +%s) - t0 )) giay"; tail -4 /root/restore-$d.out | cut -c1-300; done'

step "7. máy mới: site có trả 200 với đúng nội dung của máy cũ không"
new root bash -c 'for d in a.m18.example b.m18.example; do curl -s -o /dev/null -w "$d: HTTP %{http_code}\n" --resolve $d:80:127.0.0.1 http://$d/; echo "dau.txt: $(curl -s --resolve $d:80:127.0.0.1 http://$d/dau.txt)"; curl -s --resolve $d:80:127.0.0.1 http://$d/ | grep -o "Bai cua [a-z0-9.]* truoc khi mat may" | head -1; done; echo; tpan health 2>&1 | tail -25'

step "8. dọn: xoá máy ảo phụ"
scripts/vm.sh delete "$ci"
rm -rf "$tmp"
