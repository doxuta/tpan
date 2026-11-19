# Go căn bản qua mã của TPAN

File này dành cho người chưa viết Go bao giờ. Mỗi mục là một phần của Go mà TPAN thật sự dùng, kèm một đoạn mã lấy nguyên từ repo và đường dẫn file để bạn mở ra đọc tiếp. Viết ở M1; các mốc sau thêm mục mới khi mã dùng tới phần mới của Go.

Bản Go đang dùng: 1.27 (`go.mod` ghi `go 1.27.0`, máy dev chạy `go1.27.4`).

Cách đọc nhanh một file Go: dòng `package` ở đầu, rồi `import`, rồi các khai báo `type`, `const`, `var`, `func`. Chú thích `//` ngay trên một khai báo là tài liệu của khai báo đó; trong repo này chú thích viết tiếng Việt và nói vì sao.

## 1. Package và import

Mỗi thư mục là một package. Mọi file `.go` trong `internal/engine/` đều bắt đầu bằng `package engine`, và chỗ khác gọi tới chúng bằng `engine.Execute`, `engine.Plan`.

Tên viết hoa chữ đầu thì dùng được từ package khác (exported), viết thường thì chỉ dùng trong package. Ví dụ `engine.New` gọi được từ `internal/core`, còn `runStep` trong `internal/engine/engine.go` thì không.

Thư mục tên `internal` có luật riêng của Go: chỉ mã nằm trong cùng module mới import được. Không ai ngoài repo này import được `internal/system`.

Go cấm vòng import (A import B, B import A). TPAN né bằng cách vẽ chiều phụ thuộc rõ ràng; chú thích đầu `internal/engine/types.go` ghi lại:

```go
// Gói này import system, checks, i18n. Nó KHÔNG import store: việc ghi job và
// sự kiện đi qua giao diện Journal dưới đây, store cài đặt giao diện đó, core
// nối hai bên lại.
```

Gói lá (không import gói nào của TPAN) như `internal/i18n` thì ai cũng dùng được. Đây là cả gói, gần như trọn vẹn:

```go
type Text struct {
	Vi string `json:"vi"`
	En string `json:"en"`
}

// T dựng một Text. Viết `i18n.T("Cài nginx", "Install nginx")` gọn hơn struct literal.
func T(vi, en string) Text { return Text{Vi: vi, En: en} }
```

Một import bắt đầu bằng `_` chỉ để chạy phần khởi tạo của package đó. `internal/store/store.go` có `_ "modernc.org/sqlite"` để đăng ký driver tên `"sqlite"` cho `database/sql`, dù không gọi trực tiếp hàm nào của nó.

## 2. Kiểu có tên, hằng, struct

Go cho đặt tên mới cho một kiểu có sẵn. `internal/core/types.go`:

```go
// WebServer là web server của stack.
type WebServer string

const (
	Nginx  WebServer = "nginx"  // LEMP
	Apache WebServer = "apache" // LAMP
)
```

`WebServer` vẫn là chuỗi bên dưới, nhưng hàm nhận `WebServer` không nhận nhầm một `string` bất kỳ mà không đổi kiểu tường minh (`WebServer(s)`). `core.ParseWeb` là chỗ duy nhất đổi chuỗi người dùng gõ thành `WebServer`, và nó chỉ nhận `nginx` hoặc `apache`.

Struct là một nhóm trường. Chuỗi trong dấu huyền sau mỗi trường là tag, ở đây báo cho `encoding/json` tên khoá khi in JSON (`tpan health --json` in đúng các tên này). Cũng trong `internal/core/types.go`:

```go
type StackConfig struct {
	// Web rỗng nghĩa là stack chưa được cài bởi tpan.
	Web       WebServer `json:"web"`
	WebSource Source    `json:"web_source"`
	DBSource  Source    `json:"db_source"`
	PHPSource Source    `json:"php_source"`
	// PHPVersion là bản PHP mặc định, ví dụ "8.3". Rỗng trước khi cài: dò từ
	// /etc/php/*/fpm sau bước cài gói rồi mới ghi (mục 6.1, bước 4).
	PHPVersion string `json:"php_version"`
}
```

