#!/usr/bin/env bash
# e2e M28, khẳng định 7 (đặt quyền thư mục, S8). Chạy bằng root trong máy ảo,
# sau dung-may.sh. Dùng cho cả Apache và nginx.
#
# Bản cũ chạy `site add` lại trên site đã có WordPress nên bước chmod không bao
# giờ chạy tới (WPFiles không giải nén lại) và khẳng định xanh vô nghĩa. Bản
# này tạo site MỚI mỗi vòng, đúng lúc WPFiles giải nén và đặt quyền thư mục:
#
# - Một thư mục nạn nhân 0755 của root (/srv/m28-victim, kèm thư mục con).
# - Một vòng lặp root chạy nền chờ public của f.example.com có mục đầu tiên
#   (tar của WordPress đã bắt đầu giải nén, tức là đã qua lần kiểm "public
#   rỗng"; kiểm mỗi 10 ms, tối đa 120 giây), rồi chạy kẻ tráo dưới user Linux
#   của site đó (giả lập tiến trình có sẵn của một UID được dùng lại). Kẻ tráo
#   tạo thư mục RIÊNG public/zz-m28/con (tar không đụng tới, nên không làm hỏng
#   việc giải nén) và tráo nó với symlink tới nạn nhân trong 15 giây rồi tự
#   dừng (tra-doi.py, chế độ dir-rieng), để lúc job có lỗi mà hoàn tác thì
#   userdel không vướng tiến trình nào. Lần chạy đầu tráo chính
#   wp-includes/js: tar hỏng giữa chừng, site add không tới bước đặt quyền.
# - admin chạy `tpan site add f.example.com --email admin@example.com --yes
#   --json`. Khẳng định mỗi vòng: job site.add có sự kiện step.done của bước
#   wp.files:f.example.com (đọc job_events của tpan.db, chỉ đọc); nạn nhân giữ
#   0755; public/wp-includes (thư mục của site, không phải chỗ bị tráo) là 2750.
# - Xong vòng thì xoá hẳn site (site rm rồi trash purge). 5 vòng mặc định.
#
# Trên bản CHƯA VÁ, "nạn nhân giữ 0755" có thể ĐỎ: đó là bằng chứng bộ thử chạm
# tới đúng chỗ. Sau bản vá, chmodDirs đi qua cửa nên phải ĐẠT. Phép thử chính
# cho lỗ này vẫn là test cần root TestCuaDuaChmod (1000 vòng trên chính cửa).
. "$(dirname "$0")/chung.sh"
ROUNDS="${1:-5}"
FD=f.example.com
FDIR=/var/www/$FD
SUB=$FDIR/public/zz-m28/con
V=/srv/m28-victim
can_may

dung_tra() {
  pkill -TERM -f '^python3 - m28-tra-doi' 2>/dev/null
  local _
  for _ in $(seq 1 50); do pgrep -f '^python3 - m28-tra-doi' >/dev/null || break; sleep 0.1; done
  pkill -KILL -f '^python3 - m28-tra-doi' 2>/dev/null
}
# xoa_site: xoá hẳn site f (vào thùng rác rồi purge). Best effort.
xoa_site() {
  tpan site rm "$FD" --yes >"$W/f.rm" 2>&1 || true
  tpan trash purge "$FD" --yes >"$W/f.purge" 2>&1 || true
  tail -2 "$W/f.rm" "$W/f.purge" 2>/dev/null | sed 's/^/  | /'
}
don() {
  dung_tra
  rm -rf "$V"
  [ -d "$FDIR" ] && xoa_site
  rm -f "$W"/f.add.* "$W"/f.rm "$W"/f.purge "$W"/dem-chmod.*
}
trap don EXIT

# step_done <job id>: job có sự kiện step.done của bước wp.files:f.example.com.
step_done() {
  python3 -I -c '
import sqlite3, sys
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
n = c.execute("select count(*) from job_events where job_id=? and kind=? and step_id=?",
              (sys.argv[1], "step.done", sys.argv[2])).fetchone()[0]
print(f"  job {sys.argv[1]}: {n} sự kiện step.done của {sys.argv[2]}")
sys.exit(0 if n >= 1 else 1)' "$1" "wp.files:$FD"
}

