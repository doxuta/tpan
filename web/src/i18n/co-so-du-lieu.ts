// Chuỗi của màn Cơ sở dữ liệu. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.
// "sql", "sha256", "wp-config.php" và "MariaDB" giữ nguyên ở cả hai bản.

import { defineStrings } from "./index";

const vi = {
  title: "Cơ sở dữ liệu",
  summary: "Mỗi site có cơ sở dữ liệu và user riêng.",
  tableName: "Các cơ sở dữ liệu",

  colDb: "DB",
  colUser: "User",
  colSite: "Site",
  colSize: "Dung lượng",
  colTables: "Số bảng",
  colAction: "Việc",
  unreadable: "không đọc được",

  emptyText: "Chưa có cơ sở dữ liệu nào vì chưa có site.",
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
};

export const strings = defineStrings(vi, {
  title: "Databases",
  summary: "Each site has its own database and user.",
  tableName: "Databases",

  colDb: "DB",
  colUser: "User",
  colSite: "Site",
  colSize: "Size",
  colTables: "Tables",
  colAction: "Action",
  unreadable: "cannot be read",

  emptyText: "No databases yet because there are no sites.",
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
});
