package core

// `tpan self-update` (mục 6.11 của docs/KE-HOACH.md). Hợp đồng M12 đợt 0; thân
// do thợ đợt 2 viết.
//
// Ai chạy hai Plan này: CLI (`tpan self-update`), và từ M15 là lệnh nội bộ
// `tpan internal self-update` mà panel nhờ systemd khởi trong một đơn vị tạm
// (selfupdate_m15.go, mục 6.14). `tpan serve` không bao giờ tự Execute chúng:
// job kết thúc bằng khởi động lại tpan.service, và systemd dừng mọi tiến trình
// của dịch vụ đó ở đúng bước ấy, kể cả tiến trình đang chạy job. Panel chỉ gọi
// Prepare, PrepareRollback để lấy bản xem trước và lời từ chối.
//
// Plan "self.update":
//  1. steps.SelfDownload: tải <base>/tpan-linux-<Arch> và <base>/SHA256SUMS vào
//     TpanBin+".new", kiểm sha256, chạy `<file> version` để đọc bản mới. base
//     là https://...; http chỉ nhận với host loopback (e2e trên máy ảo).
//  2. steps.SelfInstall: TpanBin → TpanBin+".old", TpanBin+".new" → TpanBin
//     (rename, cùng thư mục). Undo đổi ngược.
//  3. steps.ServiceRestart{tpan, OnlyIfActive: true}.
//  Kiểm chứng: `TpanBin version` chạy được. Bản tải về trùng bản đang chạy thì
//  SelfDownload báo và các bước sau được bỏ qua (Check true).
// Plan "self.rollback": steps.SelfInstall{New: TpanBin+".old"...} đổi ngược
// rồi restart. Không có .old thì từ chối self_no_old. Từ M15: file .old không
// mở được tpan.db hiện tại thì từ chối self_rollback_db (oldOpensDB).
//
// Ghi chú khi viết thân (đợt 2):
//   - Bỏ qua khi trùng bản: bước 2 và 3 được bọc trong sameVersionSkip, đọc bản
//     SelfDownload vừa ghi. File .new cùng bản được để lại; lần self-update sau
//     tự gỡ nó trước khi tải.
//   - Rollback là SelfInstall{Bin: tpan, New: tpan.old, Old: tpan.new}: đúng
//     phép đổi ngược của bước 2 (bản vừa bị thay ra quay về tên .new).
//   - Gốc tải sai (không https, có user, query...) thì từ chối self_url_invalid
//     ngay lúc dựng Plan, thay vì để bản xem trước báo lỗi.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

const (
	tpanNew = TpanBin + ".new"
	tpanOld = TpanBin + ".old"
	// tpanDB là DB mà tpan.service mở (mục 4.4), cũng là DB mà một lệnh tpan
	// không có cờ --db mở.
	tpanDB = "/var/lib/tpan/tpan.db"
)

// baseURL là gốc tải: settings update.self_url, rỗng thì DefaultSelfURL.
func (s *SelfUpdate) baseURL(ctx context.Context) (string, error) {
	v, _, err := s.Settings.GetSetting(ctx, SettingSelfURL)
	if err != nil {
		return "", fmt.Errorf("đọc setting %s: %w", SettingSelfURL, err)
	}
	if v == "" {
		v = DefaultSelfURL
	}
	return v, nil
}

// Status trả phần self-update của màn Cập nhật. Chỉ đọc.
func (s *SelfUpdate) Status(ctx context.Context) (SelfStatus, error) {
	url, err := s.baseURL(ctx)
	if err != nil {
		return SelfStatus{}, err
	}
	hasOld, err := pathExists(s.Sys, tpanOld)
	if err != nil {
		return SelfStatus{}, err
	}
	return SelfStatus{Version: s.Version, URL: url, HasOld: hasOld}, nil
}

