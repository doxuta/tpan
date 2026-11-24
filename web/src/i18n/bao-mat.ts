// Chuỗi của Màn Bảo mật (mục 6.8 của KE-HOACH.md). Giọng chữ: mục 8 của
// docs/GIAO-DIEN.md. Tên và câu kết quả của từng dòng kiểm đến từ API (song
// ngữ sẵn), nên ở đây chỉ có chữ của màn.

import { defineStrings } from "./index";

const vi = {
  title: "Bảo mật",
  summary: "Tình trạng bảo mật của máy này. Mở một dòng để xem lệnh đã dùng để kiểm.",

  listHeading: "Kiểm tra bảo mật máy",
  listLabel: "Các mục bảo mật của máy",
  readOnlyNote:
    "Hai dòng SSH chỉ để đọc. TPAN không có nút sửa cho chúng, vì sửa sai SSH là mất đường vào máy. Muốn đổi thì làm tay theo phần Sửa tay của dòng đó.",
  recheck: "Kiểm lại",
  rechecking: "Đang kiểm…",
  recheckFailed: "Chưa kiểm lại được. Bấm Kiểm lại lần nữa; vẫn lỗi thì xem log của tpan.",
  empty: "Máy chủ chưa trả mục nào để kiểm.",

  // ---- mở một dòng ----
  noEvidence: "Phép kiểm này không ghi lại lệnh nào.",
  cause: "Nguyên nhân",
  manualFix: "Sửa tay",
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
  portsLabel: "Cổng sẽ mở trước khi bật tường lửa",
  portsLead: "Tường lửa chặn mọi cổng chưa mở. TPAN mở các cổng này trước, rồi mới bật:",
  portsWarn: "Đối chiếu với cổng SSH bạn đang dùng để vào máy. Thiếu cổng đó trong danh sách thì đừng chạy: bật xong là mất đường vào máy.",

  // ---- M14: khối "Cô lập giữa các site" (mục 6.13 của KE-HOACH.md) ----
  isoHeading: "Cô lập giữa các site",
  isoLead:
    "Một site bị chiếm không được đọc, ghi hay chạy mã sang site khác trên cùng máy. Kết quả dưới đây là của lần health gần nhất; mở màn này không chạy lại phép kiểm.",
  isoLoadFailed: "Chưa đọc được tình trạng cô lập.",

  matrixHeading: "Ma trận site × site",
  matrixLead:
    "Hàng là site thử lấn sang, cột là site bị thử. Mỗi ô là một phép kiểm chạy dưới user của site ở hàng: đọc wp-config.php, vào thư mục, ghi vào thư mục web, đọc access.log và ghi vào socket của site ở cột. Bấm một ô để xem lệnh và output.",
  matrixKeys: "Trong bảng, phím mũi tên chuyển sang ô bên cạnh.",
  matrixCaption: "Ma trận cô lập: hàng là site thử lấn sang, cột là site bị thử",
  matrixFrom: "Từ site",
  matrixTo: "Sang site",
  matrixCount: (total: number, pass: number, fail: number, rest: number) => `${total} cặp: ${pass} đạt, ${fail} lỗi, ${rest} chưa rõ.`,
  matrixTooFew: (n: number): string => (n === 0 ? "Chưa có site nào nên chưa có gì để so." : "Máy mới có một site nên chưa có cặp nào để so."),
  matrixNotChecked:
    "Chưa kiểm. Health chưa chạy phép kiểm cô lập nào cho các site này. Kết quả hiện ở đây sau lần health kế tiếp; muốn có ngay thì chạy tpan health trên máy.",
  cellLabel: (from: string, to: string, status: string) => `${from} sang ${to}: ${status}`,
  cellHeading: (from: string, to: string) => `${from} sang ${to}`,
  cellNoResult: "Chưa kiểm. Health chưa chạy phép kiểm của cặp này, nên chưa có lệnh hay output nào.",
  cellClose: "Đóng",

  machineHeading: "Phép kiểm cấp máy",
  machineEmpty: "Chưa kiểm. Health chưa chạy các phép kiểm OPcache, /proc và định tuyến của máy này.",
  perSiteHeading: "Phép kiểm của từng site",
  perSiteEmpty: "Chưa kiểm. Health chưa chạy phép kiểm quyền log và vhost của site nào.",
  groupCount: (n: number, pass: number, bad: number) => `${n} phép kiểm: ${pass} đạt, ${bad} chưa đạt`,

  htHeading: "Chế độ .htaccess của từng site",
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
    "Ghi file ini OPcache và drop-in ProtectProc cho mọi bản PHP, đặt lại quyền log, quét .htaccess rồi ghi lại vhost của mọi site, reload một lần. Mọi thứ đã đúng thì bỏ qua các bước đó. Bấm là xem trước, chưa có gì đổi.",
  hardenPreviewError: "Chưa dựng được bản xem trước của bản vá cô lập",
  hardenModesLabel: "Chế độ .htaccess sau bản vá",
  hardenSwitched: (n: number) => (n === 0 ? "Không site nào đổi chế độ .htaccess." : `${n} site chuyển sang an-toan`),
  hardenKept: (n: number) => `${n} site giữ day-du vì .htaccess có chỉ thị ngoài danh sách cho phép`,
  hardenKeptHint: "Sửa các dòng này trong .htaccess rồi áp bản vá lần nữa, hoặc để site ở day-du và nhận cảnh báo trong health.",
  hardenNoFindings: "Máy chủ không gửi kèm dòng nào.",

  // ---- M17: thẻ "Thử tấn công" (mục 6.15 của KE-HOACH.md, mục 7 của GIAO-DIEN.md) ----
  probeHeading: "Thử tấn công",
  probeLead:
    "TPAN tạo hai site thử có tên dành riêng, chạy các đường tấn công ở ngoài lồng rồi trong lồng, rồi xoá sạch cả hai. Mất vài phút. Site thật không bị đụng.",
  probeButton: "Thử tấn công",
  probeOnlyLabel: "Lượt chạy",
  probeOnlyBoth: "Cả hai",
  probeOnlyOutside: "Ngoài lồng",
  probeOnlyInside: "Trong lồng",
  probeLoadFailed: "Chưa đọc được kết quả thử tấn công.",
  probePreviewError: "Chưa dựng được bản xem trước của lượt thử tấn công",
  probeCleanupPreviewError: "Chưa dựng được bản xem trước của việc dọn lượt thử",
  probeEmpty: "Chưa có lượt thử nào. Bấm Thử tấn công để chạy lượt đầu tiên.",

  probeRunning: (domains: string) => `Đang có một lượt thử chạy (site thử ${domains}). Đợi nó xong rồi hãy chạy lượt khác.`,
  probePending: (n: number, domains: string) => `Còn ${n} lượt thử chưa dọn (site thử ${domains}).`,
  probeCleanupButton: "Dọn lượt thử còn sót",

  probePreviewHeading: "Hai site thử sẽ được tạo rồi xoá",
  probePreviewLead: "Tên dành riêng cho việc thử; site thật không bị đụng. Chạy xong, TPAN xoá cả hai.",
  probePreviewA: "Site thử A",
  probePreviewB: "Site thử B",
  probePreviewPasses: "Lượt chạy",

  probeLastHeading: "Lượt gần nhất",
  probeWhen: (when: string) => `Chạy lúc ${when}`,
  probePaths: (n: number) => `${n} đường`,
  probeOnlyDone: { "": "cả hai lượt", ngoai: "chỉ ngoài lồng", trong: "chỉ trong lồng" },
  probeExposedNotice: (n: number) => `Có ${n} ô lộ: đường lẽ ra phải bị chặn mà vẫn đi được. Mở ô lộ trong bảng để xem bằng chứng.`,
  probeSafeNotice: "Không ô nào lộ.",
  probeStatus: { blocked: "bị chặn", exposed: "lộ", needs_cage: "chỉ lồng chặn được", skipped: "bỏ qua", na: "không áp dụng" },
  probeCount: (n: number, status: string) => `${n} ${status}`,

  probeCaption: "Kết quả thử tấn công: mỗi đường tấn công một hàng, hai cột là ngoài lồng và trong lồng",
  probeColPath: "Đường",
  probeColName: "Cách tấn công",
  probeColOutside: "Ngoài lồng",
  probeColInside: "Trong lồng",
  probeCellLabel: (path: string, column: string, status: string) => `${path}, ${column}: ${status}`,
  probeNoEvidence: "Ô này không ghi lại lệnh nào.",
  probeNaSummary: (n: number) => `${n} đường không áp dụng cho stack này`,
};

