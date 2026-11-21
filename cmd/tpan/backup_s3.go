package main

// Lệnh con của M18 dưới `tpan backup` (mục 3.12 và 9.3 của
// docs/thay-doi/M18-thiet-ke.md):
//
//	backup target set|show|test|rm   đích S3 (kho nằm ngoài máy)
//	backup key new|show              khoá sao lưu mã hoá gói panel
//	backup push [<id>]               một lượt đẩy gõ tay
//	backup pull <id>                 tải một bản từ kho về máy
//
// Mọi quyết định nằm ở core.Remote; file này chỉ đọc cờ, đọc khoá từ stdin
// hay terminal, gọi core rồi in. Luật bí mật (B3): không cờ nào nhận khoá trên
// dòng lệnh (dòng lệnh lọt vào `ps` và lịch sử shell); hai khoá truy cập không
// bao giờ được in; khoá sao lưu chỉ in ở ba chỗ (key new, key show --reveal,
// target set khi vừa sinh), vì người quản trị phải chép nó ra ngoài máy.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/doxuta/tpan/internal/agebox"
	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// backupS3Cmds trả các lệnh con M18 của `tpan backup` (backup.go gắn chúng vào).
func backupS3Cmds(e *env) []*cobra.Command {
	target := &cobra.Command{Use: "target", Short: "Đích sao lưu S3: đặt, xem, thử, gỡ"}
	target.AddCommand(backupTargetSetCmd(e), backupTargetShowCmd(e), backupTargetTestCmd(e), backupTargetRmCmd(e))
	key := &cobra.Command{Use: "key", Short: "Khoá sao lưu (mã hoá gói panel trước khi lên kho): sinh mới, xem"}
	key.AddCommand(backupKeyNewCmd(e), backupKeyShowCmd(e))
	return []*cobra.Command{target, key, backupPushCmd(e), backupPullCmd(e)}
}

// ================================================================ dựng Remote

// remoteHook chỉ dành cho test: sửa Remote vừa dựng (kho giả s3.Mem, khoá
// lượt đẩy trong bộ nhớ, Snapshot vào máy giả). Máy thật để nil.
var remoteHook func(*core.Remote)

// remoteOf dựng core.Remote trên store đang mở. st nil: máy chưa có tpan.db
// mà chỉ xem (bảng settings và backups coi như rỗng, không ghi được gì).
func remoteOf(e *env, st *store.Store) (*core.Remote, error) {
	host, err := setupHostname()
	if err != nil {
		return nil, fmt.Errorf("đọc hostname: %w", err)
	}
	r := &core.Remote{Sys: e.sys, Settings: core.EmptySettings{}, Sites: core.EmptySiteStore{},
		Hostname: host, Version: buildinfo.Version,
		Lock: system.FileLock{Path: system.DefaultRemoteLockPath}}
	if st != nil {
		r.Settings, r.Store, r.Sites, r.Journal, r.Snapshot = st, st, st, st, st.SnapshotDB
	}
	if remoteHook != nil {
		remoteHook(r)
	}
	// Bọc sau hook: test thay khoá thì vẫn có câu báo chờ.
	r.Lock = waitNotice{Locker: r.Lock, e: e}
	return r, nil
}

// openRemote mở tpan.db rồi dựng Remote. readOnly (xem, --dry-run) mà chưa có
// file DB thì không tạo DB; readOnly thì cũng không có Journal (không chạy job).
func openRemote(ctx context.Context, e *env, readOnly bool) (*core.Remote, func(), error) {
	if readOnly && !e.fileExists(e.dbPath) {
		r, err := remoteOf(e, nil)
		return r, func() {}, err
	}
	st, err := openStore(ctx, e)
	if err != nil {
		return nil, nil, err
	}
	r, err := remoteOf(e, st)
	if err != nil {
		closeStore(st)
		return nil, nil, err
	}
	if readOnly {
		r.Journal = nil
	}
	return r, func() { closeStore(st) }, nil
}

// waitNotice in một dòng ra stderr khi phải chờ khoá lượt đẩy (mục 3.12):
// lệnh đứng im tới 30 phút mà không nói gì thì người gõ tưởng nó treo.
type waitNotice struct {
	system.Locker
	e *env
}

func (l waitNotice) Acquire(ctx context.Context, wait time.Duration) (func(), bool, error) {
	unlock, ok, err := l.Locker.Acquire(ctx, 0)
	if err != nil || ok || wait <= 0 {
		return unlock, ok, err
	}
	printf(l.e.stderr, "%s\n", l.e.tr("Đang chờ lượt đẩy lên kho đang chạy xong (tối đa "+wait.String()+")...",
		"Waiting for the running storage upload to finish (up to "+wait.String()+")..."))
	return l.Locker.Acquire(ctx, wait)
}

// requireTarget từ chối s3_not_configured khi chưa đặt đích (câu như của core).
func requireTarget(ctx context.Context, r *core.Remote) error {
	_, ok, err := r.Config(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return &core.RefusedError{Code: s3.CodeNotConfigured, Message: i18n.T(
			"chưa đặt đích S3; đặt bằng: sudo tpan backup target set --endpoint https://… --bucket …",
			"no S3 target is set; set one with: sudo tpan backup target set --endpoint https://… --bucket …")}
	}
	return nil
}

