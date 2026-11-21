#!/usr/bin/env python3
"""Kẻ tráo của e2e M28 mục 4 (đua). Chạy bằng user Linux của site (runuser),
nhận mã qua stdin để user đó không cần đọc được thư mục chứa script:

    runuser -u <user site> -- python3 - m28-tra-doi file <.htaccess> <đích symlink> <tra-doi.py
    runuser -u <user site> -- python3 - m28-tra-doi dir <public> <thư mục nạn nhân> <tra-doi.py

Tham số đầu "m28-tra-doi" chỉ là dấu để script bash tìm và dừng tiến trình
này (pkill -f). Dừng bằng SIGTERM; khi dừng in số lần đã tráo ra stdout và
để lại đúng thứ thật ở tên bị tráo (file thường, thư mục thật).

file: tráo <.htaccess> giữa một bản sao file thường (nội dung đọc lúc bắt đầu)
và symlink tới <đích>, mỗi lần bằng cách tạo tên tạm rồi os.replace: lúc nào
ở tên đó cũng có một trong hai thứ.

dir: tráo <public> với một symlink tới <thư mục nạn nhân> bằng renameat2
(RENAME_EXCHANGE): một lời gọi đổi chỗ hai tên, lúc nào ở tên <public> cũng có
thư mục thật hoặc symlink.
"""
import ctypes
import os
import signal
import sys

running = True


def stop(*_):
    global running
    running = False


signal.signal(signal.SIGTERM, stop)
signal.signal(signal.SIGINT, stop)


def run_file(ht, target):
    with open(ht, "rb") as f:
        data = f.read()
    tmp = os.path.join(os.path.dirname(ht), ".m28-tra-doi.tmp")
    n = 0

    def put_regular():
        try:
            os.unlink(tmp)
        except FileNotFoundError:
            pass
        fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o640)
        try:
            os.write(fd, data)
            os.fchmod(fd, 0o640)
        finally:
            os.close(fd)
        os.replace(tmp, ht)

    while running:
        try:
            os.unlink(tmp)
        except FileNotFoundError:
            pass
        os.symlink(target, tmp)
        os.replace(tmp, ht)
        n += 1
        put_regular()
        n += 1
    put_regular()
    return n


def run_dir(pub, victim):
    libc = ctypes.CDLL(None, use_errno=True)
    at_fdcwd, rename_exchange = -100, 2
    link = pub + ".m28-lien"
    try:
        os.unlink(link)
    except FileNotFoundError:
        pass
    os.symlink(victim, link)
    a, b = os.fsencode(pub), os.fsencode(link)

    def xchg():
        if libc.renameat2(at_fdcwd, a, at_fdcwd, b, rename_exchange) != 0:
            e = ctypes.get_errno()
            raise OSError(e, os.strerror(e), pub)

    n = 0
    while running:
        xchg()
        n += 1
    if os.path.islink(pub):
        xchg()
    os.unlink(link)
    return n


def main():
    mode, a, b = sys.argv[2:5]
    n = run_file(a, b) if mode == "file" else run_dir(a, b)
    print(f"so-lan-trao={n}")


if __name__ == "__main__":
    main()
