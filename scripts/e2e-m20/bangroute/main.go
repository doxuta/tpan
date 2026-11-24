// Command bangroute in bảng route của panel (server.RouteTable(), mục 11.10 của
// docs/thay-doi/M20-thiet-ke.md) cho e2e M20 (scripts/e2e-m20.sh, T40): mỗi
// route API không công khai một dòng, placeholder đã thay bằng giá trị cho
// trước, các cột cách nhau bằng tab:
//
//	<METHOD>	<đường dẫn>	<nhóm token, "-" là chỉ phiên>	<luật>	<mẫu gốc>
//
// Hai cách dùng:
//
//	bangroute -khong-co            placeholder là giá trị KHÔNG có thật (none.example.com,
//	                               999999...): cho các request bị chặn ở lớp token (thiếu
//	                               phạm vi, hết hạn, thu hồi, route chỉ phiên). Lớp token
//	                               có lỗi thì request rơi vào 404 chứ không đổi máy.
//	bangroute -domain a.example.com -job <id> ...
//	                               giá trị có thật trong máy ảo: cho "đủ phạm vi" (chỉ GET
//	                               và vài POST ?dry_run=1, script e2e tự chọn).
//
// Dòng cuối ra stderr: số route in ra và số route bỏ qua (công khai, catch-all).
// Đồ nghề chạy trên Mac; đọc bảng route từ mã của cây (không gọi panel).
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/doxuta/tpan/internal/server"
)

// publicPaths là các route công khai (không qua guard), bỏ qua dù RouteInfo.Rule
// ghi thế nào: token trên route công khai không phải điều T40 kiểm.
var publicPaths = map[string]bool{
	"/api/v1/health": true, "/api/v1/login": true, "/api/v1/login/2fa": true,
	"/api/v1/logout": true, "/api/v1/session": true,
}

func main() {
	missing := flag.Bool("khong-co", false, "mọi placeholder là giá trị không có thật")
	v := map[string]*string{
		"domain":   flag.String("domain", "a.example.com", "{domain}"),
		"job":      flag.String("job", "x1", "{id} của /jobs/"),
		"incident": flag.String("incident", "999999", "{id} của /incidents/"),
		"backup":   flag.String("backup", "x1", "{id} của /backups/"),
		"plan":     flag.String("plan", "x1", "{id} của /plans/"),
		"ai":       flag.String("ai", "x1", "{id} của /ai/sessions/"),
		"token":    flag.String("token", "x1", "{id} của /account/tokens/, /tokens/"),
		"session":  flag.String("session", "x1", "{id} của /account/sessions/"),
		"user":     flag.String("user", "khach", "{name} của /users/"),
		"package":  flag.String("package", "default", "{name} của /packages/"),
		"db":       flag.String("db", "khong_co_db", "{name} của /dbs/, /trash/dbs/"),
		"seq":      flag.String("seq", "1", "{seq}"),
	}
	flag.Parse()
	if *missing {
		for k := range v {
			s := "999999"
			switch k {
			case "domain":
				s = "none.example.com"
			case "user", "package", "db":
				s = "khong_co"
			case "seq":
				s = "1"
			}
			*v[k] = s
		}
	}

	printed, skipped := 0, 0
	for _, r := range server.RouteTable() {
		method, path, ok := strings.Cut(r.Pattern, " ")
		if !ok || !strings.HasPrefix(path, "/api/v1/") || r.Rule == "public" || publicPaths[path] {
			skipped++
			continue
		}
		filled, err := fill(path, v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bangroute: %s: %v\n", r.Pattern, err)
			os.Exit(1)
		}
		group := r.TokenGroup
		if group == "" {
			group = "-"
		}
		fmt.Printf("%s\t%s\t%s\t%s\t%s\n", method, filled, group, r.Rule, r.Pattern)
		printed++
	}
	fmt.Fprintf(os.Stderr, "bangroute: %d route API không công khai; bỏ qua %d (công khai, catch-all)\n", printed, skipped)
}

// fill thay từng {placeholder} theo đường dẫn (như fill của authz_test.go).
func fill(path string, v map[string]*string) (string, error) {
	for strings.Contains(path, "{") {
		i := strings.Index(path, "{")
		j := strings.Index(path[i:], "}")
		if j < 0 {
			return "", fmt.Errorf("thiếu dấu }")
		}
		key := path[i+1 : i+j]
		var val string
		switch key {
		case "domain":
			val = *v["domain"]
		case "seq":
			val = *v["seq"]
		case "id":
			val = *v[idKind(path[:i])]
		case "name":
			val = *v[nameKind(path[:i])]
		default:
			return "", fmt.Errorf("placeholder {%s} chưa có giá trị trong bangroute", key)
		}
		path = path[:i] + val + path[i+j+1:]
	}
	return path, nil
}

// idKind chọn loại {id} theo phần đường dẫn đứng trước nó.
func idKind(prefix string) string {
	switch {
	case strings.HasSuffix(prefix, "/jobs/"):
		return "job"
	case strings.HasSuffix(prefix, "/incidents/"):
		return "incident"
	case strings.HasSuffix(prefix, "/backups/"):
		return "backup"
	case strings.HasSuffix(prefix, "/plans/"):
		return "plan"
	case strings.HasSuffix(prefix, "/ai/sessions/"):
		return "ai"
	case strings.HasSuffix(prefix, "/tokens/"):
		return "token"
	case strings.HasSuffix(prefix, "/sessions/"):
		return "session"
	}
	return "plan" // loại lạ: một giá trị bất kỳ
}

func nameKind(prefix string) string {
	switch {
	case strings.HasSuffix(prefix, "/packages/"):
		return "package"
	case strings.HasSuffix(prefix, "/dbs/"):
		return "db"
	}
	return "user"
}
