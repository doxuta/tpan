package core

// Ca sử dụng của kho S3 (M18): xem, đặt, gỡ, thử đích (mục 3.3 của
// docs/thay-doi/M18-thiet-ke.md), lượt đẩy và dọn trên kho (3.5, 3.7), tải về
// và liệt kê (3.6), đầu vào của hai phép kiểm health (3.13). Đọc đích, khoá và
// dựng kho nằm ở remote_target.go; gói panel ở panel.go. Các điểm R1 tới R16
// của bản soát hẹp (mục 9.2) đè lên mục 3.x và đã áp ở đây.
//
// Hai khoá, không bao giờ chéo nhau:
//   - khoá lượt đẩy (r.locker(), flock riêng): Sync và Pull giữ suốt lượt; đặt,
//     gỡ đích và sinh khoá sao lưu giữ nó qua Planned.remoteLock (R4);
//   - khoá job của máy (System.Lock): job đẩy, tải, gói KHÔNG lấy (B10), vì
//     chúng chạy hàng giờ mà không đổi gì của máy ngoài thư mục backup.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/agebox"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// remoteJobKinds là ba loại job chỉ chạy dưới khoá lượt đẩy.
var remoteJobKinds = []string{"backup.push", "backup.pull", "backup.panel"}

// heldLockSystem là System của job chạy dưới khoá lượt đẩy: Lock không làm
// gì. Lượt đẩy đã giữ khoá riêng của nó; lấy thêm khoá job của máy thì trong
// lúc tải vài GB, mọi thao tác khác của panel bị từ chối và health bỏ mọi vòng
// kiểm (B10).
type heldLockSystem struct{ system.System }

func (heldLockSystem) Lock(context.Context) (func(), error) { return func() {}, nil }

// ================================================================ settings

// remoteState là các setting trạng thái của kho (mục 3.2). Đây là giá trị chỉ
// tpan ghi; giá trị hỏng được đọc theo hướng an toàn (giờ hỏng là không lùi,
// panel_last hỏng là chưa có gói) thay vì làm hỏng cả lượt: tệ nhất là thêm
// một lần thử.
type remoteState struct {
	panelLast    *PanelInfo
	panelError   string
	targetError  string
	panelDue     bool
	relinkDue    bool
	retryAt      time.Time
	panelRetryAt time.Time
}

func (r *Remote) getSetting(ctx context.Context, k string) (string, error) {
	v, _, err := r.Settings.GetSetting(ctx, k)
	if err != nil {
		return "", fmt.Errorf("đọc setting %s: %w", k, err)
	}
	return v, nil
}

// setSettings ghi từng cặp khoá, giá trị.
func (r *Remote) setSettings(ctx context.Context, kv ...string) error {
	for i := 0; i+1 < len(kv); i += 2 {
		if err := r.Settings.SetSetting(ctx, kv[i], kv[i+1]); err != nil {
			return fmt.Errorf("ghi setting %s: %w", kv[i], err)
		}
	}
	return nil
}

func (r *Remote) readState(ctx context.Context) (remoteState, error) {
	var st remoteState
	vals := map[string]string{}
	for _, k := range []string{SettingS3PanelLast, SettingS3PanelError, SettingS3TargetError, SettingS3PanelDue,
		SettingS3RelinkDue, SettingS3RetryAt, SettingS3PanelRetryAt} {
		v, err := r.getSetting(ctx, k)
		if err != nil {
			return st, err
		}
		vals[k] = v
	}
	st.panelLast = parsePanelLast(vals[SettingS3PanelLast])
	st.panelError, st.targetError = vals[SettingS3PanelError], vals[SettingS3TargetError]
	st.panelDue, st.relinkDue = vals[SettingS3PanelDue] == "1", vals[SettingS3RelinkDue] == "1"
	st.retryAt, _ = time.Parse(time.RFC3339, vals[SettingS3RetryAt])
	st.panelRetryAt, _ = time.Parse(time.RFC3339, vals[SettingS3PanelRetryAt])
	return st, nil
}

// parsePanelLast đọc "<tên>|<RFC 3339>|<sha256>"; nil khi rỗng hay hỏng.
func parsePanelLast(s string) *PanelInfo {
	parts := strings.Split(s, "|")
	if len(parts) != 3 {
		return nil
	}
	at, err := time.Parse(time.RFC3339, parts[1])
	if err != nil || !panelBundleName.MatchString(parts[0]) {
		return nil
	}
	return &PanelInfo{Name: parts[0], At: at, SHA256: parts[2]}
}

// backoffFor là nhịp lùi sau n lượt hỏng liên tiếp (n từ 1).
func backoffFor(n int) time.Duration {
	return RemoteBackoff[min(max(n, 1), len(RemoteBackoff))-1]
}

// bump tăng bộ đếm countKey và đặt retryKey = now + nhịp lùi.
func (r *Remote) bump(ctx context.Context, countKey, retryKey string) error {
	v, err := r.getSetting(ctx, countKey)
	if err != nil {
		return err
	}
	n, _ := strconv.Atoi(v) // hỏng thì như 0: lùi ngắn nhất, không kẹt
	n++
	return r.setSettings(ctx, countKey, strconv.Itoa(n), retryKey, r.now().Add(backoffFor(n)).UTC().Format(time.RFC3339))
}

// failMsg là câu lỗi một dòng, đã làm sạch, để ghi vào settings, dòng backups
// và SyncResult. Lỗi của client S3 đã không có khoá (s3.Error); lỗi đọc file
// bí mật chỉ nêu đường dẫn (remote_target.go). CleanMessage bỏ xuống dòng của
// errors.Join và cắt cho gọn.
func failMsg(err error) string {
	var re *RefusedError
	if errors.As(err, &re) {
		return s3.CleanMessage(re.Message.Vi)
	}
	return s3.CleanMessage(err.Error())
}

// canceled: người gọi đã huỷ. Không phải lỗi của kho hay của bản: không ghi
// gì vào dòng nào, không lùi.
func canceled(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || s3.Classify(err) == s3.CodeCanceled
}

// targetFailed ghi lỗi của đích và nhịp lùi (bước 7 của lượt đẩy).
func (r *Remote) targetFailed(ctx context.Context, res *SyncResult, err error) error {
	res.TargetError = failMsg(err)
	if werr := r.setSettings(ctx, SettingS3TargetError, res.TargetError); werr != nil {
		return werr
	}
	return r.bump(ctx, SettingS3TargetFailures, SettingS3RetryAt)
}

// targetOK xoá lỗi và nhịp lùi của đích. Chỉ gọi khi một job đẩy hay job gói
// đạt (R2a): Check đạt chưa nói được gì, vì kho có thể cho LIST mà từ chối PUT.
func (r *Remote) targetOK(ctx context.Context) error {
	return r.setSettings(ctx, SettingS3TargetError, "", SettingS3TargetFailures, "", SettingS3RetryAt, "")
}

// ================================================================ khoá sao lưu

type keyState int

const (
	keyNone keyState = iota // chưa có file
	keyOK
	keyBad // file có mà không đọc được hay sai dạng
)

// backupKeyState không trả lỗi: file khoá hỏng là một trạng thái để báo
// (panel_error, tpan.s3:fresh), không phải lý do dừng lượt đẩy bản của site.
func (r *Remote) backupKeyState() keyState {
	k, ok, err := readBackupKey(r.Sys)
	switch {
	case err != nil:
		return keyBad
	case !ok:
		return keyNone
	case agebox.CheckKey(k) != nil:
		return keyBad
	}
	return keyOK
}

// ================================================================ xem đích

// targetLabel là "<host>/<bucket>/<tiền tố>" để hiển thị (không có khoá).
func targetLabel(c TargetConfig) string {
	host := c.Endpoint
	if _, after, ok := strings.Cut(host, "://"); ok {
		host = after
	}
	return host + "/" + c.Bucket + "/" + c.Prefix
}