step "7. $ROUNDS vòng: admin site add $FD (mới), user site tráo $SUB <-> symlink tới $V (0755 của root)"
[ -d "$FDIR" ] && xoa_site
for r in $(seq 1 "$ROUNDS"); do
  step "7. vòng $r/$ROUNDS"
  rm -rf "$V"
  mkdir -p "$V/con"
  chmod 0755 "$V" "$V/con"
  chown root:root "$V" "$V/con"
  echo "  nạn nhân trước: $(stat -c '%n %U:%G %a' "$V" "$V/con" | tr '\n' ' ')"
  (
    for _ in $(seq 1 12000); do [ -n "$(ls -A "$FDIR/public" 2>/dev/null)" ] && break; sleep 0.01; done
    [ -n "$(ls -A "$FDIR/public" 2>/dev/null)" ] || exit 3
    fsu="$(stat -c %U "$FDIR")"
    runuser -u "$fsu" -- python3 - m28-tra-doi dir-rieng "$SUB" "$V" 15 <"$D/tra-doi.py" >"$W/dem-chmod.$r" 2>&1
  ) &
  watcher=$!
  tpan site add "$FD" --email admin@example.com --yes --json >"$W/f.add.$r" 2>"$W/f.add.err.$r"
  rc=$?
  kill "$watcher" 2>/dev/null
  dung_tra
  wait "$watcher" 2>/dev/null
  tr=$(sed -n 's/^so-lan-trao=//p' "$W/dem-chmod.$r" 2>/dev/null)
  echo "  site add thoát $rc; số lần tráo thành công: ${tr:-không có (kẻ tráo không chạy)}"
  jid="$(python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1]))["report"]["job"]["id"])' "$W/f.add.$r" 2>/dev/null)"
  ok "vòng $r: job site.add có sự kiện step.done của wp.files:$FD" step_done "${jid:-none}"
  echo "  nạn nhân sau: $(stat -c '%n %U:%G %a' "$V" "$V/con" | tr '\n' ' ')"
  ok "vòng $r: thư mục nạn nhân giữ quyền 0755" bash -c '[ "$(stat -c %a "$1")" = 755 ] && [ "$(stat -c %a "$1/con")" = 755 ]' _ "$V"
  ok "vòng $r: public/wp-includes là 2750 ($(stat -c %a "$FDIR/public/wp-includes" 2>/dev/null))" \
    bash -c '[ "$(stat -c %a "$1")" = 2750 ]' _ "$FDIR/public/wp-includes"
  # Lỗi gốc nằm trong JSON (error.message, report.job.error); stderr chỉ có
  # phần hoàn tác (lần chạy 11:54 ngày 21/11 chỉ in được đuôi stderr nên không
  # thấy lỗi gốc).
  if [ "$rc" != 0 ]; then
    note "vòng $r: site add thoát $rc: đầu stderr: $(head -c 400 "$W/f.add.err.$r" | tr '\n' ' ')"
    python3 -I -c '
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except Exception as e:
    sys.exit(print("  (stdout không phải JSON:", e, ")"))
err = d.get("error") or {}
job = (d.get("report") or {}).get("job") or {}
print("  lỗi:", (err.get("message") if isinstance(err, dict) else err) or "-")
print("  job.error:", job.get("error") or "-")' "$W/f.add.$r"
    # Hoàn tác của job hỏng không xoá được user Linux khi kẻ tráo còn chạy
    # dưới user đó, và vòng sau hỏng dây chuyền ("trên máy đã có user Linux
    # ..."). Kẻ tráo đã dừng ở trên: dọn user sót để các vòng độc lập.
    for u in $(getent passwd | cut -d: -f1 | grep '^tp_f_example_com_'); do
      pkill -KILL -u "$u" 2>/dev/null
      sleep 0.5
      userdel "$u" 2>/dev/null && echo "  đã dọn user sót $u"
    done
  fi
  rm -f "$W/f.add.$r" "$W/f.add.err.$r"
  xoa_site
done
