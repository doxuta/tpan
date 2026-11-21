package site

// Tên và đường dẫn của lồng theo site (M17, mục 3.2 của docs/thay-doi/M17-thiet-ke.md;
// cuối mốc gộp vào mục 6.15 của docs/KE-HOACH.md). Như Layout: mọi tên suy từ
// slug theo đúng một quy tắc, không chỗ nào khác tự ghép "tpan-cage-" + slug.
//
// Slug chỉ gồm [a-z0-9_] (validate.SlugOf), nên tên unit hợp lệ với systemd và
// không có dấu "-" sinh thêm tầng slice: tpan-<slug>.slice là con trực tiếp của
// tpan.slice (đo ở t6: slug có "-" làm slice lồng nhiều tầng).

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

const (
	// SystemdDir là nơi tpan ghi unit của lồng.
	SystemdDir = "/etc/systemd/system"
	// CageConfRoot chứa cấu hình riêng của từng lồng (php-fpm.conf, passwd, group).
	CageConfRoot = "/etc/tpan/cage"
	// CageRunRoot chứa thư mục socket của từng lồng (DirectoryMode của unit socket).
	CageRunRoot = "/run/tpan-cage"
	// CageCgroupRoot là cgroup của slice cha tpan.slice (cgroup v2).
	CageCgroupRoot = "/sys/fs/cgroup/tpan.slice"
	// CageRunMarker là phần giữa tên unit tạm của lệnh chạy thay cho site:
	// tpan-cage-<slug>-run-<16 hex>.service.
	CageRunMarker = "-run-"
)

// CageSlice là tên slice của site: tpan-<slug>.slice. Trần đặt ở đây.
func (l Layout) CageSlice() string { return "tpan-" + l.Slug.String() + ".slice" }

// CageSliceFile là file unit của slice.
func (l Layout) CageSliceFile() string { return SystemdDir + "/" + l.CageSlice() }

// cageUnitPrefix là tiền tố chung của mọi unit .service của lồng; drop-in hàng
// rào nằm ở thư mục <tiền tố>.service.d nên áp cho mọi unit mang tiền tố này.
func (l Layout) cageUnitPrefix() string { return "tpan-cage-" + l.Slug.String() + "-" }

// CageSocketUnit, CageServiceUnit: tpan-cage-<slug>-php.socket và .service.
func (l Layout) CageSocketUnit() string  { return l.cageUnitPrefix() + "php.socket" }
func (l Layout) CageServiceUnit() string { return l.cageUnitPrefix() + "php.service" }

// CageSocketFile, CageServiceFile: file unit tương ứng.
func (l Layout) CageSocketFile() string  { return SystemdDir + "/" + l.CageSocketUnit() }
func (l Layout) CageServiceFile() string { return SystemdDir + "/" + l.CageServiceUnit() }

// CageRunPrefix là tiền tố tên unit tạm của lệnh chạy thay cho site
// (tpan-cage-<slug>-run-), kèm 16 ký tự hex ngẫu nhiên và ".service".
func (l Layout) CageRunPrefix() string {
	return l.cageUnitPrefix() + strings.TrimPrefix(CageRunMarker, "-")
}

// CageFenceDir là thư mục drop-in theo tiền tố: systemd áp file trong đó cho
// mọi unit tên bắt đầu bằng tpan-cage-<slug>- (dịch vụ PHP và lệnh tạm).
func (l Layout) CageFenceDir() string { return SystemdDir + "/" + l.cageUnitPrefix() + ".service.d" }

// CageFence là file hàng rào.
func (l Layout) CageFence() string { return l.CageFenceDir() + "/fence.conf" }

// CageConfDir là /etc/tpan/cage/<slug>.
func (l Layout) CageConfDir() string { return CageConfRoot + "/" + l.Slug.String() }

// CageFPMConf, CagePasswd, CageGroup là ba file trong CageConfDir.
func (l Layout) CageFPMConf() string { return l.CageConfDir() + "/php-fpm.conf" }
func (l Layout) CagePasswd() string  { return l.CageConfDir() + "/passwd" }
func (l Layout) CageGroup() string   { return l.CageConfDir() + "/group" }

