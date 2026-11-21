package main

// `tpan cloud-init render|status|run` (M18, mục 3.9 và 3.12 của
// docs/thay-doi/M18-thiet-ke.md).
//
//   - render: sinh user-data cho máy mới (chỉ đọc, không cần root). Phần kiểm
//     và dựng nằm ở core.CloudInit.Render, dùng chung với API.
//   - run (ẩn): lệnh duy nhất mà script khởi động đầu gọi trên máy mới. Chạy
//     bằng root, không có HOME, không có terminal; lỗi chỉ thấy ở
//     /var/log/cloud-init-output.log (stdout của lệnh này), ở LogPath và ở
//     StatePath (mục 2.5). Năm việc: setup, admin, stack, sites, health.
//   - status: in StatePath cho người đọc.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/cloudinit"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

func init() { register(cloudInitCmd) }

func cloudInitCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "cloud-init",
		Short: "Sinh user-data cho cloud-init để máy mới tự cài tpan, stack và site; xem tiến độ lần khởi động đầu",
	}
	c.AddCommand(cloudInitRenderCmd(e), cloudInitStatusCmd(e), cloudInitRunCmd(e))
	return c
}

// ---------------------------------------------------------------- render

// cloudInitRenderJSON là output --json của render. Mật khẩu dạng chữ thật, đúng
// một lần, ở đây (CloudInitResult tự che nó khi ra JSON).
type cloudInitRenderJSON struct {
	UserData      string `json:"user_data"`
	Bytes         int    `json:"bytes"`
	AdminPassword string `json:"admin_password"`
}

// maxSumsFile: SHA256SUMS của một bản phát hành chỉ vài trăm byte; đọc tối đa
// chừng này để `--sums /dev/zero` không làm tpan đọc vô hạn.
const maxSumsFile = 1 << 20

func cloudInitRenderCmd(e *env) *cobra.Command {
	var (
		web, sitesPath, base, sumsPath, outPath string
		private, asJSON                         bool
		maxBytes                                int
	)
	c := &cobra.Command{
		Use:   "render --web nginx|apache [--sites FILE] [--private] [--base URL] [--sums FILE] [--max-bytes N] [--out FILE] [--json]",
		Short: "Sinh user-data (cloud-init) cho máy mới; mật khẩu quản trị của máy mới in ra một lần",
		Args:  cobra.NoArgs,
		// Không cần root: chỉ đọc file người dùng chỉ ra và (khi --base là
		// https:// mà thiếu --sums) tải SHA256SUMS. Không đọc bí mật nào.
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w, err := core.ParseWeb(web)
			if err != nil {
				return fail(e, asJSON, err)
			}
			in := core.CloudInitInput{Web: w, Private: private, Base: base, AllowFetch: true, MaxBytes: maxBytes}
			if sitesPath != "" {
				sf, err := ciReadSitesFile(e, sitesPath)
				if err != nil {
					return fail(e, asJSON, err)
				}
				in.Sites = &sf
			}
			if sumsPath != "" {
				if in.Sums, err = ciReadFileMax(sumsPath, maxSumsFile); err != nil {
					return err
				}
			}
			ci := &core.CloudInit{Sys: e.sys, Version: buildinfo.Version, Hash: store.HashPassword}
			res, err := ci.Render(ctx, in)
			if err != nil {
				return fail(e, asJSON, err)
			}
			if outPath != "" {
				if err := ciWriteOut(outPath, res.UserData); err != nil {
					return err
				}
			}
			pw := res.AdminPassword.Reveal()
			if asJSON {
				return writeJSON(e.stdout, cloudInitRenderJSON{UserData: string(res.UserData), Bytes: res.Bytes, AdminPassword: pw})
			}
			if outPath == "" {
				if _, err := e.stdout.Write(res.UserData); err != nil {
					return err
				}
			}
			// stderr, đúng một dòng: stdout có thể đang được chuyển vào file
			// user-data, mà file đó không bao giờ được chứa mật khẩu (B6).
			printf(e.stderr, "%s: %s\n", e.tr(
				"Mật khẩu quản trị panel của máy mới (chỉ hiện một lần, lưu lại ngay)",
				"Admin password of the new machine's panel (shown once, save it now)"), pw)
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&web, "web", "", "web server của máy mới: nginx hoặc apache")
	_ = c.MarkFlagRequired("web")
	f.StringVar(&sitesPath, "sites", "", "file sites.yaml: các site tạo ở lần khởi động đầu (\"-\" là stdin)")
	f.BoolVar(&private, "private", false, "panel của máy mới chỉ nghe 127.0.0.1 (như tpan setup --private)")
	f.StringVar(&base, "base", "", "nơi có tpan-linux-<arch> (mặc định: trang phát hành của bản đang chạy)")
	f.StringVar(&sumsPath, "sums", "", "file SHA256SUMS của hai file chạy (bắt buộc khi --base là http://)")
	f.IntVar(&maxBytes, "max-bytes", cloudinit.DefaultMaxBytes, "trần cỡ user-data, byte (mặc định là trần của AWS EC2)")
	f.StringVar(&outPath, "out", "", "ghi user-data vào file này (quyền 0600) thay vì stdout")
	f.BoolVar(&asJSON, "json", false, "in {user_data, bytes, admin_password} dạng JSON")
	return c
}

