#!/usr/bin/env python3
"""e2e M18, phần E và L: đổi đúng một byte của một file, tại chỗ.

  python3 doi-byte.py <file> <vị trí>

Byte mới là byte cũ XOR 0xff, nên chắc chắn khác byte cũ, và chạy lại lần nữa
thì file về như cũ. Không đổi cỡ, chủ hay quyền của file (ghi đè tại chỗ, không
tạo file mới): object của versitygw giữ nguyên ETag cũ trong thuộc tính mở rộng.
"""
import sys

path, pos = sys.argv[1], int(sys.argv[2])
with open(path, "r+b") as f:
    f.seek(pos)
    old = f.read(1)
    if len(old) != 1:
        sys.exit("%s ngắn hơn %d byte" % (path, pos + 1))
    f.seek(pos)
    f.write(bytes([old[0] ^ 0xFF]))
print("doi-byte %s vi-tri=%d %02x->%02x" % (path, pos, old[0], old[0] ^ 0xFF))
