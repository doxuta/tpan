# M28, đợt 1: bản thiết kế vá T1 và cả họ "root đụng thứ site kiểm soát"

Ngày 21/11/2026. Phiên chính của mốc M28 viết (đợt 0), trước khi viết mã. Nguồn: `agent-dev/TU-CHAY-M28.md`, báo cáo của phiên rà soát (`ra-soat/muc5/BAO-CAO.md`, `DANH-SACH.md`), mã ở commit `4269cc1`, và bản thử trên máy ảo `tpan-m28` (`docs/bang-chung/M28-nghien-cuu/`).

Từ dùng trong file này:

- **Vùng site**: mọi đường dẫn nằm hẳn bên dưới `/var/www/<tên>/`. Thư mục `/var/www/<tên>` do user Linux của site làm chủ (`2750`), nên user đó tạo, đổi tên, xoá, tráo được mọi thứ bên trong, kể cả biến một file hay một thư mục thành symlink, bất kỳ lúc nào.
- **Neo**: chính thư mục `/var/www/<tên>`. Tên này nằm trong `/var/www` (của root), user của site không tráo được nó.
- **K1**: mã chạy dưới user Linux của site A (PHP của một site bị chiếm, hoặc của một khách hosting xấu). **K2**: tài khoản panel vai `user` là chủ site A. T1 là K1 và K2 phối hợp; K1 một mình thì chờ admin, bộ hẹn giờ hay AI thao tác lên site.

## 1. Vấn đề và phạm vi

**T1.** Trên stack Apache, bước `SiteHtaccess` khai `public/.htaccess` trong `StepInfo.Files`. `engine.Preview` đọc nội dung cũ của mọi file trong `Files` bằng `os.ReadFile` (chạy bằng root, đi theo symlink, không trần cỡ, mở cả FIFO và thiết bị). API trả bản xem trước cho người gọi, không lọc theo vai. K1 đặt symlink, K2 gọi `POST /api/v1/sites?dry_run=1`, nội dung file đích về trong `steps[].info.files[].old`.

**Gốc không nằm ở một dòng.** Mã v0.2.0 chặn tráo symlink theo từng chỗ, mỗi chỗ một kiểu (lstat rồi đọc, ràng nội dung, chạy bằng user của site). Kiểu "lstat rồi làm" thua khi thư mục cha bị tráo, và thua đua khi chính file bị tráo giữa hai lời gọi (đo ở mục 2). Chỗ nào quên thì lọt hẳn (T1). Vì vậy bản vá đặt luật ở một chỗ duy nhất mà mọi thao tác file của panel vốn đã đi qua: `system.System`.

**Làm trong đợt 1:**

1. Gói `internal/sitefs` ("cửa"): cách duy nhất panel chạm vào vùng site. Không đi theo symlink ở bất kỳ thành phần nào, không ra khỏi neo, chỉ đọc file thường của chính user site, có trần cỡ, không bao giờ treo.
2. `LocalSystem` và `FakeSystem` tự chuyển mọi thao tác file có đường dẫn trong vùng site qua cửa. Mã gọi `sys.ReadFile`, `sys.Chmod`... không phải sửa, và không quên được.
3. Lệnh chạy bằng root không được nhận đường dẫn trong vùng site, trừ một danh sách ngắn có lý do và bằng chứng. `find ... -exec chmod` chạy bằng root trong `WPFiles` (thua đua, mục 2) được thay bằng mã đi qua cửa.
4. `engine.Preview` không đọc qua symlink; lỗi đọc trong vùng site thành lời từ chối đọc được.
5. Lọc theo vai ở tầng API, độc lập với lớp đọc.
6. Test quét mã, test bất biến, e2e bước 31 trên cả hai stack, e2e nâng từ v0.2.0.

**Không làm trong đợt 1** (ghi ở mục 5, phần "còn hở"): các phát hiện mục 6 của rà soát (khoá đăng nhập, argon2, chữ ký bản cập nhật) là đợt 2; `tar` chạy bằng root lúc backup trên máy đã tắt `fs.protected_hardlinks`; tiến trình của user site còn sống giữ fd vào cây đã chuyển sang thùng rác; một site cố ý làm hỏng Plan gộp nhiều site.

**Hành vi phải giữ:** `.htaccess` là file thường thì bản xem trước hiện diff đúng như v0.2.0; ba chế độ `.htaccess` của M14 không đổi; site đang chạy không đổi gì khi nâng từ v0.2.0; không sửa gì trong `web/`.

## 2. Nghiên cứu

Tra ngày 21/11/2026. Máy ảo `tpan-m28`: Ubuntu 24.04, kernel `6.8.0-147-generic`, arm64; Go `1.27.4`; `golang.org/x/sys v0.50.0`; GNU tar 1.35, coreutils 9.4, findutils 4.9.0. `fs.protected_symlinks=1`, `fs.protected_hardlinks=1` (mặc định của Ubuntu). Script và output gốc: `docs/bang-chung/M28-nghien-cuu/` (`chay.sh` chạy từng script qua `make m28-nghien-cuu T=<tên>`).

**N1: tái hiện T1 trên bản chưa vá** (`n1-t1.sh`, kết quả `n1-t1-truoc-ban-va.ket-qua.txt`; stack Apache, `khach1` vai `user`, site `e.example.com`):

| `.htaccess` của site là | Kết quả trên bản chưa vá |
|---|---|
| file thường | `old` đúng bằng file (696 byte), bản xem trước 0,40 giây |
| symlink tới `wp-config.php` của site khác | **lộ** 3480 byte |
| symlink tới `/etc/shadow` | **lộ** 980 byte |
| symlink tới `/var/lib/tpan/tpan.db` | **lộ** 884736 byte |
| hardlink tới `/etc/shadow` | kernel từ chối `ln`; tắt `fs.protected_hardlinks` thì `ln` được và **lộ** 980 byte |
| file thường 2 GiB (thưa), đúng chủ, đúng quyền | tpan chết sau 3 giây (không cần symlink) |
| FIFO | request treo quá 20 giây |
| symlink tới `/dev/zero` | kernel giết tpan vì hết RAM (3,5 GiB) |

**N2: năm cách root đọc một đường dẫn trong thư mục của user khác** (`_thu-cua/main.go`, `n2-so-cach-mo.sh`). `readfile` là `os.ReadFile` (cách của `engine.Preview`); `lstat` là `os.Lstat` rồi đọc (cách của các Step v0.2.0); `osroot` là `os.Root` của Go; `openat2` là `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS`; `walk` là `openat(O_NOFOLLOW)` từng thành phần. Ba cách cuối mở `O_NONBLOCK` rồi `fstat` trên chính fd.

| Thứ ở `public/.htaccess` | readfile | lstat | osroot | openat2 | walk |
|---|---|---|---|---|---|
| symlink ra ngoài site | lộ | từ chối | từ chối | từ chối | từ chối |
| symlink tới file khác **trong** site | đọc | từ chối | **đọc (đi theo)** | từ chối | từ chối |
| thư mục cha `public` là symlink | lộ | **lộ** | từ chối | từ chối | từ chối |
| FIFO | treo | từ chối | từ chối | từ chối | từ chối |
| file thiết bị, `/dev/zero` | đọc không dừng | từ chối | từ chối | từ chối | từ chối |
| file 2 GiB | đọc hết | **đọc hết** | từ chối | từ chối | từ chối |
| hardlink tới file bí mật | lộ | lộ | lộ | lộ | lộ |
| đua tráo file ⇄ symlink, 20000 lần đọc | lộ 9757 | **lộ 95** | 0 | 0 | 0 |
| đua tráo `public` ⇄ symlink, 20000 lần | lộ 9425 | **lộ 9966** | 0 | 0 | 0 |
| một lần đọc file thường | 2,3 µs | 2,8 µs | 3,7 µs | 2,7 µs | 3,3 µs |

Rút ra:

- Kiểu "lstat rồi đọc" của v0.2.0 không đủ: thua khi cha bị tráo, thua đua, không có trần.
- `os.Root` chặn thoát ra ngoài nhưng **đi theo symlink trong site**; tài liệu của Go 1.27 (`go doc os.Root`) ghi `Chmod`, `Chown` của nó có đua và nó không chặn file thiết bị. Không đạt bất biến "không đi theo symlink ở bất kỳ thành phần nào".
- `openat2` và `walk` cho cùng kết quả. Chọn **`walk`**: chạy được cả trên macOS (unit test của repo chạy trên Mac) và mọi kernel, một đường mã duy nhất cho máy dev và máy thật; chậm hơn `openat2` 0,6 µs mỗi lần, không đáng kể. `openat2` chỉ có trên Linux từ 5.6, dùng nó thì mã chạy thật và mã được test trên Mac là hai nhánh khác nhau.
- Hardlink là file thường với mọi cách mở: phải kiểm **chủ của file** bằng `fstat` trên fd. Mặc định Ubuntu kernel đã chặn (`fs.protected_hardlinks=1`), đây là lớp cho máy bị cấu hình lỏng.
- `O_DIRECTORY|O_NOFOLLOW` gặp symlink trả `ENOTDIR` (không phải `ELOOP`) trên kernel 6.8: cửa phải tự phân loại để báo đúng "là symlink".

**N3: lệnh tpan chạy bằng root trên cây của site** (`n3-lenh-root.sh`):

| Lệnh | Kết quả |
|---|---|
| `tar --create --one-file-system --directory=<site> public` (backup) | lưu symlink là symlink, file backup không chứa nội dung đích; `strace`: mọi `openat` trong cây có `O_NOFOLLOW` và đi theo fd. An toàn. |
| `rm -r --one-file-system` (xoá hẳn) | `openat(O_NOFOLLOW)` và `unlinkat` theo fd; file đích của symlink còn nguyên. An toàn. |
| `du -sb --one-file-system` | không cộng đích của symlink. An toàn. |
| `find <public> -type d -exec chmod 2750 {} +` bằng root (`WPFiles`, T8 của danh sách) | **thua đua sau 5 vòng**: user của site tráo một thư mục thành symlink giữa lúc `find` liệt kê và lúc `chmod` chạy; root đổi quyền thư mục nạn nhân thành `2750`. Bước này chỉ chạy khi `public` rỗng: lúc tạo site lần đầu (chưa có tiến trình nào chạy bằng UID mới, trừ khi UID được dùng lại, F1), hoặc khi user của site xoá sạch `public` đúng lúc giữa lần kiểm `drift()` và bước `wp.files` (S8). |

Nguồn đã đọc: `man 2 open`, `man 2 openat2` (Ubuntu 24.04), `go doc os.Root` và `go doc golang.org/x/sys/unix` của đúng bản đang cài (kiểm `Openat`, `Fstatat`, `Unlinkat`, `Renameat`, `Mkdirat`, `Symlinkat`, `Readlinkat`, `Fchmod`, `Fchown`, `Fstatfs` có trên cả `linux` và `darwin`; `Openat2` chỉ có trên `linux`).

## 3. Thiết kế

### 3.1 Vùng site

`sitefs.Root = "/var/www"`. `sitefs.Split(root, p)`: `p` được `path.Clean`; nếu có dạng `<root>/<tên>/<phần còn lại>` với phần còn lại khác rỗng thì trả `(neo = <root>/<tên>, rel, true)`. Chính neo (`/var/www/<tên>`) và mọi đường dẫn khác **không** thuộc vùng: chúng nằm trong thư mục của root. `sitefs.InZone(p)` là `Split(Root, p)`.

Vì sao chỉ `/var/www`: đó là nơi duy nhất user của site ghi được thư mục. Thư mục log (`root:<nhóm site> 0750`) user site không tạo hay đổi tên được gì; thùng rác và chỗ giải nén khôi phục là `0700` của root.

**Điều kiện của cả thiết kế (S15):** `Root` thuộc chính tiến trình đang chạy (root trên máy chủ) và nhóm, người khác không ghi được. Nếu ai đó làm theo hướng dẫn trên mạng `chown -R www-data /var/www` thì một tiến trình `www-data` đổi tên được neo của site này thành tên của site kia, và mọi luật bên dưới mất nghĩa. Vì vậy cửa kiểm điều kiện này ở mỗi lời gọi (luật C1); sai thì từ chối `ErrRootUnsafe`, lời từ chối nói cách sửa (`chown root:root /var/www && chmod 755 /var/www`).

### 3.2 Gói `internal/sitefs` (cửa)

Gói lá: chỉ import thư viện chuẩn và `golang.org/x/sys/unix` (thành phụ thuộc trực tiếp; lý do ghi vào mục 3 của KE-HOACH: cần các lời gọi `*at` có trên cả Linux lẫn macOS, gói `syscall` chuẩn không đủ). Không import gói nào của tpan.

```go
const Root = "/var/www"
const MaxFile = 1 << 20          // trần cỡ một file được đọc: 1 MiB
const MaxDirEntries = 100_000    // trần số mục ReadDir trả
const MaxDepth = 64              // trần số thành phần của rel

var (
    ErrPath       // rel rỗng, có "", ".", ".." hoặc byte 0, hoặc sâu quá MaxDepth
    ErrSymlink    // một thành phần của đường dẫn (kể cả neo, kể cả tên cuối khi thao tác cần mở nó) là symlink
    ErrNotRegular // không phải file thường (thư mục, FIFO, socket, thiết bị) ở chỗ cần file thường
    ErrTooLarge   // lớn hơn trần
    ErrOwner      // file không thuộc user của site
    ErrLinked     // file thường có hơn một tên (hardlink) ở thao tác đổi chủ, đổi quyền
    ErrTooMany    // thư mục có quá MaxDirEntries mục
)

// Error là lời từ chối của cửa. errors.Is(err, ErrSymlink)... dùng được.
type Error struct{ Op, Path string; Err error; Detail string }

type Info struct{ Mode fs.FileMode; Size int64; UID, GID uint32; Nlink uint64 }

func Split(root, p string) (anchor, rel string, ok bool)
func InZone(p string) bool

func ReadFile(anchor, rel string, max int64) ([]byte, error)
func Lstat(anchor, rel string) (Info, error)
func ReadDir(anchor, rel string, max int) ([]string, error)
func WriteFileAtomic(anchor, rel string, data []byte, perm fs.FileMode, uid, gid int) error
func MkdirAll(anchor, rel string, perm fs.FileMode, uid, gid int) error
func Remove(anchor, rel string) error
// Rename: bên nào ở ngoài vùng thì anchor của bên đó rỗng và rel là đường dẫn tuyệt đối.
func Rename(oldAnchor, oldRel, newAnchor, newRel string) error
func Symlink(target, anchor, rel string) error
func Readlink(anchor, rel string) (string, error)
func Chown(anchor, rel string, uid, gid int) error
func Chmod(anchor, rel string, perm fs.FileMode) error
func StatFS(anchor, rel string) (total, free uint64, err error)
```

