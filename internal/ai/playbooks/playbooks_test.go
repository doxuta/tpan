package playbooks

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestForBangDuongDan(t *testing.T) {
	cases := map[string]string{
		"/":                           "bang-dieu-khien",
		"/sites":                      "site",
		"/sites/":                     "site",
		"/sites/new":                  "site",
		"/sites/shop.example.com":     "site",
		"/sites/shop.example.com/":    "site",
		"/sites/shop.example.com/log": "site",
		"/sites?sort=name":            "site",
		"/sites/shop.example.com#x":   "site",
		"/databases":                  "co-so-du-lieu",
		"/ssl":                        "ssl",
		"/backups":                    "backup",
		"/cron":                       "cron",
		"/trash":                      "thung-rac",
		"/incidents":                  "su-co",
		"/incidents/41":               "su-co",
		"/jobs":                       "lich-su",
		"/jobs/128":                   "lich-su",
		"/stack":                      "stack",
		"/updates":                    "cap-nhat",
		"/security":                   "bao-mat",
		"/plans":                      "duyet-plan",
		"/plans/12":                   "duyet-plan",
		"/?tab=1":                     "bang-dieu-khien",
	}
	for path, want := range cases {
		name, text, ok := For(path)
		if !ok || name != want || text == "" {
			t.Errorf("For(%q) = %q, %d byte, %v; muốn %q", path, name, len(text), ok, want)
		}
	}
}

func TestForMucKhongCoBanHuongDan(t *testing.T) {
	for _, path := range []string{
		"", "/ai", "/ai/abc", "/settings", "/login", "/sitesx", "/sites//x",
		"/databases/1", "/stack/extra", "sites", "/../etc/passwd", "//",
	} {
		if name, text, ok := For(path); ok || name != "" || text != "" {
			t.Errorf("For(%q) = %q, %d byte, %v; muốn ok=false", path, name, len(text), ok)
		}
	}
}

func TestMoiTenCoFileVaMoiFileCoTen(t *testing.T) {
	got := Names()
	if len(got) != 13 {
		t.Errorf("có %d bản, muốn 13 (12 mục menu và màn duyệt Plan): %v", len(got), got)
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("Names chưa sắp: %v", got)
	}
	inNames := map[string]bool{}
	for _, n := range got {
		inNames[n] = true
		if _, err := files.ReadFile(n + ".md"); err != nil {
			t.Errorf("tên %q không có file: %v", n, err)
		}
	}
	entries, err := files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".md"); !ok || !inNames[n] {
			t.Errorf("file %q không có tên trong Names", e.Name())
		}
	}
	// Mọi đích của bảng đường dẫn phải là tên có thật.
	for _, table := range []map[string]string{exact, prefix} {
		for path, n := range table {
			if !inNames[n] {
				t.Errorf("đường dẫn %q trỏ tới %q, không có trong Names", path, n)
			}
		}
	}
	// Names trả bản sao.
	got[0] = "sửa"
	if Names()[0] == "sửa" {
		t.Error("Names trả mảng gốc, không phải bản sao")
	}
}

// toolsInPanel là công cụ AI trong panel gọi được (mục 8.1, 8.4). Bản hướng
// dẫn chỉ được nhắc đúng những tên này: nhắc công cụ không có thì AI gọi hụt.
// apply_plan cố ý không có: panel không cho AI chạy Plan.
var toolsInPanel = map[string]bool{
	"list_sites": true, "site_health": true, "stack_status": true, "machine": true,
	"tail_log": true, "managed_file": true, "list_jobs": true, "get_job": true,
	"list_plans": true, "get_plan": true, "current_view": true,
	"propose_site_add": true, "propose_site_remove": true, "propose_site_repair": true,
	"propose_stack_repair": true, "propose_health_fix": true, "propose_ssl_issue": true,
	// M9.
	"list_backups": true, "propose_backup_create": true, "propose_backup_restore": true,
}

var toolLike = regexp.MustCompile("`([a-z]+(?:_[a-z]+)+)`")

func TestNoiDungTungBan(t *testing.T) {
	for _, n := range Names() {
		b, err := files.ReadFile(n + ".md")
		if err != nil {
			t.Fatal(err)
		}
		text := string(b)
		lines := strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
		if lines < 10 || lines > 30 {
			t.Errorf("%s.md dài %d dòng, muốn 10 đến 30", n, lines)
		}
		if !strings.HasPrefix(text, "# Playbook: ") {
			t.Errorf("%s.md không mở đầu bằng \"# Playbook: \"", n)
		}
		for _, m := range toolLike.FindAllStringSubmatch(text, -1) {
			if !toolsInPanel[m[1]] {
				t.Errorf("%s.md nhắc `%s`, không phải công cụ AI trong panel gọi được", n, m[1])
			}
		}
		// Bản hướng dẫn đưa người tới đúng lệnh, không phải thứ chưa có.
		if strings.Contains(text, "apply_plan") {
			t.Errorf("%s.md nhắc apply_plan: panel không dùng", n)
		}
	}
}

func TestMucChuaCoTinhNangNoiThang(t *testing.T) {
	for _, n := range []string{"thung-rac", "cap-nhat"} {
		b, _ := files.ReadFile(n + ".md")
		text := string(b)
		if !strings.Contains(text, "does not have this operation yet") {
			t.Errorf("%s.md không nói thẳng là tpan chưa có thao tác đó", n)
		}
	}
}
