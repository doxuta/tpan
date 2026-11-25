package main

// `tpan shared-domain list|add|rm`: tên miền dùng chung (core/shared_suffix.go).
// Mặc định hai khách không thể có hai tên con của cùng một tên miền gốc. Máy
// chỉ có một tên miền để chia tên con cho mọi khách (máy training, hosting cho
// tên con miễn phí) thì admin khai tên miền đó ở đây: từ đó mỗi tên con trực
// tiếp của nó là của riêng một khách.

import (
	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/validate"
)

func init() { register(sharedDomainCmd) }

func sharedDomainCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "shared-domain",
		Short: "Tên miền dùng chung: tên miền mà nhiều khách cùng được tạo site ở các tên con của nó",
	}
	list := &cobra.Command{
		Use: "list", Short: "Liệt kê tên miền dùng chung", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, false, err)
			}
			st, err := openStore(cmd.Context(), e)
			if err != nil {
				return err
			}
			defer closeStore(st)
			ds, err := core.SharedSuffixes(cmd.Context(), st)
			if err != nil {
				return err
			}
			if len(ds) == 0 {
				printf(e.stdout, "%s\n", e.tr("Chưa khai tên miền dùng chung nào. Thêm bằng: sudo tpan shared-domain add <tên miền>",
					"No shared domain yet. Add one with: sudo tpan shared-domain add <domain>"))
			}
			for _, d := range ds {
				printf(e.stdout, "%s\n", d)
			}
			return nil
		},
	}
	change := func(use, short string, on bool) *cobra.Command {
		return &cobra.Command{
			Use: use, Short: short, Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := requireRoot(e, cmd); err != nil {
					return fail(e, false, err)
				}
				d, err := validate.ParseDomain(args[0])
				if err != nil {
					return fail(e, false, asRefusal(err))
				}
				st, err := openStore(cmd.Context(), e)
				if err != nil {
					return err
				}
				defer closeStore(st)
				if _, err := core.SetSharedSuffix(cmd.Context(), st, d, on); err != nil {
					return fail(e, false, err)
				}
				action := "shared-domain.rm"
				msg := e.tr("Đã bỏ "+d.String()+" khỏi tên miền dùng chung.", "Removed "+d.String()+" from the shared domains.")
				if on {
					action = "shared-domain.add"
					msg = e.tr("Đã khai "+d.String()+" là tên miền dùng chung: mỗi khách tự tạo được site ở tên con của nó (ví dụ shop."+d.String()+"). Panel nhận thay đổi trong vài giây.",
						d.String()+" is now a shared domain: each customer can create sites on its subdomains (for example shop."+d.String()+"). The panel picks this up within a few seconds.")
				}
				auditNoJob(cmd.Context(), e, st, action, d.String())
				printf(e.stdout, "%s\n", msg)
				return nil
			},
		}
	}
	c.AddCommand(list,
		change("add <tên miền>", "Khai một tên miền dùng chung", true),
		change("rm <tên miền>", "Bỏ một tên miền khỏi danh sách dùng chung", false))
	return c
}
