#!/usr/bin/env python3
"""Phân tích câu trả lời của API tpan cho e2e M28 (bước 31). Chạy trong máy ảo,
bằng root. Dựng từ docs/bang-chung/M28-nghien-cuu/phan-tich.py.

KHÔNG BAO GIỜ in nội dung file, mật khẩu hay cookie: chỉ in cỡ, sha256 rút gọn
(16 số hex đầu), mã HTTP, mã lỗi và kết luận. Mỗi lệnh thoát 0 khi điều được
hỏi là đúng, 1 khi sai; script bash biến mã thoát đó thành một dòng
"=> ĐẠT" hoặc "=> KHÔNG ĐẠT".

    phan-tich.py thuong <câu trả lời> <file trên đĩa> <path trong bản xem trước>
    phan-tich.py khong-lo <câu trả lời> <file đích>...
    phan-tich.py old-link <câu trả lời> <path>
    phan-tich.py loi <câu trả lời> <mã lỗi | *>
    phan-tich.py khong-var-www <câu trả lời>
    phan-tich.py dua <thư mục> <file đích>...
    phan-tich.py job-id <câu trả lời>          (in job_id)
    phan-tich.py job-status <câu trả lời>      (in job.status)
    phan-tich.py luu-muc <câu trả lời> <path> <file lưu>
    phan-tich.py giong-muc <câu trả lời> <path> <file đã lưu>

"Chứa file đích" (lệnh khong-lo, dua): một trường `old` giải base64 ra trùng
sha256 với file đích hoặc chứa dấu của nó, hoặc câu trả lời thô chứa dấu đó ở
dạng thô hay dạng base64. Dấu là 48 byte đầu của file đích; riêng file bắt
đầu bằng "<?php" (wp-config.php), 48 byte đầu là lời chú thích mẫu của
WordPress, giống hệt ở mọi site, nên dấu là 48 byte bắt đầu từ chữ
DB_PASSWORD (phần bí mật thật). Câu trả lời không phải JSON (log job ghép
nhiều câu trả lời) thì chỉ xét dạng thô.
"""
import base64
import glob
import hashlib
import json
import os
import sys


def sha(b):
    return hashlib.sha256(b).hexdigest()[:16]


def load(path):
    with open(path, "rb") as f:
        raw = f.read()
    try:
        doc = json.loads(raw)
    except ValueError:
        doc = None
    return raw, doc


def files_of(doc):
    out = []
    if isinstance(doc, dict):
        for st in doc.get("steps") or []:
            for f in ((st or {}).get("info") or {}).get("files") or []:
                out.append(f)
    return out


def entry(doc, path):
    for f in files_of(doc):
        if f.get("path") == path:
            return f
    return None


def b64(s):
    try:
        return base64.b64decode(s or "")
    except ValueError:
        return b""


def secret_of(path):
    with open(path, "rb") as f:
        data = f.read(16 << 20)
    mark = data[:48]
    if data.startswith(b"<?php"):
        i = data.find(b"DB_PASSWORD")
        if i >= 0:
            mark = data[i:i + 48]
    return data, mark


def leak(raw, doc, secret, mark):
    """Lý do câu trả lời chứa file đích, hoặc '' nếu không chứa."""
    if not secret:
        return ""
    full = hashlib.sha256(secret).digest()
    for f in files_of(doc):
        old = b64(f.get("old"))
        if not old:
            continue
        if hashlib.sha256(old).digest() == full:
            return f"trường old của {f.get('path')} trùng sha256 file đích"
        if mark in old:
            return f"trường old của {f.get('path')} chứa dấu của file đích"
    if mark in raw:
        return "câu trả lời thô chứa dấu của file đích"
    if base64.b64encode(mark) in raw:
        return "câu trả lời thô chứa base64 của dấu file đích"
    return ""


def describe(raw, doc):
    if doc is None:
        return f"{len(raw)} byte, không phải JSON"
    if isinstance(doc, dict) and "error" in doc:
        e = doc.get("error") or {}
        return f"{len(raw)} byte, lỗi code={e.get('code')}"
    return f"{len(raw)} byte, {len(files_of(doc))} file trong bản xem trước"


def cmd_thuong(resp, disk, path):
    raw, doc = load(resp)
    with open(disk, "rb") as f:
        want = f.read()
    f = entry(doc, path)
    if f is None:
        print(f"    phân tích: không có mục {path} ({describe(raw, doc)})")
        return 1
    old, new = b64(f.get("old")), b64(f.get("new"))
    print(f"    phân tích: mục {path}: old_exists={f.get('old_exists')}, old {len(old)} byte sha={sha(old)}, "
          f"file trên đĩa {len(want)} byte sha={sha(want)}, new {len(new)} byte sha={sha(new)}")
    return 0 if f.get("old_exists") is True and old == want and len(new) > 0 else 1


def cmd_khong_lo(resp, *targets):
    raw, doc = load(resp)
    bad = 0
    for t in targets:
        secret, mark = secret_of(t)
        why = leak(raw, doc, secret, mark)
        print(f"    phân tích: {t} ({len(secret)} byte sha={sha(secret)}): "
              f"{'CHỨA: ' + why if why else 'không chứa'}; câu trả lời {describe(raw, doc)}")
        bad += bool(why)
    return 1 if bad else 0