func argsInvalid(vi, en string) error {
	return &core.RefusedError{Code: "args_invalid", Message: i18n.T(vi, en)}
}

// ================================================================ đọc khoá

// maxKeysInput: hai dòng khoá dài nhất (128 + 256 ký tự) cộng dư. Đọc có trần
// để một stdin vô tận (`yes | tpan …`) không ăn hết RAM.
const maxKeysInput = 1024

func keysInvalid(vi, en string) error {
	return &core.RefusedError{Code: s3.CodeKeysInvalid, Message: i18n.T(vi, en)}
}

// readKeysStdin đọc --keys-stdin: dòng 1 access key, dòng 2 secret key. Lời
// từ chối chỉ nêu số dòng, số byte, không bao giờ chép nội dung đã đọc.
func readKeysStdin(r io.Reader) (string, system.Secret, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxKeysInput+1))
	if err != nil {
		return "", system.Secret{}, fmt.Errorf("đọc khoá từ stdin: %w", err)
	}
	if len(b) > maxKeysInput {
		return "", system.Secret{}, keysInvalid("stdin dài quá "+strconv.Itoa(maxKeysInput)+" byte; --keys-stdin chỉ nhận hai dòng: access key, rồi secret key",
			"stdin is longer than "+strconv.Itoa(maxKeysInput)+" bytes; --keys-stdin takes just two lines: the access key, then the secret key")
	}
	lines := strings.Split(strings.TrimRight(string(b), "\r\n"), "\n")
	if len(lines) != 2 {
		n := strconv.Itoa(len(lines))
		return "", system.Secret{}, keysInvalid("--keys-stdin cần đúng hai dòng: dòng 1 access key, dòng 2 secret key (nhận "+n+" dòng)",
			"--keys-stdin needs exactly two lines: line 1 the access key, line 2 the secret key (got "+n+" lines)")
	}
	return strings.TrimSpace(lines[0]), system.NewSecret(strings.TrimSpace(lines[1])), nil
}

// readLine đọc một dòng, từng byte một: không đệm trước phần sau dòng, để
// câu hỏi "Chạy? [y/N]" (đọc bằng bufio riêng) vẫn thấy câu trả lời của nó.
func readLine(r io.Reader, limit int) (string, error) {
	var b []byte
	one := make([]byte, 1)
	for len(b) <= limit {
		n, err := r.Read(one)
		if n == 1 {
			if one[0] == '\n' {
				return strings.TrimSpace(string(b)), nil
			}
			b = append(b, one[0])
		}
		if errors.Is(err, io.EOF) {
			return strings.TrimSpace(string(b)), nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", keysInvalid("dòng nhập dài quá "+strconv.Itoa(limit)+" ký tự", "the line is longer than "+strconv.Itoa(limit)+" characters")
}

// promptKeys hỏi hai khoá trên terminal; secret key không hiện chữ (không
// nằm lại trên màn hình hay trong bản ghi phiên terminal). Câu hỏi ra stderr
// để stdout chỉ có kết quả.
func promptKeys(e *env) (string, system.Secret, error) {
	printf(e.stderr, "%s", e.tr("Access key: ", "Access key: "))
	access, err := readLine(e.stdin, maxKeysInput)
	if err != nil {
		return "", system.Secret{}, err
	}
	printf(e.stderr, "%s", e.tr("Secret key (gõ không hiện chữ): ", "Secret key (typing is hidden): "))
	var secret string
	if f, ok := e.stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) { //nolint:gosec // fd của stdin luôn nhỏ, không tràn int
		b, rerr := term.ReadPassword(int(f.Fd())) //nolint:gosec // như trên
		secret, err = strings.TrimSpace(string(b)), rerr
		printf(e.stderr, "\n")
	} else {
		secret, err = readLine(e.stdin, maxKeysInput)
	}
	if err != nil {
		return "", system.Secret{}, fmt.Errorf("đọc secret key: %w", err)
	}
	return access, system.NewSecret(secret), nil
}

// maxBackupKeyFile: khoá sao lưu là một dòng 63 ký tự; file lớn hơn thế này
// chắc chắn không phải khoá.
const maxBackupKeyFile = 4 << 10

// readBackupKeyFile đọc khoá sao lưu cho `key show` và dấu vân tay sau `target
// set`. ok=false: chưa có file. Lỗi chỉ nêu đường dẫn và độ dài, không bao giờ
// nội dung (R13). ponytail: core có readBackupKey nhưng không xuất ra ngoài
// gói; core xuất hàm đó thì bỏ hàm này.
func readBackupKeyFile(sys system.System) (system.Secret, bool, error) {
	p := steps.BackupKeyFile
	fi, err := sys.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return system.Secret{}, false, nil
	}
	if err != nil {
		return system.Secret{}, false, fmt.Errorf("kiểm %s: %w", p, err)
	}
	if !fi.Mode.IsRegular() || fi.Size > maxBackupKeyFile {
		return system.Secret{}, false, fmt.Errorf("%s không phải file khoá (cần file thường, tối đa %d byte)", p, maxBackupKeyFile)
	}
	b, err := sys.ReadFile(p)
	if err != nil {
		return system.Secret{}, false, fmt.Errorf("đọc %s: %w", p, err)
	}
	k := system.NewSecret(strings.TrimSpace(string(b)))
	if agebox.CheckKey(k) != nil {
		return system.Secret{}, false, fmt.Errorf("%s sai dạng (dài %d ký tự, cần tpan-key-1- và 52 ký tự base32); sinh khoá mới bằng: sudo tpan backup key new",
			p, len(k.Reveal()))
	}
	return k, true, nil
}

