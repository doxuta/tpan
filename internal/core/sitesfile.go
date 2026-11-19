package core

// Hợp đồng M6, đợt 0: file sites.yaml (mục 7 của docs/KE-HOACH.md) cho `tpan
// apply -f`, `tpan export` và thân JSON của POST /api/v1/apply. Thân viết ở
// đợt 1. Apply (apply.go) dùng SitesFile.Resolve.

import (
	"context"
	"errors"
	"io"
)

var errSitesFileTODO = errors.New("core: sites.yaml chưa viết (M6 đợt 1)")

// SitesFile là nội dung của sites.yaml (mục 7). Cũng là thân JSON của
// POST /api/v1/apply (cùng tên trường).
type SitesFile struct {
	Defaults SitesDefaults `yaml:"defaults,omitempty" json:"defaults"`
	Sites    []SitesEntry  `yaml:"sites" json:"sites"`
}

// SitesDefaults áp cho mọi dòng không tự ghi trường đó.
type SitesDefaults struct {
	AdminEmail string `yaml:"admin_email,omitempty" json:"admin_email,omitempty"`
	SSL        bool   `yaml:"ssl,omitempty" json:"ssl,omitempty"`
}

// SitesEntry là một site trong file. Domain bắt buộc; Title rỗng thì dùng
// domain; AdminEmail rỗng thì lấy Defaults; SSL nil thì lấy Defaults.
type SitesEntry struct {
	Domain     string `yaml:"domain" json:"domain"`
	Title      string `yaml:"title,omitempty" json:"title,omitempty"`
	AdminEmail string `yaml:"admin_email,omitempty" json:"admin_email,omitempty"`
	SSL        *bool  `yaml:"ssl,omitempty" json:"ssl,omitempty"`
}

// MaxSitesFile: trần kích thước file và số dòng, để một file lạ không làm
// tpan (root) đọc vô hạn.
const (
	MaxSitesFileBytes = 1 << 20
	MaxSitesEntries   = 500
)

// ParseSitesFile đọc sites.yaml: tối đa MaxSitesFileBytes, KnownFields (khoá
// lạ là lỗi, để gõ sai `admin_mail` không bị lặng lẽ bỏ qua), đúng một tài
// liệu YAML. Chỉ kiểm cú pháp; kiểm giá trị ở SitesFile.Resolve.
// Lỗi là *RefusedError code "sites_file_invalid" kèm dòng của YAML nếu có.
func ParseSitesFile(r io.Reader) (SitesFile, error) { return SitesFile{}, errSitesFileTODO }

// Marshal ghi file ra YAML (cho `tpan export`), có dòng chú thích đầu nói file
// do tpan export sinh và thiếu gì (title, email không lưu trong tpan).
func (f SitesFile) Marshal() ([]byte, error) { return nil, errSitesFileTODO }

// Resolve kiểm và chuẩn hoá từng dòng: validate.ParseDomain, ParseEmail (dòng
// rồi tới Defaults), checkSiteTitle, không trùng domain (sau chuẩn hoá), không
// quá MaxSitesEntries. Lỗi đầu tiên là *RefusedError nói rõ dòng thứ mấy (tính
// từ 1) và domain; không trả kết quả dở. Email thiếu chưa phải lỗi ở đây (site
// đã có không cần email); PrepareApply từ chối khi site MỚI không có email.
func (f SitesFile) Resolve() ([]ApplySite, error) { return nil, errSitesFileTODO }

// ApplySite là một dòng của file sau khi đã kiểm.
type ApplySite struct {
	Input SiteInput `json:"input"`
	SSL   bool      `json:"ssl"`
}

// Export dựng SitesFile từ trạng thái hiện tại: mọi site active, sắp theo
// domain, chỉ domain và ssl (title và email không lưu trong tpan; mục 7 ghi
// "export in ra file từ trạng thái hiện tại"). Site trong thùng rác không có.
func (s *Sites) Export(ctx context.Context) (SitesFile, error) { return SitesFile{}, errSitesFileTODO }