// Prepare dựng Plan "self.update". baseURL rỗng thì đọc settings, rồi DefaultSelfURL.
func (s *SelfUpdate) Prepare(ctx context.Context, baseURL string) (*Planned, error) {
	if baseURL == "" {
		var err error
		if baseURL, err = s.baseURL(ctx); err != nil {
			return nil, err
		}
	}
	got := new(string)
	dl := &steps.SelfDownload{Base: baseURL, Arch: s.Arch, Dest: tpanNew, NewVersion: got}
	// Check của SelfDownload kiểm gốc tải và kiến trúc trước, và trước lần tải
	// đầu nó không đụng tới máy: dùng nó để từ chối sớm.
	if _, err := dl.Check(ctx, s.Sys); err != nil {
		return nil, refuse("self_url_invalid", i18n.T(
			"không tải được bản tpan từ đây: "+err.Error(),
			"cannot download a tpan release from here: "+err.Error()))
	}
	plan := engine.Plan{
		Kind:   "self.update",
		Target: baseURL,
		Title:  i18n.T("Cập nhật tpan (đang chạy bản "+s.Version+")", "Update tpan (running "+s.Version+")"),
		Steps: []engine.Step{
			dl,
			&sameVersionSkip{Step: &steps.SelfInstall{Bin: TpanBin, New: tpanNew, Old: tpanOld}, got: got, running: s.Version},
			&sameVersionSkip{Step: &steps.ServiceRestart{Unit: "tpan", OnlyIfActive: true}, got: got, running: s.Version},
		},
		Verify: []checks.Check{tpanRuns{}},
	}
	return newPlanned(s.Sys, s.Journal, plan, nil), nil
}

// PrepareRollback dựng Plan "self.rollback". Ba điều kiện đầu, áp cho cả CLI
// lẫn panel: có file cũ (self_no_old); máy không còn lồng hay lượt thử tấn công
// chưa dọn, thứ bản trước M17 không biết (CodeCageActive, M17); file cũ mở được
// tpan.db hiện tại (CodeSelfRollbackDB, M15). Kiểm lồng trước vì nó không chạy
// file cũ và lời từ chối có lệnh gỡ chạy được ngay.
func (s *SelfUpdate) PrepareRollback(ctx context.Context) (*Planned, error) {
	ok, err := pathExists(s.Sys, tpanOld)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, refuse("self_no_old", i18n.T(
			"không có "+tpanOld+" để quay về (chưa từng self-update, hoặc đã quay về rồi)",
			"there is no "+tpanOld+" to go back to (no self-update yet, or already rolled back)"))
	}
	if err := s.refuseCageActive(ctx); err != nil {
		return nil, err
	}
	if err := s.oldOpensDB(ctx); err != nil {
		return nil, err
	}
	plan := engine.Plan{
		Kind:   "self.rollback",
		Target: TpanBin,
		Title:  i18n.T("Quay tpan về bản trước ("+tpanOld+")", "Roll tpan back to the previous release ("+tpanOld+")"),
		Steps: []engine.Step{
			&steps.SelfInstall{Bin: TpanBin, New: tpanOld, Old: tpanNew},
			&steps.ServiceRestart{Unit: "tpan", OnlyIfActive: true},
		},
		Verify: []checks.Check{tpanRuns{}},
	}
	return newPlanned(s.Sys, s.Journal, plan, nil), nil
}

// cageUnitDir là chỗ systemd đọc unit do tpan ghi cho lồng (tpan-cage-*).
const cageUnitDir = "/etc/systemd/system"