// Target là phần xem của đích (mục 3.3): không trường nào mang khoá.
func (r *Remote) Target(ctx context.Context) (TargetView, error) {
	c, ok, err := r.Config(ctx)
	if err != nil {
		return TargetView{}, err
	}
	v := TargetView{Configured: ok}
	if k, has, err := readBackupKey(r.Sys); err == nil && has && agebox.CheckKey(k) == nil {
		v.HasBackupKey, v.KeyFingerprint = true, agebox.Fingerprint(k)
	}
	if !ok {
		return v, nil
	}
	v.Endpoint, v.Region, v.Bucket, v.Prefix = c.Endpoint, c.Region, c.Bucket, c.Prefix
	v.PathStyle, v.Keep, v.TargetID = c.PathStyle, c.Keep, c.ID
	access, _, hasKeys, err := readKeys(r.Sys)
	if err != nil {
		return TargetView{}, err
	}
	if hasKeys {
		if v.AccessKeyTail = s3.AccessKeyTail(access); v.AccessKeyTail == "" {
			v.AccessKeyLen = len(access)
		}
	}
	if _, v.HasCA, err = readSecret(r.Sys, steps.S3CAFile); err != nil {
		return TargetView{}, err
	}
	st, err := r.readState(ctx)
	if err != nil {
		return TargetView{}, err
	}
	v.PanelLast, v.PanelError, v.PanelDue = st.panelLast, st.panelError, st.panelDue
	v.TargetError, v.RetryAt = st.targetError, st.retryAt
	if r.Store == nil {
		return v, nil
	}
	rows, err := r.Store.Backups(ctx, "")
	if err != nil {
		return TargetView{}, fmt.Errorf("đọc bảng backups: %w", err)
	}
	for _, b := range rows {
		switch {
		case b.Remote == site.RemoteStored && b.RemoteTarget == c.ID && !b.Local:
			v.RemoteOnly++
		case b.Remote == site.RemoteStored && b.RemoteTarget != c.ID:
			v.OtherTarget++
		case b.Remote == site.RemoteFailed:
			v.Failed++
		}
	}
	return v, nil
}

// ================================================================ đặt đích

// PrepareSetTarget dựng Plan "backup.target.set" (mục 3.3, R1, R4, R5).
func (r *Remote) PrepareSetTarget(ctx context.Context, in TargetInput) (*TargetSet, error) {
	ep, err := s3.CheckEndpoint(in.Endpoint)
	if err != nil {
		return nil, s3Refuse(err)
	}
	region, err := s3.CheckRegion(in.Region)
	if err != nil {
		return nil, s3Refuse(err)
	}
	if err := s3.CheckBucket(in.Bucket); err != nil {
		return nil, s3Refuse(err)
	}
	prefix := in.Prefix
	if prefix == "" {
		prefix = s3.DefaultPrefix(r.Hostname)
	}
	if err := s3.CheckPrefix(prefix); err != nil {
		return nil, s3Refuse(err)
	}
	keep := in.Keep
	if keep == 0 {
		keep = s3.DefaultKeep
	}
	if err := s3.CheckKeep(keep); err != nil {
		return nil, s3Refuse(err)
	}
	curCA, _, err := readSecret(r.Sys, steps.S3CAFile)
	if err != nil {
		return nil, err
	}
	var ca []byte
	var caStep engine.Step
	switch in.CA {
	case CAKeep:
		ca = curCA
	case CASet:
		if len(in.CAPEM) == 0 {
			return nil, refuse(s3.CodeCAInvalid, i18n.T("chưa đưa chứng chỉ gốc (file PEM rỗng)", "no CA certificate given (empty PEM file)"))
		}
		if err := s3.CheckCA(in.CAPEM); err != nil {
			return nil, s3Refuse(err)
		}
		ca = in.CAPEM
		caStep = &steps.SecretFile{Path: steps.S3CAFile, Content: system.NewSecret(string(ca)),
			Title: i18n.T("Ghi chứng chỉ gốc riêng của kho "+steps.S3CAFile, "Write the store's private CA certificate "+steps.S3CAFile),
			Why: i18n.T("Kho dùng chứng chỉ do tổ chức tự cấp; không có chứng chỉ gốc này thì tpan từ chối nói chuyện với kho vì không kiểm được nó là thật.",
				"The store uses a certificate from a private CA; without this root tpan refuses to talk to the store because it cannot verify it is genuine.")}
	case CANone:
		caStep = &steps.SecretAbsent{Path: steps.S3CAFile,
			Title: i18n.T("Bỏ chứng chỉ gốc riêng "+steps.S3CAFile, "Remove the private CA certificate "+steps.S3CAFile),
			Why: i18n.T("Đích mới dùng chứng chỉ công khai; để lại chứng chỉ gốc cũ thì tpan chỉ tin chứng chỉ đó và không nối được tới đích mới.",
				"The new target uses a public certificate; leaving the old root makes tpan trust only it and fail to reach the new target.")}
	default:
		return nil, fmt.Errorf("đặt đích: CAMode %d không hợp lệ", in.CA)
	}
	access, secret := in.AccessKey, in.SecretKey
	if in.KeepKeys {
		// R1: giữ khoá đang lưu chỉ khi đích không đổi chỗ. Kiểm TRƯỚC khi dựng
		// kho: một phiên admin bị chiếm mà trỏ endpoint về máy của nó sẽ nhận
		// được request ký bằng khoá thật (access key đầy đủ, chữ ký để dò
		// secret) ngay ở điều kiện đầu, cả lúc xem trước.
		cur, ok, err := r.Config(ctx)
		if err != nil {
			return nil, err
		}
		if !ok || cur.Endpoint != ep || cur.Region != region || cur.Bucket != in.Bucket || !bytes.Equal(curCA, ca) {
			return nil, refuse(s3.CodeKeysInvalid, i18n.T(
				"đổi endpoint, vùng, bucket hay CA thì gửi lại cả hai khoá (access key và secret key)",
				"changing the endpoint, region, bucket or CA requires sending both keys again (access key and secret key)"))
		}
		a, s, has, err := readKeys(r.Sys)
		if err != nil {
			return nil, refuse(s3.CodeKeysInvalid, i18n.T("không đọc được khoá đang lưu: "+err.Error()+"; gửi lại cả hai khoá",
				"cannot read the stored keys: "+err.Error()+"; send both keys again"))
		}
		if !has {
			return nil, refuse(s3.CodeKeysInvalid, i18n.T("chưa có khoá truy cập nào đang lưu; gửi cả access key và secret key",
				"no access keys are stored yet; send both the access key and the secret key"))
		}
		access, secret = a, s
	} else {
		if err := s3.CheckAccessKey(access); err != nil {
			return nil, s3Refuse(err)
		}
		if err := s3.CheckSecretKey(secret.Reveal()); err != nil {
			return nil, s3Refuse(err)
		}
	}
	c := TargetConfig{Endpoint: ep, Region: region, Bucket: in.Bucket, Prefix: prefix, PathStyle: in.PathStyle, Keep: keep,
		ID: s3.TargetID(ep, in.Bucket, prefix)}
	// Điều kiện đầu: thử thật trước khi ghi gì (chạy cả lúc xem trước).
	store, err := r.buildStore(s3Config(c, access, secret, ca, r.Now))
	if err != nil {
		return nil, s3Refuse(err)
	}
	if err := store.Check(ctx); err != nil {
		return nil, s3Refuse(err)
	}
	keys, err := encodeKeys(access, secret)
	if err != nil {
		return nil, err
	}
	label := targetLabel(c)
	stepList := []engine.Step{
		// SecretFile cần /etc/tpan có sẵn (root 0700 từ M3; máy mới thì tạo).
		&steps.Dir{Path: "/etc/tpan", Perm: 0o700, Owner: system.Root},
		&steps.S3Probe{Store: store, Label: label},
		&steps.SecretFile{Path: steps.S3KeysFile, Content: keys,
			Title: i18n.T("Ghi hai khoá truy cập kho vào "+steps.S3KeysFile, "Write the two store access keys to "+steps.S3KeysFile),
			Why: i18n.T("Lượt đẩy chạy nền cần khoá để ký mọi request tới kho. Khoá chỉ nằm trong file root 0600 này, không trong tpan.db, không trong bản xem trước.",
				"The background push needs the keys to sign every request to the store. They live only in this root 0600 file, not in tpan.db, not in the preview.")},
	}
	if caStep != nil {
		stepList = append(stepList, caStep)
	}
	ts := &TargetSet{}
	if in.GenerateKey {
		_, has, err := readBackupKey(r.Sys)
		if err != nil {
			return nil, err
		}
		if !has {
			k, err := agebox.GenerateKey()
			if err != nil {
				return nil, err
			}
			ts.NewKey = k
			stepList = append(stepList, backupKeyStep(k))
		}
	}
	plan := engine.Plan{
		Kind:   "backup.target.set",
		Target: c.Endpoint + " " + c.Bucket + " " + c.Prefix,
		Title:  i18n.T("Đặt đích sao lưu S3 "+label, "Set the S3 backup target "+label),
		Steps:  stepList,
	}
	ts.Planned = newPlanned(r.Sys, r.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		res, err := r.afterSetTarget(ctx, store, c)
		if err != nil {
			return fmt.Errorf("%w; chạy lại lệnh (an toàn: mọi bước so nội dung trước khi ghi)", err)
		}
		ts.Result = &res
		return nil
	})
	ts.remoteLock, ts.remoteWait = r.locker(), r.lockWait()
	return ts, nil
}