export const strings = defineStrings(vi, {
  title: "Security",
  summary: "The security state of this machine. Open a line to see the command used to check it.",

  listHeading: "Machine security check",
  listLabel: "Security items of the machine",
  readOnlyNote:
    "The two SSH lines are read-only. TPAN has no fix button for them, because a wrong SSH change locks you out of the machine. To change them, follow the Fix by hand part of that line.",
  recheck: "Check again",
  rechecking: "Checking…",
  recheckFailed: "Could not check again. Press Check again once more; if it keeps failing, see the tpan log.",
  empty: "The server returned nothing to check.",

  noEvidence: "This check did not record any command.",
  cause: "Cause",
  manualFix: "Fix by hand",
  sshNoFix: "TPAN does not fix this line for you: a wrong SSH change locks you out of the machine.",

  ufwButton: "Turn on the firewall",
  ufwHint: "Opens the ports in use first (SSH, web, panel), then turns it on. Pressing it shows a preview; nothing changes yet.",
  fail2banButton: "Install fail2ban",
  fail2banHint: "Installs fail2ban and enables the sshd jail, which blocks IPs that enter a wrong SSH password many times. Pressing it shows a preview; nothing changes yet.",
  previewing: "Building the preview…",
  previewError: "Could not build the preview",
  checksHeading: "Conditions not met",

  portsLabel: "Ports opened before the firewall turns on",
  portsLead: "The firewall blocks every port that is not open. These ports are opened first, then it is turned on:",
  portsWarn: "Compare with the SSH port you use to reach the machine. If it is missing from the list, do not run: once the firewall is on you lose access to the machine.",

  isoHeading: "Isolation between sites",
  isoLead:
    "A compromised site must not read, write or run code in another site on the same machine. The results below are from the latest health run; opening this screen does not run the checks again.",
  isoLoadFailed: "Could not read the isolation state.",

  matrixHeading: "Site × site matrix",
  matrixLead:
    "The row is the site that tries to reach across, the column is the site it tries to reach. Each cell is one check run as the row site's user: read wp-config.php, enter the directory, write to the web root, read access.log and write to the socket of the column site. Press a cell to see the commands and output.",
  matrixKeys: "Inside the table, the arrow keys move you to the next cell.",
  matrixCaption: "Isolation matrix: the row is the site that tries to reach across, the column is the site it tries to reach",
  matrixFrom: "From site",
  matrixTo: "To site",
  matrixCount: (total, pass, fail, rest) => `${total} ${total === 1 ? "pair" : "pairs"}: ${pass} pass, ${fail} failed, ${rest} not known.`,
  matrixTooFew: (n) => (n === 0 ? "No sites yet, so there is nothing to compare." : "The machine has one site, so there is no pair to compare yet."),
  matrixNotChecked:
    "Not checked. Health has not run any isolation check for these sites yet. Results show up here after the next health run; to get them now, run tpan health on the machine.",
  cellLabel: (from, to, status) => `${from} to ${to}: ${status}`,
  cellHeading: (from, to) => `${from} to ${to}`,
  cellNoResult: "Not checked. Health has not run the check for this pair, so there is no command or output yet.",
  cellClose: "Close",

  machineHeading: "Machine-level checks",
  machineEmpty: "Not checked. Health has not run the OPcache, /proc and routing checks of this machine yet.",
  perSiteHeading: "Checks of each site",
  perSiteEmpty: "Not checked. Health has not run the log permission and vhost checks of any site yet.",
  groupCount: (n, pass, bad) => `${n} ${n === 1 ? "check" : "checks"}: ${pass} passed, ${bad} not passed`,

  htHeading: ".htaccess mode of each site",
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
  hardenModesLabel: ".htaccess mode after the hardening",
  hardenSwitched: (n) => (n === 0 ? "No site changes its .htaccess mode." : `${n} ${n === 1 ? "site switches" : "sites switch"} to an-toan`),
  hardenKept: (n) => `${n} ${n === 1 ? "site stays" : "sites stay"} on day-du because .htaccess has directives outside the allow list`,
  hardenKeptHint: "Fix these lines in .htaccess and apply the hardening again, or leave the site on day-du and accept the warning in health.",
  hardenNoFindings: "The server sent no lines with it.",

  probeHeading: "Attack test",
  probeLead:
    "TPAN creates two test sites with reserved names, runs the attack paths outside the cage and then inside it, then deletes both. It takes a few minutes. Your real sites are not touched.",
  probeButton: "Attack test",
  probeOnlyLabel: "Pass to run",
  probeOnlyBoth: "Both",
  probeOnlyOutside: "Not caged",
  probeOnlyInside: "Caged",
  probeLoadFailed: "Could not read the attack test result.",
  probePreviewError: "Could not build the preview of the attack test",
  probeCleanupPreviewError: "Could not build the preview of the clean-up",
  probeEmpty: "No test run yet. Press Attack test to run the first one.",

  probeRunning: (domains) => `A test run is in progress (test sites ${domains}). Wait for it to finish before starting another.`,
  probePending: (n, domains) => `${n} test ${n === 1 ? "run is" : "runs are"} still not cleaned up (test sites ${domains}).`,
  probeCleanupButton: "Clean up leftover test runs",

  probePreviewHeading: "Two test sites will be created, then deleted",
  probePreviewLead: "The names are reserved for testing; your real sites are not touched. When the run ends, TPAN deletes both.",
  probePreviewA: "Test site A",
  probePreviewB: "Test site B",
  probePreviewPasses: "Pass to run",

  probeLastHeading: "Latest run",
  probeWhen: (when) => `Ran at ${when}`,
  probePaths: (n) => `${n} ${n === 1 ? "path" : "paths"}`,
  probeOnlyDone: { "": "both passes", ngoai: "not caged only", trong: "caged only" },
  probeExposedNotice: (n) => `${n} ${n === 1 ? "cell is" : "cells are"} exposed: a path that should be blocked still gets through. Open the exposed cell in the table to see the evidence.`,
  probeSafeNotice: "No cell is exposed.",
  probeStatus: { blocked: "blocked", exposed: "exposed", needs_cage: "needs a cage", skipped: "skipped", na: "not applicable" },
  probeCount: (n, status) => `${n} ${status}`,

  probeCaption: "Attack test result: one row per attack path, with a not caged column and a caged column",
  probeColPath: "Path",
  probeColName: "Attack",
  probeColOutside: "Not caged",
  probeColInside: "Caged",
  probeCellLabel: (path, column, status) => `${path}, ${column}: ${status}`,
  probeNoEvidence: "This cell did not record any command.",
  probeNaSummary: (n) => `${n} ${n === 1 ? "path does" : "paths do"} not apply to this stack`,
});