**Luật của cửa** (mọi hàm):

- **C1, gốc và neo.** `open(Root, O_RDONLY|O_DIRECTORY|O_CLOEXEC)` (các thành phần của chính `Root` thuộc root nên được đi theo), `fstat`: chủ phải là uid của tiến trình và không có bit ghi cho nhóm, người khác, không thì `ErrRootUnsafe` (S15). Rồi `openat(rootfd, <tên neo>, O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC)`. Neo không có: lỗi thoả `fs.ErrNotExist`. Neo là symlink: `ErrSymlink` (kể cả symlink do admin đặt để dời site sang đĩa khác; mục 6 nói cách thay bằng bind mount, S11).
- **Đổi bit quyền hai chiều (S3).** `fs.FileMode` của Go để setgid ở bit 22, kernel ở `0o2000`. Mọi `fchmod`, `mkdirat`, `openat(O_CREAT)` đổi như `syscallMode` của gói `os` (setuid `04000`, setgid `02000`, sticky `01000`, 9 bit thường); mọi `Info.Mode` dựng từ `Stat_t` như `fillFileStatFromSys` (loại file sang `fs.ModeDir`, `fs.ModeSymlink`, `fs.ModeNamedPipe`, `fs.ModeSocket`, `fs.ModeDevice|fs.ModeCharDevice`; ba bit đặc biệt sang `fs.ModeSetuid`, `fs.ModeSetgid`, `fs.ModeSticky`). Làm sai thì `public` ra `0750` thay vì `2750` mà test trên máy giả vẫn xanh, nên có bất biến B16.
- **C2, đi từng thành phần.** `rel` tách theo `/`; thành phần rỗng, `.`, `..`, có byte 0, hoặc quá `MaxDepth` thành phần: `ErrPath`, không gọi kernel. Mỗi thành phần giữa: `openat(fd, c, O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC)`. Lỗi `ELOOP` hoặc `ENOTDIR`: `fstatat(fd, c, AT_SYMLINK_NOFOLLOW)` để phân loại (chỉ để viết câu lỗi): là symlink thì `ErrSymlink`, không thì giữ `ENOTDIR`. `ENOENT` giữ nguyên (thoả `fs.ErrNotExist`).
- **C3, tên cuối.** Chỉ thao tác bằng fd đã mở (`fstat`, `fchmod`, `fchown`, `read`, `fstatfs`) hoặc bằng lời gọi `*at` trên fd thư mục cha đã qua C2 (`fstatat(AT_SYMLINK_NOFOLLOW)`, `unlinkat`, `renameat`, `mkdirat`, `symlinkat`, `readlinkat`). Không hàm nào của gói nhận lại một đường dẫn đầy đủ để đưa cho `os.*`.
- **C4, đọc** (`ReadFile`). (a) `fstatat(AT_SYMLINK_NOFOLLOW)` trước: symlink thì `ErrSymlink`, loại khác file thường thì `ErrNotRegular`, **không mở** (không bao giờ mở thiết bị hay FIFO có sẵn). (b) `openat(O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC)`: `O_NONBLOCK` để một FIFO bị tráo vào sau bước (a) không làm treo. (c) `fstat` trên fd, và đây là lần kiểm có giá trị: phải là file thường; `st.Uid` phải bằng uid của neo, trừ khi neo thuộc root (uid 0) thì bỏ luật chủ; `st.Size ≤ max`. (d) đọc qua `io.LimitReader(max+1)`, dài hơn `max` là `ErrTooLarge` (file lớn dần sau `fstat`).
- **C5, đổi chủ, đổi quyền** (`Chown`, `Chmod`). `fstatat` trước như C4(a); `openat(O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC)`; `fstat` trên fd: thư mục, hoặc file thường có **đúng một tên** (`Nlink == 1`, không thì `ErrLinked`); rồi `fchown` hoặc `fchmod` trên fd. Symlink: `ErrSymlink` (không đổi chủ của chính cái link như `Lchown`). Không bao giờ dùng `fchmodat`, `fchownat` theo tên cho việc này: trên Linux `fchmodat` không có cờ "không theo symlink", nên nó đi theo symlink tráo vào sau lần `fstatat`, đúng kiểu lỗ T8 (S7).
- **C6, ghi** (`WriteFileAtomic`). File tạm `.<tên>.tpan-<16 hex ngẫu nhiên>` tạo bằng `openat(dirfd, tmp, O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC, 0600)`; ghi, `fchown`, `fchmod`, `fsync`, đóng; `renameat(dirfd, tmp, dirfd, tên)`. Hỏng thì `unlinkat` file tạm. `rename` thay chính thứ đang ở tên đó (kể cả symlink), không ghi qua nó.
- **C7, tạo thư mục** (`MkdirAll`). Đi như C2; thành phần chưa có: `mkdirat` rồi mở lại bằng `O_DIRECTORY|O_NOFOLLOW`, `fchown`, `fchmod` trên fd vừa mở. Chỉ thư mục mới tạo nhận quyền và chủ.
- **C8, xoá, đổi tên, symlink, đọc link, stat.** `Symlink`: `symlinkat` rồi đổi chủ của chính cái link về uid, gid của neo bằng `fchownat(dirfd, tên, uid, gid, AT_SYMLINK_NOFOLLOW)` (S14): symlink thuộc root trỏ tới file của root thì qua được `SymLinksIfOwnerMatch` của Apache và `disable_symlinks if_not_owner` của nginx; bị tráo giữa hai lời gọi thì chỉ đổi chủ một thứ của chính site. `Remove`: `unlinkat(dirfd, tên, 0)`, là thư mục thì `unlinkat(..., AT_REMOVEDIR)`. `Rename`: `renameat` giữa hai fd cha; bên ngoài vùng dùng `AT_FDCWD` với đường dẫn tuyệt đối. `Lstat`: `fstatat(AT_SYMLINK_NOFOLLOW)`. `ReadDir`: mở thư mục như C2 cho cả tên cuối, đọc tối đa `max+1` tên, quá thì `ErrTooMany`; trả tên đã sắp xếp.
- **C9, không treo.** Không lời gọi nào của cửa có thể chờ vô hạn: mọi `open` trên tên cuối có `O_NONBLOCK`, chỉ đọc file thường, có trần. Vì vậy cửa không cần `context`.
- **C10, lỗi.** Đường dẫn không có: lỗi thoả `errors.Is(err, fs.ErrNotExist)` như hợp đồng của `System`. Lời từ chối là `*sitefs.Error`; câu lỗi nêu đường dẫn và lý do, **không bao giờ chứa nội dung file hay đích của symlink**.

### 3.3 `system.System`: mọi thao tác file tự đi qua cửa

Hợp đồng ở `internal/system/types.go` thêm mục "Vùng site": với đường dẫn mà `sitefs.Split` nhận, mười hai hàm `ReadFile`, `WriteFileAtomic`, `MkdirAll`, `Remove`, `Rename`, `Symlink`, `Readlink`, `Stat`, `ReadDir`, `Chown`, `Chmod`, `StatFS` theo luật C1 đến C10. Ngoài vùng: giữ nguyên như v0.2.0.

- `LocalSystem` thêm trường `SiteRoot` (`NewLocal` đặt `sitefs.Root`; test đặt một thư mục tạm). Mỗi hàm bắt đầu bằng `sitefs.Split(s.SiteRoot, path)`; trong vùng thì gọi hàm cùng tên của `sitefs` (`ReadFile` với `sitefs.MaxFile`, `ReadDir` với `sitefs.MaxDirEntries`), ngoài vùng thì chạy mã cũ. `Rename` xét cả hai đường dẫn. Việc đổi `Owner` (tên) sang uid, gid vẫn ở `LocalSystem`.
- `FakeSystem` theo đúng các luật đó trên hệ thống file giả (gốc vùng cố định `/var/www`): trong vùng, symlink ở bất kỳ thành phần nào bên dưới neo là `ErrSymlink`; `ReadFile` chỉ nhận file thường, kiểm chủ (so `Owner.User` của file với của neo; neo thuộc `root` thì bỏ, như bản thật), kiểm trần; `Chown`, `Chmod` từ chối symlink và thứ không phải thư mục hay file thường. Lỗi dùng chung kiểu `*sitefs.Error`. Nhờ vậy test của `engine`, `steps`, `core`, `server` chạy trên Mac thấy đúng hành vi của máy thật.
- `Stat` trong vùng vẫn là "lstat" cho tên cuối (symlink trả về chính nó), nhưng thư mục cha là symlink thì lỗi `ErrSymlink`, **không** phải "không tồn tại": mã gọi không được hiểu nhầm thành "chưa có, tạo mới".
- **`LocalSystem.Run` chạy mỗi lệnh trong nhóm tiến trình riêng** (`SysProcAttr.Setpgid`), và khi `ctx` bị huỷ thì `Cancel` gửi `SIGKILL` cho cả nhóm (S1). Lý do, đã thử trên máy ảo (mục 9, trả lời S1): `runuser` fork ra lệnh thật rồi đứng chờ; Go chỉ giết `runuser`, lệnh con ở lại làm tiến trình mồ côi trong cgroup của `tpan.service` và giữ đầu ghi của ống stdout, nên `Wait` còn chờ thêm `WaitDelay`. CLI vẫn dừng đúng khi bấm Ctrl-C: `cmd/tpan/main.go` bắt `SIGINT`, `SIGTERM` bằng `signal.NotifyContext`, `ctx` bị huỷ thì cả nhóm bị giết.
- **`FakeSystem.SetSite(l site.Layout)`** (hàm cho test, S16): dựng neo và `public` với chủ `l.User:www-data`, quyền `2750`, để test của B2 chạy trên máy giả với luật chủ thật sự bật (neo do `mkParents` tạo ngầm thuộc root nên luật chủ bị bỏ).

### 3.4 Lệnh chạy bằng root

`LocalSystem.Run` và `FakeSystem.Run` từ chối (trả lỗi `system.ErrRootZoneCommand`, không chạy) một lệnh **chạm vùng site** mà không thuộc ngoại lệ. Một lệnh chạm vùng site khi, sau khi bỏ qua trường hợp miễn ở dưới (S4):

- `Name`, `Dir`, một phần tử của `Args`, hay một giá trị trong `Env` có chứa chuỗi `<gốc vùng>/` ở bất kỳ vị trí nào (bắt cả `-C/var/www/...`, `--tên=/var/www/...`, `-e "source /var/www/..."`); hoặc
- một phần tử của `Args` không bắt đầu bằng `-` mà là đường dẫn tương đối, ghép với `Dir` (rỗng là `/`) thì nằm dưới gốc vùng.

Dương tính giả chỉ làm test đỏ, không mở lỗ. Chỉ miễn xét khi `AsUser` là user của một site (khớp `^tp_[a-z0-9_]{1,29}$`, đúng dạng `site.Layout.User`): kernel giới hạn lệnh đó trong quyền của user ấy. `AsUser` khác (ví dụ `www-data`) vẫn bị xét như root.

Ngoại lệ là hằng `rootZoneCommands` trong gói `system`, **theo cặp chương trình và dạng đối số** (S5). Với mọi lệnh được miễn, mỗi đường dẫn trong vùng có trong argv phải đúng là một neo hoặc `<neo>/<một thành phần>`: cha của nó là neo, mà tên neo do root giữ. Đối số sâu hơn (ví dụ `<neo>/public/wp-content`) thì kernel đi theo symlink ở `public` trước khi lệnh kịp mở bằng fd, nên bị từ chối.

| Lệnh | Dạng được | Vì sao |
|---|---|---|
| `tar` | có `--create`, không có `--extract`, `-x`, `--get`, `-h`, `--dereference`, `-L` | tạo backup; N3: mở bằng fd và `O_NOFOLLOW`, lưu symlink là symlink. Giải nén vào cây do site giữ là kiểu tấn công symlink kinh điển, nên không bao giờ được miễn |
| `du` | mọi dạng (đường dẫn vẫn theo luật một thành phần) | N3: không đi theo symlink, chỉ in số |
| `useradd` | mọi dạng | `--home-dir <neo>` chỉ là chuỗi ghi vào `/etc/passwd` (`--no-create-home`) |

Không có `rm`, `find`, `chmod`, `chown`: hiện không lời gọi nào của tpan chạy chúng bằng root trên vùng site (`rm -r` chỉ chạy trên thùng rác, chỗ khôi phục, thư mục log). Danh sách chốt bằng cách bật luật trong `FakeSystem` rồi chạy toàn bộ unit test: lệnh nào bị chặn mà đúng là an toàn thì thêm kèm lý do, lệnh nào không an toàn thì sửa chỗ gọi.

### 3.5 `engine.Preview`

`FileChange` thêm trường `OldLink string` (`json:"old_link,omitempty"`): đích của symlink đang nằm ở `Path`, do engine điền, không đi theo.

Với mỗi `FileChange` lúc xem trước:

- `Path` ngoài vùng site: như v0.2.0 (`sys.ReadFile`, không có thì `OldExists=false`, lỗi khác thì báo lỗi).
- `Path` trong vùng site: `sys.Stat(Path)` trước (lstat qua cửa).
  - không có: `OldExists=false`;
  - là symlink: `OldLink = sys.Readlink(Path)`, `OldExists=true`, `Old` để trống. Không đọc đích. Lúc chạy thật bước `File` thay symlink bằng file thường như v0.2.0, nên bản xem trước nói đúng điều sẽ xảy ra;
  - là file thường: `sys.ReadFile(Path)` (cửa kiểm lại trên fd: file bị tráo sau `Stat` thì ra lỗi, không lộ);
  - loại khác, hoặc bất kỳ lỗi nào của cửa (`ErrSymlink` ở thư mục cha, `ErrNotRegular`, `ErrTooLarge`, `ErrOwner`): `Preview` trả lỗi bọc `*sitefs.Error`. Đúng nguyên tắc cũ của hàm này: không vẽ được diff đúng thì báo lỗi, không giả vờ. Lúc chạy thật các trường hợp đó cũng làm job hỏng ở bước `File` (hàm `capture` không giữ được nội dung cũ để hoàn tác), nên xem trước và chạy thật nói cùng một điều.

