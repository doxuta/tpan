package main

// Lệnh `tpan brain` và `tpan brain-link` (mục 8.4 của docs/KE-HOACH.md).
//
//	máy cá nhân:  tpan brain <máy>  ──ssh <máy> sudo -n tpan brain-link──►  máy chủ
//	                 │ chạy claude -p                                     │ chép stdin/stdout
//	                 ▼                                                    ▼ sang /run/tpan/brain.sock
//	              Claude Code của người vận hành                       tpan serve (bridge.Hub)
//
// `tpan brain` chạy bằng user thường trên máy cá nhân, không bao giờ đọc
// ~/.claude: nó chỉ chạy lệnh `claude` với HOME của người vận hành để chính
// claude tìm đăng nhập của nó (luật 9 của AGENTS.md).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/ai"
	"github.com/doxuta/tpan/internal/ai/bridge"
	"github.com/doxuta/tpan/internal/ai/claudecode"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

func init() { register(brainCmd, brainLinkCmd) }

var (
	// sshTargetRe: đích ssh dạng máy, user@máy, user@10.0.0.1. Không cho bắt
	// đầu bằng "-": ssh sẽ hiểu đó là một cờ (ví dụ -oProxyCommand=...).
	sshTargetRe = regexp.MustCompile(`^[A-Za-z0-9_\[][A-Za-z0-9._@:%\[\]-]{0,254}$`)
	// modelRe: tên model đi vào --model của claude; bắt đầu bằng "-" thì thành cờ.
	modelRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\[\]-]{0,99}$`)
)

// brainEnvKeys là các biến môi trường của tiến trình tpan brain được chuyển
// cho claude và ssh. LocalSystem không kế thừa môi trường (chỉ có PATH chuẩn
// và LC_ALL=C), nên phải chuyển tay đúng thứ cần: HOME để claude tìm đăng
// nhập và ssh tìm khoá, PATH để tìm được claude (thường ở ~/.local/bin hay
// thư mục của Homebrew), USER và LOGNAME để tìm đúng mục đăng nhập trong
// Keychain của macOS, TMPDIR vì macOS đặt thư mục tạm riêng cho từng user.
// Không chuyển gì khác: claude không cần biết thêm về máy của bạn.
var brainEnvKeys = []string{"HOME", "PATH", "USER", "LOGNAME", "TMPDIR"}

func brainEnv() []string {
	var env []string
	for _, k := range brainEnvKeys {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

func brainCmd(e *env) *cobra.Command {
	var (
		model    string
		maxTurns int
		sshOpts  []string
	)
	c := &cobra.Command{
		Use:   "brain <ssh-đích> [--model M] [--max-turns N] [--ssh-opt OPT ...]",
		Short: "Chạy trên máy cá nhân: nối Claude Code của bạn vào panel của <máy> (AI trong panel)",
		Long: `Chạy trên máy cá nhân (không cần root). tpan brain kiểm claude --version và
claude auth status, rồi giữ kết nối ssh <máy> sudo -n tpan brain-link. Mỗi câu
hỏi từ panel chạy claude -p trên máy này, bằng gói Claude của bạn; claude gọi
công cụ của TPAN qua ssh <máy> sudo -n tpan mcp. Dừng bằng Ctrl+C.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if len(args) != 1 {
				return fail(e, false, &core.RefusedError{Code: "brain_target_missing", Message: i18n.T(
					"cần đúng một máy chủ để nối, ví dụ: tpan brain ubuntu@example.com",
					"give exactly one server to connect to, e.g. tpan brain ubuntu@example.com")})
			}
			target := args[0]
			if !sshTargetRe.MatchString(target) {
				return fail(e, false, &core.RefusedError{Code: "brain_target_invalid", Message: i18n.T(
					"đích ssh "+quote(target)+" không hợp lệ (dạng máy hoặc user@máy, không bắt đầu bằng -)",
					"ssh target "+quote(target)+" is invalid (use host or user@host, not starting with -)")})
			}
			if !modelRe.MatchString(model) {
				return fail(e, false, &core.RefusedError{Code: "brain_model_invalid", Message: i18n.T(
					"--model "+quote(model)+" không hợp lệ (ví dụ sonnet, opus)",
					"--model "+quote(model)+" is invalid (e.g. sonnet, opus)")})
			}
			return runBrain(ctx, e, target, model, maxTurns, sshOpts)
		},
	}
	fl := c.Flags()
	fl.StringVar(&model, "model", ai.DefaultModel, "model của Claude cho mọi câu hỏi từ panel")
	fl.IntVar(&maxTurns, "max-turns", ai.MaxTurns, "số vòng gọi công cụ tối đa của một câu hỏi (chỉ hạ được, trần là 12)")
	fl.StringArrayVar(&sshOpts, "ssh-opt", nil, "thêm đúng một tham số cho ssh (dùng cho cả kết nối brain-link lẫn MCP), lặp lại được, ví dụ --ssh-opt=-oPort=2222 --ssh-opt=-oIdentityFile=~/.ssh/vm")
	return c
}

