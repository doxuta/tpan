package demo

// M28 đợt 2: máy demo trả lời như máy thật đã cài bằng bản vá.
//
//   - MariaDB: mọi user DB có trần 20 kết nối (CREATE USER ... WITH
//     MAX_USER_CONNECTIONS, site.DBMaxUserConnections), max_connections là 151
//     (mặc định của MariaDB). Hai câu hỏi: của phép kiểm db.user_limit
//     (checks.DBUserLimit) và của Step mariadb.user-limit (steps.DBUserLimitState).
//   - Apache: `apachectl -M` in các module đang bật (m10.mods). status_module
//     chỉ có khi ai đó bật lại: cài stack và chuyển sang Apache tắt nó
//     (steps.ApacheModulesOff).

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// demoMaxConnections là max_connections mặc định của MariaDB.
const demoMaxConnections = 151

var (
	// checks.DBUserLimit: "SELECT 'limit', max_user_connections FROM mysql.user
	// WHERE User='<u>' ...; SELECT 'max', @@GLOBAL.max_connections;".
	sqlLimitCheck = regexp.MustCompile(`SELECT 'limit', max_user_connections FROM mysql\.user WHERE User='([a-z0-9_]+)'`)
	// steps.DBUserLimitState: "SELECT COUNT(*), COALESCE(MAX(max_user_connections), 0)
	// FROM mysql.user WHERE User='<u>' ...".
	sqlLimitState = regexp.MustCompile(`COALESCE\(MAX\(max_user_connections\), 0\) FROM mysql\.user WHERE User='([a-z0-9_]+)'`)
)

// mariadbLimit trả lời hai câu đọc trần kết nối. ok=false: không phải câu của
// nó. Người gọi giữ m.mu.
func (m *machine) mariadbLimit(sql string) (system.Result, bool) {
	if g := sqlLimitCheck.FindStringSubmatch(sql); g != nil {
		out := ""
		if m.dbUsers[g[1]] {
			out = fmt.Sprintf("limit\t%d\n", site.DBMaxUserConnections)
		}
		return exit(0, out+fmt.Sprintf("max\t%d\n", demoMaxConnections), ""), true
	}
	if g := sqlLimitState.FindStringSubmatch(sql); g != nil {
		if m.dbUsers[g[1]] {
			return exit(0, fmt.Sprintf("1\t%d\n", site.DBMaxUserConnections), ""), true
		}
		return exit(0, "0\t0\n", ""), true
	}
	return system.Result{}, false
}

// apachectlM là `apachectl -M`: "Loaded Modules:" rồi mỗi module một dòng
// "<tên>_module (shared)", như Apache thật (bỏ các module static).
func (x *m10) apachectlM(system.Cmd) system.Result {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.missing["apache2"] {
		return exit(127, "", "apachectl: command not found\n")
	}
	var out strings.Builder
	out.WriteString("Loaded Modules:\n core_module (static)\n")
	mods := make([]string, 0, len(x.mods))
	for mod := range x.mods {
		mods = append(mods, mod)
	}
	sort.Strings(mods)
	for _, mod := range mods {
		out.WriteString(" " + mod + "_module (shared)\n")
	}
	return exit(0, out.String(), "")
}
