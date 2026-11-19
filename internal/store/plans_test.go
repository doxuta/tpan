package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
)

// planPreview dựng một bản xem trước có đủ loại trường (bước, lệnh, file có
// nội dung nhị phân, NoUndo) để test round trip bắt được trường nào bị rơi.
func planPreview() engine.Preview {
	return engine.Preview{
		Kind:   "site.create",
		Target: "shop.example.com",
		Title:  i18n.T("Tạo site shop.example.com", "Create site shop.example.com"),
		Steps: []engine.StepPreview{
			{
				Info: engine.StepInfo{
					ID:       "apt.install:nginx",
					Title:    i18n.T("Cài nginx", "Install nginx"),
					Why:      i18n.T("Không có nginx thì không có web server", "Without nginx there is no web server"),
					Commands: []string{"apt-get install -y nginx"},
					Danger:   true,
					NoUndo:   i18n.T("gỡ gói có thể phá gói khác", "removing a package may break others"),
				},
				Done: true,
			},
			{
				Info: engine.StepInfo{
					ID:    "nginx.vhost:shop.example.com",
					Title: i18n.T("Ghi vhost", "Write vhost"),
					Files: []engine.FileChange{{Path: "/etc/nginx/sites-available/shop.example.com", New: []byte("server {\n\tlisten 80;\n}\n\x00\xff")}},
				},
				CheckError: "nginx chưa cài",
			},
		},
	}
}

func planSample(id string, created time.Time) engine.PendingPlan {
	pv := planPreview()
	return engine.PendingPlan{
		ID:          id,
		Kind:        pv.Kind,
		Target:      pv.Target,
		Title:       pv.Title,
		Request:     json.RawMessage(`{"domain":"shop.example.com","email":"a@example.com"}`),
		Preview:     pv,
		Fingerprint: engine.Fingerprint(pv),
		Origin:      engine.OriginMCP,
		SessionID:   "sess1",
		Untrusted:   true,
		Status:      engine.PlanPending,
		Created:     created,
	}
}

