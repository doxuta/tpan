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
});
