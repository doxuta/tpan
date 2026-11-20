// Chuỗi của màn Cơ sở dữ liệu. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.
// "sql", "sha256", "wp-config.php" và "MariaDB" giữ nguyên ở cả hai bản.

import { defineStrings } from "./index";

const vi = {
  title: "Cơ sở dữ liệu",
  summary: "Mỗi site có cơ sở dữ liệu và user riêng. DB rời là DB không gắn với site nào.",
  tableName: "Các cơ sở dữ liệu",

  colDb: "DB",
  colUser: "User",
  colDbUser: "DB và user",
  sameUser: "user cùng tên",
  colSite: "Site",
  colSize: "Cỡ",
  colTables: "Số bảng",
  colAction: "Việc",
  unreadable: "không đọc được",

  emptyText: "Chưa có cơ sở dữ liệu nào. DB của site có khi tạo site; DB rời tạo bằng nút Tạo DB.",
  emptyLink: "Tạo site đầu tiên",

  hint: "Đổi mật khẩu: TPAN sinh mật khẩu mới, đổi trong MariaDB và ghi vào wp-config.php. Xuất .sql: ghi một file .sql vào thư mục backup của site, xem ở màn Backup. Nhập: đưa một bản đã xuất vào lại cơ sở dữ liệu. Bấm là xem trước từng bước, chưa có gì đổi.",

  password: "Đổi mật khẩu",
  passwordFor: (d: string) => `Đổi mật khẩu cơ sở dữ liệu của ${d}`,
  dump: "Xuất .sql",
  dumpFor: (d: string) => `Xuất cơ sở dữ liệu của ${d} ra file .sql`,
  importOpen: "Nhập từ bản đã xuất",
  importFor: (d: string) => `Nhập vào cơ sở dữ liệu của ${d} từ một bản đã xuất`,
  previewing: "Đang dựng bản xem trước…",

  trashedLead: "Site đang trong thùng rác nên chỉ xuất được. Khôi phục nó khỏi ",
  trashedLink: "thùng rác",
  trashedTail: " để đổi mật khẩu hay nhập.",

  // Chọn bản đã xuất để nhập
  importHeading: (d: string) => `Nhập vào cơ sở dữ liệu của ${d}`,
  importHint: "Chỉ nhận bản TPAN đã xuất (TPAN kiểm sha256 trước khi nhập). TPAN xuất bản hiện tại trước, nên nhập nhầm vẫn quay lại được.",
  importNone: (d: string) => `Chưa có bản xuất nào của ${d}. Bấm Xuất .sql ở hàng của site trước, rồi nhập lại từ bản đó.`,
  importLabel: "Bản đã xuất",
  importFile: "File:",
  importPreview: "Xem trước nhập",

  // Lời đặt trên bản xem trước
  passwordWarning: (db: string, d: string) =>
    `Mật khẩu mới của ${db} không hiện ra ở đâu. TPAN sinh nó, ghi vào wp-config.php của ${d} và không in ra màn hình hay log; cần xem thì mở file đó trên máy. Site có thể chậm hoặc báo lỗi vài giây lúc đổi. Bước nào hỏng thì TPAN đặt lại mật khẩu cũ.`,
  dumpWarning: (db: string) => `Xuất ${db} ra một file .sql trong thư mục backup của site. Site vẫn chạy bình thường. Đĩa gần đầy thì TPAN từ chối và nói rõ lý do.`,
  importWarning: (d: string, when: string) =>
    `Nhập ghi đè cơ sở dữ liệu của ${d} bằng bản xuất lúc ${when}. TPAN tự xuất bản hiện tại trước, nên bạn quay lại được bằng cách nhập lại bản đó.`,

  passwordErrorHeading: (d: string) => `Chưa dựng được bản xem trước đổi mật khẩu của ${d}`,
  dumpErrorHeading: (d: string) => `Chưa dựng được bản xem trước xuất cơ sở dữ liệu của ${d}`,
  importErrorHeading: (d: string) => `Chưa dựng được bản xem trước nhập vào cơ sở dữ liệu của ${d}`,
  checksHeading: "Điều kiện chưa đạt",

  // ---- M13: DB rời, bộ ký tự, tạm khoá (mục 6.12) ----
  listHeading: (n: number) => `${n} cơ sở dữ liệu`,
  createButton: "Tạo DB",
  none: "—",
  colType: "Loại",
  colCharset: "Bộ ký tự",
  kindSite: "DB của site",
  kindStandalone: "DB rời",
  noteLabel: "Ghi chú",
  hintM13:
    "DB rời không gắn với site nào: tên là <user>_<tên>, mật khẩu hiện một lần sau khi tạo. Xoá DB rời là đưa vào thùng rác (xuất file .sql trước), khôi phục được tới hạn xoá hẳn. Tạm khoá DB khoá user DB và ngắt các kết nối đang mở; dữ liệu giữ nguyên.",

  edit: "Sửa",
  editFor: (db: string) => `Sửa ${db}`,
  charset: "Đổi charset",
  charsetFor: (d: string) => `Đổi bộ ký tự của cơ sở dữ liệu của ${d}`,
  passwordForDb: (db: string) => `Đổi mật khẩu của ${db}`,
  suspend: "Tạm khoá",
  unsuspend: "Mở khoá",
  suspendFor: (db: string) => `Tạm khoá ${db}`,
  unsuspendFor: (db: string) => `Mở khoá ${db}`,
  trash: "Xoá",
  trashFor: (db: string) => `Đưa ${db} vào thùng rác`,
  importBlocked: "Site đang tạm khoá nên chưa nhập được. Mở khoá site trước.",
  inTrashDb: "Đang trong thùng rác.",

  // Hộp Tạo DB
  createTitle: "Tạo DB rời",
  createDescription: "DB rời không gắn với site nào. Xem trước từng bước rồi mới tạo.",
  createPreview: "Xem trước",
  createOwnerLabel: "Chủ",
  createOwnerSelf: (name: string) => `${name} (bạn)`,
  createOwnerHint: "DB tính vào gói của user này.",
  createNameLabel: "Tên DB",
  createNameHint: (prefix: string) => `Phần sau “${prefix}”. Chỉ chữ thường a–z, số và dấu gạch dưới.`,
  createFullName: "Tên đầy đủ:",
  createFullNameLength: (n: number) => `${n}/64 ký tự`,
  createNameRequired: "Nhập tên DB, ví dụ shop.",
  createNoteHint: "Một dòng cho bạn nhớ DB này dùng làm gì. Tối đa 500 byte.",
  createWarning: (full: string) => `Tạo DB ${full} cùng một user DB cùng tên. TPAN sinh mật khẩu và hiện đúng một lần ở trang job sau khi tạo xong: chép ngay, TPAN không giữ.`,
  createErrorHeading: "Chưa dựng được bản xem trước tạo DB",
  collationsLoading: "Đang đọc danh sách bộ ký tự…",
  collationsFailed: "Chưa đọc được danh sách bộ ký tự.",

  // Chọn bộ ký tự
  charsetLabel: "Bộ ký tự (charset)",
  collationLabel: "Collation",
  collationDefault: " (mặc định)",

  // Khối Sửa và Đổi charset
  editHeading: (db: string) => `Sửa ${db}`,
  charsetHeading: (d: string) => `Đổi bộ ký tự của cơ sở dữ liệu của ${d}`,
  charsetIntro: "Chỉ đổi bộ ký tự mặc định của DB: bảng tạo sau theo bộ mới, bảng đã có giữ nguyên bộ cũ. Bấm là xem trước, chưa có gì đổi.",
  charsetPreview: "Xem trước đổi bộ ký tự",
  charsetSame: "Đang dùng cặp này. Chọn cặp khác để đổi.",
  noteHeading: "Ghi chú",
  noteSave: "Lưu ghi chú",
  noteSaving: "Đang lưu…",
  noteSaved: "Đã lưu ghi chú.",
  noteFailed: "Chưa lưu được ghi chú",
  noteTooLong: "Ghi chú dài quá 500 byte. Rút ngắn rồi lưu lại.",
  noteIntro: "Ghi chú lưu ngay, không qua xem trước: nó chỉ là chữ cho bạn nhớ, không đổi gì trên máy.",
  editClose: "Đóng",

  // Lời đặt trên bản xem trước
  passwordDbWarning: (db: string) =>
    `Đổi mật khẩu ${db}: mật khẩu mới hiện đúng một lần ở trang job sau khi chạy xong, chép ngay vì TPAN không giữ. Ứng dụng đang dùng mật khẩu cũ mất kết nối tới khi bạn đổi theo. Không hoàn tác được.`,
  suspendDbWarning: (db: string) =>
    `Tạm khoá ${db}: user DB bị khoá và mọi kết nối đang mở bị ngắt. Ứng dụng dùng DB này báo lỗi kết nối tới khi mở khoá. Dữ liệu giữ nguyên.`,
  suspendSiteDbWarning: (d: string, db: string) =>
    `Tạm khoá ${db}, DB của ${d}: WordPress không vào được DB, nên site báo lỗi kết nối cơ sở dữ liệu cho khách tới khi mở khoá. Muốn khách thấy trang “Site đang tạm khoá” thì tạm khoá cả site ở màn Site.`,
  unsuspendDbWarning: (db: string) => `Mở khoá ${db}: user DB dùng lại được, ứng dụng kết nối lại.`,
  trashDbWarning: (db: string) => `Đưa ${db} vào thùng rác: TPAN xuất file .sql rồi mới xoá DB. Khôi phục được ở màn Thùng rác cho tới hạn xoá hẳn.`,
  charsetWarning: (db: string, cs: string, co: string) => `Đổi bộ ký tự mặc định của ${db} sang ${cs} / ${co}. Chỉ bảng tạo sau theo bộ mới; bảng đã có giữ nguyên.`,

  passwordDbErrorHeading: (db: string) => `Chưa dựng được bản xem trước đổi mật khẩu của ${db}`,
  suspendErrorHeading: (db: string) => `Chưa dựng được bản xem trước tạm khoá ${db}`,
  unsuspendErrorHeading: (db: string) => `Chưa dựng được bản xem trước mở khoá ${db}`,
  trashErrorHeading: (db: string) => `Chưa dựng được bản xem trước đưa ${db} vào thùng rác`,
  charsetErrorHeading: (db: string) => `Chưa dựng được bản xem trước đổi bộ ký tự của ${db}`,

  // Mật khẩu DB hiện một lần (trang job)
  secretLabel: "MẬT KHẨU CƠ SỞ DỮ LIỆU",
  secretTitle: (db: string) => `Lưu mật khẩu của ${db} ngay`,
  secretBody: "TPAN không giữ mật khẩu này và sẽ không hiện lại. Chép vào trình quản lý mật khẩu trước khi rời trang.",
  secretDb: "DB",
  secretUser: "User",
  secretPassword: "Mật khẩu",
  secretCopy: "Chép",
  secretCopyOf: (what: string) => `Chép ${what}`,
  secretDismiss: "Đã lưu, ẩn đi",
};

