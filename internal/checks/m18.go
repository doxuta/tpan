package checks

// Hai phép kiểm của M18 về kho sao lưu S3 (mục 3.13 của
// docs/thay-doi/M18-thiet-ke.md). Hợp đồng ở m18_types.go. Gói này không import
// core hay s3 nên cả hai nhận hàm do health đưa vào; health chỉ thêm chúng khi
// máy đã đặt đích (máy không đặt đích thì kết quả health không đổi).

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// s3FreshMaxAge: một ngày lịch cộng một ngày trễ, cùng ngưỡng với
// tpan.backup:db của M9. Gói panel cũ hơn mức này nghĩa là lịch đã lỡ ít nhất
// một đêm.
const s3FreshMaxAge = 48 * time.Hour

// s3ListMax: số domain hay số bản tối đa nêu tên trong một câu tóm tắt. Máy
// có trăm site mà kho hỏng thì câu tóm tắt vẫn đọc được trong một dòng.
const s3ListMax = 5

// s3Recheck là lệnh kiểm lại, đặt cuối ManualFix của hai phép kiểm.
var s3Recheck = line("sudo", "tpan", "health")

// s3Names ghép tối đa s3ListMax tên (đã làm sạch), phần dư thành "và N nữa".
func s3Names(names []string) (vi, en string) {
	more := len(names) - s3ListMax
	if more > 0 {
		names = names[:s3ListMax]
	}
	clean := make([]string, len(names))
	for i, n := range names {
		clean[i] = sslClean(n)
	}
	vi = strings.Join(clean, ", ")
	en = vi
	if more > 0 {
		vi += fmt.Sprintf(" và %d mục nữa", more)
		en += fmt.Sprintf(" and %d more", more)
	}
	return vi, en
}

// S3Target (ID "tpan.s3:target", StageTPAN) kiểm kho còn dùng được: nối được,
// khoá còn được nhận, bucket còn đó, và các bản mới nhất đã lên kho vẫn còn
// nguyên object. health bọc nó bằng Heavy vì nó gọi mạng.
//
// Vì sao: bản sao lưu chỉ có ích khi lấy lại được. Kho đổi khoá, bucket bị xoá
// hay object bị dọn từ phía nhà cung cấp thì máy vẫn chạy bình thường và không
// ai biết cho đến ngày cần khôi phục. probe do core dựng (Check và Head từng
// bản); probe lỗi nghĩa là chưa kiểm được (ví dụ chưa đọc được hai khoá), nên
// là Unknown chứ không phải Fail: không đổ lỗi cho kho khi chưa hỏi được nó.
// Problem (lỗi của đích) đi trước Missing: kho không trả lời thì "mất bản"
// không có nghĩa.
func S3Target(probe func(ctx context.Context) (S3Probe, error)) Check {
	b := base{id: "tpan.s3:target", stage: StageTPAN,
		title: i18n.T("Kho sao lưu ngoài máy dùng được", "The off-machine backup store works")}
	return checkFunc(func(ctx context.Context, _ system.System) Result {
		p, err := probe(ctx)
		if err != nil {
			return b.unknown(i18n.T("không hỏi được kho sao lưu ("+sslClean(err.Error())+")",
				"could not query the backup store ("+sslClean(err.Error())+")"), p.Evidence...)
		}

		if p.Problem != "" {
			return s3TargetProblem(b, p)
		}
		if len(p.Missing) > 0 {
			vi, en := s3Names(p.Missing)
			return b.bad(Fail,
				i18n.T("kho mất bản: "+vi, "the store lost backups: "+en),
				i18n.T("Các bản này đã lên kho và đã đọc lại khớp, nhưng giờ object không còn hoặc sai cỡ: có người xoá, bucket bị làm rỗng hoặc kho đổi chính sách giữ file. Bản còn trên máy thì đẩy lại được; bản đã dọn khỏi máy thì kho là chỗ giữ cuối cùng.",
					"These backups were uploaded and read back correctly, but now the object is gone or the wrong size: someone deleted it, the bucket was emptied or the store changed its retention rules. A copy still on this machine can be pushed again; one already removed from the machine had the store as its last home."),
				[]string{line("sudo", "tpan", "backup", "push"), s3Recheck}, p.Evidence...)
		}
		return b.pass(i18n.T("Kho sao lưu trả lời và còn đủ các bản mới nhất",
			"The backup store answers and still holds the newest copies"), p.Evidence...)
	})
}