`Execute` không đổi. Sự kiện job chưa bao giờ mang `FileChange.Old` hay `New`; thêm test giữ điều đó.

### 3.6 Các Step

- `steps.File.Check`: so cỡ trước khi đọc (`fi.Size != len(Content)` thì chưa đạt, không đọc). File lớn không còn bị đọc chỉ để so.
- `steps.WPFiles`: bỏ lệnh `find <public> -mindepth 1 -type d -exec chmod 2750 {} +` chạy bằng root. Thay bằng hàm `chmodDirs(ctx, sys, root, perm)` trong gói `steps`: duyệt cây bằng `sys.ReadDir`, `sys.Stat`, `sys.Chmod` (cả ba đi qua cửa: không theo symlink, `fchmod` trên fd). Ba điểm để tương đương lệnh cũ (S13): không đổi quyền chính `root` (như `-mindepth 1`; bước `Dir` đã lo `public`); mục là symlink hay file thì bỏ qua; mục biến mất (`ENOENT`) hay thành symlink (`ErrSymlink`) giữa lúc liệt kê và lúc `Chmod` thì bỏ qua, vì chỉ hại chính site; lỗi khác làm hỏng bước. Không dừng ở ranh phân vùng, đúng như lệnh `find` cũ (không có `-xdev`): user của site không mount được gì root đi vào được (FUSE không có `allow_other` thì root bị từ chối). Giới hạn: sâu tối đa `sitefs.MaxDepth`, tối đa 200000 thư mục, kiểm `ctx` mỗi thư mục; vượt thì lỗi. Giữa lúc liệt kê và lúc `Chmod` user của site vẫn tráo được thư mục này bằng **một thư mục khác của chính nó** (vô hại), không tráo được ra ngoài. `StepInfo.Commands` ghi một dòng mô tả thay cho lệnh `find` cũ. Lệnh `find ... -type f -exec chmod 0640` (chạy bằng user của site) giữ nguyên. Bước này chỉ chạy khi `public` rỗng: lần tạo site đầu tiên, hoặc khi user của site xoá sạch `public` đúng lúc giữa lần kiểm `drift()` và bước `wp.files` (S8); lúc tạo site lần đầu chưa có tiến trình nào chạy bằng UID mới, trừ khi UID được dùng lại (F1, đợt 2).
- `checks.ScanHtaccess` (M14) đọc từng `.htaccess` bằng `dd if=<file> iflag=nonblock bs=<trần> count=1 status=none` dưới user của site thay cho `head` (S1): một FIFO (hay symlink tới FIFO) không làm lệnh chờ 10 giây rồi để lại tiến trình. Vẫn đi theo symlink như Apache khi đọc `.htaccess`.
- Các Step khác (`Dir`, `OwnedFile`, `WPConfig`, `WPConfigPerm`, `DBPassword.readOld`, `currentWPConfig`, `MoveTree`, `capture`, `saved.restore`) không sửa mã: lời gọi `sys.*` của chúng trên đường dẫn trong vùng tự đi qua cửa. Các lớp chặn riêng của chúng (ràng nội dung `DB_NAME`, lstat thư mục cha) giữ nguyên làm lớp thứ hai.
- Nội dung do site viết đi vào ngữ cảnh AI (S6): `tools.managedFile` và `tools.textPreview` (bản xem trước dạng chữ của `get_plan`, `propose_*`) bọc `Old` và `OldLink` của đường dẫn trong vùng site bằng `ai.Untrusted("file:<path>", ...)`, bọc `CheckError` bằng `ai.Untrusted("check-error", ...)`; `fileText` thêm `old_link`, `hidden`. Cờ "phiên có dữ liệu không tin" cho Plan đề xuất là việc của P10 (bộ che dữ liệu gửi AI, mốc M29), không làm ở đây.
- CLI (`cmd/tpan/print.go`, `health.go`): bước có `OldLink` in "thay symlink (→ <đích>) bằng file thường", bước có `Hidden` in lý do; đích symlink và mọi dòng nội dung lấy từ vùng site được in qua `strconv.Quote` hay lọc ký tự điều khiển, vì do site đặt nên có thể chứa chuỗi escape của terminal.

### 3.7 Lọc theo vai ở tầng API (lớp thứ hai, độc lập với cửa)

Mọi handler trả bản xem trước gọi `s.writePreview(w, r, body, pv, targets...)` thay cho `writeJSON`. Admin: không lọc. Vai `user`:

1. **Tập được phép.** `site.Layout.Owns(path)` (hàm mới ở gói `site`): `path` nằm trong `l.Dir/` hoặc `l.LogDir/`, hoặc bằng `NginxConf`, `NginxLink`, `ApacheConf`, `ApacheLink`, file pool `tpan-<slug>.conf` của bất kỳ bản PHP nào, file cron của site. Tập được phép của một request là mọi site của người gọi (`Store.SitesOf`) cộng các domain đích mà handler đã phân quyền xong (site đang được tạo).
2. **Ngoài tập** (file của máy, của site khác): xoá `Old`, `New`, `OldLink`; đặt `Hidden = "foreign"` (trường mới `json:"hidden,omitempty"` của `FileChange`). Đường dẫn và tên bước vẫn hiện.
3. **Trong tập và trong vùng site**, có `Old`: nội dung do root đọc chỉ được trả khi **một tiến trình chạy bằng chính user Linux của site đọc ra đúng những byte đó**: `timeout -k 1 2 dd if=<path> iflag=nonblock,nofollow bs=<MaxFile+1> count=1 status=none` với `AsUser = l.User`, `ctx` của lời gọi hạn 4 giây (S1). `nonblock`: tên đó bị tráo thành FIFO giữa lần đọc của cửa và lần đọc lại thì `dd` mở và đọc xong ngay (đã thử trên máy ảo: 4 ms, mã 0, không byte nào); `nofollow`: symlink thì `dd` từ chối mở; `timeout` chạy bên trong `runuser` để lệnh con chết theo hạn kể cả khi `runuser` bị giết; nhóm tiến trình riêng của `LocalSystem.Run` (mục 3.3) là lớp thứ hai cho việc đó. Khác nhau, lỗi, hay quá hạn: xoá `Old`, đặt `Hidden = "unverified"`. Lớp này không dựa vào cửa mà dựa vào quyền file của kernel: nếu ngày nào đó lớp đọc lại đi theo symlink, user thường vẫn chỉ nhận được thứ user Linux của site tự đọc được. Hàm đọc lại tách riêng (`verifyAsSiteUser(ctx, sys, user, path, old) bool`) để test được bằng `LocalSystem` và một user thật trong máy ảo (S9).
4. **Trong tập, ngoài vùng site** (vhost, pool, cron của chính site): giữ nguyên.

**Mã lỗi ở API** (S12), trong `apiErrorStatus`:

| Lỗi | HTTP, mã |
|---|---|
| `sitefs.Refused(err)` (một `Err*` của cửa) | 409 `site_path_unsafe` |
| lỗi bọc `*sitefs.Error` khác, trừ `ENOENT` (`ENOTDIR` khi `public` là file thường, `EACCES`, `ELOOP` không do symlink) | 409 `site_path_unsafe` |
| `ENOENT` trong vùng site | như trước (mã gọi tự xử lý "chưa có") |

Câu song ngữ nêu đường dẫn, lý do (theo `Err`) và cách xử lý (`ls -la <path>`, đưa về thư mục, file thường của site rồi thử lại). Không có nội dung file nào trong câu.

**Giao diện web** (S10) chưa biết `hidden` và `old_link`, nên với vai `user` màn duyệt hiện file của máy bị ẩn là "sửa" kèm "Nội dung không đổi", và `.htaccess` là symlink là "sửa" với cả file là dòng thêm. Luật 4 của `agent-dev/LO-TRINH.md` cấm sửa `web/` trước khi M16 vào `dev`, nên đợt này chỉ sửa CLI và JSON. Việc cho màn duyệt hiện đúng hai trường này ghi vào mục "Để sau" của KE-HOACH, làm sau khi M16 đã vào `dev`.

Bằng chứng và log job (`GET /jobs/{id}`, SSE): nội dung file không đi qua đường này (engine không đưa `Old`/`New` vào sự kiện; phép kiểm đọc file của site bằng user của site). Bộ lọc `foreignCheck` của M13 giữ nguyên.

### 3.8 Giới hạn từ chối phục vụ

- Trần cỡ một file panel đọc trong vùng site: 1 MiB. Trần số mục một thư mục: 100000. Trần độ sâu: 64.
- Không mở thiết bị, FIFO, socket (C4a); có bị tráo vào thì `O_NONBLOCK` và `fstat` trên fd từ chối ngay (C4b, c).
- Hạn: lời từ chối của cửa không chờ gì nên tính bằng mili giây. Cam kết đo ở e2e: bản xem trước với `.htaccess` là `/dev/zero`, FIFO hay file 2 GiB trả lời trong **5 giây** (bản thường mất 0,4 giây), tpan giữ nguyên PID.

### 3.9 Bảng mọi chỗ panel chạm vùng site

"Qua cửa" nghĩa là lời gọi `sys.*` đó giờ tự theo luật C1 đến C10. Cột cuối là test riêng chứng minh, kể cả chỗ phiên rà soát ghi "đã có lớp chặn".

| # | Chỗ | Đường dẫn | Loại | v0.2.0 | Sau M28 | Test |
|---|---|---|---|---|---|---|
| T1 | `engine.Preview` điền `Old` | `public/.htaccess` | site ghi được | `os.ReadFile`: lộ | mục 3.5, qua cửa, lọc theo vai | `TestXemTruocKhongDocQuaSymlink`, e2e |
| T1b | `steps.File.Check`, `capture` (bước `.htaccess`) | `public/.htaccess` | site ghi được | lstat rồi đọc: thua đua; nội dung giữ để hoàn tác có thể bị ghi lại cho site | qua cửa | `TestCuaDua`, `TestFileStepTrongVung` |
| T1c | `File.Apply`, `saved.restore`: `WriteFileAtomic` | `public/.htaccess` | site ghi được | cha bị tráo thì root ghi file sang thư mục khác | qua cửa (C6) | `TestVungSiteCaHaiBan/write` |
| T2 | `WPConfig` tạo, chmod | `public/wp-config.php` | site ghi được | chạy bằng user site | không đổi; `Stat`, `Remove` qua cửa | `TestVungSiteCaHaiBan/remove` |
| T3 | `WPConfigPerm` chown | `public/wp-config.php` | site ghi được | `Lchown` theo đường dẫn: cha bị tráo thì đổi chủ file nơi khác | qua cửa (C5) | `TestVungSiteCaHaiBan/chown` |
| T4 | `DBPassword.readOld` | `public/wp-config.php` | site ghi được | lstat, ràng nội dung; không trần | qua cửa, ràng nội dung giữ | `TestDocWPConfigQuaCua` |
| T5 | `currentWPConfig` | `public/wp-config.php` | site ghi được | lstat cả cha, ràng nội dung | qua cửa, giữ lớp cũ | `TestDocWPConfigQuaCua` |
| T6 | `BackupArchive` (`tar` root) | cả cây site | site ghi được | an toàn (N3) | giữ, có trong `rootZoneCommands` | `TestLenhRootKhongNhanDuongDanSite`, e2e |
| T7 | `BackupExtract` | `/var/lib/tpan/restore` | TPAN quản | an toàn | không đổi | (ngoài vùng) |
| T8 | `WPFiles`: `find -exec chmod` root | cây `public` | site ghi được | **thua đua (N3)** | `chmodDirs` qua cửa | `TestWPFilesDatQuyenQuaCua`, e2e |
| T8b | `Dir.Apply`: `Chown` rồi `Chmod` | `public`, `tmp` | site ghi được | `os.Chmod` đi theo symlink: tráo sau lstat thì root chmod đích | qua cửa (C5) | `TestVungSiteCaHaiBan/chmod` |
| T9 | `ScanHtaccess` | `public/**/.htaccess` | site ghi được | chạy bằng user site | không đổi | (đã có) |
| T10 | `Isolated` | file của site đích | site ghi được | chạy bằng user site | không đổi | (đã có) |
| T11 | `MoveTree` (`Rename`) | neo, `public` | neo: TPAN; `public`: site | rename không theo tên cuối | `public` qua cửa | `TestVungSiteCaHaiBan/rename` |
| T12 | `RemoveTree` (`rm -r` root) | log, thùng rác | TPAN quản | an toàn (N3) | giữ | `TestLenhRootKhongNhanDuongDanSite` |
| T13 | `WPArchive` | `/var/cache/tpan` | TPAN quản | ngoài vùng | không đổi | |
| T14 | `OwnedFile` | thư mục log | TPAN quản (user site không ghi được thư mục) | ngoài vùng | không đổi | |
| T15 | `tools.managedFile`, `Server.acceptFile`, `Fixes.dropAccepted` đọc file trong `managed_files` | `public/.htaccess` có trong sổ | site ghi được | `os.ReadFile`: admin, AI bị dụ đọc file tuỳ ý; `/dev/zero` làm sập | qua cửa; nội dung bọc `Untrusted` | `TestManagedFileTrongVung` |
| T16 | `du` root (đo dung lượng, đo backup) | neo, `public` | site ghi được | an toàn (N3) | giữ, chỉ nhận neo hay `<neo>/<một thành phần>` | `TestLenhRootKhongNhanDuongDanSite` |
| T17 | `core.stack_switch.htaccessIfMissing`: `Stat(public/.htaccess)` lúc dựng Plan chuyển sang Apache | `public/.htaccess` | site ghi được | lstat đi theo `public`, rồi bước ghi `.htaccess` sang thư mục đích (T1c) | qua cửa; `public` là symlink thì từ chối cả Plan kèm tên site và đường dẫn (S2, mục 5) | `TestVungSiteCaHaiBan/stat`, e2e B |
| T18 | `WPFiles.Apply`: `ReadDir(public)` | `public` | site ghi được | đi theo symlink; lời từ chối in số mục của một thư mục bất kỳ | qua cửa | `TestVungSiteCaHaiBan/readdir` |
| T19 | `core.Sites.drift`: `pathExists(wp-config.php)` | `public/wp-config.php` | site ghi được | lstat đi theo `public` | qua cửa | `TestVungSiteCaHaiBan/stat` |
| T20 | `checks.siteStat` (health): `Stat` các file của site | `public/...` | site ghi được | lstat đi theo `public` | qua cửa | `TestVungSiteCaHaiBan/stat` |
| T21 | `steps.backupPublic`: `Stat(<neo>/public)` | `public` | site ghi được | lstat, đúng | qua cửa; neo là symlink thì từ chối (S11, mục 6) | `TestVungSiteCaHaiBan/stat` |
| T22 | `checks.ScanHtaccess`: `head` dưới user site | `public/**/.htaccess` | site ghi được | FIFO làm chờ 10 giây và để lại tiến trình (S1) | `dd iflag=nonblock`; `Run` giết cả nhóm tiến trình khi hết hạn | `TestLenhRootHuyKhongDeLaiTienTrinh`, `TestScanHtaccess` (argv có `iflag=nonblock`), e2e A5 biến thể đua FIFO |

