#!/usr/bin/env python3
"""ĐỒ NGHỀ TEST của scripts/e2e-m20.sh, chỉ chạy TRONG máy ảo test bằng root.

Đọc thẳng /var/lib/tpan/tpan.db (chỉ đọc), không qua tpan, để e2e kiểm bằng
một đường độc lập với thứ tpan tự báo; và ở ĐÚNG MỘT chỗ thì ghi.

  db.py sql <câu SELECT> [tham số...]   in từng dòng, các cột cách nhau bằng tab (chỉ đọc)
  db.py hethan <id token>               GHI: đặt expires_at của token đó về một giờ
                                        trước (T40 "token hết hạn": không có lệnh
                                        hay API nào tạo được token đã hết hạn, và
                                        chờ một ngày thì e2e không chạy nổi).
"""
import datetime
import sqlite3
import sys

DB = "/var/lib/tpan/tpan.db"


def stamp(t):
    # Cùng khuôn với timeLayout của internal/store/store.go: UTC, 9 số lẻ.
    return t.strftime("%Y-%m-%dT%H:%M:%S.") + "%06d000Z" % t.microsecond


def main(argv):
    cmd = argv[1] if len(argv) > 1 else ""
    if cmd == "sql" and len(argv) >= 3:
        c = sqlite3.connect("file:%s?mode=ro" % DB, uri=True, timeout=10)
        for r in c.execute(argv[2], argv[3:]):
            print(*[str(x) for x in r], sep="\t")
    elif cmd == "hethan" and len(argv) == 3:
        past = datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(hours=1)
        c = sqlite3.connect(DB, timeout=10)
        with c:
            n = c.execute("UPDATE api_tokens SET expires_at = ? WHERE id = ?", (stamp(past), argv[2])).rowcount
        c.close()
        print("token %s: %d dòng, expires_at = %s" % (argv[2], n, stamp(past)))
        return 0 if n == 1 else 1
    else:
        sys.stderr.write(__doc__)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
