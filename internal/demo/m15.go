package demo

// Phần M15 của máy giả (mục 6.14 của docs/KE-HOACH.md): khối "Phiên bản và
// cập nhật" xem được ở chế độ --demo.
//
//   - "Kiểm bản mới": nguồn tải mặc định (core.DefaultSelfURL) trả lời hai
//     lệnh curl của steps.SelfLatest. Máy giả không có file /usr/local/bin/tpan
//     nên core chỉ so tên bản: luôn ra "có bản mới" (demoSelfLatest).
//   - "Quay về bản trước": có sẵn /usr/local/bin/tpan.old, và `tpan.old
//     version` in một dòng phiên bản để hộp xác nhận có tên bản. Máy giả không
//     có /var/lib/tpan/tpan.db nên core bỏ qua phép thử `tpan.old site list`.
//   - Đơn vị tạm tpan-self-update không bao giờ chạy: `systemctl is-active`
//     của m10.go trả "inactive", mã 3, như systemd thật với đơn vị không có.
//   - Chạy thật (systemd-run) bị server chặn ở chế độ demo (409
//     demo_no_self_update), nên máy giả không trả lời lệnh đó.

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/system"
)

const (
	// demoSelfLatest là tên bản "đang phát hành" ở nguồn tải giả: rõ là ví dụ
	// và mới hơn mọi bản thật, để demo luôn hiện được trạng thái "có bản mới".
	demoSelfLatest = "v9.9.9-demo"
	// demoSelfOld là bản của file tpan.old trên máy giả.
	demoSelfOld = "v0.1.2"
)

// demoSelfSum là sha256 ghi trong SHA256SUMS giả cho tpan-linux-<arch>: cố
// định, không của file thật nào.
func demoSelfSum(arch string) string {
	sum := sha256.Sum256([]byte("tpan-linux-" + arch + " " + demoSelfLatest))
	return hex.EncodeToString(sum[:])
}

// m15Setup đặt file và handler của phần M15.
func (m *machine) m15Setup() {
	old := core.TpanBin + ".old"
	m.SetFile(old, []byte("tpan "+demoSelfOld+" (file giả của demo)\n"), 0o755, rootOwner)
	m.Handle([]string{old, "version"}, func(system.Cmd) system.Result {
		return exit(0, "tpan "+demoSelfOld+" (commit 0000000, build 2026-01-01T00:00:00Z)\n", "")
	})
	// Đúng đầu dòng lệnh của steps.SelfLatest, kể cả thời gian chờ 15 giây.
	// Tiền tố dài nên thắng handler `curl` chung của m10.go mà không giẫm lên
	// `curl -fsS` của m12.go (ghi chú PHP) hay `curl -fsSL -o` (tải WordPress,
	// WP-CLI): máy giả chọn handler có tiền tố khớp dài nhất.
	m.Handle([]string{"curl", "-fsSL", "--max-time", "15", "--proto-redir", "=https"}, selfLatestDemo)
}

// selfLatestDemo trả lời hai file của nguồn tải mặc định. URL khác (ai đó đặt
// update.self_url riêng) thì 404 như curl -f: demo không có nguồn tải nào khác.
func selfLatestDemo(c system.Cmd) system.Result {
	switch c.Args[len(c.Args)-1] {
	case core.DefaultSelfURL + "/SHA256SUMS":
		return exit(0, demoSelfSum("amd64")+"  tpan-linux-amd64\n"+demoSelfSum("arm64")+"  tpan-linux-arm64\n", "")
	case core.DefaultSelfURL + "/VERSION":
		return exit(0, demoSelfLatest+"\n", "")
	}
	return exit(22, "", "curl: (22) The requested URL returned error: 404\n")
}