// printNewKey in khoá sao lưu vừa sinh, ĐÚNG MỘT LẦN, sau khi job đã ghi nó.
func printNewKey(e *env, k system.Secret) {
	w := e.stdout
	printf(w, "\n%s\n", e.tr("Khoá sao lưu mới (chỉ in lần này):", "New backup key (shown only this once):"))
	printf(w, "  %s\n", k.Reveal())
	printf(w, "  %s %s\n", e.tr("Dấu vân tay:", "Fingerprint:"), agebox.Fingerprint(k))
	printf(w, "%s\n", e.tr(
		"Chép khoá này ra NGOÀI máy ngay (trình quản lý mật khẩu, hoặc giấy cất kỹ). Gói panel trên kho (danh sách user, gói, cài đặt) được mã hoá bằng nó: mất máy mà mất cả khoá thì không giải mã được gói panel. File .tar.gz của site trên kho không mã hoá, vẫn dùng được.",
		"Copy this key OFF the machine now (a password manager, or paper kept safe). The panel bundle on the store (users, plans, settings) is encrypted with it: lose the machine and the key and the bundle cannot be decrypted. The sites' .tar.gz files on the store are not encrypted and stay usable."))
	printKeyHint(e)
}

func printKeyHint(e *env) {
	printf(e.stdout, "%s\n", e.tr("Xem lại khoá bằng: sudo tpan backup key show --reveal",
		"Show the key again with: sudo tpan backup key show --reveal"))
}

// ================================================================ target

// targetSetOut là JSON của `target set`. BackupKey, KeyFingerprint chỉ có khi
// lần chạy này vừa sinh và ghi khoá sao lưu.
type targetSetOut struct {
	Report         engine.Report         `json:"report"`
	Result         *core.TargetSetResult `json:"result"`
	BackupKey      string                `json:"backup_key,omitempty"`
	KeyFingerprint string                `json:"key_fingerprint,omitempty"`
}

func backupTargetSetCmd(e *env) *cobra.Command {
	var (
		f                     planFlags
		in                    core.TargetInput
		vhost, noCA, keysPipe bool
		caFile                string
	)
	c := &cobra.Command{
		Use:   "set --endpoint URL --bucket B [--region R] [--prefix P] [--vhost] [--ca-file F | --no-ca] [--keep N] [--keys-stdin]",
		Short: "Đặt đích sao lưu S3: thử ghi, đọc, xoá thật rồi mới lưu; hai khoá đọc từ stdin hoặc hỏi trên terminal",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if caFile != "" && noCA {
				return fail(e, f.asJSON, argsInvalid("--ca-file và --no-ca không đi cùng nhau", "--ca-file and --no-ca do not go together"))
			}
			// Cả lúc xem trước cũng cần root: core đọc khoá đang lưu và thử nối kho.
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, f.asJSON, err)
			}
			in.PathStyle, in.GenerateKey = !vhost, true
			switch {
			case caFile != "":
				// File của người dùng, không do tpan quản: đọc thẳng (như
				// cloud-init render), có trần; quá MaxCABytes thì core từ chối.
				pem, err := ciReadFileMax(caFile, s3.MaxCABytes+1)
				if err != nil {
					return fail(e, f.asJSON, &core.RefusedError{Code: s3.CodeCAInvalid, Message: i18n.T(
						"không đọc được --ca-file: "+err.Error(), "cannot read --ca-file: "+err.Error())})
				}
				in.CA, in.CAPEM = core.CASet, pem
			case noCA:
				in.CA = core.CANone
			}
			var err error
			switch {
			case keysPipe:
				in.AccessKey, in.SecretKey, err = readKeysStdin(e.stdin)
			case e.isTerminal():
				in.AccessKey, in.SecretKey, err = promptKeys(e)
			default:
				// Không ai trả lời được câu hỏi: giữ hai khoá đang lưu. Core chỉ
				// cho khi endpoint, vùng, bucket, CA không đổi (R1).
				in.KeepKeys = true
			}
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			r, done, err := openRemote(ctx, e, f.dryRun)
			if err != nil {
				return err
			}
			defer done()
			ts, err := r.PrepareSetTarget(ctx, in)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			newKey := ts.NewKey.Reveal() != ""
			rep, err := runPlannedOut(ctx, e, f, r.Journal, "backup.target.set", ts.Plan.Target, ts.Planned, func(rep engine.Report) any {
				out := targetSetOut{Report: rep, Result: ts.Result}
				if newKey && rep.Job.Status == engine.JobSucceeded {
					out.BackupKey, out.KeyFingerprint = ts.NewKey.Reveal(), agebox.Fingerprint(ts.NewKey)
				}
				return out
			})
			// Job đạt thì khoá đã nằm trên đĩa: in nó kể cả khi after lỗi, không
			// thì người dùng không bao giờ thấy khoá lần nào.
			if rep.Job.Status == engine.JobSucceeded && !f.asJSON {
				printTargetSetTail(e, ts)
			}
			return err
		},
	}
	fl := c.Flags()
	fl.StringVar(&in.Endpoint, "endpoint", "", "địa chỉ kho, ví dụ https://s3.example.com (http chỉ cho máy này: 127.0.0.1, localhost)")
	fl.StringVar(&in.Bucket, "bucket", "", "tên bucket")
	fl.StringVar(&in.Region, "region", "", "vùng của kho (mặc định "+s3.DefaultRegion+")")
	fl.StringVar(&in.Prefix, "prefix", "", "thư mục trong bucket (mặc định tpan/<tên máy>)")
	fl.BoolVar(&vhost, "vhost", false, "gọi kho theo kiểu <bucket>.<host> thay cho <host>/<bucket>")
	fl.StringVar(&caFile, "ca-file", "", "file PEM chứng chỉ gốc riêng của kho (kho nội bộ tự cấp chứng chỉ)")
	fl.BoolVar(&noCA, "no-ca", false, "bỏ chứng chỉ gốc riêng đang lưu")
	fl.IntVar(&in.Keep, "keep", 0, "số bản giữ trên kho mỗi site, 1 tới "+strconv.Itoa(s3.MaxKeep)+" (mặc định "+strconv.Itoa(s3.DefaultKeep)+")")
	fl.BoolVar(&keysPipe, "keys-stdin", false, "đọc hai khoá từ stdin: dòng 1 access key, dòng 2 secret key")
	bindChangeFlags(c, &f)
	_ = c.MarkFlagRequired("endpoint")
	_ = c.MarkFlagRequired("bucket")
	return c
}

