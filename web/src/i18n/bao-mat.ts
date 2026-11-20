// Chuỗi của Màn Bảo mật (mục 6.8 của KE-HOACH.md). Giọng chữ: mục 8 của
// docs/GIAO-DIEN.md. Tên và câu kết quả của từng dòng kiểm đến từ API (song
// ngữ sẵn), nên ở đây chỉ có chữ của màn.

import { defineStrings } from "./index";

const vi = {
  title: "Bảo mật",
  summary: "Tình trạng bảo mật của máy này. Mỗi dòng ghi lệnh đã dùng để kiểm.",

  listHeading: "Tình trạng",
  listLabel: "Các mục bảo mật của máy",
  readOnlyNote:
    "Hai dòng SSH chỉ để đọc. TPAN không có nút sửa cho chúng, vì sửa sai SSH là mất đường vào máy. Muốn đổi thì làm tay theo phần SỬA TAY của dòng đó.",
  recheck: "Kiểm lại",
  rechecking: "Đang kiểm…",
  recheckFailed: "Chưa kiểm lại được. Bấm Kiểm lại lần nữa; vẫn lỗi thì xem log của tpan.",
  empty: "Máy chủ chưa trả mục nào để kiểm.",
  commandsUsed: "Lệnh đã dùng",
  commandsMore: (n: number) => `và ${n} lệnh nữa`,

  // ---- mở một dòng ----
  noEvidence: "Phép kiểm này không ghi lại lệnh nào.",
  cause: "NGUYÊN NHÂN",
  manualFix: "SỬA TAY",
  sshNoFix: "TPAN không sửa dòng này giúp bạn: sửa sai SSH là mất đường vào máy.",

  // ---- hai thao tác ----
  ufwButton: "Bật tường lửa",
  ufwHint: "Mở trước các cổng đang dùng (SSH, web, panel) rồi mới bật. Bấm là xem trước, chưa có gì đổi.",
  fail2banButton: "Cài fail2ban",
  fail2banHint: "Cài fail2ban và bật jail sshd để chặn IP đoán mật khẩu SSH sai nhiều lần. Bấm là xem trước, chưa có gì đổi.",
  previewing: "Đang dựng bản xem trước…",
  previewError: "Chưa dựng được bản xem trước",
  checksHeading: "Điều kiện chưa đạt",

  // ---- bản xem trước ----
  portsLabel: "CỔNG SẼ MỞ TRƯỚC KHI BẬT TƯỜNG LỬA",
  portsLead: "Tường lửa chặn mọi cổng chưa được mở. Các cổng này được mở trước, rồi mới bật:",
  portsWarn: "Đối chiếu với cổng SSH bạn đang dùng để vào máy. Thiếu cổng đó trong danh sách thì đừng chạy: bật xong là mất đường vào máy.",

  // ---- M14: khối "Cô lập giữa các site" (mục 6.13 của KE-HOACH.md) ----
  isoHeading: "Cô lập giữa các site",
  isoLead:
    "Một site bị chiếm không được đọc, ghi hay chạy mã sang site khác trên cùng máy. Kết quả dưới đây là của lần health gần nhất; mở màn này không chạy lại phép kiểm.",
  isoLoadFailed: "Chưa đọc được tình trạng cô lập.",

  matrixHeading: "MA TRẬN SITE × SITE",
  matrixLead:
    "Hàng là site thử lấn sang, cột là site bị thử. Mỗi ô là một phép kiểm chạy dưới user của site ở hàng: đọc wp-config.php, vào thư mục, ghi vào thư mục web, đọc access.log và ghi vào socket của site ở cột. Bấm một ô để xem lệnh và output.",
  matrixKeys: "Đang ở trong bảng thì phím mũi tên đưa bạn sang ô bên cạnh.",
  matrixCaption: "Ma trận cô lập: hàng là site thử lấn sang, cột là site bị thử",
  matrixFrom: "TỪ SITE",
  matrixTo: "SANG SITE",
  matrixCount: (total: number, pass: number, fail: number, rest: number) => `${total} cặp: ${pass} đạt, ${fail} lỗi, ${rest} chưa rõ.`,
  matrixTooFew: (n: number): string => (n === 0 ? "Chưa có site nào nên chưa có gì để so." : "Máy mới có một site nên chưa có cặp nào để so."),
  matrixNotChecked:
    "Chưa kiểm. Health chưa chạy phép kiểm cô lập nào cho các site này. Kết quả hiện ở đây sau lần health kế tiếp; muốn có ngay thì chạy tpan health trên máy.",
  cellLabel: (from: string, to: string, status: string) => `${from} sang ${to}: ${status}`,
  cellHeading: (from: string, to: string) => `${from} sang ${to}`,
  cellNoResult: "Chưa kiểm. Health chưa chạy phép kiểm của cặp này, nên chưa có lệnh hay output nào.",
  cellClose: "Đóng",

  machineHeading: "PHÉP KIỂM CẤP MÁY",
  machineEmpty: "Chưa kiểm. Health chưa chạy các phép kiểm OPcache, /proc và định tuyến của máy này.",
  perSiteHeading: "PHÉP KIỂM CỦA TỪNG SITE",
  perSiteEmpty: "Chưa kiểm. Health chưa chạy phép kiểm quyền log và vhost của site nào.",

  htHeading: "CHẾ ĐỘ .HTACCESS CỦA TỪNG SITE",
  htLead:
    "an-toan: .htaccess chỉ dùng được các chỉ thị trong danh sách cho phép. tat: Apache không đọc .htaccess. day-du: .htaccess viết được mọi chỉ thị, nên site bị chiếm có thể chạy mã dưới user của site khác. Đổi chế độ ở trang của từng site.",
  htCaption: "Chế độ .htaccess của từng site",
  htColDomain: "Domain",
  htColMode: "Chế độ",
  htColResult: "Kết quả",
  htColAction: "Việc",
  htNoResult: "Chưa kiểm.",
  htOpenSite: "Mở trang site",
  htOpenSiteFor: (domain: string) => `Mở trang site ${domain}`,
  htEmpty: "Chưa có site nào.",

  hardenButton: "Áp bản vá cô lập",
  hardenNeeded: "Có phép kiểm chưa đạt mà bản vá này sửa được.",
  hardenHint:
    "Ghi file ini OPcache và drop-in ProtectProc cho mọi bản PHP, đặt lại quyền log, quét .htaccess rồi ghi lại vhost của mọi site, reload một lần. Mọi thứ đã đúng thì các bước được bỏ qua. Bấm là xem trước, chưa có gì đổi.",
  hardenPreviewError: "Chưa dựng được bản xem trước của bản vá cô lập",
  hardenModesLabel: "CHẾ ĐỘ .HTACCESS SAU BẢN VÁ",
  hardenSwitched: (n: number) => (n === 0 ? "Không site nào đổi chế độ .htaccess." : `${n} site chuyển sang an-toan`),
  hardenKept: (n: number) => `${n} site giữ day-du vì .htaccess có chỉ thị ngoài danh sách cho phép`,
  hardenKeptHint: "Sửa các dòng này trong .htaccess rồi áp bản vá lần nữa, hoặc để site ở day-du và nhận cảnh báo trong health.",
  hardenNoFindings: "Máy chủ không gửi kèm dòng nào.",
};

