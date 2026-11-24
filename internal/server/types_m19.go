package server

// Hợp đồng HTTP của M19 (docs/thay-doi/M19-thiet-ke.md mục 3.7): tên phụ của
// site và màn "Tên miền". Dòng của GET .../names là core.NameView (đã có thẻ
// JSON); luật chuyển hướng ở RedirectAdd và RedirectView.

// AliasAdd là thân của POST /api/v1/sites/{domain}/names.
type AliasAdd struct {
	Name string `json:"name"`
	// Mode: "redirect" (mặc định khi rỗng) hoặc "serve".
	Mode string `json:"mode,omitempty"`
}

// AliasMode là thân của POST /api/v1/sites/{domain}/names/{name}/mode.
type AliasMode struct {
	Mode string `json:"mode"`
}

// DomainRow là một dòng của GET /api/v1/domains: một site trong phạm vi người
// xem, các tên phụ active của nó và số luật chuyển hướng.
type DomainRow struct {
	Site      string   `json:"site"`
	Names     []string `json:"names"`
	Redirects int      `json:"redirects"`
}

// RedirectAdd là thân của POST /api/v1/sites/{domain}/redirects.
type RedirectAdd struct {
	Source string `json:"source"`
	Target string `json:"target"`
	// Code: 301 (mặc định khi rỗng hay 0) hoặc 302.
	Code      int  `json:"code,omitempty"`
	KeepQuery bool `json:"keep_query,omitempty"`
}

// RedirectView là một dòng của GET /api/v1/sites/{domain}/redirects.
type RedirectView struct {
	ID        int64  `json:"id"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Code      int    `json:"code"`
	KeepQuery bool   `json:"keep_query"`
	// State: "active" (RedirectList chỉ trả luật đã có trong vhost).
	State string `json:"state"`
}