// printTargetSetTail: khoá sao lưu vừa sinh (một lần), hoặc dấu vân tay của
// khoá đang có (S33); rồi kết quả nối lại các bản ở đích cũ.
func printTargetSetTail(e *env, ts *core.TargetSet) {
	if ts.NewKey.Reveal() != "" {
		printNewKey(e, ts.NewKey)
	} else {
		printKeyFingerprint(e)
	}
	if res := ts.Result; res != nil && (res.Relinked > 0 || res.OtherTarget > 0) {
		printf(e.stdout, e.tr("Nối lại %d bản sang đích này; %d bản còn ở đích khác.\n", "Relinked %d copies to this target; %d copies remain in another target.\n"),
			res.Relinked, res.OtherTarget)
	}
}

// printKeyFingerprint in dấu vân tay khoá đang có, hoặc cách sinh khi chưa có.
func printKeyFingerprint(e *env) {
	k, ok, err := readBackupKeyFile(e.sys)
	switch {
	case err != nil:
		printf(e.stdout, "%s %v\n", e.tr("Khoá sao lưu:", "Backup key:"), err)
	case !ok:
		printf(e.stdout, "%s\n", e.tr("Chưa có khoá sao lưu, nên chưa có gói panel; sinh bằng: sudo tpan backup key new",
			"There is no backup key, so no panel bundle yet; generate one with: sudo tpan backup key new"))
	default:
		printf(e.stdout, "%s %s\n", e.tr("Dấu vân tay khoá sao lưu:", "Backup key fingerprint:"), agebox.Fingerprint(k))
		printKeyHint(e)
	}
}

func backupTargetShowCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show",
		Short: "Xem đích sao lưu S3 đang đặt (không bao giờ in khoá)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			r, done, err := openRemote(ctx, e, true)
			if err != nil {
				return err
			}
			defer done()
			v, err := r.Target(ctx)
			if err != nil {
				return fail(e, asJSON, err)
			}
			if asJSON {
				return writeJSON(e.stdout, v)
			}
			printTargetView(e, v)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func printTargetView(e *env, v core.TargetView) {
	w := e.stdout
	row := func(k, val string) { printf(w, "%-26s %s\n", k, val) }
	yesNo := func(b bool) string {
		if b {
			return e.tr("có", "yes")
		}
		return e.tr("không", "no")
	}
	if !v.Configured {
		printf(w, "%s\n", e.tr("Chưa đặt đích S3. Đặt bằng: sudo tpan backup target set --endpoint https://… --bucket …",
			"No S3 target is set. Set one with: sudo tpan backup target set --endpoint https://… --bucket …"))
	} else {
		row("Endpoint:", v.Endpoint)
		row(e.tr("Vùng:", "Region:"), v.Region)
		row("Bucket:", v.Bucket)
		row(e.tr("Tiền tố:", "Prefix:"), v.Prefix)
		style := "path-style (<host>/<bucket>)"
		if !v.PathStyle {
			style = "virtual-host (<bucket>.<host>)"
		}
		row(e.tr("Kiểu địa chỉ:", "Addressing:"), style)
		row(e.tr("Giữ trên kho:", "Kept on the store:"), e.tr(strconv.Itoa(v.Keep)+" bản mỗi site", strconv.Itoa(v.Keep)+" copies per site"))
		row(e.tr("Mã đích:", "Target id:"), v.TargetID)
		row(e.tr("Chứng chỉ gốc riêng:", "Private CA:"), yesNo(v.HasCA))
		access := "…" + v.AccessKeyTail
		if v.AccessKeyTail == "" {
			access = e.tr("("+strconv.Itoa(v.AccessKeyLen)+" ký tự)", "("+strconv.Itoa(v.AccessKeyLen)+" characters)")
		}
		row("Access key:", access)
	}
	if v.HasBackupKey {
		row(e.tr("Khoá sao lưu:", "Backup key:"), e.tr("có, dấu vân tay ", "yes, fingerprint ")+v.KeyFingerprint)
	} else {
		row(e.tr("Khoá sao lưu:", "Backup key:"), e.tr("chưa có (sinh bằng: sudo tpan backup key new)", "none (generate with: sudo tpan backup key new)"))
	}
	if !v.Configured {
		if v.HasBackupKey {
			printKeyHint(e)
		}
		return
	}
	if v.PanelLast != nil {
		row(e.tr("Gói panel gần nhất:", "Latest panel bundle:"), v.PanelLast.Name+" ("+v.PanelLast.At.Local().Format("2006-01-02 15:04")+")")
	} else {
		row(e.tr("Gói panel gần nhất:", "Latest panel bundle:"), e.tr("chưa có", "none yet"))
	}
	if v.PanelError != "" {
		row(e.tr("Lỗi gói panel:", "Panel bundle error:"), v.PanelError)
	}
	if v.TargetError != "" {
		row(e.tr("Lỗi của kho:", "Store error:"), v.TargetError)
	}
	if !v.RetryAt.IsZero() {
		row(e.tr("Đẩy lại sau:", "Next try after:"), v.RetryAt.Local().Format("2006-01-02 15:04"))
	}
	row(e.tr("Bản chỉ còn trên kho:", "Copies only on the store:"), strconv.Itoa(v.RemoteOnly))
	row(e.tr("Bản ở đích khác:", "Copies in another target:"), strconv.Itoa(v.OtherTarget))
	row(e.tr("Bản đẩy lỗi:", "Copies that failed to upload:"), strconv.Itoa(v.Failed))
	if v.HasBackupKey {
		printKeyHint(e)
	}
}

func backupTargetTestCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "test",
		Short: "Thử đích đang đặt: ghi, đọc lại, xoá một object nhỏ (là một job, có lịch sử)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRemotePlan(cmd, e, f, "backup.target.test", func(ctx context.Context, r *core.Remote) (*core.Planned, error) {
				return r.PrepareTestTarget(ctx)
			})
		},
	}
	bindChangeFlags(c, &f)
	return c
}

func backupTargetRmCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "rm",
		Short: "Gỡ đích S3 khỏi máy (không xoá gì trên kho, không xoá khoá sao lưu)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRemotePlan(cmd, e, f, "backup.target.rm", func(ctx context.Context, r *core.Remote) (*core.Planned, error) {
				return r.PrepareRemoveTarget(ctx)
			})
		},
	}
	bindChangeFlags(c, &f)
	return c
}

// runRemotePlan là nhịp chung của target test, target rm: cần root, dựng Plan
// rồi đi runPlanned (xem trước, hỏi lại trừ --yes, chạy, in Report).
func runRemotePlan(cmd *cobra.Command, e *env, f planFlags, action string, prepare func(context.Context, *core.Remote) (*core.Planned, error)) error {
	ctx := cmd.Context()
	if err := requireRoot(e, cmd); err != nil {
		return fail(e, f.asJSON, err)
	}
	r, done, err := openRemote(ctx, e, f.dryRun)
	if err != nil {
		return err
	}
	defer done()
	p, err := prepare(ctx, r)
	if err != nil {
		return fail(e, f.asJSON, err)
	}
	_, err = runPlanned(ctx, e, f, r.Journal, action, p.Plan.Target, p)
	return err
}

// ================================================================ key

// keyNewOut là JSON của `key new`; Key, KeyFingerprint chỉ có khi job đạt.
type keyNewOut struct {
	Report         engine.Report `json:"report"`
	Key            string        `json:"key,omitempty"`
	KeyFingerprint string        `json:"key_fingerprint,omitempty"`
}

func backupKeyNewCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "new",
		Short: "Sinh khoá sao lưu mới (gói panel cũ chỉ mở được bằng khoá cũ)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, f.asJSON, err)
			}
			r, done, err := openRemote(ctx, e, f.dryRun)
			if err != nil {
				return err
			}
			defer done()
			kn, err := r.PrepareNewKey(ctx)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			rep, err := runPlannedOut(ctx, e, f, r.Journal, "backup.key.new", kn.Plan.Target, kn.Planned, func(rep engine.Report) any {
				out := keyNewOut{Report: rep}
				if rep.Job.Status == engine.JobSucceeded {
					out.Key, out.KeyFingerprint = kn.Key.Reveal(), agebox.Fingerprint(kn.Key)
				}
				return out
			})
			if rep.Job.Status == engine.JobSucceeded && !f.asJSON {
				printNewKey(e, kn.Key)
				printf(e.stdout, "%s\n", e.tr(
					"Gói panel cũ trên kho chỉ mở được bằng khoá CŨ, tpan không giữ khoá cũ: còn cần các gói đó thì cất cả khoá cũ. Gói mang khoá mới lên kho ở lượt đẩy kế tiếp (hoặc: sudo tpan backup push).",
					"Older panel bundles on the store open only with the OLD key, and tpan does not keep it: if you still need them, keep the old key too. A bundle with the new key goes up on the next push run (or: sudo tpan backup push)."))
			}
			return err
		},
	}
	bindChangeFlags(c, &f)
	return c
}

