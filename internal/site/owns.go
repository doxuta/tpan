package site

import (
	"path"
	"strings"
)

// Owns reports whether p is one of this site's own files: anything strictly
// below Dir or LogDir, its vhost files and links, its PHP-FPM pool file for any
// PHP version, or its cron file (pool and cron also when renamed .suspended by
// a suspension). The server uses it to decide which file contents a non-admin
// caller may see in a plan preview (M28, docs/thay-doi/M28-thiet-ke.md §3.7).
// A zero Layout owns nothing.
func (l Layout) Owns(p string) bool {
	if l.Domain.IsZero() || l.Dir == "" || l.LogDir == "" || l.Pool == "" || !path.IsAbs(p) {
		return false
	}
	p = path.Clean(p)
	if strings.HasPrefix(p, l.Dir+"/") || strings.HasPrefix(p, l.LogDir+"/") {
		return true
	}
	switch p {
	case l.NginxConf, l.NginxLink, l.ApacheConf, l.ApacheLink:
		return true
	}
	p = strings.TrimSuffix(p, ".suspended")
	if p == CronFile(l) {
		return true
	}
	// /etc/php/<version>/fpm/pool.d/<pool>.conf
	rest, ok := strings.CutPrefix(p, "/etc/php/")
	if !ok {
		return false
	}
	version, tail, ok := strings.Cut(rest, "/")
	return ok && version != "" && tail == "fpm/pool.d/"+l.Pool+".conf"
}
