package cloudinit

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/doxuta/tpan/internal/i18n"
)

var (
	baseRe    = regexp.MustCompile(`^https?://[A-Za-z0-9._~:/-]+$`)
	versionRe = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)
	sumRe     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// Dòng của sha256sum: "<hex>  <tên>" (chế độ chữ) hoặc "<hex> *<tên>" (nhị phân).
	sumLineRe = regexp.MustCompile(`^([0-9a-f]{64}) [ *](.+)$`)
)

// header đứng trước output của bộ ghi YAML. Là hằng, không chứa giá trị nào
// của người dùng; cloud-init chỉ nhận file khi dòng đầu đúng "#cloud-config".
const header = "#cloud-config\n# Do \"tpan cloud-init render\" sinh. Không chứa mật khẩu, khoá hay token dạng rõ.\n"

// scriptTmpl là nguyên văn khuôn ở mục 3.9 của thiết kế. Bốn chỗ @...@ được
// thay bằng giá trị đã qua luật ký tự (không nháy, không khoảng trắng, không $).
const scriptTmpl = `#!/bin/sh
# Do "tpan cloud-init render" sinh cho tpan @VERSION@. Tải đúng file chạy đã ghim,
# kiểm sha256, rồi giao phần còn lại cho chính tpan.
set -eu
bin=/usr/local/bin/tpan
# Lần khởi động đầu đã chạy (có file trạng thái): không bao giờ đụng lại file
# chạy, vì máy có thể đã self-update lên bản mới hơn bản đã ghim.
if [ -e /var/lib/tpan/first-boot.json ] && [ -x "$bin" ]; then
  exec "$bin" cloud-init run
fi
arch="$(dpkg --print-architecture)"
case "$arch" in
  amd64) sum='@SUMAMD64@' ;;
  arm64) sum='@SUMARM64@' ;;
  *) echo "tpan: kiến trúc $arch không được hỗ trợ" >&2; exit 1 ;;
esac
if ! { [ -x "$bin" ] && echo "$sum  $bin" | sha256sum -c --status -; }; then
  command -v curl >/dev/null 2>&1 || { apt-get update -q && apt-get install -y -q curl; }
  tmp="$bin.first-boot"
  curl -fsSL --retry 5 --retry-delay 3 --connect-timeout 20 -o "$tmp" '@BASE@/tpan-linux-'"$arch"
  echo "$sum  $tmp" | sha256sum -c --status - || { echo "tpan: sha256 của file tải về không khớp bản đã ghim; không cài" >&2; rm -f "$tmp"; exit 1; }
  chown root:root "$tmp" && chmod 0755 "$tmp" && mv -f "$tmp" "$bin"
fi
exec "$bin" cloud-init run
`

func invalid(vi, en string) error { return &InvalidError{Msg: i18n.T(vi, en)} }

// CheckBase kiểm nơi tải file chạy. Luật ký tự chặt (không nháy, khoảng trắng,
// $, ;) vì giá trị này đi thẳng vào script shell, trong nháy đơn.
func CheckBase(s string) error {
	if !baseRe.MatchString(s) || strings.Contains(s, "..") || strings.HasSuffix(s, "/") {
		return invalid(
			"--base phải là http:// hoặc https://, chỉ gồm chữ, số và . _ ~ : / -, không có \"..\", không kết thúc bằng /",
			"--base must be http:// or https://, only letters, digits and . _ ~ : / -, no \"..\", no trailing /")
	}
	if u, err := url.Parse(s); err != nil || u.Hostname() == "" {
		return invalid("--base thiếu tên máy chủ", "--base has no host")
	}
	return nil
}

// VersionOK cho biết tên bản có dùng được trong dòng chú thích của script không.
func VersionOK(v string) bool { return versionRe.MatchString(v) }

// Script trả script khởi động đầu. Tự kiểm lại bốn giá trị dù Render đã kiểm,
// vì đây là hàm công khai nhận Params thô: Version sai thành "unknown"; Base
// hay sha256 sai thì để rỗng, script chạy sẽ hỏng ở bước kiểm sha256 hoặc
// curl chứ không bao giờ mang một dấu nháy hở vào shell.
func Script(p Params) string {
	v, base, a, b := p.Version, p.Base, p.SumAMD64, p.SumARM64
	if !VersionOK(v) {
		v = "unknown"
	}
	if CheckBase(base) != nil {
		base = ""
	}
	if !sumRe.MatchString(a) {
		a = ""
	}
	if !sumRe.MatchString(b) {
		b = ""
	}
	return strings.NewReplacer("@VERSION@", v, "@BASE@", base, "@SUMAMD64@", a, "@SUMARM64@", b).Replace(scriptTmpl)
}

// Các kiểu dưới đây là đúng phần cloud-init đọc. Thứ tự trường là thứ tự khoá
// trong file ra.
type userData struct {
	WriteFiles []writeFile  `yaml:"write_files"`
	Runcmd     []*yaml.Node `yaml:"runcmd"`
}

type writeFile struct {
	Path        string     `yaml:"path"`
	Permissions string     `yaml:"permissions"`
	Encoding    string     `yaml:"encoding,omitempty"`
	Content     *yaml.Node `yaml:"content"`
}

func str(v string, style yaml.Style) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v, Style: style}
}

