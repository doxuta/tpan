package core

// F1 của rà soát v0.2.0 (M28 đợt 2): site mới nhận UID chưa từng cấp, và xoá
// hẳn dọn crontab, tiến trình, file tạm của user trước userdel.

import (
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

func TestF1XoaHanDonTruocUserdelVaKhongDungLaiUID(t *testing.T) {
	const a, b = "a.example.com", "b.example.com"
	be := newBktEnv(t, a)
	se := be.siteEnv
	se.s.Backups = be.bs
	la, lb := site.LayoutOf(mustDomain(t, a)), site.LayoutOf(mustDomain(t, b))

	useradd := func(l site.Layout, uid string) string {
		return "useradd --system --uid " + uid + " --gid " + uid + " --home-dir " + l.Dir + " --no-create-home --shell /usr/sbin/nologin " + l.User
	}
	if !slices.Contains(ranStrs(se.m.Ran()), useradd(la, "65536")) {
		t.Fatalf("site A không tạo user với UID 65536: %v", ranStrs(se.m.Ran()))
	}

	// Site A để lại crontab riêng; /tmp, /var/tmp có trên máy.
	crontab := "/var/spool/cron/crontabs/" + la.User
	se.m.SetFile(crontab, []byte("* * * * * /var/tmp/x\n"), 0o600, system.Root)
	se.m.Handle([]string{"crontab", "-r", "-u", la.User}, func(system.Cmd) system.Result {
		_ = se.m.Remove(crontab)
		return system.Result{}
	})
	se.m.SetDir("/tmp", 0o777, system.Root)
	se.m.SetDir("/var/tmp", 0o777, system.Root)

	se.trash(t, a)
	p, err := se.s.PreparePurge(ctx, mustDomain(t, a))
	if err != nil {
		t.Fatal(err)
	}
	se.m.ResetRan()
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("xoá hẳn: %v %s (%s)", err, rep.Job.Status, rep.Job.Error)
	}
	// Máy giả trả UID 998 cho mọi user (getent của siteSim).
	want := []string{
		"crontab -r -u " + la.User,
		"pgrep -U 998", "pgrep -u 998",
		"find /tmp -xdev -uid 998 '!' -type d -delete",
		"find /tmp -xdev -uid 998 -type d -empty -delete",
		"find /var/tmp -xdev -uid 998 '!' -type d -delete",
		"find /var/tmp -xdev -uid 998 -type d -empty -delete",
		"userdel " + la.User,
	}
	var got []string
	for _, c := range ranStrs(se.m.Ran()) {
		if slices.Contains(want, c) {
			got = append(got, c)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("lệnh dọn và xoá user:\n%s\nmuốn đúng thứ tự:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if se.exists(crontab) {
		t.Error("crontab riêng của user còn")
	}

	// Site B của người khác tạo sau: số mới, không phải số của A.
	se.m.ResetRan()
	se.add(t, b)
	if !slices.Contains(ranStrs(se.m.Ran()), useradd(lb, "65537")) {
		t.Errorf("site B không nhận UID 65537: %v", ranStrs(se.m.Ran()))
	}
}

func ranStrs(cs []system.Cmd) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.String()
	}
	return out
}
