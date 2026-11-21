#!/usr/bin/env python3
"""e2e M18, phần H và L: đọc file user-data do `tpan cloud-init render` sinh,
giải từng file trong write_files (b64, gz+b64) ra một thư mục để quét bí mật ở
dạng rõ, và in cấu trúc để kiểm B12 (đúng một lệnh runcmd).

  python3 giai-user-data.py <user-data> <thư mục ra>

Đọc bằng PyYAML (có sẵn trên Ubuntu vì cloud-init dùng nó), safe_load.
Output: dòng đầu của file, "tep <đường dẫn> <quyền> <mã hoá> <số byte đã giải>"
cho từng file, "runcmd <JSON>", "so-runcmd <n>".
"""
import base64
import gzip
import json
import os
import sys

import yaml

src, out = sys.argv[1], sys.argv[2]
with open(src, "rb") as f:
    raw = f.read()
print("dong-dau", raw.split(b"\n", 1)[0].decode("utf-8", "replace"))
print("co-user-data", len(raw))
doc = yaml.safe_load(raw)
os.makedirs(out, mode=0o700, exist_ok=True)
for i, wf in enumerate(doc.get("write_files") or []):
    enc = wf.get("encoding") or ""
    content = wf.get("content") or ""
    if enc in ("b64", "base64"):
        data = base64.b64decode(content)
    elif enc in ("gz+b64", "gzip+base64", "gz+base64"):
        data = gzip.decompress(base64.b64decode(content))
    else:
        data = content.encode("utf-8")
    name = os.path.join(out, "%d-%s" % (i, os.path.basename(wf.get("path", "khong-ten"))))
    with open(name, "wb") as f:
        f.write(data)
    print("tep", wf.get("path"), wf.get("permissions", "-"), enc or "-", len(data))
runcmd = doc.get("runcmd") or []
print("runcmd", json.dumps(runcmd))
print("so-runcmd", len(runcmd))
