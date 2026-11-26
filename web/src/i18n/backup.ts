// Chuỗi của màn Backup (mục 6.5 của KE-HOACH.md). Giọng chữ: mục 8 của
// docs/GIAO-DIEN.md. "full", "sql", "sha256" và tên lý do máy (manual,
// before-purge...) không dịch: người vận hành thấy chúng cả trong CLI và đường
// dẫn file.

import { defineStrings } from "./index";

const vi = {
  title: "Backup",
  summary: "Bản sao của site và cơ sở dữ liệu, tạo tay hoặc theo lịch.",

  // Backup ngay
  createHeading: "Backup ngay",
  createLabel: "Site",
  createHint: "Chọn một site đang chạy. Bản backup gồm thư mục site và cơ sở dữ liệu. Bạn xem trước các bước rồi mới chạy.",
  createButton: "Backup ngay",
  createNoSitesLead: "Chưa có site đang chạy để backup. ",
  createNoSitesLink: "Tạo site đầu tiên",
  createNoSitesTail: ".",
  createErrorHeading: (d: string) => `Chưa dựng được bản xem trước backup ${d}`,
  overviewFailed: "Chưa đọc được danh sách site đang chạy.",

  // Danh sách
  listHeading: "Các bản backup",
  listHint: "full là cả site (thư mục và cơ sở dữ liệu), sql là bản xuất riêng cơ sở dữ liệu. Bản của site đã xoá hẳn vẫn giữ ở đây.",
  filterLabel: "Lọc theo site",
  filterAll: "Mọi site",
  tableName: "Các bản backup",
  colDomain: "Site",
  colKind: "Loại",
  colReason: "Lý do",
  colCreated: "Tạo lúc",
  colSize: "Cỡ",
  colWhere: "Ở đâu",
  colAction: "Việc",
  // Nơi bản nằm (máy chủ tính, core.BackupLocation). push_failed không có title vì nó là dòng trạng thái kèm câu lỗi;
  // title của other_target nằm ở otherTitles, otherLocalTitles bên dưới vì nó đổi theo việc đã đặt đích chưa.
  locations: { machine: "máy", remote: "kho", both: "máy và kho", other_target: "kho khác", push_failed: "đẩy lỗi" },
  locTitles: {
    machine: "Bản chỉ có trên máy này.",
    remote: "Bản chỉ còn trên kho. Tải về máy thì mới khôi phục được.",
    both: "Bản có cả trên máy này lẫn trên kho.",
  },
  // Title của dòng "kho khác" theo việc đã đặt đích chưa: set (đã đặt), unset (chưa
  // đặt), unknown (user thường, không đọc được đích). otherTitles: file không còn
  // trên máy; otherLocalTitles: file còn trên máy (nhãn hiện "máy").
  otherTitles: {
    set: "Bản nằm ở một kho khác với kho đang đặt. Đặt lại kho đó thì mới dùng được.",
    unset: "Bản nằm ở một kho mà máy chưa đặt làm đích. Đặt lại kho đó thì mới dùng được.",
    unknown: "Bản nằm ở một kho mà máy hiện không dùng. Đặt lại kho đó thì mới dùng được.",
  },
  otherLocalTitles: {
    set: "Bản có trên máy này; bản trên kho nằm ở một kho khác với kho đang đặt.",
    unset: "Bản có trên máy này; bản trên kho nằm ở một kho mà máy chưa đặt làm đích.",
    unknown: "Bản có trên máy này; bản trên kho nằm ở một kho mà máy hiện không dùng.",
  },
  // Việc với kho
  push: "Đẩy lên kho",
  pushFor: (d: string, when: string) => `Đẩy lên kho bản của ${d} lúc ${when}`,
  pushWarning: (d: string, when: string) => `Đẩy bản của ${d} lúc ${when} lên kho. Bản trên máy giữ nguyên.`,
  pushErrorHeading: (d: string) => `Chưa dựng được bản xem trước đẩy lên kho ${d}`,
  pull: "Tải về máy",
  pullFor: (d: string, when: string) => `Tải về máy bản của ${d} lúc ${when}`,
  pullWarning: (d: string, when: string) => `Tải bản của ${d} lúc ${when} từ kho về máy. Site chưa bị đổi gì.`,
  pullErrorHeading: (d: string) => `Chưa dựng được bản xem trước tải về ${d}`,
  queued: "Đã nhận việc. Xem kết quả ở Lịch sử.",
  // Tải về chưa có job: một lượt đẩy đang giữ khoá kho (tối đa 30 phút, core.RemoteLockWait).
  pullWaiting: "Đang chờ lượt đẩy lên kho khác xong, có thể tới 30 phút. Không thấy job tải về ở Lịch sử sau đó thì bấm lại.",
  // Khôi phục một bản chỉ còn trên kho
  pullFirstTitle: "Tải về máy trước khi khôi phục?",
  pullFirstBody: "Bản này chỉ còn trên kho. Tải về máy trước (một job), rồi mới khôi phục được.",
  askAdminToPull: "Bản này chỉ còn trên kho; nhờ admin tải về máy trước.",
  // Khôi phục một bản chỉ còn ở một kho khác (sau khi gỡ hay đổi đích)
  otherTargetSetAgain: "Bản này nằm ở một kho khác. Đặt lại đích đó ở thẻ Đích ngoài máy để tải về.",
  otherTargetAskAdmin: "Bản này nằm ở một kho khác; nhờ admin đặt lại đích đó và tải về máy trước.",
  pulled: "Đã tải về. Bấm Khôi phục để xem trước việc khôi phục.",
  kindFull: "Thư mục site và cơ sở dữ liệu",
  kindSql: "Chỉ cơ sở dữ liệu",
  reasons: {
    manual: "Bấm tay",
    schedule: "Theo lịch",
    "before-restore": "Tự backup trước khi khôi phục",
    "before-purge": "Tự backup trước khi xoá hẳn",
    "before-import": "Tự xuất trước khi nhập DB",
  },
  siteGone: "site đã xoá",
  restore: "Khôi phục",
  restoreFor: (d: string, when: string) => `Khôi phục ${d} từ bản lúc ${when}`,
  previewing: "Đang dựng bản xem trước…",
  trashedLead: "Site đang nằm trong thùng rác. Khôi phục nó khỏi ",
  trashedLink: "thùng rác",
  trashedTail: " trước.",
  goneNote: "Site đã xoá hẳn. Bản này vẫn dùng được: TPAN tạo lại site rồi khôi phục mã nguồn và DB.",
  restoreRecreate: "Tạo lại site",
  restoreRecreateFor: (d: string, when: string) => `Tạo lại site ${d} và khôi phục bản backup lúc ${when}`,
  restoreRecreateWarning: (d: string, when: string) =>
    `Site ${d} đã xoá hẳn. TPAN sẽ tạo lại site này (WordPress mới, mật khẩu DB mới) rồi đưa mã nguồn và DB của bản backup lúc ${when} vào. Tên miền phụ, chứng chỉ SSL và bản PHP riêng của site cũ không tự có lại.`,
  sqlLead: "Nhập ở màn ",
  sqlLink: "Cơ sở dữ liệu",
  sqlTail: ".",
  emptyAll: "Chưa có bản backup nào. Chọn site ở thẻ Backup ngay phía trên, hoặc đặt giờ backup hằng ngày ở thẻ Lịch và số bản giữ bên dưới.",
  emptyFor: (d: string) => `Chưa có bản backup nào của ${d}.`,
  restoreWarning: (d: string, when: string) =>
    `Khôi phục ${d} ghi đè file và cơ sở dữ liệu hiện tại bằng bản lúc ${when}. TPAN tự backup hiện trạng trước, nên bạn quay lại được.`,
  restoreErrorHeading: (d: string) => `Chưa dựng được bản xem trước khôi phục ${d}`,
  download: "Tải về máy tính",
  copySha: "Chép mã sha256",
  copyPath: "Chép đường dẫn file",
  deleteLocal: "Xoá bản này",
  moreFor: (d: string, when: string) => `Việc khác với bản backup của ${d} lúc ${when}: tải về máy tính, chép mã sha256, chép đường dẫn, xoá`,
  deleteWarning: (d: string, when: string, stored: boolean) =>
    stored
      ? `Xoá khỏi máy bản backup của ${d} lúc ${when}. Bản đã đẩy lên kho vẫn còn và tải về lại được. File trên máy xoá rồi không lấy lại được.`
      : `Xoá bản backup của ${d} lúc ${when}. Đây là bản duy nhất của nó: xoá rồi không lấy lại được.`,
  deleteErrorHeading: (d: string) => `Chưa dựng được bản xem trước xoá backup của ${d}`,

  // Lịch và số bản giữ
  settingsHeading: "Lịch và số bản giữ",
  keepLabel: "Số bản giữ cho mỗi site",
  keepHint: "Từ 1 đến 30. Bản mới đẩy bản cũ nhất ra. Bản tự backup trước khi xoá hẳn site không bao giờ bị dọn.",
  keepInvalid: "Nhập số nguyên từ 1 đến 30.",
  atLabel: "Giờ backup hằng ngày",
  atHint: "Theo giờ của máy chủ. Để trống là tắt backup theo lịch. Lịch ghi vào một file cron trên máy, nên có bước xem trước khi lưu.",
  atClear: "Bỏ giờ (tắt lịch)",
  current: (keep: number, at: string) => (at ? `Đang giữ ${keep} bản, backup lúc ${at} mỗi ngày.` : `Đang giữ ${keep} bản, lịch backup đang tắt.`),
  settingsPreview: "Xem trước thay đổi",
  settingsErrorHeading: "Chưa dựng được bản xem trước lịch backup",

  checksHeading: "Điều kiện chưa đạt",
};

