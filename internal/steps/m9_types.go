package steps

// Hợp đồng các Step của M9 (mục 6.5, 6.9, 6.10 của docs/KE-HOACH.md). Chỉ có
// chú thích chung; kiểu nằm ở backup.go (thợ backup: BackupArchive,
// BackupExtract, DBImport, đo chỗ trống) và db_m9.go (thợ DB: DBPassword,
// WPConstant), mỗi thợ thêm trường riêng và phương thức vào file của mình. Luật chung ở skill tpan-step.
//
// Luật an toàn chung cho mọi bước đụng vào thư mục site (thư mục do user của
// site kiểm soát, mà tpan chạy bằng root):
//   - Không bao giờ đi theo symlink trong cây của site. Đọc cây bằng GNU tar
//     (không -h: symlink được lưu là symlink; tar mở file theo thư mục cha,
//     không đi theo symlink) hoặc chạy lệnh dưới user của site (AsUser).
//   - Không giải nén đè lên cây đang chạy: giải nén vào site.RestoreDir (root
//     0700, cùng phân vùng), chuẩn hoá chủ và quyền ở đó, rồi tráo bằng rename.
//   - Không lấy uid, gid, mode từ header của tar (--no-same-owner,
//     --no-same-permissions); đặt lại từ site.Layout: tp_<slug>:www-data, thư
//     mục 2750, file 640, wp-config.php 600, không bit setuid/setgid trên file.
//   - Kiểm sha256 của file backup với dòng trong bảng backups trước khi giải nén.
//   - Tar từ chối thành viên tuyệt đối hoặc có ".." (GNU tar mặc định làm vậy;
//     kiểm thêm danh sách thành viên bằng `tar -tzf` trước khi giải nén:
//     chỉ nhận manifest.json, db.sql, và public/...; thiết bị, FIFO bị từ chối).
//
// Lệnh dùng: tar, gzip (gói Essential của Ubuntu, luôn có), sha256sum, du,
// mariadb-dump, mariadb, wp. Mục 6.5 nói "nén bằng thư viện chuẩn của Go":
// M9 dùng GNU tar vì System không có API đọc ghi theo luồng (ReadFile và
// WriteFileAtomic giữ cả file trong bộ nhớ, site vài GB thì không được), và
// tar đọc cây của site mà không đi theo symlink. Chỗ khác đặc tả này ghi ở
// docs/thay-doi/M9.md.
//
// Chặn đầy đĩa (mục 6.5), viết ở backup.go, core gọi lúc dựng Plan để từ
// chối sớm, BackupArchive và MariaDBDump (khi DB khác rỗng) gọi lại lúc Apply:
//
//	// SpaceOK: free ≥ need × 1,2 VÀ free − need ≥ total / 10.
//	func SpaceOK(free, total, need uint64) bool
//	// MeasureDB: cỡ DB của site (information_schema: DATA_LENGTH +
//	// INDEX_LENGTH), 0 khi DB chưa có bảng.
//	func MeasureDB(ctx context.Context, sys system.System, l site.Layout) (uint64, error)
//	// MeasureBackup: need = `du -sb --one-file-system <src>/public` + MeasureDB;
//	// free, total = sys.StatFS(dir), hoặc của thư mục cha gần nhất đã có khi
//	// dir chưa có (lần backup đầu).
//	func MeasureBackup(ctx context.Context, sys system.System, l site.Layout, src, dir string) (need, free, total uint64, err error)
//
// MoveTree (trash.go) hiện chỉ nhận đường dẫn dưới /var/www và thùng rác; thợ
// backup nới cho site.RestoreRoot (khôi phục tráo l.Public với
// RestoreDir/public và RestoreDir/old-public).
