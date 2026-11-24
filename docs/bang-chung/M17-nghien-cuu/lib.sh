#!/bin/bash
# lib.sh: hàm dùng chung của các bản thử M17. `make nc-m17 T=<tên>` nối file này
# vào trước script rồi chạy bằng root trong máy ảo tpan-m17 (không chạy ở đâu khác).
#
# Mô hình đang thử (khác bản thử t5 của M14 ở ba điểm):
#   1. Tiến trình chính của PHP-FPM chạy bằng CHÍNH user của site, không phải root.
#   2. Socket do systemd tạo và giữ (unit .socket), trao cho FPM qua biến FPM_SOCKETS.
#   3. Hàng rào nằm trong một drop-in theo TIỀN TỐ tên unit, nên dịch vụ PHP và mọi
#      lệnh chạy thay cho site (systemd-run --unit=tpan-cage-<slug>-run-...) cùng một hàng rào.
set -u
A=a.example.com; B=b.example.com
V=${V:-8.3}
slug_of(){ stat -c %U "/var/www/$1" | sed 's/^tp_//'; }
SA=$(slug_of $A); SB=$(slug_of $B); UA=tp_$SA; UB=tp_$SB
ca(){ local p=$1; shift; curl -s --max-time 60 --resolve $A:80:127.0.0.1 "$@" "http://$A/$p"; }
cb(){ local p=$1; shift; curl -s --max-time 60 --resolve $B:80:127.0.0.1 "$@" "http://$B/$p"; }
code_a(){ ca "$1" -o /dev/null -w '%{http_code}'; }
code_b(){ cb "$1" -o /dev/null -w '%{http_code}'; }
sock_of(){ echo "/run/tpan-cage/$(slug_of $1)/php.sock"; }
# Chạy một lệnh thay cho site, trong đúng hàng rào và slice của nó.
run_in(){ local d=$1; shift; systemd-run --quiet --wait --pipe --collect --unit="tpan-cage-$(slug_of $d)-run-$RANDOM$RANDOM" -- "$@"; }
# Đặt file PHP vào public của site (dưới user của site).
put_php(){ local d=$1 name=$2; runuser -u "tp_$(slug_of $d)" -- tee "/var/www/$d/public/$name" >/dev/null; }

# HANG_RAO: các dòng hàng rào. Bản thử có thể đặt HANG_RAO_BO="Ten1 Ten2" để bỏ
# vài thuộc tính (dùng khi dò thuộc tính nào làm hỏng WordPress).
hang_rao(){ # domain
  local d=$1 s u dir etc
  s=$(slug_of $d); u=tp_$s; dir=/var/www/$d; etc=/etc/tpan/cage/$s
  cat <<EOF
User=$u
Group=$u
Slice=tpan-$s.slice
NoNewPrivileges=yes
CapabilityBoundingSet=
AmbientCapabilities=
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
PrivateIPC=yes
ProtectProc=invisible
ProcSubset=pid
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
SystemCallArchitectures=native
SystemCallFilter=@system-service
SystemCallErrorNumber=EPERM
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
UMask=0022
TemporaryFileSystem=/var:ro
BindPaths=$dir
BindPaths=/var/log/tpan/sites/$d
BindReadOnlyPaths=/var/cache/tpan
BindReadOnlyPaths=/run/mysqld:/var/run/mysqld
TemporaryFileSystem=/run:ro
BindReadOnlyPaths=/run/mysqld
BindReadOnlyPaths=/run/systemd/resolve
BindReadOnlyPaths=/run/systemd/notify
BindReadOnlyPaths=/run/systemd/journal/dev-log
TemporaryFileSystem=/dev/shm:rw,nosuid,nodev,noexec,mode=1777
TemporaryFileSystem=/etc/tpan:ro
BindReadOnlyPaths=$etc
BindReadOnlyPaths=$etc/passwd:/etc/passwd
BindReadOnlyPaths=$etc/group:/etc/group
InaccessiblePaths=-/etc/nginx -/etc/apache2 -/etc/cron.d -/etc/letsencrypt -/etc/systemd -/etc/php/$V/fpm/pool.d
InaccessiblePaths=-/etc/passwd- -/etc/group- -/etc/shadow -/etc/shadow- -/etc/gshadow -/etc/gshadow-
InaccessiblePaths=-/sys/fs/cgroup -/boot -/mnt -/media -/srv
EOF
}

