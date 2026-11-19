// Chuỗi của màn Stack (mục 6.11 và mục 10 của KE-HOACH.md). Giọng chữ: mục 8 của
// docs/GIAO-DIEN.md. Dấu trạng thái, bằng chứng, nguyên nhân và sửa tay của các
// dịch vụ dùng chuỗi của trang Site (CheckItem), nên không lặp ở đây.

import { defineStrings } from "./index";

const vi = {
  title: "Stack",
  summary: "Web server, MariaDB và các bản PHP trên máy này.",
  previewing: "Đang dựng bản xem trước…",
  previewHint: "Bấm là xem trước, chưa có gì đổi.",
  checksHeading: "Điều kiện chưa đạt",
  noValue: "—",
  sourceUbuntu: "Ubuntu",
  sourcePhpSury: "kho của Ondřej Surý",

  // ---- cấu hình ----
  configHeading: "Cấu hình",
  cfgWeb: "Web server",
  cfgWebSource: "Nguồn web server",
  cfgDbSource: "Nguồn MariaDB",
  cfgPhp: "Bản PHP mặc định",
  dbLine: (line: string) => `dòng ${line}`,
  notInstalled: "Stack chưa cài trên máy này nên chưa có cấu hình và dịch vụ để hiện.",
  sourceFixed:
    "Nguồn của web server và MariaDB chỉ chọn được lúc cài stack, nên màn này không có nút đổi nguồn. " +
    "Muốn nguồn khác thì chạy lệnh cài trên máy với nguồn bạn chọn. Lệnh dưới đây chỉ là ví dụ: có --dry-run nên nó chỉ cho xem trước, chưa đổi gì.",
  installExample: "sudo tpan stack install --web nginx --web-source nginx.org --db-source mariadb.org --db-line 11.8 --dry-run",
  loadFailed: "Chưa đọc được cấu hình stack.",

  // ---- dịch vụ ----
  servicesHeading: "Dịch vụ",
  servicesNone: "Chưa có dịch vụ nào để kiểm.",

  // ---- bản PHP ----
  phpHeading: "Các bản PHP",
  phpTable: "Các bản PHP trên máy này",
  colVersion: "Bản",
  colInstalled: "Trạng thái",
  colSites: "Site dùng",
  colSource: "Nguồn",
  colSupport: "Hỗ trợ",
  colWp: "WordPress",
  colAction: "Việc",
  phpEmpty: "Chưa có bản PHP nào để hiện. Kho gói chưa trả về gói php-fpm nào.",
  installed: "Đã cài",
  notInstalledPhp: "Chưa cài",
  defaultTag: "mặc định",
  sitesToggle: (version: string, n: number, open: boolean) => `${open ? "Đóng" : "Mở"} danh sách ${n} site dùng PHP ${version}`,
  sitesList: (version: string) => `Site dùng PHP ${version}`,
  supportState: { stable: "hỗ trợ đầy đủ", security: "chỉ vá bảo mật", eol: "hết hỗ trợ" },
  supportActive: (date: string) => `đầy đủ tới ${date}`,
  supportSecurity: (date: string) => `bảo mật tới ${date}`,
  supportEnded: (date: string) => `hết hạn ${date}`,
  wpRecommended: "khuyên dùng",
  wpRecommendedTitle: "Bản PHP mà WordPress.org khuyên dùng",
  install: "Cài",
  installLabel: (version: string) => `Cài PHP ${version}`,
  remove: "Gỡ",
  removeLabel: (version: string) => `Gỡ PHP ${version}`,
  actionHint:
    "Chỉ gỡ được bản đã cài mà không site nào dùng và không phải bản mặc định. Gỡ có một bước nguy hiểm, nên bản xem trước sẽ hỏi lại bạn.",
  notesMissing: "Chưa hỏi được ghi chú hạn hỗ trợ và bản WordPress khuyên dùng (máy có thể không ra được mạng). Danh sách bản PHP vẫn đủ.",
  suryOff:
    "Kho PHP của Ondřej Surý chưa được thêm. Cài một bản mà Ubuntu không có sẽ thêm kho ngoài này vào máy; bản xem trước hiện rõ kho và khoá ký sẽ thêm.",
  previewError: (version: string, install: boolean) => `Chưa dựng được bản xem trước để ${install ? "cài" : "gỡ"} PHP ${version}`,

  // ---- đổi web server (M10) ----
  switchHeading: "Đổi web server",
  switchCurrent: "Đang chạy",
  switchNotInstalled: "Stack chưa cài trên máy này nên chưa có web server để đổi.",
  switchTo: (web: string) => `Đổi sang ${web}`,
  switchHint: (from: string, to: string) =>
    `Cài ${to} nếu máy chưa có, ghi lại vhost của mọi site, rồi dừng ${from} và chạy ${to}. Mọi site ngừng trả lời một khoảnh khắc lúc đổi. ` +
    `Bước nào lỗi thì máy quay về ${from}.`,
  switchCommandHint: "Muốn tự gõ trên máy thì dùng lệnh này. Có --dry-run nên nó chỉ cho xem trước, chưa đổi gì.",
  switchCommand: (web: string) => `sudo tpan stack switch --to ${web} --dry-run`,
  switchPreviewError: (web: string) => `Chưa dựng được bản xem trước để đổi sang ${web}`,
};