// keyShowOut là JSON của `key show`; Key chỉ có với --reveal.
type keyShowOut struct {
	HasKey         bool   `json:"has_key"`
	KeyFingerprint string `json:"key_fingerprint,omitempty"`
	Key            string `json:"key,omitempty"`
}

func backupKeyShowCmd(e *env) *cobra.Command {
	var asJSON, reveal bool
	c := &cobra.Command{
		Use:   "show [--reveal]",
		Short: "Xem dấu vân tay khoá sao lưu; --reveal in cả khoá",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			k, ok, err := readBackupKeyFile(e.sys)
			if err != nil {
				return err
			}
			out := keyShowOut{HasKey: ok}
			if ok {
				out.KeyFingerprint = agebox.Fingerprint(k)
				if reveal {
					out.Key = k.Reveal()
				}
			}
			if asJSON {
				return writeJSON(e.stdout, out)
			}
			w := e.stdout
			if !ok {
				printf(w, "%s\n", e.tr("Chưa có khoá sao lưu. Sinh bằng: sudo tpan backup key new",
					"There is no backup key. Generate one with: sudo tpan backup key new"))
				return nil
			}
			printf(w, "%-14s %s\n", e.tr("File:", "File:"), steps.BackupKeyFile)
			printf(w, "%-14s %s\n", e.tr("Dấu vân tay:", "Fingerprint:"), out.KeyFingerprint)
			if !reveal {
				printKeyHint(e)
				return nil
			}
			printf(w, "%-14s %s\n", e.tr("Khoá:", "Key:"), out.Key)
			printf(w, "%s\n", e.tr(
				"Cảnh báo: ai có khoá này mở được mọi gói panel trên kho (danh sách user, mật khẩu băm, cài đặt). Cất ngoài máy, đừng dán vào chat hay ticket.",
				"Warning: anyone with this key can open every panel bundle on the store (users, password hashes, settings). Keep it off the machine; do not paste it into chat or tickets."))
			return nil
		},
	}
	c.Flags().BoolVar(&reveal, "reveal", false, "in cả khoá (để chép ra ngoài máy)")
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

// ================================================================ push, pull

// backupIDArg: id bản sao lưu là chữ số hex (như core); kiểm trước khi đi
// tiếp để chuỗi lạ không đi vào output hay câu lỗi.
var backupIDArg = regexp.MustCompile(`^[0-9a-f]{1,64}$`)

func badBackupID() error {
	return &core.RefusedError{Code: "backup_not_found", Message: i18n.T(
		"id backup không đúng dạng (chữ số hex); xem các bản bằng: sudo tpan backup list",
		"the backup id is not in the right form (hex digits); list the copies with: sudo tpan backup list")}
}

func backupPushCmd(e *env) *cobra.Command {
	var asJSON, verbose bool
	c := &cobra.Command{
		Use:   "push [<id>]",
		Short: "Đẩy lên kho ngay các bản đang chờ (hoặc một bản), bỏ qua nhịp lùi sau lỗi",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			opt := core.SyncOptions{Wait: true, Retry: true}
			if len(args) == 1 {
				if !backupIDArg.MatchString(args[0]) {
					return fail(e, asJSON, badBackupID())
				}
				opt.Only = []string{args[0]}
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			r, done, err := openRemote(ctx, e, false)
			if err != nil {
				return err
			}
			defer done()
			if err := requireTarget(ctx, r); err != nil {
				return fail(e, asJSON, err)
			}
			res, err := r.Sync(ctx, opt, planProgress(e, planFlags{asJSON: asJSON, verbose: verbose}, engine.Plan{}).onEvent)
			auditSync(ctx, e, r.Journal, res)
			if asJSON {
				if werr := writeJSON(e.stdout, res); werr != nil {
					return werr
				}
			} else {
				printSync(e, res)
			}
			if err != nil {
				return err
			}
			if !res.Ran || len(res.Failed) > 0 {
				return &exitCode{1}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	c.Flags().BoolVar(&verbose, "verbose", false, "in lệnh và output của mọi bước")
	return c
}

// auditSync ghi nhật ký cho từng job đẩy, gói panel của một lượt chạy từ CLI.
func auditSync(ctx context.Context, e *env, journal any, res core.SyncResult) {
	for _, rep := range res.Reports {
		cliAudit(ctx, e, journal, rep.Job.Kind, rep.Job.Target, rep.Job)
	}
}

func printSync(e *env, res core.SyncResult) {
	w := e.stdout
	if !res.Ran {
		printf(w, "%s %s\n", e.tr("Bỏ qua lượt đẩy:", "Upload run skipped:"), res.Skipped)
	}
	for _, id := range res.Pushed {
		printf(w, "%s %s\n", e.tr("Đã lên kho:", "Uploaded:"), id)
	}
	for _, f := range res.Failed {
		dom := ""
		if f.Domain != "" {
			dom = " (" + f.Domain + ")"
		}
		printf(w, "%s %s%s: %s\n", e.tr("LỖI", "FAILED"), f.ID, dom, f.Error)
	}
	if res.Panel != "" {
		printf(w, "%s %s\n", e.tr("Gói panel đã lên kho:", "Panel bundle uploaded:"), res.Panel)
	}
	if res.PanelError != "" {
		printf(w, "%s %s\n", e.tr("Gói panel lỗi:", "Panel bundle failed:"), res.PanelError)
	}
	if res.TargetError != "" {
		printf(w, "%s %s\n", e.tr("Kho lỗi:", "Store error:"), res.TargetError)
	}
	if res.Ran && len(res.Pushed) == 0 && len(res.Failed) == 0 && res.Panel == "" && res.PanelError == "" && res.TargetError == "" {
		printf(w, "%s\n", e.tr("Không có bản nào cần đẩy.", "Nothing to upload."))
	}
}

func backupPullCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "pull <id>",
		Short: "Tải một bản sao lưu từ kho về máy (kiểm sha256 trước khi giữ)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id := args[0]
			if !backupIDArg.MatchString(id) {
				return fail(e, f.asJSON, badBackupID())
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, f.asJSON, err)
			}
			r, done, err := openRemote(ctx, e, f.dryRun)
			if err != nil {
				return err
			}
			defer done()
			// Dựng Plan một lần cho bản xem trước, câu hỏi lại và tên bước
			// trong tiến độ; Pull tự dựng lại sau khi đã giữ khoá lượt đẩy (R6).
			p, err := r.PreparePull(ctx, id)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if ok, err := confirmPlanned(ctx, e, f, p); !ok {
				return err
			}
			rep, err := r.Pull(ctx, id, planProgress(e, f, p.Plan).onEvent)
			if err != nil && rep.Job.ID == "" {
				return fail(e, f.asJSON, err)
			}
			_, err = finishPlanned(ctx, e, f, r.Journal, "backup.pull", rep.Job.Target, rep, err, nil)
			return err
		},
	}
	bindChangeFlags(c, &f)
	return c
}

