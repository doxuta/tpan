// Package i18n giữ kiểu chữ song ngữ dùng chung. Nó là gói lá (không import gói
// nào của tpan) để engine, checks, steps, store và server cùng dùng được mà
// không tạo vòng import.
package i18n

// Text là một câu viết sẵn bằng hai ngôn ngữ. Giao diện và CLI chọn bản theo
// ngôn ngữ người dùng; chuỗi do AI sinh ra không bao giờ nằm trong kiểu này.
type Text struct {
	Vi string `json:"vi"`
	En string `json:"en"`
}

// T dựng một Text. Viết `i18n.T("Cài nginx", "Install nginx")` gọn hơn struct literal.
func T(vi, en string) Text { return Text{Vi: vi, En: en} }

// In trả về bản theo ngôn ngữ lang ("vi" hoặc "en"). Ngôn ngữ lạ hoặc bản
// tiếng Anh còn trống thì trả bản tiếng Việt, vì tiếng Việt luôn được viết trước.
func (t Text) In(lang string) string {
	if lang == "en" && t.En != "" {
		return t.En
	}
	return t.Vi
}

// IsZero cho biết câu còn trống ở cả hai ngôn ngữ.
func (t Text) IsZero() bool { return t.Vi == "" && t.En == "" }
