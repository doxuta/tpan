package core

// File sites.yaml (mục 7 của docs/KE-HOACH.md) cho `tpan apply -f`, `tpan
// export` và thân JSON của POST /api/v1/apply. Apply (apply.go) dùng
// SitesFile.Resolve.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// codeSitesFileInvalid là mã lỗi của mọi lỗi cú pháp hoặc cấu trúc của file.
// Lỗi giá trị (domain, email, tiêu đề) giữ mã gốc của validate/checkSiteTitle.
const codeSitesFileInvalid = "sites_file_invalid"

// SitesFile là nội dung của sites.yaml (mục 7). Cũng là thân JSON của
// POST /api/v1/apply (cùng tên trường).
type SitesFile struct {
	Defaults SitesDefaults `yaml:"defaults,omitempty" json:"defaults"`
	Sites    []SitesEntry  `yaml:"sites" json:"sites"`
	// Omitted (M19, S21): thứ có trên máy mà file export chưa mang. Chỉ Export
	// điền; không ra YAML hay JSON, người gọi dùng nó để cảnh báo.
	Omitted ExportOmitted `yaml:"-" json:"-"`
}

// ExportOmitted liệt kê site mang tên phụ hay luật chuyển hướng: sites.yaml
// chưa có chỗ cho chúng, nên `tpan apply -f` trên máy khác không tạo lại. Thêm
// lại bằng `tpan site alias add` và `tpan site redirect add`.
type ExportOmitted struct {
	NameSites []string // site có tên phụ active
	RuleSites []string // site có luật chuyển hướng active
}

// Any cho biết có gì bị bỏ lại.
func (o ExportOmitted) Any() bool { return len(o.NameSites)+len(o.RuleSites) > 0 }

// SitesDefaults áp cho mọi dòng không tự ghi trường đó.
type SitesDefaults struct {
	AdminEmail string `yaml:"admin_email,omitempty" json:"admin_email,omitempty"`
	SSL        bool   `yaml:"ssl,omitempty" json:"ssl,omitempty"`
	// Owner (M13) là chủ của site mới; rỗng là admin (hoặc chính người gọi khi
	// áp theo phạm vi một user, ApplyInput.Scope).
	Owner string `yaml:"owner,omitempty" json:"owner,omitempty"`
}

// SitesEntry là một site trong file. Domain bắt buộc; Title rỗng thì dùng
// domain; AdminEmail, Owner rỗng thì lấy Defaults; SSL nil thì lấy Defaults.
type SitesEntry struct {
	Domain     string `yaml:"domain" json:"domain"`
	Title      string `yaml:"title,omitempty" json:"title,omitempty"`
	AdminEmail string `yaml:"admin_email,omitempty" json:"admin_email,omitempty"`
	SSL        *bool  `yaml:"ssl,omitempty" json:"ssl,omitempty"`
	Owner      string `yaml:"owner,omitempty" json:"owner,omitempty"`
	// App (M29): loại site, "wordpress" (mặc định khi vắng) hoặc "php" (site
	// PHP trống). Export ghi "php" cho site PHP trống; apply tạo đúng loại đó
	// và trả mật khẩu DB của từng site một lần (ApplySiteResult.DBPassword).
	// Site PHP trống không cần admin_email.
	App string `yaml:"app,omitempty" json:"app,omitempty"`
}

// MaxSitesFile: trần kích thước file và số dòng, để một file lạ không làm
// tpan (root) đọc vô hạn.
const (
	MaxSitesFileBytes = 1 << 20
	MaxSitesEntries   = 500
)

// sitesFileErr dựng lỗi sites_file_invalid hai ngôn ngữ. Chuỗi từ thư viện YAML
// có thể chứa tên khoá do người dùng gõ, nên làm sạch ký tự điều khiển trước.
func sitesFileErr(vi, en string) *RefusedError {
	return refuse(codeSitesFileInvalid, i18n.T(cleanCtl(vi), cleanCtl(en)))
}