export const strings = defineStrings(vi, {
  title: "Backups",
  summary: "Copies of sites and their databases, made by hand or on a schedule.",

  createHeading: "Back up now",
  createLabel: "Site",
  createHint: "Pick a running site. The backup holds the site folder and its database. You see the steps first and run them after.",
  createButton: "Back up now",
  createNoSitesLead: "There is no running site to back up yet. ",
  createNoSitesLink: "Create the first site",
  createNoSitesTail: ".",
  createErrorHeading: (d) => `Could not build the backup preview for ${d}`,
  overviewFailed: "Could not read the list of running sites.",

  listHeading: "Backups",
  listHint: "full is the whole site (folder and database), sql is a separate export of the database. Backups of permanently deleted sites stay here.",
  filterLabel: "Filter by site",
  filterAll: "All sites",
  tableName: "Backups",
  colDomain: "Site",
  colKind: "Kind",
  colReason: "Reason",
  colCreated: "Created",
  colSize: "Size",
  colWhere: "Where",
  colAction: "Action",
  locations: { machine: "server", remote: "storage", both: "server and storage", other_target: "other storage", push_failed: "upload failed" },
  locTitles: {
    machine: "This backup is only on this server.",
    remote: "This backup is only in storage. Download it to this server before you can restore it.",
    both: "This backup is on this server and in storage.",
  },
  otherTitles: {
    set: "This backup is in a different storage than the one set now. Set that storage again to use it.",
    unset: "This backup is in a storage that is not set as the target on this server. Set that storage again to use it.",
    unknown: "This backup is in a storage this server does not use now. Set that storage again to use it.",
  },
  otherLocalTitles: {
    set: "This backup is on this server; the copy in storage is in a different storage than the one set now.",
    unset: "This backup is on this server; the copy in storage is in a storage that is not set as the target on this server.",
    unknown: "This backup is on this server; the copy in storage is in a storage this server does not use now.",
  },
  push: "Upload to storage",
  pushFor: (d, when) => `Upload to storage the backup of ${d} from ${when}`,
  pushWarning: (d, when) => `Uploads the backup of ${d} from ${when} to storage. The copy on this server stays.`,
  pushErrorHeading: (d) => `Could not build the upload preview for ${d}`,
  pull: "Download to this server",
  pullFor: (d, when) => `Download to this server the backup of ${d} from ${when}`,
  pullWarning: (d, when) => `Downloads the backup of ${d} from ${when} from storage to this server. The site is not changed.`,
  pullErrorHeading: (d) => `Could not build the download preview for ${d}`,
  queued: "Request received. See the result in History.",
  pullWaiting: "Waiting for another upload to storage to finish, up to 30 minutes. If no download job shows in History after that, press again.",
  pullFirstTitle: "Download to this server before restoring?",
  pullFirstBody: "This backup is only in storage. Download it to this server first (one job), then you can restore it.",
  askAdminToPull: "This backup is only in storage; ask an admin to download it to this server first.",
  otherTargetSetAgain: "This backup is in a different storage. Set that target again in the Off-site target card to download it.",
  otherTargetAskAdmin: "This backup is in a different storage; ask an admin to set that target again and download it to this server first.",
  pulled: "Downloaded. Press Restore to preview the restore.",
  kindFull: "Site folder and database",
  kindSql: "Database only",
  reasons: {
    manual: "By hand",
    schedule: "On schedule",
    "before-restore": "Automatic, before a restore",
    "before-purge": "Automatic, before permanent deletion",
    "before-import": "Automatic export, before a DB import",
  },
  siteGone: "site deleted",
  restore: "Restore",
  restoreFor: (d, when) => `Restore ${d} from the backup of ${when}`,
  previewing: "Building the preview…",
  trashedLead: "The site is in the trash. Restore it from the ",
  trashedLink: "trash",
  trashedTail: " first.",
  goneNote: "The site was deleted for good. This backup still works: TPAN creates the site again, then restores its code and database.",
  restoreRecreate: "Recreate site",
  restoreRecreateFor: (d, when) => `Recreate site ${d} and restore the backup from ${when}`,
  restoreRecreateWarning: (d, when) =>
    `Site ${d} was deleted for good. TPAN will create it again (fresh WordPress, new database password), then put the code and database of the backup from ${when} in. The old site's alias domains, SSL certificate and own PHP version do not come back by themselves.`,
  sqlLead: "Import it on the ",
  sqlLink: "Databases",
  sqlTail: " screen.",
  emptyAll: "There are no backups yet. Pick a site in the Back up now card above, or set a daily time in the Schedule and copies kept card below.",
  emptyFor: (d) => `There are no backups of ${d} yet.`,
  restoreWarning: (d, when) =>
    `Restoring ${d} overwrites its current files and database with the backup of ${when}. TPAN backs up the current state first, so you can go back.`,
  restoreErrorHeading: (d) => `Could not build the restore preview for ${d}`,
  download: "Download to this computer",
  copySha: "Copy the sha256",
  copyPath: "Copy the file path",
  deleteLocal: "Delete this backup",
  moreFor: (d, when) => `More for the backup of ${d} from ${when}: download, copy the sha256, copy the path, delete`,
  deleteWarning: (d, when, stored) =>
    stored
      ? `Deletes the backup of ${d} from ${when} from this server. The copy pushed to storage stays and can be fetched again. The file on this server cannot be brought back.`
      : `Deletes the backup of ${d} from ${when}. This is its only copy: once deleted it cannot be brought back.`,
  deleteErrorHeading: (d) => `Could not build the preview to delete the backup of ${d}`,

  settingsHeading: "Schedule and copies kept",
  keepLabel: "Copies kept per site",
  keepHint: "From 1 to 30. A new copy pushes out the oldest. The automatic backup taken before a permanent deletion is never cleared.",
  keepInvalid: "Enter a whole number from 1 to 30.",
  atLabel: "Daily backup time",
  atHint: "In the server's time. Leave it empty to turn the scheduled backup off. The schedule is written as a cron file on the machine, so you see a preview before saving.",
  atClear: "Clear the time (turn off)",
  current: (keep, at) => {
    const copies = `${keep} ${keep === 1 ? "copy" : "copies"}`;
    return at ? `Keeping ${copies}, backing up at ${at} every day.` : `Keeping ${copies}, scheduled backup is off.`;
  },
  settingsPreview: "Preview the change",
  settingsErrorHeading: "Could not build the backup schedule preview",

  checksHeading: "Conditions not met",
});