Struct mới tạo mà chưa gán gì thì mỗi trường mang "giá trị không" (zero value): chuỗi rỗng, số 0, `nil`. TPAN dựa vào điều này nhiều chỗ: `cfg.Web == ""` nghĩa là chưa cài.

Trường viết thường không ai ngoài package đọc hay gán được. `internal/validate/validate.go` dùng điều này để buộc mọi tên miền phải đi qua hàm kiểm:

```go
type Domain struct{ s string }
```

Ngoài package `validate` không ai viết được `Domain{s: "a.com; rm -rf /"}`; cách duy nhất có một `Domain` là gọi `validate.ParseDomain`.

## 3. Hàm, method, con trỏ

Hàm Go trả được nhiều giá trị. Kiểu quen nhất là `(giá trị, error)`. Method là hàm gắn với một kiểu, kiểu đó viết trong ngoặc trước tên hàm (gọi là receiver).

Receiver là giá trị (`t Text`) thì method nhận một bản sao. Receiver là con trỏ (`s *File`) thì method sửa được chính đối tượng. Các Step của TPAN dùng con trỏ vì `Apply` phải nhớ trạng thái cũ để `Undo` trả lại. `internal/steps/files.go`:

```go
	if err := verifyOrRestore(ctx, sys, prev, s.Test, s.Reload, "ghi "+s.Path); err != nil {
		return err
	}
	s.prev, s.applied = prev, true
	return nil
}
```

Dòng `s.prev, s.applied = prev, true` ghi vào chính Step. Vì vậy core luôn dựng Step bằng `&steps.File{...}` (dấu `&` lấy địa chỉ, ra con trỏ).

Con trỏ cũng dùng để hai chỗ chia nhau một biến. Trong `internal/core/stack.go`, `PrepareInstall` tạo `php := new(string)` (một chuỗi rỗng và con trỏ tới nó), đưa con trỏ đó cho bước `steps.DetectPHP{Result: php}`. Khi bước dò chạy xong, nó ghi `*s.Result = v`; `Install.Execute` đọc lại bằng `*in.php`. Dấu `*` trước con trỏ nghĩa là "giá trị nằm ở địa chỉ này".

## 4. Interface

Interface là danh sách method. Kiểu nào có đủ các method đó thì tự động thoả interface, không cần khai báo "implements". Đây là phần Go quan trọng nhất để hiểu TPAN.

`internal/system/types.go` khai báo `System`: mọi thao tác TPAN được làm trên máy. Phần đầu của nó (các method còn lại là `ReadFile`, `WriteFileAtomic`, `MkdirAll`, `Remove`, `Rename`, `Symlink`, `Readlink`, `Stat`, `ReadDir`, `Chown`, `Chmod`, `StatFS`, `Lock`):

```go
type System interface {
	// Run chạy một chương trình bằng argv, KHÔNG qua shell. Kết quả luôn được
	// trả về đầy đủ (kể cả khi có lỗi) để làm bằng chứng.
	//
	// Lỗi:
	//   - chương trình chạy xong với mã thoát khác 0: *ExitError;
	//   - không chạy được (không có chương trình, ctx bị huỷ): lỗi khác.
	// Ai coi mã thoát khác 0 là câu trả lời bình thường (ví dụ `systemctl
	// is-active` trả 3) thì dùng errors.As để lấy *ExitError.
	//
	// LocalSystem luôn đặt môi trường cố định LC_ALL=C và
	// DEBIAN_FRONTEND=noninteractive (cộng PATH chuẩn), không kế thừa môi
	// trường của tiến trình gọi.
	Run(ctx context.Context, cmd Cmd) (Result, error)