// backupKeyStep ghi khoá sao lưu mới.
func backupKeyStep(k system.Secret) engine.Step {
	return &steps.SecretFile{Path: steps.BackupKeyFile, Content: k,
		Title: i18n.T("Ghi khoá sao lưu mới vào "+steps.BackupKeyFile, "Write a new backup key to "+steps.BackupKeyFile),
		Why: i18n.T("Gói panel (tpan.db) được mã hoá bằng khoá này trước khi lên kho. Không có khoá thì không có gói panel; mất máy là mất danh sách user, gói và cài đặt. Chép khoá ra ngoài máy ngay khi tpan in nó.",
			"The panel bundle (tpan.db) is encrypted with this key before it goes to the store. Without the key there is no panel bundle; losing the machine loses users, plans and settings. Copy the key off the machine as soon as tpan prints it.")}
}

// afterSetTarget ghi setting của đích mới rồi nối lại (R5: cờ bền relink_due,
// nên lần chạy lại sau khi nối lại hỏng vẫn nối tiếp).
func (r *Remote) afterSetTarget(ctx context.Context, store steps.ObjectStore, c TargetConfig) (TargetSetResult, error) {
	old, hadOld, oerr := r.Config(ctx)
	changed := oerr != nil || !hadOld || old.ID != c.ID
	ps := "0"
	if c.PathStyle {
		ps = "1"
	}
	kv := []string{
		SettingS3Endpoint, c.Endpoint, SettingS3Region, c.Region, SettingS3Bucket, c.Bucket,
		SettingS3Prefix, c.Prefix, SettingS3PathStyle, ps, SettingS3Keep, strconv.Itoa(c.Keep),
		SettingS3TargetError, "", SettingS3TargetFailures, "", SettingS3RetryAt, "",
		SettingS3PanelFailures, "", SettingS3PanelRetryAt, "", SettingS3PanelDue, "1",
	}
	if changed {
		// Gói panel mới nhất nằm ở đích cũ: với đích mới là chưa có gói nào.
		kv = append(kv, SettingS3PanelLast, "", SettingS3PanelError, "", SettingS3RelinkDue, "1")
	}
	if err := r.setSettings(ctx, kv...); err != nil {
		return TargetSetResult{}, err
	}
	due := changed
	if !due {
		v, err := r.getSetting(ctx, SettingS3RelinkDue)
		if err != nil {
			return TargetSetResult{}, err
		}
		due = v == "1"
	}
	res, err := r.relink(ctx, store, c.ID, due)
	if err != nil {
		return res, err
	}
	if due {
		if err := r.setSettings(ctx, SettingS3RelinkDue, ""); err != nil {
			return res, err
		}
	}
	return res, nil
}

// relink nối lại (S18): dòng stored ở mã khác mà object cùng khoá, đúng cỡ có
// trên đích mới thì đổi remote_target sang mã mới (đích đổi cách viết mà vẫn
// là cùng một kho). list=false thì chỉ đếm, không gọi kho.
func (r *Remote) relink(ctx context.Context, store steps.ObjectStore, id string, list bool) (TargetSetResult, error) {
	var res TargetSetResult
	if r.Store == nil {
		return res, nil
	}
	rows, err := r.Store.Backups(ctx, "")
	if err != nil {
		return res, fmt.Errorf("đọc bảng backups để nối lại: %w", err)
	}
	var others []Backup
	for _, b := range rows {
		if b.Remote == site.RemoteStored && b.RemoteTarget != id {
			others = append(others, b)
		}
	}
	if !list || len(others) == 0 {
		res.OtherTarget = len(others)
		return res, nil
	}
	sizes := map[string]int64{}
	if err := store.List(ctx, "sites/", func(o s3.Object) error {
		sizes[o.Key] = o.Size
		return nil
	}); err != nil {
		return res, fmt.Errorf("liệt kê sites/ trên đích mới để nối lại: %w", err)
	}
	for _, b := range others {
		if sz, ok := sizes[b.RemoteKey]; ok && sz == b.Size {
			if err := r.Store.SetBackupRemoteTarget(ctx, b.ID, id); err != nil {
				return res, err
			}
			res.Relinked++
			continue
		}
		res.OtherTarget++
	}
	return res, nil
}

// PrepareRemoveTarget dựng "backup.target.rm" (mục 3.3).
func (r *Remote) PrepareRemoveTarget(ctx context.Context) (*Planned, error) {
	c, ok, err := r.Config(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errNotConfigured()
	}
	only := 0
	if r.Store != nil {
		rows, err := r.Store.Backups(ctx, "")
		if err != nil {
			return nil, fmt.Errorf("đọc bảng backups: %w", err)
		}
		for _, b := range rows {
			if b.Remote == site.RemoteStored && b.RemoteTarget == c.ID && !b.Local {
				only++
			}
		}
	}
	label := targetLabel(c)
	n := strconv.Itoa(only)
	plan := engine.Plan{
		Kind:   "backup.target.rm",
		Target: "s3",
		Title: i18n.T("Gỡ đích sao lưu S3 "+label+" ("+n+" bản chỉ còn trên kho sẽ không tải về được cho tới khi đặt lại đích này)",
			"Remove the S3 backup target "+label+" ("+n+" copies that only exist on the store cannot be downloaded until this target is set again)"),
		Steps: []engine.Step{
			&steps.SecretAbsent{Path: steps.S3KeysFile,
				Title: i18n.T("Gỡ hai khoá truy cập kho "+steps.S3KeysFile+" ("+n+" bản chỉ còn trên kho)",
					"Remove the two store access keys "+steps.S3KeysFile+" ("+n+" copies only on the store)"),
				Why: i18n.T("Không còn đích thì khoá không còn việc gì trên máy. Gỡ đích không xoá gì trên kho, không xoá khoá sao lưu.",
					"Without a target the keys have no use on the machine. Removing the target deletes nothing on the store and keeps the backup key.")},
			&steps.SecretAbsent{Path: steps.S3CAFile},
		},
	}
	p := newPlanned(r.Sys, r.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		var kv []string
		for _, k := range []string{SettingS3Endpoint, SettingS3Region, SettingS3Bucket, SettingS3Prefix, SettingS3PathStyle,
			SettingS3Keep, SettingS3TargetError, SettingS3TargetFailures, SettingS3RetryAt, SettingS3PanelLast,
			SettingS3PanelError, SettingS3PanelDue, SettingS3PanelFailures, SettingS3PanelRetryAt, SettingS3RelinkDue} {
			kv = append(kv, k, "")
		}
		return r.setSettings(ctx, kv...)
	})
	p.remoteLock, p.remoteWait = r.locker(), r.lockWait()
	return p, nil
}