// sshArgs là argv của ssh (không gồm "ssh") tới target để chạy remote.
// BatchMode=yes: ssh không bao giờ dừng lại hỏi mật khẩu, vì claude mở ssh
// cho MCP ở nền, không có ai trả lời.
func sshArgs(opts []string, target string, remote ...string) []string {
	args := append([]string{}, opts...)
	args = append(args, "-o", "BatchMode=yes", target)
	return append(args, remote...)
}

func runBrain(ctx context.Context, e *env, target, model string, maxTurns int, sshOpts []string) error {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("tìm thư mục cache của user: %w", err)
	}
	env := brainEnv()
	// e.sys là system.NewLocal() khi chạy thật; test thay bằng FakeSystem để
	// kiểm các lời từ chối mà không cần claude. Khoá toàn cục (Lock) không
	// dùng ở đây: tpan brain không đổi gì trên máy.
	cfg := claudecode.Config{
		Sys:      e.sys,
		Claude:   "claude",
		Env:      env,
		Model:    model,
		MaxTurns: maxTurns,
		MCP:      claudecode.MCPServer{Command: "ssh", Args: sshArgs(sshOpts, target, "sudo", "-n", "tpan", "mcp")},
		WorkRoot: filepath.Join(cacheDir, "tpan-brain"),
	}

	version, err := claudecode.Version(ctx, cfg)
	if err != nil {
		return fail(e, false, &core.RefusedError{Code: "claude_missing", Message: i18n.T(
			"không chạy được claude ("+err.Error()+"). Cài Claude Code trên máy này rồi chạy lại.",
			"could not run claude ("+err.Error()+"). Install Claude Code on this machine and try again.")})
	}
	if version != claudecode.TestedVersion {
		printf(e.stderr, "%s\n", e.tr(
			fmt.Sprintf("CẢNH BÁO: Claude Code bản %s; tpan đã kiểm với bản %s. Bản khác có thể đổi cách chạy claude -p (ví dụ --bare thành mặc định); nếu panel báo bộ não bị từ chối thì đọc lời từ chối.", version, claudecode.TestedVersion),
			fmt.Sprintf("WARNING: Claude Code %s; tpan was tested with %s. Another version may change how claude -p runs (e.g. --bare becoming the default); if the panel says the brain was refused, read the refusal.", version, claudecode.TestedVersion)))
	}
	auth, err := claudecode.Auth(ctx, cfg)
	if err != nil {
		return fmt.Errorf("kiểm đăng nhập Claude Code: %w", err)
	}
	if !auth.LoggedIn {
		return fail(e, false, &core.RefusedError{Code: "claude_not_logged_in", Message: i18n.T(
			"Claude Code trên máy này chưa đăng nhập. Chạy claude auth login rồi chạy lại tpan brain.",
			"Claude Code on this machine is not logged in. Run claude auth login, then run tpan brain again.")})
	}
	if auth.AuthMethod != "claude.ai" {
		printf(e.stderr, "%s\n", e.tr(
			"CẢNH BÁO: Claude Code đăng nhập bằng "+quote(auth.AuthMethod)+", không phải gói Claude: mỗi câu hỏi từ panel sẽ tính tiền API.",
			"WARNING: Claude Code is logged in with "+quote(auth.AuthMethod)+", not a Claude plan: every question from the panel is billed to the API."))
	}
	brain, err := claudecode.New(cfg)
	if err != nil {
		return err
	}
	host, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("đọc hostname: %w", err)
	}

	pipe, err := system.StartPipe(ctx, system.Cmd{Name: "ssh", Args: sshArgs(sshOpts, target, "sudo", "-n", "tpan", "brain-link"), Env: env})
	if err != nil {
		return fmt.Errorf("mở ssh tới %s: %w", target, err)
	}
	welcomed := false
	client := &bridge.Client{
		Conn:  pipeConn{pipe},
		Brain: brain,
		Hello: bridge.Hello{Host: host, ClaudeVersion: version, TestedVersion: claudecode.TestedVersion, Model: model, AuthMethod: auth.AuthMethod},
		Log: func(line string) {
			if line == bridge.LogWelcome {
				welcomed = true
				printf(e.stdout, "%s\n%s\n", e.tr(
					fmt.Sprintf("Bộ não: đã nối %s, Claude Code %s, model %s", target, version, model),
					fmt.Sprintf("Brain: connected to %s, Claude Code %s, model %s", target, version, model)),
					e.tr("Mỗi câu hỏi từ panel hiện sau dấu ?, mỗi lần gọi công cụ sau dấu →. Dừng bằng Ctrl+C.",
						"Each question from the panel shows after ?, each tool call after →. Stop with Ctrl+C."))
				return
			}
			printf(e.stdout, "%s %s\n", time.Now().Format("15:04:05"), line)
		},
	}
	serveErr := client.Serve(ctx)
	// Serve đã đóng ống (stdin của ssh): ssh và brain-link ở máy chủ thoát theo.
	waitErr := pipe.Wait()
	if ctx.Err() != nil {
		// Ctrl+C: ssh bị giết, lỗi của nó là chuyện đương nhiên, không in.
		printf(e.stdout, "%s\n", e.tr("Bộ não: đã ngắt.", "Brain: disconnected."))
		return nil
	}
	if !welcomed && waitErr != nil {
		printf(e.stderr, "%s\n", e.tr(
			"Không nối được panel qua ssh (xem lỗi của ssh ở trên). tpan brain chạy ssh với BatchMode=yes nên không hỏi mật khẩu: cần đăng nhập bằng khoá, và trên máy chủ cần dòng sudoers cho phép user của bạn chạy /usr/local/bin/tpan brain-link và /usr/local/bin/tpan mcp không mật khẩu (sudo -n).",
			"Could not reach the panel over ssh (see ssh's error above). tpan brain runs ssh with BatchMode=yes, so it never asks for a password: you need key login, and on the server a sudoers line letting your user run /usr/local/bin/tpan brain-link and /usr/local/bin/tpan mcp without a password (sudo -n)."))
	}
	if serveErr != nil {
		if waitErr != nil {
			return fmt.Errorf("%w (%v)", serveErr, waitErr)
		}
		return serveErr
	}
	return waitErr
}

