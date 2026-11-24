package checks

import (
	"maps"
	"testing"
)

func TestDeclaredNames(t *testing.T) {
	nginx := `nginx: the configuration file /etc/nginx/nginx.conf syntax is ok
# configuration file /etc/nginx/sites-enabled/tpan-example.com.conf:
server {
    listen 80;
    server_name example.com www.example.com;
}
# configuration file /etc/nginx/sites-enabled/webmail.conf:
server {
    listen 80;
    server_name webmail.example.com Example.com.;
}
`
	apache := `VirtualHost configuration:
*:80                   is a NameVirtualHost
         port 80 namevhost example.com (/etc/apache2/sites-enabled/tpan-example.com.conf:2)
                 alias www.example.com
         port 80 namevhost webmail.example.com (/etc/apache2/sites-enabled/webmail.conf:1)
`
	cases := []struct {
		web, dump string
		want      map[string]string
		ok        bool
	}{
		{"nginx", nginx, map[string]string{
			"example.com":         "/etc/nginx/sites-enabled/tpan-example.com.conf",
			"www.example.com":     "/etc/nginx/sites-enabled/tpan-example.com.conf",
			"webmail.example.com": "/etc/nginx/sites-enabled/webmail.conf",
		}, true},
		{"apache", apache, map[string]string{
			"example.com":         "/etc/apache2/sites-enabled/tpan-example.com.conf",
			"www.example.com":     "/etc/apache2/sites-enabled/tpan-example.com.conf",
			"webmail.example.com": "/etc/apache2/sites-enabled/webmail.conf",
		}, true},
		{"nginx", "garbage", nil, false},
		{"apache", "garbage", nil, false},
		{"caddy", nginx, nil, false},
	}
	for _, c := range cases {
		got, ok := DeclaredNames(c.web, c.dump)
		if ok != c.ok || !maps.Equal(got, c.want) {
			t.Errorf("DeclaredNames(%s) = %v, %v; muốn %v, %v", c.web, got, ok, c.want, c.ok)
		}
	}
}

// TestDeclaredNamesWildcard (soát cuối C9): tên khớp wildcard của một vhost
// khác coi như đã khai ở file đó.
func TestDeclaredNamesWildcard(t *testing.T) {
	nginx := `# configuration file /etc/nginx/sites-enabled/tpan-shop.example.org.conf:
server {
    listen 80;
    server_name shop.example.org;
}
# configuration file /etc/nginx/sites-enabled/wild.conf:
server {
    listen 80;
    server_name *.example.com .example.net mail.example.* _ ~^(?<u>.+)\.example\.org$;
}
`
	apache := `VirtualHost configuration:
*:80                   is a NameVirtualHost
         port 80 namevhost tools.example.org (/etc/apache2/sites-enabled/tools.conf:1)
                 alias *.example.com
                 alias t?.example.net
`
	ng, ok := DeclaredNames("nginx", nginx)
	if !ok {
		t.Fatal("nginx: không đọc được")
	}
	ap, ok := DeclaredNames("apache", apache)
	if !ok {
		t.Fatal("apache: không đọc được")
	}
	const wild, tools = "/etc/nginx/sites-enabled/wild.conf", "/etc/apache2/sites-enabled/tools.conf"
	cases := []struct {
		decl map[string]string
		name string
		want string
	}{
		{ng, "x.example.com", wild},
		{ng, "a.b.example.com", wild},
		{ng, "example.com", ""}, // *.x không khớp chính x
		{ng, "example.net", wild},
		{ng, "www.example.net", wild},
		{ng, "mail.example.org", wild},
		{ng, "shop.example.org", "/etc/nginx/sites-enabled/tpan-shop.example.org.conf"}, // khớp đúng thắng regex
		{ng, "x.example.org", ""}, // regex bị bỏ qua
		{ng, "_", wild},           // chỉ khớp đúng chữ "_"
		{ng, "x.example.co", ""},
		{ap, "x.example.com", tools},
		{ap, "tt.example.net", tools},
		{ap, "ttt.example.net", ""},
		{ap, "tools.example.org", tools},
	}
	for _, c := range cases {
		if got := DeclaredFile(c.decl, c.name); got != c.want {
			t.Errorf("DeclaredFile(%s) = %q; muốn %q", c.name, got, c.want)
		}
	}
}