// Render dựng user-data. Mọi giá trị của người dùng (config, sites.yaml) chỉ
// vào nội dung file dạng base64; runcmd và các đường dẫn là hằng (B12).
func Render(p Params) ([]byte, error) {
	if err := CheckBase(p.Base); err != nil {
		return nil, err
	}
	if !sumRe.MatchString(p.SumAMD64) || !sumRe.MatchString(p.SumARM64) {
		return nil, invalid("sha256 của tpan-linux-amd64 và tpan-linux-arm64 phải là 64 ký tự hex thường",
			"sha256 of tpan-linux-amd64 and tpan-linux-arm64 must be 64 lowercase hex characters")
	}
	if err := checkConfig(p.Config); err != nil {
		return nil, err
	}
	// config.json trỏ tới sites.yaml thì file đó phải có, và ngược lại: lệch
	// nhau là lần khởi động đầu chắc chắn hỏng hoặc bỏ sót site.
	if (p.SitesYAML != nil) != (p.Config.SitesFile == SitesPath) {
		return nil, invalid("sites_file trong config phải là "+SitesPath+" khi và chỉ khi có file site",
			"sites_file in config must be "+SitesPath+" if and only if a sites file is given")
	}
	cfg, err := json.Marshal(p.Config)
	if err != nil {
		return nil, err
	}

	files := []writeFile{{
		Path: ConfigPath, Permissions: "0600", Encoding: "b64",
		Content: str(base64.StdEncoding.EncodeToString(cfg), 0),
	}}
	if p.SitesYAML != nil {
		// gzip trước khi base64: sites.yaml là phần lớn nhất của user-data, mà
		// trần của AWS chỉ 16 KB. ModTime để 0 cho output tất định.
		var gz bytes.Buffer
		zw := gzip.NewWriter(&gz)
		if _, err := zw.Write(p.SitesYAML); err != nil {
			return nil, err
		}
		if err := zw.Close(); err != nil {
			return nil, err
		}
		files = append(files, writeFile{
			Path: SitesPath, Permissions: "0600", Encoding: "gz+b64",
			Content: str(base64.StdEncoding.EncodeToString(gz.Bytes()), 0),
		})
	}
	// Khối literal (|): dạng PyYAML của cloud-init đọc ra đúng từng byte mà
	// người mở user-data vẫn đọc được script.
	files = append(files, writeFile{Path: ScriptPath, Permissions: "0700", Content: str(Script(p), yaml.LiteralStyle)})

	// Đúng một lệnh, là script: mã thoát của nó là mã thoát của cả lượt
	// cloud-init (lệnh lỗi ở giữa runcmd không làm cloud-init báo lỗi, mục 2.5).
	cmd := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle, Content: []*yaml.Node{str(ScriptPath, 0)}}

	var out bytes.Buffer
	out.WriteString(header)
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(userData{WriteFiles: files, Runcmd: []*yaml.Node{cmd}}); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}

	limit := p.MaxBytes
	if limit == 0 {
		limit = DefaultMaxBytes
	}
	if out.Len() > limit {
		return nil, &TooLargeError{Size: out.Len(), Max: limit}
	}
	return out.Bytes(), nil
}

// ParseSums lấy sha256 của hai file chạy Linux từ SHA256SUMS. Dòng của file
// khác hoặc sai khuôn bị bỏ qua: file của bản phát hành có cả tên khác.
func ParseSums(text []byte) (amd64, arm64 string, err error) {
	got := map[string]string{"tpan-linux-amd64": "", "tpan-linux-arm64": ""}
	sc := bufio.NewScanner(bytes.NewReader(text))
	for sc.Scan() {
		m := sumLineRe.FindStringSubmatch(strings.TrimSuffix(sc.Text(), "\r"))
		if m == nil {
			continue
		}
		old, want := got[m[2]]
		if !want {
			continue
		}
		if old != "" && old != m[1] {
			return "", "", invalid("SHA256SUMS có hai dòng khác nhau cho "+m[2],
				"SHA256SUMS has two different lines for "+m[2])
		}
		got[m[2]] = m[1]
	}
	if err := sc.Err(); err != nil {
		return "", "", invalid("không đọc được SHA256SUMS", "cannot read SHA256SUMS")
	}
	for _, name := range []string{"tpan-linux-amd64", "tpan-linux-arm64"} {
		if got[name] == "" {
			return "", "", invalid("SHA256SUMS thiếu dòng của "+name, "SHA256SUMS has no line for "+name)
		}
	}
	return got["tpan-linux-amd64"], got["tpan-linux-arm64"], nil
}

// ParseConfig đọc config.json ở lần khởi động đầu. Trường lạ và dữ liệu thừa
// sau đối tượng JSON đều là lỗi: file này điều khiển một tiến trình root.
func ParseConfig(b []byte) (Config, error) {
	var c Config
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, invalid("config.json sai khuôn: "+err.Error(), "malformed config.json: "+err.Error())
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Config{}, invalid("config.json có dữ liệu thừa sau đối tượng", "config.json has trailing data")
	}
	if err := checkConfig(c); err != nil {
		return Config{}, err
	}
	return c, nil
}

// checkConfig dùng chung cho Render và ParseConfig, để Render không bao giờ
// sinh ra config mà lần khởi động đầu từ chối.
func checkConfig(c Config) error {
	switch {
	case c.Version != 1:
		return invalid("config.json: version phải là 1", "config.json: version must be 1")
	case c.Web != "nginx" && c.Web != "apache":
		return invalid("config.json: web phải là nginx hoặc apache", "config.json: web must be nginx or apache")
	case c.AdminHash != "" && !strings.HasPrefix(c.AdminHash, "$argon2id$"):
		return invalid("config.json: admin_hash phải là bản băm argon2id", "config.json: admin_hash must be an argon2id hash")
	case c.SitesFile != "" && c.SitesFile != SitesPath:
		return invalid("config.json: sites_file phải rỗng hoặc là "+SitesPath, "config.json: sites_file must be empty or "+SitesPath)
	}
	return nil
}
