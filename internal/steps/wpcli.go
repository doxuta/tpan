package steps

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// Bản WP-CLI mặc định và sha512 chính thức của file phar.
//
// Tra ngày 19/11/2026 bằng curl từ máy dev:
//   - bản mới nhất: https://github.com/wp-cli/wp-cli/releases/latest
//     chuyển tới .../releases/tag/v2.12.0
//   - sha512: https://github.com/wp-cli/wp-cli/releases/download/v2.12.0/wp-cli-2.12.0.phar.sha512
//     (đã tải wp-cli-2.12.0.phar và tính `shasum -a 512`, ra đúng số này).
//
// Lên bản mới: tra lại cả hai URL, đổi cả hai hằng cùng lúc.
const (
	DefaultWPCLIVersion = "2.12.0"
	DefaultWPCLISHA512  = "be928f6b8ca1e8dfb9d2f4b75a13aa4aee0896f8a9a0a1c45cd5d2c98605e6172e6d014dda2e27f88c98befc16c040cbb2bd1bfa121510ea5cdf5f6a30fe8832"
)

const (
	wpBin      = "/usr/local/bin/wp"
	wpCacheDir = "/var/cache/tpan" // thư mục do một Dir khác tạo trước bước này
)

var (
	semver    = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	sha512Hex = regexp.MustCompile(`^[0-9a-f]{128}$`)
)

// WPCLI tải WP-CLI (mục 6.1, bước 6) từ bản phát hành chính thức trên GitHub,
// kiểm sha512 bằng Go rồi mới đặt vào /usr/local/bin/wp.
//
// File tải về nằm lại ở /var/cache/tpan/wp-cli-<bản>.phar (mục 5). Chạy lại thì
// dùng file đó nếu sha512 vẫn khớp, không tải lại.
type WPCLI struct {
	Version string
	SHA512  string

	prevBin, prevCache saved
	applied            bool
}

func (s *WPCLI) validate() error {
	if !semver.MatchString(s.Version) {
		return fmt.Errorf("bản WP-CLI %q không đúng dạng số.số.số", s.Version)
	}
	if !sha512Hex.MatchString(strings.ToLower(s.SHA512)) {
		return errors.New("sha512 của WP-CLI phải là 128 ký tự hex")
	}
	return nil
}

func (s *WPCLI) cachePath() string { return wpCacheDir + "/wp-cli-" + s.Version + ".phar" }

func (s *WPCLI) download() system.Cmd {
	url := "https://github.com/wp-cli/wp-cli/releases/download/v" + s.Version + "/wp-cli-" + s.Version + ".phar"
	return system.Cmd{Name: "curl", Args: []string{"-fsSL", "-o", s.cachePath(), url}}
}

func (s *WPCLI) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "wpcli:" + wpBin,
		Title: i18n.T("Cài WP-CLI "+s.Version+" vào "+wpBin, "Install WP-CLI "+s.Version+" to "+wpBin),
		Why: i18n.T(
			"tpan cài, cấu hình và kiểm WordPress bằng WP-CLI. Thiếu nó thì không tạo được site. sha512 được kiểm trước khi đặt vào máy: file tải về bị sửa giữa đường thì không bao giờ được chạy bằng quyền root.",
			"tpan installs, configures and checks WordPress with WP-CLI. Without it no site can be created. The sha512 is checked before the file is put in place, so a file tampered with in transit is never run as root.",
		),
		Commands: []string{s.download().String()},
	}
}

func sumOK(data []byte, want string) bool {
	sum := sha512.Sum512(data)
	return hex.EncodeToString(sum[:]) == strings.ToLower(want)
}

func (s *WPCLI) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	fi, err := sys.Stat(wpBin)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", wpBin, err)
	}
	if !fi.Mode.IsRegular() || fi.Mode.Perm() != 0o755 {
		return false, nil
	}
	data, err := sys.ReadFile(wpBin)
	if err != nil {
		return false, fmt.Errorf("đọc %s: %w", wpBin, err)
	}
	return sumOK(data, s.SHA512), nil
}

func (s *WPCLI) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	cache := s.cachePath()
	prevCache, err := capture(sys, cache)
	if err != nil {
		return fmt.Errorf("cài WP-CLI: %w", err)
	}
	prevBin, err := capture(sys, wpBin)
	if err != nil {
		return fmt.Errorf("cài WP-CLI: %w", err)
	}

	// File đã tải lần trước mà sha512 vẫn khớp thì dùng lại.
	data := prevCache.data
	if !prevCache.exists || prevCache.link != "" || !sumOK(data, s.SHA512) {
		if res, err := sys.Run(ctx, s.download()); err != nil {
			err = withStdout(fmt.Errorf("tải WP-CLI %s: %w", s.Version, err), res)
			if rerr := prevCache.restore(sys); rerr != nil {
				return errors.Join(err, rerr)
			}
			return err
		}
		if data, err = sys.ReadFile(cache); err != nil {
			err = fmt.Errorf("đọc WP-CLI vừa tải %s: %w", cache, err)
			if rerr := prevCache.restore(sys); rerr != nil {
				return errors.Join(err, rerr)
			}
			return err
		}
		if !sumOK(data, s.SHA512) {
			// Không khớp: bỏ file vừa tải (trả về như trước khi tải), không cài.
			err := fmt.Errorf("WP-CLI %s tải về có sha512 không khớp bản chính thức; đã bỏ file %s", s.Version, cache)
			if rerr := prevCache.restore(sys); rerr != nil {
				return errors.Join(err, rerr)
			}
			return err
		}
	}
	if err := sys.WriteFileAtomic(wpBin, data, 0o755, system.Root); err != nil {
		err = fmt.Errorf("đặt WP-CLI vào %s: %w", wpBin, err)
		if rerr := prevCache.restore(sys); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	s.prevBin, s.prevCache, s.applied = prevBin, prevCache, true
	return nil
}

// Undo trả /usr/local/bin/wp về như cũ (xoá nếu trước đó chưa có) và bỏ file
// đã tải nếu bước này tải nó.
func (s *WPCLI) Undo(_ context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("WP-CLI: %w", errNotApplied)
	}
	return errors.Join(s.prevBin.restore(sys), s.prevCache.restore(sys))
}