`StepInfo.Files` trong toàn bộ mã: `File` (đường dẫn do người dựng Plan đưa), `Absent`, `apt_m12`, `php_m12`, `suspend`, `harden_m14`. Chỉ một chỗ đưa đường dẫn trong vùng site: `SiteHtaccess` (`site_web.go:129`). Các đường dẫn còn lại đều dưới `/etc`, `/var/lib/tpan`, `/usr/local` (TPAN quản).

## 4. Bất biến

| # | Câu luôn đúng | Test |
|---|---|---|
| B1 | Panel không bao giờ đọc nội dung một đường dẫn trong vùng site bằng lời gọi đi theo symlink, ở bất kỳ thành phần nào. | `sitefs`: `TestCuaKhongDiTheoSymlink` (mọi hàm × symlink ở neo, ở giữa, ở cuối); `system`: `TestVungSiteCaHaiBan` (cùng bảng cho `LocalSystem` và `FakeSystem`) |
| B2 | Trong vùng site panel chỉ đọc file thường của chính user site, có trần cỡ, không treo. | `TestCuaChiDocFileThuong` (thư mục, FIFO, socket), `TestCuaTranCo` (file thưa 2 GiB, file lớn dần), `TestCuaKhongTreo` (FIFO, hạn 2 giây), `TestCuaChuFile` (cần root: chạy trong máy ảo) |
| B3 | Kiểm rồi mới làm không có khoảng hở: tráo file hay thư mục cha trong lúc đọc không bao giờ cho ra nội dung ngoài site. | `TestCuaDua` (1000 vòng tráo file, 1000 vòng tráo thư mục cha, một goroutine tráo bằng `rename`) |
| B4 | Root không đổi chủ, đổi quyền, ghi, xoá, đổi tên thứ gì ngoài neo qua một đường dẫn trong vùng site, kể cả khi bị đua (S7). | `TestCuaKhongRaNgoai` (mỗi thao tác ghi × cha là symlink ra thư mục "nạn nhân": nạn nhân không đổi); `TestCuaDuaGhi` (mỗi thao tác `Chmod`, `Chown`, `WriteFileAtomic`, `MkdirAll`, `Remove`, `Rename`, `Symlink` × một goroutine tráo liên tục thư mục cha và tên cuối với symlink tới nạn nhân, 1000 vòng: nạn nhân giữ nguyên quyền, chủ, nội dung, danh sách mục); `TestCuaDuaChmod` (cần root, trong máy ảo) |
| B5 | Mọi hàm của `System` có tham số đường dẫn đều theo luật vùng site. | `TestMoiHamCoLuatVungSite` (dùng reflect liệt kê hàm của `System`; hàm chưa có trong bảng test thì đỏ) |
| B6 | Không lệnh root nào nhận đường dẫn vùng site ngoài `rootZoneCommands`. | `TestLenhRootKhongNhanDuongDanSite`; luật bật trong `FakeSystem` nên mọi test chạy Plan đều kiểm |
| B7 | Ngoài gói `system` và `sitefs`, không mã nào gọi thẳng hàm file của `os`, `io/ioutil`, `path/filepath.Walk*`, `syscall`, `unix`. | `TestKhongGoiOSTrucTiep` (quét bằng `go/ast`; danh sách ngoại lệ theo file, mỗi dòng có lý do: `store` mở `tpan.db`, `ai/bridge` mở socket, `cmd/tpan` đọc file người gõ lệnh đưa) |
| B8 | Bản xem trước không chứa byte nào của đích một symlink trong vùng site. | `engine`: `TestXemTruocKhongDocQuaSymlink`, `TestXemTruocSymlinkHienDich` |
| B9 | `.htaccess` là file thường: bản xem trước giống v0.2.0 (`old`, `new`, `old_exists`). | `TestXemTruocFileThuongGiuDiff`; e2e so với bản xem trước của v0.2.0 |
| B10 | Vai `user` không bao giờ nhận nội dung file ngoài site của mình và ngoài file TPAN quản cho site đó. | `server`: `TestBanXemTruocLocTheoVai` |
| B11 | Hỏng lớp đọc vẫn không lộ: `System` giả đi theo symlink, vai `user` vẫn không nhận nội dung đích. Hỏng lớp API vẫn không lộ: admin (không lọc) cũng không nhận. | `TestBanXemTruocHaiLop` (máy giả); `TestVungSiteDocLaiBangUserSite` (S9, cần root, trong máy ảo: `LocalSystem` thật, user thật; `Old` dựng tay bằng đúng các byte của `/etc/shadow` cho một đường dẫn là symlink tới `/etc/shadow` thì ra "không khớp"; file của chính user thì khớp; FIFO tráo vào thì trả lời trong 4 giây) |
| B12 | Mọi handler trả bản xem trước đều đi qua `writePreview`. | `TestMoiHandlerXemTruocQuaWritePreview` (quét `go/ast`: hàm nào gọi `.Preview(` hay `.PreviewBatch(` mà không gọi `writePreview`/`filterPreview` thì đỏ); kèm khẳng định mọi route trả bản xem trước đã lưu (`/plans...`) là `admin` trong bảng route (S17: quét theo tên không thấy bản xem trước đã lưu) |
| B13 | Sự kiện và báo cáo job không mang nội dung file. | `engine`: `TestSuKienKhongChuaNoiDungFile` |
| B14 | Lỗi của cửa không chứa nội dung file hay đích symlink. | `TestLoiCuaKhongLoNoiDung` |
| B15 | Huỷ một lệnh (hết hạn, `ctx` bị huỷ) không để lại tiến trình nào, kể cả lệnh chạy qua `runuser` (S1). | `TestLenhRootHuyKhongDeLaiTienTrinh` (cần root, trong máy ảo: `runuser -u nobody -- sleep 30` với hạn 1 giây; sau đó `pgrep -u nobody sleep` rỗng); e2e A5 biến thể đua FIFO |
| B16 | `Lstat`, `Chmod`, `MkdirAll` trong vùng cho ra cùng `Mode` như `os.Lstat`, `os.Chmod`, `os.Mkdir` (S3). | `TestCuaGiuSetgid`, `TestCuaModeNhuOS` (thư mục `2750`, file `0640`, symlink, FIFO, socket; trên Mac và trong máy ảo bằng root); `TestVungSiteDirSetgid` (cần root: `steps.Dir{public, fs.ModeSetgid\|0o750}` trên `LocalSystem` ra `2750`, `Check` lần hai trả true); e2e D: chạy lại `site add` sau khi nâng thì các bước `dir:` bỏ qua và `public` vẫn `2750` |
| B17 | Cửa không rò fd (S7). | `TestCuaKhongRoFD` (đếm fd của tiến trình trước và sau 10000 lời gọi mỗi hàm, kể cả nhánh lỗi) |
| B18 | Symlink root tạo trong vùng thuộc user của site (S14). | `TestCuaSymlinkDoiChu` (cần root); e2e: sau mọi job không có symlink nào thuộc root trong vùng site (`find /var/www -type l -user root`) |
| B19 | Gốc vùng phải thuộc tiến trình và không ai khác ghi được (S15). | `TestCuaGocKhongAnToan` (`Root` có quyền `0777`: mọi hàm trả `ErrRootUnsafe`) |

## 5. Mô hình đe doạ

Kẻ tấn công: K1 và K2 (đầu file). Thứ cần giữ: file của site khác (`wp-config.php`: mật khẩu DB, khoá muối), `/etc/shadow`, `/var/lib/tpan/tpan.db`, khoá TLS; quyền và chủ của mọi file trên máy; panel phải sống.

| # | Đường tấn công | Ai | Chặn bằng | Test |
|---|---|---|---|---|
| D1 | `.htaccess` thành symlink, xin bản xem trước | K1 + K2 | cửa (C4a), `Preview` chỉ ghi đích; API cần user site đọc lại | B1, B8, B11; e2e ba mục tiêu |
| D2 | thư mục cha (`public`) thành symlink | K1 + K2 | cửa (C2) | B1, B4; e2e |
| D3 | tráo file hoặc thư mục đúng giữa lúc kiểm và lúc đọc | K1 + K2 | kiểm trên fd đã mở (C4c), không có lần kiểm theo đường dẫn nào được tin | B3; e2e vòng đua 1000 lần |
| D4 | hardlink tới file bí mật (máy tắt `fs.protected_hardlinks`) | K1 + K2 | chủ file phải là user site (C4c); đổi chủ, đổi quyền đòi một tên (C5) | B2; e2e |
| D5 | FIFO, socket, thiết bị, `/dev/zero`, file 2 GiB | K1 + K2, hoặc K1 chờ admin | C4a, C4b, trần (3.8) | B2; e2e: PID không đổi, trả lời trong 5 giây |
| D6 | tráo thư mục trong lúc root đặt quyền cây (`WPFiles`, `Dir`) | K1 + K2 (chạy lại `site add`) | `chmodDirs` và `Chmod` qua cửa (C5) | B4, B6; e2e |
| D7 | cha bị tráo lúc root ghi `.htaccess`, đổi chủ hay xoá `wp-config.php` | K1 + K2 | cửa (C6, C5, C8) | B4 |
| D8 | file trong `managed_files` bị tráo, chờ admin bấm "nhận bản sửa tay" hoặc AI gọi `managed_file` | K1 | cửa; nội dung bọc `Untrusted` | `TestManagedFileTrongVung` |
| D9 | tên file lạ (xuống dòng, bắt đầu bằng `-`, rất dài, thành phần `..`) | K1 | đường dẫn trong vùng chỉ đi qua `openat` từng thành phần, không vào shell; `ErrPath` | `TestCuaTenLa`; e2e |
| D10 | mã sau này thêm một lời gọi `os.ReadFile` hay một lệnh root mới trên đường dẫn site | người viết mã | B5, B6, B7 làm test đỏ | B5, B6, B7 |

**Riêng cho "panel chạy bằng root đụng thứ site kiểm soát"** (ba câu để nhớ):

1. Tên trong thư mục của site không nói gì về thứ nó trỏ tới. Chỉ tin thứ đã mở: mở không theo symlink, rồi hỏi chính fd đó (`fstat`).
2. Kiểm theo đường dẫn rồi làm theo đường dẫn là hai lần đi đường; người giữ thư mục đổi được đường giữa hai lần. Làm trên fd, hoặc để kernel giới hạn bằng cách chạy với quyền của user site.
3. Thứ root đọc được trong thư mục site là dữ liệu của site: có cỡ tuỳ ý, có thể không bao giờ kết thúc, và có thể là một câu lệnh cài cho AI.

**Còn hở, không sửa trong đợt 1:**

- `tar` chạy bằng root lúc backup đọc mọi file thường trong cây, kể cả hardlink tới file bí mật nếu máy đã tắt `fs.protected_hardlinks` (mặc định Ubuntu là bật). Bản khôi phục sẽ trao nội dung đó cho site. Đề xuất cho đợt sau: phép kiểm health cho hai sysctl `fs.protected_hardlinks`, `fs.protected_symlinks`.
- `Chown` qua cửa trên một file thường có một tên: máy đã tắt `fs.protected_hardlinks` thì user site có thể giữ một hardlink tới inode cũ của một file hệ thống sau khi file đó được thay bằng `rename`; sửa tự động `wp.config.perm` (chỉ admin) sẽ trao inode đó. Cùng điều kiện với dòng trên.
- Một tiến trình của user site còn sống (không phải PHP-FPM) giữ fd vào cây site vẫn sửa được cây sau khi nó vào thùng rác. Panel không đọc nội dung trong thùng rác; `rm` và `tar` an toàn theo N3.
- Một site làm hỏng bản xem trước hay job của một Plan gộp nhiều site (chuyển web server) bằng cách đặt thư mục hay FIFO ở `.htaccess` (đã có từ v0.2.0: `capture` từ chối), hoặc cho `public` thành symlink (mới từ M28, S2: v0.2.0 không chặn mà ghi `.htaccess` sang thư mục đích, tức lỗ T1c). Cả hai giờ bị từ chối ngay ở bản xem trước kèm tên site và đường dẫn. Đây là lựa chọn có chủ ý: bỏ site đó khỏi bước `.htaccess` không giúp admin, vì kiểm chứng sau khi chuyển của site đó sẽ hỏng và cả job vẫn hoàn tác. Chưa tách site lỗi khỏi Plan; admin sửa site đó (hoặc tạm khoá) rồi chạy lại. Harden toàn máy không bị: nó quét `.htaccess` dưới user site và đổi lỗi thành "giữ chế độ".
- Mã của site có chạy trong cgroup của tpan qua các lệnh panel gọi dưới user của site (có từ trước M28): để đợt 2 (cùng nhóm với F2 của M17); hướng sửa là chạy các lệnh đó trong một scope systemd tạm có trần tài nguyên và hạn giờ. Chi tiết giữ ngoài repo tới khi vá.
- FUSE: theo tài liệu, mount không có `allow_other` thì root bị từ chối truy cập, nên cửa không treo được (máy ảo: `fusermount3` có setuid, `/etc/fuse.conf` không bật `user_allow_other`; chưa thử mount thật). Máy bật `user_allow_other` thì một site có thể làm lời đọc của root chờ lâu.
- Màn duyệt của giao diện web chưa hiện `hidden`, `old_link` (S10, mục 3.7): làm sau khi M16 vào `dev`.
- Các phát hiện mục 6 của rà soát (P1, P2...) và nhóm F4, D5, H5, M1-DBCONN: đợt 2 (`agent-dev/M28-dot-2.md`).
- Soát cuối (`docs/thay-doi/M28-soat-cuoi.md`) tìm ra họ thứ hai, có từ trước M28, để đợt 2: panel chưa đặt trần cỡ và thời gian cho output của các lệnh chạy trên dữ liệu do site điều khiển, nên một site bị chiếm có thể làm panel chậm, giữ khoá hay hết bộ nhớ (C1 đến C4, C9). Hướng sửa: trần output và hạn giờ ngay trong `Run`. Chi tiết giữ ngoài repo tới khi vá.

