package core

// M36 (v0.2.17): người dùng tự đặt mật khẩu user DB thay vì nhận một mật khẩu
// ngẫu nhiên. Áp cho DB rời (lúc tạo và lúc đổi mật khẩu) và DB của site PHP
// trống (lúc đổi mật khẩu): ở hai chỗ này chính người dùng phải chép mật khẩu
// vào cấu hình mã nguồn của họ.
//
// DB của site WordPress không nhận mật khẩu tự đặt: tpan ghi mật khẩu vào
// wp-config.php, không ai phải gõ nó; và các bước sau này của tpan (đổi mật
// khẩu, tạo lại user DB đã mất, khôi phục) chỉ tin mật khẩu trong wp-config.php
// khi nó đúng dạng tpan sinh (steps.dbTpanPasswordRe), để không đụng vào một
// mật khẩu người khác đặt và có thể còn dùng ở chỗ khác.

import (
	"regexp"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

const (
	// CodeDBPasswordInvalid: mật khẩu tự đặt không đúng luật (ChosenDBPasswordRule).
	CodeDBPasswordInvalid = "db_password_invalid"
	// CodeDBPasswordWordPress: tự đặt mật khẩu cho DB của site WordPress.
	CodeDBPasswordWordPress = "db_password_wordpress"
)

// chosenDBPasswordRe phải khớp steps.dbChosenPasswordRe: mật khẩu nằm trong
// '...' của câu SQL gửi qua stdin của mariadb, nên không có nháy đơn, nháy
// kép, backtick, gạch chéo ngược, khoảng trắng hay xuống dòng. $ & ; | < > (
// ) cũng không có, để mật khẩu chép vào file .env hay lệnh shell của khách
// không bị hiểu sai.
var chosenDBPasswordRe = regexp.MustCompile(`^[A-Za-z0-9!#%*+,\-./:=?@^_~]{12,64}$`)

// ChosenDBPasswordRule là luật mật khẩu tự đặt, viết cho người dùng đọc.
var ChosenDBPasswordRule = i18n.T(
	"12 đến 64 ký tự, gồm chữ cái không dấu, chữ số và các dấu ! # % * + , - . / : = ? @ ^ _ ~ (không có khoảng trắng, dấu nháy, \\, $, &, ;); có ít nhất một chữ cái và một chữ số; không chứa tên DB",
	"12 to 64 characters: unaccented letters, digits and ! # % * + , - . / : = ? @ ^ _ ~ (no spaces, quotes, \\, $, &, ;); at least one letter and one digit; must not contain the database name")

// checkChosenDBPassword kiểm mật khẩu người dùng đặt cho user DB name. Không
// bao giờ đưa mật khẩu vào lời từ chối.
func checkChosenDBPassword(pw, name string) error {
	bad := func(why i18n.Text) error {
		return refuse(CodeDBPasswordInvalid, i18n.T(
			"mật khẩu DB không dùng được: "+why.Vi+". Luật: "+ChosenDBPasswordRule.Vi,
			"the database password cannot be used: "+why.En+". Rule: "+ChosenDBPasswordRule.En))
	}
	switch {
	case len(pw) < 12:
		return bad(i18n.T("ngắn hơn 12 ký tự", "shorter than 12 characters"))
	case len(pw) > 64:
		return bad(i18n.T("dài hơn 64 ký tự", "longer than 64 characters"))
	case !chosenDBPasswordRe.MatchString(pw):
		return bad(i18n.T("có ký tự ngoài các ký tự được nhận", "it contains a character that is not allowed"))
	case !strings.ContainsAny(pw, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz") || !strings.ContainsAny(pw, "0123456789"):
		return bad(i18n.T("cần ít nhất một chữ cái và một chữ số", "it needs at least one letter and one digit"))
	case name != "" && strings.Contains(strings.ToLower(pw), strings.ToLower(name)):
		return bad(i18n.T("chứa tên DB "+name, "it contains the database name "+name))
	}
	return nil
}

// pickDBPassword: chosen rỗng thì sinh mật khẩu ngẫu nhiên như trước (picked
// false); có thì kiểm rồi dùng đúng nó (picked true).
func pickDBPassword(chosen, name string) (pw system.Secret, picked bool, err error) {
	if chosen == "" {
		pw, err = sitePassword()
		return pw, false, err
	}
	if err := checkChosenDBPassword(chosen, name); err != nil {
		return system.Secret{}, false, err
	}
	return system.NewSecret(chosen), true, nil
}

// refuseChosenForWordPress từ chối mật khẩu tự đặt cho DB của site WordPress.
func refuseChosenForWordPress(st site.Site) error {
	d := st.Domain.String()
	return refuse(CodeDBPasswordWordPress, i18n.T(
		"site "+d+" là site WordPress: tpan tự ghi mật khẩu DB vào wp-config.php nên không nhận mật khẩu tự đặt. Bỏ ô mật khẩu để tpan sinh một mật khẩu mới",
		d+" is a WordPress site: tpan writes the database password into wp-config.php itself, so it does not take a chosen password. Leave the password empty and tpan generates a new one"))
}

// secretUnlessChosen trả mật khẩu để hiện một lần, trừ khi người dùng tự đặt
// (họ đã biết nó; không gửi lại qua mạng, không giữ trong job).
func secretUnlessChosen(pw system.Secret, picked bool) system.Secret {
	if picked {
		return system.Secret{}
	}
	return pw
}