export const strings = defineStrings(vi, {
  title: "Databases",
  summary: "Each site has its own database and user. A standalone database belongs to no site.",
  tableName: "Databases",

  colDb: "DB",
  colUser: "User",
  colDbUser: "DB and user",
  sameUser: "user has the same name",
  colSite: "Site",
  colSize: "Size",
  colTables: "Tables",
  colAction: "Action",
  unreadable: "cannot be read",

  emptyText: "No databases yet. A site database appears when you create a site; a standalone one with the Create DB button.",
  emptyLink: "Create the first site",

  hint: "Change password: TPAN generates a new password, sets it in MariaDB and writes it to wp-config.php. Export .sql: writes a .sql file to the site's backup folder, see it on the Backups screen. Import: puts an exported copy back into the database. Pressing a button shows the steps first, nothing changes yet.",

  password: "Change password",
  passwordFor: (d) => `Change the database password of ${d}`,
  dump: "Export .sql",
  dumpFor: (d) => `Export the database of ${d} to a .sql file`,
  importOpen: "Import an export",
  importFor: (d) => `Import an exported copy into the database of ${d}`,
  previewing: "Building the preview…",

  trashedLead: "The site is in the trash, so it can only be exported. Restore it from the ",
  trashedLink: "trash",
  trashedTail: " to change the password or import.",

  importHeading: (d) => `Import into the database of ${d}`,
  importHint: "Only accepts exports TPAN made itself (TPAN checks the sha256 before importing). TPAN exports the current database first, so a wrong import can be undone.",
  importNone: (d) => `There is no export of ${d} yet. Press Export .sql on the site's row first, then import from it.`,
  importLabel: "Exported copy",
  importFile: "File:",
  importPreview: "Preview the import",

  passwordWarning: (db, d) =>
    `The new password of ${db} is shown nowhere. TPAN generates it, writes it to wp-config.php of ${d} and never prints it on screen or in a log; to read it, open that file on the machine. The site may be slow or show an error for a few seconds during the change. If a step fails, TPAN sets the old password back.`,
  dumpWarning: (db) => `Exports ${db} to a .sql file in the site's backup folder. The site keeps running. If the disk is nearly full, TPAN refuses and says why.`,
  importWarning: (d, when) =>
    `Importing overwrites the database of ${d} with the export of ${when}. TPAN exports the current database first, so you can go back by importing that copy.`,

  passwordErrorHeading: (d) => `Could not build the password change preview for ${d}`,
  dumpErrorHeading: (d) => `Could not build the export preview for ${d}`,
  importErrorHeading: (d) => `Could not build the import preview for ${d}`,
  checksHeading: "Conditions not met",

  listHeading: (n) => `${n} ${n === 1 ? "database" : "databases"}`,
  createButton: "Create DB",
  none: "—",
  colType: "Type",
  colCharset: "Charset",
  kindSite: "Site database",
  kindStandalone: "Standalone DB",
  noteLabel: "Note",
  hintM13:
    "A standalone DB belongs to no site: it is named <user>_<name> and its password shows once after creation. Deleting a standalone DB moves it to the trash (a .sql export first) and it can be restored until it is permanently deleted. Suspending a DB locks the DB user and drops open connections; the data stays.",

  edit: "Edit",
  editFor: (db) => `Edit ${db}`,
  charset: "Change charset",
  charsetFor: (d) => `Change the charset of the database of ${d}`,
  passwordForDb: (db) => `Change the password of ${db}`,
  suspend: "Suspend",
  unsuspend: "Unsuspend",
  suspendFor: (db) => `Suspend ${db}`,
  unsuspendFor: (db) => `Unsuspend ${db}`,
  trash: "Delete",
  trashFor: (db) => `Move ${db} to the trash`,
  importBlocked: "The site is suspended, so it cannot import yet. Unsuspend the site first.",
  inTrashDb: "It is in the trash.",

  createTitle: "Create a standalone DB",
  createDescription: "A standalone DB belongs to no site. You see the steps before anything is created.",
  createPreview: "Preview",
  createOwnerLabel: "Owner",
  createOwnerSelf: (name) => `${name} (you)`,
  createOwnerHint: "The DB counts against this user's package.",
  createNameLabel: "DB name",
  createNameHint: (prefix) => `The part after “${prefix}”. Lowercase a–z, digits and underscores only.`,
  createFullName: "Full name:",
  createFullNameLength: (n) => `${n}/64 characters`,
  createNameRequired: "Enter a DB name, for example shop.",
  createNoteHint: "One line to remember what this DB is for. Up to 500 bytes.",
  createWarning: (full) => `Creates the DB ${full} and a DB user with the same name. TPAN generates the password and shows it once on the job page when it is done: copy it right away, TPAN does not keep it.`,
  createErrorHeading: "Could not build the create DB preview",
  collationsLoading: "Reading the list of charsets…",
  collationsFailed: "Could not read the list of charsets.",

  charsetLabel: "Charset",
  collationLabel: "Collation",
  collationDefault: " (default)",

  editHeading: (db) => `Edit ${db}`,
  charsetHeading: (d) => `Change the charset of the database of ${d}`,
  charsetIntro: "This only changes the DB's default charset: tables created later use the new one, existing tables keep the old one. Pressing the button shows the steps first, nothing changes yet.",
  charsetPreview: "Preview the charset change",
  charsetSame: "It already uses this pair. Pick another to change it.",
  noteHeading: "Note",
  noteSave: "Save note",
  noteSaving: "Saving…",
  noteSaved: "Note saved.",
  noteFailed: "Could not save the note",
  noteTooLong: "The note is over 500 bytes. Shorten it and save again.",
  noteIntro: "A note saves at once, without a preview: it is only text to help you remember and changes nothing on the machine.",
  editClose: "Close",

  passwordDbWarning: (db) =>
    `Changing the password of ${db}: the new password shows once on the job page when it is done, copy it right away because TPAN does not keep it. An app using the old password loses its connection until you update it. It cannot be undone.`,
  suspendDbWarning: (db) =>
    `Suspending ${db}: the DB user is locked and every open connection is dropped. An app using this DB shows a connection error until you unsuspend. The data stays.`,
  suspendSiteDbWarning: (d, db) =>
    `Suspending ${db}, the database of ${d}: WordPress cannot reach the database, so visitors see a database connection error until you unsuspend. To show visitors the “Site suspended” page instead, suspend the whole site on the Sites screen.`,
  unsuspendDbWarning: (db) => `Unsuspending ${db}: the DB user works again and apps reconnect.`,
  trashDbWarning: (db) => `Moving ${db} to the trash: TPAN exports a .sql file first, then deletes the DB. You can restore it on the Trash screen until it is permanently deleted.`,
  charsetWarning: (db, cs, co) => `Changing the default charset of ${db} to ${cs} / ${co}. Only tables created later use it; existing tables stay as they are.`,

  passwordDbErrorHeading: (db) => `Could not build the password change preview for ${db}`,
  suspendErrorHeading: (db) => `Could not build the suspend preview for ${db}`,
  unsuspendErrorHeading: (db) => `Could not build the unsuspend preview for ${db}`,
  trashErrorHeading: (db) => `Could not build the preview to move ${db} to the trash`,
  charsetErrorHeading: (db) => `Could not build the charset change preview for ${db}`,

  secretLabel: "DATABASE PASSWORD",
  secretTitle: (db) => `Save the password of ${db} now`,
  secretBody: "TPAN does not keep this password and will not show it again. Copy it into your password manager before you leave this page.",
  secretDb: "DB",
  secretUser: "User",
  secretPassword: "Password",
  secretCopy: "Copy",
  secretCopyOf: (what) => `Copy ${what}`,
  secretDismiss: "Saved, hide it",
});