## 6. Nâng cấp và lùi

- Không có migration, không đổi schema, không đổi file nào trên đĩa lúc nâng. Thay file chạy và khởi động lại dịch vụ là xong.
- Site đang chạy không đổi gì. Không có thao tác nào tự chạy lúc nâng.
- Lùi về v0.2.0 (`tpan self-update --rollback`) chạy được vì DB không đổi; lùi là mở lại T1.
- Chỗ khác v0.2.0, chỉ gặp ở máy đang ở trạng thái lạ:
  1. File trong vùng site mà panel cần đọc (`.htaccess`, `wp-config.php`) không thuộc user của site (ví dụ admin sửa bằng `sudo sed -i` làm file thành của root): trước đây panel đọc và ghi đè; giờ từ chối kèm lệnh `chown` cần chạy. `wp-config.php` sai chủ vẫn sửa tự động được (bước đó không đọc file).
  2. `.htaccess` lớn hơn 1 MiB: từ chối.
  3. Bản xem trước của vai `user` không còn nội dung các file của máy (vhost mặc định cổng 443 khi xin SSL); đường dẫn và tên bước vẫn hiện, JSON có `hidden`. Màn duyệt của giao diện web chưa biết trường này nên hiện "Nội dung không đổi" (mục 3.7).
  4. Symlink ở `.htaccess`: JSON có `old_link` (đích của link) thay cho nội dung đích, CLI in "thay symlink bằng file thường"; chạy thật vẫn thay bằng file thường như cũ. Màn duyệt của web hiện cả file là dòng thêm.
  5. Neo là symlink (admin dời site sang đĩa khác bằng `ln -s /data/<site> /var/www/<site>`, S11): v0.2.0 vẫn backup được site đó (chạy lại `site add` thì đã hỏng từ v0.2.0 ở bước `Dir`); từ M28 mọi thao tác trong vùng của site đó trả `site_path_unsafe`. Cách đúng: thay symlink bằng bind mount (`mount --bind /data/<site> /var/www/<site>`, ghi vào `/etc/fstab`): neo là thư mục thật, cửa đi qua bình thường. Ghi vào hướng dẫn nâng cấp.
  6. `/var/www` bị `chown` cho user khác root, hay có quyền ghi cho nhóm hoặc người khác (S15): mọi thao tác trong vùng trả `site_path_unsafe` kèm lệnh sửa.
- e2e nâng cấp: cài v0.2.0 từ bản phát hành (Apache), tạo site, lưu bản xem trước; lên bản của nhánh; bản xem trước của `.htaccess` thường giống hệt; health không có lỗi mới; site vẫn 200; chạy lại `site add` thì các bước `dir:` bỏ qua và `public` vẫn `2750` (S3); ba mục tiêu của T1 từ "lộ" thành "bị chặn".

## 7. Kế hoạch kiểm thử

**Unit (Mac, `make check`)**: các test ở mục 4. Test cần root (`TestCuaChuFile`, `TestCuaDuaChmod`, `TestCuaSymlinkDoiChu`, `TestLenhRootHuyKhongDeLaiTienTrinh`, `TestVungSiteDirSetgid`, `TestVungSiteDocLaiBangUserSite`, phần thiết bị) tự bỏ qua trên Mac và chạy trong máy ảo: `make m28-test-vm` build `go test -c` cho Linux của ba gói `sitefs`, `system`, `server`, đẩy vào máy ảo, chạy bằng root với `-test.run 'Cua|VungSite|LenhRoot'`; e2e đòi kết quả `PASS` và không có dòng `SKIP`.

**E2E bước 31** (`scripts/e2e-m28.sh`, log `docs/bang-chung/M28.log`, máy ảo `tpan-m28`). Mỗi khẳng định in `=> ĐẠT` hoặc `=> KHÔNG ĐẠT`:

- **A, Apache, máy sạch.** Hai site của admin, user `khach1` và site của nó.
  1. `.htaccess` thường: `old` đúng bằng file, `new` là bản chuẩn, HTTP 200.
  2. Ba mục tiêu (`wp-config.php` của site khác, `/etc/shadow`, `tpan.db`): câu trả lời không chứa file đích (so sha256 từng `old`, tìm 48 byte đầu dạng thô và dạng base64 trong cả câu trả lời); có `old_link`.
  3. Thư mục cha là symlink; hardlink (kernel chặn; tắt `fs.protected_hardlinks` thì cửa chặn, HTTP 409 `site_path_unsafe`); tên lạ trong `public` (xuống dòng, `-rf`, 255 ký tự) không làm hỏng bản xem trước.
  4. Đua: một tiến trình của user site tráo `.htaccess` giữa file thường và symlink tới `/etc/shadow`, rồi tráo `public`; 1000 lần gọi bản xem trước; không lần nào chứa file đích.
  5. File 2 GiB, FIFO: HTTP 409 `site_path_unsafe` trong 5 giây. Symlink tới `/dev/zero` (user của site chỉ đặt được `/dev/zero` bằng symlink, kernel cấm `mknod`): HTTP 200 trong 5 giây, `old_link`, không `old` (mục 3.5). PID của tpan không đổi, `NRestarts` không tăng. Biến thể đua (S1): kẻ tráo đặt FIFO đè lên `.htaccess` liên tục trong lúc khach1 gọi bản xem trước 50 lần (`.htaccess` là file thường lúc cửa đọc, FIFO lúc đọc lại): lần nào cũng trả lời trong 5 giây, sau cùng `pgrep -u <user site>` không còn `dd`/`head` nào.
  6. Chạy thật (`POST /sites` không `dry_run`) với symlink ở `.htaccess`: job thành công, `.htaccess` thành file thường, file đích không đổi (sha256), log job và `GET /jobs/{id}` không chứa file đích.
  7. Đặt quyền thư mục (S8): tạo một site mới `f.example.com`; một vòng lặp root chờ `/var/www/f.example.com/public/wp-includes` xuất hiện rồi khởi động kẻ tráo dưới user của site đó (giả lập tiến trình có sẵn của một UID được dùng lại) để tráo một thư mục con với symlink tới thư mục nạn nhân `0755` của root. Khẳng định: job có `step.done` của `wp.files:f.example.com`; nạn nhân giữ `0755`; các thư mục của site ra `2750`. Lặp 5 lần (mỗi lần xoá hẳn site). Phép thử chính cho lỗ này là test cần root `TestCuaDuaChmod` (1000 vòng trên chính cửa).
  8. Admin: `POST /files/accept` và công cụ `managed_file` trên `.htaccess` là symlink: từ chối, không có nội dung đích.
  9. Bộ L1 đến L9 của M14 (`scripts/e2e-m14/tan-cong.sh`) vẫn "bị chặn"; health không có lỗi; backup rồi khôi phục site chạy được (đường `tar`, `MoveTree`, `currentWPConfig` qua cửa); sau tất cả các phần trên không có symlink nào thuộc root trong `/var/www` (S14); `public` của mọi site vẫn `2750` (S3).
- **B, nginx, máy sạch.** Mục 1 không áp dụng (`.htaccess` không nằm trong Plan): khẳng định bản xem trước không có file nào trong vùng site. Chạy lại mục 3, 6 (đường `wp-config.php`: đổi mật khẩu DB với `wp-config.php` là symlink, là file 2 GiB), 9. Thêm (S2): `public` của một site là symlink thì `tpan stack switch --to apache --dry-run` thoát khác 0, lời từ chối nêu tên site và đường dẫn, máy không đổi gì (vhost, service vẫn như cũ).
- **C, test cần root trong máy ảo**: `make m28-test-vm`.
- **D, nâng từ v0.2.0** (mục 6).
- **Hồi quy**: `scripts/e2e-m13.sh chinh`, `scripts/e2e-m14.sh chinh`, `scripts/e2e-m15.sh`, mỗi cái từ máy sạch, log `docs/bang-chung/M28-hoi-quy-m13.log`, `-m14.log`, `-m15.log`.

## 8. Bảng chia việc

Toàn bộ mã là phần nhạy cảm (luật 8 của `LO-TRINH.md`): thợ `opus` effort high, hoặc phiên chính. Không hai thợ chung file.

| Đợt | Phần | Vùng file | Ai | Effort |
|---|---|---|---|---|
| 0 | Nghiên cứu, bản thử, bản thiết kế này, hợp đồng (`internal/sitefs/types.go`: hằng, lỗi, chữ ký; `engine.FileChange.OldLink`, `Hidden`) | `docs/`, `internal/sitefs/types.go`, `internal/engine/types.go` | phiên chính | max |
| 0 | Soát thiết kế (thợ `fable` đầu tiên chết vì hết hạn mức; chạy lại bằng `opus` theo lệnh 09:08 của chủ dự án) | chỉ đọc file này | thợ `opus` | max |
| 0 | Script e2e bước 31 và `make m28-test-vm`, chạy thử trên bản chưa vá (xong trước khi soát) | `scripts/e2e-m28.sh`, `scripts/e2e-m28/`, `Makefile` | thợ `opus` (`tho-may-ao`) | high |
| 1 | Cửa và `System`: thân các hàm `sitefs`, `LocalSystem` (cả nhóm tiến trình của `Run`), `FakeSystem` (cả `SetSite`), luật lệnh root, test B1 đến B7, B14 đến B19 | `internal/sitefs/` (trừ hợp đồng), `internal/system/` | thợ A `opus` | high |
| 1 | Lọc theo vai: `writePreview`, `filterPreview`, `verifyAsSiteUser`, `Layout.Owns`, mã lỗi 409, test B10 đến B12 (cả test cần root của B11) | `internal/server/`, `internal/site/owns*.go` | thợ B `opus` | high |
| 1 | Sửa e2e theo soát: A5 biến thể đua FIFO, A7 tạo site mới có kẻ tráo, A9 không symlink thuộc root và `public` `2750`, B chuyển web server khi `public` là symlink, D chạy lại `site add`; `m28-test-vm` thêm gói `server` | `scripts/e2e-m28.sh`, `scripts/e2e-m28/`, `Makefile` | thợ C `sonnet` (test, không nhạy cảm) | mặc định |
| 1 | Engine, Step, công cụ AI, CLI: `Preview`, `File.Check`, `chmodDirs`, `ScanHtaccess` bằng `dd`, `textPreview` và `managedFile` bọc `Untrusted`, in `OldLink`/`Hidden`; test B8, B9, B13; sửa test của các gói khác hỏng vì máy giả đổi hành vi | `internal/engine/`, `internal/steps/`, `internal/checks/m14*.go`, `internal/tools/`, `cmd/tpan/`, test của `internal/core/` và gói khác | phiên chính | max |
| 2 | Chạy e2e A, B, C, D, sửa lỗi gốc | máy ảo, `docs/bang-chung/M28*.log` | phiên chính | max |
| 3 | Soát cuối: tìm đường vòng qua bản vá và chỗ thứ hai cùng họ, thử phá trên máy ảo | chỉ đọc | thợ `opus` chưa đọc mã mốc này | max |
| 3 | Ba hồi quy (M13, M14, M15), một lần, trên bản cuối | máy ảo | phiên chính | max |
| 5 | File giải thích `docs/giai-thich/28-m28-va-theo-ra-soat.md` | `docs/giai-thich/` | thợ `opus` | medium |
| 5 | Gộp vào KE-HOACH (mục 6.26, hàng M28, bước e2e 31), merge `origin/dev`, đẩy `moc/M28:dev` | `docs/KE-HOACH.md` | phiên chính | max |

## 9. Phát hiện của soát thiết kế

(Thợ soát ghi vào đây; phiên chính trả lời từng điểm và sửa các mục trên.)

Soát ngày 21/11/2026 trên bản thiết kế này, mã ở nhánh `moc/M28` (commit `d6b535f`) và `internal/sitefs/types.go` đang viết dở. Chỉ đọc mã, không chạy gì trên máy ảo; chỗ nào cần đo thì ghi rõ ở phần đề xuất. Tổng: 1 nặng, 10 vừa, 7 nhẹ.