// ciReadSitesFile đọc sites.yaml như readSitesFile nhưng chưa Resolve: Render
// tự Resolve rồi ghi lại file từ kết quả đã kiểm.
func ciReadSitesFile(e *env, path string) (core.SitesFile, error) {
	r := e.stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return core.SitesFile{}, fmt.Errorf("mở %s: %w", path, err)
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	return core.ParseSitesFile(r)
}

func ciReadFileMax(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("mở %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, fmt.Errorf("đọc %s: %w", path, err)
	}
	return b, nil
}

// ciWriteOut ghi user-data cho người đang gõ lệnh, quyền 0600. Dùng thư
// viện chuẩn chứ không qua system.System: render chạy không cần root, còn
// WriteFileAtomic luôn chown về root. O_NOFOLLOW: không ghi xuyên symlink
// (khi chạy bằng sudo trong thư mục người khác ghi được).
func ciWriteOut(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("ghi %s: %w", path, err)
	}
	// File đã có từ trước giữ quyền cũ khi mở: đặt lại 0600 trước khi ghi.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("ghi %s: %w", path, err)
	}
	return f.Close()
}

// ---------------------------------------------------------------- status

func cloudInitStatusCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Xem tiến độ lần khởi động đầu (cloud-init) của máy này",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// File trạng thái là root 0600.
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			st, found, err := readFirstBootState(e)
			if err != nil {
				return err
			}
			if !found {
				printf(e.stderr, "%s\n", e.tr(
					"Máy này chưa có lần khởi động đầu nào do tpan cloud-init chạy (không có "+cloudinit.StatePath+").",
					"This machine has no first boot run by tpan cloud-init (no "+cloudinit.StatePath+")."))
				return &exitCode{1}
			}
			if asJSON {
				return writeJSON(e.stdout, st)
			}
			printFirstBootState(e, st)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in nguyên nội dung file trạng thái dạng JSON")
	return c
}

func readFirstBootState(e *env) (cloudinit.State, bool, error) {
	b, err := e.sys.ReadFile(cloudinit.StatePath)
	if errors.Is(err, fs.ErrNotExist) {
		return cloudinit.State{}, false, nil
	}
	if err != nil {
		return cloudinit.State{}, false, fmt.Errorf("đọc %s: %w", cloudinit.StatePath, err)
	}
	var st cloudinit.State
	if err := json.Unmarshal(b, &st); err != nil {
		return cloudinit.State{}, false, fmt.Errorf("đọc %s: %w", cloudinit.StatePath, err)
	}
	return st, true, nil
}

