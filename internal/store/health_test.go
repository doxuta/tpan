package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
)

func res(id string, st checks.Status) checks.Result {
	return checks.Result{
		ID: id, Stage: checks.StageWeb, Status: st,
		Title:    i18n.T("Kiểm "+id, "Check "+id),
		Summary:  i18n.T(string(st), string(st)),
		Evidence: []checks.Evidence{{Command: "systemctl is-active nginx", Output: string(st)}},
	}
}

func record(t *testing.T, s *Store, target string, now time.Time, rs ...checks.Result) Transition {
	t.Helper()
	tr, err := s.RecordResults(context.Background(), target, rs, now)
	if err != nil {
		t.Fatalf("RecordResults: %v", err)
	}
	return tr
}

func TestRecordResultsMoDongSuCo(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	id := "service.active:nginx"

	// Lần đầu thấy mà đã Fail: mở.
	tr := record(t, s, "", t0, res(id, checks.Fail))
	if len(tr.Opened) != 1 || len(tr.Closed) != 0 {
		t.Fatalf("lần đầu Fail: %+v", tr)
	}
	inc := tr.Opened[0]
	if inc.CheckID != id || inc.Target != "" || inc.Stage != checks.StageWeb || !inc.Opened.Equal(t0) ||
		inc.OpenResult.Status != checks.Fail || inc.Title.Vi != "Kiểm "+id || inc.Jobs == nil {
		t.Errorf("sự cố mở = %+v", inc)
	}

	// Fail tiếp, Warn, Unknown: không mở thêm, không đóng.
	for i, st := range []checks.Status{checks.Fail, checks.Warn, checks.Unknown} {
		tr = record(t, s, "", t0.Add(time.Duration(i+1)*time.Minute), res(id, st))
		if len(tr.Opened)+len(tr.Closed) != 0 {
			t.Errorf("%s: không được đổi sự cố, có %+v", st, tr)
		}
	}
	open, err := s.Incidents(ctx, true, 0)
	if err != nil || len(open) != 1 {
		t.Fatalf("Incidents(open) = %v, %v", open, err)
	}

	// Pass: đóng với lý do "pass", giữ bằng chứng lúc đóng.
	tClose := t0.Add(10 * time.Minute)
	tr = record(t, s, "", tClose, res(id, checks.Pass))
	if len(tr.Closed) != 1 || len(tr.Opened) != 0 {
		t.Fatalf("Pass: %+v", tr)
	}
	c := tr.Closed[0]
	if c.ID != inc.ID || c.CloseReason != "pass" || !c.Closed.Equal(tClose) ||
		c.CloseResult == nil || c.CloseResult.Status != checks.Pass {
		t.Errorf("sự cố đóng = %+v", c)
	}

	// Fail lại: sự cố mới.
	tr = record(t, s, "", t0.Add(20*time.Minute), res(id, checks.Fail))
	if len(tr.Opened) != 1 || tr.Opened[0].ID == inc.ID {
		t.Errorf("Fail lại phải mở sự cố mới: %+v", tr)
	}
	all, err := s.Incidents(ctx, false, 0)
	if err != nil || len(all) != 2 {
		t.Fatalf("Incidents = %d, %v", len(all), err)
	}
	if !all[0].Closed.IsZero() || all[1].CloseReason != "pass" {
		t.Errorf("thứ tự phải mới nhất trước: %+v", all)
	}
	if got, _ := s.Incidents(ctx, false, 1); len(got) != 1 || got[0].ID != all[0].ID {
		t.Errorf("limit 1 = %+v", got)
	}
}

func TestRecordResultsPassLanDauKhongMo(t *testing.T) {
	s, _ := openTemp(t)
	tr := record(t, s, "", t0, res("a", checks.Pass), res("b", checks.Warn), res("c", checks.Unknown))
	if len(tr.Opened)+len(tr.Closed) != 0 {
		t.Errorf("không Fail thì không mở: %+v", tr)
	}
}

func TestRecordResultsChangedAt(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	id := "disk.usage:/"
	record(t, s, "", t0, res(id, checks.Pass))
	record(t, s, "", t0.Add(time.Minute), res(id, checks.Pass))
	r, ok, err := s.CheckResult(ctx, id)
	if err != nil || !ok {
		t.Fatalf("CheckResult = %v, %v", ok, err)
	}
	if !r.Changed.Equal(t0) || !r.Checked.Equal(t0.Add(time.Minute)) {
		t.Errorf("cùng status: changed %v (muốn %v), checked %v", r.Changed, t0, r.Checked)
	}
	record(t, s, "x.example.com", t0.Add(2*time.Minute), res(id, checks.Warn))
	r, _, _ = s.CheckResult(ctx, id)
	if !r.Changed.Equal(t0.Add(2*time.Minute)) || r.Status != checks.Warn || r.Target != "x.example.com" {
		t.Errorf("đổi status: %+v", r)
	}
	if len(r.Evidence) != 1 || r.Title.En != "Check "+id {
		t.Errorf("data phải khứ hồi đủ: %+v", r.Result)
	}
	if _, ok, err := s.CheckResult(ctx, "khong.co"); ok || err != nil {
		t.Errorf("CheckResult(không có) = %v, %v", ok, err)
	}
}

