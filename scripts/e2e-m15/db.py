#!/usr/bin/env python3
"""ĐỒ NGHỀ TEST của scripts/e2e-m15.sh, chỉ chạy TRONG máy ảo test bằng root.

Đọc (và ở đúng một chỗ: ghi) thẳng /var/lib/tpan/tpan.db, không qua tpan, để
e2e kiểm bằng một đường độc lập với thứ tpan tự báo.

  db.py seturl <url>   ghi setting update.self_url (nguồn tải của nút cập nhật).
                       Chưa có lệnh CLI hay API nào đặt setting này, nên e2e
                       ghi thẳng bảng settings. Đây là chỗ GHI duy nhất.
  db.py delurl         xoá setting update.self_url (về nguồn tải mặc định)
  db.py get <khoá>     in "<khoá>=<giá trị>" hoặc "<khoá>=(không có)"
  db.py audit          các dòng audit có action self.*, cũ trước mới sau
  db.py jobs           các job có kind self.*, cũ trước mới sau
"""
import datetime
import sqlite3
import sys

DB = "/var/lib/tpan/tpan.db"
KEY_URL = "update.self_url"


def ro():
    return sqlite3.connect("file:%s?mode=ro" % DB, uri=True, timeout=10)


def now():
    # Cùng khuôn với timeLayout của internal/store/store.go: UTC, 9 số lẻ.
    t = datetime.datetime.now(datetime.timezone.utc)
    return t.strftime("%Y-%m-%dT%H:%M:%S.") + "%06d000Z" % t.microsecond


def get(key):
    row = ro().execute("SELECT value FROM settings WHERE key = ?", (key,)).fetchone()
    print("%s=%s" % (key, row[0] if row else "(không có)"))


def main(argv):
    cmd = argv[1] if len(argv) > 1 else ""
    if cmd == "seturl" and len(argv) == 3:
        c = sqlite3.connect(DB, timeout=10)
        with c:
            c.execute(
                "INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?) "
                "ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at",
                (KEY_URL, argv[2], now()),
            )
        c.close()
        get(KEY_URL)
    elif cmd == "delurl" and len(argv) == 2:
        c = sqlite3.connect(DB, timeout=10)
        with c:
            c.execute("DELETE FROM settings WHERE key = ?", (KEY_URL,))
        c.close()
        get(KEY_URL)
    elif cmd == "get" and len(argv) == 3:
        get(argv[2])
    elif cmd == "audit" and len(argv) == 2:
        print("audit|id|ip|actor|action|target|result|job_id")
        q = "SELECT id, ip, actor, action, target, result, job_id FROM audit WHERE action LIKE 'self.%' ORDER BY id"
        for r in ro().execute(q):
            print("audit|" + "|".join(str(x) for x in r))
    elif cmd == "jobs" and len(argv) == 2:
        print("job|id|kind|status|actor|target|error")
        q = "SELECT id, kind, status, actor, target, substr(error, 1, 200) FROM jobs WHERE kind LIKE 'self.%' ORDER BY started_at"
        for r in ro().execute(q):
            print("job|" + "|".join(str(x) for x in r))
    else:
        sys.stderr.write(__doc__)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