// refuseCageActive từ chối lùi bản (cage_active, mục 6 của
// docs/thay-doi/M17-thiet-ke.md) khi máy còn thứ mà bản trước M17 không biết:
// site có cờ caged (mọi trạng thái: site ở thùng rác vẫn giữ cờ), file
// tpan-cage-* trong /etc/systemd/system (lồng dựng dở cũng tính), hay lượt thử
// tấn công chưa dọn. Bản cũ sẽ ghi lại pool chung cho site đang trỏ vào lồng và
// không biết dọn phép thử. Lời từ chối liệt kê từng thứ kèm lệnh chạy được
// ngay. Sites, Runs nil: chỉ xét file trên máy.
func (s *SelfUpdate) refuseCageActive(ctx context.Context) error {
	var vi, en []string
	if s.Sites != nil {
		all, err := s.Sites.Sites(ctx)
		if err != nil {
			return fmt.Errorf("đọc danh sách site: %w", err)
		}
		// Site trong thùng rác không chặn: file của lồng đã gỡ lúc đưa vào thùng
		// rác (còn sót thì phần đọc thư mục unit bên dưới bắt). Site đang tạm
		// khoá thì cage disable từ chối, nên lời khuyên là mở khoá trước.
		var caged, suspended []string
		for _, st := range all {
			switch {
			case !st.Caged || st.Status != site.Active:
			case st.Suspended != account.NotSuspended:
				suspended = append(suspended, st.Domain.String())
			default:
				caged = append(caged, st.Domain.String())
			}
		}
		if len(caged) > 0 {
			list := strings.Join(caged, ", ")
			vi = append(vi, "site đang ở trong lồng: "+list+" (đưa ra bằng: sudo tpan cage disable --all --yes)")
			en = append(en, "caged sites: "+list+" (take them out with: sudo tpan cage disable --all --yes)")
		}
		for _, d := range suspended {
			vi = append(vi, "site trong lồng đang tạm khoá: "+d+" (mở khoá rồi đưa ra: sudo tpan site unsuspend "+d+" --yes, rồi sudo tpan cage disable "+d+" --yes)")
			en = append(en, "suspended caged site: "+d+" (unsuspend it, then take it out: sudo tpan site unsuspend "+d+" --yes, then sudo tpan cage disable "+d+" --yes)")
		}
	}
	names, err := s.Sys.ReadDir(cageUnitDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("đọc %s: %w", cageUnitDir, err)
	}
	var units []string
	for _, n := range names {
		if strings.HasPrefix(n, "tpan-cage-") {
			units = append(units, n)
		}
	}
	if len(units) > 0 {
		list := strings.Join(units, ", ")
		vi = append(vi, "file của lồng trong "+cageUnitDir+": "+list+" (gỡ bằng: sudo tpan cage disable --all --yes)")
		en = append(en, "cage files in "+cageUnitDir+": "+list+" (remove them with: sudo tpan cage disable --all --yes)")
	}
	if s.Runs != nil {
		runs, err := s.Runs.PendingProbeRuns(ctx)
		if err != nil {
			return fmt.Errorf("đọc sổ phép thử tấn công: %w", err)
		}
		if len(runs) > 0 {
			ids := make([]string, 0, len(runs))
			for _, r := range runs {
				ids = append(ids, r.ID)
			}
			list := strings.Join(ids, ", ")
			vi = append(vi, "lượt thử tấn công chưa dọn: "+list+" (dọn bằng: sudo tpan isolation probe --cleanup --yes)")
			en = append(en, "attack probe runs not cleaned up: "+list+" (clean up with: sudo tpan isolation probe --cleanup --yes)")
		}
	}
	if len(vi) == 0 {
		return nil
	}
	return refuse(CodeCageActive, i18n.T(
		"không lùi tpan về bản trước được: bản trước không biết lồng theo site và phép thử tấn công. Còn: "+strings.Join(vi, "; ")+". Làm các lệnh đó rồi chạy lại.",
		"cannot roll tpan back: the previous release knows nothing about per-site cages or the attack probe. Still present: "+strings.Join(en, "; ")+". Run those commands, then try again."))
}

