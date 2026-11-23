#!/usr/bin/env python3
"""e2e M18: đọc tpan.db (chỉ đọc) để kiểm job, sự kiện, dòng backups, setting.

Chạy TRONG máy ảo bằng root (scripts/e2e-m18.sh chép vào /root/e2e-m18/):

  python3 /root/e2e-m18/jobs.py [--db FILE] <lệnh> [đối số]

Lệnh:
  mark                    "moc <rowid lớn nhất của jobs>" (đánh dấu trước một thao tác)
  since <moc>             mỗi job có rowid > moc: "job <id> <status> <kind> <target> loi=<lỗi>"
  count <kind>            "so-job <kind> <số job>"
  last <kind> [target]    job mới nhất của loại đó và mọi sự kiện của nó:
                          "ev <seq> <kind> <step_id> <message>"
  lastid <kind>           chỉ id của job mới nhất loại đó
  backups [domain]        mỗi dòng backups, mới nhất trước:
                          "bk <id> <domain> <kind> <reason> local=<0|1> remote=<...> tries=<n> key=<remote_key>"
  row <id>                "dong <id> local=<0|1> remote=<...>" (dùng cho tpan.db trong gói panel)
  settings                "set <khoá>=<giá trị>" của mọi setting s3.*

Không lệnh nào in cột chứa bí mật: tpan.db không có khoá S3 hay khoá sao lưu
(mục 3.2 của docs/thay-doi/M18-thiet-ke.md), và phần H quét chính file này.
Đọc bằng câu SQL cố định với tham số, không ghép chuỗi.
"""
import sqlite3
import sys


def one_line(s, n=240):
    return " ".join((s or "").split())[:n]


def main(argv):
    db = "/var/lib/tpan/tpan.db"
    if len(argv) >= 3 and argv[1] == "--db":
        db = argv[2]
        argv = argv[:1] + argv[3:]
    if len(argv) < 2:
        print(__doc__)
        return 2
    c = sqlite3.connect("file:%s?mode=ro" % db, uri=True)
    cmd, args = argv[1], argv[2:]
    if cmd == "mark":
        print("moc", c.execute("SELECT COALESCE(MAX(rowid), 0) FROM jobs").fetchone()[0])
    elif cmd == "since":
        for r in c.execute(
            "SELECT id, status, kind, target, error FROM jobs WHERE rowid > ? ORDER BY rowid", (int(args[0]),)
        ):
            print("job", r[0], r[1], r[2], r[3], "loi=" + one_line(r[4], 300))
    elif cmd == "count":
        n = c.execute("SELECT COUNT(*) FROM jobs WHERE kind = ?", (args[0],)).fetchone()[0]
        print("so-job", args[0], n)
    elif cmd in ("last", "lastid"):
        q, p = "SELECT id, status, kind, target, error FROM jobs WHERE kind = ?", [args[0]]
        if len(args) > 1:
            q += " AND target = ?"
            p.append(args[1])
        r = c.execute(q + " ORDER BY rowid DESC LIMIT 1", p).fetchone()
        if r is None:
            print("khong-co-job", args[0])
            return 1
        if cmd == "lastid":
            print(r[0])
            return 0
        print("job", r[0], r[1], r[2], r[3], "loi=" + one_line(r[4], 300))
        for e in c.execute("SELECT seq, kind, step_id, message FROM job_events WHERE job_id = ? ORDER BY seq", (r[0],)):
            print("ev", e[0], e[1], e[2] or "-", one_line(e[3]))
    elif cmd == "backups":
        q, p = "SELECT id, domain, kind, reason, local, remote, remote_tries, remote_key FROM backups", []
        if args:
            q += " WHERE domain = ?"
            p.append(args[0])
        for r in c.execute(q + " ORDER BY created_at DESC, rowid DESC", p):
            print("bk", r[0], r[1], r[2], r[3], "local=%d" % r[4], "remote=" + (r[5] or "''"), "tries=%d" % r[6], "key=" + (r[7] or "-"))
    elif cmd == "row":
        r = c.execute("SELECT local, remote FROM backups WHERE id = ?", (args[0],)).fetchone()
        if r is None:
            print("khong-co-dong", args[0])
            return 1
        print("dong", args[0], "local=%d" % r[0], "remote=" + (r[1] or "''"))
    elif cmd == "settings":
        for r in c.execute("SELECT key, value FROM settings WHERE key LIKE 's3.%' ORDER BY key"):
            print("set %s=%s" % (r[0], one_line(r[1], 200)))
    else:
        print(__doc__)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