func TestPlanRoundTripGiuNguyenPreviewVaFingerprint(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	in := planSample("p1", t0)
	if err := s.CreatePlan(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.Plan(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("đọc ra %+v\nmuốn    %+v", got, in)
	}
	// Điều người duyệt cần: bản xem trước đọc ra vẫn băm ra đúng dấu vân tay đã lưu.
	if fp := engine.Fingerprint(got.Preview); fp != got.Fingerprint {
		t.Errorf("Fingerprint(Preview đọc ra) = %s, cột fingerprint = %s", fp, got.Fingerprint)
	}
	if string(got.Request) != string(in.Request) {
		t.Errorf("Request = %s, muốn %s", got.Request, in.Request)
	}
}

func TestCreatePlanStatusBoTrongLaPending(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	in := planSample("p1", t0)
	in.Status = ""
	if err := s.CreatePlan(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Plan(ctx, "p1")
	if got.Status != engine.PlanPending {
		t.Errorf("Status = %q, muốn pending", got.Status)
	}
}

func TestCreatePlanTuChoi(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.CreatePlan(ctx, planSample("p1", t0)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*engine.PendingPlan){
		"trùng id":              func(p *engine.PendingPlan) {},
		"đã applied":            func(p *engine.PendingPlan) { p.ID = "p2"; p.Status = engine.PlanApplied },
		"request rỗng":          func(p *engine.PendingPlan) { p.ID = "p3"; p.Request = nil },
		"request không là JSON": func(p *engine.PendingPlan) { p.ID = "p4"; p.Request = json.RawMessage("{domain") },
		"origin lạ":             func(p *engine.PendingPlan) { p.ID = "p5"; p.Origin = "robot" },
	}
	for name, mod := range cases {
		p := planSample("p1", t0)
		mod(&p)
		if err := s.CreatePlan(ctx, p); err == nil {
			t.Errorf("%s: CreatePlan không lỗi", name)
		}
	}
	if got, _ := s.Plans(ctx, "", 0); len(got) != 1 {
		t.Errorf("sau các lần bị từ chối còn %d Plan, muốn 1", len(got))
	}
}

func TestPlanKhongCoThiErrNotFound(t *testing.T) {
	s, _ := openTemp(t)
	_, err := s.Plan(context.Background(), "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, muốn ErrNotFound", err)
	}
}

func TestPlansMoiNhatTruocVaLocTheoStatus(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	// Cố ý tạo không theo thứ tự thời gian: sắp theo created_at, không theo thứ tự chèn.
	for _, p := range []struct {
		id string
		at time.Time
	}{{"b", t0.Add(time.Minute)}, {"a", t0}, {"c", t0.Add(2 * time.Minute)}} {
		if err := s.CreatePlan(ctx, planSample(p.id, p.at)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DecidePlan(ctx, "b", engine.PlanRejected, "cli", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	ids := func(status engine.PlanStatus, limit int) []string {
		ps, err := s.Plans(ctx, status, limit)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, p := range ps {
			out = append(out, p.ID)
		}
		return out
	}
	if got, want := ids("", 0), []string{"c", "b", "a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("mọi trạng thái = %v, muốn %v", got, want)
	}
	if got, want := ids(engine.PlanPending, 0), []string{"c", "a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("pending = %v, muốn %v", got, want)
	}
	if got, want := ids(engine.PlanRejected, 0), []string{"b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rejected = %v, muốn %v", got, want)
	}
	if got, want := ids("", 2), []string{"c", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("limit 2 = %v, muốn %v", got, want)
	}
	if got := ids(engine.PlanApplied, 0); got == nil || len(got) != 0 {
		t.Errorf("applied = %v, muốn rỗng", got)
	}
}

func TestUpdatePlanPreview(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.CreatePlan(ctx, planSample("p1", t0)); err != nil {
		t.Fatal(err)
	}
	pv := planPreview()
	pv.Steps[0].Done = false
	pv.Steps = append(pv.Steps, engine.StepPreview{Info: engine.StepInfo{ID: "nginx.reload"}})
	fp := engine.Fingerprint(pv)
	if err := s.UpdatePlanPreview(ctx, "p1", pv, fp); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Plan(ctx, "p1")
	if got.Fingerprint != fp || len(got.Preview.Steps) != 3 || got.Preview.Steps[0].Done {
		t.Errorf("preview chưa được thay: %+v", got)
	}
	if engine.Fingerprint(got.Preview) != got.Fingerprint {
		t.Errorf("fingerprint lệch sau khi đọc lại")
	}

	if err := s.DecidePlan(ctx, "p1", engine.PlanApplied, "cli", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePlanPreview(ctx, "p1", pv, fp); !errors.Is(err, ErrPlanNotPending) {
		t.Errorf("Plan đã duyệt: err = %v, muốn ErrPlanNotPending", err)
	}
	if err := s.UpdatePlanPreview(ctx, "nope", pv, fp); !errors.Is(err, ErrNotFound) {
		t.Errorf("Plan không có: err = %v, muốn ErrNotFound", err)
	}
}

func TestDecidePlanGhiNguoiVaGio(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.CreatePlan(ctx, planSample("p1", t0)); err != nil {
		t.Fatal(err)
	}
	at := t0.Add(5 * time.Minute)
	if err := s.DecidePlan(ctx, "p1", engine.PlanApplied, "web:203.0.113.7", at); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Plan(ctx, "p1")
	if got.Status != engine.PlanApplied || got.DecidedBy != "web:203.0.113.7" || !got.Decided.Equal(at) {
		t.Errorf("sau duyệt: status=%s by=%q at=%v", got.Status, got.DecidedBy, got.Decided)
	}
}

func TestDecidePlanLanHaiLaErrPlanNotPending(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.CreatePlan(ctx, planSample("p1", t0)); err != nil {
		t.Fatal(err)
	}
	if err := s.DecidePlan(ctx, "p1", engine.PlanApplied, "cli", t0); err != nil {
		t.Fatal(err)
	}
	// Duyệt lần nữa, hay từ chối sau khi đã duyệt: đều bị chặn, và Plan giữ nguyên.
	for _, st := range []engine.PlanStatus{engine.PlanApplied, engine.PlanRejected} {
		err := s.DecidePlan(ctx, "p1", st, "web:198.51.100.1", t0.Add(time.Hour))
		if !errors.Is(err, ErrPlanNotPending) {
			t.Errorf("lần hai (%s): err = %v, muốn ErrPlanNotPending", st, err)
		}
		if errors.Is(err, ErrNotFound) {
			t.Errorf("lần hai (%s): không được lẫn với ErrNotFound", st)
		}
	}
	got, _ := s.Plan(ctx, "p1")
	if got.Status != engine.PlanApplied || got.DecidedBy != "cli" {
		t.Errorf("Plan bị lần hai ghi đè: status=%s by=%q", got.Status, got.DecidedBy)
	}
}

func TestDecidePlanKhongCoVaTrangThaiSai(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.DecidePlan(ctx, "nope", engine.PlanApplied, "cli", t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("Plan không có: err = %v, muốn ErrNotFound", err)
	}
	if err := s.CreatePlan(ctx, planSample("p1", t0)); err != nil {
		t.Fatal(err)
	}
	for _, st := range []engine.PlanStatus{engine.PlanPending, "", "done"} {
		if err := s.DecidePlan(ctx, "p1", st, "cli", t0); err == nil {
			t.Errorf("DecidePlan với status %q không lỗi", st)
		}
	}
	if got, _ := s.Plan(ctx, "p1"); got.Status != engine.PlanPending {
		t.Errorf("Plan bị đổi thành %s", got.Status)
	}
}

// Nhiều bên cùng bấm duyệt: đúng một bên thắng. Đây là lý do DecidePlan phải
// là một câu UPDATE có điều kiện chứ không phải đọc rồi ghi.
func TestDecidePlanDongThoiChiMotBenThang(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.CreatePlan(ctx, planSample("p1", t0)); err != nil {
		t.Fatal(err)
	}
	const n = 16
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = s.DecidePlan(ctx, "p1", engine.PlanApplied, "web:"+string(rune('a'+i)), t0)
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, ErrPlanNotPending):
			t.Errorf("bên thua nhận lỗi %v, muốn ErrPlanNotPending", err)
		}
	}
	if wins != 1 {
		t.Errorf("%d bên thắng, muốn đúng 1", wins)
	}
}

func TestReopenPlan(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	for _, id := range []string{"chua-job", "co-job", "cho-duyet", "bi-tu-choi"} {
		if err := s.CreatePlan(ctx, planSample(id, t0)); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"chua-job", "co-job"} {
		if err := s.DecidePlan(ctx, id, engine.PlanApplied, "cli", t0); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DecidePlan(ctx, "bi-tu-choi", engine.PlanRejected, "cli", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlanJob(ctx, "co-job", "job1"); err != nil {
		t.Fatal(err)
	}

	// Duyệt rồi mà job chưa bắt đầu: mở lại được, và xoá sạch dấu vết quyết định.
	if err := s.ReopenPlan(ctx, "chua-job"); err != nil {
		t.Fatalf("ReopenPlan(chua-job): %v", err)
	}
	got, _ := s.Plan(ctx, "chua-job")
	if got.Status != engine.PlanPending || !got.Decided.IsZero() || got.DecidedBy != "" || got.JobID != "" {
		t.Errorf("sau mở lại: %+v", got)
	}
	// Mở lại rồi thì duyệt được lần nữa.
	if err := s.DecidePlan(ctx, "chua-job", engine.PlanApplied, "cli", t0); err != nil {
		t.Errorf("duyệt lại sau khi mở lại: %v", err)
	}

	// Đã có job thì Plan đã chạy: không mở lại được.
	if err := s.ReopenPlan(ctx, "co-job"); !errors.Is(err, ErrPlanNotPending) {
		t.Errorf("Plan có job: err = %v, muốn ErrPlanNotPending", err)
	}
	if got, _ := s.Plan(ctx, "co-job"); got.Status != engine.PlanApplied || got.JobID != "job1" {
		t.Errorf("Plan có job bị đổi: %+v", got)
	}
	// Đang pending hay đã bị từ chối: cũng không phải trường hợp để mở lại.
	for _, id := range []string{"cho-duyet", "bi-tu-choi"} {
		if err := s.ReopenPlan(ctx, id); !errors.Is(err, ErrPlanNotPending) {
			t.Errorf("ReopenPlan(%s): err = %v, muốn ErrPlanNotPending", id, err)
		}
	}
	if got, _ := s.Plan(ctx, "bi-tu-choi"); got.Status != engine.PlanRejected {
		t.Errorf("Plan bị từ chối bị mở lại: %s", got.Status)
	}
	if err := s.ReopenPlan(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Plan không có: err = %v, muốn ErrNotFound", err)
	}
}

func TestSetPlanJob(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.CreatePlan(ctx, planSample("p1", t0)); err != nil {
		t.Fatal(err)
	}
	// Chưa duyệt thì chưa có job để gán.
	if err := s.SetPlanJob(ctx, "p1", "job1"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Plan pending: err = %v, muốn lỗi thường", err)
	}
	if err := s.DecidePlan(ctx, "p1", engine.PlanApplied, "cli", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlanJob(ctx, "p1", ""); err == nil {
		t.Error("job id rỗng không lỗi")
	}
	if err := s.SetPlanJob(ctx, "p1", "job1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Plan(ctx, "p1"); got.JobID != "job1" {
		t.Errorf("JobID = %q, muốn job1", got.JobID)
	}
	if err := s.SetPlanJob(ctx, "nope", "job1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Plan không có: err = %v, muốn ErrNotFound", err)
	}
}
