package core

// Tên miền dùng chung (admin khai): tên miền mà chủ máy chia tên con cho nhiều
// khách, ví dụ máy chỉ có một tên miền dạng *.lab.example.com cho mọi khách. Không
// khai thì luật tên miền gốc (M28 P4) coi mọi tên con của nó là của một chủ:
// admin có một site ở đó là không khách nào tự tạo được site.
//
// Lưu ở settings (khoá SettingSharedSuffixes, các tên cách nhau dấu phẩy). Mỗi
// tiến trình nạp vào validate.SetSharedSuffixes: CLI lúc mở DB, server lúc
// khởi động và nạp lại định kỳ (server.refreshShared).

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/validate"
)

// SettingSharedSuffixes là khoá settings của danh sách tên miền dùng chung.
const SettingSharedSuffixes = "shared_suffixes"

// SharedSuffixes đọc danh sách đã khai, theo thứ tự chữ cái.
func SharedSuffixes(ctx context.Context, st Settings) ([]validate.Domain, error) {
	raw, ok, err := st.GetSetting(ctx, SettingSharedSuffixes)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách tên miền dùng chung: %w", err)
	}
	var out []validate.Domain
	if !ok {
		return out, nil
	}
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		d, err := validate.ParseDomain(s)
		if err != nil {
			continue // dòng bị sửa tay: bỏ tên hỏng, không làm chết cả panel
		}
		out = append(out, d)
	}
	return out, nil
}

// LoadSharedSuffixes nạp danh sách trong settings vào tiến trình này.
func LoadSharedSuffixes(ctx context.Context, st Settings) error {
	list, err := SharedSuffixes(ctx, st)
	if err != nil {
		return err
	}
	validate.SetSharedSuffixes(list)
	return nil
}

// SetSharedSuffix thêm (on) hay bỏ (!on) một tên miền dùng chung rồi nạp lại.
// Trả danh sách sau khi đổi. Thêm tên đã có hay bỏ tên không có thì từ chối.
func SetSharedSuffix(ctx context.Context, st Settings, d validate.Domain, on bool) ([]validate.Domain, error) {
	list, err := SharedSuffixes(ctx, st)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(list, func(x validate.Domain) bool { return x == d })
	switch {
	case on && i >= 0:
		return nil, refuse("shared_domain_exists", i18n.T(
			d.String()+" đã là tên miền dùng chung", d.String()+" is already a shared domain"))
	case !on && i < 0:
		return nil, refuse("shared_domain_not_found", i18n.T(
			d.String()+" không nằm trong danh sách tên miền dùng chung", d.String()+" is not in the list of shared domains"))
	case on:
		list = append(list, d)
	default:
		list = slices.Delete(list, i, i+1)
	}
	names := make([]string, len(list))
	for i, x := range list {
		names[i] = x.String()
	}
	slices.Sort(names)
	if err := st.SetSetting(ctx, SettingSharedSuffixes, strings.Join(names, ",")); err != nil {
		return nil, fmt.Errorf("ghi danh sách tên miền dùng chung: %w", err)
	}
	return list, LoadSharedSuffixes(ctx, st)
}