// oldOpensDB thử xem file chạy cũ có mở được tpan.db hiện tại không, bằng cách
// chạy `tpan.old site list`: lệnh chỉ đọc, có từ v0.1.0, và mở DB đúng như
// dịch vụ sẽ mở sau khi quay về. Bản mới thường đã nâng cấu trúc DB; bản cũ
// gặp DB mới hơn nó biết thì thoát ngay, nên quay về là để tpan.service khởi
// động lại liên tục trong khi job báo thành công (đã gặp ở M13, mục 6.12).
// Biết chắc không lùi được thì từ chối trước khi đổi file.
//
// Chỉ thử khi máy đã có tpan.db: chưa có mà chạy `site list` thì chính file cũ
// tạo một DB rỗng, tức là bản xem trước làm đổi máy. Chưa có DB thì cũng không
// có gì để bản cũ không mở được.
func (s *SelfUpdate) oldOpensDB(ctx context.Context) error {
	hasDB, err := pathExists(s.Sys, tpanDB)
	if err != nil || !hasDB {
		return err
	}
	cmd := system.Cmd{Name: tpanOld, Args: []string{"site", "list"}}
	res, err := s.Sys.Run(ctx, cmd)
	if err == nil {
		return nil
	}
	// Lệnh bị cắt vì ctx (người dùng đóng trang, tiến trình bị dừng) không nói
	// gì về file cũ: báo lỗi thường, không báo "bản cũ không mở được DB".
	if ctx.Err() != nil {
		return fmt.Errorf("thử %s: %w", cmd.String(), err)
	}
	// File cũ in lý do ra stderr ("DB ở bản 1400, tpan này chỉ biết tới bản
	// 900..."). Không in gì (file hỏng, không chạy nổi) thì dùng lỗi của Run.
	said := oneLine(res.Stderr, selfOutputMax)
	if said == "" {
		said = oneLine(res.Stdout, selfOutputMax)
	}
	if said == "" {
		said = oneLine(err.Error(), selfOutputMax)
	}
	return refuse(CodeSelfRollbackDB, i18n.T(
		"bản trước ("+tpanOld+") không mở được "+tpanDB+" hiện tại, thường vì bản đang chạy đã nâng cấu trúc DB. Quay về lúc này thì dịch vụ tpan khởi động lại liên tục, nên chưa đổi gì. Bản trước báo: "+said+". Hai đường còn lại: chép lại bản sao "+tpanDB+" có từ trước khi nâng rồi mới quay về, hoặc ở lại bản này",
		"the previous release ("+tpanOld+") cannot open the current "+tpanDB+", usually because the running release upgraded the DB schema. Rolling back now would leave the tpan service restarting in a loop, so nothing was changed. The previous release said: "+said+". The two ways left: put back a copy of "+tpanDB+" taken before the upgrade and then roll back, or stay on this release"))
}

// sameVersionSkip bọc một bước của self.update: file vừa tải cùng bản với bản
// đang chạy thì không thay file, không khởi động lại (đã đúng, bỏ qua).
//   - *got rỗng (xem trước, chưa tải): bước sẽ chạy. Không hỏi bước bên trong,
//     vì SelfInstall.Check thấy chưa có .new sẽ tưởng là đã thay xong.
//   - *got bằng bản đang chạy: đã đúng.
//   - khác: hỏi bước bên trong.
type sameVersionSkip struct {
	engine.Step
	got     *string
	running string
}

func (w *sameVersionSkip) Check(ctx context.Context, sys system.System) (bool, error) {
	switch *w.got {
	case "":
		return false, nil
	case w.running:
		return true, nil
	}
	return w.Step.Check(ctx, sys)
}

// tpanRuns là kiểm chứng của self.update và self.rollback: file chạy ở
// TpanBin chạy được và in đúng dòng phiên bản. Không đạt thì engine hoàn tác
// (trả file cũ về chỗ).
type tpanRuns struct{}

func (tpanRuns) Run(ctx context.Context, sys system.System) checks.Result {
	cmd := system.Cmd{Name: TpanBin, Args: []string{"version"}}
	res, err := sys.Run(ctx, cmd)
	ev := checks.Evidence{Command: cmd.String(), ExitCode: res.ExitCode, Output: strings.TrimSpace(res.Stdout + res.Stderr)}
	r := checks.Result{
		ID:       "self.version",
		Stage:    checks.StageTPAN,
		Title:    i18n.T("File chạy "+TpanBin+" chạy được", "The "+TpanBin+" executable runs"),
		Evidence: []checks.Evidence{ev},
	}
	if ee := (*system.ExitError)(nil); err != nil && !errors.As(err, &ee) {
		r.Evidence[0].ExitCode, r.Evidence[0].Output = -1, err.Error()
	}
	line, _, _ := strings.Cut(strings.TrimSpace(res.Stdout), "\n")
	if err == nil && strings.HasPrefix(line, "tpan ") {
		r.Status, r.Summary = checks.Pass, i18n.T(line, line)
		return r
	}
	r.Status = checks.Fail
	r.Summary = i18n.T(TpanBin+" version không chạy hoặc in dòng lạ", TpanBin+" version does not run or prints something unexpected")
	r.Cause = i18n.T(
		"File chạy mới hỏng hoặc không hợp kiến trúc của máy. tpan tự trả file cũ về chỗ; nếu chưa được thì đổi tay như dưới.",
		"The new executable is broken or built for another architecture. tpan puts the old file back by itself; if it did not, swap them by hand as below.")
	r.ManualFix = []string{"mv " + tpanOld + " " + TpanBin, "systemctl restart tpan", TpanBin + " version"}
	return r
}