func printFirstBootState(e *env, st cloudinit.State) {
	w := e.stdout
	var verdict string
	switch {
	case st.OK:
		verdict = e.tr("đã xong", "finished")
	case !st.Finished.IsZero() || firstBootLastFailed(st):
		verdict = e.tr("LỖI (sửa rồi chạy lại: sudo tpan cloud-init run --force)", "FAILED (fix it, then run again: sudo tpan cloud-init run --force)")
	default:
		verdict = e.tr("đang chạy, hoặc bị ngắt giữa chừng", "running, or interrupted")
	}
	printf(w, "%s %s\n", e.tr("Lần khởi động đầu:", "First boot:"), verdict)
	printf(w, "%-12s %s\n", e.tr("Bắt đầu:", "Started:"), st.Started.Format(time.RFC3339))
	if !st.Finished.IsZero() {
		printf(w, "%-12s %s\n", e.tr("Kết thúc:", "Finished:"), st.Finished.Format(time.RFC3339))
	}
	printf(w, "\n%-8s %-10s %8s  %-16s %s\n", e.tr("VIỆC", "STEP"), e.tr("TRẠNG THÁI", "STATUS"), e.tr("GIÂY", "SECONDS"), "JOB", e.tr("LỖI", "ERROR"))
	for _, s := range st.Steps {
		printf(w, "%-8s %-10s %8.1f  %-16s %s\n", s.Name, s.Status, s.Seconds, s.JobID, s.Error)
	}
	printf(w, "\n%s %s\n", e.tr("Log:", "Log:"), cloudinit.LogPath)
}

func firstBootLastFailed(st cloudinit.State) bool {
	return len(st.Steps) > 0 && st.Steps[len(st.Steps)-1].Status == "failed"
}

// ---------------------------------------------------------------- run

// Thư mục của file trạng thái và log. /var/lib/tpan cùng quyền với bước Dir của
// tpan setup (core.StateDirPerm), để bước đó thấy "đã đúng". /var/log/tpan
// 0755: user của từng site phải đi qua được để tới /var/log/tpan/sites/<domain>.
const (
	firstBootStateDir = "/var/lib/tpan"
	firstBootLogDir   = "/var/log/tpan"
)

func cloudInitRunCmd(e *env) *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:    "run [--force]",
		Short:  "Chạy lần khởi động đầu theo " + cloudinit.ConfigPath + " (script cloud-init gọi; chạy lại bằng tay với --force)",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, false, err)
			}
			cfg, err := loadFirstBootConfig(e)
			if err != nil {
				return fail(e, false, err)
			}
			old, found, err := readFirstBootState(e)
			if err != nil {
				return err
			}
			// S13: lần khởi động đầu đã xong thì không làm lại gì (script gọi
			// lại lệnh này mỗi khi cloud-init chạy lại scripts_user).
			if found && old.OK && !force {
				printf(e.stdout, "%s %s\n", e.tr("Lần khởi động đầu đã xong lúc", "The first boot already finished at"), old.Finished.Format(time.RFC3339))
				return nil
			}
			fb := &firstBoot{e: e, cfg: cfg}
			if found && !force {
				for _, s := range old.Steps {
					if s.Status == "done" || s.Status == "skipped" {
						fb.prev = append(fb.prev, s)
					}
				}
			}
			return fb.run(ctx)
		},
	}
	c.Flags().BoolVar(&force, "force", false, "chạy lại từ đầu dù lần trước đã xong (mọi Plan tự bỏ qua bước đã đúng)")
	return c
}