export const strings = defineStrings(vi, {
  title: "Security",
  summary: "The security state of this machine. Each line shows the command used to check it.",

  listHeading: "State",
  listLabel: "Security items of the machine",
  readOnlyNote:
    "The two SSH lines are read-only. TPAN has no fix button for them, because a wrong SSH change locks you out of the machine. To change them, follow the FIX BY HAND of that line by hand.",
  recheck: "Check again",
  rechecking: "Checking…",
  recheckFailed: "Could not check again. Press Check again once more; if it keeps failing, see the tpan log.",
  empty: "The server returned nothing to check.",
  commandsUsed: "Commands used",
  commandsMore: (n) => `and ${n} more`,

  noEvidence: "This check did not record any command.",
  cause: "CAUSE",
  manualFix: "FIX BY HAND",
  sshNoFix: "TPAN does not fix this line for you: a wrong SSH change locks you out of the machine.",

  ufwButton: "Turn on the firewall",
  ufwHint: "Opens the ports in use first (SSH, web, panel), then turns it on. Pressing it shows a preview; nothing changes yet.",
  fail2banButton: "Install fail2ban",
  fail2banHint: "Installs fail2ban and enables the sshd jail, which blocks IPs that enter a wrong SSH password many times. Pressing it shows a preview; nothing changes yet.",
  previewing: "Building the preview…",
  previewError: "Could not build the preview",
  checksHeading: "Conditions not met",

  portsLabel: "PORTS OPENED BEFORE THE FIREWALL TURNS ON",
  portsLead: "The firewall blocks every port that is not open. These ports are opened first, then it is turned on:",
  portsWarn: "Compare with the SSH port you use to reach the machine. If it is missing from the list, do not run: once the firewall is on you lose access to the machine.",

  isoHeading: "Isolation between sites",
  isoLead:
    "A compromised site must not read, write or run code in another site on the same machine. The results below are from the latest health run; opening this screen does not run the checks again.",
  isoLoadFailed: "Could not read the isolation state.",

  matrixHeading: "SITE × SITE MATRIX",
  matrixLead:
    "The row is the site that tries to reach across, the column is the site it tries to reach. Each cell is one check run as the row site's user: read wp-config.php, enter the directory, write to the web root, read access.log and write to the socket of the column site. Press a cell to see the commands and output.",
  matrixKeys: "Inside the table, the arrow keys move you to the next cell.",
  matrixCaption: "Isolation matrix: the row is the site that tries to reach across, the column is the site it tries to reach",
  matrixFrom: "FROM SITE",
  matrixTo: "TO SITE",
  matrixCount: (total, pass, fail, rest) => `${total} ${total === 1 ? "pair" : "pairs"}: ${pass} pass, ${fail} failed, ${rest} not known.`,
  matrixTooFew: (n) => (n === 0 ? "No sites yet, so there is nothing to compare." : "The machine has one site, so there is no pair to compare yet."),
  matrixNotChecked:
    "Not checked. Health has not run any isolation check for these sites yet. Results show up here after the next health run; to get them now, run tpan health on the machine.",
  cellLabel: (from, to, status) => `${from} to ${to}: ${status}`,
  cellHeading: (from, to) => `${from} to ${to}`,
  cellNoResult: "Not checked. Health has not run the check for this pair, so there is no command or output yet.",
  cellClose: "Close",

  machineHeading: "MACHINE-LEVEL CHECKS",
  machineEmpty: "Not checked. Health has not run the OPcache, /proc and routing checks of this machine yet.",
  perSiteHeading: "CHECKS OF EACH SITE",
  perSiteEmpty: "Not checked. Health has not run the log permission and vhost checks of any site yet.",

  htHeading: ".HTACCESS MODE OF EACH SITE",
  htLead:
    "an-toan (safe): .htaccess may only use the directives on the allow list. tat (off): Apache does not read .htaccess. day-du (full): .htaccess may use any directive, so a compromised site can run code as another site's user. Change the mode on each site's page.",
  htCaption: ".htaccess mode of each site",
  htColDomain: "Domain",
  htColMode: "Mode",
  htColResult: "Result",
  htColAction: "Action",
  htNoResult: "Not checked.",
  htOpenSite: "Open the site page",
  htOpenSiteFor: (domain) => `Open the page of site ${domain}`,
  htEmpty: "No sites yet.",

  hardenButton: "Apply isolation hardening",
  hardenNeeded: "Some checks do not pass, and this hardening can fix them.",
  hardenHint:
    "Writes the OPcache ini file and the ProtectProc drop-in for every PHP version, resets log permissions, scans .htaccess and rewrites the vhost of every site, then reloads once. Steps that are already right are skipped. Pressing it shows a preview; nothing changes yet.",
  hardenPreviewError: "Could not build the preview of the isolation hardening",
  hardenModesLabel: ".HTACCESS MODE AFTER THE HARDENING",
  hardenSwitched: (n) => (n === 0 ? "No site changes its .htaccess mode." : `${n} ${n === 1 ? "site switches" : "sites switch"} to an-toan`),
  hardenKept: (n) => `${n} ${n === 1 ? "site stays" : "sites stay"} on day-du because .htaccess has directives outside the allow list`,
  hardenKeptHint: "Fix these lines in .htaccess and apply the hardening again, or leave the site on day-du and accept the warning in health.",
  hardenNoFindings: "The server sent no lines with it.",
});
