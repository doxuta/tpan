#!/usr/bin/env python3
"""Gửi một request HTTP thô (đọc từ stdin, byte nguyên văn) tới 127.0.0.1:<cổng>, in kết quả.

Cần cho bộ R01-R14 của e2e M19: curl không gửi được Host rỗng, Host UTF-8 thô, hay dòng
yêu cầu dạng tuyệt đối (GET http://tên/ HTTP/1.1) đúng nguyên văn.

    printf 'GET / HTTP/1.1\\r\\nHost: x\\r\\nConnection: close\\r\\n\\r\\n' | http-raw.py 80

In đúng hai dòng:
    STATUS <mã HTTP | CLOSED>     CLOSED: máy chủ đóng kết nối mà không trả dòng trạng thái
    LOCATION <giá trị header Location, hoặc ->
"""
import socket
import sys


def main() -> int:
    port = int(sys.argv[1])
    req = sys.stdin.buffer.read()
    data = b""
    try:
        s = socket.create_connection(("127.0.0.1", port), timeout=10)
        s.sendall(req)
        while True:
            chunk = s.recv(65536)
            if not chunk:
                break
            data += chunk
            if b"\r\n\r\n" in data:
                break
        s.close()
    except (ConnectionResetError, BrokenPipeError):
        pass
    except OSError as e:
        print("STATUS ERROR", e)
        print("LOCATION -")
        return 1
    head = data.split(b"\r\n\r\n", 1)[0].decode("latin-1")
    lines = head.split("\r\n")
    if not data or not lines[0].startswith("HTTP/"):
        print("STATUS CLOSED")
        print("LOCATION -")
        return 0
    status = lines[0].split(" ", 2)[1]
    loc = "-"
    for ln in lines[1:]:
        if ln.lower().startswith("location:"):
            loc = ln.split(":", 1)[1].strip()
            break
    print("STATUS", status)
    print("LOCATION", loc)
    return 0


sys.exit(main())