```

Có hai kiểu thoả `System`:

- `LocalSystem` trong `internal/system/local.go`: `Run` gọi `exec.CommandContext` chạy chương trình thật, `WriteFileAtomic` ghi file thật.
- `FakeSystem` trong `internal/system/fake.go`: giữ hệ thống file và danh sách lệnh trong bộ nhớ. `Run` không chạy gì, chỉ ghi lại lệnh rồi trả lời theo các handler test đã đăng ký bằng `Handle`.

Mọi Step, phép kiểm và engine chỉ nhận `system.System`, không biết bên dưới là máy thật hay máy giả. Vì vậy cùng một mã Step chạy được trong unit test trên Mac (không root, không apt) và trên máy ảo. `cmd/tpan/main.go` là chỗ duy nhất chọn máy thật: hàm `realEnv` đặt `sys: system.NewLocal()`.

Để chắc một kiểu thoả interface ngay lúc biên dịch, Go có mẹo gán vào biến `_`. `internal/system/fake_test.go` có `var _ System = (*FakeSystem)(nil)`; `internal/steps/common.go` làm vậy cho mọi Step:

```go
// Mọi Step của gói phải thoả engine.Step; lỡ sai chữ ký thì không biên dịch được.
var (
	_ engine.Step = (*AptUpdate)(nil)
	_ engine.Step = (*AptInstall)(nil)
	_ engine.Step = (*Service)(nil)
	_ engine.Step = (*Dir)(nil)
	_ engine.Step = (*File)(nil)
```

Interface thứ hai cần biết là `engine.Step` trong `internal/engine/types.go`: bốn method `Info`, `Check`, `Apply`, `Undo`. File `01-m1-cai-stack.md` đi qua nó kỹ.

Interface nhỏ cũng có ích. `internal/core/stack.go` chỉ cần đọc và ghi settings, nên khai báo đúng hai method:

```go
type Settings interface {
	GetSetting(ctx context.Context, key string) (value string, ok bool, err error)
	SetSetting(ctx context.Context, key, value string) error
}
```

`*store.Store` thoả nó (DB thật). `core.EmptySettings{}` cũng thoả nó: đọc luôn trả "chưa có", ghi luôn lỗi. `--dry-run` trên máy chưa có DB dùng bản rỗng này, nhờ vậy xem trước không tạo file DB nào.

Nhúng (embedding): một struct có thể chứa một interface không đặt tên trường; mọi method của interface đó thành method của struct, và struct chỉ viết lại method nào nó muốn đổi. `internal/engine/engine.go`:

```go
type recSys struct {
	system.System
	r      *run
	stepID string
}
```

`recSys` tự có `ReadFile`, `Stat`, ... của System bên trong, và chỉ viết lại `Run` để ghi mỗi lệnh thành một sự kiện của job.

## 5. Lỗi: error, %w, errors.Is, errors.As

Go không có exception. Hàm có thể lỗi thì trả thêm một giá trị kiểu `error`; người gọi kiểm `if err != nil`. Đó là lý do bạn thấy `if err != nil { return ... }` ở khắp nơi.

Khi trả lỗi lên trên, TPAN bọc thêm ngữ cảnh bằng `fmt.Errorf` với `%w`. `%w` giữ lỗi gốc bên trong, để tầng trên vẫn hỏi được "lỗi gốc có phải là X không". Ví dụ trong `internal/system/local.go`:

```go
func (s *LocalSystem) ReadFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("đọc %s: %w", path, err)
	}
	return b, nil
}
```

Thông báo lỗi đọc thành chuỗi từ ngoài vào trong. Lỗi thật trên máy ảo ở `docs/bang-chung/M1.log` (lần 1) bắt đầu bằng: `bước apt.install:...: thực hiện: cài gói ...: apt-get install -y ...: mã thoát 100: E: Failed to fetch ...`. Mỗi đoạn trước dấu hai chấm do một tầng thêm vào: engine, `runStep`, `AptInstall.Apply`, rồi `ExitError` của system.

`errors.Is(err, X)` hỏi: trong chuỗi lỗi có lỗi X không. `internal/steps/files.go` dùng nó để phân biệt "file chưa có" (câu trả lời bình thường) với lỗi thật:

```go
	fi, err := sys.Stat(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", s.Path, err)
	}