// s3TargetProblem dựng kết quả Fail cho một mã s3_* của đích. Mã lạ rơi vào
// nhánh s3_error: vẫn là "kho có vấn đề", chỉ không biết nói cụ thể hơn.
func s3TargetProblem(b base, p S3Probe) Result {
	var what, cause i18n.Text
	switch p.Problem {
	case "s3_unreachable":
		what = i18n.T("không nối được tới kho", "cannot reach the store")
		cause = i18n.T("Máy không nối được tới địa chỉ kho: sai địa chỉ, kho đang tắt, tường lửa chặn cổng ra, hoặc DNS không phân giải. Địa chỉ có thể đổi khi nhà cung cấp chuyển vùng.",
			"This machine cannot connect to the store address: a wrong address, the store is down, a firewall blocks the outgoing port, or DNS does not resolve. The address can change when the provider moves regions.")
	case "s3_auth_failed":
		what = i18n.T("kho từ chối khoá truy cập", "the store rejected the access key")
		cause = i18n.T("Kho nhận được yêu cầu nhưng không nhận khoá: khoá đã bị thu hồi hoặc đổi, gõ sai, hoặc khoá không có quyền với bucket này.",
			"The store got the request but does not accept the key: it was revoked or rotated, mistyped, or has no rights on this bucket.")
	case "s3_bucket_not_found":
		what = i18n.T("kho không có bucket này", "the store has no such bucket")
		cause = i18n.T("Kho trả lời nhưng không có bucket đã đặt: bucket bị xoá, gõ sai tên, hoặc đang trỏ nhầm vùng hay nhầm kho.",
			"The store answers but has no bucket by that name: it was deleted, the name is misspelled, or the region or store is wrong.")
	default: // s3_error và mọi mã chưa biết
		what = i18n.T("kho trả lỗi", "the store returned an error")
		cause = i18n.T("Kho trả lời nhưng báo lỗi (xem câu lỗi trong tóm tắt): có thể kho đang quá tải, hết hạn mức, hoặc từ chối yêu cầu vì lý do riêng của nhà cung cấp.",
			"The store answered with an error (see the message in the summary): it may be overloaded, over quota, or refusing the request for a provider-specific reason.")
	}
	vi, en := "Kho sao lưu: "+what.Vi, "Backup store: "+what.En
	if d := sslClean(p.Detail); d != "" {
		vi += " (" + d + ")"
		en += " (" + d + ")"
	}
	return b.bad(Fail, i18n.T(vi, en), cause, []string{
		line("sudo", "tpan", "backup", "target", "set", "--endpoint", "URL", "--bucket", "BUCKET"),
		line("sudo", "tpan", "backup", "target", "test"),
	}, p.Evidence...)
}