// PrepareTestTarget dựng "backup.target.test" (một S3Probe trên đích đang lưu).
func (r *Remote) PrepareTestTarget(ctx context.Context) (*Planned, error) {
	store, c, err := r.open(ctx)
	if err != nil {
		return nil, err
	}
	label := targetLabel(c)
	plan := engine.Plan{
		Kind:   "backup.target.test",
		Target: "s3",
		Title:  i18n.T("Thử đích sao lưu S3 "+label, "Test the S3 backup target "+label),
		Steps:  []engine.Step{&steps.S3Probe{Store: store, Label: label}},
	}
	return newPlanned(r.Sys, r.Journal, plan, nil), nil
}

// PrepareNewKey dựng "backup.key.new" (chỉ CLI, mục 3.8).
func (r *Remote) PrepareNewKey(ctx context.Context) (*KeyNew, error) {
	k, err := agebox.GenerateKey()
	if err != nil {
		return nil, err
	}
	plan := engine.Plan{
		Kind:   "backup.key.new",
		Target: "backup.key",
		Title:  i18n.T("Sinh khoá sao lưu mới", "Generate a new backup key"),
		Steps:  []engine.Step{&steps.Dir{Path: "/etc/tpan", Perm: 0o700, Owner: system.Root}, backupKeyStep(k)},
	}
	p := newPlanned(r.Sys, r.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		// Gói mang khoá mới cần có sớm: người ta đổi khoá vì khoá cũ đã mất hay lộ.
		return r.setSettings(ctx, SettingS3PanelDue, "1")
	})
	p.remoteLock, p.remoteWait = r.locker(), r.lockWait()
	return &KeyNew{Planned: p, Key: k}, nil
}

// ================================================================ luật giữ bản

// remoteSurvives là luật duy nhất "bản này sống qua lần dọn kho" (mục 3.5 và
// R8), dùng cho cả chọn bản cần đẩy lẫn pruneRemote: hai bên dùng chung một
// hàm thì bản vừa bị dọn không bao giờ được chọn đẩy lại (B14).
//   - before-purge: luôn sống (đường lùi duy nhất của site đã xoá hẳn).
//   - manual, schedule: sống khi số dòng S (cùng domain, full, lý do manual
//     hay schedule, stored ở targetID, mới hơn row) nhỏ hơn keep.
//   - lý do khác (before-restore, before-import): sống khi không có dòng full
//     nào của domain mới hơn row mà cùng lý do. Nhờ vậy mỗi lần thử khôi phục
//     không đẩy bản lịch cũ nhất khỏi kho (R8).
//
// "Mới hơn": Created lớn hơn; cùng giờ thì đứng trước trong rows (thứ tự của
// Store.Backups, mới nhất trước).
func remoteSurvives(row Backup, rows []Backup, targetID string, keep int) bool {
	if row.Reason == ReasonBeforePurge {
		return true
	}
	pos := slices.IndexFunc(rows, func(b Backup) bool { return b.ID == row.ID })
	if pos < 0 {
		pos = len(rows)
	}
	newer := func(i int, s Backup) bool {
		return s.Created.After(row.Created) || (s.Created.Equal(row.Created) && i < pos)
	}
	scheduled := func(x BackupReason) bool { return x == ReasonManual || x == ReasonSchedule }
	n := 0
	for i, s := range rows {
		if s.ID == row.ID || s.Domain != row.Domain || s.Kind != BackupFull || !newer(i, s) {
			continue
		}
		if !scheduled(row.Reason) {
			if s.Reason == row.Reason {
				return false
			}
			continue
		}
		if scheduled(s.Reason) && s.Remote == site.RemoteStored && s.RemoteTarget == targetID {
			n++
		}
	}
	return !scheduled(row.Reason) || n < keep
}

// pushBlock trả lý do row không được đẩy (rỗng: được), và here: row đã
// stored ở đích này (không phải lỗi; lượt tự động bỏ qua, Only thì Head lại).
func pushBlock(row Backup, rows []Backup, c TargetConfig, retry bool) (why string, here bool) {
	switch {
	case row.Kind != BackupFull:
		return "không phải bản đầy đủ (bản xuất DB riêng không lên kho)", false
	case !row.Local:
		return "không còn trên máy", false
	case !remoteSurvives(row, rows, c.ID, c.Keep):
		return fmt.Sprintf("nằm ngoài s3.keep: kho chỉ giữ %d bản mới nhất của %s", c.Keep, row.Domain), false
	case row.Remote == site.RemoteStored && row.RemoteTarget == c.ID:
		return "", true
	case !retry && row.RemoteTries >= RemoteMaxTries:
		return fmt.Sprintf("đã đẩy lỗi %d lần liên tiếp; đẩy lại bằng: sudo tpan backup push %s", row.RemoteTries, row.ID), false
	}
	return "", false
}

// pruneRemote dọn trên kho của một domain (mục 3.7): dòng full, stored ở đích
// này mà remoteSurvives sai thì xoá object rồi ForgetBackupRemote. Xoá object
// trước, sửa DB sau: hỏng giữa chừng thì lần sau làm lại (xoá object đã mất là
// thành công). Tính trên một lần đọc là đủ: bản bị dọn luôn cũ hơn keep bản
// còn sống, nên bỏ nó không làm bản cũ hơn nữa sống lại.
func (r *Remote) pruneRemote(ctx context.Context, store steps.ObjectStore, c TargetConfig, domain string) (int, error) {
	rows, err := r.Store.Backups(ctx, domain)
	if err != nil {
		return 0, fmt.Errorf("dọn kho của %s: %w", domain, err)
	}
	n := 0
	var errs []error
	for _, b := range rows {
		if b.Domain != domain || b.Kind != BackupFull || b.Remote != site.RemoteStored || b.RemoteTarget != c.ID ||
			remoteSurvives(b, rows, c.ID, c.Keep) {
			continue
		}
		if err := store.Delete(ctx, b.RemoteKey); err != nil {
			errs = append(errs, fmt.Errorf("dọn bản %s của %s khỏi kho: %w", b.ID, domain, err))
			continue
		}
		if _, err := r.Store.ForgetBackupRemote(ctx, b.ID); err != nil {
			errs = append(errs, fmt.Errorf("dọn bản %s của %s (object đã xoá khỏi kho): %w", b.ID, domain, err))
			continue
		}
		n++
	}
	if n > 0 {
		// R3b: cờ bền, để gói panel kế tiếp ghi đúng bảng backups sau khi dọn.
		errs = append(errs, r.setSettings(context.WithoutCancel(ctx), SettingS3PanelDue, "1"))
	}
	return n, errors.Join(errs...)
}

// ================================================================ đẩy một bản

// pushHolder giữ lỗi thật của job đẩy (hay đẩy gói) để Sync phân loại: engine
// chỉ để lại chuỗi trong Report, mà chuỗi thì không errors.Is được.
type pushHolder struct {
	err    error
	stored bool
	pruned int
}

func (h *pushHolder) keep(err error) {
	if err != nil && h.err == nil {
		h.err = err
	}
}

// capturePush là S3Push giữ lại lỗi của Apply.
type capturePush struct {
	*steps.S3Push
	h *pushHolder
}

func (c *capturePush) Apply(ctx context.Context, sys system.System) error {
	err := c.S3Push.Apply(ctx, sys)
	c.h.keep(err)
	return err
}

// remoteObjectCheck là kiểm chứng của backup.push: HEAD object, cỡ khớp. Kiểm
// bằng một lời gọi khác với lời đã ghi (S3Push đã đọc lại cả object; đây là
// bằng chứng trong job rằng object còn đó sau bước).
type remoteObjectCheck struct {
	store steps.ObjectStore
	id    string
	key   string
	size  int64
	h     *pushHolder
}