### S1 (nặng): Lớp lọc thứ hai đọc lại bằng `head` theo đường dẫn nên mở lại đúng lỗ FIFO mà cửa vừa đóng: mỗi bản xem trước bị đua để lại một tiến trình treo trong cgroup của tpan, cộng dồn tới khi tpan không fork được lệnh nào.
- Ở đâu: mục 3.7 bước 3 (`head -c <MaxFile+1> -- <path>`, `AsUser = l.User`, hạn 3 giây), mục 3.8 (cam kết 5 giây), D5 ở mục 5. Mã: `internal/system/local.go:94` (`exec.CommandContext`, không có nhóm tiến trình riêng), `local.go:62` (`WaitDelay = 5s`), `local.go:178` (`runuser -u <user> --`), `cmd/tpan/setup.go:295` (unit của tpan không đặt `TasksMax`, `KillMode`). Cùng cơ chế đã có từ M14 ở `internal/checks/m14.go:583` (ScanHtaccess đọc từng `.htaccess` bằng `head` dưới user site, hạn `htaccessReadTimeout = 10s` ở `m14.go:526`).
- Vì sao sai, theo thứ tự (stack Apache, nơi `.htaccess` nằm trong Plan):
  1. K1 giữ `public/.htaccess` là file thường của chính mình. FIFO tĩnh thì C4a từ chối ngay, không tới được lớp này.
  2. K2 gọi `POST /api/v1/sites?dry_run=1` cho site của mình. `engine.Preview` đọc `Old` qua cửa (hợp lệ), rồi còn chạy `Check` của mọi bước (nhiều lệnh `wp`), xong handler mới tới bước lọc. Khoảng giữa hai lần đọc dài hàng trăm mili giây tới vài giây.
  3. Trong khoảng đó K1 `rename` một FIFO đè lên `.htaccess` (K1 biết lúc root đọc qua inotify, vì thư mục là của K1, hoặc cứ tráo liên tục).
  4. `head` dưới user site chặn ở `open()` vì FIFO không có người ghi.
  5. Hết 3 giây, Go chỉ giết tiến trình con trực tiếp là `runuser`. `head` do runuser fork ra (runuser mở phiên PAM nên tự đứng chờ con) thành mồ côi, vẫn ở trong cgroup `tpan.service`, và giữ đầu ghi của ống stdout. `Wait` vì vậy chờ thêm `WaitDelay` 5 giây: request mất khoảng 8 giây, vượt cam kết 5 giây của 3.8. Còn `head` thì nằm đó mãi.
  6. Không có giới hạn số request xem trước, kể cả song song. Mỗi request để lại một tiến trình. Unit không đặt `TasksMax` nên systemd dùng mặc định 15% số tác vụ tối đa của máy, trên VPS 1 tới 2 GB vào khoảng một tới hai nghìn rưỡi.
  7. Chạm trần là mọi `fork/exec` của tpan lỗi: job, health, backup, xem trước đều hỏng cho tới khi admin khởi động lại dịch vụ. Với 50 request song song, mỗi cái 8 giây, việc này mất vài phút.
  8. e2e A.5 chỉ thử FIFO tĩnh nên vẫn xanh trong khi lỗ còn đó. ScanHtaccess có cùng cơ chế, và K1 một mình kích được qua health hay qua kiểm chứng sau mỗi job.
  9. Vì sao xếp nặng, dù phần "Để đợt 2" có ghi một đường làm cạn tác vụ khác đã có từ v0.2.0: D5, C9 và 3.8 của chính bản thiết kế này nói lỗ treo do FIFO đã đóng, còn cơ chế mới ở 3.7 mở lại nó; K2 kích bằng một lần `dry_run` rẻ (không job, không khoá); và cách sửa nằm trọn trong phần việc của đợt 1 (3.7 và `LocalSystem.Run`).
- Đề xuất:
  - Sửa 3.7 bước 3: đọc lại bằng một lệnh không chặn lúc mở và không đi theo tên cuối, ví dụ `dd if=<path> iflag=nonblock,nofollow bs=<MaxFile+1> count=1 status=none` (xác nhận cờ trên coreutils 9.4 của máy ảo), bọc trong `timeout -k 1s 2s` chạy bên trong `runuser`, để tiến trình con chết theo hạn kể cả khi runuser bị giết. Đây là cách sửa chính.
  - Thêm vào 3.3 một luật cho `LocalSystem.Run`: mỗi lệnh chạy trong nhóm tiến trình riêng (`SysProcAttr.Setpgid`), và `Cancel` giết cả nhóm. Luật này cũng vá ScanHtaccess, với điều kiện `runuser` không đưa tiến trình con sang nhóm hay phiên mới (kiểm trên máy ảo).
  - Thêm bất biến B15 "huỷ một lệnh không để lại tiến trình nào". Test trên máy ảo: tráo FIFO vào giữa lần đọc của cửa và lần đọc lại, 50 lần; sau đó `pgrep -u <user site>` rỗng và request nào cũng trả lời trong 5 giây.
  - Thêm biến thể đua này vào e2e A.5, bên cạnh FIFO tĩnh. Xác nhận trên máy ảo rằng `runuser` để lại tiến trình con khi bị SIGKILL, để test chứng minh đúng điều nó nói.

### S2 (vừa): Một site có `public` là symlink chặn cả Plan chuyển nginx sang Apache của máy, và ý 4 của "còn hở" nói "đã có từ v0.2.0" là sai với trường hợp này.
- Ở đâu: mục 3.3 gạch cuối (`Stat` trong vùng trả `ErrSymlink` khi cha là symlink), mục 5 "Còn hở" ý 4. Mã: `internal/core/stack_switch.go:381-399` (vòng qua mọi site active, gọi `htaccessIfMissing`) và `stack_switch.go:425-431` (`sys.Stat(public+"/.htaccess")` lúc dựng Plan).
- Vì sao sai, theo thứ tự:
  1. Máy nginx có nhiều site. K1 của một site cho `public` thành symlink, hoặc tráo qua lại.
  2. Admin chạy `tpan stack switch --to apache`, có hay không `--dry-run`. Lúc dựng Plan, `htaccessIfMissing` nhận `ErrSymlink` và trả lỗi, nên Plan của cả máy không dựng được.
  3. Nếu lúc dựng Plan site đó chưa có `.htaccess` (nên Plan có bước ghi nó) và K1 tráo `public` sau đó, thì bước `.htaccess` lỗi lúc chạy và cả job (một Plan phẳng) bị hoàn tác.
  4. Ở v0.2.0, lstat đi theo `public` nên không chặn gì; thay vào đó là lỗ T1c (ghi `.htaccess` sang thư mục đích). Như vậy K1 một mình chặn được một thao tác của admin trên cả máy, và gây lại được ngay sau khi admin sửa.
  5. Đã kiểm hai luồng gộp khác, không bị: `POST /apply` cho site đã có ra `skip`; harden toàn máy dùng ScanHtaccess dưới user site và đổi lỗi thành "giữ chế độ".
- Đề xuất: sửa ý 4 của mục 5 cho đúng. `htaccessIfMissing` coi lời từ chối của cửa là "bỏ site này khỏi bước `.htaccess`, kèm cảnh báo trong bản xem trước", không làm hỏng cả Plan. Thêm vào e2e B một bước: máy nginx hai site, một site có `public` là symlink; `stack switch --to apache --dry-run` vẫn ra Plan cho site còn lại.

### S3 (vừa): Bản thiết kế không ghi, và không test nào bắt, việc cửa phải đổi `fs.FileMode` sang bit của kernel và ngược lại; làm sai thì thư mục site mất setgid mà mọi unit test vẫn xanh.
- Ở đâu: mục 3.2 (chữ ký `Chmod(..., perm fs.FileMode)`, `MkdirAll`, `Info.Mode`, luật C5 và C7), mục 3.6 (`chmodDirs`), mục 4. Mã: `internal/steps/site_user.go:276-277` (`fs.ModeSetgid | 0o750`), `internal/steps/common.go:114` (`dirMode`), `internal/steps/files.go:364` (`Dir.Check` so `dirMode(fi.Mode) == s.Perm`), `internal/system/fake.go:550-552` (fake lưu thẳng `fs.FileMode`). Hợp đồng `internal/sitefs/types.go` có một câu về việc đổi bit; bản thiết kế và bảng bất biến thì không.
- Vì sao sai, theo thứ tự:
  1. `fs.ModeSetgid` của Go là bit 22, không phải `0o2000`. Cài đúng chữ của C5 là `unix.Fchmod(fd, uint32(perm))`; kernel chỉ giữ 12 bit thấp, nên setgid mất mà không báo lỗi.
  2. Bước `Dir` của `public` và `chmodDirs` ra `0750` thay vì `2750`. Thư mục, file tạo sau đó (ảnh tải lên, file `tar` giải nén dưới user site) mang nhóm riêng của user site thay vì `www-data`, nên nginx hay Apache không đọc được. Trang vẫn 200, nhưng ảnh, CSS mới thì 403.
  3. Chiều ngược lại: `Info.Mode` lấy từ `fstatat` mà không đổi `S_IFDIR`, `S_ISGID` sang `fs.ModeDir`, `fs.ModeSetgid`, thì `Dir.Check` luôn sai, hoặc báo "không phải thư mục".
  4. `FakeSystem` lưu thẳng `fs.FileMode`, nên test của engine, steps, core vẫn xanh. e2e A chỉ đo HTTP 200 nên cũng có thể xanh. Trên máy nâng từ v0.2.0, lần chạy lại `site add` đầu tiên sẽ làm hỏng site đang chạy.
- Đề xuất: mục 3.2 ghi rõ hai chiều đổi bit, theo đúng `syscallMode` và `fillFileStatFromSys` của gói `os`. Mục 4 thêm B16 "`Lstat`, `Chmod`, `MkdirAll` trong vùng cho ra cùng `Mode` như `os.Lstat`, `os.Chmod`", với test so `sitefs.Lstat` và `os.Lstat` trên thư mục 2750, file 0640, symlink, FIFO, socket, chạy cả trên Mac lẫn máy ảo bằng root. Thêm test `Dir{public, fs.ModeSetgid|0o750}` trên `LocalSystem` trong máy ảo: `stat -c %a` ra `2750`, và lần `Check` thứ hai trả true. e2e D thêm một bước sau khi nâng: chạy lại `site add`, các bước `dir:` phải ra `skipped` và `public` vẫn `2750`.

### S4 (vừa): Cách luật 3.4 nhận ra "tham số là đường dẫn trong vùng site" bỏ sót tên chương trình, đường dẫn tương đối, cờ ngắn dính giá trị và đường dẫn nằm trong tham số ghép.
- Ở đâu: mục 3.4, câu đầu và câu cuối. Mã: `internal/system/local.go:184-194` (`lookPath`: tên có "/" thì chạy đúng đường dẫn đó), `local.go:96-100` (`Dir` rỗng nghĩa là "/"), `internal/system/types.go:145-150` (`Env` được mang `PATH`).
- Vì sao sai: luật chỉ xét từng tham số (với `--tên=` thì phần sau dấu `=`) và `Dir`. Các dạng dưới đây đều qua được B6 và D10, mà vẫn là root chạm vùng site:
  1. `Cmd{Name: "/var/www/a.example.com/public/x"}`: root chạy chương trình do site viết. Đây là trường hợp tệ nhất, tệ hơn mọi thứ luật đang chặn.
  2. Đường dẫn tương đối: `Dir` mặc định là "/", nên `Args: ["var/www/a.example.com/public/x"]` chính là một đường dẫn trong vùng.
  3. Cờ ngắn dính giá trị: `tar -C/var/www/...`, `curl -o/var/www/...`.
  4. Đường dẫn nằm trong một tham số ghép: `mariadb -e "source /var/www/.../x.sql"`.
  5. Câu "lệnh chạy bằng AsUser không bị xét" chỉ đúng khi AsUser là user của site. Với AsUser là `www-data` thì lệnh đọc được file 0640 nhóm `www-data` của mọi site; với `root` thì không giới hạn gì.
- Đề xuất: sửa 3.4. Xét cả `Name` và các giá trị trong `Env`. Đường dẫn tương đối thì ghép với `Dir` trước khi xét. Ngoài phép `Split`, coi là chạm vùng mọi phần tử argv có chứa chuỗi `SiteRoot + "/"` ở bất kỳ vị trí nào; dương tính giả chỉ làm test đỏ, không mở lỗ. Chỉ miễn xét khi `AsUser` là user của một site (dạng `tp_*`). Thêm vào B6 một bảng test, mỗi dạng ở trên một dòng.

### S5 (vừa): Ngoại lệ `rootZoneCommands` đi theo tên chương trình, trong khi bằng chứng N3 chỉ phủ đúng ba dạng đối số đang dùng.
- Ở đâu: mục 3.4 (bảng ngoại lệ), mục 2 (N3). Mã: `internal/steps/backup.go:270-276` (`tar --create ... --directory=<neo> public`), `internal/steps/site_user.go:442-444` (`rm -r --one-file-system --`), `backup.go:116-118` và `internal/core/users.go:821` (`du`).
- Vì sao sai: hai kiểu gọi dưới đây qua được B6 nhưng không an toàn.
  1. `tar --extract -C /var/www/<d>/public` bằng root. Root giải nén vào một cây do site giữ là kiểu tấn công symlink kinh điển: user site đặt sẵn symlink ở thư mục giữa. N3 không đo trường hợp này, và không có gì trong luật hiện tại ép câu "chỉ dùng để tạo backup".
  2. Đối số có thành phần giữa do site giữ, ví dụ `rm -r --one-file-system -- /var/www/<d>/public/wp-content/cache` hay `tar --create --directory=/var/www/<d>/public wp-content`. Kernel giải đường dẫn đối số và đi theo symlink ở `public` trước khi phần đi bằng fd mà N3 đo được bắt đầu. K1 tráo `public` thành symlink là root xoá hay nén cây của người khác.
- Đề xuất: sửa 3.4 thành ngoại lệ theo cặp (chương trình, dạng đối số). `tar` chỉ được miễn khi có `--create` và không có `--extract`, `-x`, `-h`, `--dereference`. Với mọi lệnh được miễn, mỗi đường dẫn vùng site trong argv phải là một neo hoặc `<neo>/<một thành phần>`: không thành phần giữa nào do site giữ, chỉ thành phần cuối nằm trong thư mục của site, và `tar`, `du`, `rm` không đi theo symlink ở thành phần cuối (N3). Lưu ý neo thuộc user site (2750), nên điều kiện là "không có thành phần giữa do site giữ", không phải "cha thuộc root"; viết nhầm thành vậy là chặn luôn `du -- <neo>/public` của bước đo trước backup. Thêm vào B6 test cho hai dạng bị từ chối ở trên.

### S6 (vừa): Bản xem trước dạng chữ của công cụ AI đưa nguyên văn `.htaccess` của site vào ngữ cảnh AI mà không bọc `Untrusted`, trong khi 3.6 chỉ bọc `managed_file`.
- Ở đâu: mục 3.6 gạch cuối, mục 5 câu nhớ thứ 3 ("có thể là một câu lệnh cài cho AI"). Mã: `internal/tools/read.go:458-463` (`textPreview` chép `Old` thô), `read.go:418-426` (`get_plan`), `internal/tools/plan.go:41-63` (`propose_*` trả `Preview: textPreview(...)`), `internal/tools/catalog.go:200-229` (`get_plan`, `propose_*` đều khai `untrusted=false`).
- Vì sao sai, theo thứ tự:
  1. K1 viết vào `public/.htaccess`, file thường của chính site nên cửa cho đọc, một đoạn chữ dạng chỉ thị cho AI.
  2. Admin hỏi AI về site đó; AI gọi `propose_site_repair`, hoặc `get_plan` trên một Plan đã có.
  3. Trường `old` đưa nguyên văn file vào ngữ cảnh của AI, không có thẻ `<untrusted>`. Phiên không bị đánh dấu, nên Plan đề xuất sau đó không mang cảnh báo. Ở chế độ client của MCP, agent được tự chạy Plan của chính nó (trừ tạo site) sau khi hỏi người dùng của agent.
  4. `CheckError` (chứa output của `wp`, tức mã PHP của site) cũng đi thẳng vào ngữ cảnh.
  5. `fileText` không có `old_link`, `hidden`, nên với symlink AI thấy `old_exists: true, old: ""` và hiểu là "file rỗng".