```

`errors.As(err, &x)` hỏi: trong chuỗi có lỗi kiểu này không, nếu có thì gán vào `x`. `system.ExitError` là kiểu lỗi riêng của TPAN cho "chương trình chạy xong với mã thoát khác 0". `internal/steps/common.go`:

```go
func isExit(err error) (*system.ExitError, bool) {
	var ee *system.ExitError
	ok := errors.As(err, &ee)
	return ee, ok
}
```

Nhờ vậy `systemctl is-active nginx` trả mã 3 được hiểu là "không chạy", còn "không tìm thấy chương trình systemctl" mới là lỗi.

Một kiểu muốn làm lỗi chỉ cần có method `Error() string`. `core.RefusedError` (lời từ chối có chủ ý, ví dụ máy không sạch) và `system.ExitError` đều là struct có method đó.

`errors.Join(a, b)` gộp hai lỗi thành một, dùng khi đang báo lỗi mà việc dọn dẹp cũng lỗi; ví dụ `verifyOrRestore` trong `internal/steps/common.go`.

## 6. defer, panic, recover

`defer f()` hẹn gọi `f` lúc hàm hiện tại kết thúc, dù kết thúc bằng `return` hay bằng panic. Engine dùng nó để luôn nhả khoá toàn cục (`internal/engine/engine.go`):

```go
	unlock, err := e.sys.Lock(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("lấy khoá toàn cục: %w", err)
	}
	// defer để khoá được nhả cả khi có panic lọt ra ngoài.
	defer unlock()
```

Panic là lỗi làm dừng chương trình (ví dụ đọc phần tử ngoài mảng). `recover()` gọi trong một hàm defer thì bắt được panic. Engine biến panic trong mã của một Step thành lỗi thường, để vẫn hoàn tác được các bước trước:

```go
func safe(fn func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return fn()
}
```

Ở đây `err` là giá trị trả về có tên, nên hàm defer gán được vào nó sau khi `fn()` đã panic.

## 7. context

`context.Context` là một giá trị đi kèm mọi lời gọi có thể lâu (chạy lệnh, truy vấn DB). Nó mang tín hiệu "huỷ đi" và hạn giờ. Quy ước Go: tham số đầu tiên tên `ctx`.

Tín hiệu huỷ bắt đầu ở `cmd/tpan/main.go`:

```go
	// Ctrl+C hoặc `systemctl stop` gửi tín hiệu; ctx bị huỷ, server tắt êm, job
	// đang chạy dừng và hoàn tác các bước đã làm.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], realEnv())
