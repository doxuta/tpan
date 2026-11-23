package core

// Sinh user-data cho cloud-init (M18, mục 3.9 của docs/thay-doi/M18-thiet-ke.md).
// Phần dựng chữ nằm ở gói cloudinit; ở đây là phần "ca sử dụng": kiểm đầu vào
// của người dùng, ghi lại sites.yaml từ cấu trúc đã kiểm, chọn nguồn sha256,
// sinh mật khẩu quản trị cho máy mới. CLI (`tpan cloud-init render`) và API
// (`POST /api/v1/cloud-init/render`) cùng gọi Render.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cloudinit"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// cloudInitInvalid dựng lời từ chối cloudinit_invalid (API trả 400).
func cloudInitInvalid(vi, en string) *RefusedError {
	return refuse(CodeCloudInitInvalid, i18n.T(vi, en))
}

// Render sinh user-data chế độ site mới (mục 3.9). Mật khẩu quản trị mới
// sinh bằng sitePassword, chỉ bản băm (c.Hash) vào user-data.
func (c *CloudInit) Render(ctx context.Context, in CloudInitInput) (CloudInitResult, error) {
	// Từ chối bằng cloudinit_invalid (không phải web_invalid của ParseWeb): API
	// hứa mọi lỗi đầu vào của route này là một trong hai mã cloudinit_* (400).
	if in.Web != Nginx && in.Web != Apache {
		return CloudInitResult{}, cloudInitInvalid(
			fmt.Sprintf("web server phải là nginx hoặc apache (nhận được %q)", string(in.Web)),
			fmt.Sprintf("the web server must be nginx or apache (got %q)", string(in.Web)))
	}
	if in.MaxBytes < 0 {
		return CloudInitResult{}, cloudInitInvalid("--max-bytes phải là số dương", "--max-bytes must be positive")
	}
	if c.Hash == nil {
		return CloudInitResult{}, errors.New("cloud-init: thiếu hàm băm mật khẩu (CloudInit.Hash)")
	}

	sitesYAML, err := cloudInitSites(in.Sites)
	if err != nil {
		return CloudInitResult{}, err
	}

	base := in.Base
	if base == "" {
		// Bản build tay có tên lạ thì không đoán được nơi phát hành.
		if !cloudinit.VersionOK(c.Version) {
			return CloudInitResult{}, cloudInitInvalid(
				fmt.Sprintf("bản tpan đang chạy (%q) không có tên bản phát hành dùng được; chỉ nơi đặt file chạy bằng --base URL", c.Version),
				fmt.Sprintf("the running tpan build (%q) has no usable release name; give the binary location with --base URL", c.Version))
		}
		base = fmt.Sprintf(cloudinit.DefaultBaseFormat, c.Version)
	}
	// Kiểm Base TRƯỚC khi tải gì: giá trị chưa qua kiểm không được vào argv
	// của curl hay vào câu lỗi.
	if err := cloudinit.CheckBase(base); err != nil {
		return CloudInitResult{}, cloudInitErr(err)
	}

	sumA, sumB, err := c.sums(ctx, in, base)
	if err != nil {
		return CloudInitResult{}, err
	}

	pw, err := sitePassword()
	if err != nil {
		return CloudInitResult{}, err
	}
	hash, err := c.Hash(pw.Reveal())
	if err != nil {
		return CloudInitResult{}, fmt.Errorf("băm mật khẩu quản trị của máy mới: %w", err)
	}
	cfg := cloudinit.Config{Version: 1, Web: string(in.Web), Private: in.Private, AdminHash: hash}
	if sitesYAML != nil {
		cfg.SitesFile = cloudinit.SitesPath
	}
	ud, err := cloudinit.Render(cloudinit.Params{
		Version: c.Version, Base: base, SumAMD64: sumA, SumARM64: sumB,
		Config: cfg, SitesYAML: sitesYAML, MaxBytes: in.MaxBytes,
	})
	if err != nil {
		return CloudInitResult{}, cloudInitErr(err)
	}
	return CloudInitResult{UserData: ud, Bytes: len(ud), AdminPassword: pw}, nil
}

