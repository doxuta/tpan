# M8: AI trong panel (`tpan brain`)

Đọc kèm: `07-m7-cong-cho-agent.md` (công cụ, bộ che, `tpan mcp`, Plan chờ duyệt), `03-m3-giao-dien-va-api.md` (API, SSE của job), `00-go-can-ban.md` (interface, context, goroutine), `docs/KE-HOACH.md` mục 8.1, 8.2, 8.4, 12 (bước 13), 13 (dòng M8), `docs/thay-doi/M8.md` (bảng chia việc, 7 lần gọi `claude -p` thật, chỗ khác đặc tả), và bốn log trong `docs/bang-chung/`: `M8-claude-init.log`, `M8.log`, `M8-claude.log`, `M8-claude-1.log`.

## 1. Mốc này làm được gì

- Trên panel, bạn bấm "Hỏi AI", "Giải thích" (bước job lỗi, phép kiểm không đạt, dòng log), "Chẩn đoán" (sự cố) hoặc gõ vào `⌘K`. Ngăn AI hiện đúng thứ sẽ gửi đi (bản đã che) trước khi bạn bấm Gửi; chữ trả lời hiện dần, kèm từng lần AI gọi công cụ.
- Bộ não là Claude Code đã đăng nhập trên máy cá nhân của bạn. Bạn chạy `tpan brain <máy>` trong một terminal; lệnh đó giữ một kết nối SSH tới máy chủ và chạy `claude -p` cho mỗi câu hỏi từ panel. Máy chủ không giữ khoá hay đăng nhập AI nào.
- AI đọc máy chủ qua công cụ của `tpan mcp` (M7) và chỉ đề xuất được Plan chờ duyệt. Có thêm ba công cụ: `current_view`, `propose_health_fix`, `propose_ssl_issue`.
- Mỗi câu hỏi mang theo nơi bạn đang đứng, đường vừa đi và bằng chứng của thứ bạn đang chỉ vào. Chuyển sang trang khác rồi hỏi "còn site kia thì sao?" thì AI hiểu đúng. Trang "Phiên AI" cho xem lại từng lượt đã gửi gì.
- Trước khi tin một phiên Claude Code, tpan đọc sự kiện `init` của nó; thấy một công cụ hay MCP server lạ thì dừng phiên, không câu trả lời nào được hiện.

## 2. Chạy thử

Có bốn log của mốc này:

| Log | Chạy gì | Lúc |
|---|---|---|
| `M8-claude-init.log` | lần gọi `claude -p` thật số 1 đến 3, trên Mac, để xem sự kiện `init` thật | đợt 0 và 1 |
| `M8.log` | e2e bước 13 với lệnh `claude` giả, máy ảo `tpan-m8`. Có hai bản: bản đầu (105 dòng `=> ĐẠT`) commit ở `f97ca1c`; bản hiện tại là lần chạy lại trên mã cuối (106 dòng `=> ĐẠT`), dòng cuối của cả hai là `TỔNG KẾT: 0 khẳng định không đạt` | bản đầu 22:00 tới 22:01; chạy lại 22:21 tới 22:22 |
| `M8-claude-1.log` | Claude Code thật hỏi từ panel, lần gọi 4 và 5, **trước** khi sửa chữ dính | 21:48 tới 21:50 |
| `M8-claude.log` | Claude Code thật, lần gọi 6 và 7, chạy lại sau khi sửa chữ dính, 63 dòng `=> ĐẠT`, 0 không đạt | 21:56 tới 21:57 |

Tên file dễ gây nhầm: `M8-claude-1.log` là lần chạy **cũ hơn**.

Điều quan trọng khi đọc ba log máy ảo: cả ba build từ commit `e03df6e`. Ở `M8-claude-1.log`, `git status --short` không có file mã nào đã sửa (chỉ có log và kịch bản e2e chưa commit). Ở `M8.log` và `M8-claude.log`, bản build là `e03df6e-dirty` và `git status --short` liệt kê `stream.go`, `types.go` và test của `claudecode` là đã sửa (bản sửa chữ dính), nhưng **không** có `internal/ai/panel.go`. Tức là bản sửa "câu trả lời là toàn bộ chữ đã nối" (commit `a7a6090`, sửa `panel.go`) được commit sau cả ba lần chạy đó. Sau đó e2e M8 được chạy lại trên mã cuối (build từ `2543ecc`, có cả bản sửa reload của 2.13 lúc chưa commit) và ghi đè `docs/bang-chung/M8.log`; kết quả ở 2.10.

Các mục 2.1 đến 2.8 trích **bản đầu** của `M8.log`, xem lại bằng `git show f97ca1c:docs/bang-chung/M8.log`. Bản chạy lại đi đúng các bước đó nhưng id phiên, id Plan và số mili giây khác (ví dụ phiên `8dc11f0a8ba8a814`, Plan `b40a4e8ee0eb88b0`, lời từ chối `Bash` ở `+0.194s`).

Kịch bản tự che: `<vm>` là IP máy ảo, `<tmp>` là thư mục tạm trên Mac, `<mac-host>` là tên máy Mac, `<user>` là tên user. `curl` đứng thay trình duyệt: không log nào mở giao diện thật. Phần ngăn AI trên trình duyệt chưa có bằng chứng chạy tay.

### 2.1 Bật bộ não trên máy chủ (phần c của `M8.log`)

Chạy trên Mac, lệnh vào máy ảo:

```bash
scripts/vm.sh root tpan mcp config --brain bridge
scripts/vm.sh root systemctl restart tpan
```

Rồi kịch bản xem socket nội bộ mà `tpan serve` vừa mở:

```
active
drwx------ root:root /run/tpan
srw------- root:root /run/tpan/brain.sock
[2026-11-19 22:01:18] (mã thoát 0)
  => ĐẠT: tpan active, /run/tpan/brain.sock là socket 0600 của root
```

Chữ `s` đầu `srw-------` nghĩa là file này là một Unix socket. Chỉ root đọc ghi được nó (xem phần 4).

Hỏi panel trước khi có bộ não nào nối (`GET /api/v1/ai/brain`):

```
{
  "mode": "bridge",
  "connected": false,
  "connect_command": "tpan brain <user>@tpan-m8"
}
```

### 2.2 Nối bộ não (phần e)

Chạy trên Mac. Log dùng `claude` giả đặt đầu `PATH` và một dãy `--ssh-opt` để không đụng `~/.ssh` của bạn; phần cần hiểu là:

```bash
tpan brain ubuntu@<vm> --max-turns 4
```

```
Bộ não: đã nối ubuntu@<vm>, Claude Code 2.1.328, model sonnet
Mỗi câu hỏi từ panel hiện sau dấu ?, mỗi lần gọi công cụ sau dấu →. Dừng bằng Ctrl+C.
  => ĐẠT: tpan brain nối được (in "Bộ não: đã nối")
```

Panel giờ thấy bộ não:

```
{
  "mode": "bridge",
  "connected": true,
  "host": "<mac-host>",
  "claude_version": "2.1.328",
  "tested_version": "2.1.328",
  "model": "sonnet",
  "auth_method": "claude.ai",
  "since": "2026-11-19T22:01:19.846068668+07:00",
  "connect_command": "tpan brain <user>@tpan-m8"
}
```

`auth_method: claude.ai` nghĩa là Claude Code đăng nhập bằng gói Claude, không phải khoá API. tpan chỉ đọc đúng hai trường của `claude auth status` (2.11).

### 2.3 Xem trước thứ sẽ gửi (phần f)

Giao diện báo chuyển mục bằng `POST /api/v1/ai/views` với `{"path":"/sites/a.example.com","label":"Trang site a.example.com"}` (trả 204), rồi xin bản xem trước bằng `POST /api/v1/ai/preview`. Kết quả (đã cắt phần đầu):

```
  "playbook": "site",
  "chips": [
    {
      "key": "view",
      "label": "Đang ở",
      "omitted": false,
      "content": "Trang site site-a.example (/sites/site-a.example)"
    },
    {
      "key": "trail",
      "label": "Vừa đi qua",
      "omitted": false,
      "content": "Bảng điều khiển (/)\nTrang site site-a.example (/sites/site-a.example)"
    },
    {
      "key": "machine",
      "label": "Bối cảnh máy",
      "omitted": false,
      "content": "web server: nginx\ndefault php: 8.3\nactive sites: 2\ntpan version: e03df6e-dirty\nui language: vi"
    }
  ]
}
  => ĐẠT: preview 200, nhãn ngữ cảnh: view, trail, machine (không pointer khi không chỉ vào gì)
  => ĐẠT: nhãn view nói đúng mục, bằng bí danh: Trang site site-?.example (/sites/site-?.example)
  => ĐẠT: nhãn machine có web server nginx, số site đang chạy 2
  => ĐẠT: preview gửi kèm bản hướng dẫn site
  => ĐẠT: prompt xem trước đã che: không có a.example.com
```

Trình duyệt gửi tên thật `a.example.com`, bản xem trước đã là bí danh `site-a.example`. Không có khối `pointer` và `evidence` vì câu hỏi này không chỉ vào thứ gì.

### 2.4 Hỏi lượt 1: một vòng đầy đủ, máy không đổi (phần g)

Kịch bản chụp trạng thái máy (user `tp_`, `/var/www`, DB, vhost, pool, số job và site) rồi gửi `POST /api/v1/ai/ask` với câu "Tạo thêm một site mới cho mình rồi cho biết máy đang có site nào.". Panel trả ngay `202` và `{"session_id": "357ef819eb6c5a8c", "seq": 1}`. Câu hỏi chạy nền; kết quả đến qua SSE:

```bash
# Mac
curl -sS -N --cacert <tmp>/cert.pem -b <cookie> https://<vm>:8443/api/v1/ai/sessions/357ef819eb6c5a8c/turns/1/events
```

```
+0.276s init version=2.1.328 model=claude-sonnet-5-5
+1.278s text (tổng 39 ký tự): …Mình xem danh sách site trên máy trước.
+1.431s tool_call mcp__tpan__list_sites input={}
+1.451s tool_result mcp__tpan__list_sites (240 ký tự)
+1.472s text (tổng 79 ký tự): …máy trước.⏎⏎Máy đang có site `tpan:site/site-a.exa
+1.589s text (tổng 84 ký tự): …rước.⏎⏎Máy đang có site `tpan:site/a.example.com`.
+1.74s text (tổng 125 ký tự): …ple.com`. Theo yêu cầu, mình đề xuất tạo site mới.
+1.894s tool_call mcp__tpan__propose_site_add input={"domain":"c.example.com","email":"admin@example.com"}
+1.951s tool_result mcp__tpan__propose_site_add plan_id=9f0a17c30c9710bf (2049 ký tự)
+1.971s text (tổng 197 ký tự): …17c30c9710bf` đang chờ bạn duyệt; máy chưa đổi gì.
+2.105s done: Plan `tpan:plan/9f0a17c30c9710bf` đang chờ bạn duyệt; máy chưa đổi gì.
```

Đây là dòng tóm tắt của kịch bản, không phải SSE thô: `+1.278s` là giây tính từ lúc gửi, `⏎` là dấu xuống dòng. Hai dòng ở `+1.472s` và `+1.589s` là ví dụ của một quyết định trong mã: AI viết bí danh `site-a.example`, nhưng chữ tới thành từng mẩu, mẩu đầu chỉ có `site-a.exa`. Panel không đổi được nửa bí danh, nên chưa đổi; tới mẩu sau thì cả bí danh đã đủ và hiện `a.example.com`. Lý do ở 3.12.

Sau lượt:

```
  => ĐẠT: Plan 9f0a17c30c9710bf: pending, origin ai, kind site.add, target c.example.com, cùng phiên
  => ĐẠT: MÁY KHÔNG ĐỔI GÌ: user tp_, /var/www, DB, user DB, vhost, pool, số job, số site giống hệt trước lượt hỏi