// loadFirstBootConfig đọc và kiểm ConfigPath. Không có file:
// cloudinit_no_config; sai khuôn: cloudinit_invalid.
func loadFirstBootConfig(e *env) (cloudinit.Config, error) {
	raw, err := e.sys.ReadFile(cloudinit.ConfigPath)
	if errors.Is(err, fs.ErrNotExist) {
		return cloudinit.Config{}, &core.RefusedError{Code: core.CodeCloudInitNoConfig, Message: i18n.T(
			"không có "+cloudinit.ConfigPath+"; máy này không được tạo từ user-data của tpan cloud-init render",
			"no "+cloudinit.ConfigPath+"; this machine was not created from tpan cloud-init render user-data")}
	}
	if err != nil {
		return cloudinit.Config{}, fmt.Errorf("đọc %s: %w", cloudinit.ConfigPath, err)
	}
	cfg, err := cloudinit.ParseConfig(raw)
	var inv *cloudinit.InvalidError
	if errors.As(err, &inv) {
		return cloudinit.Config{}, &core.RefusedError{Code: core.CodeCloudInitInvalid, Message: inv.Msg}
	}
	return cfg, err
}

// firstBoot là một lần chạy `tpan cloud-init run`.
type firstBoot struct {
	e     *env
	cfg   cloudinit.Config
	st    *store.Store
	state cloudinit.State
	// prev: các việc đã đạt ở lần chạy trước chưa xong (không có --force),
	// theo thứ tự. Các việc này không chạy lại (R14 của bản soát hẹp): máy
	// tạo từ snapshot của một máy đã xoá vài site mà chạy lại việc sites thì
	// apply tạo lại đúng các site đã xoá.
	prev []cloudinit.StepState
}

// donePrev: việc name đã đạt ở lần chạy trước.
func (fb *firstBoot) donePrev(name string) bool {
	for _, s := range fb.prev {
		if s.Name == name {
			return true
		}
	}
	return false
}

func (fb *firstBoot) run(ctx context.Context) error {
	e := fb.e
	if err := e.sys.MkdirAll(firstBootStateDir, core.StateDirPerm, system.Root); err != nil {
		return err
	}
	if err := e.sys.MkdirAll(firstBootLogDir, 0o755, system.Root); err != nil {
		return err
	}
	// File trạng thái có TRƯỚC việc đầu tiên (B15): từ lúc này script khởi
	// động đầu không bao giờ thay file chạy nữa. Không ghi được thì dừng.
	// Việc đã đạt ở lần trước vào file trạng thái ngay từ lần ghi đầu: tiến
	// trình chết giữa chừng thì lần sau vẫn biết chúng đã đạt.
	fb.state = cloudinit.State{Started: time.Now().UTC(), Steps: append([]cloudinit.StepState{}, fb.prev...)}
	if err := fb.save(); err != nil {
		return err
	}
	fb.log(fmt.Sprintf("start tpan %s web=%s private=%t sites=%t", buildinfo.Version, fb.cfg.Web, fb.cfg.Private, fb.cfg.SitesFile != ""))
	defer func() {
		if fb.st != nil {
			closeStore(fb.st)
		}
	}()

	jobs := []struct {
		name string
		fn   func(context.Context) (jobID string, skipped bool, err error)
	}{
		{"setup", fb.setup},
		{"admin", fb.admin},
		{"stack", fb.stack},
		{"sites", fb.sites},
		{"health", fb.health},
	}
	for _, j := range jobs {
		if j.name == "sites" && fb.cfg.SitesFile == "" {
			continue
		}
		if fb.donePrev(j.name) {
			// setup là việc mở DB cho các việc sau: bỏ qua nó thì vẫn phải mở.
			if j.name == "setup" {
				st, err := openStore(ctx, e)
				if err != nil {
					return err
				}
				fb.st = st
			}
			fb.log(j.name + " skipped 0.0s (đã đạt ở lần chạy trước; chạy lại cả việc này: --force)")
			continue
		}
		if err := fb.step(ctx, j.name, j.fn); err != nil {
			fb.state.Finished = time.Now().UTC()
			fb.state.OK = false
			_ = fb.save()
			fb.log("failed")
			printf(e.stderr, "tpan: %s %s: %s\n", e.tr("lần khởi động đầu lỗi ở việc", "first boot failed at step"), j.name, firstBootLine(err.Error()))
			return &exitCode{1}
		}
	}
	fb.state.Finished = time.Now().UTC()
	fb.state.OK = true
	if err := fb.save(); err != nil {
		return err
	}
	fb.log("ok")
	return nil
}