// CageRunDir là thư mục socket: /run/tpan-cage/<16 hex đầu của sha256(domain)>.
// Không mang slug: /proc/self/net/unix liệt kê đường dẫn mọi Unix socket của
// máy cho mọi tiến trình (mạng dùng chung, t14 P5), nên tên này không được để
// lộ tên miền. 16 hex là 64 bit: không trùng giữa các site trên một máy.
func (l Layout) CageRunDir() string {
	if l.Domain.IsZero() {
		return ""
	}
	sum := sha256.Sum256([]byte(l.Domain.String()))
	return CageRunRoot + "/" + hex.EncodeToString(sum[:8])
}

// CageSocket là socket PHP của lồng: <CageRunDir>/php.sock, do systemd tạo
// (www-data:www-data 0660) và giữ qua mọi lần dịch vụ khởi động lại.
func (l Layout) CageSocket() string { return l.CageRunDir() + "/php.sock" }

// CageCgroup là thư mục cgroup v2 của slice. Chỉ có từ lúc dịch vụ hay lệnh
// đầu tiên của lồng chạy kể từ khi máy lên (t7b).
func (l Layout) CageCgroup() string { return CageCgroupRoot + "/" + l.CageSlice() }

// CageManagedFiles là năm file của lồng vào sổ managed_files (phát hiện lệch).
// passwd và group không có ở đây: nội dung chỉ biết khi user của site đã có
// (uid, gid), nên Step không khai được trong Info().Files; cage.fence kiểm chúng.
func (l Layout) CageManagedFiles() []string {
	return []string{l.CageSliceFile(), l.CageSocketFile(), l.CageServiceFile(), l.CageFence(), l.CageFPMConf()}
}

// CageFiles là bảy file tpan ghi cho một lồng (năm file trên cộng passwd, group).
func (l Layout) CageFiles() []string {
	return append(l.CageManagedFiles(), l.CagePasswd(), l.CageGroup())
}

// OwnsCagePath cho biết p là một trong các file hay thư mục của lồng của site
// này. So bằng dấu bằng: slug này không bao giờ nhận đường dẫn của slug khác
// dài hơn (tpan-cage-<slug>- có dấu "-" ở cuối, thư mục có "/" ở cuối).
func (l Layout) OwnsCagePath(p string) bool {
	if l.Slug.IsZero() {
		return false
	}
	switch p {
	case l.CageFenceDir(), l.CageConfDir(), l.CageRunDir(), l.CageSocket():
		return true
	}
	for _, f := range l.CageFiles() {
		if p == f {
			return true
		}
	}
	return false
}

// cageUserRE là dạng tên user Linux của site: tp_<slug>.
var cageUserRE = regexp.MustCompile(`^tp_([a-z0-9_]{1,40})$`)

// CageSlugOfUser đổi tên user Linux của site (tp_<slug>) về slug. ok=false với
// mọi tên khác dạng đó (root, www-data, tên có ký tự lạ): lớp bọc lệnh của lồng
// không bao giờ ghép tên unit từ một chuỗi chưa qua dạng này.
func CageSlugOfUser(user string) (slug string, ok bool) {
	m := cageUserRE.FindStringSubmatch(user)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// CageFenceOfSlug là file hàng rào của một slug (dùng ở lớp bọc lệnh, nơi chỉ
// biết tên user chứ không biết domain). slug phải đã qua CageSlugOfUser.
func CageFenceOfSlug(slug string) string {
	return SystemdDir + "/tpan-cage-" + slug + "-.service.d/fence.conf"
}

// CageRunPrefixOfSlug là CageRunPrefix của một slug (lớp bọc lệnh).
func CageRunPrefixOfSlug(slug string) string { return "tpan-cage-" + slug + CageRunMarker }

// ProbeDomainRE là dạng tên miền của hai site thử do `tpan isolation probe`
// tạo (mục 3.9): tpan-probe-a-<12 hex>.test và tpan-probe-b-<12 hex>.test.
// Tạo site bằng tên khớp mẫu này bị từ chối (domain_reserved).
var ProbeDomainRE = regexp.MustCompile(`^tpan-probe-[ab]-[0-9a-f]{12}\.test$`)

// IsProbeDomain cho biết d là tên dành riêng cho phép thử tấn công.
func IsProbeDomain(d string) bool { return ProbeDomainRE.MatchString(d) }
