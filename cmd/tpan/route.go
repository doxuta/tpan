package main

// Lệnh `tpan route explain <domain>` (M14, mục 6.13 và 9 của
// docs/KE-HOACH.md): request tới tên miền này đi đường nào trên máy, từng
// chặng kèm lệnh đã chạy và output. Chỉ đọc; mọi việc lần đường nằm ở
// core.Sites.RouteExplain, lệnh này chỉ in.

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
)

func init() { register(routeCmd) }

func routeCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "route",
		Short: "Xem request tới một tên miền đi đường nào trên máy này",
	}
	c.AddCommand(routeExplainCmd(e))
	return c
}

func routeExplainCmd(e *env) *cobra.Command {
	var asJSON, evidence bool
	c := &cobra.Command{
		Use:   "explain <domain> [--evidence] [--json]",
		Short: "Lần đường đi của request: tên miền, vhost, thư mục gốc, socket, pool, user, open_basedir, gọi thật (mã thoát 1 khi có chặng lỗi)",
		Long: "Lần từ cấu hình web server ĐANG NẠP (nginx -T hoặc apache2ctl -S), không từ điều tpan nghĩ nó đã ghi:\n" +
			"vhost nào nhận tên miền, vhost đó phục vụ thư mục nào, chuyển .php tới socket nào, pool nào nghe\n" +
			"socket đó, pool chạy dưới user nào và được mở thư mục nào, rồi gọi thật vào site. Mỗi chặng so với\n" +
			"thứ tpan mong đợi; chặng không đạt thì in luôn lệnh đã chạy và output. Lệnh không đổi gì trên máy.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			d, err := parseDomainArg(args[0])
			if err != nil {
				return fail(e, asJSON, err)
			}
			// Cần root như health: nginx -T đọc cả file chỉ root đọc được (khoá
			// của chứng chỉ), và file pool nằm trong thư mục của root.
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			// Mở như --dry-run: lệnh chỉ đọc, chưa có DB thì không tạo.
			s, done, err := openSites(ctx, e, true)
			if err != nil {
				return err
			}
			defer done()
			tr, err := s.RouteExplain(ctx, d)
			if err != nil {
				return fail(e, asJSON, err)
			}
			if asJSON {
				if err := writeJSON(e.stdout, tr); err != nil {
					return err
				}
			} else {
				printRoute(e, tr, evidence)
			}
			if routeFails(tr) > 0 {
				return &exitCode{1}
			}
			return nil
		},
	}
	f := c.Flags()
	f.BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	f.BoolVar(&evidence, "evidence", false, "in lệnh đã chạy và output của mọi chặng (mặc định chỉ in ở chặng không đạt)")
	return c
}

// routeFails đếm số chặng lỗi. Cảnh báo (DNS chưa trỏ về máy) và "chưa rõ"
// không tính, như `tpan health`.
func routeFails(tr core.RouteTrace) (n int) {
	for _, h := range tr.Hops {
		if h.Status == checks.Fail {
			n++
		}
	}
	return n
}

// printRoute in từng chặng một dòng "[trạng thái] tên chặng: giá trị", dưới đó
// là câu nói kết quả; bằng chứng (lệnh và output) in khi có --evidence hoặc
// khi chặng không đạt, vì đó là lúc người đọc cần tự kiểm lại.
func printRoute(e *env, tr core.RouteTrace, evidence bool) {
	w := e.stdout
	printf(w, "%s %s (%s)\n\n", e.tr("Request tới", "A request to"), tr.Domain, tr.Web)
	for _, h := range tr.Hops {
		value := h.Value
		if value == "" {
			value = "-"
		}
		printf(w, "[%s] %s: %s\n", statusWord(e.lang, h.Status), h.Title.In(e.lang), value)
		printf(w, "    %s\n", h.Summary.In(e.lang))
		if !evidence && h.Status == checks.Pass {
			continue
		}
		for _, ev := range h.Evidence {
			printf(w, "    $ %s  (%s %d)\n", ev.Command, e.tr("mã thoát", "exit"), ev.ExitCode)
			for _, ln := range strings.Split(strings.TrimRight(ev.Output, "\n"), "\n") {
				if ln != "" {
					printf(w, "      %s\n", ln)
				}
			}
		}
	}
	printf(w, "\n")
	if n := routeFails(tr); n > 0 {
		printf(w, e.tr("Có %d chặng lỗi: request tới %s không đi đúng đường của site. Xem các chặng [lỗi] ở trên.\n",
			"%d hop(s) failed: a request to %s does not follow the site's own path. See the [fail] hops above.\n"), n, tr.Domain)
		return
	}
	printf(w, e.tr("Không chặng nào lỗi: request tới %s đi đúng vhost, thư mục, pool và user của chính site.\n",
		"No hop failed: a request to %s reaches the site's own vhost, directory, pool and user.\n"), tr.Domain)
}