func TestCheckResultsThuTu(t *testing.T) {
	s, _ := openTemp(t)
	record(t, s, "b.example.com", t0, res("z", checks.Pass), res("a2", checks.Pass))
	record(t, s, "", t0, res("y", checks.Pass))
	record(t, s, TargetTPAN, t0, res("x", checks.Pass))
	got, err := s.CheckResults(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range got {
		ids = append(ids, r.Target+"/"+r.ID)
	}
	want := []string{"/y", "b.example.com/a2", "b.example.com/z", "tpan/x"}
	if len(ids) != len(want) {
		t.Fatalf("CheckResults = %v", ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("CheckResults = %v, muốn %v", ids, want)
			break
		}
	}
}

func TestPruneResults(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	record(t, s, "a.example.com", t0, res("http.site:a.example.com", checks.Fail), res("keep", checks.Fail))
	// Sự cố mở mà không còn dòng check_results (dữ liệu cũ): vẫn phải đóng.
	record(t, s, "", t0, res("orphan", checks.Fail))
	if _, err := s.db.ExecContext(ctx, "DELETE FROM check_results WHERE id = 'orphan'"); err != nil {
		t.Fatal(err)
	}

	tPrune := t0.Add(time.Hour)
	tr, err := s.PruneResults(ctx, []string{"keep"}, tPrune)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Closed) != 2 || len(tr.Opened) != 0 {
		t.Fatalf("Prune: %+v", tr)
	}
	for _, c := range tr.Closed {
		if c.CloseReason != "gone" || c.CloseResult != nil || !c.Closed.Equal(tPrune) {
			t.Errorf("đóng vì gone: %+v", c)
		}
	}
	rs, _ := s.CheckResults(ctx)
	if len(rs) != 1 || rs[0].ID != "keep" {
		t.Errorf("còn lại %+v", rs)
	}
	open, _ := s.Incidents(ctx, true, 0)
	if len(open) != 1 || open[0].CheckID != "keep" {
		t.Errorf("sự cố còn mở = %+v", open)
	}

	// keep rỗng: xoá hết.
	if _, err := s.PruneResults(ctx, nil, tPrune); err != nil {
		t.Fatal(err)
	}
	if rs, _ := s.CheckResults(ctx); len(rs) != 0 {
		t.Errorf("keep rỗng còn %+v", rs)
	}
}

func TestIncidentVaJobs(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	tr := record(t, s, "", t0, res("a", checks.Fail), res("b", checks.Fail))
	a, b := tr.Opened[0], tr.Opened[1]

	for i, j := range []string{"job-2", "job-1"} {
		if err := s.LinkIncidentJob(ctx, "a", j, t0.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	// Gắn lại cùng job: không lỗi, không nhân đôi.
	if err := s.LinkIncidentJob(ctx, "a", "job-2", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkIncidentJob(ctx, "b", "job-3", t0); err != nil {
		t.Fatal(err)
	}
	// Không có sự cố mở: không làm gì.
	if err := s.LinkIncidentJob(ctx, "khong.co", "job-9", t0); err != nil {
		t.Errorf("LinkIncidentJob không có sự cố: %v", err)
	}

	got, err := s.Incident(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Jobs) != 2 || got.Jobs[0] != "job-2" || got.Jobs[1] != "job-1" {
		t.Errorf("Jobs = %v, muốn [job-2 job-1] (cũ trước)", got.Jobs)
	}

	// Nhiều sự cố cùng có job: đọc danh sách không được treo (một kết nối).
	all, err := s.Incidents(ctx, false, 0)
	if err != nil || len(all) != 2 {
		t.Fatalf("Incidents = %v, %v", all, err)
	}
	for _, inc := range all {
		if inc.ID == b.ID && (len(inc.Jobs) != 1 || inc.Jobs[0] != "job-3") {
			t.Errorf("Jobs của b = %v", inc.Jobs)
		}
	}

	// Đóng rồi thì gắn job không còn vào sự cố cũ.
	record(t, s, "", t0.Add(time.Hour), res("b", checks.Pass))
	if err := s.LinkIncidentJob(ctx, "b", "job-4", t0); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Incident(ctx, b.ID); len(got.Jobs) != 1 {
		t.Errorf("sự cố đã đóng bị gắn thêm job: %v", got.Jobs)
	}

	if _, err := s.Incident(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Incident(không có) = %v, muốn ErrNotFound", err)
	}
}

func TestIntegrityCheck(t *testing.T) {
	s, _ := openTemp(t)
	got, err := s.IntegrityCheck(context.Background())
	if err != nil || got != "ok" {
		t.Errorf("IntegrityCheck = %q, %v", got, err)
	}
}

func TestStuckJobs(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	old := sampleJob("old", t0)
	fresh := sampleJob("fresh", t0.Add(2*time.Hour))
	done := sampleJob("done", t0)
	for _, j := range []engine.Job{old, fresh, done} {
		if err := s.CreateJob(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	done.Status, done.Finished = engine.JobSucceeded, t0.Add(time.Minute)
	if err := s.FinishJob(ctx, done); err != nil {
		t.Fatal(err)
	}
	got, err := s.StuckJobs(ctx, t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "old" {
		t.Errorf("StuckJobs = %+v", got)
	}
}
