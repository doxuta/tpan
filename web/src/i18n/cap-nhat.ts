// Chuỗi của màn Cập nhật (mục 6.11 và mục 10 của KE-HOACH.md). Giọng chữ: mục 8
// của docs/GIAO-DIEN.md. Dấu trạng thái, bằng chứng, nguyên nhân và sửa tay của
// dòng "bản vá tự động" dùng chuỗi của màn Bảo mật (CheckRow), nên không lặp ở đây.

import { defineStrings } from "./index";

const vi = {
  title: "Cập nhật",
  summary: "Gói của stack có bản mới, bản vá bảo mật tự động, nâng MariaDB và cập nhật chính TPAN.",
  previewing: "Đang dựng bản xem trước…",
  previewHint: "Bấm là xem trước, chưa có gì đổi.",
  previewError: "Chưa dựng được bản xem trước",
  previewErrorLine: (line: string) => `Chưa dựng được bản xem trước để nâng lên ${line}`,
  checksHeading: "Điều kiện chưa đạt",
  noValue: "—",

  // (a) Gói của stack
  packagesHeading: "Gói của stack",
  packagesTable: "Gói của stack có bản mới",
  colPackage: "Gói",
  colFrom: "Từ bản",
  colTo: "Lên bản",
  colKind: "Loại",
  securityTag: "bản vá bảo mật",
  packagesNone: "Gói của stack đã ở bản mới nhất. Chưa có gì để cập nhật.",
  packagesLead: (n: number, security: number) =>
    security > 0 ? `${n} gói của stack có bản mới, ${security} trong đó là bản vá bảo mật.` : `${n} gói của stack có bản mới, chưa có bản vá bảo mật nào trong đó.`,
  packagesButton: (n: number) => `Cập nhật ${n} gói`,
  packagesHint: "Xem trước, chạy, kiểm config, khởi động lại dịch vụ cần thiết rồi kiểm lại mọi site. Site nào đang đạt mà sau đó không đạt thì mở sự cố ngay.",
  listsUpdated: (when: string) => `Danh sách gói được làm mới lần cuối lúc ${when}.`,
  others: (n: number, security: number) =>
    n === 0
      ? "Hệ điều hành không còn gói nào khác đang chờ."
      : `Còn ${n} gói khác của hệ điều hành đang chờ, ${security} trong đó là bản vá bảo mật. unattended-upgrades tự cài các bản vá bảo mật, TPAN không đụng tới chúng.`,

  // (b) Bản vá bảo mật tự động
  autoHeading: "Bản vá bảo mật tự động",
  autoLabel: "Bản vá bảo mật tự động",
  autoOrigins: "Kho ngoài đang được tự vá",
  autoOriginsNone: "Chưa có kho ngoài nào trong danh sách tự vá.",
  autoButton: "Bật bản vá tự động",
  autoHint: "Cài và bật unattended-upgrades để bản vá bảo mật tự cài hằng ngày, và thêm các kho ngoài đã chọn vào danh sách tự vá. " + "Bấm là xem trước, chưa có gì đổi.",

  // (c) MariaDB
  mariaHeading: "MariaDB",
  mariaSource: "Nguồn",
  mariaLine: "Dòng",
  mariaVersion: "Bản đang chạy",
  mariaNotInstalled: "MariaDB chưa được cài trên máy này.",
  mariaWarn:
    "Nâng MariaDB là một chiều. Dữ liệu đã nâng thì bản cũ không đọc lại được, và TPAN không hoàn tác tự động. " +
    "Đường lùi là cài lại dòng cũ rồi nhập lại từ file xuất mà TPAN tạo trước khi nâng.",
  mariaUpgrades: "Dòng nâng lên được",
  mariaUpgradesLabel: "Các dòng MariaDB nâng lên được",
  mariaEol: (date: string) => `hết hỗ trợ ${date}`,
  mariaUpgradeButton: (line: string) => `Nâng lên ${line}`,
  mariaNone: "Không có dòng nào để nâng. Có thể MariaDB đã ở dòng LTS mới nhất, hoặc TPAN không hỏi được kho của MariaDB vì máy không ra được mạng.",

  // (d) Chính TPAN: khối "Phiên bản và cập nhật" dùng chung với màn Cài đặt, chuỗi ở phien-ban.ts.
};

export const strings = defineStrings(vi, {
  title: "Updates",
  summary: "Stack packages with new versions, automatic security patches, MariaDB upgrades and updating TPAN itself.",
  previewing: "Building the preview…",
  previewHint: "Pressing it shows a preview; nothing changes yet.",
  previewError: "Could not build the preview",
  previewErrorLine: (line) => `Could not build the preview for upgrading to ${line}`,
  checksHeading: "Conditions not met",
  noValue: "—",

  packagesHeading: "Stack packages",
  packagesTable: "Stack packages with a new version",
  colPackage: "Package",
  colFrom: "From",
  colTo: "To",
  colKind: "Kind",
  securityTag: "security patch",
  packagesNone: "The stack packages are on their latest version. There is nothing to update.",
  packagesLead: (n, security) =>
    security > 0
      ? `${n} stack ${n === 1 ? "package has" : "packages have"} a new version, ${security} of them ${security === 1 ? "is a security patch" : "are security patches"}.`
      : `${n} stack ${n === 1 ? "package has" : "packages have"} a new version, none of them a security patch.`,
  packagesButton: (n) => (n === 1 ? "Update 1 package" : `Update ${n} packages`),
  packagesHint:
    "Preview, run, check the config, restart the services that need it, then check every site again. A site that was passing and no longer does gets an incident right away.",
  listsUpdated: (when) => `The package lists were last refreshed at ${when}.`,
  others: (n, security) =>
    n === 0
      ? "The operating system has no other packages waiting."
      : `${n} other operating system ${n === 1 ? "package is" : "packages are"} waiting, ${security} of them security patches. unattended-upgrades installs the security patches; TPAN does not touch them.`,

  autoHeading: "Automatic security patches",
  autoLabel: "Automatic security patches",
  autoOrigins: "External repositories patched automatically",
  autoOriginsNone: "No external repository is in the automatic patch list.",
  autoButton: "Turn on automatic patches",
  autoHint: "Installs and enables unattended-upgrades so security patches install daily, and adds the external repositories you chose to the patch list. " + "Pressing it shows a preview; nothing changes yet.",

  mariaHeading: "MariaDB",
  mariaSource: "Source",
  mariaLine: "Series",
  mariaVersion: "Running version",
  mariaNotInstalled: "MariaDB is not installed on this machine.",
  mariaWarn:
    "Upgrading MariaDB goes one way. Once the data is upgraded, the old version cannot read it, and TPAN does not roll it back automatically. " +
    "The way back is to install the old series again and import the export file that TPAN makes before upgrading.",
  mariaUpgrades: "Series you can upgrade to",
  mariaUpgradesLabel: "MariaDB series you can upgrade to",
  mariaEol: (date) => `support ends ${date}`,
  mariaUpgradeButton: (line) => `Upgrade to ${line}`,
  mariaNone: "There is no series to upgrade to. MariaDB may already be on the latest LTS series, or TPAN could not ask the MariaDB repository because this machine has no network.",
});
