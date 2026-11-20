#!/bin/bash
# Thu tren may ao tpan-m14: thu muc log root:<nhom site> 0750 thi nginx con ghi
# access.log sau khi logrotate gui USR1 khong? So voi HUP.
set -u
B=b.example.com; DB=/var/www/$B
UB=$(stat -c %U $DB)
LB=/var/log/tpan/sites/$B
cb(){ curl -s --max-time 10 --resolve $B:80:127.0.0.1 -o /dev/null "http://$B/$1"; }
n(){ wc -l < "$1" 2>/dev/null || echo "-"; }
echo "## 0. truoc"; stat -c '%A %U:%G %n' $LB $LB/*.log
chgrp $UB $LB $LB/access.log $LB/error.log; chmod 0750 $LB; chmod 0640 $LB/access.log $LB/error.log
systemctl reload nginx; sleep 1
cb ""; cb ""; echo "sau reload: access.log $(n $LB/access.log) dong"
echo; echo "## 1. logrotate -f voi postrotate USR1 (ban hien tai)"
grep -n 'USR1\|HUP' /etc/logrotate.d/tpan-sites
logrotate -f /etc/logrotate.d/tpan-sites; sleep 1
cb "x1"; cb "x2"; cb "x3"; sleep 1
stat -c '%A %U:%G %n' $LB/access.log*
echo "access.log: $(n $LB/access.log) dong | access.log.1: $(n $LB/access.log.1) dong"
echo "x1..x3 nam o dau: access.log=$(grep -c ' /x[123] ' $LB/access.log) access.log.1=$(grep -c ' /x[123] ' $LB/access.log.1)"
echo "nginx error log (may):"; tail -n 5 /var/log/nginx/error.log | cut -c1-220
echo; echo "## 2. gui HUP (reload) roi goi tiep"
kill -HUP "$(cat /run/nginx.pid)"; sleep 2
cb "y1"; cb "y2"; sleep 1
echo "y1,y2 nam o dau: access.log=$(grep -c ' /y[12] ' $LB/access.log) access.log.1=$(grep -c ' /y[12] ' $LB/access.log.1)"
echo; echo "## 3. dat lai chu root, logrotate -f voi postrotate HUP"
chown root $LB/access.log $LB/error.log 2>/dev/null
sed -i 's/kill -USR1/kill -HUP/' /etc/logrotate.d/tpan-sites
cb "z0"; logrotate -f /etc/logrotate.d/tpan-sites; sleep 2
cb "z1"; cb "z2"; sleep 1
stat -c '%A %U:%G %n' $LB/access.log*
echo "z1,z2 nam o dau: access.log=$(grep -c ' /z[12] ' $LB/access.log) access.log.1=$(grep -c ' /z[12] ' $LB/access.log.1)"
echo "nginx error log (may):"; tail -n 3 /var/log/nginx/error.log | cut -c1-220
runuser -u $UB -- head -c 1 $LB/access.log >/dev/null 2>&1; echo "user B doc access.log cua minh: ma $?"