// pipeConn ghép stdout và stdin của ssh thành Conn của bridge.Client. Close
// đóng stdin trước để ssh và brain-link ở máy chủ thấy EOF rồi thoát.
type pipeConn struct{ p *system.Pipe }

func (c pipeConn) Read(b []byte) (int, error)  { return c.p.Stdout.Read(b) }
func (c pipeConn) Write(b []byte) (int, error) { return c.p.Stdin.Write(b) }
func (c pipeConn) Close() error {
	return errors.Join(c.p.Stdin.Close(), c.p.Stdout.Close())
}

// brainLinkCmd là đầu máy chủ của tpan brain: ống nối stdin/stdout của ssh sang
// socket nội bộ của tpan serve. stdout chỉ chở giao thức, nên mọi lời từ chối
// và lỗi ra stderr (ssh chuyển nó về terminal của tpan brain).
func brainLinkCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:    "brain-link",
		Short:  "Ống nối của tpan brain tới panel (tpan brain gọi qua ssh; không gõ tay)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, false, err)
			}
			st, err := openStore(ctx, e)
			if err != nil {
				return err
			}
			cfg, err := core.AgentConfig(ctx, st)
			// Đóng DB trước khi giữ kết nối hàng giờ: brain-link không cần nó nữa.
			closeStore(st)
			if err != nil {
				return err
			}
			if cfg.Brain != core.BrainBridge {
				return fail(e, false, &core.RefusedError{Code: "brain_off", Message: i18n.T(
					"AI trong panel đang tắt (ai.brain = "+cfg.Brain+"). Bật trên máy chủ: sudo tpan mcp config --brain bridge",
					"AI in the panel is off (ai.brain = "+cfg.Brain+"). Turn it on on the server: sudo tpan mcp config --brain bridge")})
			}
			return bridge.Link(ctx, bridge.SocketPath, e.stdin, e.stdout)
		},
	}
}
