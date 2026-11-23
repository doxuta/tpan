#!/usr/bin/env bash
# M18, đợt 0, câu "cloud-init trên máy ảo thử: user-data dài tối đa bao nhiêu,
# chạy bằng quyền gì, lỗi thì thấy ở đâu, chạy lại có an toàn không". Chạy trên Mac:
#
#   make m18-nc S=08-cloud-init-thu
#
# Sinh một user-data thử (độn cho tới cỡ PAD_KB, mặc định 300 KB, để biết
# Multipass nhận user-data lớn tới đâu), tạo máy ảo phụ <máy ảo chính>-ci từ nó,
# rồi đọc lại những gì cloud-init đã làm. Cuối cùng xoá máy phụ.
set -uo pipefail
cd "$(dirname "$0")/../../.."
main="$(tr -d '[:space:]' <.tpan-vm)"
ci="$main-ci"
ud="$(mktemp -d)/user-data.yaml"
pad_kb="${PAD_KB:-300}"

{
  cat <<'EOF'
#cloud-config
write_files:
  - path: /root/ci-thuong.txt
    permissions: '0600'
    content: |
      file ghi dang van ban thuong
  - path: /etc/tpan-ci/b64.txt
    encoding: b64
    permissions: '0640'
    content: eGluIGNoYW8K
  - path: /usr/local/sbin/tpan-ci-thu
    permissions: '0700'
    content: |
      #!/bin/sh
      # Ghi lai moi truong ma cloud-init chay lenh cua user-data.
      id > /root/ci-id.txt
      env | sort > /root/ci-env.txt
      pwd > /root/ci-pwd.txt
      umask > /root/ci-umask.txt
      date -u +%FT%TZ >> /root/ci-lan-chay.txt
      echo "dong nay phai co trong cloud-init-output.log"
EOF
  printf '  - path: /root/ci-don.bin\n    encoding: b64\n    content: '
  head -c $((pad_kb * 1024 * 3 / 4)) /dev/urandom | base64 | tr -d '\n'
  printf '\n'
  cat <<'EOF'
runcmd:
  - [ /usr/local/sbin/tpan-ci-thu ]
  - [ sh, -c, 'echo "lenh loi co chu y (ma 7)"; exit 7' ]
  - [ sh, -c, 'echo "lenh sau lenh loi van chay" | tee /root/ci-sau-loi.txt' ]
EOF
} >"$ud"
echo "== user-data thử: $(wc -c <"$ud") byte ($(date -u +%FT%TZ))"

scripts/vm.sh delete "$ci" >/dev/null 2>&1
t0=$(date +%s)
scripts/vm.sh cloud-init "$ud" "$ci"
echo "== multipass launch trả về mã $? sau $(($(date +%s) - t0)) giây"

on() { scripts/vm.sh on "$ci" "$@" </dev/null; }

echo; echo "--- cloud-init status --long"
on root cloud-init status --long
echo; echo "--- /run/cloud-init/result.json"
on root cat /run/cloud-init/result.json
echo; echo "--- lệnh của user-data chạy bằng quyền gì"
on root bash -c 'echo "id: $(cat /root/ci-id.txt)"; echo "pwd: $(cat /root/ci-pwd.txt)"; echo "umask: $(cat /root/ci-umask.txt)"; echo "env:"; sed "s/^/  /" /root/ci-env.txt'
echo; echo "--- file do write_files ghi"
on root bash -c 'ls -l /root/ci-thuong.txt /etc/tpan-ci/b64.txt /usr/local/sbin/tpan-ci-thu /root/ci-don.bin; cat /etc/tpan-ci/b64.txt; ls -ld /etc/tpan-ci'
echo; echo "--- lỗi thấy ở đâu: cloud-init-output.log (đuôi) và dòng lỗi trong cloud-init.log"
on root bash -c 'ls -l /var/log/cloud-init-output.log /var/log/cloud-init.log; tail -12 /var/log/cloud-init-output.log; echo ...; grep -n "runcmd\|Failed\|WARNING\|scripts-user" /var/log/cloud-init.log | tail -8; echo; cat /root/ci-sau-loi.txt'
echo; echo "--- ai đọc được user-data trên máy"
on root bash -c 'ls -l /var/lib/cloud/instance/user-data.txt /var/lib/cloud/instance/scripts/runcmd; ls -ld /var/lib/cloud/instance/ /var/lib/cloud/instances'
on exec bash -c 'echo "user thuong ($(id -un)) doc user-data.txt:"; head -c 40 /var/lib/cloud/instance/user-data.txt 2>&1 | head -2; echo; echo "cloud-init query userdata:"; cloud-init query userdata 2>&1 | head -c 200; echo; echo "doc o dia chi metadata 169.254.169.254:"; curl -s -m 3 -o /dev/null -w "%{http_code}\n" http://169.254.169.254/ 2>&1; echo "datasource: $(cloud-id 2>&1)"; lsblk -o NAME,TYPE,FSTYPE,LABEL,SIZE,MOUNTPOINT | head -12'
echo; echo "--- chạy lại: khởi động lại máy thì runcmd có chạy lần nữa không"
on root bash -c 'cat /root/ci-lan-chay.txt; ls /var/lib/cloud/instance/sem/ | tr "\n" " "; echo'
scripts/vm.sh on "$ci" down
scripts/vm.sh on "$ci" start >/dev/null 2>&1
sleep 20
on root bash -c 'echo "sau khi khoi dong lai:"; cat /root/ci-lan-chay.txt; cloud-init status'
echo; echo "--- chạy lại bằng tay: cloud-init single --name scripts_user --frequency always"
on root bash -c 'cloud-init single --name scripts_user --frequency always >/dev/null 2>&1; echo "ma thoat $?"; cat /root/ci-lan-chay.txt'

echo; echo "--- dọn: xoá máy ảo phụ"
scripts/vm.sh delete "$ci"
multipass list | grep -c "$ci" || true
