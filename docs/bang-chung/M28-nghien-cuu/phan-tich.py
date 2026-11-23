#!/usr/bin/env python3
"""Đọc câu trả lời của POST /api/v1/sites?dry_run=1 và nói nó có chứa nội dung
của file đích hay không. Không in nội dung nào: chỉ in cỡ, sha256 rút gọn và
kết luận.

    phan-tich.py <file câu trả lời> <file đích | -> <nhãn> [<đường dẫn file trong bản xem trước>]

"LO" khi một trường `old` giải base64 ra trùng file đích, hoặc chứa 48 byte đầu
của nó, hoặc câu trả lời thô chứa base64 của 48 byte đầu đó. File đích "-":
chỉ tóm tắt câu trả lời (dùng cho /dev/zero, FIFO, file lớn).
"""
import base64
import hashlib
import json
import sys


def sha(b):
    return hashlib.sha256(b).hexdigest()[:16]


def main():
    resp_path, target, label = sys.argv[1:4]
    want_path = sys.argv[4] if len(sys.argv) > 4 else ""
    raw = open(resp_path, "rb").read()
    secret = b""
    if target != "-":
        with open(target, "rb") as f:
            secret = f.read(16 << 20)
    try:
        doc = json.loads(raw)
    except ValueError:
        print(f"KQ {label}: câu trả lời không phải JSON ({len(raw)} byte)")
        return
    if isinstance(doc, dict) and "error" in doc:
        e = doc["error"]
        msg = (e.get("message") or {}).get("vi", "")
        leak = bool(secret) and (secret[:48] in raw or base64.b64encode(secret[:48]) in raw)
        print(f"  câu trả lời là lỗi: code={e.get('code')} message.vi={msg[:300]!r}")
        print(f"KQ {label}: {'LO (lỗi chứa byte của file đích)' if leak else 'CHAN (bản xem trước bị từ chối, không có byte nào của file đích)'}")
        return
    files = []
    for st in doc.get("steps", []):
        for f in (st.get("info") or {}).get("files") or []:
            files.append((st["info"].get("id", ""), f))
    if label.startswith("thuong:"):
        # File thường của chính site: `old` phải đúng bằng file trên đĩa.
        for sid, f in files:
            if f.get("path") == want_path:
                old = base64.b64decode(f.get("old") or "")
                ok = f.get("old_exists") and sha(old) == sha(secret) and len(old) == len(secret)
                print(f"  file {want_path}: old {len(old)} byte sha={sha(old)}, file trên đĩa {len(secret)} byte sha={sha(secret)}")
                print(f"KQ {label}: {'DUNG (old đúng bằng file trên đĩa, vẽ được diff)' if ok else 'SAI (old khác file trên đĩa)'}")
                return
        print(f"KQ {label}: SAI (bản xem trước không có file {want_path})")
        return
    hit = False
    for sid, f in files:
        old = base64.b64decode(f.get("old") or "")
        mark = ""
        if secret and old:
            if sha(old) == sha(secret):
                mark = "TRÙNG file đích"
            elif secret[:48] in old:
                mark = "CHỨA 48 byte đầu của file đích"
        if mark:
            hit = True
        if mark or (want_path and f.get("path") == want_path):
            extra = {k: v for k, v in f.items() if k not in ("old", "new", "path")}
            print(f"  file {f.get('path')}: old {len(old)} byte sha={sha(old) if old else '-'} {extra} {mark}")
    if secret and not hit and base64.b64encode(secret[:48]) in raw:
        hit = True
        print("  câu trả lời thô chứa base64 của 48 byte đầu file đích")
    if not secret:
        print(f"KQ {label}: bản xem trước có {len(files)} file, {len(raw)} byte")
    elif hit:
        print(f"KQ {label}: LO (file đích {len(secret)} byte sha={sha(secret)} nằm trong bản xem trước)")
    else:
        print(f"KQ {label}: CHAN (không trường old nào chứa file đích; {len(files)} file trong bản xem trước)")


if __name__ == "__main__":
    main()