// ================================================================ móc vào create, list, restore (backup.go)

// backupSyncTail là phần cuối của `backup create` (mục 3.5): chỉ khi đã đặt
// đích, một lượt đẩy có chờ. Kho lỗi không đổi mã thoát (B1: bản trên máy đã
// xong), chỉ in ra stderr để stdout của M9 không đổi. Chưa đặt đích: không in,
// không gọi gì, trả nil (B11).
func backupSyncTail(ctx context.Context, e *env, b *core.Backups, panelDue, verbose bool) *core.SyncResult {
	notYet := func(why string) {
		printf(e.stderr, "%s\n", e.tr(
			"đã sao lưu trên máy, CHƯA lên kho: "+why+"; sẽ đẩy bù ở lượt sau, hoặc: sudo tpan backup push",
			"backed up on this machine, NOT yet on the store: "+why+"; it will be uploaded on a later run, or: sudo tpan backup push"))
	}
	_, ok, err := core.CurrentTargetID(ctx, b.Sites.Settings)
	if err != nil {
		notYet(err.Error())
		return nil
	}
	st, isStore := b.Sites.Store.(*store.Store)
	if !ok || !isStore {
		return nil
	}
	r, err := remoteOf(e, st)
	if err != nil {
		notYet(err.Error())
		return nil
	}
	res, err := r.Sync(ctx, core.SyncOptions{Wait: true, PanelDue: panelDue}, newProgress(e.stderr, e.lang, verbose, engine.Plan{}).onEvent)
	auditSync(ctx, e, st, res)
	if len(res.Pushed) > 0 {
		printf(e.stderr, "%s %s\n", e.tr("đã lên kho:", "uploaded to the store:"), strings.Join(res.Pushed, ", "))
	}
	switch {
	case err != nil:
		notYet(err.Error())
	case !res.Ran:
		notYet(res.Skipped)
	case res.TargetError != "":
		notYet(res.TargetError)
	case len(res.Failed) > 0:
		f := res.Failed[0]
		why := f.ID + ": " + f.Error
		if n := len(res.Failed) - 1; n > 0 {
			why += e.tr(" (và "+strconv.Itoa(n)+" bản khác)", " (and "+strconv.Itoa(n)+" more)")
		}
		notYet(why)
	}
	if res.PanelError != "" {
		printf(e.stderr, "%s %s\n", e.tr("gói panel chưa lên kho:", "panel bundle not uploaded:"), res.PanelError)
	}
	return &res
}

// backupWhere là cột "Ở ĐÂU" của `backup list` (mục 3.12, 3.16). cur rỗng:
// chưa đặt đích, nên mọi dòng stored đều ở "đích khác".
func backupWhere(e *env, b core.Backup, cur string) string {
	switch {
	case b.Remote == site.RemoteFailed:
		return e.tr("đẩy lỗi", "push failed")
	case b.Remote == site.RemoteStored && b.RemoteTarget != cur:
		return e.tr("kho (đích khác)", "other target")
	case b.Remote == site.RemoteStored && b.Local:
		return e.tr("máy+kho", "both")
	case b.Remote == site.RemoteStored:
		return e.tr("kho", "remote")
	}
	return e.tr("máy", "machine")
}

