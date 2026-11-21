#!/usr/bin/env bash
# Thử cơ chế tách tiến trình cho nút cập nhật TPAN trong panel (M15, đợt 0).
# Chạy TRONG máy ảo test bằng quyền root, không chạy ở đâu khác:
#
#   scripts/vm.sh root bash /home/ubuntu/thu/thu-co-che-vm.sh
#
# Chuẩn bị (làm trên Mac trước khi gọi, xem đầu docs/bang-chung/M15-thu-co-che.log):
#   - máy ảo sạch, /usr/local/bin/tpan là bản A, đã `tpan setup` (tpan.service chạy);
#   - /home/ubuntu/thu/a, /home/ubuntu/thu/b: mỗi thư mục có tpan-linux-<arch> và
#     SHA256SUMS của một bản (A là bản đang cài, B là "bản mới");
#   - /home/ubuntu/thu/sai: file chạy của B nhưng SHA256SUMS ghi số khác.
#
# Câu hỏi cần trả lời bằng output thật:
#   1. Vì sao không chạy self-update thẳng từ panel: tiến trình con của
#      tpan.service có bị cắt khi tpan.service restart không?
#   2. `systemd-run --unit tpan-self-update --collect ...` gọi từ BÊN TRONG
#      tpan.service có sống qua lần restart đó không, job có ghi kết quả không?
#   3. Gọi lần hai khi lượt trước đang chạy thì systemd trả lời gì?
#   4. Sai sha256 trong đơn vị tạm: file chạy cũ còn nguyên, panel không restart?
#   5. Quay về (--rollback) chạy cùng cơ chế được không?
# Không dùng set -e: nhiều lệnh ở đây được chờ là phải lỗi.
set -uo pipefail

DIR=/home/ubuntu/thu
PORT=8015
BASE=http://127.0.0.1:$PORT
BIN=/usr/local/bin/tpan
UNIT=tpan-self-update
CG=/sys/fs/cgroup/system.slice/tpan.service/cgroup.procs
ARCH="$(dpkg --print-architecture)"

say() { printf '\n\n######## %s\n' "$*"; }
run() {
  printf '\n$ %s\n' "$*"
  "$@"
  printf '(mã thoát %s)\n' "$?"
}
sha() { sha256sum "$1" 2>/dev/null | cut -c1-16; }
state() {
  printf 'tpan version: %s\n' "$("$BIN" version 2>&1)"
  printf 'sha256 %s: %s   .old: %s   .new: %s\n' "$BIN" "$(sha "$BIN")" "$(sha "$BIN.old")" "$(sha "$BIN.new")"
  systemctl show tpan -p MainPID -p ActiveState -p NRestarts -p ActiveEnterTimestamp | tr '\n' ' '
  printf '\n'
}
# Ba dòng cuối của bảng jobs và audit, đọc thẳng DB (không qua tpan).
db() {
  python3 - <<'PY'
import sqlite3
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
for t, cols in (("jobs", "id, kind, target, status, started_at, finished_at, substr(error, 1, 60)"), ("audit", "time, ip, action, target, result, job_id")):
    print("-- 3 dòng cuối của " + t)
    for r in c.execute("SELECT %s FROM %s ORDER BY rowid DESC LIMIT 3" % (cols, t)):
        print("   " + " | ".join(str(x) for x in r))
PY
}
# in_tpan <lệnh...>: chạy lệnh như một tiến trình con của tpan.service (đưa
# shell vào cgroup của dịch vụ rồi exec). Đây là vị trí của mã panel khi nó tự
# gọi một lệnh.
in_tpan() {
  bash -c 'echo $$ >"$0" && exec "$@"' "$CG" "$@"
}
wait_gone() { # chờ đơn vị tạm chạy xong (tối đa 90 giây)
  local i
  for i in $(seq 1 90); do
    systemctl is-active --quiet "$UNIT" || return 0
    sleep 1
  done
  return 1
}
# Đưa máy về đầu: file chạy là bản A, không còn .old/.new, tpan.service chạy.
ve_dau() {
  install -m 0755 "$DIR/a/tpan-linux-$ARCH" "$BIN"
  rm -f "$BIN.old" "$BIN.new"
  systemctl reset-failed "$UNIT" 2>/dev/null
  systemctl restart tpan
  sleep 2
}