// cloudInitSites kiểm file site và GHI LẠI nó từ cấu trúc đã kiểm (B12): mỗi
// dòng chỉ domain, title, admin_email. Không chép nguyên văn file người dùng
// đưa, nên chú thích, khoá YAML lạ, neo/alias hay chuỗi khéo léo không bao giờ
// tới máy mới. nil: không có file site.
func cloudInitSites(f *SitesFile) ([]byte, error) {
	if f == nil {
		return nil, nil
	}
	list, err := f.Resolve()
	if err != nil {
		return nil, err
	}
	out := SitesFile{Sites: make([]SitesEntry, 0, len(list))}
	for i, s := range list {
		where := fmt.Sprintf("dòng %d (%s)", i+1, s.Input.Domain)
		whereEn := fmt.Sprintf("line %d (%s)", i+1, s.Input.Domain)
		// Ba ca dưới chắc chắn hỏng ở lần khởi động đầu (không ai ở đó để
		// sửa), nên từ chối ngay lúc sinh.
		switch {
		case s.Input.Email.IsZero():
			return nil, cloudInitInvalid(where+": thiếu admin_email (ghi ở dòng hoặc ở defaults); WordPress không cài được khi không có email quản trị",
				whereEn+": no admin_email (set it on the line or in defaults); WordPress cannot be installed without an admin email")
		case s.Input.Owner != "" && s.Input.Owner != account.AdminName:
			return nil, cloudInitInvalid(where+": owner phải để trống hoặc là admin; máy mới chưa có user nào khác",
				whereEn+": owner must be empty or admin; the new machine has no other user yet")
		case s.SSL:
			return nil, cloudInitInvalid(where+": không nhận ssl: true; lúc máy mới khởi động, DNS chưa trỏ về nó nên Let's Encrypt chắc chắn từ chối. Bật SSL sau bằng tpan ssl issue",
				whereEn+": ssl: true is not accepted; when the new machine first boots DNS does not point at it yet, so Let's Encrypt is sure to refuse. Turn SSL on later with tpan ssl issue")
		}
		out.Sites = append(out.Sites, SitesEntry{Domain: s.Input.Domain.String(), Title: s.Input.Title, AdminEmail: s.Input.Email.String()})
	}
	b, err := out.Marshal()
	if err != nil {
		return nil, err
	}
	// Marshal chèn dòng chú thích của `tpan export` (nói file thiếu title và
	// email), sai với file này và tốn chỗ trong trần 16 KB: bỏ đi.
	return bytes.TrimPrefix(b, []byte(sitesFileHeader)), nil
}

// sums chọn sha256 của hai file chạy theo thứ tự: hai trường có sẵn, file
// SHA256SUMS người dùng đưa, rồi tải <base>/SHA256SUMS (chỉ CLI, chỉ https).
func (c *CloudInit) sums(ctx context.Context, in CloudInitInput, base string) (amd64, arm64 string, err error) {
	switch {
	case in.SumAMD64 != "" && in.SumARM64 != "":
		return in.SumAMD64, in.SumARM64, nil
	case len(in.Sums) > 0:
		a, b, err := cloudinit.ParseSums(in.Sums)
		if err != nil {
			return "", "", cloudInitErr(err)
		}
		return a, b, nil
	case !in.AllowFetch:
		// API (mục 3.11, S12): server không tải gì từ base do người gọi đưa.
		return "", "", cloudInitInvalid("cần cả sha256_amd64 và sha256_arm64 (64 ký tự hex) của hai file chạy",
			"both sha256_amd64 and sha256_arm64 (64 hex characters) of the two binaries are required")
	case !strings.HasPrefix(base, "https://"):
		// S12: SHA256SUMS lấy qua http:// thì kẻ đứng giữa đổi được cả file
		// chạy lẫn sha256 của nó; sha256 phải đến từ file người chủ có sẵn.
		return "", "", cloudInitInvalid("--base là http:// nên không tự tải SHA256SUMS được (kẻ đứng giữa sửa được cả hai); đưa file SHA256SUMS bằng --sums FILE",
			"--base is http://, so SHA256SUMS is not fetched (a man in the middle could change both); give the SHA256SUMS file with --sums FILE")
	}
	url := base + "/SHA256SUMS"
	res, err := c.Sys.Run(ctx, system.Cmd{Name: "curl", Args: []string{
		"-fsSL", "--proto", "=https", "--proto-redir", "=https", "--max-time", "60", url,
	}})
	if err != nil {
		msg := cleanCtl(err.Error())
		return "", "", cloudInitInvalid(
			fmt.Sprintf("không tải được %s: %s; tải file đó về rồi chạy lại với --sums FILE", url, msg),
			fmt.Sprintf("could not download %s: %s; download it yourself and run again with --sums FILE", url, msg))
	}
	a, b, err := cloudinit.ParseSums([]byte(res.Stdout))
	if err != nil {
		return "", "", cloudInitErr(err)
	}
	return a, b, nil
}

// cloudInitErr đổi lỗi của gói cloudinit thành lời từ chối.
func cloudInitErr(err error) error {
	var inv *cloudinit.InvalidError
	if errors.As(err, &inv) {
		return refuse(CodeCloudInitInvalid, i18n.T(cleanCtl(inv.Msg.Vi), cleanCtl(inv.Msg.En)))
	}
	var big *cloudinit.TooLargeError
	if errors.As(err, &big) {
		// Bảng giới hạn ở mục 2.5 (tra ngày 20/11/2026). Mặc định 16 KB là trần
		// nhỏ nhất (AWS EC2) để file chạy được ở mọi nơi đã tra.
		return refuse(CodeCloudInitTooLarge, i18n.T(
			fmt.Sprintf("user-data %d byte, lớn hơn mức tối đa %d byte. Giới hạn của nhà cung cấp: AWS EC2 16 KB, OpenStack 64 KB, Azure 64 KB, GCP 256 KB. Bớt site, hoặc nếu nhà cung cấp của bạn nhận file lớn hơn thì nâng mức tối đa bằng --max-bytes N (API: max_bytes)", big.Size, big.Max),
			fmt.Sprintf("user-data is %d bytes, above the %d-byte limit. Provider limits: AWS EC2 16 KB, OpenStack 64 KB, Azure 64 KB, GCP 256 KB. Use fewer sites, or if your provider accepts more, raise the limit with --max-bytes N (API: max_bytes)", big.Size, big.Max)))
	}
	return err
}