```

`ctx` này đi xuống cobra, core, engine, Step, rồi tới `exec.CommandContext` trong `LocalSystem.Run`. Bấm Ctrl+C thì lệnh con (ví dụ `apt-get`) bị giết, và `runStep` trong engine thấy `ctx.Err()` khác nil trước bước kế tiếp.

Nhưng hoàn tác thì vẫn phải chạy sau khi người dùng đã bấm dừng. `internal/engine/engine.go` tạo một context mới không nhận tín hiệu huỷ, có hạn 5 phút:

```go
func undoStep(ctx context.Context, s Step, sys system.System) error {
	uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), undoTimeout)
	defer cancel()
	return safe(func() error { return s.Undo(uctx, sys) })
}
```

Cùng lý do, engine ghi sự kiện vào DB bằng `jctx: context.WithoutCancel(ctx)`.

Chờ có huỷ được: `LocalSystem.Lock` trong `internal/system/local.go` thử lấy khoá, chưa được thì chờ 100 ms, nhưng thoát ngay khi ctx bị huỷ. `select` chờ cái nào tới trước:

```go
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("chờ khoá tpan %s: %w", s.LockPath, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
```

## 8. Goroutine, mutex, channel

Goroutine là một luồng chạy song song, rất nhẹ, mở bằng chữ `go`. `internal/server/server.go` mở một goroutine chỉ để chờ ctx bị huỷ rồi tắt HTTP server, trong lúc luồng chính đang bận phục vụ request:

```go
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
```

Có goroutine mà TPAN không tự viết chữ `go`: khi `exec.Cmd` có `Stdout` và `Stderr` không phải file, thư viện chuẩn tự mở goroutine để chép từng luồng output. Hai goroutine đó có thể gọi cùng lúc, nên `LocalSystem.Run` cho hai bộ ghi dùng chung một `sync.Mutex`:

```go
	// Một mutex chung cho hai luồng: OnOutput không bao giờ bị gọi song song,
	// nên người viết OnOutput không phải tự khoá.
	var mu sync.Mutex
	stdout := &lineWriter{mu: &mu, fn: cmd.OnOutput}
	stderr := &lineWriter{mu: &mu, fn: cmd.OnOutput}
```

Mutex là khoá trong một tiến trình: `mu.Lock()` thì goroutine khác gọi `Lock()` phải chờ tới khi `mu.Unlock()`. Engine cũng có một mutex (`run.mu` trong `internal/engine/engine.go`) để số thứ tự `Seq` của sự kiện luôn liên tục dù output tới từ hai luồng. Test `TestConcurrentOutputKeepsSeq` trong `internal/engine/engine_test.go` kiểm đúng điều đó.

`sync.Once` bảo đảm một việc chỉ làm một lần; hàm `unlock` mà `Lock` trả về dùng nó, nên gọi `unlock` hai lần cũng không sao.

Channel là ống để goroutine gửi giá trị cho nhau. `FakeSystem` dùng một channel chứa được đúng 1 phần tử làm khoá (`internal/system/fake.go`): gửi được vào ống nghĩa là đang giữ khoá, rút ra là nhả.

```go
func (f *FakeSystem) Lock(ctx context.Context) (func(), error) {
	select {
	case f.lock <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("chờ khoá tpan: %w", ctx.Err())
	}
	var once sync.Once
	return func() { once.Do(func() { <-f.lock }) }, nil
}
```

Khoá giữa hai tiến trình khác nhau (CLI và `tpan serve`) thì mutex không làm được; đó là việc của `flock`, xem `01-m1-cai-stack.md`.

## 9. Hàm là giá trị, closure

Trong Go, hàm truyền đi được như mọi giá trị. CLI đưa method `onEvent` của bộ in tiến độ cho engine (`cmd/tpan/stack.go`):

```go
			p := newProgress(out, e.lang, verbose, in.Plan)
			rep, err := in.Execute(ctx, p.onEvent)
```

Engine không biết gì về màn hình; nó chỉ gọi hàm được đưa cho mỗi khi có sự kiện.

Closure là hàm viết tại chỗ và nhớ biến bên ngoài nó. `installPlan` trong `internal/core/stack.go` đưa cho phép kiểm một hàm đọc bản PHP: `func() string { return *php }`. Lúc dựng Plan chưa biết bản PHP; tới lúc kiểm chứng, hàm đó đọc giá trị mà bước dò đã ghi.

## 10. embed: nhúng file vào file chạy

Chú thích đặc biệt `//go:embed` bảo trình biên dịch chép file vào ngay trong file chạy. Cài TPAN chỉ cần chép một file `tpan`, không kèm thư mục template hay SQL nào.

Template config, trong `internal/templates/templates.go`:

```go
//go:embed files/*.tmpl
var files embed.FS

// Các template được phân tích một lần lúc nạp gói. Template hỏng (lỗi cú pháp)
// là lỗi lập trình, nên panic ngay khi chạy test thay vì để lọt tới máy chủ.
// Option missingkey=error: tên trường gõ sai thì lỗi chứ không in "<no value>"
// vào file config.
var tmpl = template.Must(
	template.New("").Option("missingkey=error").ParseFS(files, "files/*.tmpl"),
)
```

Migration của DB, trong `internal/store/store.go`: `//go:embed migrations/*.sql` rồi `var migrationFS embed.FS`. Hàm `migrate` đọc các file `.sql` từ `migrationFS` như đọc thư mục thường.

Giao diện web đã build, trong `web/embed.go`: `//go:embed all:dist`. Tiền tố `all:` để nhúng cả file bắt đầu bằng dấu chấm.

## 11. Test

File tên `..._test.go` là test, nằm cạnh mã nó kiểm, chỉ được biên dịch khi chạy `go test`. Hàm test có dạng `func TestTen(t *testing.T)`; gọi `t.Errorf` là đánh dấu hỏng nhưng chạy tiếp, `t.Fatalf` là hỏng và dừng hàm.

Kiểu test phổ biến nhất trong Go là test bảng: một danh sách trường hợp, một vòng lặp chạy cùng một phép kiểm. `internal/validate/validate_test.go`:

```go
	cases := []struct{ in, want string }{
		{"example.com", "example.com"},
		{"Example.COM", "example.com"},
		{"  shop.vn \t", "shop.vn"},
		{"a.b.c.example.com", "a.b.c.example.com"},
		{"a-b.com", "a-b.com"},
		{"123.com", "123.com"},
		{"xn--bcher-kva.example", "xn--bcher-kva.example"},
		// Tên miền có dấu phải ra dạng punycode.
		{"bücher.example", "xn--bcher-kva.example"},
		{"BÜCHER.example", "xn--bcher-kva.example"},
		{strings.Repeat("a", 63) + ".com", strings.Repeat("a", 63) + ".com"},
	}
	for _, c := range cases {
		got, err := ParseDomain(c.in)
```

`[]struct{ in, want string }` là một slice (mảng co giãn) của struct không đặt tên, khai báo ngay tại chỗ. Thêm một trường hợp là thêm một dòng.

`t.Run(tên, func...)` tách mỗi trường hợp thành một test con có tên riêng, để khi hỏng biết ngay trường hợp nào. `TestPlanOrder` trong `internal/core/stack_test.go` chạy một lần cho `nginx`, một lần cho `apache`, và so thứ tự ID các bước của Plan với danh sách mong đợi.

Test của engine, steps, checks, core đều chạy trên `FakeSystem`. Test giả lập máy bằng `Handle`: "lệnh nào bắt đầu bằng `ss` thì trả output này". Ví dụ trong `internal/checks/ports_test.go`, hàm phụ `reply(fake, []string{"ss"}, 0, out, "")` đăng ký output giả cho `ss`, rồi kiểm `PortLocalOnly(3306, StageDB)` trả `Fail` khi MariaDB nghe ở `0.0.0.0`.

Golden test: `internal/templates/templates_test.go` sinh file config từ template rồi so từng byte với file mẫu trong `internal/templates/testdata/*.golden`. Ai sửa template mà quên cập nhật mẫu thì test đỏ, nên mọi thay đổi config đều hiện ra trong diff của commit.

Test không chỉ kiểm hành vi. `internal/system/noshell_test.go` đọc mọi file `.go` của repo và báo lỗi nếu thấy gọi shell kèm `-c` hoặc import `os/exec` ngoài `internal/system`.

Chạy thử trên Mac, trong thư mục repo (đã chạy, output thật):

```
$ go test ./internal/engine/ -run 'TestUndoReverseOrderOnlyApplied|TestLock' -v
=== RUN   TestUndoReverseOrderOnlyApplied
--- PASS: TestUndoReverseOrderOnlyApplied (0.00s)
=== RUN   TestLock
=== RUN   TestLock/giữ_và_nhả_đúng_một_lần
=== RUN   TestLock/không_lấy_được_khoá
--- PASS: TestLock (0.00s)
    --- PASS: TestLock/giữ_và_nhả_đúng_một_lần (0.00s)
    --- PASS: TestLock/không_lấy_được_khoá (0.00s)
PASS
ok  	github.com/.../tpan/internal/engine	0.415s
```

(Ở dòng `ok` cuối của hai khối, mình rút gọn đường dẫn module thành `github.com/.../tpan`.) Chạy toàn bộ: `make check` (go vet, golangci-lint, unit test, kiểm kiểu TypeScript).

```
$ go test ./internal/system -run 'TestNoShell|TestShellPatterns' -v
=== RUN   TestNoShell
--- PASS: TestNoShell (0.01s)
=== RUN   TestShellPatterns
--- PASS: TestShellPatterns (0.00s)
PASS
ok  	github.com/.../tpan/internal/system	0.101s
```

## 12. Vài thứ của bản Go mới bạn sẽ gặp

Những thứ này có từ Go 1.21 trở đi; tài liệu cũ trên mạng có thể chưa nhắc.

- `strings.Lines(s)` trả về một dãy các dòng để duyệt bằng `for line := range strings.Lines(...)`. Gặp trong `missingPackages` ở `internal/steps/apt.go`. Mỗi dòng còn giữ dấu xuống dòng ở cuối, nên mã gọi `strings.TrimRight(line, "\n")`.
- Tag `json:"...,omitzero"`: bỏ trường khỏi JSON khi nó mang giá trị không. Gặp ở `Finished time.Time` trong `internal/engine/types.go`, vì `omitempty` không bỏ được một `time.Time` rỗng.
- Package `slices` (`slices.Contains`, `slices.Clone`) trong thư viện chuẩn, gặp ở `internal/steps/apache.go`.
- `context.WithoutCancel`, đã nói ở mục 7.

## 13. Tự kiểm

1. Vì sao Step trong `internal/steps` luôn được dựng bằng `&steps.File{...}` mà không phải `steps.File{...}`?
2. `LocalSystem` và `FakeSystem` không có dòng nào ghi "implements System". Làm sao Go biết chúng thoả `System`, và nếu thiếu một method thì lỗi hiện ra lúc nào?
3. Trong `undoStep`, bỏ `context.WithoutCancel` thì chuyện gì xảy ra khi người dùng bấm Ctrl+C giữa lúc cài?
4. `fmt.Errorf("stat %s: %v", path, err)` khác gì `%w`? Đổi `%w` thành `%v` trong `LocalSystem.Stat` thì `File.Check` hỏng ở đâu?
5. Vì sao `lineWriter` của stdout và stderr dùng chung một mutex mà không mỗi cái một mutex?
6. Xoá file `internal/templates/files/nginx-default.conf.tmpl` rồi `go build ./...`. Build có qua không, và lỗi hiện ra ở đâu?

### Đáp án

1. Vì `Apply` ghi trạng thái cũ vào chính Step (`s.prev, s.applied = prev, true`) để `Undo` dùng. Engine giữ Step dưới dạng interface `engine.Step`. Nếu là giá trị chứ không phải con trỏ thì method có receiver `*File` không thuộc về kiểu `File`, và `steps.File{...}` không thoả `engine.Step`; trình biên dịch báo lỗi ngay.
2. Go so danh sách method: kiểu có đủ method với đúng chữ ký thì thoả interface. Dòng `var _ System = (*FakeSystem)(nil)` trong `internal/system/fake_test.go` (và `var _ engine.Step = ...` trong `internal/steps/common.go`) buộc trình biên dịch kiểm việc này; thiếu method thì lỗi lúc biên dịch, không đợi tới lúc chạy.
3. Ctrl+C huỷ `ctx`. Nếu Undo dùng chính `ctx` đó thì mọi lệnh hoàn tác (`systemctl reload`, `a2enmod`) chạy qua `exec.CommandContext` sẽ bị giết ngay, máy nằm lại ở trạng thái dở. `WithoutCancel` giữ cho hoàn tác chạy hết, còn `WithTimeout` 5 phút giữ cho một lệnh treo không giữ khoá mãi.
4. `%v` chỉ chép chữ của lỗi gốc; `%w` chép chữ và giữ lỗi gốc bên trong. Với `%v`, `errors.Is(err, fs.ErrNotExist)` trong `File.Check` không còn thấy lỗi gốc, nên "file chưa có" bị coi là lỗi thật: `Check` trả lỗi thay vì `false`, và bước ghi một file chưa tồn tại luôn hỏng trên máy thật. Unit test vẫn có thể xanh vì `FakeSystem` có `Stat` riêng; chỉ e2e trên máy ảo mới bắt được. Đây là lý do hợp đồng ở `internal/system/types.go` ghi rõ cả hai bản đều phải trả lỗi thoả `errors.Is(err, fs.ErrNotExist)`.
5. Một `OnOutput` nhận dòng từ cả hai luồng. Hai mutex riêng thì dòng stdout và dòng stderr vẫn có thể gọi `OnOutput` cùng lúc. Một mutex chung bảo đảm mỗi lúc chỉ một dòng được đẩy ra, nên engine nhận dòng tuần tự.
6. Theo cách đọc mã thì build vẫn qua: mẫu `files/*.tmpl` còn khớp hai file kia, và `template.Must` chỉ panic khi template sai cú pháp. Lỗi hiện ra lúc chạy: `NginxDefault` gọi `ExecuteTemplate` với tên `nginx-default.conf.tmpl` không còn, trả lỗi, nên golden test trong `internal/templates/templates_test.go` đỏ, và `tpan stack install --web nginx` báo lỗi lúc dựng Plan (trong `installPlan`), trước khi đụng vào máy. Chưa chạy; tự kiểm trên một nhánh thử: xoá file, `go build ./...`, rồi `go test ./internal/templates/`.