// step chạy một việc và ghi lại tiến độ của nó vào file trạng thái và log.
func (fb *firstBoot) step(ctx context.Context, name string, fn func(context.Context) (string, bool, error)) error {
	fb.state.Steps = append(fb.state.Steps, cloudinit.StepState{Name: name, Status: "running"})
	cur := &fb.state.Steps[len(fb.state.Steps)-1]
	if err := fb.save(); err != nil {
		return err
	}
	printf(fb.e.stdout, "\n== %s\n", name)
	t0 := time.Now()
	jobID, skipped, err := fn(ctx)
	cur.Seconds = time.Since(t0).Round(100 * time.Millisecond).Seconds()
	cur.JobID = jobID
	cur.Status = "done"
	if skipped {
		cur.Status = "skipped"
	}
	if err != nil {
		cur.Status = "failed"
		cur.Error = firstBootLine(err.Error())
	}
	line := fmt.Sprintf("%s %s %.1fs", name, cur.Status, cur.Seconds)
	if jobID != "" {
		line += " job=" + jobID
	}
	if cur.Error != "" {
		line += " error=" + cur.Error
	}
	fb.log(line)
	if serr := fb.save(); serr != nil && err == nil {
		return serr
	}
	return err
}

func (fb *firstBoot) save() error {
	b, err := json.MarshalIndent(fb.state, "", "  ")
	if err != nil {
		return err
	}
	if err := fb.e.sys.WriteFileAtomic(cloudinit.StatePath, append(b, '\n'), 0o600, system.Root); err != nil {
		return fmt.Errorf("ghi %s: %w", cloudinit.StatePath, err)
	}
	return nil
}

// log ghi một dòng có giờ vào LogPath và in cùng dòng ra stdout (cloud-init
// chép stdout vào cloud-init-output.log). Ghi log lỗi chỉ in ra stderr: file
// trạng thái mới là nguồn chính, mất một dòng log không được làm hỏng lượt chạy.
func (fb *firstBoot) log(msg string) {
	line := time.Now().UTC().Format(time.RFC3339) + " " + msg + "\n"
	printf(fb.e.stdout, "%s", line)
	if err := firstBootAppend(fb.e.sys, cloudinit.LogPath, []byte(line)); err != nil {
		printf(fb.e.stderr, "tpan: %v\n", err)
	}
}

// firstBootAppend nối data vào cuối file (root 0600): đọc nội dung cũ, ghi lại
// cả file bằng WriteFileAtomic để không bao giờ có file ghi dở.
func firstBootAppend(sys system.System, path string, data []byte) error {
	old, err := sys.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("đọc %s: %w", path, err)
	}
	if err := sys.WriteFileAtomic(path, append(old, data...), 0o600, system.Root); err != nil {
		return fmt.Errorf("ghi %s: %w", path, err)
	}
	return nil
}

// firstBootLine gom câu lỗi về một dòng: log và file trạng thái mỗi việc một dòng.
func firstBootLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }), " | ")
}

// firstBootJobErr biến một job không thành công thành lỗi có ngữ cảnh.
func firstBootJobErr(what string, rep engine.Report) error {
	if rep.Job.Status == engine.JobSucceeded {
		return nil
	}
	return fmt.Errorf("%s: job %s %s: %s", what, rep.Job.ID, rep.Job.Status, rep.Job.Error)
}