- Đề xuất: mục 3.6 thêm `tools.textPreview`. `Old` và `OldLink` của đường dẫn trong vùng site bọc `ai.Untrusted("file:<path>", ...)`; `CheckError` bọc `ai.Untrusted("check-error", ...)`; thêm `old_link`, `hidden` vào `fileText`. Bản xem trước có `Old` trong vùng site thì đặt cờ untrusted cho phiên và cho Plan đề xuất. Thêm test ở `internal/tools`.

### S7 (vừa): Không bất biến nào thử đua trên các thao tác ghi, mà cả ba lỗ ghi thật của v0.2.0 đều là đua; cũng không ai đếm fd rò.
- Ở đâu: mục 4 B3 (chỉ "trong lúc đọc"), B4 ("cha là symlink" tĩnh); mục 3.2 C5, C6, C7.
- Vì sao sai:
  1. T8 (`find -exec chmod`), T8b (`Lchown` rồi `os.Chmod`), T1c (`WriteFileAtomic`) trong bảng 3.9 đều thua vì đua, không phải vì symlink tĩnh.
  2. Một bản C5 cài kiểu "`fstatat` thấy thư mục rồi `fchmodat(dirfd, tên)`" vẫn qua B4. Lý do: `fchmodat` trên Linux không có cờ không theo symlink, nên nó đi theo symlink tráo vào sau, đúng như T8. Tương tự cho C7 nếu mở lại theo tên rồi đổi chủ theo tên.
  3. B3 có đua, nhưng chỉ cho `ReadFile`.
  4. Mỗi lần đi đường mở tối đa `MaxDepth` fd. Quên đóng một fd ở một nhánh lỗi thì tpan cạn fd sau vài nghìn lần gọi (`chmodDirs` gọi hàng nghìn lần cho mỗi site), và không bất biến nào nói tới chuyện này.
- Đề xuất: mục 4 thêm biến thể đua cho B4. Với từng thao tác ghi (`Chmod`, `Chown`, `WriteFileAtomic`, `MkdirAll`, `Remove`, `Rename`, `Symlink`), một goroutine tráo liên tục thư mục cha và tên cuối giữa thư mục thật và symlink tới thư mục "nạn nhân", 1000 vòng; nạn nhân phải giữ nguyên quyền, chủ, nội dung và danh sách mục. Thêm bất biến "cửa không rò fd": đếm fd đang mở của tiến trình trước và sau 10000 lần gọi mỗi hàm, kể cả nhánh lỗi, hai số phải bằng nhau.

### S8 (vừa): e2e A.7 viết như hiện tại sẽ xanh mà không lần nào chạy tới `chmodDirs`.
- Ở đâu: mục 7 A.7 ("chạy lại `site add` trên `public` rỗng"), mục 2 N3 ("Chủ site gọi lại được bước này qua POST /sites"). Mã: `internal/core/site.go:258-283` (`drift`), `site.go:420` (gọi `drift` trong `PrepareAdd`), `internal/steps/site_wp.go:353-359` (`WPFiles.Apply` từ chối `public` không rỗng), `site_wp.go:331-343` (`WPFiles.Check`).
- Vì sao sai:
  1. Site đã có thì user DB có. Làm rỗng `public` trước khi gọi `site add` là mất luôn `wp-config.php`, nên `PrepareAdd` từ chối `site_drift` (409) và không job nào chạy.
  2. Giữ `wp-config.php` thì `public` không rỗng, và `WPFiles.Apply` từ chối.
  3. Đường duy nhất tới bước đặt quyền là đua: `drift()` qua ở `PrepareAdd`, rồi user site xoá hết `public` trước khi bước `wp.files` chạy. Hoặc user DB cũng đã mất.
  4. Viết A.7 đúng như chữ thì 50 vòng không vòng nào chạy `chmodDirs`, nạn nhân "giữ nguyên quyền", và test xanh mà không kiểm gì.
- Đề xuất: mục 7 A.7 ghi rõ cách tới được bước này. Cách 1: tạo site mới, với một vòng lặp root chờ `/var/www/<d>/public/wp-includes` xuất hiện rồi mới khởi động tiến trình tráo dưới user của site. Cách 2: trên site có sẵn, xoá `public` ngay sau khi job đã bắt đầu. Mỗi vòng phải khẳng định có sự kiện `step.done` của `wp.files:<domain>` trong job, và các thư mục của chính site mà tiến trình tráo đưa vào ra `2750`, để biết `chmodDirs` thật sự đã chạy. Câu trong N3 cũng nên ghi đúng điều kiện đua này.

### S9 (vừa): Khẳng định "hai lớp độc lập" (B10, B11) chỉ được thử trên `FakeSystem`, nơi "user site đọc lại" là một handler do người viết test tự đặt.
- Ở đâu: mục 4 B10, B11; mục 7 C (`make m28-test-vm` chỉ build hai gói `sitefs`, `system`). Mã: `internal/system/fake.go:143-180` (`Run` trả rỗng, mã 0 khi không có handler).
- Vì sao sai: ý chính của 3.7 là lớp thứ hai dựa vào quyền file của kernel. Trên `FakeSystem` thì không có kernel. Không đặt handler thì `head` "đọc" ra chuỗi rỗng với mã 0, nên mọi phép so đều ra khác nhau. Một lỗi kiểu "luôn trả `Old`" vẫn bị B11 bắt. Nhưng chính thuộc tính cần chứng minh (kernel không cho user site đọc file của người khác) thì không được thử, và một lỗi làm lớp thứ hai từ chối nhầm chỉ lộ ra nếu người viết test tự đặt handler đúng. Không chỗ nào chạy lớp thứ hai với `runuser` thật và một file mà user site không đọc được.
- Đề xuất: đưa test lớp thứ hai vào `make m28-test-vm`, chạy bằng root với `LocalSystem` và một user site thật. Dựng tay một bản xem trước có `Old` đúng bằng các byte của `/etc/shadow`, cho một đường dẫn trong vùng là symlink tới `/etc/shadow` (giả lập lớp đọc hỏng); lọc theo vai `user` phải ra `Hidden = "unverified"`. Cùng bộ dữ liệu với một file của chính site thì phải giữ `Old`. Ghi thêm gói đó vào mục 7 C.

### S10 (vừa): Giữ nguyên `web/` làm màn duyệt nói sai với vai `user`: file của máy bị ẩn hiện "Nội dung không đổi.", còn symlink thì không được nhắc tới.
- Ở đâu: mục 1 ("không sửa gì trong `web/`"), mục 3.5 (`OldLink`), mục 3.7 (`Hidden`, xoá `Old` và `New`), mục 6 ý 3 và 4. Mã: `web/src/screens/duyet-plan/index.tsx:204` và `:210`, `web/src/screens/duyet-plan/diff.ts:49`, `cmd/tpan/print.go:128-138` (`fileAction`). Màn `PlanReview` được dùng ở màn tạo site, SSL và backup của vai `user`.
- Vì sao sai: giao diện không biết `hidden` và `old_link`.
  1. File của máy bị ẩn với vai `user`, ví dụ vhost mặc định cổng 443 khi xin SSL. Nó còn `old_exists: true` nhưng mất `old` và `new`, nên màn hiện "sửa" kèm "Nội dung không đổi.", trong khi bước đó sẽ ghi lại file.
  2. `.htaccess` là symlink: `old` rỗng, `new` là bản chuẩn, nên màn hiện "sửa" với cả file là dòng thêm, không nhắc gì tới symlink. Câu ở mục 6 ý 4 ("bản xem trước hiện đích của link") chỉ đúng trong JSON.
  3. `Hidden = "unverified"` vẫn giữ `new`, nên trông như file hiện tại rỗng.
  4. CLI (`fileAction`) cũng in "sửa" và bỏ qua `OldLink`. Đây đúng là kiểu "giả vờ" mà 3.5 cấm.
- Đề xuất: bỏ ràng buộc "không sửa gì trong `web/`" cho đúng phần này. `FileDiffView` gặp `hidden` thì hiện một dòng "Nội dung không hiện với tài khoản này (file của máy, hoặc chưa xác minh được)"; gặp `old_link` thì hiện "Đang là symlink tới <đích>; bước này thay bằng file thường". CLI in tương tự, và lọc ký tự điều khiển trong `OldLink` trước khi in ra terminal, vì đích symlink do site đặt nên có thể chứa chuỗi escape. Sửa lại mục 6 ý 3 và 4 cho khớp.

### S11 (vừa): Neo là symlink do admin đặt chạy được ở v0.2.0 cho backup theo lịch, nhưng bị C1 từ chối sau khi nâng, và mục 6 không nói tới.
- Ở đâu: mục 3.2 C1 ("Neo là symlink: `ErrSymlink`"), mục 6. Mã: `internal/steps/backup.go:132-142` (`backupPublic` gọi `sys.Stat(<neo>/public)`), `backup.go:270-276` (`tar --directory=<neo>`).
- Vì sao sai, theo thứ tự:
  1. Tên neo nằm trong `/var/www` của root, nên neo là symlink chỉ có thể do admin đặt. Ví dụ admin dời một site sang đĩa khác rồi chạy `ln -s /data/a.example.com /var/www/a.example.com`.
  2. Ở v0.2.0, backup hằng ngày của site đó vẫn chạy: `os.Lstat` chỉ không theo tên cuối `public`, còn `tar --directory` và `du` đi theo neo.
  3. Sau M28, `backupPublic` gặp `ErrSymlink` ở C1, nên backup theo lịch của site đó hỏng mỗi đêm, và phép kiểm health đọc `wp-config.php` ra Unknown.
  4. Mục 6 nói "site đang chạy không đổi gì". (Chạy lại `site add` cho site như vậy thì đã hỏng từ v0.2.0, ở bước `Dir` của neo.)
- Đề xuất: chọn một trong hai và ghi vào 3.2. Cách 1: C1 cho đi theo neo khi `lstat` thấy neo là symlink do root làm chủ, nằm trong một `Root` thuộc root và không ai khác ghi được (người tráo được link đó chỉ có root), rồi mở đích bằng `O_DIRECTORY` và dùng fd đó. Cách 2: giữ từ chối, nhưng ghi vào mục 6 và thêm một phép kiểm health báo trước "neo là symlink, backup của site này sẽ hỏng".

### S12 (nhẹ): Mã HTTP cho lỗi của cửa và khẳng định của e2e A.5 chưa khớp với chính mục 3.5.
- Ở đâu: mục 3.7 đoạn `apiErrorStatus` ("lỗi bọc `*sitefs.Error` thành HTTP 409"), mục 3.2 C2 (ENOTDIR giữ nguyên), mục 7 A.5. Mã: `internal/sitefs/types.go` (hàm `Refused`).
- Vì sao sai:
  1. Nếu 409 áp cho mọi lỗi bọc `*sitefs.Error`, thì một ENOENT bọc trong đó cũng thành `site_path_unsafe` thay vì "không có".
  2. Nếu chỉ áp cho `Refused`, thì `public` là file thường (C2 trả ENOTDIR, không phải lời từ chối) hay EACCES lại ra 500, trái câu "Không còn ra 500". Đây là trạng thái K1 tự tạo được.
  3. A.5 đòi HTTP 409 cho `/dev/zero`, nhưng theo 3.5 một symlink tới `/dev/zero` cho ra `old_link` và HTTP 200. Chỉ FIFO, file 2 GiB và nút thiết bị do root tạo mới ra 409.
- Đề xuất: ghi vào 3.7 một bảng ba dòng. `sitefs.Refused(err)` ra 409 `site_path_unsafe`. Lỗi kernel khác trên đường dẫn trong vùng (ENOTDIR, ELOOP mà không phải symlink, EACCES) cũng ra 409 với mã riêng, hoặc C2 xếp ENOTDIR vào `ErrNotRegular`. ENOENT vẫn là "không có". Sửa A.5: symlink tới `/dev/zero` thì 200 kèm `old_link`; FIFO và file 2 GiB thì 409.

### S13 (nhẹ): `chmodDirs` chưa nói rõ ba điểm cần để tương đương lệnh `find` cũ.
- Ở đâu: mục 3.6 gạch 2. Mã: `internal/steps/site_wp.go:298-301`.
- Vì sao sai: lệnh `find <public> -mindepth 1 -type d` cũ có ba đặc điểm: không đổi quyền chính `public` (`-mindepth 1`), bỏ qua mục biến mất giữa chừng, và không có `-xdev`. Thiết kế chưa nói:
  1. `chmodDirs(ctx, sys, root, perm)` có đổi quyền chính `root` không.
  2. Gặp `ErrSymlink` hay ENOENT (user site vừa tráo) thì bỏ qua, hay làm hỏng cả bước.
  3. Có dừng ở ranh phân vùng như `tar`, `du`, `rm` (đều chạy với `--one-file-system`) không.
- Đề xuất: ghi vào 3.6 ba điều. Không đổi quyền `root`, vì bước `Dir` đã lo. Mục biến mất hay thành symlink giữa lúc liệt kê và lúc `Chmod` thì bỏ qua, vì chỉ hại chính site; lỗi khác thì làm hỏng bước. Không đi xuống thư mục có `st_dev` khác neo; muốn vậy `Info` cần thêm trường `Dev`.

### S14 (nhẹ): Cửa cho root tạo symlink trong vùng site, mà symlink do root làm chủ thì qua được `SymLinksIfOwnerMatch` của Apache và `disable_symlinks if_not_owner` của nginx.
- Ở đâu: mục 3.2 (`Symlink(target, anchor, rel)`, C8). Mã: `internal/steps/common.go:375-378` và `:395-408` (`saved.restore` dựng lại symlink đã chụp bằng `replaceSymlink`), `internal/templates/testdata/site-apache.golden:30`, `site-nginx.golden:17`.
- Vì sao sai, theo thứ tự:
  1. K1 cho `.htaccess` thành symlink tới một file của root.
  2. Một job có bước `.htaccess` chạy rồi bị hoàn tác. `restore` tạo lại đúng symlink đó, nhưng giờ chủ của link là root.
  3. Hai luật chống symlink của web server chỉ so chủ của link với chủ của đích. Link của root trỏ tới một file của root mà `www-data` đọc được (một file cấu hình chẳng hạn) thì được đi theo.
  4. Hôm nay chưa khai thác được, vì `.htaccess` không bao giờ được phục vụ. Nhưng `Symlink` là hàm chung của cửa, dùng được cho mọi đường dẫn trong vùng.
