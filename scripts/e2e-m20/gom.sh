#!/usr/bin/env bash
# ĐỒ NGHỀ TEST của scripts/e2e-m20.sh (T63, T65), chỉ chạy TRONG máy ảo test,
# bằng root.
#
#   gom.sh <file header token> <file chạy mcpclient> <thư mục ra>
#
# Gom những thứ T60–T66 sẽ quét bí mật:
#   1. sự kiện và bằng chứng của MỌI job qua API: GET /api/v1/jobs, rồi với
#      mỗi job GET /api/v1/jobs/{id} và luồng /api/v1/jobs/{id}/events, bằng
#      token (đủ phạm vi đọc jobs) của e2e;
#   2. output công cụ MCP: client thử của M7 (scripts/e2e-m7/mcpclient, build
#      cho Linux) mở `tpan mcp` ngay trong máy, gọi list_sites, list_jobs và
#      get_job cho mọi job.
# Chỉ in số đếm, không in nội dung (nội dung để quet.sh đếm bí mật).
# -k: gọi vòng 127.0.0.1 trong chính máy ảo test.
set -uo pipefail
umask 077
hdr=$1 mcp=$2 out=$3
base=https://127.0.0.1:8443
mkdir -p "$out/jobs"

curl -sk -m 30 -H @"$hdr" -o "$out/jobs.json" -w 'GET /api/v1/jobs?limit=500: HTTP %{http_code}\n' "$base/api/v1/jobs?limit=500"
jq -r '.[].id' "$out/jobs.json" >"$out/job-ids" 2>/dev/null || : >"$out/job-ids"
ok=0 sse=0
while read -r id; do
  [ -n "$id" ] || continue
  c=$(curl -sk -m 30 -H @"$hdr" -o "$out/jobs/$id.json" -w '%{http_code}' "$base/api/v1/jobs/$id")
  [ "$c" = 200 ] && ok=$((ok + 1))
  # Luồng SSE của job đã xong gửi lại mọi sự kiện rồi đóng; -m chặn trường hợp nó giữ kết nối.
  c=$(curl -sk -m 8 -H @"$hdr" -H 'Accept: text/event-stream' -o "$out/jobs/$id.events" -w '%{http_code}' "$base/api/v1/jobs/$id/events")
  [ "$c" = 200 ] && sse=$((sse + 1))
done <"$out/job-ids"
echo "job: $(grep -c . "$out/job-ids"); GET /jobs/{id} 200: $ok; /events 200: $sse; tổng cỡ: $(du -sb "$out/jobs" | cut -f1) byte"

{
  echo "list_sites {}"
  echo 'list_jobs {"limit":50}'
  while read -r id; do
    [ -n "$id" ] && jq -nc --arg id "$id" '"get_job " + ({id: $id} | tojson)' -r
  done <"$out/job-ids"
} >"$out/mcp.in"
"$mcp" -timeout 10m -- /usr/local/bin/tpan mcp <"$out/mcp.in" >"$out/mcp.out" 2>"$out/mcp.err"
rc=$?
echo "mcpclient: mã thoát $rc; lời gọi công cụ: $(grep -c '"op":"call"' "$out/mcp.out"), có isError: $(grep -c '"isError":true' "$out/mcp.out"); cỡ output: $(wc -c <"$out/mcp.out") byte"
jq -r 'select(.op == "call") | .tool + (if .result.isError then " (isError)" else "" end) + (if .error then " (LỖI GIAO THỨC)" else "" end)' "$out/mcp.out" 2>/dev/null | sort | uniq -c