// cleanCtl đổi ký tự điều khiển thành '?', để lời lỗi không làm bẩn log hay
// giao diện (xuống dòng giả, mã thoát terminal).
func cleanCtl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return '?'
		}
		return r
	}, s)
}

// yamlProblem dịch lỗi của thư viện YAML thành lỗi của ta. Thư viện đã ghi sẵn
// "line N:" trong chuỗi lỗi nên giữ nguyên phần đó, chỉ bỏ tiền tố "yaml: ".
func yamlProblem(err error) *RefusedError {
	msg := strings.TrimPrefix(err.Error(), "yaml: ")
	var te *yaml.TypeError
	if errors.As(err, &te) {
		msg = strings.Join(te.Errors, "; ")
	}
	return sitesFileErr("sites.yaml không hợp lệ: "+msg, "sites.yaml is not valid: "+msg)
}

// ParseSitesFile đọc sites.yaml: tối đa MaxSitesFileBytes, KnownFields (khoá
// lạ là lỗi, để gõ sai `admin_mail` không bị lặng lẽ bỏ qua), đúng một tài
// liệu YAML. Chỉ kiểm cú pháp; kiểm giá trị ở SitesFile.Resolve.
// Lỗi là *RefusedError code "sites_file_invalid" kèm dòng của YAML nếu có.
func ParseSitesFile(r io.Reader) (SitesFile, error) {
	// Đọc thừa 1 byte: nếu đọc được MaxSitesFileBytes+1 byte thì file quá cỡ.
	// Đọc hết vào bộ nhớ (tối đa 1 MiB) để việc kiểm cỡ không phụ thuộc cách
	// bộ giải mã YAML tự đệm dữ liệu.
	raw, err := io.ReadAll(io.LimitReader(r, MaxSitesFileBytes+1))
	if err != nil {
		return SitesFile{}, fmt.Errorf("đọc sites.yaml: %w", err)
	}
	if len(raw) > MaxSitesFileBytes {
		return SitesFile{}, sitesFileErr(
			fmt.Sprintf("sites.yaml lớn quá %d byte", MaxSitesFileBytes),
			fmt.Sprintf("sites.yaml is larger than %d bytes", MaxSitesFileBytes))
	}

	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var f SitesFile
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return SitesFile{}, sitesFileErr("sites.yaml đang trống", "sites.yaml is empty")
		}
		return SitesFile{}, yamlProblem(err)
	}
	// Tài liệu thứ hai (sau dòng "---") sẽ bị bỏ lặng lẽ nếu không kiểm: người
	// dùng tưởng cả hai được áp. Phải gặp đúng io.EOF.
	var extra yaml.Node
	switch err := dec.Decode(&extra); {
	case errors.Is(err, io.EOF):
	case err == nil:
		return SitesFile{}, sitesFileErr(
			"sites.yaml có nhiều hơn một tài liệu YAML (dòng '---'); chỉ nhận một",
			"sites.yaml has more than one YAML document ('---' line); only one is accepted")
	default:
		return SitesFile{}, yamlProblem(err)
	}
	return f, nil
}

// sitesFileHeader là dòng chú thích đầu file export. YAML bỏ qua dòng bắt đầu
// bằng '#', nên đưa file này vào `tpan apply -f` vẫn đọc được.
const sitesFileHeader = `# File này do "tpan export" sinh từ trạng thái hiện tại của máy.
# Chỉ có domain, ssl và owner (khi chủ không phải admin): tpan không lưu
# title và admin_email của từng site, nên hai trường đó không có ở đây. Muốn tạo site mới từ file này thì thêm
# admin_email (ở defaults hoặc từng dòng) và title nếu cần.
# Tên phụ (www., tên miền khác) và luật chuyển hướng của site cũng không có
# trong file này: sau khi áp file, thêm lại bằng "tpan site alias add" và
# "tpan site redirect add".
`