def cmd_old_link(resp, path):
    raw, doc = load(resp)
    f = entry(doc, path)
    if f is None:
        print(f"    phân tích: không có mục {path} ({describe(raw, doc)})")
        return 1
    link = f.get("old_link") or ""
    old = b64(f.get("old"))
    print(f"    phân tích: mục {path}: old_link {'có (' + str(len(link)) + ' ký tự)' if link else 'không có'}, "
          f"old {len(old)} byte, old_exists={f.get('old_exists')}")
    return 0 if link and not old else 1


def cmd_loi(resp, code):
    raw, doc = load(resp)
    if not (isinstance(doc, dict) and isinstance(doc.get("error"), dict)):
        print(f"    phân tích: không phải JSON lỗi ({describe(raw, doc)})")
        return 1
    got = doc["error"].get("code")
    print(f"    phân tích: error.code={got}")
    return 0 if got and (code == "*" or got == code) else 1


def cmd_khong_var_www(resp):
    raw, doc = load(resp)
    if doc is None or (isinstance(doc, dict) and "error" in doc):
        print(f"    phân tích: không phải bản xem trước ({describe(raw, doc)})")
        return 1
    paths = [f.get("path") or "" for f in files_of(doc)]
    inz = [p for p in paths if p.startswith("/var/www/")]
    print(f"    phân tích: {len(paths)} file trong bản xem trước, {len(inz)} file dưới /var/www/: {inz}")
    return 0 if not inz else 1


def cmd_dua(d, *targets):
    codes = {}
    with_code = 0
    for p in sorted(glob.glob(os.path.join(d, "codes.*"))):
        with open(p) as f:
            for line in f:
                parts = line.split()
                if len(parts) == 2:
                    codes[parts[1]] = codes.get(parts[1], 0) + 1
                    with_code += 1
    secrets = [secret_of(t) for t in targets]
    resps = sorted(glob.glob(os.path.join(d, "resp.*")))
    leaks = 0
    reasons = {}
    for r in resps:
        raw, doc = load(r)
        for secret, mark in secrets:
            why = leak(raw, doc, secret, mark)
            if why:
                leaks += 1
                reasons[why] = reasons.get(why, 0) + 1
                break
    print(f"    phân tích: {with_code} lần gọi, theo mã HTTP: {dict(sorted(codes.items()))}; "
          f"{len(resps)} câu trả lời, {leaks} câu trả lời chứa file đích {reasons if reasons else ''}")
    return 0 if leaks == 0 else 1


def cmd_job_id(resp):
    _, doc = load(resp)
    print((doc or {}).get("job_id", "") if isinstance(doc, dict) else "")
    return 0


def cmd_job_status(resp):
    _, doc = load(resp)
    st = ""
    if isinstance(doc, dict):
        st = ((doc.get("job") or {}).get("status")) or ""
    print(st)
    return 0


def pick(f):
    return {"path": f.get("path"), "old": f.get("old"), "new": f.get("new"),
            "old_exists": f.get("old_exists")}


def cmd_luu_muc(resp, path, out):
    raw, doc = load(resp)
    f = entry(doc, path)
    if f is None:
        print(f"    phân tích: không có mục {path} ({describe(raw, doc)})")
        return 1
    m = pick(f)
    fd = os.open(out, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as w:
        json.dump(m, w)
    print(f"    phân tích: đã lưu mục {path}: old sha={sha(b64(m['old']))}, new sha={sha(b64(m['new']))}, "
          f"old_exists={m['old_exists']}")
    return 0


def cmd_giong_muc(resp, path, saved):
    raw, doc = load(resp)
    f = entry(doc, path)
    with open(saved) as r:
        before = json.load(r)
    if f is None:
        print(f"    phân tích: không có mục {path} ({describe(raw, doc)})")
        return 1
    now = pick(f)
    diff = [k for k in ("path", "old", "new", "old_exists") if now.get(k) != before.get(k)]
    print(f"    phân tích: trước old sha={sha(b64(before['old']))} new sha={sha(b64(before['new']))}; "
          f"sau old sha={sha(b64(now['old']))} new sha={sha(b64(now['new']))}; trường khác: {diff or 'không'}")
    return 0 if not diff else 1


CMDS = {
    "thuong": cmd_thuong,
    "khong-lo": cmd_khong_lo,
    "old-link": cmd_old_link,
    "loi": cmd_loi,
    "khong-var-www": cmd_khong_var_www,
    "dua": cmd_dua,
    "job-id": cmd_job_id,
    "job-status": cmd_job_status,
    "luu-muc": cmd_luu_muc,
    "giong-muc": cmd_giong_muc,
}

if __name__ == "__main__":
    if len(sys.argv) < 3 or sys.argv[1] not in CMDS:
        print(__doc__, file=sys.stderr)
        sys.exit(2)
    try:
        sys.exit(CMDS[sys.argv[1]](*sys.argv[2:]))
    except (OSError, TypeError, KeyError) as e:
        # Không in nội dung: chỉ loại lỗi và đường dẫn (OSError mang tên file).
        print(f"    phân tích: lỗi {type(e).__name__}: {getattr(e, 'filename', '') or ''}")
        sys.exit(1)