func printRemoteListing(e *env, l core.RemoteListing) {
	w := e.stdout
	if len(l.Objects) == 0 {
		printf(w, "%s\n", e.tr("Kho chưa có object nào dưới tiền tố của đích.", "The store has no object under the target's prefix."))
	} else {
		printf(w, "%-60s %10s %-16s %s\n", "KEY", e.tr("CỠ", "SIZE"), e.tr("LÚC SỬA", "MODIFIED"), e.tr("BẢN", "BACKUP"))
		for _, o := range l.Objects {
			known := o.BackupID + " " + o.Domain
			switch {
			case o.BackupID != "":
			case strings.HasPrefix(o.Key, "panel/"):
				known = e.tr("gói panel", "panel bundle")
			default:
				known = e.tr("kho có mà tpan.db không biết", "on the store, unknown to tpan.db")
			}
			printf(w, "%-60s %10s %-16s %s\n", o.Key, humanSize(o.Size), o.Modified.Local().Format("2006-01-02 15:04"), known)
		}
	}
	if len(l.Missing) > 0 {
		printf(w, "\n%s\n", e.tr("Bản tpan.db ghi là đã lên kho mà kho không còn (đẩy lại: sudo tpan backup push <id>):",
			"Copies tpan.db records as uploaded that the store no longer has (upload again: sudo tpan backup push <id>):"))
		for _, b := range l.Missing {
			printf(w, "  %s %s\n", b.ID, b.Domain)
		}
	}
	if l.OtherTarget > 0 {
		printf(w, "\n%s %d\n", e.tr("Bản ở đích khác (không liệt kê ở đây):", "Copies in another target (not listed here):"), l.OtherTarget)
	}
}

// restorePull lo bản khôi phục chỉ còn trên kho (mục 3.6, R15). Trả
// pulled=true khi đã tải về xong và người gọi dựng lại PrepareRestore; trả
// pulled=false, err=nil khi lệnh đã xong (dry-run in câu R15); err khác nil
// khi đã từ chối, huỷ hay tải lỗi (đã in).
func restorePull(ctx context.Context, e *env, f planFlags, b *core.Backups, src core.Backup) (pulled bool, err error) {
	size := humanSize(src.Size)
	msg := i18n.T("bản "+src.ID+" ở kho (cỡ "+size+"), cần tải về trước khi xem được bản xem trước khôi phục; tải về: sudo tpan backup pull "+src.ID,
		"backup "+src.ID+" is in the store ("+size+"); it must be downloaded before a restore preview can be shown; download: sudo tpan backup pull "+src.ID)
	if f.dryRun {
		// Không tải trong lúc xem trước: tải là ghi file và đổi dòng backups.
		if f.asJSON {
			return false, writeJSON(e.stdout, map[string]any{"pull_required": true, "id": src.ID, "size": src.Size, "message": msg.In(e.lang)})
		}
		printf(e.stdout, "%s\n", msg.In(e.lang))
		return false, nil
	}
	if !f.yes {
		// Hỏi trước khi tải: tải vài GB rồi mới hỏi thì "không" là phí cả lượt tải.
		if f.asJSON || !e.isTerminal() {
			return false, fail(e, f.asJSON, &core.RefusedError{Code: "confirm_required", Message: i18n.T(
				"bản "+src.ID+" ở kho, phải tải về trước khi khôi phục; không hỏi xác nhận được (không có terminal hoặc đang dùng --json): chạy lại với --yes",
				"backup "+src.ID+" is in the store and must be downloaded before restoring; cannot ask for confirmation (no terminal, or --json is set): run again with --yes")})
		}
		printf(e.stdout, "%s\n", msg.In(e.lang))
		if !confirm(e) {
			printf(e.stdout, "%s\n", e.tr("Đã huỷ, không thay đổi gì.", "Cancelled, nothing changed."))
			return false, &exitCode{1}
		}
	}
	st, ok := b.Sites.Store.(*store.Store)
	if !ok {
		return false, errors.New("tải về: chưa có DB của tpan")
	}
	r, err := remoteOf(e, st)
	if err != nil {
		return false, err
	}
	// --json: stdout chỉ dành cho Report của job khôi phục; job tải về in ra stderr.
	rep, err := r.Pull(ctx, src.ID, planProgress(e, f, engine.Plan{}).onEvent)
	cliAudit(ctx, e, st, "backup.pull", rep.Job.Target, rep.Job)
	if err != nil && rep.Job.ID == "" {
		return false, fail(e, f.asJSON, err)
	}
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		// Tải hỏng thì Report của job tải là kết quả của lệnh. Lý do ra cả
		// stderr: Report chữ nằm ở stdout, script đọc lỗi ở stderr.
		why := rep.Job.Error
		if err != nil {
			why = err.Error()
		}
		printf(e.stderr, "%s %s: %s\n", e.tr("Tải bản từ kho về lỗi, chưa khôi phục gì:", "Downloading the backup from the store failed, nothing restored:"), src.ID, why)
		_, ferr := finishPlanned(ctx, e, f, nil, "", "", rep, err, nil)
		return false, ferr
	}
	if f.asJSON {
		printf(e.stderr, "%s %s (job %s)\n", e.tr("Đã tải về máy:", "Downloaded:"), src.ID, rep.Job.ID)
	} else {
		printReport(e, rep)
		printf(e.stdout, "\n")
	}
	return true, nil
}