// setup: đúng Plan của `tpan setup`, nhưng KHÔNG sinh và không in mật khẩu
// quản trị (việc admin đặt bản băm từ user-data).
func (fb *firstBoot) setup(ctx context.Context) (string, bool, error) {
	e := fb.e
	sp, err := buildSetupPlan(e.sys, fb.cfg.Private)
	if err != nil {
		return "", false, err
	}
	// Như setup: store.Open tạo /var/lib/tpan/tpan.db; mở trước khi chạy Plan
	// để job setup có trong lịch sử. Giữ mở cho các việc sau.
	st, err := openStore(ctx, e)
	if err != nil {
		return "", false, err
	}
	fb.st = st
	rep, err := engine.New(e.sys, engine.Options{Journal: st, OnEvent: newProgress(e.stdout, e.lang, false, sp.plan).onEvent}).Execute(ctx, sp.plan)
	cliAudit(ctx, e, st, "setup", sp.listen, rep.Job)
	if err != nil {
		return rep.Job.ID, false, fmt.Errorf("setup: %w", err)
	}
	return rep.Job.ID, false, firstBootJobErr("setup", rep)
}

// admin: đặt bản băm mật khẩu quản trị từ user-data khi DB chưa có. Đã có (chạy
// lại, hoặc ai đó đã đổi bằng tpan admin passwd) thì không đụng.
func (fb *firstBoot) admin(ctx context.Context) (string, bool, error) {
	if _, ok, err := fb.st.AdminPasswordHash(ctx); err != nil || ok {
		return "", ok, err
	}
	if fb.cfg.AdminHash == "" {
		printf(fb.e.stdout, "%s\n", fb.e.tr(
			"config.json không có admin_hash; đặt mật khẩu quản trị bằng: sudo tpan admin passwd",
			"config.json has no admin_hash; set the admin password with: sudo tpan admin passwd"))
		return "", true, nil
	}
	// ParseConfig chỉ kiểm tiền tố; VerifyPassword đọc đủ chuỗi PHC (tham số,
	// salt, hash) và trả lỗi khi hỏng. Ghi một bản băm hỏng là khoá chết cửa
	// đăng nhập panel.
	if _, err := store.VerifyPassword(fb.cfg.AdminHash, "x"); err != nil {
		return "", false, fmt.Errorf("admin_hash trong %s hỏng: %w", cloudinit.ConfigPath, err)
	}
	return "", false, fb.st.SetAdminPasswordHash(ctx, fb.cfg.AdminHash)
}

// stack: như `tpan stack install --web <web> --yes`.
func (fb *firstBoot) stack(ctx context.Context) (string, bool, error) {
	e := fb.e
	w, err := core.ParseWeb(fb.cfg.Web)
	if err != nil {
		return "", false, err
	}
	s := &core.Stack{Sys: e.sys, Settings: fb.st, Journal: fb.st}
	in, err := s.PrepareInstallWith(ctx, w, core.InstallOptions{})
	if err != nil {
		return "", false, err
	}
	printPreconditionWarnings(e, in.Preconditions)
	rep, err := in.Execute(ctx, newProgress(e.stdout, e.lang, false, in.Plan).onEvent)
	cliAudit(ctx, e, fb.st, "stack.install", string(w), rep.Job)
	if err != nil {
		return rep.Job.ID, false, fmt.Errorf("stack install: %w", err)
	}
	return rep.Job.ID, false, firstBootJobErr("stack install", rep)
}