// S3Fresh (ID "tpan.s3:fresh", StageTPAN, nhẹ: chỉ đọc tpan.db và settings)
// cảnh báo khi việc đẩy lên kho đang chậm hay đang hỏng.
//
// Bốn ý, đúng như m18_types.go: (a) có bản đẩy lỗi hoặc lần đẩy gần nhất hỏng
// vì kho; (b) lịch bật mà có site thiếu bản đầy đủ trên kho trong 48 giờ; (c)
// lịch bật, có khoá, mà gói panel (tpan.db) trên kho quá cũ hoặc lần đẩy gói
// lỗi; (d) chưa có khoá sao lưu nên không đẩy được gì. Chỉ Warn, không Fail:
// máy vẫn chạy, chỉ là bản sao ngoài máy đang tụt lại. Mốc 48 giờ so với now()
// chứ không phải time.Now để test đặt được giờ.
func S3Fresh(state func(ctx context.Context) (S3State, error), now func() time.Time) Check {
	if now == nil {
		now = time.Now
	}
	b := base{id: "tpan.s3:fresh", stage: StageTPAN,
		title: i18n.T("Bản sao lưu ngoài máy được đẩy đều", "Off-machine backups are being pushed on time")}
	return checkFunc(func(ctx context.Context, _ system.System) Result {
		st, err := state(ctx)
		if err != nil {
			return b.unknown(i18n.T("không đọc được trạng thái đẩy lên kho ("+sslClean(err.Error())+")",
				"could not read the push state ("+sslClean(err.Error())+")"))
		}

		// Bằng chứng: các con số đã đọc, không có bí mật (LastError đã được làm
		// sạch từ nơi ghi). Giao diện hiện nó khi người dùng bấm vào dấu trạng thái.
		panelLast := "none"
		if !st.PanelLast.IsZero() {
			panelLast = st.PanelLast.UTC().Format(time.RFC3339)
		}
		ev := Evidence{
			Command: "read tpan.db (backups, settings s3.*)",
			Output: fmt.Sprintf("failed=%d stale=%d schedule=%t has_key=%t panel_last=%s target_error=%t panel_error=%t",
				st.Failed, len(st.Stale), st.Schedule, st.HasKey, panelLast, st.TargetError != "", st.PanelError != ""),
		}

		var viParts, enParts []string
		add := func(vi, en string) { viParts, enParts = append(viParts, vi), append(enParts, en) }

		// (a)
		if st.Failed > 0 || st.TargetError != "" {
			var vi, en string
			if st.Failed > 0 {
				names := ""
				namesEn := ""
				if len(st.FailedDomains) > 0 {
					names, namesEn = s3Names(st.FailedDomains)
					names, namesEn = " ("+names+")", " ("+namesEn+")"
				}
				vi = fmt.Sprintf("%d bản chưa lên được kho%s", st.Failed, names)
				en = fmt.Sprintf("%d %s could not be uploaded%s", st.Failed, plural(st.Failed, "backup", "backups"), namesEn)
				if st.LastError != "" {
					vi += ", lỗi gần nhất: " + sslClean(st.LastError)
					en += ", latest error: " + sslClean(st.LastError)
				}
			}
			if st.TargetError != "" {
				sep := ""
				if vi != "" {
					sep = "; "
				}
				vi += sep + "lượt đẩy gần nhất không nối được kho: " + sslClean(st.TargetError)
				en += sep + "the latest push could not use the store: " + sslClean(st.TargetError)
			}
			add(vi, en)
		}
		// (b)
		if st.Schedule && len(st.Stale) > 0 {
			vi, en := s3Names(st.Stale)
			add(fmt.Sprintf("%d site chưa có bản đầy đủ nào trên kho trong 48 giờ qua (%s)", len(st.Stale), vi),
				fmt.Sprintf("%d %s without a full backup in the store for 48 hours (%s)", len(st.Stale), plural(len(st.Stale), "site", "sites"), en))
		}
		// (c)
		if st.Schedule && st.HasKey {
			switch {
			case st.PanelError != "":
				add("lần đẩy gói panel gần nhất lỗi: "+sslClean(st.PanelError),
					"the latest panel bundle upload failed: "+sslClean(st.PanelError))
			case st.PanelLast.IsZero():
				add("chưa có gói panel (tpan.db) nào trên kho", "there is no panel bundle (tpan.db) in the store yet")
			case now().Sub(st.PanelLast) > s3FreshMaxAge:
				h := int(now().Sub(st.PanelLast) / time.Hour)
				add(fmt.Sprintf("gói panel trên kho đã %d giờ tuổi", h), fmt.Sprintf("the panel bundle in the store is %d hours old", h))
			}
		}
		// (d)
		missingKey := !st.HasKey
		if missingKey {
			add("chưa có khoá sao lưu nên không đẩy gói panel được", "there is no backup key, so the panel bundle cannot be uploaded")
		}

		if len(viParts) == 0 {
			return b.pass(i18n.T("Bản sao lưu ngoài máy đang được đẩy đều", "Off-machine backups are being pushed on time"), ev)
		}
		fix := []string{line("sudo", "tpan", "backup", "push"), s3Recheck}
		if missingKey {
			fix = []string{line("sudo", "tpan", "backup", "key", "new"), line("sudo", "tpan", "backup", "push"), s3Recheck}
		}
		return b.bad(Warn,
			i18n.T("Bản sao ngoài máy đang tụt lại: "+strings.Join(viParts, "; "), "Off-machine backups are falling behind: "+strings.Join(enParts, "; ")),
			i18n.T("Máy vẫn chạy, nhưng nếu máy mất lúc này thì bản sao trên kho không đủ mới để dựng lại. Thường do kho đổi khoá hay hết chỗ, mạng ra ngoài bị chặn, hoặc lịch backup đã ngừng.",
				"The machine still runs, but if it were lost now the copies in the store would not be recent enough to rebuild from. Usually the store changed keys or is full, outgoing network is blocked, or the backup schedule stopped."),
			fix, ev)
	})
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