func (c remoteObjectCheck) Run(ctx context.Context, _ system.System) checks.Result {
	res := checks.Result{
		ID:    "s3.object:" + c.id,
		Stage: checks.StageTPAN,
		Title: i18n.T("Kho có "+c.key+" đúng cỡ", "The store has "+c.key+" with the right size"),
	}
	ev := checks.Evidence{Command: "s3 head " + c.key}
	obj, err := c.store.Head(ctx, c.key)
	switch {
	case err != nil:
		c.h.keep(err)
		ev.ExitCode, ev.Output = 1, failMsg(err)
		res.Summary = i18n.T("không xem được "+c.key+" trên kho", "cannot look up "+c.key+" on the store")
	case obj.Size != c.size:
		c.h.keep(fmt.Errorf("%w: kho báo %s có %d byte, bản có %d byte", steps.ErrRemoteMismatch, c.key, obj.Size, c.size))
		ev.ExitCode, ev.Output = 1, fmt.Sprintf("size %d", obj.Size)
		res.Summary = i18n.T(fmt.Sprintf("%s trên kho có %d byte, bản có %d byte", c.key, obj.Size, c.size),
			fmt.Sprintf("%s on the store has %d bytes, the copy has %d", c.key, obj.Size, c.size))
	default:
		ev.Output = fmt.Sprintf("size %d", obj.Size)
		res.Evidence, res.Status = []checks.Evidence{ev}, checks.Pass
		res.Summary = i18n.T(fmt.Sprintf("kho có %s, %d byte", c.key, obj.Size), fmt.Sprintf("the store has %s, %d bytes", c.key, obj.Size))
		return res
	}
	res.Evidence, res.Status = []checks.Evidence{ev}, checks.Fail
	res.Cause = i18n.T("Kho không giữ được object vừa tải lên, hoặc không trả lời. tpan xoá object và thử lại ở lượt sau.",
		"The store did not keep the object just uploaded, or did not answer. tpan deletes the object and retries in a later run.")
	res.ManualFix = []string{"sudo tpan backup target test", "sudo tpan backup push " + c.id}
	return res
}

// preparePush dựng "backup.push" cho một dòng trên kho đã dựng.
func (r *Remote) preparePush(_ context.Context, store steps.ObjectStore, c TargetConfig, row Backup) (*Planned, *pushHolder, error) {
	if row.Kind != BackupFull || !row.Local {
		return nil, nil, refuse("backup_not_found", i18n.T(
			"bản "+row.ID+" không phải bản đầy đủ còn trên máy; xem các bản bằng: sudo tpan backup list",
			"backup "+row.ID+" is not a full backup on this machine; list the copies with: sudo tpan backup list"))
	}
	d, err := validate.ParseDomain(row.Domain)
	if err != nil {
		return nil, nil, fmt.Errorf("dòng backup %s có domain hỏng: %w", row.ID, err)
	}
	key := "sites/" + d.String() + "/" + path.Base(row.Path)
	if err := s3.CheckKey(key); err != nil {
		return nil, nil, fmt.Errorf("dòng backup %s: %w", row.ID, err)
	}
	h := &pushHolder{}
	push := &capturePush{S3Push: &steps.S3Push{Store: store, ID: row.ID, Key: key, Path: row.Path, Size: row.Size, SHA256: row.SHA256}, h: h}
	plan := engine.Plan{
		Kind:   "backup.push",
		Target: d.String(),
		Title:  i18n.T("Đẩy bản sao lưu "+row.ID+" của "+d.String()+" lên kho", "Upload backup "+row.ID+" of "+d.String()+" to the store"),
		Steps:  []engine.Step{push},
		Verify: []checks.Check{remoteObjectCheck{store: store, id: row.ID, key: key, size: row.Size, h: h}},
	}
	p := newPlanned(heldLockSystem{r.Sys}, r.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		if r.Store == nil {
			return errors.New("ghi bản đã lên kho: chưa có DB của tpan")
		}
		// Object đã đọc lại khớp sha256: ghi nhận cả khi người dùng vừa huỷ,
		// không thì lần sau tải lên lại cả file.
		dbctx := context.WithoutCancel(ctx)
		ok, err := r.Store.MarkBackupStored(dbctx, row.ID, key, c.ID, r.now())
		if err != nil {
			return fmt.Errorf("ghi bản %s đã lên kho: %w", row.ID, err)
		}
		if !ok {
			// Dòng đã mất (dọn trên máy chen vào): object không còn ai trỏ tới.
			if err := store.Delete(ctx, key); err != nil {
				return fmt.Errorf("bản %s đã bị dọn khỏi máy trong lúc đẩy; xoá object %s: %w", row.ID, key, err)
			}
			return nil
		}
		h.stored = true
		// R3b: cờ bền cho gói panel; R2a: chỉ job đẩy đạt mới xoá lỗi của đích.
		if err := r.setSettings(dbctx, SettingS3PanelDue, "1"); err != nil {
			return err
		}
		if err := r.targetOK(dbctx); err != nil {
			return err
		}
		h.pruned, err = r.pruneRemote(ctx, store, c, row.Domain)
		return err
	})
	p.owner = account.AdminName
	return p, h, nil
}

// PreparePush dựng "backup.push" cho một bản. Job chỉ được chạy dưới khoá lượt
// đẩy (Sync làm việc đó); Planned không lấy khoá job của máy.
func (r *Remote) PreparePush(ctx context.Context, id string) (*Planned, error) {
	store, c, err := r.open(ctx)
	if err != nil {
		return nil, err
	}
	row, err := r.findBackup(ctx, id)
	if err != nil {
		return nil, err
	}
	p, _, err := r.preparePush(ctx, store, c, row)
	return p, err
}

// findBackup đọc một dòng theo id; không có: backup_not_found.
func (r *Remote) findBackup(ctx context.Context, id string) (Backup, error) {
	notFound := refuse("backup_not_found", i18n.T(
		"không có bản sao lưu "+id+"; xem các bản bằng: sudo tpan backup list",
		"there is no backup "+id+"; list the copies with: sudo tpan backup list"))
	if !backupIDRe.MatchString(id) {
		return Backup{}, refuse("backup_not_found", i18n.T("id backup không đúng dạng (chữ số hex)", "the backup id is not in the right form (hex digits)"))
	}
	if r.Store == nil {
		return Backup{}, notFound
	}
	row, ok, err := r.Store.Backup(ctx, id)
	if err != nil {
		return Backup{}, fmt.Errorf("đọc backup %s: %w", id, err)
	}
	if !ok {
		return Backup{}, notFound
	}
	return row, nil
}

// ================================================================ lượt đẩy

// syncItem là một bản trong danh sách cần đẩy của lượt.
type syncItem struct {
	id, domain string
	only       bool
}

// pickPush là việc 6 của lượt: danh sách cần đẩy (mới nhất trước) và các id
// của Only không qua luật chọn (R9).
func pickPush(rows []Backup, c TargetConfig, opt SyncOptions) ([]syncItem, []SyncFailure) {
	var todo []syncItem
	var failed []SyncFailure
	want := map[string]bool{}
	for _, id := range opt.Only {
		want[id] = true
	}
	found := map[string]bool{}
	for _, b := range rows {
		if len(want) > 0 && !want[b.ID] {
			continue
		}
		found[b.ID] = true
		why, here := pushBlock(b, rows, c, opt.Retry)
		switch {
		case why != "" && len(want) > 0:
			failed = append(failed, SyncFailure{ID: b.ID, Domain: b.Domain, Error: why})
		case why != "", here && len(want) == 0:
		default:
			todo = append(todo, syncItem{id: b.ID, domain: b.Domain, only: len(want) > 0})
		}
	}
	for _, id := range opt.Only {
		if !found[id] {
			found[id] = true
			failed = append(failed, SyncFailure{ID: id, Error: "không có bản sao lưu này"})
		}
	}
	return todo, failed
}