// Marshal ghi file ra YAML (cho `tpan export`), có dòng chú thích đầu nói file
// do tpan export sinh và thiếu gì (title, email không lưu trong tpan).
func (f SitesFile) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(sitesFileHeader)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2) // khớp ví dụ ở mục 7 (mặc định của thư viện là 4)
	if err := enc.Encode(f); err != nil {
		return nil, fmt.Errorf("ghi sites.yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("ghi sites.yaml: %w", err)
	}
	return buf.Bytes(), nil
}

// lineErr bọc lỗi giá trị của dòng thứ line (từ 1): giữ mã gốc (domain_invalid,
// email_invalid, title_invalid) và Checks nếu có, thêm "dòng N" vào đầu lời.
// Cố ý không chép lại giá trị người dùng gõ: validate đã tránh việc đó vì giá
// trị có thể chứa xuống dòng hay ký tự điều khiển.
func lineErr(where string, err error) error {
	var code string
	var msg i18n.Text
	var ve *validate.Error
	var re *RefusedError
	switch {
	case errors.As(err, &ve):
		code, msg = ve.Code, ve.Message
	case errors.As(err, &re):
		code, msg = re.Code, re.Message
	default:
		return sitesFileErr(where+": "+err.Error(), where+": "+err.Error())
	}
	return refuse(code, i18n.T(where+": "+msg.Vi, where+": "+msg.En))
}

// Resolve kiểm và chuẩn hoá từng dòng: validate.ParseDomain, ParseEmail (dòng
// rồi tới Defaults), checkSiteTitle, không trùng domain (sau chuẩn hoá), không
// quá MaxSitesEntries. Lỗi đầu tiên là *RefusedError nói rõ dòng thứ mấy (tính
// từ 1) và domain; không trả kết quả dở. Email thiếu chưa phải lỗi ở đây (site
// đã có không cần email); PrepareApply từ chối khi site MỚI không có email.
func (f SitesFile) Resolve() ([]ApplySite, error) {
	if len(f.Sites) > MaxSitesEntries {
		return nil, sitesFileErr(
			fmt.Sprintf("sites.yaml có %d site, tối đa %d", len(f.Sites), MaxSitesEntries),
			fmt.Sprintf("sites.yaml has %d sites, the limit is %d", len(f.Sites), MaxSitesEntries))
	}

	// Email mặc định kiểm một lần ở đây để lỗi chỉ đúng vào "defaults" thay vì
	// lặp ở mọi dòng dùng nó.
	var defEmail validate.Email
	if f.Defaults.AdminEmail != "" {
		e, err := validate.ParseEmail(f.Defaults.AdminEmail)
		if err != nil {
			return nil, lineErr("defaults.admin_email", err)
		}
		defEmail = e
	}
	if err := checkOwnerField(f.Defaults.Owner); err != nil {
		return nil, lineErr("defaults.owner", err)
	}

	out := make([]ApplySite, 0, len(f.Sites))
	seen := make(map[string]int, len(f.Sites)) // domain đã chuẩn hoá -> dòng đầu tiên
	for i, e := range f.Sites {
		n := i + 1
		where := fmt.Sprintf("dòng %d", n)

		d, err := validate.ParseDomain(e.Domain)
		if err != nil {
			return nil, lineErr(where, err)
		}
		// Từ đây domain đã sạch (chữ thường, ASCII) nên in ra được.
		where = fmt.Sprintf("dòng %d (%s)", n, d)
		if first, dup := seen[d.String()]; dup {
			return nil, sitesFileErr(
				fmt.Sprintf("%s: trùng với dòng %d (cùng domain sau khi chuẩn hoá)", where, first),
				fmt.Sprintf("line %d (%s): same domain as line %d (after normalisation)", n, d, first))
		}
		seen[d.String()] = n

		email := defEmail
		if e.AdminEmail != "" {
			if email, err = validate.ParseEmail(e.AdminEmail); err != nil {
				return nil, lineErr(where, err)
			}
		}

		// Title rỗng nghĩa là "dùng domain" (SiteInput), không phải lỗi; chỉ
		// kiểm khi người dùng có ghi.
		if e.Title != "" {
			if err := checkSiteTitle(e.Title); err != nil {
				return nil, lineErr(where, err)
			}
		}

		ssl := f.Defaults.SSL
		if e.SSL != nil {
			ssl = *e.SSL
		}
		owner := f.Defaults.Owner
		if e.Owner != "" {
			if err := checkOwnerField(e.Owner); err != nil {
				return nil, lineErr(where, err)
			}
			owner = e.Owner
		}
		if !site.App(e.App).Valid() {
			return nil, sitesFileErr(
				fmt.Sprintf("%s: app phải là wordpress hoặc php", where),
				fmt.Sprintf("line %d (%s): app must be wordpress or php", n, d))
		}
		out = append(out, ApplySite{
			Input: SiteInput{Domain: d, Title: e.Title, Email: email, Owner: owner, App: site.App(e.App)},
			SSL:   ssl,
		})
	}
	return out, nil
}