// sites: như `tpan apply -f <SitesFile> --yes`. Mật khẩu quản trị WordPress
// được nối vào PasswordsPath NGAY khi Execute trả về, kể cả khi lô lỗi một
// phần: mất nó là mất tài khoản quản trị của site đã cài (S33). Không in ra
// stdout hay log, vì cloud-init-output.log và console do nhà cung cấp giữ.
func (fb *firstBoot) sites(ctx context.Context) (string, bool, error) {
	e := fb.e
	raw, err := e.sys.ReadFile(fb.cfg.SitesFile)
	if err != nil {
		return "", false, fmt.Errorf("đọc %s: %w", fb.cfg.SitesFile, err)
	}
	sf, err := core.ParseSitesFile(bytes.NewReader(raw))
	if err != nil {
		return "", false, err
	}
	list, err := sf.Resolve()
	if err != nil {
		return "", false, err
	}
	st := fb.st
	s := &core.Sites{Sys: e.sys, Settings: st, Store: st, Backups: st, Accounts: st, Journal: st}
	ap, err := s.PrepareApply(ctx, core.ApplyInput{Sites: list})
	if err != nil {
		return "", false, err
	}
	// Dòng bị từ chối là site sẽ không có trên máy mới: với lần khởi động đầu
	// (không ai ở đó để xem bảng kế hoạch) đó là lỗi.
	var refused []string
	for _, it := range ap.Items {
		if it.Action == core.ApplyRefused {
			refused = append(refused, it.Domain+" ("+it.Code+": "+it.Reason.In(e.lang)+")")
		}
	}
	if ap.Empty() {
		if len(refused) > 0 {
			return "", false, fmt.Errorf("site bị từ chối: %s", strings.Join(refused, "; "))
		}
		return "", true, nil
	}
	pr := newProgress(e.stdout, e.lang, false, batchPlan(ap.Batch))
	res, err := ap.Execute(ctx, func(ev engine.Event) { applyEvent(pr, ev) })
	// Mật khẩu trước mọi thứ khác.
	if perr := saveSitePasswords(e.sys, res.Created); perr != nil {
		return "", false, perr
	}
	jobID := ""
	if res.Report != nil {
		jobID = res.Report.Job.ID
		cliAudit(ctx, e, st, "apply", res.Report.Job.Target, res.Report.Job)
		printApplyUnits(e, res.Report.Units)
	}
	if err != nil {
		return jobID, false, fmt.Errorf("apply: %w", err)
	}
	if !applyOK(res) {
		return jobID, false, fmt.Errorf("apply: có site lỗi (xem bảng kết quả ở trên, và job %s ở trang Lịch sử)", jobID)
	}
	if len(refused) > 0 {
		return jobID, false, fmt.Errorf("site bị từ chối: %s", strings.Join(refused, "; "))
	}
	return jobID, false, nil
}

// saveSitePasswords nối "giờ domain user mật_khẩu" của mọi site vừa cài
// WordPress vào PasswordsPath (root 0600).
func saveSitePasswords(sys system.System, created []core.ApplySiteResult) error {
	var b strings.Builder
	now := time.Now().UTC().Format(time.RFC3339)
	for _, c := range created {
		if c.WPInstalled {
			fmt.Fprintf(&b, "%s %s %s %s\n", now, c.Domain, c.AdminUser, c.AdminPassword.Reveal())
		}
	}
	if b.Len() == 0 {
		return nil
	}
	if err := firstBootAppend(sys, cloudinit.PasswordsPath, []byte(b.String())); err != nil {
		return fmt.Errorf("lưu mật khẩu quản trị WordPress: %w", err)
	}
	return nil
}

// health: như `tpan health` (lần chạy đầy đủ). Có phép kiểm lỗi, hoặc stack
// chưa cài, thì việc này lỗi (cùng luật mã thoát với healthExit).
func (fb *firstBoot) health(ctx context.Context) (string, bool, error) {
	e := fb.e
	h := &healthRun{e: e, st: fb.st, runner: &health.Runner{Sys: e.sys, Store: fb.st, DBPath: e.dbPath}}
	rep, err := h.run(ctx)
	if err != nil {
		return "", false, err
	}
	measureDisk(ctx, e, fb.st)
	if !rep.Installed {
		return "", false, errors.New("health: stack chưa cài")
	}
	var bad []string
	for _, g := range rep.Groups {
		for _, r := range g.Results {
			if r.Status == checks.Fail {
				bad = append(bad, r.ID)
			}
		}
	}
	if len(bad) > 0 {
		return "", false, fmt.Errorf("health: phép kiểm lỗi: %s", strings.Join(bad, ", "))
	}
	return "", false, nil
}