// cleanPanelTmp là việc 5 (S29): thư mục tạm ".tmp-<giờ>" của gói panel cũ
// hơn một giờ còn sót khi tiến trình bị giết giữa chừng; trong đó là bản rõ
// của tpan.db. Tuổi đọc từ tên (giờ UTC do PanelBundle đặt), vì FileInfo
// không có mtime. Lỗi bỏ qua: lượt sau dọn lại, và không mất dữ liệu nào.
func (r *Remote) cleanPanelTmp(ctx context.Context, now time.Time) {
	names, err := r.Sys.ReadDir(site.TPANDBBackupDir)
	if err != nil {
		return
	}
	for _, n := range names {
		stamp, ok := strings.CutPrefix(n, ".tmp-")
		if !ok {
			continue
		}
		t, err := time.Parse("20060102-150405", stamp)
		if err != nil || now.Sub(t) <= time.Hour {
			continue
		}
		_ = (&steps.RemoveTree{Path: site.TPANDBBackupDir + "/" + n, Under: site.TPANDBBackupDir}).Apply(ctx, r.Sys)
	}
}

// panelWanted: lượt này cần làm gói panel (việc 9, R3). ks != keyNone để
// khoá hỏng cũng được báo vào panel_error.
func panelWanted(st remoteState, ks keyState, now time.Time, retry bool) bool {
	if ks == keyNone || (!retry && now.Before(st.panelRetryAt)) {
		return false
	}
	return st.panelDue || st.panelError != "" || st.panelLast == nil || now.Sub(st.panelLast.At) > PanelMaxAge
}

