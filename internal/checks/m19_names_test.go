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
