package htaccess

import (
	"reflect"
	"strings"
	"testing"
)

// Apache bỏ lặng lẽ phần vượt quá 64 tên trên một dòng AllowOverrideList.
func TestAllowedVuaMotDong(t *testing.T) {
	if len(Allowed) != 55 {
		t.Errorf("Allowed có %d tên, đặc tả (mục 6.13) ghi 55; đổi danh sách thì đổi cả đặc tả", len(Allowed))
	}
	if len(Allowed) > MaxNames {
		t.Fatalf("Allowed có %d tên, Apache chỉ nhận %d tên trên một dòng AllowOverrideList", len(Allowed), MaxNames)
	}
	seen := map[string]bool{}
	for _, n := range Allowed {
		low := strings.ToLower(n)
		if seen[low] {
			t.Errorf("tên %q lặp lại", n)
		}
		seen[low] = true
		if strings.ContainsAny(n, " \t\n>") {
			t.Errorf("tên %q có ký tự không hợp lệ", n)
		}
	}
	if got := len(strings.Fields(List())); got != len(Allowed) {
		t.Errorf("List() có %d từ, muốn %d", got, len(Allowed))
	}
}

// Các chỉ thị đổi được handler hay biến gửi sang PHP-FPM (L4), và Options
// nguyên chỉ thị (L9), không bao giờ được nằm trong danh sách.
func TestAllowedKhongCoChiThiCam(t *testing.T) {
	for _, bad := range []string{"SetHandler", "AddHandler", "RemoveHandler", "Action", "ProxyFCGISetEnvIf", "ProxyFCGIBackendType", "Options"} {
		if containsFold(Allowed, bad) {
			t.Errorf("Allowed chứa %s", bad)
		}
	}
	for _, bad := range []string{"FollowSymLinks", "ExecCGI", "Includes", "All"} {
		if containsFold(AllowedOptions, bad) {
			t.Errorf("AllowedOptions chứa %s", bad)
		}
	}
	if got, want := OverrideOptions(), "Options=Indexes,MultiViews,SymLinksIfOwnerMatch"; got != want {
		t.Errorf("OverrideOptions() = %q, muốn %q", got, want)
	}
}

func TestScan(t *testing.T) {
	const f = "/var/www/a.example.com/public/.htaccess"
	cases := []struct {
		name string
		in   string
		want []Finding
	}{
		{"rỗng", "", nil},
		{"bản chuẩn của WordPress", "# BEGIN WordPress\nRewriteEngine On\nRewriteRule .* - [E=HTTP_AUTHORIZATION:%{HTTP:Authorization}]\nRewriteBase /\nRewriteRule ^index\\.php$ - [L]\nRewriteCond %{REQUEST_FILENAME} !-f\nRewriteCond %{REQUEST_FILENAME} !-d\nRewriteRule . /index.php [L]\n# END WordPress\n", nil},
		{"thứ plugin hay ghi", "Options -Indexes\n<IfModule mod_headers.c>\n    Header set X-Thu \"1\"\n</IfModule>\n<FilesMatch \"\\.(ini|log)$\">\n  Require all denied\n</FilesMatch>\nErrorDocument 404 /index.php\nAddType image/webp .webp\n", nil},
		{"L3 chỉ dùng SetEnv thì qua (vhost gỡ biến)", "SetEnv PHP_ADMIN_VALUE \"open_basedir=/\"\n", nil},
		{"L4", "\n<Files \"qua.php\">\n    SetHandler \"proxy:unix:/run/php/x.sock|fcgi://localhost\"\n    ProxyFCGISetEnvIf \"true\" SCRIPT_FILENAME \"/x\"\n</Files>\n", []Finding{
			{f, 3, "SetHandler"}, {f, 4, "ProxyFCGISetEnvIf"},
		}},
		{"không phân biệt hoa thường, thụt lề bằng tab", "\tsethandler none\nREWRITEENGINE on\n", []Finding{{f, 1, "sethandler"}}},
		{"dòng nối bằng gạch chéo", "RewriteCond %{HTTP_HOST} a \\\n  [NC]\nAddHandler x \\\n  .php\nAction a /b\n", []Finding{{f, 3, "AddHandler"}, {f, 5, "Action"}}},
		{"chú thích và dòng trắng", "  # SetHandler trong chú thích\n\n\r\n#AddHandler\n", nil},
		{"khối lạ, khối không tham số", "<Directory /x>\n</Directory>\n<RequireAll>\nRequire all granted\n</RequireAll>\n", []Finding{{f, 1, "<Directory"}}},
		{"L9: Options không dấu", "Options FollowSymLinks\n", []Finding{{f, 1, "Options FollowSymLinks"}}},
		{"Options có dấu và nhiều tham số", "Options +FollowSymLinks -Indexes +ExecCGI -MultiViews\n", []Finding{{f, 1, "Options FollowSymLinks"}, {f, 1, "Options ExecCGI"}}},
		{"Options được phép", "Options None\nOptions +SymLinksIfOwnerMatch -multiviews\noptions indexes\n", nil},
		{"Options All", "Options All\n", []Finding{{f, 1, "Options All"}}},
		{"dòng kiểu Windows", "RewriteEngine On\r\nphp_value memory_limit 256M\r\n", []Finding{{f, 2, "php_value"}}},
	}
	for _, c := range cases {
		if got := Scan(f, []byte(c.in)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n  được %+v\n  muốn %+v", c.name, got, c.want)
		}
	}
}