// Sync là một lượt đẩy (mục 3.5, mười việc theo thứ tự; R2, R3, R4, R9, R12,
// R15c của bản soát hẹp).
func (r *Remote) Sync(ctx context.Context, opt SyncOptions, onEvent func(engine.Event)) (SyncResult, error) {
	res := SyncResult{Pushed: []string{}, Failed: []SyncFailure{}, Reports: []engine.Report{}}
	// 1. Đọc trước khoá chỉ để thoát nhanh khi chưa đặt đích.
	if _, ok, err := r.Config(ctx); err != nil {
		return res, err
	} else if !ok {
		res.Skipped = "chưa đặt đích S3"
		return res, nil
	}
	// 2. Trước khi lấy khoá: lượt này có bị bỏ thì lượt sau vẫn thấy cờ.
	if opt.PanelDue {
		if err := r.setSettings(ctx, SettingS3PanelDue, "1"); err != nil {
			return res, err
		}
	}
	// 3.
	var wait time.Duration
	if opt.Wait {
		wait = r.lockWait()
	}
	unlock, ok, err := r.locker().Acquire(ctx, wait)
	if err != nil {
		return res, fmt.Errorf("lấy khoá lượt đẩy: %w", err)
	}
	if !ok {
		res.Skipped = "một lượt đẩy khác đang chạy"
		return res, nil
	}
	defer unlock()
	if r.Store == nil {
		return res, errors.New("lượt đẩy: chưa có DB của tpan")
	}
	// R12: đang giữ khoá lượt đẩy thì không job đẩy, tải, gói nào còn sống.
	if _, err := r.Store.AbortStaleJobs(ctx, remoteJobKinds, "đứt vì tiến trình dừng giữa chừng"); err != nil {
		slog.Warn("lượt đẩy: không đóng được job đứt", "err", err)
	}
	// R4: đích đọc lại sau khi giữ khoá (target set, rm có thể vừa xong); chỉ
	// dùng bản đọc này.
	cfg, ok, err := r.Config(ctx)
	if err != nil {
		return res, err
	}
	if !ok {
		res.Skipped = "chưa đặt đích S3"
		return res, nil
	}
	st, err := r.readState(ctx)
	if err != nil {
		return res, err
	}
	// 4.
	now := r.now()
	if !opt.Retry && now.Before(st.retryAt) {
		res.Skipped = "đang lùi sau lỗi của đích tới " + st.retryAt.Format(time.RFC3339)
		return res, nil
	}
	res.Ran = true
	// 5.
	r.cleanPanelTmp(ctx, now)
	// 6. Không có việc thì không gọi mạng: bộ đẩy nền chạy 2 phút một lần.
	rows, err := r.Store.Backups(ctx, "")
	if err != nil {
		return res, fmt.Errorf("đọc bảng backups: %w", err)
	}
	todo, failed := pickPush(rows, cfg, opt)
	res.Failed = append(res.Failed, failed...)
	ks := r.backupKeyState()
	if len(todo) == 0 && !panelWanted(st, ks, now, opt.Retry) {
		return res, nil
	}
	// 7. Lỗi bất kỳ ở đây (đọc khoá, dựng client, Check) là lỗi của đích.
	store, _, err := r.open(ctx)
	if err == nil {
		err = store.Check(ctx)
	}
	if err != nil {
		if canceled(ctx, err) {
			return res, err
		}
		return res, r.targetFailed(ctx, &res, err)
	}
	// 8.
	_, _ = store.AbortUploads(ctx, "", 24*time.Hour) // S29; kho không hỗ trợ thì thôi
	var errs []error
	stop := false
	for _, it := range todo {
		if stop {
			break
		}
		if ctx.Err() != nil {
			return res, errors.Join(append(errs, ctx.Err())...)
		}
		fail := func(row Backup, cause error) {
			if canceled(ctx, cause) {
				stop = true // R2: huỷ thì không ghi gì vào dòng
				return
			}
			msg := failMsg(cause)
			res.Failed = append(res.Failed, SyncFailure{ID: row.ID, Domain: row.Domain, Error: msg})
			if !s3.IsTargetError(cause) {
				// R2c: lỗi không phải của đích là của riêng bản (file cục bộ hỏng,
				// kho trả dữ liệu khác, kho từ chối đúng object này): đếm.
				errs = append(errs, r.Store.MarkBackupPushFailed(ctx, row.ID, cfg.ID, msg, true))
				return
			}
			// R2b: lỗi của đích sau khi Check của lượt đã đạt. Check lại: vẫn
			// đạt thì kho từ chối riêng bản này (PUT 503 mãi, kho đầy): đếm,
			// để không có vòng đẩy lại không trần. Cả hai ca ngắt cầu dao.
			countTry, terr := true, cause
			if cerr := store.Check(ctx); cerr != nil {
				if canceled(ctx, cerr) {
					stop = true
					return
				}
				countTry, terr = false, cerr
			}
			errs = append(errs, r.Store.MarkBackupPushFailed(ctx, row.ID, cfg.ID, msg, countTry), r.targetFailed(ctx, &res, terr))
			stop = true
		}
		// R15c: đọc lại các dòng của domain trước mỗi bản; bản vừa thành ngoài
		// s3.keep (vì bản mới hơn vừa lên kho) thì bỏ, không tải lên rồi dọn ngay.
		drows, err := r.Store.Backups(ctx, it.domain)
		if err != nil {
			errs = append(errs, fmt.Errorf("đọc backup của %s: %w", it.domain, err))
			continue
		}
		i := slices.IndexFunc(drows, func(b Backup) bool { return b.ID == it.id })
		if i < 0 {
			if it.only {
				res.Failed = append(res.Failed, SyncFailure{ID: it.id, Domain: it.domain, Error: "bản đã bị dọn khỏi máy trong lượt này"})
			}
			continue
		}
		row := drows[i]
		why, here := pushBlock(row, drows, cfg, opt.Retry)
		if why != "" || (here && !it.only) {
			if why != "" && it.only {
				res.Failed = append(res.Failed, SyncFailure{ID: row.ID, Domain: row.Domain, Error: why})
			}
			continue
		}
		if here {
			// R9: đẩy lại một bản đã stored ở đích này chỉ khi object mất hay sai cỡ.
			obj, err := store.Head(ctx, row.RemoteKey)
			switch {
			case err == nil && obj.Size == row.Size:
				res.Pushed = append(res.Pushed, row.ID)
				continue
			case err != nil && !errors.Is(err, s3.ErrNotFound):
				fail(row, err)
				continue
			}
			if _, err := r.Store.ForgetBackupRemote(ctx, row.ID); err != nil {
				errs = append(errs, err)
				continue
			}
			row.Remote, row.RemoteKey, row.RemoteTarget = site.RemoteNone, "", ""
		}
		if opt.Retry && row.RemoteTries > 0 {
			if err := r.Store.ResetBackupTries(ctx, row.ID); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		p, h, err := r.preparePush(ctx, store, cfg, row)
		if err != nil {
			res.Failed = append(res.Failed, SyncFailure{ID: row.ID, Domain: row.Domain, Error: failMsg(err)})
			continue
		}
		rep, err := p.Execute(ctx, onEvent)
		if rep.Job.ID == "" {
			// Không tạo được job (journal hỏng): bản sau cũng sẽ vậy.
			res.Failed = append(res.Failed, SyncFailure{ID: row.ID, Domain: row.Domain, Error: failMsg(err)})
			errs = append(errs, err)
			break
		}
		res.Reports = append(res.Reports, rep)
		if rep.Job.Status == engine.JobSucceeded {
			if err != nil {
				// Object đã lên kho; chỉ phần ghi sau (dọn kho, settings) lỗi.
				res.Failed = append(res.Failed, SyncFailure{ID: row.ID, Domain: row.Domain, Error: failMsg(err)})
				continue
			}
			res.Pushed = append(res.Pushed, row.ID)
			continue
		}
		cause := h.err
		if cause == nil {
			cause = errors.New(rep.Job.Error)
		}
		fail(row, cause)
	}
	if ctx.Err() != nil {
		return res, errors.Join(append(errs, ctx.Err())...)
	}
	// 9. Gói panel ở cuối lượt, sau mọi lần đẩy và dọn. Chỉ đọc setting (R3b):
	// after của job đẩy và lần dọn kho đã bật cờ panel_due.
	if res.TargetError == "" && !stop {
		if st, err = r.readState(ctx); err != nil {
			return res, errors.Join(append(errs, err)...)
		}
		if panelWanted(st, ks, now, opt.Retry) {
			errs = append(errs, r.syncPanel(ctx, &res, store, cfg, ks, onEvent))
		}
	}
	return res, errors.Join(errs...)
}

// syncPanel chạy job backup.panel trong lượt và ghi kết quả (R3a).
func (r *Remote) syncPanel(ctx context.Context, res *SyncResult, store steps.ObjectStore, cfg TargetConfig, ks keyState, onEvent func(engine.Event)) error {
	if ks == keyBad {
		// Không tạo job: khoá hỏng thì gói nào cũng hỏng. Câu lỗi chỉ nêu
		// đường dẫn, không bao giờ nội dung khoá (R13).
		return r.panelFailed(ctx, res, "khoá sao lưu sai dạng ("+steps.BackupKeyFile+")", nil)
	}
	p, run, err := r.preparePanel(ctx, store, cfg)
	if err != nil {
		return r.panelFailed(ctx, res, failMsg(err), err)
	}
	rep, err := p.Execute(ctx, onEvent)
	if rep.Job.ID != "" {
		res.Reports = append(res.Reports, rep)
	}
	switch {
	case rep.Job.Status == engine.JobSucceeded && err == nil:
		res.Panel = run.bundle.Name
		return nil
	case rep.Job.Status == engine.JobSucceeded:
		// Gói đã lên kho, panel_last đã ghi; chỉ dọn gói cũ lỗi.
		res.Panel, res.PanelError = run.bundle.Name, failMsg(err)
		return nil
	case canceled(ctx, err) || canceled(ctx, run.h.err):
		return nil
	}
	cause := run.h.err
	if cause == nil {
		if cause = err; cause == nil {
			cause = errors.New(rep.Job.Error)
		}
	}
	return r.panelFailed(ctx, res, failMsg(cause), cause)
}

// panelFailed ghi lỗi gói panel và nhịp lùi riêng của gói; lỗi của đích thì
// thêm nhịp lùi của đích như việc 7.
func (r *Remote) panelFailed(ctx context.Context, res *SyncResult, msg string, cause error) error {
	res.PanelError = msg
	errs := []error{r.setSettings(ctx, SettingS3PanelError, msg), r.bump(ctx, SettingS3PanelFailures, SettingS3PanelRetryAt)}
	if cause != nil && s3.IsTargetError(cause) {
		errs = append(errs, r.targetFailed(ctx, res, cause))
	}
	return errors.Join(errs...)
}

// ================================================================ tải về

// PreparePull dựng "backup.pull" (mục 3.6, R16).
func (r *Remote) PreparePull(ctx context.Context, id string) (*Planned, error) {
	store, c, err := r.open(ctx)
	if err != nil {
		return nil, err
	}
	row, err := r.findBackup(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.Kind != BackupFull {
		return nil, refuse("backup_not_found", i18n.T("bản "+id+" không phải bản đầy đủ", "backup "+id+" is not a full backup"))
	}
	d, err := validate.ParseDomain(row.Domain)
	if err != nil {
		return nil, fmt.Errorf("dòng backup %s có domain hỏng: %w", id, err)
	}
	dir := site.BackupDir(site.LayoutOf(d))
	// Chặn dòng bị sửa tay: bước chạy bằng root tạo file ở đường dẫn này.
	if path.Clean(row.Path) != row.Path || path.Dir(row.Path) != dir {
		return nil, fmt.Errorf("dòng backup %s trỏ tới %q, không nằm ngay dưới %s; kiểm dòng này bằng tay", id, row.Path, dir)
	}
	if row.Remote != site.RemoteStored || row.RemoteTarget != c.ID {
		onDisk := false
		if row.Local {
			fi, err := r.Sys.Stat(row.Path)
			switch {
			case err == nil:
				onDisk = fi.Mode.IsRegular()
			case !errors.Is(err, fs.ErrNotExist):
				return nil, fmt.Errorf("kiểm %s: %w", row.Path, err)
			}
		}
		if onDisk {
			return nil, refuse(CodeBackupAlreadyLocal, i18n.T(
				"bản "+id+" của "+d.String()+" đã có trên máy, không cần tải về",
				"backup "+id+" of "+d.String()+" is already on this machine; nothing to download"))
		}
		return nil, refuse(CodeBackupNotOnMachine, i18n.T(
			"bản "+id+" của "+d.String()+" không có trên máy và không nằm ở đích đang đặt; đặt lại đích chứa nó (sudo tpan backup target set …) để tải về",
			"backup "+id+" of "+d.String()+" is not on this machine and not in the target set now; set the target that holds it (sudo tpan backup target set …) to download it"))
	}
	// R16: stored ở đích này thì luôn cho tải; S3Pull.Check so sha256 của file
	// đang có: đúng thì job không làm gì, hỏng thì xoá và tải lại.
	plan := engine.Plan{
		Kind:   "backup.pull",
		Target: d.String(),
		Title:  i18n.T("Tải bản sao lưu "+id+" của "+d.String()+" từ kho về máy", "Download backup "+id+" of "+d.String()+" from the store"),
		Steps: []engine.Step{&steps.S3Pull{Store: store, ID: id, Key: row.RemoteKey, Dest: row.Path, Dir: dir,
			Size: row.Size, SHA256: row.SHA256}},
	}
	p := newPlanned(heldLockSystem{r.Sys}, r.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		ok, err := r.Store.SetBackupLocal(context.WithoutCancel(ctx), id)
		if err != nil {
			return fmt.Errorf("ghi bản %s đã có trên máy: %w", id, err)
		}
		if !ok {
			return fmt.Errorf("dòng backup %s không còn trong bảng; file %s đã tải về nhưng không ai trỏ tới, xoá tay nếu không cần", id, row.Path)
		}
		return nil
	})
	p.owner = account.AdminName
	return p, nil
}

// Pull lấy khoá lượt đẩy TRƯỚC rồi mới đọc dòng và dựng Plan (R6: trong lúc
// chờ, dọn kho có thể đã xoá dòng), chạy, nhả khoá sau khi after đã chạy.
// Không giữ khoá job của máy (B10).
func (r *Remote) Pull(ctx context.Context, id string, onEvent func(engine.Event)) (engine.Report, error) {
	unlock, ok, err := r.locker().Acquire(ctx, r.lockWait())
	if err != nil {
		return engine.Report{}, fmt.Errorf("lấy khoá lượt đẩy: %w", err)
	}
	if !ok {
		return engine.Report{}, fmt.Errorf("một lượt đẩy lên kho đang chạy quá %s; thử lại sau", r.lockWait())
	}
	defer unlock()
	p, err := r.PreparePull(ctx, id)
	if err != nil {
		return engine.Report{}, err
	}
	return p.Execute(ctx, onEvent)
}

// ================================================================ liệt kê, health

// ListRemote liệt kê trên kho, ghép với bảng backups (mục 3.6).
func (r *Remote) ListRemote(ctx context.Context) (RemoteListing, error) {
	store, c, err := r.open(ctx)
	if err != nil {
		return RemoteListing{}, err
	}
	out := RemoteListing{Objects: []RemoteObject{}, Missing: []Backup{}}
	var rows []Backup
	if r.Store != nil {
		if rows, err = r.Store.Backups(ctx, ""); err != nil {
			return RemoteListing{}, fmt.Errorf("đọc bảng backups: %w", err)
		}
	}
	byKey := map[string]Backup{}
	for _, b := range rows {
		switch {
		case b.Remote != site.RemoteStored:
		case b.RemoteTarget == c.ID:
			byKey[b.RemoteKey] = b
		default:
			out.OtherTarget++
		}
	}
	seen := map[string]bool{}
	if err := store.List(ctx, "", func(o s3.Object) error {
		obj := RemoteObject{Key: o.Key, Size: o.Size, Modified: o.Modified}
		if b, ok := byKey[o.Key]; ok {
			obj.BackupID, obj.Domain = b.ID, b.Domain
		}
		seen[o.Key] = true
		out.Objects = append(out.Objects, obj)
		return nil
	}); err != nil {
		return RemoteListing{}, s3Refuse(err)
	}
	for _, b := range rows {
		if b.Remote == site.RemoteStored && b.RemoteTarget == c.ID && !seen[b.RemoteKey] {
			out.Missing = append(out.Missing, b)
		}
	}
	return out, nil
}

// State là đầu vào của tpan.s3:fresh (mục 3.13): chỉ đọc tpan.db, settings
// và file khoá, không gọi kho.
func (r *Remote) State(ctx context.Context) (checks.S3State, error) {
	c, ok, err := r.Config(ctx)
	if err != nil {
		return checks.S3State{}, err
	}
	if !ok {
		return checks.S3State{}, errNotConfigured()
	}
	if r.Store == nil || r.Sites == nil {
		return checks.S3State{}, errors.New("tpan.s3:fresh: chưa có DB của tpan")
	}
	st, err := r.readState(ctx)
	if err != nil {
		return checks.S3State{}, err
	}
	out := checks.S3State{TargetError: st.targetError, PanelError: st.panelError, HasKey: r.backupKeyState() == keyOK}
	if st.panelLast != nil {
		out.PanelLast = st.panelLast.At
	}
	at, err := r.getSetting(ctx, SettingBackupAt)
	if err != nil {
		return checks.S3State{}, err
	}
	out.Schedule = at != ""
	rows, err := r.Store.Backups(ctx, "")
	if err != nil {
		return checks.S3State{}, fmt.Errorf("đọc bảng backups: %w", err)
	}
	for _, b := range rows { // mới nhất trước
		if b.Remote != site.RemoteFailed {
			continue
		}
		out.Failed++
		if out.LastError == "" {
			out.LastError = b.RemoteError
		}
		if len(out.FailedDomains) < 5 && !slices.Contains(out.FailedDomains, b.Domain) {
			out.FailedDomains = append(out.FailedDomains, b.Domain)
		}
	}
	sites, err := r.Sites.Sites(ctx)
	if err != nil {
		return checks.S3State{}, fmt.Errorf("đọc danh sách site: %w", err)
	}
	now := r.now()
	for _, s := range sites {
		if s.Status != site.Active || now.Sub(s.Created) < 24*time.Hour {
			continue
		}
		dn := s.Domain.String()
		fresh := slices.ContainsFunc(rows, func(b Backup) bool {
			return b.Domain == dn && b.Kind == BackupFull && (b.Reason == ReasonManual || b.Reason == ReasonSchedule) &&
				now.Sub(b.Created) <= 48*time.Hour && b.Remote == site.RemoteStored && b.RemoteTarget == c.ID
		})
		if !fresh {
			out.Stale = append(out.Stale, dn)
		}
	}
	return out, nil
}

// problemOf là mã s3_* và câu đã làm sạch của một lỗi khi nói chuyện với kho.
func problemOf(err error) (string, string) {
	var re *RefusedError
	if errors.As(err, &re) {
		return re.Code, s3.CleanMessage(re.Message.Vi)
	}
	return s3.Classify(err), failMsg(err)
}

// Probe là đầu vào của tpan.s3:target (mục 3.13): Check, rồi Head bản stored
// mới nhất ở đích này của từng site active. Evidence không chứa khoá.
func (r *Remote) Probe(ctx context.Context) (checks.S3Probe, error) {
	var out checks.S3Probe
	store, c, err := r.open(ctx)
	if err != nil {
		var re *RefusedError
		if errors.As(err, &re) && re.Code == s3.CodeNotConfigured {
			return out, err
		}
		out.Problem, out.Detail = problemOf(err)
		out.Evidence = []checks.Evidence{{Command: "tpan: đọc khoá và dựng client S3", ExitCode: 1, Output: out.Detail}}
		return out, nil
	}
	label := targetLabel(c)
	if err := store.Check(ctx); err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		out.Problem, out.Detail = problemOf(err)
		out.Evidence = []checks.Evidence{{Command: "s3 check " + label, ExitCode: 1, Output: out.Detail}}
		return out, nil
	}
	out.Evidence = []checks.Evidence{{Command: "s3 check " + label, Output: "ok"}}
	if r.Store == nil || r.Sites == nil {
		return out, nil
	}
	rows, err := r.Store.Backups(ctx, "")
	if err != nil {
		return out, fmt.Errorf("đọc bảng backups: %w", err)
	}
	sites, err := r.Sites.Sites(ctx)
	if err != nil {
		return out, fmt.Errorf("đọc danh sách site: %w", err)
	}
	for _, s := range sites {
		if s.Status != site.Active {
			continue
		}
		dn := s.Domain.String()
		i := slices.IndexFunc(rows, func(b Backup) bool {
			return b.Domain == dn && b.Kind == BackupFull && b.Remote == site.RemoteStored && b.RemoteTarget == c.ID
		})
		if i < 0 {
			continue
		}
		b := rows[i]
		obj, err := store.Head(ctx, b.RemoteKey)
		ev := checks.Evidence{Command: "s3 head " + b.RemoteKey}
		switch {
		case err == nil && obj.Size == b.Size:
			ev.Output = fmt.Sprintf("size %d", obj.Size)
		case err == nil:
			ev.ExitCode, ev.Output = 1, fmt.Sprintf("size %d, bản có %d byte", obj.Size, b.Size)
			out.Missing = append(out.Missing, dn+" ("+b.ID+")")
		case errors.Is(err, s3.ErrNotFound):
			ev.ExitCode, ev.Output = 1, failMsg(err)
			out.Missing = append(out.Missing, dn+" ("+b.ID+")")
		default:
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			ev.ExitCode, ev.Output = 1, failMsg(err)
			out.Evidence = append(out.Evidence, ev)
			out.Problem, out.Detail = problemOf(err)
			return out, nil
		}
		out.Evidence = append(out.Evidence, ev)
	}
	return out, nil
}