# long_dung <domain> [ramMB] [cpu%] [tasks]: dựng lồng cho site (slice, hàng rào,
# socket, dịch vụ), CHƯA trỏ vhost. 0 là không đặt trần đó.
long_dung(){
  local d=$1 ram=${2:-0} cpu=${3:-0} tasks=${4:-0} s u dir etc sock bo p
  s=$(slug_of $d); u=tp_$s; dir=/var/www/$d; etc=/etc/tpan/cage/$s; sock=$(sock_of $d)
  mkdir -p "$etc" "/etc/systemd/system/tpan-cage-$s-.service.d"
  { getent passwd root; getent passwd "$u"; getent passwd nobody; } > "$etc/passwd"
  { getent group root; getent group "$u"; getent group nogroup; } > "$etc/group"
  chmod 644 "$etc/passwd" "$etc/group"
  { echo "[Unit]"; echo "Description=TPAN: tran tai nguyen cua $d"; echo "[Slice]"
    echo "MemoryAccounting=yes"; echo "CPUAccounting=yes"; echo "TasksAccounting=yes"; echo "IOAccounting=yes"
    [ "$ram" != 0 ] && { echo "MemoryMax=${ram}M"; echo "MemorySwapMax=0"; }
    [ "$cpu" != 0 ] && echo "CPUQuota=${cpu}%"
    [ "$tasks" != 0 ] && echo "TasksMax=$tasks"
  } > "/etc/systemd/system/tpan-$s.slice"
  { echo "[Service]"; hang_rao "$d"; } > "/etc/systemd/system/tpan-cage-$s-.service.d/10-hang-rao.conf"
  for bo in ${HANG_RAO_BO:-}; do sed -i "/^$bo=/d" "/etc/systemd/system/tpan-cage-$s-.service.d/10-hang-rao.conf"; done
  cat > "/etc/systemd/system/tpan-cage-$s-php.socket" <<EOF
[Unit]
Description=TPAN: socket PHP cua $d
[Socket]
ListenStream=$sock
SocketUser=www-data
SocketGroup=www-data
SocketMode=0660
DirectoryMode=0755
RemoveOnStop=yes
[Install]
WantedBy=sockets.target
EOF
  cat > "/etc/systemd/system/tpan-cage-$s-php.service" <<EOF
[Unit]
Description=TPAN: PHP-FPM $V rieng cua $d
# t5 lần đầu: mặc định systemd chỉ cho 5 lần khởi động trong 10 giây; quá thì dịch
# vụ VÀ socket cùng vào trạng thái failed, file socket bị gỡ, site ra 502 cho tới
# khi có người reset. Nới ra để vài lần restart liền nhau của tpan không dính.
StartLimitIntervalSec=${SLI:-30}
StartLimitBurst=${SLB:-10}
[Service]
Type=notify
Environment=FPM_SOCKETS=$sock=3
ExecStart=/usr/sbin/php-fpm$V --nodaemonize --fpm-config $etc/php-fpm.conf
ExecReload=/bin/kill -USR2 \$MAINPID
KillSignal=SIGQUIT
TimeoutStopSec=30
OOMPolicy=continue
Restart=on-failure
EOF
  cat > "$etc/php-fpm.conf" <<EOF
[global]
; Lần chạy đầu của t1: error_log = /proc/self/fd/2 làm FPM không khởi động được
; ("No such device or address"): stderr của một unit systemd là socket của
; journald, không mở lại qua /proc được. syslog thì đi vào journal của đúng unit.
error_log = syslog
syslog.ident = php-fpm-$s
daemonize = no
; Mặc định là 0: khi dừng hay reload, FPM gửi SIGQUIT rồi NGAY lập tức SIGTERM cho
; tiến trình con, request đang chạy bị cắt (t1: 1 trong 40 request ra 502,
; "Connection reset by peer"). Có hạn chờ thì tiến trình con làm xong request rồi mới thoát.
process_control_timeout = ${PCT:-10s}
[tpan-$s]
listen = $sock
pm = ondemand
pm.max_children = ${WORKERS:-5}
pm.process_idle_timeout = 10s
pm.max_requests = 500
php_admin_value[open_basedir] = $dir/
php_admin_value[upload_tmp_dir] = $dir/tmp
php_admin_value[session.save_path] = $dir/tmp
php_admin_value[sys_temp_dir] = $dir/tmp
php_admin_value[upload_max_filesize] = 64M
php_admin_value[post_max_size] = 64M
php_admin_flag[log_errors] = on
php_admin_value[error_log] = /var/log/tpan/sites/$d/php-error.log
EOF
  chmod 644 "$etc/php-fpm.conf"
  systemctl daemon-reload
  systemctl enable --now --quiet "tpan-cage-$s-php.socket"
}

# long_vao <domain>: trỏ vhost nginx sang socket của lồng (giữ bản gốc để trả lại).
long_vao(){
  local d=$1 vh=/etc/nginx/sites-available/tpan-$1.conf sock
  sock=$(sock_of $d)
  [ -f "/root/vh-goc-$d" ] || cp -p "$vh" "/root/vh-goc-$d"
  sed -i "s|unix:[^;]*;|unix:$sock;|" "$vh"
  nginx -t 2>&1 | tail -1; systemctl reload nginx; sleep 0.5
}

# long_go <domain>: trả vhost, dừng và xoá mọi thứ của lồng.
long_go(){
  local d=$1 s vh=/etc/nginx/sites-available/tpan-$1.conf
  s=$(slug_of $d)
  if [ -f "/root/vh-goc-$d" ]; then cp -p "/root/vh-goc-$d" "$vh"; rm -f "/root/vh-goc-$d"; nginx -t 2>&1 | tail -1; systemctl reload nginx; fi
  systemctl disable --now --quiet "tpan-cage-$s-php.socket" 2>/dev/null
  systemctl stop "tpan-cage-$s-php.service" 2>/dev/null
  systemctl stop "tpan-$s.slice" 2>/dev/null
  rm -rf "/etc/systemd/system/tpan-cage-$s-php.socket" "/etc/systemd/system/tpan-cage-$s-php.service" \
         "/etc/systemd/system/tpan-cage-$s-.service.d" "/etc/systemd/system/tpan-$s.slice" "/etc/tpan/cage/$s"
  rmdir /etc/tpan/cage /etc/tpan 2>/dev/null
  systemctl daemon-reload; systemctl reset-failed "tpan-cage-$s-php.service" 2>/dev/null
  rmdir "/run/tpan-cage/$s" /run/tpan-cage 2>/dev/null
  return 0
}