export const strings = defineStrings(vi, {
  title: "Stack",
  summary: "The web server, MariaDB and the PHP versions on this machine.",
  previewing: "Building the preview…",
  previewHint: "Pressing it shows a preview; nothing changes yet.",
  checksHeading: "Conditions not met",
  noValue: "—",
  sourceUbuntu: "Ubuntu",
  sourcePhpSury: "Ondřej Surý's repository",

  configHeading: "Configuration",
  cfgWeb: "Web server",
  cfgWebSource: "Web server source",
  cfgDbSource: "MariaDB source",
  cfgPhp: "Default PHP version",
  dbLine: (line) => `series ${line}`,
  notInstalled: "The stack is not installed on this machine, so there is no configuration or service to show yet.",
  sourceFixed:
    "The source of the web server and MariaDB can only be chosen when the stack is installed, so this page has no button to change it. " +
    "To use another source, run the install command on the machine with the source you want. The command below is only an example: it has --dry-run, so it only shows a preview and changes nothing.",
  installExample: "sudo tpan stack install --web nginx --web-source nginx.org --db-source mariadb.org --db-line 11.8 --dry-run",
  loadFailed: "Could not read the stack configuration.",

  servicesHeading: "Services",
  servicesNone: "There is no service to check yet.",

  phpHeading: "PHP versions",
  phpTable: "PHP versions on this machine",
  colVersion: "Version",
  colInstalled: "Status",
  colSites: "Sites",
  colSource: "Source",
  colSupport: "Support",
  colWp: "WordPress",
  colAction: "Action",
  phpEmpty: "There is no PHP version to show. The package repositories returned no php-fpm package.",
  installed: "Installed",
  notInstalledPhp: "Not installed",
  defaultTag: "default",
  sitesToggle: (version, n, open) => `${open ? "Close" : "Open"} the list of ${n} ${n === 1 ? "site" : "sites"} using PHP ${version}`,
  sitesList: (version) => `Sites using PHP ${version}`,
  supportState: { stable: "full support", security: "security fixes only", eol: "end of life" },
  supportActive: (date) => `full until ${date}`,
  supportSecurity: (date) => `security until ${date}`,
  supportEnded: (date) => `ended ${date}`,
  wpRecommended: "recommended",
  wpRecommendedTitle: "The PHP version WordPress.org recommends",
  install: "Install",
  installLabel: (version) => `Install PHP ${version}`,
  remove: "Remove",
  removeLabel: (version) => `Remove PHP ${version}`,
  actionHint:
    "You can only remove a version that is installed, that no site uses and that is not the default. Removing has a dangerous step, so the preview asks you again.",
  notesMissing: "Could not ask for the support dates and the WordPress recommendation (the machine may have no network). The list of PHP versions is still complete.",
  suryOff:
    "Ondřej Surý's PHP repository has not been added. Installing a version Ubuntu does not have adds this external repository to the machine; the preview shows the repository and the signing key it will add.",
  previewError: (version, install) => `Could not build the preview to ${install ? "install" : "remove"} PHP ${version}`,

  switchHeading: "Change web server",
  switchCurrent: "Running",
  switchNotInstalled: "The stack is not installed on this machine, so there is no web server to change yet.",
  switchTo: (web) => `Switch to ${web}`,
  switchHint: (from, to) =>
    `Installs ${to} if the machine does not have it, rewrites the vhost of every site, then stops ${from} and starts ${to}. Every site stops answering for a moment during the switch. ` +
    `If any step fails, the machine goes back to ${from}.`,
  switchCommandHint: "To type it yourself on the machine, use this command. It has --dry-run, so it only shows a preview and changes nothing.",
  switchCommand: (web) => `sudo tpan stack switch --to ${web} --dry-run`,
  switchPreviewError: (web) => `Could not build the preview to switch to ${web}`,
});