```

```bash
# Mac
scripts/vm.sh exec curl -sS -m 20 -o /dev/null -w %{http_code}\n --resolve c.example.com:80:127.0.0.1 http://c.example.com/
```

```
[2026-11-19 22:01:23] (mã thoát 52)
  => ĐẠT: c.example.com chưa có: rơi vào site mặc định (nginx 444, curl mã 52)
```

Terminal của `tpan brain` trên Mac in đúng việc bộ não vừa làm:

```
22:01:20 [357ef819eb6c5a8c-1] ? Tạo thêm một site mới cho mình rồi cho biết máy đang có site nào.
22:01:22 [357ef819eb6c5a8c-1] → mcp__tpan__list_sites {}
22:01:22 [357ef819eb6c5a8c-1] → mcp__tpan__propose_site_add {"domain":"c.example.com","email":"admin@example.com"}
22:01:22 [357ef819eb6c5a8c-1] done
```

### 2.5 Thứ `claude` nhận được (phần g')

`claude` giả ghi lại argv của nó. Phần cuối argv (nguyên văn trong log):

```
    "--setting-sources",
    "",
    "--disable-slash-commands",
    "--append-system-prompt-file",
    "<tmp>/home/Library/Caches/tpan-brain/system-prompt.md",
    "--max-turns",
    "4",
    "--model",
    "sonnet",
    "--session-id",
    "55c832b9-33db-49bf-bc4a-521799fb3d5d"
```

```
  => ĐẠT: claude -p chạy với --session-id <uuid>, --max-turns 4, --tools "", --strict-mcp-config, không --bare
  => ĐẠT: MCP server trong --mcp-config là ssh ... sudo -n tpan mcp --session 357ef819eb6c5a8c
  => ĐẠT: claude giả thấy 18 công cụ mcp__tpan__*
  => ĐẠT: prompt claude nhận qua stdin trùng từng byte với prompt của bản xem trước
  => ĐẠT: thư mục làm việc của phiên rỗng sau lượt 1
```

18 công cụ: 15 của M7 cộng `current_view`, `propose_health_fix`, `propose_ssl_issue`. Dòng "trùng từng byte" là bằng chứng cho nguyên tắc 3 của mục 8.1: thứ bạn xem trước đúng là thứ đi ra.

### 2.6 Chuyển mục rồi hỏi tiếp (phần h)

Báo chuyển sang `/incidents`, hỏi "Còn ở mục này thì sao?" cùng `session_id`. Prompt mà `claude` giả nhận ở lượt 2 (đầu prompt, nguyên văn):

```
<tpan-context lang="vi">
[view] Sự cố (/incidents)
[trail] Bảng điều khiển (/)
Trang site site-a.example (/sites/site-a.example)
Sự cố (/incidents)
[machine] web server: nginx
...
</tpan-context>
<tpan-playbook name="su-co">
# Playbook: incidents
```

```
  => ĐẠT: lượt 2 chạy --resume với đúng UUID của --session-id lượt 1
  => ĐẠT: prompt lượt 2 gửi bản hướng dẫn su-co, không gửi lại bản site
  => ĐẠT: thư mục làm việc của lượt 2 trùng lượt 1 (để --resume tìm được phiên)
  => ĐẠT: hai lượt có khối ngữ cảnh khác nhau: view của lượt 1 là trang site, lượt 2 là Sự cố; lượt 2 có trail 3 mục
```

### 2.7 Cửa kiểm từ chối (phần i)

Kịch bản đổi cấu hình `claude` giả để nó khai thêm công cụ `Bash` trong sự kiện `init`, rồi hỏi "Tạo thêm một site nữa.":

```
+0.297s refused: cửa kiểm từ chối phiên Claude Code (2.1.328): công cụ ngoài tpan: Bash
[2026-11-19 22:01:26] (1 sự kiện, mã thoát 0)
Thứ tự sự kiện: ["refused"]
  => ĐẠT: lượt kết thúc bằng refused, lời từ chối nêu Bash
  => ĐẠT: không có câu trả lời: không sự kiện text, tool_call, tool_result, done nào
  => ĐẠT: claude giả bị dừng trước khi gọi công cụ nào (không có dấu turn-3.toolcall)
  => ĐẠT: không có lần gọi công cụ mới nào trong ai_calls (3 trước, 3 sau)