say "0. Chuẩn bị: máy chủ tải trên loopback (cũng là một đơn vị tạm)"
systemctl stop tpan-thu-http 2>/dev/null
run systemd-run --unit tpan-thu-http --collect python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$DIR"
sleep 1
run curl -fsS "$BASE/b/SHA256SUMS"
ve_dau
state

say "1. Chạy self-update như tiến trình con của tpan.service (cách KHÔNG dùng)"
printf 'Lệnh: (trong cgroup của tpan.service) %s self-update --yes --url %s/b\n' "$BIN" "$BASE"
in_tpan "$BIN" self-update --yes --url "$BASE/b" >"$DIR/thu1.out" 2>&1 &
wait $!
printf '(mã thoát của tiến trình self-update: %s)\n' "$?"
printf -- '--- output của nó:\n'
cat "$DIR/thu1.out"
printf -- '--- sau đó:\n'
sleep 3
state
db
run journalctl -u tpan --no-pager -o short-precise -n 12

say "2. Cùng việc đó qua systemd-run, gọi từ BÊN TRONG tpan.service (cách dùng)"
ve_dau
state
PID_TRUOC="$(systemctl show tpan -p MainPID --value)"
run in_tpan systemd-run --unit "$UNIT" --collect --description "tpan: cap nhat tu panel" "$BIN" self-update --yes --url "$BASE/b"
run systemctl is-active "$UNIT"
systemctl show "$UNIT" -p ControlGroup -p MainPID 2>/dev/null | tr '\n' ' '
printf '\n'
wait_gone
printf '(đơn vị tạm đã chạy xong)\n'
sleep 2
state
printf 'MainPID của tpan.service trước: %s, sau: %s\n' "$PID_TRUOC" "$(systemctl show tpan -p MainPID --value)"
db
run journalctl -u "$UNIT" --no-pager -o short-precise -n 40
run journalctl -u tpan --no-pager -o short-precise -n 8
run systemctl is-active "$UNIT"
run curl -ksS -o /dev/null -w 'panel trả %{http_code}\n' https://127.0.0.1:8443/api/v1/health

say "3. Gọi lần hai khi lượt trước đang chạy"
run systemd-run --unit "$UNIT" --collect sleep 8
run systemctl is-active "$UNIT"
run systemd-run --unit "$UNIT" --collect true
sleep 9
run systemctl is-active "$UNIT"
printf -- '--- lượt lỗi có --collect: đơn vị tự dọn, lượt sau chạy được\n'
run systemd-run --unit "$UNIT" --collect false
sleep 1
run systemctl is-active "$UNIT"
run systemd-run --unit "$UNIT" --collect true
sleep 1
printf -- '--- lượt lỗi KHÔNG có --collect: đơn vị nằm lại ở trạng thái failed và chặn lượt sau\n'
run systemd-run --unit "$UNIT" false
sleep 1
run systemctl is-active "$UNIT"
run systemd-run --unit "$UNIT" --collect true
run systemctl reset-failed "$UNIT"

say "4. Sai sha256 trong đơn vị tạm: file cũ phải còn nguyên, panel không restart"
ve_dau
state
PID_TRUOC="$(systemctl show tpan -p MainPID --value)"
SHA_TRUOC="$(sha "$BIN")"
run in_tpan systemd-run --unit "$UNIT" --collect "$BIN" self-update --yes --url "$BASE/sai"
wait_gone
sleep 1
state
printf 'MainPID trước: %s, sau: %s; sha256 file chạy trước: %s, sau: %s\n' "$PID_TRUOC" "$(systemctl show tpan -p MainPID --value)" "$SHA_TRUOC" "$(sha "$BIN")"
db
run journalctl -u "$UNIT" --no-pager -o short-precise -n 25

say "5. Quay về (--rollback) cùng cơ chế: lên B rồi về A"
ve_dau
in_tpan systemd-run --unit "$UNIT" --collect "$BIN" self-update --yes --url "$BASE/b"
wait_gone
sleep 2
state
run in_tpan systemd-run --unit "$UNIT" --collect "$BIN" self-update --rollback --yes
wait_gone
sleep 2
state
db
run journalctl -u "$UNIT" --no-pager -o short-precise -n 14

say "Dọn: tắt máy chủ tải"
run systemctl stop tpan-thu-http
