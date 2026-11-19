package main

// Lệnh `tpan mcp` (mục 8.3 của docs/KE-HOACH.md): chạy MCP server qua stdio
// cho agent nối bằng SSH, và `tpan mcp config` để xem, đổi chế độ duyệt, mức
// che dữ liệu và bộ não của AI trong panel (M8). Ba setting này chỉ đổi được ở
// đây, bằng root trên máy chủ: bật chế độ client là cho agent tự chạy Plan nó
// đề xuất, bật bộ não là mở socket cho tpan brain-link; không được để một
// phiên web hay chính agent tự bật.

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/mcp"
	"github.com/doxuta/tpan/internal/tools"
)

func init() { register(mcpCmd) }

func mcpCmd(e *env) *cobra.Command {
	c := mcpServeCmd(e)
	c.AddCommand(mcpConfigCmd(e))
	return c
}

func mcpConfigCmd(e *env) *cobra.Command {
	var (
		approval, redact, brain string
		asJSON                  bool
	)
	c := &cobra.Command{
		Use:   "config [--approval panel|client] [--redact all|secrets] [--brain off|bridge]",
		Short: "Xem hoặc đổi chế độ duyệt Plan, mức che dữ liệu và bộ não AI của panel",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			setApproval, setRedact := cmd.Flags().Changed("approval"), cmd.Flags().Changed("redact")
			setBrain := cmd.Flags().Changed("brain")
			// Kiểm giá trị trước mọi việc khác: sai thì không mở DB, không ghi gì.
			if setApproval {
				switch core.ApprovalMode(approval) {
				case core.ApprovalPanel, core.ApprovalClient:
				default:
					return fail(e, asJSON, &core.RefusedError{Code: "approval_invalid", Message: i18n.T(
						"--approval chỉ nhận panel hoặc client (nhận được "+quote(approval)+")",
						"--approval accepts only panel or client (got "+quote(approval)+")")})
				}
			}
			if setRedact && redact != "all" && redact != "secrets" {
				return fail(e, asJSON, &core.RefusedError{Code: "redact_invalid", Message: i18n.T(
					"--redact chỉ nhận all hoặc secrets (nhận được "+quote(redact)+")",
					"--redact accepts only all or secrets (got "+quote(redact)+")")})
			}
			if setBrain && brain != core.BrainOff && brain != core.BrainBridge {
				return fail(e, asJSON, &core.RefusedError{Code: "brain_invalid", Message: i18n.T(
					"--brain chỉ nhận off hoặc bridge (nhận được "+quote(brain)+")",
					"--brain accepts only off or bridge (got "+quote(brain)+")")})
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			st, err := openStore(ctx, e)
			if err != nil {
				return err
			}
			defer closeStore(st)

			var changed []string
			if setApproval {
				if err := st.SetSetting(ctx, core.SettingMCPApproval, approval); err != nil {
					return err
				}
				changed = append(changed, "approval="+approval)
			}
			if setRedact {
				if err := st.SetSetting(ctx, core.SettingAIRedact, redact); err != nil {
					return err
				}
				changed = append(changed, "redact="+redact)
			}
			if setBrain {
				if err := st.SetSetting(ctx, core.SettingAIBrain, brain); err != nil {
					return err
				}
				changed = append(changed, "brain="+brain)
			}
			if len(changed) > 0 {
				auditNoJob(ctx, e, st, "mcp.config", strings.Join(changed, " "))
			}
			cfg, err := core.AgentConfig(ctx, st)
			if err != nil {
				return err
			}
			if setApproval && cfg.Approval == core.ApprovalClient {
				// Ra stderr khi --json để stdout chỉ có JSON; người vẫn đọc được.
				w := e.stdout
				if asJSON {
					w = e.stderr
				}
				printf(w, "%s\n", e.tr(
					"CẢNH BÁO: chế độ client cho agent nối qua `tpan mcp` tự chạy Plan nó đề xuất (apply_plan), chỉ dựa vào bước hỏi quyền của chính agent. Tắt bằng: sudo tpan mcp config --approval panel",
					"WARNING: client mode lets an agent connected through `tpan mcp` run the plans it proposes (apply_plan), relying only on the agent's own permission prompt. Turn it off with: sudo tpan mcp config --approval panel"))
			}
			if asJSON {
				return writeJSON(e.stdout, cfg)
			}
			printAgentConfig(e, cfg)
			return nil
		},
	}
	fl := c.Flags()
	fl.StringVar(&approval, "approval", "", "panel: người duyệt Plan trên web hoặc bằng tpan plan approve; client: agent tự chạy Plan nó đề xuất")
	fl.StringVar(&redact, "redact", "", "all: che bí mật và thay domain, IP, email bằng bí danh; secrets: chỉ che bí mật")
	fl.StringVar(&brain, "brain", "", "off: panel không có AI; bridge: bộ não là Claude Code trên máy của bạn, nối bằng tpan brain <máy>")
	fl.BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func printAgentConfig(e *env, cfg core.AgentSettings) {
	approval := e.tr("người duyệt trên web hoặc bằng sudo tpan plan approve <id>", "a person approves on the web or with sudo tpan plan approve <id>")
	if cfg.Approval == core.ApprovalClient {
		approval = e.tr("agent nối trực tiếp tự chạy Plan nó đề xuất", "a directly connected agent runs the plans it proposes")
	}
	redact := e.tr("che bí mật và thay domain, IP, email bằng bí danh", "hide secrets and replace domains, IPs and emails with aliases")
	if cfg.Redact == "secrets" {
		redact = e.tr("chỉ che bí mật (mật khẩu, khoá, salt)", "hide secrets only (passwords, keys, salts)")
	}
	printf(e.stdout, "%-16s %s (%s)\n", e.tr("Chế độ duyệt:", "Approval:"), cfg.Approval, approval)
	printf(e.stdout, "%-16s %s (%s)\n", e.tr("Mức che:", "Redaction:"), cfg.Redact, redact)
	brain := e.tr("panel không có AI; bật bằng sudo tpan mcp config --brain bridge", "the panel has no AI; turn it on with sudo tpan mcp config --brain bridge")
	if cfg.Brain == core.BrainBridge {
		brain = e.tr("Claude Code trên máy của bạn, nối bằng tpan brain <máy>", "Claude Code on your machine, connected with tpan brain <host>")
	}
	printf(e.stdout, "%-16s %s (%s)\n", e.tr("Bộ não:", "Brain:"), cfg.Brain, brain)
}

// mcpServeCmd chạy MCP server trên stdin/stdout. Agent gọi nó qua SSH (`ssh
// <máy> tpan mcp`), nên stdout chỉ được chở JSON-RPC: lời từ chối và lỗi đều
// ra stderr, kể cả khi chưa kịp mở server.
func mcpServeCmd(e *env) *cobra.Command {
	var sessionID string
	c := &cobra.Command{
		Use:   "mcp [--session ID]",
		Short: "Chạy MCP server qua stdio cho agent (gọi qua SSH từ máy cá nhân)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, false, err)
			}
			st, err := openStore(ctx, e)
			if err != nil {
				return err
			}
			defer closeStore(st)
			host, err := setupHostname()
			if err != nil {
				return fmt.Errorf("đọc hostname: %w", err)
			}
			// Dựng giống server.Deps của `tpan serve` trên máy thật: WPHash,
			// WPCLISHA512 rỗng nghĩa là core dùng mã đã ghim.
			sess, err := tools.OpenSession(ctx, tools.Deps{Sys: e.sys, Store: st, Hostname: host}, tools.SessionOptions{ID: sessionID})
			if err != nil {
				return fmt.Errorf("mở phiên MCP: %w", err)
			}
			return mcp.Serve(ctx, sess)
		},
	}
	c.Flags().StringVar(&sessionID, "session", "", "nối vào phiên đã mở từ một câu hỏi trên panel (M8)")
	return c
}