- Đề xuất: sửa C8 ở 3.2. Tạo symlink trong vùng xong thì đổi chủ của chính link về uid, gid của neo bằng `fchownat(..., AT_SYMLINK_NOFOLLOW)`; bị tráo giữa chừng thì chỉ đổi chủ một thứ của chính site. Hoặc cấm `Symlink` trong vùng, trừ đường hoàn tác. Thêm vào e2e khẳng định "không có symlink nào thuộc root trong vùng site sau mọi job".

### S15 (nhẹ): Cả bản thiết kế dựa trên giả định `/var/www` thuộc root và không ai khác ghi được, nhưng không chỗ nào kiểm điều đó.
- Ở đâu: mục 3.1, luật C1. Mã: `internal/steps/files.go:392` (`MkdirAll(path.Dir(l.Dir), 0o755, Root)` chỉ tạo khi thư mục còn thiếu).
- Vì sao sai: nhiều hướng dẫn trên mạng bảo chạy `chown -R www-data:www-data /var/www`. Máy làm theo thì một tiến trình `www-data` (web server bị chiếm) đổi tên được neo của site này thành tên của site kia. C1 vẫn mở được neo, vì nó không phải symlink; luật chủ so với chủ của neo đã bị tráo; và bước `Dir` của neo (nằm ngoài vùng) chạy `os.Chmod` theo đường dẫn.
- Đề xuất: ghi điều kiện này vào 3.1. Cửa, hoặc `NewLocal`, kiểm một lần lúc khởi động rằng `Root` là thư mục thật của root và không có quyền ghi cho nhóm hay người khác. Sai thì từ chối mọi thao tác trong vùng, kèm một phép kiểm health nói cách sửa.

### S16 (nhẹ): Trên `FakeSystem`, neo được tạo ngầm với chủ root, nên luật chủ của C4 bị tắt trong gần như mọi test.
- Ở đâu: mục 3.3 gạch 2 ("neo thuộc `root` thì bỏ, như bản thật"). Mã: `internal/system/fake.go:493-498` (`mkParents` tạo thư mục cha 0755, chủ root), `fake.go:54-60` (`SetFile`).
- Vì sao sai: test của engine, steps, core, server dựng file của site bằng `SetFile("/var/www/<d>/public/.htaccess", ...)`. Neo `/var/www/<d>` vì vậy được tạo ngầm với chủ root, và `ReadFile` bỏ luật chủ. Lớp chặn duy nhất cho hardlink (D4) không được các test đó chạm tới, nên một thay đổi làm hỏng luật chủ vẫn xanh. Câu "test ... thấy đúng hành vi của máy thật" vì vậy chỉ đúng một phần.
- Đề xuất: thêm vào 3.3 hàm test `SetSite(l site.Layout)` dựng neo và `public` với chủ `l.User:www-data`, quyền 2750. Cho `FakeSystem` báo lỗi khi đọc trong một neo do `mkParents` tạo ngầm. Chạy test của B2 trên cả `FakeSystem` với neo đúng chủ.

### S17 (nhẹ): B12 quét theo tên hàm `.Preview(`, nên không chứng minh được câu "mọi handler trả bản xem trước đều qua `writePreview`".
- Ở đâu: mục 4 B12. Mã: `internal/server/api_trash.go:163` (`ApplyPreview` bọc `*engine.Preview`), `internal/server/types_m14.go:40-44` (`HardenPreview` nhúng `engine.Preview`), `internal/server/api_plans.go:116-130` (`GET /plans/{id}` trả `PendingPlan.Preview` đã lưu, không gọi `.Preview(`).
- Vì sao sai: handler nào trả một bản xem trước đã lưu, hay đã dựng sẵn ở chỗ khác (Plan chờ duyệt, kết quả lô), mà không gọi `.Preview(`, thì phép quét không thấy. Hôm nay `GET /plans` chỉ cho admin nên chưa lộ gì. Nếu mai mở cho chủ site, B12 vẫn xanh.
- Đề xuất: đổi B12 sang quét theo kiểu bằng `go/types`. Mọi lời gọi `writeJSON` trong gói `server` có đối số mà kiểu là, hoặc chứa (đệ quy qua trường, con trỏ, slice), `engine.Preview` hay `engine.FileChange` thì test đỏ. Miễn cho bên trong `writePreview` và cho route `adminOnly` lấy từ bảng route.

### S18 (nhẹ): Bảng 3.9 thiếu năm chỗ chạm vùng site qua `sys.*`, trong đó hai chỗ đổi hành vi thấy được.
- Ở đâu: mục 3.9. Mã:
  - `internal/core/stack_switch.go:425-431` (`htaccessIfMissing` gọi `Stat`)
  - `internal/steps/site_wp.go:353` (`WPFiles.Apply` gọi `ReadDir(l.Public)`)
  - `internal/core/site.go:263` (`drift` gọi `pathExists(l.WPConfig)`)
  - `internal/checks/site.go:94` (`siteStat` của health)
  - `internal/steps/backup.go:134` (`backupPublic`)
- Vì sao sai: sau M28 cả năm chỗ đều đi qua cửa nên không lộ gì, nhưng bảng tự nhận là "mọi chỗ" và cột test của chúng để trống. Hai chỗ đổi hành vi thấy được: `htaccessIfMissing` làm hỏng cả Plan chuyển web server (S2), và `backupPublic` làm hỏng backup của site có neo là symlink (S11). Ở v0.2.0, `ReadDir(public)` đi theo symlink, và lời từ chối của `WPFiles` in ra số mục của một thư mục bất kỳ trên máy.
- Đề xuất: thêm năm dòng đó vào bảng, mỗi dòng một test; có thể gộp vào `TestVungSiteCaHaiBan`.

### Để đợt 2

- Hai phát hiện về từ chối phục vụ do mã của site gây ra (mã chạy trong cgroup của tpan; output của lệnh không có trần), có từ trước M28: để đợt 2, chi tiết giữ ngoài repo tới khi vá.

### Đã kiểm, không thấy sai

- Mục 3.1: thùng rác và chỗ khôi phục (`/var/lib/tpan/trash`, `/var/lib/tpan/restore`, 0700 root), thư mục log (root:<nhóm site> 0750), webroot ACME (`/var/lib/tpan/acme`), thư mục tạm của apt (`mktemp -d -p /tmp`): user site không tạo hay đổi tên được mục nào ở đó. Ngoại lệ duy nhất là tiến trình còn giữ fd, đã ghi ở mục 5.
- `sitefs.Split` chạy `path.Clean` trước khi so, và trả false khi root rỗng. Chỉ `NewLocal` dựng `LocalSystem`.
- Mục 2: `go doc os.Root` của Go 1.27.4 đúng như trích. `golang.org/x/sys v0.50.0` có `Openat`, `Fstatat`, `Unlinkat`, `Renameat`, `Mkdirat`, `Symlinkat`, `Readlinkat`, `Fchmod`, `Fchown`, `Fstatfs` cho cả darwin lẫn linux; `Openat2` chỉ có trên linux.
- Grep `FileChange{`: chỉ `SiteHtaccess` đưa đường dẫn trong vùng vào `StepInfo.Files`. `WPConfig`, `WPConfigPerm`, `DBPassword` không khai `wp-config.php`.
- Lệnh root trên vùng site hiện có đúng các loại `tar --create`, `du`, `find -exec chmod` (của `WPFiles`) và `useradd --home-dir`; `rm -r` chỉ chạy trên thùng rác, chỗ khôi phục và thư mục log. Đã grep mọi `system.Cmd` có `l.Dir`, `l.Public`, `l.WPConfig`. Ở mọi lời gọi hiện có trên vùng site, đối số là neo hoặc `<neo>/public`: không thành phần giữa nào do site giữ, còn thành phần cuối thì `tar`, `du` không đi theo nếu là symlink (N3).
- N3 nói "chủ site gọi lại được bước đặt quyền": đúng, nhưng chỉ qua đua sau `drift()` (xem S8).
- Luật C4: thứ tự `fstatat`, rồi `openat(O_NOFOLLOW|O_NONBLOCK)`, rồi `fstat` trên fd, rồi `LimitReader`, chặn được FIFO, thiết bị, symlink và file lớn dần. `O_NONBLOCK` còn làm lease ghi của K1 trả `EWOULDBLOCK` ngay, thay vì bắt root chờ 45 giây.
- Luật C5, C6, C7 trước đua: tráo chỉ thay được bằng thư mục hay file của chính user site, vì đổi tên một thư mục sang cha khác cần quyền ghi trên chính thư mục đó, và hardlink bị `fs.protected_hardlinks` chặn. Dời thư mục đang giữ fd ra ngoài neo không đổi gì, vì cửa tin inode chứ không tin đường dẫn. Root `chown` một file thường thì kernel xoá setuid và setgid, nên C5 không tạo được chương trình setuid root.
- Luồng khôi phục backup (`MoveTree` của `public` qua `RestoreDir`, `afterAside`): K1 tráo `public` giữa chừng chỉ làm hỏng chính site. `rm -r --one-file-system` trên `RestoreDir` không đi theo symlink.
- Phân quyền: `siteCreateOwner` chặn vai `user` chạy lại site của người khác. `/plans` và `/ai/*` chỉ cho admin. Sự kiện job không mang `Old`, `New`. Health bỏ phép kiểm lệch của `.htaccess`.
- Bảng 3.9: T1c, T3, T8b là lỗ thật ở v0.2.0. `os.CreateTemp` và `os.Lchown` đi theo thư mục cha; `os.Chmod` đi theo symlink.
- Mục 3.5: chạy thật nói cùng điều với xem trước. Với symlink, `capture` giữ đích và `WriteFileAtomic` thay link. Với `ErrOwner`, `ErrTooLarge`, thư mục, FIFO, `Apply` hỏng ở `capture`.

### Trả lời của phiên chính (21/11/2026, 10:10)

Không còn điểm nặng: S1 đã sửa trong thiết kế, các điểm vừa và nhẹ dưới đây hoặc đã sửa, hoặc ghi rõ vì sao không làm trong đợt 1.

- **S1, nhận, đã sửa (3.3, 3.6, 3.7, B15, e2e A5).** Đã thử trên máy ảo trước khi sửa: `runuser -u nobody -- sleep 300 &`, `kill -9 <pid của runuser>` thì `sleep` vẫn chạy, cùng nhóm tiến trình với `runuser` (đúng như S1 đoán); `runuser -u nobody -- timeout -k 1 2 dd if=<FIFO> iflag=nonblock,nofollow bs=1048577 count=1 status=none` trả về sau 4 ms, mã 0, không byte nào; trên symlink thì `dd: failed to open ...: Too many levels of symbolic links`, mã 1; trên file thường thì ra đúng nội dung (coreutils 9.4). Ba lớp: đọc lại bằng `dd` không chặn, `timeout` bên trong `runuser`, và `LocalSystem.Run` giết cả nhóm tiến trình khi `ctx` bị huỷ. `ScanHtaccess` (M14, cùng cơ chế) đổi sang `dd iflag=nonblock`.
- **S2, nhận một phần.** Giữ từ chối cả Plan chuyển web server khi `public` của một site là symlink (bỏ site đó khỏi bước `.htaccess` thì kiểm chứng sau khi chuyển vẫn hỏng và cả job vẫn hoàn tác, không giúp được admin). Đã sửa câu sai ở mục 5 ("đã có từ v0.2.0") và thêm khẳng định vào e2e B: lời từ chối nêu tên site, máy không đổi gì.
- **S3, nhận, đã sửa (3.2, B16, e2e D).**
- **S4, S5, nhận, đã sửa (3.4).** `rm` ra khỏi danh sách ngoại lệ (không lời gọi nào cần).
- **S6, nhận phần bọc `Untrusted` và trường mới của `fileText` (3.6).** Cờ "phiên có dữ liệu không tin" để P10 (M29).
- **S7, nhận, đã sửa (B4, B17).**
- **S8, nhận, đã sửa (3.6, bảng N3 ở mục 2, e2e A7).**
- **S9, nhận, đã sửa (3.7: hàm `verifyAsSiteUser` tách riêng; B11: test cần root `TestVungSiteDocLaiBangUserSite`; `make m28-test-vm` thêm gói `server`).**
- **S10, không sửa `web/` trong đợt này.** Luật 4 của `agent-dev/LO-TRINH.md` cấm sửa `web/` trước khi M16 vào `dev`; luật đó do chủ dự án đặt, phiên này không có quyền bỏ. Đã sửa CLI (3.6), ghi giới hạn ở 3.7, mục 5, mục 6 ý 3 và 4, và sẽ ghi việc cho màn duyệt vào mục "Để sau" của KE-HOACH.
- **S11, chọn cách 2 (giữ từ chối), ghi vào mục 6 ý 5 kèm cách thay bằng bind mount.** Không làm phép kiểm health riêng trong đợt 1.
- **S12, nhận, đã sửa (bảng mã lỗi ở 3.7; e2e A5 đã sửa trước khi soát xong, `docs/thay-doi/M28.md` ý 12).**
- **S13, nhận (3.6).** Không thêm `Dev` vào `Info`: lệnh `find` cũ cũng không có `-xdev`, và user của site không mount được gì root đi vào được.
- **S14, nhận (C8, B18).**
- **S15, nhận (3.1, C1, B19).** Hợp đồng thêm `sitefs.ErrRootUnsafe`.
- **S16, nhận phần `SetSite` (3.3).** Không bắt máy giả báo lỗi khi đọc trong neo tạo ngầm: làm vậy đổi hành vi của hàng trăm test có sẵn không liên quan tới mốc này.
- **S17, nhận phần khẳng định "route trả bản xem trước đã lưu là admin" (B12).** Không chuyển sang quét theo kiểu bằng `go/types` (cần nạp kiểu của cả gói; thư viện chuẩn làm được nhưng chậm và mong manh), ghi giới hạn này ở B12.
- **S18, nhận (bảng 3.9, T17 đến T22).**
- **"Để đợt 2"**: hai ý đã ghi vào mục 5.