// checkOwnerField kiểm dạng tên chủ trong file (rỗng là không ghi). Có thật
// hay không, có thuộc phạm vi người gọi không thì PrepareApply kiểm từng dòng.
func checkOwnerField(s string) error {
	if s == "" || s == account.AdminName {
		return nil
	}
	_, err := validate.ParseUserName(s)
	return err
}

// ApplySite là một dòng của file sau khi đã kiểm.
type ApplySite struct {
	Input SiteInput `json:"input"`
	SSL   bool      `json:"ssl"`
}

// Export dựng SitesFile từ trạng thái hiện tại: mọi site active, sắp theo
// domain, chỉ domain, ssl và owner (title và email không lưu trong tpan; mục 7
// ghi "export in ra file từ trạng thái hiện tại"). Site trong thùng rác không có.
func (s *Sites) Export(ctx context.Context) (SitesFile, error) {
	return s.ExportScope(ctx, "")
}

// ExportScope là Export chỉ với site của user scope (rỗng là mọi site), cho
// GET /api/v1/export của user thường (M13).
//
// owner chỉ in khi chủ không phải admin: file export của máy chỉ có admin (mọi
// máy trước M13) giữ đúng như cũ, và file đó áp lại vẫn ra cùng chủ vì owner
// rỗng là admin.
func (s *Sites) ExportScope(ctx context.Context, scope string) (SitesFile, error) {
	all, err := s.Store.Sites(ctx)
	if err != nil {
		return SitesFile{}, fmt.Errorf("đọc danh sách site để export: %w", err)
	}
	f := SitesFile{Sites: []SitesEntry{}}
	for _, st := range all {
		if st.Status != site.Active || (scope != "" && ownerOf(st) != scope) {
			continue
		}
		e := SitesEntry{Domain: st.Domain.String()}
		if o := ownerOf(st); o != account.AdminName {
			e.Owner = o
		}
		if st.Plain() {
			e.App = string(site.AppPHP)
		}
		if st.SSL {
			t := true // chỉ ghi khi true: false là mặc định, ghi ra chỉ thêm nhiễu
			e.SSL = &t
		}
		f.Sites = append(f.Sites, e)
		if len(st.Names.Serve)+len(st.Names.Redirect) > 0 {
			f.Omitted.NameSites = append(f.Omitted.NameSites, e.Domain)
		}
		if len(st.Names.Rules) > 0 {
			f.Omitted.RuleSites = append(f.Omitted.RuleSites, e.Domain)
		}
	}
	// Store hứa sắp theo domain nhưng export phải ổn định dù store đổi cách sắp.
	slices.SortFunc(f.Sites, func(a, b SitesEntry) int { return strings.Compare(a.Domain, b.Domain) })
	slices.Sort(f.Omitted.NameSites)
	slices.Sort(f.Omitted.RuleSites)
	return f, nil
}