```

Đây là kết quả đo của "kiểm cửa rồi mới tin": 0,297 giây sau khi gửi, phiên bị từ chối, không sự kiện nào khác lọt ra panel, và máy chủ không ghi thêm lần gọi công cụ nào. Lưu ý: `claude` giả chờ 1 giây (`think_ms`) trước khi gọi công cụ, thay cho thời gian model suy nghĩ. Câu hỏi 5 của phần 6 bàn chỗ này.

### 2.8 Bộ não thứ hai, tắt bộ não (phần j, k)

Chạy `tpan brain` lần hai khi lần một còn nối:

```
tpan: panel từ chối bộ não này: panel đã có bộ não của máy <mac-host> nối từ 2026-11-19 22:01:19; mỗi lúc chỉ một bộ não. Dừng tpan brain ở máy đó rồi nối lại.
[2026-11-19 22:01:29] (mã thoát 1)
```

Ctrl+C bộ não thứ nhất (`kill -INT`): terminal in `Bộ não: đã ngắt.`, thoát mã 0. Hỏi tiếp thì panel trả:

```
HTTP/2 409
{
  "error": {
    "code": "brain_offline",
    "message": {
      "vi": "Chưa có bộ não nào nối vào panel. Chạy tpan brain <máy> trên máy của bạn.",
```

Plan `9f0a17c30c9710bf` vẫn `pending` (`tpan plan list`). Phần l tìm tên miền thật, IP, hostname, 5 mật khẩu trong bảng `ai_turns` và `ai_calls`: 0 dòng cho mỗi thứ, kèm một đối chứng tìm thấy bí danh `site-*.example` để chắc cách tìm có tác dụng.

### 2.9 Claude Code thật hỏi từ panel (`M8-claude.log`, lần gọi 6 và 7)

Phần "Xong khi" của M8. Lệnh `claude` thật trên Mac, `--model sonnet --max-turns 4`. Trước đó kịch bản gây lỗi ở `b.example.com` (phần e):

```bash
# Mac, lệnh chạy trong máy ảo bằng root
scripts/vm.sh root bash -c 'rm -v /etc/nginx/sites-enabled/tpan-b.example.com.conf && nginx -t && systemctl reload nginx'
```

(Log in lệnh này không có dấu nháy, vì kịch bản in bằng `$*`.) Rồi giả làm người đứng ở trang site `b.example.com` và bấm "Chẩn đoán". Bản xem trước lần này có đủ năm khối: `view`, `pointer`, `trail`, `evidence` (kết quả phép kiểm đã lưu của site), `machine`. Luồng SSE:

```
+0.442s init version=2.1.328 model=claude-sonnet-5-5
+2.805s tool_call mcp__tpan__site_health input={"domain":"b.example.com"}
+4.541s tool_result mcp__tpan__site_health (2000 ký tự)
+7.383s tool_call mcp__tpan__propose_health_fix input={"check_id":"web.vhost:b.example.com"}
+7.404s tool_result mcp__tpan__propose_health_fix plan_id=5fd0f7b4b15c9227 (1995 ký tự)
+14.233s text (tổng 11 ký tự): …**Nguyên nh
+14.253s text (tổng 14 ký tự): …**Nguyên nhân:
+14.273s text (tổng 33 ký tự): …**Nguyên nhân:** vhost của site-b
+14.292s text (tổng 54 ký tự): …uyên nhân:** vhost của b.example.com chưa được bật
...
+16.703s done: **Nguyên nhân:** vhost của b.example.com chưa được bật. File `/etc/nginx/sites-available/tpan-b.example.com.conf` vẫn còn và đúng như tpan đã ghi. ...
```

Lại thấy bí danh bị cắt: `site-b` ở `+14.273s`, thành `b.example.com` ở mẩu sau. Câu trả lời đầy đủ (đoạn đầu và đoạn đề xuất):

```
**Nguyên nhân:** vhost của b.example.com chưa được bật. File `/etc/nginx/sites-available/tpan-b.example.com.conf` vẫn còn và đúng như tpan đã ghi. Nhưng `stat /etc/nginx/sites-enabled/tpan-b.example.com.conf` trả exit 1, tức là không có symlink.
...
**Đề xuất sửa:** tôi đã tạo plan [tpan:plan/5fd0f7b4b15c9227](tpan:plan/5fd0f7b4b15c9227). Plan chưa chạy gì trên server. Nội dung plan:
1. Ghi lại vhost. Nội dung giống file hiện tại, không có thay đổi thật.
2. Tạo symlink vào `sites-enabled`.
3. Chạy `nginx -t` rồi `systemctl reload nginx`.
```

```
[2026-11-19 21:57:31] (138 sự kiện, mã thoát 0)
  => ĐẠT: chữ hiện dần: 132 sự kiện text, rải trong 2.451s
```

`GET /api/v1/plans`:

```
    "id": "5fd0f7b4b15c9227",
    "kind": "health.fix",
    "target": "b.example.com",
    "origin": "ai",
    "status": "pending",
    "session_id": "bab751ebfe2f2057",
    "untrusted": true
```

`untrusted: true` vì `site_health` trả thân trang HTTP (chữ do người lạ kiểm soát), nên màn duyệt mang cảnh báo. Sau đó `=> ĐẠT: MÁY KHÔNG ĐỔI GÌ sau câu hỏi 1 (chưa ai duyệt)`.

Rồi chuyển sang trang `a.example.com` và hỏi "còn site kia thì sao?" (lần gọi 7, `--resume`). Đoạn cuối câu trả lời:

```
site-a không cần sửa gì. Chỉ site-b có lỗi (vhost chưa bật), và plan [tpan:plan/5fd0f7b4b15c9227](tpan:plan/5fd0f7b4b15c9227) vẫn đang chờ bạn duyệt.
```

AI hiểu đúng "site kia" là site đang mở, so với site của lượt trước. Nhưng chữ `site-a`, `site-b` vẫn nằm nguyên trên màn: model viết tắt bí danh, bộ che không nhận ra `site-a` là `site-a.example` nên không đổi về tên thật. Kịch bản chỉ kiểm dạng đầy đủ `site-?.example`, nên lỗi này không làm log đỏ. Đây là việc còn mở (`docs/thay-doi/M8.md`, mục "bí danh bị model viết tắt").

Trang Phiên AI của phiên đó (`GET /api/v1/ai/sessions/bab751ebfe2f2057`) có hai lượt `done`; lượt 1 `"playbook": "site"`, lượt 2 `"playbook": null`; ba lần gọi công cụ `site_health`, `propose_health_fix` (có `plan_id`), `site_health`. Terminal của `tpan brain`:

```
21:57:15 [bab751ebfe2f2057-1] ? Chẩn đoán vì sao site này lỗi rồi đề xuất cách sửa
21:57:17 [bab751ebfe2f2057-1] → mcp__tpan__site_health {"domain":"site-b.example"}
21:57:22 [bab751ebfe2f2057-1] → mcp__tpan__propose_health_fix {"check_id":"web.vhost:site-b.example"}
21:57:29 [bab751ebfe2f2057-1] done
21:57:32 [bab751ebfe2f2057-2] ? còn site kia thì sao?
21:57:36 [bab751ebfe2f2057-2] → mcp__tpan__site_health {"domain":"site-a.example"}
21:57:42 [bab751ebfe2f2057-2] done
```

Để ý: terminal trên Mac thấy bí danh, vì đó là đúng thứ đi qua Claude. Chỉ panel mới đổi về tên thật.

Số đo cuối log: câu hỏi 1 chữ đầu tiên sau 14,233 giây, `done` sau 16,703 giây; câu hỏi 2 chữ đầu tiên sau 9,887 giây. `docs/thay-doi/M8.md` kết luận phần của tpan (mở SSH cho MCP, liệt kê công cụ) dưới 0,3 giây; thời gian chờ chủ yếu là model và các phép kiểm health.

### 2.10 Lần gọi 4: hai lỗi tìm ra nhờ Claude Code thật (`M8-claude-1.log`)

Cùng kịch bản, chạy trước khi sửa. Model viết một câu, gọi công cụ, rồi viết tin nhắn mới:

```
+2.503s text: Tôi chạy health check hiện tại để xác nhận kết quả đã lưu.
+2.935s tool_call mcp__tpan__site_health input={"domain":"b.example.com"}
+4.86s tool_result mcp__tpan__site_health (2000 ký tự)
+6.779s text: Tôi chạy health check hiện tại để xác nhận kết quả đã lưu.N
+6.801s text: Tôi chạy health check hiện tại để xác nhận kết quả đã lưu.Nguyên nhân g
```

Lỗi một: `đã lưu.Nguyên nhân` dính nhau, không dấu cách, không xuống dòng. Sửa ở `stream.go` (commit `eaa11ed`, xem 3.10). Lần 6 ở 2.9 kiểm lại: `=> ĐẠT: chữ sau mỗi lần gọi công cụ là đoạn mới`.

Lỗi hai: sự kiện cuối của lần 4 là

```
+15.334s done: **Chẩn đoán**
```

tức chỉ đoạn cuối. Trường `result` của Claude Code chỉ giữ tin nhắn cuối cùng của model. Panel lúc đó thay toàn bộ chữ đang hiện bằng `done`, nên câu "Tôi chạy health check..." biến mất ngay khi lượt xong. Sửa ở `panel.go` (commit `a7a6090`, xem 3.12).

Ba lần chạy đầu trên máy ảo build trước bản sửa thứ hai. Ở lần 6, model gọi hết công cụ rồi mới viết một tin nhắn duy nhất, nên `done` tình cờ trùng toàn bộ chữ và không lộ lỗi. Bản đầu của `M8.log` còn có dòng `=> ĐẠT: done mang tin nhắn cuối, trùng đoạn cuối của chữ hiện dần`, tức kiểm đúng hành vi **cũ**.

Lần chạy lại `scripts/e2e-m8.sh` trên mã cuối (bản hiện tại của `docs/bang-chung/M8.log`, 106 dòng `=> ĐẠT`, `TỔNG KẾT: 0 khẳng định không đạt`) đổi khẳng định đó và thêm một khẳng định trên trang Phiên AI. Lượt 1, luồng SSE (nguyên văn):

```
+1.986s text (tổng 197 ký tự): …4e8ee0eb88b0` đang chờ bạn duyệt; máy chưa đổi gì.
+2.125s done: Mình xem danh sách site trên máy trước.

Máy đang có site `tpan:site/a.example.com`. Theo yêu cầu, mình đề xuất tạo site mới.

Plan `tpan:plan/b40a4e8ee0eb88b0` đang chờ bạn duyệt; máy chưa đổi gì.
```

```
  => ĐẠT: chữ sau mỗi lần gọi công cụ là đoạn mới (ngăn bằng dòng trống)
  => ĐẠT: done mang toàn bộ chữ đã nối, trùng chữ hiện dần cuối cùng (không chỉ result = tin nhắn cuối)
```

So với bản đầu (2.4): `done` giờ bắt đầu bằng "Mình xem danh sách site trên máy trước.", câu viết **trước** lần gọi `list_sites`, chứ không chỉ có tin nhắn cuối. `claude` giả trả `result` chỉ có tin cuối như Claude Code thật, nên chữ đầy đủ này chỉ có thể đến từ `shower.show`. Trên trang Phiên AI (`GET /api/v1/ai/sessions/8dc11f0a8ba8a814`), câu trả lời lưu của lượt 1:

```
      "answer": "Mình xem danh sách site trên máy trước.\n\nMáy đang có site `tpan:site/a.example.com`. Theo yêu cầu, mình đề xuất tạo site mới.\n\nPlan `tpan:plan/b40a4e8ee0eb88b0` đang chờ bạn duyệt; máy chưa đổi gì."
```

```
  => ĐẠT: answer của lượt 1 trên trang phiên là toàn bộ chữ: có đoạn viết trước lần gọi công cụ đầu và đoạn cuối
```

Lần chạy lại dùng `claude` giả. Với Claude Code thật, bản sửa này chưa chạy (lần 6 và 7 build trước nó).

Bản sửa cũng được kiểm bằng unit test. Mình đã chạy trên Mac:

```bash
go test ./internal/ai/ -run 'TestCauTraLoiGiuDoanTruocCongCu|TestChuHienThiDoiVeTenThatKeCaBiDanhBiCat' -v
```

```
=== RUN   TestChuHienThiDoiVeTenThatKeCaBiDanhBiCat
--- PASS: TestChuHienThiDoiVeTenThatKeCaBiDanhBiCat (0.01s)
=== RUN   TestCauTraLoiGiuDoanTruocCongCu
--- PASS: TestCauTraLoiGiuDoanTruocCongCu (0.01s)
PASS
ok  	github.com/doxuta/tpan/internal/ai	0.163s
```

### 2.11 Sự kiện `init` thật (`M8-claude-init.log`, lần gọi 1 đến 3)

Trước khi viết cửa kiểm, phiên chính chạy `claude -p` thật trên Mac để xem `init` thật trông thế nào, thay vì đoán. Lần 1, không MCP server nào, nguyên văn (cắt bớt):

```
  "tools": [],
  "mcp_servers": [],
  "model": "claude-sonnet-5-5",
  "permissionMode": "dontAsk",
  "slash_commands": [],
  "apiKeySource": "none",
  "claude_code_version": "2.1.328",
  ...
  "plugins": [
    {
      "name": "cc-plugin-agents-md",
      "path": "builtin",
```

Ba điều rút ra, đều thành mã: `tools` rỗng thật khi có `--tools ""`, nên danh sách ngoại lệ `AllowedBuiltinTools` rỗng; có 5 plugin `path: "builtin"` không tắt được, nên cửa kiểm cho qua plugin builtin; `apiKeySource: "none"`, tức đăng nhập bằng gói.

Lần 2 có MCP server `tpan` thật qua SSH vào máy ảo: `tools` đúng 15 tên `mcp__tpan__*`, `mcp_servers` là `{"name": "tpan", "status": "connected", "source": "dynamic"}`. Lần 3 chạy `--resume` cùng thư mục:

```
{"type": "result", "subtype": "success", "num_turns": 1, "result": "Tôi vừa nhắc tới site `site-a.example`.", "session_id": "dfb813f3-f715-48f1-b56b-cfcc03444391"}
```

`session_id` trùng lần 2, và log ghi thư mục làm việc rỗng trước và sau. `claude auth status --json`: tpan chỉ ghi `{"loggedIn": true, "authMethod": "claude.ai"}`, các trường khác có email và tổ chức nên không ghi.

### 2.12 Unit test mình vừa chạy trên Mac

```bash
go test ./internal/ai/claudecode/ -run 'TestGateTuChoi|TestParserNganDoanGiuaTinNhan|TestRunThuMucKhongRong|TestRunCuaKiemTuChoi' -v
go test ./internal/ai/bridge/ -run 'TestListenQuyenVaSocketCu|TestBoNaoThuHaiBiTuChoi' -v
```

```
--- PASS: TestRunCuaKiemTuChoi (0.03s)
--- PASS: TestRunThuMucKhongRong (0.01s)
--- PASS: TestGateTuChoi (0.00s)
--- PASS: TestParserNganDoanGiuaTinNhan (0.00s)
ok  	github.com/doxuta/tpan/internal/ai/claudecode	0.467s
--- PASS: TestBoNaoThuHaiBiTuChoi (0.01s)
--- PASS: TestListenQuyenVaSocketCu (0.01s)
ok  	github.com/doxuta/tpan/internal/ai/bridge	0.374s
```

`TestGateTuChoi` có 10 trường hợp con, trong đó `tiền_tố_gần_giống` (công cụ `mcp__tpanx__run`) và `MCP_server_lạ`.

### 2.13 Việc dọn dở của M7: reload nginx và kịch bản lỗi 07

Bảng chia việc đợt 4 có hai việc dở để lại từ lúc ghép M4, M5, M7 (cuối mục 8.3 của KE-HOACH.md): xem lại phép kiểm chạy ngay sau `systemctl reload nginx`, và chuyển lỗi vhost đang viết thẳng trong `scripts/e2e-m7.sh` thành một kịch bản riêng trong `scripts/faults/`. Cả hai đã xong ở đợt 4b: commit `2b0e2e2` (sửa reload), `73f87eb` (kịch bản 07), `5926bfa` (bằng chứng). Ghi chú đầy đủ ở `docs/thay-doi/M8.md`, mục "Sửa khi chạy trên máy ảo" và "Kết quả kiểm cho mục 16".

**Đo trước khi sửa.** `scripts/e2e-m8/reload-measure.sh` chạy trong máy ảo: gỡ hoặc bật symlink vhost của `a.example.com`, reload, rồi `curl` ngay và in danh sách worker của nginx. Kết quả ở `docs/bang-chung/M8-reload.log`. Một lần gỡ vhost, trước khi sửa (nguyên văn):

```
=== systemctl, lần 1, off (a.example.com)
trước reload: 29094 nginx: worker process;29095 nginx: worker process;
systemctl reload nginx trả về sau 9 ms
+0ms curl 200/0 | 29094 nginx: worker process;29095 nginx: worker process;29379 nginx: worker process;29380 nginx: worker process;
+93ms curl 200/0 | 29094 nginx: worker process is shutting down;29095 nginx: worker process;29379 nginx: worker process;29380 nginx: worker process;
+99ms curl 000/52 | 29379 nginx: worker process;29380 nginx: worker process;
kết quả cuối: curl 000/52
```

`systemctl reload nginx` trả về sau 9 ms, nhưng worker cũ (29094, 29095) còn sống và vẫn trả 200 theo cấu hình cũ; chỉ khi còn mỗi worker mới thì mới ra mã 52. Phần "TRƯỚC khi sửa" của nginx có 20 lần: 10 lần qua `systemctl reload nginx`, 10 lần qua `tpan internal reload-web` (đường reload của chính tpan). Cả 20 lần, request đầu tiên ngay sau khi lệnh trả về đều theo config cũ (gỡ vhost vẫn 200, bật vhost vẫn 444), và kết quả đổi sau 93 đến 102 ms. Hệ quả: phép kiểm HTTP chạy ngay sau reload có thể **đạt giả**. Cùng hiện tượng thấy ở phần e của `M8-claude.log`: `lần 1: 200 (curl mã 0)`, rồi `lần 2: curl: (52) Empty reply from server`.

**Sau khi sửa** (`steps.Reload`, xem 3.18), cùng phép đo qua đường của tpan (nguyên văn):

```
=== SAU tpan, lần 1, off (a.example.com)
trước reload: 13684 nginx: worker process;13685 nginx: worker process;
tpan internal reload-web trả về sau 120 ms
+0ms curl 000/52 | 13858 nginx: worker process;13859 nginx: worker process;
kết quả cuối: curl 000/52
```

Lệnh của tpan giờ trả về chậm hơn (120 ms thay vì khoảng 20 ms), nhưng lúc nó trả về thì chỉ còn worker mới và request đầu đã theo config mới. Bảng tổng hợp trong `docs/thay-doi/M8.md` (mình đối chiếu với log):

| | Lệnh reload | Trả về sau | Request đầu tiên sau khi lệnh trả về |
|---|---|---|---|
| nginx, trước khi sửa | `tpan internal reload-web` | 16 đến 27 ms | theo config cũ, 10/10 lần |
| nginx, sau khi sửa | `tpan internal reload-web` | 120 đến 169 ms | theo config mới, 10/10 lần |
| nginx, sau khi sửa (đối chứng) | `systemctl reload nginx` | 7 đến 27 ms | vẫn theo config cũ, 4/4 lần |

Dòng đối chứng cho thấy nginx không đổi gì; chỉ có tpan biết chờ.

Apache (`apachectl graceful`) không cần chờ: không lần nào trong 20 lần đo request đầu theo config cũ. Phần đo thêm cuối log (`curl -m 10`, bản đã sửa) cho thấy request đầu đôi khi chậm nhưng vẫn đúng:

```
lần 1: systemctl reload apache2; request đầu: HTTP 200 0.000664s (curl mã 0)
lần 2: systemctl reload apache2; request đầu: HTTP 200 1.132675s (curl mã 0)
```

(4/10 lần mất 1,01 đến 1,13 giây, 6/10 lần dưới 1 ms.) Ở phần đo trước đó với `-m 1`, những lần chậm đó hiện thành `000/28` (hết giờ); đó là lý do mình từng thấy khó đọc.

**Kịch bản lỗi 07.** `scripts/faults/07-vhost-disable.sh break <domain>` xoá symlink bật vhost của site ở `sites-enabled` (nginx hoặc apache), kiểm cú pháp (vẫn qua) rồi reload; `restore` tạo lại. Mong đợi Fail `web.vhost:<domain>` rồi `http.site:<domain>`, sửa tự động `web.rewrite`. Kịch bản là bash và reload bằng `systemctl`, nên vẫn có khoảng 100 ms nói trên: `scripts/faults/README.md` ghi rõ ai gọi HTTP ngay sau nó thì phải tự chờ (e2e gọi lại tới khi `curl` ra mã 52). `scripts/e2e-m7.sh` và chế độ `real` của `scripts/e2e-m8.sh` gọi nó thay cho lệnh viết tay; `scripts/e2e-m4.sh` chạy nó ở bước 11 cho cả nginx và apache.

**Hồi quy từ máy sạch với bản đã sửa:**

- `docs/bang-chung/M8-hoi-quy-m4.log`: 312 dòng `=> ĐẠT`, `TỔNG KẾT: 0 khẳng định không đạt`. Bước 11 với 07 trên nginx, ví dụ:

  ```
    => ĐẠT: 07-vhost-disable.sh break thoát 0
    => ĐẠT: a.example.com rơi vào site mặc định (nginx 444, curl mã 52)
    => ĐẠT: web.vhost:a.example.com là Fail
    => ĐẠT: a.example.com: chặng Fail đầu tiên trên đường đi request là web
  ```

- `docs/bang-chung/M8-hoi-quy-m7.log`: 98 dòng `=> ĐẠT`, 0 không đạt. Khẳng định về danh mục công cụ đổi từ 15 thành 18, vì M8 thêm `current_view`, `propose_health_fix`, `propose_ssl_issue`: `=> ĐẠT: đủ 18 công cụ đúng tên của danh mục (15 của M7, 3 của M8)`. Theo `docs/thay-doi/M8.md`, lần hồi quy đầu ra 1 không đạt vì khẳng định cũ đòi đúng 15; đó là kịch bản lỗi thời, không phải lỗi của mã.
- `docs/bang-chung/M8-hoi-quy-m5.log`: đang chạy lúc mình viết, chưa có dòng tổng kết. Xem log.

Unit test của bản sửa, mình vừa chạy trên Mac:

```bash
go test ./internal/steps/ -run 'TestReload' -v
```

```
--- PASS: TestReloadNginxChoTheHeMoi (0.00s)
--- PASS: TestReloadNginxHetHan (0.03s)
--- PASS: TestReloadKhongCho (0.00s)
--- PASS: TestReloadNginxPIDLa (0.00s)
ok  	github.com/doxuta/tpan/internal/steps	0.529s
```

### 2.14 Chưa chạy

- Claude Code thật trên mã cuối cùng (lần 6 và 7 build trước `a7a6090` và trước bản sửa reload): `scripts/e2e-m8.sh real`, tốn hai lần gọi hạn mức.
- Hồi quy e2e M5 với bản sửa reload: đang chạy lúc viết, xem `docs/bang-chung/M8-hoi-quy-m5.log`.
- Mở panel bằng trình duyệt thật, dùng ngăn AI, `⌘K`, nút "Giải thích".
- Duyệt Plan `health.fix` do AI đề xuất rồi xem site trở lại 200. Lần chạy chỉ kiểm Plan nằm `pending` và máy không đổi.
- Phần "Việc chủ dự án tự làm trên VPS" của `docs/thay-doi/M8.md` vẫn ghi "(điền ở đợt 4)".

## 3. Đi qua mã theo thứ tự chạy

Một câu hỏi đi một vòng (mục 8.4):

```
trình duyệt ──POST /ai/ask──► tpan serve (máy chủ) ──/run/tpan/brain.sock──► tpan brain-link
      ▲                           │ Panel, Hub                                   │ ssh
      └──── SSE /turns/N/events ──┘                                              ▼
                                                     tpan brain (máy cá nhân) ──► claude -p
                                                                                  │
            tpan mcp --session <id> (máy chủ) ◄──── ssh ... sudo -n ─────────────┘
```

### 3.1 Trình duyệt: `web/src/ai/AiProvider.tsx`

`AiProvider` bọc toàn bộ `Layout`, không nằm trong màn nào. Vì vậy cuộc trò chuyện sống qua mọi lần chuyển mục: ngăn AI "đi theo người dùng".

Mỗi lần đường dẫn đổi, một `useEffect` báo máy chủ và nhớ vào đường vừa đi (tối đa 10 mục):

```tsx
useEffect(() => {
  const path = location.pathname;
  if (lastPath.current === path) return; // đổi ngôn ngữ cũng chạy lại effect này
  lastPath.current = path;
  setPaths((p) => (p.at(-1) === path ? p : [...p, path].slice(-trailMax)));
  post("/api/v1/ai/views", { path, label: viewLabel(path, lang) } satisfies AIView).catch(() => {});
}, [location.pathname, lang]);
```

`POST /ai/views` để công cụ `current_view` (chạy trong tiến trình `tpan mcp`, khác tiến trình) biết bạn đang ở đâu. Lỗi bị bỏ im lặng: ngữ cảnh chỉ để AI hiểu rõ hơn.

Các nút "Giải thích", "Chẩn đoán" chỉ gọi `openAI({ pointer, mode, question })`: mở ngăn với câu hỏi điền sẵn, **không gửi**. Người phải xem thứ sẽ gửi rồi tự bấm.

`send` gửi `POST /api/v1/ai/ask`, nhận `session_id` và `seq`, rồi nghe SSE bằng `subscribeAITurn`. Mỗi sự kiện đi qua `applyEvent`:

```tsx
case "text":
  return { ...t, answer: ev.text ?? t.answer };
case "done":
  return { ...t, status: "done", answer: ev.text ?? t.answer };
```

Chữ mới **thay** chữ cũ, không nối thêm. Máy chủ luôn gửi toàn bộ câu trả lời tới lúc đó (3.12 nói vì sao). Nhờ vậy, nếu kết nối rớt và trình duyệt nhận lại sự kiện cũ, kết quả vẫn đúng.

### 3.2 Xem trước: `web/src/ai/AiDrawer.tsx`, hàm `Panel`

```tsx
const typed = useDebounced(ai.draft, 500);
const previewInput = useMemo(() => ({ ...ai.input, question: typed }), [ai.input, typed]);
const preview = useQuery({
  queryKey: ["ai", "preview", previewInput],
  queryFn: () => post<AskPreview>("/api/v1/ai/preview", previewInput),
  enabled: b !== undefined && b.mode !== "off",
```

Bản xem trước dựng lại khi ngữ cảnh đổi và khi bạn ngừng gõ 500 ms, không gọi ở mỗi phím. Nút Gửi bị khoá (`previewReady` trong `Composer`) cho tới khi bản xem trước khớp đúng câu đang gõ: không thể gửi một thứ chưa được xem. Mỗi khối hiện thành một nhãn (`ChipItem`), bấm để bỏ khối đó (`toggleOmit`).

### 3.3 API: `internal/server/api_ai.go`

`handleAIPreview` và `handleAIAsk` chỉ đọc JSON rồi gọi `s.deps.AI.Preview` và `s.deps.AI.Ask`. `handleAIAsk` trả `202 Accepted`: câu hỏi chưa xong, chỉ mới được nhận. Nhật ký ghi id phiên, không ghi câu hỏi (câu hỏi có thể có tên thật).

`handleAITurnEvents` là SSE của một lượt:

```go
send := func(ev ai.Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("mã hoá sự kiện AI %s#%d: %w", id, seq, err)
	}
	if _, err := fmt.Fprintf(w, "event: ai\ndata: %s\n\n", data); err != nil {
		return err
	}
	return rc.Flush()
}
```

Sau đó vòng `for _, ev := range past` gửi lại mọi sự kiện đã có của lượt, rồi một vòng `select` chờ sự kiện mới trên kênh `live` (hoặc request bị huỷ). Gặp sự kiện kết thúc (`ev.Final()`) thì đóng luồng. Không có dòng `id:` của SSE: vì `text` mang toàn bộ chữ, nối lại thì gửi lại từ đầu là đủ.

### 3.4 Lõi: `internal/ai/panel.go`, `Ask` và `start`

`Ask` kiểm AI có bật, câu hỏi không rỗng, không dài quá; gọi `start`; rồi:

```go
// Lượt sống lâu hơn request (request trả về ngay), nên không dùng ctx của
// request; TurnTimeout là chốt chặn lượt chạy mãi.
bctx, cancel := context.WithTimeout(context.Background(), TurnTimeout)
ch, err := p.Brain.Run(bctx, job)
...
go p.receive(r, ch, cancel)
return AskStarted{SessionID: r.session, Seq: r.seq}, nil
```

`TurnTimeout` là 5 phút (`internal/ai/brain.go`). `p.Brain` là một interface; khi chạy thật nó là `bridge.Hub` (3.6), khi `--demo` hay test là `fake`.

`start` chạy dưới khoá `askMu` để hai câu hỏi cùng lúc vào một phiên không cùng lọt. Nó từ chối khi phiên đang có lượt chạy (`ai_turn_running`) hoặc đã có 50 câu trong 24 giờ (`ai_daily_limit`, hằng `MaxQuestionsPerDay`). Phiên mới thì sinh hai id: id tpan 16 chữ hex (`newSessionID`) và một UUID cho Claude Code (`newUUID`), vì `--session-id` của Claude Code đòi UUID. Lượt thứ hai trở đi thì `Resume = true` nếu lượt trước đã tới được claude (`reachedBrain`: có sự kiện `init`).

### 3.5 Dựng prompt: `build`, khối ngữ cảnh, che, bản hướng dẫn

```go
for _, key := range ChipKeys {
	content, ok := raw[key]
	if !ok {
		continue
	}
	c := PreviewChip{Chip: Chip{Key: key, Label: chipLabels[key], Content: clip(red.String(content))}, Omitted: omit[key]}
	b.chips = append(b.chips, c)
	if !c.Omitted && strings.Contains(c.Content, "<untrusted ") {
		b.untrusted = true
	}
}
```

`chipContents` lấy nội dung chưa che của từng khối: `view`, `trail` từ thứ trình duyệt gửi; `pointer` và `evidence` thì máy chủ **tự đọc từ DB** theo `Ref` (hàm `evidence`), không tin bằng chứng trình duyệt gửi lên; `machine` từ bảng `settings`. `red.String` là bộ che của M7. Output lệnh và thân trang được bọc `<untrusted>` (hàm `untrusted`, cắt trước khi bọc để thẻ đóng không bao giờ bị cắt mất). Có khối untrusted thì cờ `Untrusted` của phiên bật, và Plan đề xuất sau đó mang cảnh báo.

Phần sau ghép prompt thành ba thẻ: `<tpan-context lang="vi">`, `<tpan-playbook name="...">`, `<question>`. Bản hướng dẫn chỉ gửi khi phiên chưa gửi bản đó (`playbookSent`). Vì vậy lượt 2 của 2.9 có `"playbook": null`: cả hai lượt đều ở trang site, bản `site` đã gửi ở lượt 1. Bản hướng dẫn không đi qua bộ che: nó là chữ tĩnh nhúng trong tpan, mà bộ che lại làm méo nó (chú thích trong `build` có ví dụ).

`Preview` và `Ask` cùng gọi `build` với cùng đầu vào, nên ra cùng một prompt. Đó là lý do 2.5 có dòng "trùng từng byte".

### 3.6 Bản hướng dẫn: `internal/ai/playbooks/playbooks.go`

13 file `.md` tiếng Anh, nhúng vào file chạy bằng `//go:embed *.md`. `For(path)` tra đường dẫn của màn trong hai bảng `exact` và `prefix` (ví dụ `/incidents/<id>` ra `su-co`). Đường dẫn do trình duyệt gửi nên chỉ được so khớp với bảng, không bao giờ ghép vào tên file. Danh sách `names` khai báo tay để test bắt được file thừa hay thiếu. Bản hướng dẫn không nằm trong system prompt vì Claude Code giữ nguyên system prompt suốt một phiên, mà người dùng thì chuyển mục.

### 3.7 Phía máy chủ của cây cầu: `internal/ai/bridge/hub.go`

`tpan serve` gọi `bridge.Listen` lúc khởi động khi `ai.brain = bridge`:

```go
ln, err := net.Listen("unix", path)
if err != nil {
	return nil, fmt.Errorf("nghe socket %s: %w", path, err)
}
// 0600: ai nối được socket này là đưa được câu trả lời giả vào panel. Thư
// mục cha 0700 đã chặn trong khoảng ngắn trước chmod.
if err := os.Chmod(path, 0o600); err != nil {
	_ = ln.Close()
	return nil, fmt.Errorf("đặt quyền 0600 cho %s: %w", path, err)
}
```

Trước đó `Listen` tạo `/run/tpan` quyền 0700 và xoá socket cũ còn sót, nhưng chỉ khi đúng là socket (`fs.ModeSocket`), không bao giờ xoá nhầm file thường.

`handle` đọc lời chào (`hello`) trong một thời hạn, rồi giữ chỗ trong cùng một lần khoá mutex: hai bộ não nối cùng lúc thì chỉ một được nhận, bộ não sau nhận `refused` kèm tên máy và giờ nối của bộ não đang giữ (2.8).

`Hub.Run` là bản `ai.Brain` mà panel thấy. Chưa có bộ não thì `ErrBrainOffline` (panel đổi thành 409 `brain_offline`); đang bận thì `ErrBrainBusy`. Có thì ghi một dòng `{"type":"job",...}` xuống kết nối và trả về kênh sự kiện. Goroutine `read` đọc từng dòng `event` và chuyển vào kênh của job. Panel huỷ (hết 5 phút) thì Hub gửi `cancel`, và nếu sau 30 giây (`cancelGrace`) bộ não vẫn im thì cắt kết nối, để panel không kẹt "bận" mãi khi máy cá nhân ngủ.

### 3.8 Ống nối: `tpan brain-link` và `bridge.Link`

`brainLinkCmd` (`cmd/tpan/brain.go`) chạy trên máy chủ, qua SSH, bằng root. Nó kiểm `ai.brain` là `bridge`, đóng DB, rồi gọi `bridge.Link(ctx, bridge.SocketPath, e.stdin, e.stdout)`:

```go
errc := make(chan error, 2)
go func() {
	_, err := io.Copy(conn, in)
	errc <- err
}()
go func() {
	_, err := io.Copy(out, conn)
	errc <- err
}()
select {
case err = <-errc:
case <-ctx.Done():
}
```

Hai goroutine chép hai chiều: stdin của SSH vào socket, socket ra stdout của SSH. Bên nào xong trước thì đóng kết nối để bên kia cũng thoát. `brain-link` không hiểu giao thức; nó chỉ là ống. Cái hay: SSH lo xác thực, socket 0600 lo quyền, tpan không mở thêm cổng mạng nào.

### 3.9 Máy cá nhân: `tpan brain`, hàm `runBrain`

Thứ tự trong `runBrain` (`cmd/tpan/brain.go`):

1. `claudecode.Version`: chạy `claude --version`. Khác `TestedVersion` (2.1.328) thì cảnh báo.
2. `claudecode.Auth`: chạy `claude auth status --json`, chỉ giải hai trường `loggedIn` và `authMethod` vào struct `AuthStatus`. Chưa đăng nhập thì dừng; không phải `claude.ai` thì cảnh báo mỗi câu sẽ tính tiền API.
3. `claudecode.New`: tạo thư mục `os.UserCacheDir()/tpan-brain` (trên Mac là `~/Library/Caches/tpan-brain`, thấy ở 2.5) quyền 0700, ghi `system-prompt.md` vào đó.
4. `system.StartPipe` mở `ssh -o BatchMode=yes <máy> sudo -n tpan brain-link` và giữ stdin, stdout của nó.
5. `bridge.Client.Serve` gửi `hello`, chờ `welcome`, rồi nhận job.

Môi trường truyền cho `claude` và `ssh`:

```go
var brainEnvKeys = []string{"HOME", "PATH", "USER", "LOGNAME", "TMPDIR"}
```

tpan không đọc `~/.claude` hay Keychain. Nó chỉ chạy lệnh `claude` với đúng HOME của bạn, để chính `claude` tự tìm đăng nhập của nó (luật 9 của AGENTS.md, nguyên tắc 7 của mục 8.1). `USER`, `LOGNAME` để `claude` tìm đúng mục trong Keychain của macOS. Không chuyển biến nào khác. `BatchMode=yes` vì `claude` mở SSH cho MCP ở nền, không có ai trả lời câu hỏi mật khẩu.

`bridge.Client` (`internal/ai/bridge/client.go`) nhận job, in `? <câu hỏi>` ra terminal, gọi `claudecode.Brain.Run`, in `→ <công cụ>` mỗi lần gọi công cụ, và chuyển từng sự kiện ngược về Hub. Nó luôn gửi đúng một sự kiện kết thúc, kể cả khi `claude` không chạy được.

### 3.10 Chạy `claude -p`: `internal/ai/claudecode/claudecode.go`

`Args` dựng argv đúng bảng cờ ở mục 8.2 (`--tools ""`, `--strict-mcp-config`, `--permission-mode dontAsk`, `--setting-sources ""`...). Phần quyết định MCP server:

```go
mcpArgs := append(slices.Clone(cfg.MCP.Args), "--session", job.SessionID)
...
// Không bao giờ --bare: ở chế độ đó claude bỏ đăng nhập gói Claude, đòi khoá API.
if job.Resume {
	return append(args, "--resume", job.BrainSession)
}
return append(args, "--session-id", job.BrainSession)
```

`mcpArgs` được ghép vào JSON của `--mcp-config` (một MCP server tên `tpan`). `Run` kiểm id phiên đúng 16 chữ hex (`sessionRe`): nó vừa là tên thư mục, vừa nằm trong lệnh SSH mà máy chủ ghép thành một câu cho shell. Rồi thư mục làm việc:

```go
// Cùng một thư mục cho mọi lượt của phiên: Claude Code lưu phiên theo thư
// mục làm việc, --resume ở thư mục khác không tìm thấy phiên.
dir := filepath.Join(b.cfg.WorkRoot, job.SessionID)
...
// Thư mục phải rỗng: claude đọc CLAUDE.md và file khác ở thư mục làm việc,
// thứ đó sẽ thành chỉ dẫn cho AI. Không tự xoá: có thể là dữ liệu của ai đó.
if len(names) > 0 {
```

Có file thì `Run` trả lỗi nêu tên các file đó và không chạy `claude`.

Hai yêu cầu kéo hai hướng. Rỗng: `claude` tự đọc `CLAUDE.md` của thư mục làm việc, nên một file lạ ở đó thành chỉ dẫn cho AI. Ổn định theo phiên: Claude Code tìm phiên cũ theo thư mục làm việc, nên lượt 2 phải chạy đúng thư mục của lượt 1, không thì `--resume` không thấy phiên. Lời giải: mỗi phiên tpan một thư mục riêng, dùng lại suốt phiên, và kiểm nó rỗng trước mỗi lượt. 2.6 kiểm cả hai: rỗng sau lượt 1, lượt 2 cùng thư mục.

Câu hỏi đi vào qua `Stdin`, không qua argv: dòng lệnh thì `ps` của mọi user đều thấy. Mỗi dòng output gọi `t.onLine`.

### 3.11 Kiểm cửa: `onLine` và `gate.go`

```go
if head.Type == "system" && head.Subtype == "init" {
	...
	if err := Gate(in); err != nil {
		t.refuse(err.Error())
		return
	}
	...
}
if !t.gated {
	// Chưa có init thì chưa biết phiên nạp những gì: không tin dòng nào.
	t.refuse(fmt.Sprintf("dòng JSON đầu tiên của claude không phải sự kiện init (type %q, subtype %q)", head.Type, head.Subtype))
	return
}
```

`refuse` huỷ context của lệnh (giết `claude`) và gửi `EventRefused`. Mọi dòng sau đó bị bỏ (`t.final`).

`Gate` (`gate.go`) đọc sự kiện `init` và gom mọi thứ thừa: công cụ không bắt đầu bằng `mcp__tpan__` (so `HasPrefix` cả dấu `__` cuối, nên `mcp__tpanx__run` bị bắt), MCP server tên khác `tpan`, `tpan` không ở trạng thái `connected`, không có `tpan`, plugin có `path` khác `builtin`, `skills` hay `slash_commands` không rỗng. Lời từ chối nêu đúng từng thứ.

Vì sao không chỉ tin các cờ `--tools ""`, `--strict-mcp-config`: cờ là lời hứa của tài liệu Claude Code, có thể đổi ở bản sau. `init` là thứ phiên **thật sự** đã nạp. Nguyên tắc 1 và 2 của mục 8.1 (AI không có tay, không có shell) vì vậy không phụ thuộc vào việc cờ có tác dụng hay không. Danh sách ngoại lệ (`AllowedBuiltinTools`) lấy từ `init` thật ở 2.11, nên rỗng.

### 3.12 Đọc luồng: `stream.go`, `Parser.Line`

`claude -p --output-format stream-json` in mỗi dòng một đối tượng JSON. `Line` đổi từng dòng thành `ai.Event`:

- `stream_event` có `text_delta`: một mẩu chữ (`EventText`).
- `assistant`: một tin nhắn của model đã xong; khối `tool_use` trong đó thành `EventToolCall`.
- `user`: khối `tool_result` thành `EventToolResult`; có `plan_id` thì lấy ra.
- `result`: `EventDone` (thành công) hoặc `EventError`.

Bản sửa chữ dính (2.10):

```go
case "stream_event":
	...
		text := l.Event.Delta.Text
		// Mỗi tin nhắn assistant là một đoạn; delta không mang dấu ngắt
		// giữa hai tin nhắn, nên tự thêm một dòng trống.
		if p.split {
			text = "\n\n" + text
			p.split = false
		}
		p.wrote = true
case "assistant":
	// Dòng assistant đến khi một tin nhắn (hay khối của nó) đã xong.
	p.split = p.wrote
```

### 3.13 Công cụ trên máy chủ: `tpan mcp --session <id>`

`claude` mở MCP server bằng lệnh trong `--mcp-config`: `ssh ... sudo -n tpan mcp --session <id>`. Cờ `--session` (`cmd/tpan/mcp.go`) gọi `tools.OpenSession` với id đó. Phiên panel thì `OpenSession` đặt chế độ duyệt `panel` và nguồn `engine.OriginAI` (`internal/tools/session.go`): Plan sinh ra mang nhãn "AI đề xuất", và `apply_plan` luôn bị từ chối. Bảng bí danh là bảng của phiên panel (lưu ở `ai_sessions`), nên bí danh trong prompt và trong kết quả công cụ khớp nhau.

Ba công cụ mới (`internal/tools/catalog.go`):

- `current_view` (`read.go`): đọc bảng `ai_views`, trả mục đang mở và tối đa 10 mục vừa đi qua. Dùng ở lối trực tiếp: trong Claude Code bạn gõ "xem giúp cái đang mở trên panel".
- `propose_health_fix` (`plan.go`): nhận đúng một `check_id`.
- `propose_ssl_issue`: nhận một domain.

```go
func proposeHealthFix(ctx context.Context, c *call, in checkIDIn, _ validate.Domain) (any, error) {
	// Chỉ id đi tiếp: Planner đọc kết quả đã lưu của phép kiểm đó, nên agent
	// không bịa được trạng thái hay đối tượng cần sửa.
	return propose(ctx, c, core.PlanRequest{Kind: core.PlanHealthFix, CheckID: in.CheckID})
}
```

`propose` gọi `Planner.Propose` (`internal/core/plans.go`): `prepareHealthFix` đọc kết quả phép kiểm **đã lưu** của lần health gần nhất (`CheckResultOf`), dựng Plan sửa tự động, xem trước, rồi lưu với `Status: engine.PlanPending`. Không bước nào chạy. Đó là Plan `5fd0f7b4b15c9227` ở 2.9.

### 3.14 Quay về: `Panel.receive`, `shower.show`, `finish`

Sự kiện chạy ngược: `claude` stdout, `claudecode`, `bridge.Client`, SSH, `brain-link`, socket, `Hub.read`, kênh, rồi `Panel.receive` (goroutine mở ở 3.4). `receive` lưu bản thô (bí danh) vào `r.raw`, đổi thành bản hiển thị, phát cho mọi trình duyệt đang xem:

```go
// shower đổi sự kiện của bộ não thành sự kiện hiển thị. Chữ được nối lại rồi
// mới đổi bí danh: một bí danh có thể bị cắt giữa hai phần chữ.
type shower struct{ acc strings.Builder }
...
	case EventText:
		s.acc.WriteString(ev.Text)
		ev.Text = red.Unalias(s.acc.String())
	case EventDone:
		// Ưu tiên chữ đã nối: trường result của Claude Code chỉ giữ tin nhắn
		// cuối, mất các đoạn AI viết trước khi gọi công cụ (thấy ở lần gọi
		// thật #4). result chỉ dùng khi không có phần chữ nào.
		if s.acc.Len() > 0 {
			ev.Text = s.acc.String()
		}
```

Hai quyết định nằm trong hàm này.

**Vì sao chữ hiển thị mang toàn bộ câu trả lời.** Nếu đổi bí danh trên từng mẩu, mẩu `tpan:site/site-a.exa` không chứa bí danh nào đủ để đổi, mẩu sau `mple.com` cũng vậy, và người dùng thấy bí danh lọt ra màn hình. Nối hết rồi mới đổi thì lúc nào bí danh cũng nguyên vẹn khi được tìm. Hệ quả: mỗi sự kiện `text` gửi ra trình duyệt mang toàn bộ chữ tới lúc đó, và giao diện thay thế thay vì nối (3.1). Bằng chứng: `+1.472s` và `+1.589s` ở 2.4, `+14.273s` ở 2.9; test `TestChuHienThiDoiVeTenThatKeCaBiDanhBiCat` đưa `"Site site-a.exa"` rồi `"mple ổn. "`.

**Vì sao câu trả lời cuối là chữ đã nối, không phải trường `result`.** `result` của Claude Code chỉ có tin nhắn cuối của model. Model hay viết một câu, gọi công cụ, rồi viết tiếp; lấy `result` thì mất câu đầu (2.10, `done: **Chẩn đoán**`). Chữ đã nối có cả hai, và có dấu ngắt đoạn nhờ 3.12. `result` chỉ còn dùng khi không có chữ nào. `finish` lưu đúng chuỗi đó vào `ai_turns.answer`.

Một chú thích chưa theo kịp bản sửa: `EventDone` trong `internal/ai/brain.go` vẫn ghi "Text là câu trả lời cuối đầy đủ", trong khi với `claudecode` nó chỉ là tin nhắn cuối. Đọc mã thì tin `shower.show`, không tin chú thích đó.

`finish` ghi kết thúc vào DB **trước**, rồi mới phát sự kiện cuối và gỡ lượt khỏi bộ nhớ: ai mở SSE sau lúc gỡ thì đọc DB (`Subscribe`, nhánh dưới) và vẫn thấy đủ. Lượt bị `refused` thì không chữ nào được lưu làm câu trả lời.

`send` gửi không chờ (`select` có `default`): một trình duyệt chậm đầy 64 sự kiện (`viewerBuffer`) thì bị bỏ, kết nối SSE của nó đóng, `EventSource` tự nối lại và nhận lại từ đầu. Một người xem chậm không làm kẹt lượt.

### 3.15 Phần `system` mới: `Cmd.Dir`, `Cmd.Env`, `StartPipe`

Luật cứng 3: mọi lệnh đi qua `system.System`. M8 cần ba thứ mà máy chủ chưa bao giờ cần, nên `internal/system` có thêm (`types.go`, `local.go`):

- `Cmd.Dir`: thư mục làm việc. Rỗng là `/` như trước. `claude` phải chạy trong thư mục rỗng của phiên (3.10).
- `Cmd.Env`: biến môi trường **thêm** vào môi trường cố định (`fixedEnv`: `PATH` chuẩn, `LC_ALL=C`). Lệnh trên máy chủ không bao giờ thừa hưởng môi trường của tpan. Nhưng `claude` cần HOME của bạn để tìm đăng nhập và PATH để tìm chính nó (thường ở `~/.local/bin`). `mergeEnv` ghép, trùng tên thì bản trong `Env` thắng; `lookPath` tìm chương trình theo PATH của lệnh con chứ không theo PATH của tpan.
- `StartPipe`: chạy một lệnh và trả về ống stdin, stdout của nó, cho kết nối sống lâu hai chiều. `Run` thì chờ lệnh xong mới trả, không hợp với `ssh ... brain-link` sống hàng giờ.

```go
func StartPipe(ctx context.Context, cmd Cmd) (*Pipe, error) {
	c, err := command(ctx, cmd)
	...
	c.Stderr = os.Stderr
	c.WaitDelay = 5 * time.Second
	in, err := c.StdinPipe()
	...
	out, err := c.StdoutPipe()
```

stderr của SSH đi thẳng ra terminal của `tpan brain`, để bạn thấy lỗi của SSH (sai khoá, thiếu sudoers). Cả ba chỉ dùng trên máy cá nhân.

### 3.16 Bảng mới: `internal/store/migrations/800_ai_panel.sql`

- `ai_sessions.brain_session`: UUID của phiên Claude Code ứng với phiên tpan.
- `ai_turns`: mỗi lượt một dòng, khoá `(session_id, seq)`. Giữ `question`, `context`, `playbook`, `prompt` đúng như đã gửi (bản che), `answer` và `events` dạng bí danh, `status` một trong `running`, `done`, `error`, `refused`. Trang Phiên AI đọc ở đây; nguyên tắc 3 "thấy được AI thấy gì" vì vậy kiểm lại được sau này.
- `ai_views`: các lần chuyển mục, cho `current_view`.

Số bản là 800 vì mốc `n` dùng dải `n*100` đến `n*100+99` (mục 13).

### 3.17 Kiểm thử: `scripts/e2e-m8.sh` và `scripts/e2e-m8/fakeclaude`

`fakeclaude` là một chương trình Go đứng thay lệnh `claude`. Nó trả `2.1.328 (Claude Code)` cho `--version`, `{"loggedIn":true,"authMethod":"claude.ai"}` cho `auth status`, và với `-p` thì: đọc prompt từ stdin, ghi argv và prompt ra file để kịch bản so; mở **thật** MCP server trong `--mcp-config` bằng SDK MCP chính thức (tức thật sự SSH vào máy ảo chạy `tpan mcp --session`); in sự kiện `init` theo đúng dạng của `M8-claude-init.log`; chờ `think_ms`; rồi diễn kịch bản (`propose` hoặc `read`) với lần gọi công cụ thật. Nó cố tình cắt bí danh làm hai mẩu, viết mỗi tin nhắn không có dấu cách đầu, và trả `result` chỉ có tin cuối, đúng như Claude Code thật ở 2.10.

Cấu hình đọc từ `fakeclaude.json` cạnh file chạy, vì `tpan brain` chỉ chuyển 5 biến môi trường cho `claude` (3.9), biến môi trường kịch bản đặt sẽ không tới.

`e2e-m8.sh` có ba chế độ: mặc định (claude giả, `M8.log`), `real-rehearse` (kịch bản thật chạy bằng claude giả, không tốn hạn mức), `real` (`claude` thật, `M8-claude.log`).

### 3.18 Ngoài đường của AI: `steps.Reload` (`internal/steps/common.go`)

Chặng này không nằm trên đường đi của một câu hỏi, nhưng là việc dở M8 nhận làm (2.13). Mọi Step ghi file, tạo hay xoá symlink đều reload qua `runReload`, và `runReload` gọi `Reload`:

```go
func Reload(ctx context.Context, sys system.System, cmd system.Cmd) (system.Result, error) {
	if cmd.Name != "systemctl" || !slices.Equal(cmd.Args, []string{"reload", "nginx"}) {
		return sys.Run(ctx, cmd)
	}
	master, old := nginxWorkers(ctx, sys, "")
	res, err := sys.Run(ctx, cmd)
	if err != nil || master == "" || len(old) == 0 {
		return res, err
	}
	return res, waitNginxReload(ctx, sys, master, old)
}
```

Thứ tự là: chụp danh sách worker cũ, reload, rồi chờ. `nginxWorkers` đọc PID của master ở `/run/nginx.pid`, chạy `ps -o pid=,args= --ppid <master>` (vẫn qua `system.System`, argv) và chỉ đếm tiến trình tên đúng `nginx: worker process`; worker cũ đã nhận lệnh thôi việc đổi tên thành `nginx: worker process is shutting down` nên không bị đếm. `waitNginxReload` hỏi lại mỗi 20 ms (`reloadWaitTick`) tới khi có worker và không worker nào thuộc tập cũ. Quá 5 giây (`reloadWaitMax`) là lỗi: nginx từ chối config mới thì chỉ ghi vào `error.log`, `systemctl` vẫn báo thành công, nên không được im lặng coi như đã reload.

Vì sao chờ worker chứ không `sleep 200ms`: một con số cố định là đoán, máy chậm thì vẫn hụt, máy nhanh thì phí. Chờ đúng điều kiện "chỉ còn thế hệ mới nhận kết nối" thì vừa đủ. Apache và mọi lệnh khác đi thẳng `sys.Run`, vì đo cho thấy Apache không có khoảng hở (2.13). Không đọc được PID (test với `FakeSystem`, `--demo`, nginx chưa chạy) thì không chờ.

`core.ReloadWeb` (`internal/core/ssl.go`), đường reload của SSL và hook gia hạn, đổi `sys.Run(ctx, reload)` thành `steps.Reload(ctx, sys, reload)`. Test ở `internal/steps/reload_test.go` dựng các "thế hệ" worker giả: `TestReloadNginxChoTheHeMoi` (lẫn cũ mới hai lần rồi mới đổi, `ps` chạy trước reload), `TestReloadNginxHetHan` (worker cũ còn mãi thì lỗi), `TestReloadKhongCho` (không có `/run/nginx.pid`, reload apache hay PHP-FPM: chỉ chạy đúng lệnh reload, không `ps`), `TestReloadNginxPIDLa` (PID file chứa `1 --pid 2` thì không được lọt vào argv của `ps`, chỉ chạy reload).

## 4. Kiến thức nền

### Unix socket và quyền 600

Unix socket là một điểm nối giữa hai tiến trình trên cùng một máy, trông như một file (chữ `s` đầu dòng `ls -l`). Khác cổng TCP, nó không ra mạng, và ai được nối thì quyết định bằng quyền file như mọi file khác. Quyền `600` (`rw-------`) nghĩa là chỉ chủ (root) đọc ghi được; nhóm và người khác không có gì. Ai nối được socket này là đưa được câu trả lời giả vào panel, nên chỉ root.

Trong mã: `bridge.Listen` (`net.Listen("unix", ...)`, `os.Chmod(path, 0o600)`), `bridge.Link` (`d.DialContext(ctx, "unix", path)`).

Tự xem (trên Mac, lệnh chạy trong máy ảo; dạng này có trong `M8.log` phần c, gọn hơn thì chưa chạy):

```bash
scripts/vm.sh root stat -c "%A %U:%G %n" /run/tpan /run/tpan/brain.sock
scripts/vm.sh exec bash -c 'echo x | nc -U /run/tpan/brain.sock'   # chưa chạy: user ubuntu, chờ bị từ chối quyền
```

### stdin và stdout đi qua SSH

`ssh <máy> <lệnh>` chạy lệnh ở máy kia và nối stdin, stdout của lệnh đó về đúng stdin, stdout của tiến trình `ssh` ở máy mình. Hai đầu thấy nhau như hai đầu một ống, dù giữa chúng là mạng. Không có lệnh nào khác cần mở cổng. M8 dùng ống này hai lần: `tpan brain` giữ `ssh ... brain-link` sống suốt (stdin là job đi xuống, stdout là sự kiện đi lên), và `claude` mở `ssh ... tpan mcp --session` cho mỗi lượt (JSON-RPC của MCP, như M7). Vì stdout là đường dây dữ liệu, `brain-link` in mọi lời từ chối ra stderr.

Trong mã: `system.StartPipe` và `pipeConn` (`cmd/tpan/brain.go`), `bridge.Link`, `sshArgs`.

Tự xem (chưa chạy; trên Mac, cần khoá SSH vào máy ảo như kịch bản tạo): `ssh -i <khoá> ubuntu@<vm> sudo -n tpan brain-link` rồi **dán** (đừng gõ tay) dòng `{"type":"hello","hello":{"host":"thu"}}` và Enter: Hub chỉ chờ lời chào 10 giây (`helloTimeout` trong `hub.go`). Nếu `ai.brain = bridge` và chưa có bộ não nào nối, bạn sẽ thấy dòng `{"type":"welcome"}` trả về. Từ lúc đó terminal của bạn đang giữ chỗ bộ não duy nhất của panel: một `tpan brain` thật sẽ bị từ chối cho tới khi bạn Ctrl+D.

### Server-Sent Events (SSE)

SSE là cách máy chủ đẩy sự kiện xuống trình duyệt trên một kết nối HTTP mở lâu. Máy chủ trả `Content-Type: text/event-stream` và viết từng khối `event: <tên>`, `data: <chữ>`, kết thúc bằng một dòng trống. Trình duyệt dùng `EventSource`; kết nối rớt thì `EventSource` tự nối lại. Chỉ một chiều, máy chủ xuống trình duyệt; câu hỏi đi lên bằng `POST` thường.

Trong mã: `handleAITurnEvents` (`fmt.Fprintf(w, "event: ai\ndata: %s\n\n", data)`, `rc.Flush()`), `subscribeAITurn` trong `web/src/api/client.ts`.

Tự xem: lệnh `curl -sS -N ... /turns/1/events` ở 2.4 (`-N` để `curl` in ngay, không gom). Muốn thấy SSE thô thì bỏ phần tóm tắt của kịch bản và chạy thẳng `curl -N` với cookie đăng nhập (chưa chạy).

### Goroutine và channel trong Go

Goroutine là một hàm chạy song song, mở bằng chữ `go` trước lời gọi. Channel (`chan`) là đường ống có kiểu giữa các goroutine: một bên `ch <- ev` gửi, bên kia `ev := <-ch` nhận, hoặc `for ev := range ch` nhận tới khi kênh đóng. `select` chờ nhiều kênh cùng lúc và lấy kênh nào có trước; có nhánh `default` thì không chờ.

Trong mã: `go p.receive(r, ch, cancel)` trong `Panel.Ask` mở một goroutine đọc kênh `<-chan Event` mà `Brain.Run` trả về, nên câu hỏi chạy tiếp sau khi request HTTP đã trả 202; và `turnRun.send` dùng `select` có `default` để một người xem chậm bị bỏ thay vì làm kẹt cả lượt.

Tự xem: đây là chuyện bên trong tiến trình, máy ảo không cho nhìn trực tiếp. Trên Mac: `go test ./internal/ai/bridge/ -run TestBoNaoThuHaiBiTuChoi -v` (đã chạy ở 2.12); thêm `-race` để Go tự dò hai goroutine đụng cùng một biến (chưa chạy).

### JSON lines

Mỗi dòng một đối tượng JSON trọn vẹn, xuống dòng là hết một tin. Bên nhận đọc từng dòng, không cần biết trước độ dài. Dùng ở ba chỗ: output `stream-json` của `claude -p`, giao thức giữa Hub và Client (`Msg`, đọc bằng `json.Decoder`, ghi bằng `json.Encoder` tự thêm `\n`), và JSON-RPC của MCP.

Trong mã: `turn.onLine` và `Parser.Line` (`claudecode`), `Hub.read` và `clientSession.write` (`bridge`).

Tự xem: `docs/bang-chung/M8-claude-init.log` có dòng `result` nguyên văn. Trên máy ảo, `tpan mcp` nói JSON lines: ba dòng JSON-RPC ở `07-m7-cong-cho-agent.md` mục 2.3 (đã chạy ở M7).

### `context` và thời hạn

`context.Context` mang lệnh "dừng lại" qua nhiều lớp hàm. `context.WithTimeout(ctx, 5*time.Minute)` tự phát lệnh dừng sau 5 phút. Chú ý `Ask` tạo context từ `context.Background()`, không từ context của request: request trả 202 ngay và context của nó bị huỷ, nếu dùng nó thì `claude` bị giết ngay khi vừa chạy.

Trong mã: `Ask` (`panel.go`), `claudecode.Brain.Run` (`runCtx`), `failText` phân biệt "bị huỷ" với "quá giờ".

Tự xem: đây cũng là chuyện bên trong tiến trình. Trên Mac, `go test ./internal/ai/claudecode/ -run TestRunHuyCtx -v` huỷ context giữa lượt và kiểm `claude` giả bị dừng. Mình vừa chạy: `--- PASS: TestRunHuyCtx (0.02s)`.

## 5. Cách khác đã cân nhắc

### Gọi API của Anthropic bằng khoá API

Cách quen thuộc: máy chủ giữ một khoá API và tự gọi model. Không chọn (quyết định ngày 19/11/2026, mục 8.2). Khoá API là một bí mật phải nằm trên VPS, và mỗi câu hỏi tính tiền riêng, trong khi người vận hành đã có gói Claude. Còn phải tự viết vòng lặp "hỏi model, model gọi công cụ, trả kết quả, hỏi tiếp" mà Claude Code đã làm sẵn. Interface `ai.Brain` để ngỏ cho cách này về sau: viết thêm một bản là xong.

### Chạy `claude` trên chính máy chủ (`brain = local`)

Đơn giản hơn: không cần `tpan brain`, không cần cầu. Không chọn cho VPS: muốn vậy phải đăng nhập Claude trên máy chủ, trái nguyên tắc 7. Trang điều khoản của Claude Code (mục 8.2) cũng nói người làm sản phẩm không được nhận, lưu hay chuyển tiếp đăng nhập Claude của người dùng; giữ bộ não trên máy của chính người đó là cách bám đúng ranh giới. Mục 8.2 vẫn có giá trị `local` cho lúc TPAN và Claude Code cùng ở máy cá nhân, nhưng M8 chưa làm (`docs/thay-doi/M8.md`): không "Xong khi" nào cần, và `--demo` dùng bộ não giả.

### `claude -p --bare`

Tài liệu Claude Code khuyên `--bare` cho script: không nạp cấu hình, hook, plugin của người dùng, đúng thứ tpan muốn. Không dùng: ở chế độ đó Claude Code bỏ đăng nhập bằng gói và đòi khoá API, tức quay về cách thứ nhất. tpan thay bằng `--setting-sources ""`, `--disable-slash-commands`, `--strict-mcp-config`, cộng cửa kiểm `init`. Tài liệu cũng nói `--bare` sẽ thành mặc định của `-p` ở một bản sau; vì vậy `tpan brain` ghi `TestedVersion` và cảnh báo khi gặp bản khác. Chưa có bản nào đổi mặc định để thử.

### WebSocket thay cho SSE

WebSocket là kết nối hai chiều, một ống cho cả câu hỏi lẫn câu trả lời. Không cần ở đây: chiều lên chỉ có một câu hỏi mỗi lượt, `POST` thường làm được; chiều xuống là luồng sự kiện, đúng việc của SSE. SSE là HTTP thường, `EventSource` tự nối lại, và M3 đã có SSE cho log của job, nên dùng lại được cách làm và hàm giao diện. Thiết kế "mỗi sự kiện mang toàn bộ chữ" (3.14) còn làm việc nối lại thành vô hại: `handleAITurnEvents` không cần dòng `id:`, cứ gửi lại từ đầu. Ở đây không có lần thử WebSocket nào; đây là lựa chọn theo thiết kế.

### Mở một kết nối SSH dùng chung (`ControlMaster`)

Mỗi lượt `claude` mở một SSH mới cho MCP. Có thể dùng `ControlMaster` của OpenSSH để các lượt dùng lại một kết nối. Không làm, vì đã đo: phần mở SSH cộng liệt kê công cụ mất 218 ms (`M8.log`, phần đo) và dưới 0,3 giây theo `docs/thay-doi/M8.md`, trong khi cả lượt thật mất 10 đến 17 giây.

### Chỗ sai trong mốc này

- **Chữ hai tin nhắn dính nhau.** Bộ đọc luồng nối mọi `text_delta` như một tin. Phát hiện ở lần gọi thật 4 (`đã lưu.Nguyên nhân`, 2.10). Cùng lúc sửa (`eaa11ed`), `claude` giả được sửa để viết như Claude Code thật ("nay viết mỗi tin nhắn không có dấu cách đầu và trả `result` chỉ có tin cuối", theo `docs/thay-doi/M8.md`), và `e2e-m8.sh` kiểm "chữ sau mỗi lần gọi công cụ là đoạn mới". Bài học: bản giả chỉ bắt được lỗi mà người viết nó đã nghĩ tới; hai lỗi này chỉ lộ khi chạy bản thật.
- **Câu trả lời cuối chỉ là `result`.** Hợp đồng ban đầu của `ai.Event` coi `result` là câu trả lời. Phát hiện cùng lần gọi 4: tới `done`, các câu viết trước khi gọi công cụ biến mất khỏi ngăn AI. Sửa ở `a7a6090`; kiểm bằng unit test và bằng lần chạy lại e2e M8 với `claude` giả (2.10). Với Claude Code thật thì chưa chạy lại.
- **Bí danh viết tắt (còn mở).** Model viết `site-a` thay cho `site-a.example`, panel hiện nguyên `site-a` (2.9). Bộ che chỉ đổi đúng bí danh đầy đủ, để không đổi nhầm chữ khác. Chưa sửa; e2e không bắt được vì chỉ tìm dạng đầy đủ.

## 6. Tự kiểm

1. Người review hỏi: "TPAN có lưu đăng nhập Claude ở đâu không?" Chỉ ra trong mã những chỗ chứng minh là không, và giải thích làm sao `claude` vẫn đăng nhập được khi `tpan brain` chạy nó.
2. Vì sao mỗi sự kiện `text` gửi ra trình duyệt mang toàn bộ chữ tới lúc đó thay vì chỉ mẩu mới? Nếu đổi thành chỉ gửi mẩu mới (và đổi bí danh trên từng mẩu), 2.4 sẽ hiện gì?
3. Sửa `claudecode.Run` để mỗi lượt chạy trong một thư mục tạm mới (`os.MkdirTemp`). Chuyện gì hỏng, và hỏng ở lượt thứ mấy? Còn nếu bỏ đoạn kiểm thư mục rỗng thì kẻ xấu làm được gì?
4. Lần gọi 4 kết thúc bằng `done: **Chẩn đoán**`. Vì sao? Mã hiện tại xử lý thế nào, và vì sao vẫn giữ `result` làm đường lùi?
5. Cửa kiểm chạy khi đọc sự kiện `init`, tức sau khi `claude` đã khởi động. Trong khoảng đó `claude` có kịp gọi một công cụ lạ không? Log cho biết gì, và vì sao `claude` giả phải chờ `think_ms`?
6. Lượt 2 ở 2.9 có `"playbook": null`, còn lượt 2 ở 2.6 có `su-co`. Vì sao khác nhau? Nếu lượt 1 bị `refused`, lượt sau có gửi lại bản hướng dẫn không?
7. Một dòng JSON `{"type":"assistant",...}` tới trước sự kiện `init`. `onLine` làm gì? Vì sao không bỏ qua dòng đó rồi chờ `init`?
8. Vì sao `bridge.Listen` chmod socket về 600 mà không để 660 cho một nhóm như socket PHP-FPM của M2? Nếu quên chmod thì ai làm được gì?
9. (Cần máy ảo) Tự kiểm socket và cửa kiểm: xem quyền của `/run/tpan/brain.sock`, rồi chạy lại phần cửa kiểm từ chối `Bash`. Lời từ chối nguyên văn là gì, và bằng cách nào bạn chứng minh không có lần gọi công cụ nào lọt?
10. (Cần máy ảo) Hỏi từ panel khi chưa chạy `tpan brain`. Mã HTTP là gì? Rồi chạy hai `tpan brain` cùng lúc: cái thứ hai in gì, và nó lấy tên máy ở đâu ra?

## Đáp án

1. Không có chỗ nào. `brainEnvKeys` trong `cmd/tpan/brain.go` chỉ chuyển `HOME`, `PATH`, `USER`, `LOGNAME`, `TMPDIR` cho `claude`; chú thích đầu file ghi `tpan brain` không bao giờ đọc `~/.claude`. `claudecode.Auth` chỉ giải hai trường `loggedIn`, `authMethod` vào struct `AuthStatus`; không in, không lưu trường khác. Không có ô nhập token nào trên giao diện, không có cột nào trong migration 800 chứa đăng nhập. `Args` không bao giờ thêm `--bare`. `claude` đăng nhập được vì nó chạy dưới user của bạn với HOME của bạn và tự tìm thông tin đăng nhập của nó (trên macOS là Keychain, cần `USER`/`LOGNAME`). Máy chủ chỉ nhận chữ (job đi xuống, sự kiện đi lên) qua `brain-link`.
2. Vì bí danh có thể bị cắt giữa hai mẩu: `tpan:site/site-a.exa` rồi `mple.com` (2.4, `+1.472s` và `+1.589s`). Đổi trên từng mẩu thì không mẩu nào chứa bí danh nguyên vẹn, `Unalias` không đổi gì, và màn hình hiện `site-a.example`, tức người dùng thấy bí danh thay cho tên thật. `shower.show` nối vào `acc` rồi đổi trên cả chuỗi. Gửi toàn bộ chữ còn có lợi phụ: trình duyệt nối lại SSE nhận lại sự kiện cũ cũng không sai, vì nó thay thế chứ không nối thêm.
3. Lượt 1 vẫn chạy. Lượt 2 hỏng: theo chú thích trong `claudecode.Run`, Claude Code lưu phiên theo thư mục làm việc và `--resume <uuid>` ở thư mục khác không tìm thấy phiên. Panel khi đó sẽ hiện lượt `error` với lỗi của `claude`; trường hợp này chưa chạy thử. 2.6 kiểm chiều ngược lại ("thư mục làm việc của lượt 2 trùng lượt 1"). Bỏ kiểm thư mục rỗng: `claude` tự đọc `CLAUDE.md` và các file ở thư mục làm việc, nên ai đặt được một file vào đó (một chương trình khác dưới user của bạn) là chèn được chỉ dẫn cho AI. AI vẫn không có công cụ shell (cửa kiểm), nhưng nó có thể bị dẫn đi đề xuất Plan sai hoặc trả lời sai. `TestRunThuMucKhongRong` kiểm Run từ chối.
4. Trường `result` của `claude -p` chỉ chứa tin nhắn cuối của model. Ở lần 4 model viết "Tôi chạy health check...", gọi công cụ, viết thêm, gọi công cụ nữa, rồi mới viết tin cuối bắt đầu bằng `**Chẩn đoán**`. Mã cũ lấy `result` làm câu trả lời và giao diện thay chữ đang hiện bằng nó, nên các đoạn trước biến mất. Mã hiện tại (`shower.show`, `finish`, commit `a7a6090`) lấy chữ đã nối `acc`. `result` còn giữ cho trường hợp không có `text_delta` nào (một bộ não khác, hay một bản Claude Code không phát delta): khi đó không còn gì khác để hiện. Đã kiểm bằng `TestCauTraLoiGiuDoanTruocCongCu` và bằng lần chạy lại e2e M8 (`=> ĐẠT: done mang toàn bộ chữ đã nối...`, 2.10).
5. Về nguyên tắc có một khoảng: `claude` in `init` rồi mới hỏi model, và cửa kiểm giết `claude` ngay khi đọc dòng `init` (chưa đo khoảng này). Model cần thời gian để quyết định gọi công cụ: lần thật 6 gọi công cụ đầu ở `+2.805s` trong khi `init` ở `+0.442s`. Tức cửa kiểm thắng nhờ độ trễ của model, không phải nhờ một rào cứng. `claude` giả thì không có model, nên phải chờ `think_ms` (1 giây) để mô phỏng độ trễ đó; không chờ thì nó gọi công cụ ngay và phép thử không đo đúng tình huống thật. Log 2.7: `refused` ở `+0.297s`, không có dấu `turn-3.toolcall`, `ai_calls` 3 trước 3 sau. Lớp chặn còn lại: mọi công cụ `tpan` chỉ đọc hoặc lập Plan chờ duyệt. Đây là chỗ nên nói thẳng với người review: cửa kiểm là lớp bảo đảm "nếu cờ không còn tác dụng thì phát hiện và dừng", không phải chặn tuyệt đối từng mili giây.
6. `playbookSent` chỉ gửi bản hướng dẫn khi phiên chưa gửi bản cùng tên ở một lượt đã tới được `claude`. Ở 2.9 cả hai lượt ở trang site, bản `site` đã gửi ở lượt 1, nên lượt 2 không gửi lại (`null`). Ở 2.6 lượt 2 ở `/incidents`, bản `su-co` chưa gửi, nên gửi. Lượt bị `refused` không có sự kiện `init` (cửa kiểm từ chối trước khi phát `EventInit`), nên `reachedBrain` là false và bản hướng dẫn của lượt đó được coi là chưa gửi: lượt sau gửi lại.
7. `onLine` gọi `refuse`: giết `claude`, gửi `EventRefused` với lời "dòng JSON đầu tiên của claude không phải sự kiện init". Không chờ được vì chưa có `init` thì chưa biết phiên đã nạp gì; một dòng `assistant` có thể mang `tool_use` của một công cụ lạ, và tin nó trước khi qua cửa là ngược nguyên tắc "kiểm cửa rồi mới tin". `TestRunCuaKiemTuChoi/status-first` kiểm trường hợp gần giống.
8. Socket PHP-FPM là 0660 vì nginx (user `www-data`) phải nối được. Ở đây chỉ một tiến trình cần nối: `tpan brain-link`, và nó chạy bằng root qua `sudo -n`. Không có nhóm nào cần quyền, nên không cho nhóm nào. Trên Linux, muốn nối vào một Unix socket thì phải có quyền ghi trên file socket. Không chmod thì quyền của socket tuỳ umask của tiến trình lúc tạo: umask 022 cho `srwxr-xr-x` (người khác không ghi được nên chưa nối được), nhưng umask 000 cho `srwxrwxrwx`, và khi đó một user thường trên máy chủ (ví dụ user của một site WordPress bị chiếm) nối được, gửi `hello` khi chưa có bộ não, rồi đưa câu trả lời và "sự kiện" giả vào panel. Chmod 0600 làm kết quả không phụ thuộc umask. Thêm một lớp: thư mục `/run/tpan` 0700, người khác không đi vào được để tới file socket, kể cả trong khoảnh khắc trước khi chmod. Mình chưa kiểm umask của dịch vụ `tpan` trên máy ảo.
9. Trên Mac: `scripts/vm.sh root stat -c "%A %U:%G %n" /run/tpan /run/tpan/brain.sock` phải ra `drwx------ root:root /run/tpan` và `srw------- root:root /run/tpan/brain.sock` (như 2.1). Cửa kiểm: chạy `scripts/e2e-m8.sh`; phần i đổi `fakeclaude.json` thành `"extra_tool":"Bash"`. Lời từ chối: `cửa kiểm từ chối phiên Claude Code (2.1.328): công cụ ngoài tpan: Bash`. Chứng minh không lọt: luồng SSE chỉ có đúng một sự kiện `refused`; `claude` giả không ghi file dấu `turn-3.toolcall`; số dòng `ai_calls` của phiên trước và sau bằng nhau (máy chủ tự ghi mọi lần gọi công cụ, không dựa vào lời kể). Tự làm lại thì chưa chạy; log 2.7 có đủ.
10. `POST /api/v1/ai/ask` trả `409` với mã `brain_offline` (2.8): `Hub.Run` trả `ai.ErrBrainOffline`, `Panel.Ask` đổi thành lời từ chối có mã. Bộ não thứ hai in `tpan: panel từ chối bộ não này: panel đã có bộ não của máy <tên máy> nối từ <giờ>; mỗi lúc chỉ một bộ não...` và thoát mã 1. Tên máy lấy từ `Hello.Host` mà bộ não thứ nhất gửi lúc nối, do `runBrain` đọc bằng `os.Hostname()` trên máy cá nhân; Hub giữ nó trong `brainConn.hello`. Chạy hai `tpan brain` trên Mac theo lệnh ở 2.2 (cần khoá SSH và cấu hình như kịch bản); tự làm lại thì chưa chạy.
