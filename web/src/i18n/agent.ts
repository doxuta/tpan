// Chuỗi của cả M7 (cổng cho agent): màn Plan chờ duyệt, khối "chờ duyệt" ở Bảng
// điều khiển, nút "Chép lệnh cho Claude Code" và mục "Agent và MCP" ở Cài đặt.
// Gom một file để các phần của M7 không sửa chung file chuỗi của màn khác.
// Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  // ---- nguồn gốc của Plan ----
  originAi: "AI đề xuất",
  originMcp: "Agent qua MCP đề xuất",
  originHuman: "Người tạo",

  // ---- trạng thái của Plan ----
  status: { pending: "chờ duyệt", applied: "đã chạy", rejected: "đã từ chối" },

  // ---- danh sách /plans ----
  listTitle: "Plan chờ duyệt",
  listSummary: "Thay đổi do AI hoặc agent đề xuất, và những Plan đã được duyệt hay từ chối. Máy chỉ đổi khi bạn duyệt.",
  filterLabel: "Lọc theo trạng thái",
  filterAll: "Tất cả",
  colPlan: "PLAN",
  colOrigin: "NGUỒN",
  colTarget: "ĐỐI TƯỢNG",
  colStatus: "TRẠNG THÁI",
  colTime: "ĐỀ XUẤT LÚC",
  listCaption: "Danh sách Plan",
  emptyPending: "Không có Plan nào chờ duyệt.",
  emptyApplied: "Chưa có Plan nào đã chạy.",
  emptyRejected: "Chưa có Plan nào bị từ chối.",
  emptyAll: "Chưa có Plan nào.",
  listFailed: "Chưa tải được danh sách Plan.",

  // ---- chi tiết /plans/:id ----
  detailLoadFailed: "Chưa tải được Plan này.",
  backToList: "Về danh sách Plan",
  proposedAt: (time: string) => `Đề xuất lúc ${time}`,
  untrustedWarning: "Plan này được đề xuất sau khi AI đọc nội dung do người lạ gửi tới (log). Đọc kỹ từng bước trước khi duyệt.",
  untrustedLabel: "Cảnh báo",
  planChanged: "Máy đã khác lúc đề xuất. Bản xem trước vừa được dựng lại, xem lại rồi duyệt lần nữa.",
  appliedLine: "Plan này đã chạy.",
  openJob: "Xem job và bằng chứng",
  // Đứng sau tên người từ chối (chữ mono) ở trang chi tiết.
  rejectedLine: (time: string) => `đã từ chối Plan này lúc ${time}.`,
  rejectedNoWho: (time: string) => `Plan này đã bị từ chối lúc ${time}.`,
  rejectHeading: "Không duyệt",
  rejectHint: "Từ chối thì Plan này không chạy, và máy giữ nguyên.",
  reject: "Từ chối Plan",
  rejectTitle: "Từ chối Plan này?",
  rejectBody: "Plan sẽ không chạy và không duyệt lại được. Agent hoặc AI có thể đề xuất một Plan mới.",
  rejectConfirm: "Từ chối",

  // ---- khối ở Bảng điều khiển ----
  pendingBlockTitle: "Plan đang chờ duyệt",
  pendingNone: "Không có Plan nào chờ bạn duyệt.",
  pendingAll: "Xem tất cả",
  pendingMore: (n: number) => `Còn ${n} Plan nữa`,
  pendingFailed: "Chưa tải được các Plan chờ duyệt.",

  // ---- nút chép lệnh cho Claude Code ----
  claudeCopy: "Chép lệnh cho Claude Code",
  claudeLabel: "LỆNH SẼ CHÉP",
  claudeHint: "Dán vào terminal trên máy cá nhân đã nối Claude Code với tpan. Plan nó đề xuất sẽ hiện ở đây để bạn duyệt.",
  thisMachine: "máy này",
  diagnoseCheck: (check: string, where: string) => `Dùng công cụ tpan chẩn đoán phép kiểm ${check} của ${where} đang lỗi rồi đề xuất Plan sửa.`,

  // ---- Cài đặt: Agent và MCP ----
  agentHeading: "Agent và MCP",
  agentIntro: "Cho Claude Code, Codex hay agent khác dùng tpan qua MCP, nối bằng SSH. Agent chỉ đọc và đề xuất Plan.",
  agentLoadFailed: "Chưa tải được thiết lập agent.",
  approvalLabel: "CHẾ ĐỘ DUYỆT",
  approvalPanel: "Plan của agent chờ bạn duyệt ở đây",
  approvalClient: "Agent tự chạy Plan nó đề xuất sau khi bạn đồng ý trong agent",
  redactLabel: "MỨC CHE DỮ LIỆU",
  redactAll: "Che hết: IP, email, tên miền đổi thành bí danh; mật khẩu và khoá bị loại.",
  redactSecrets: "Chỉ che mật khẩu và khoá; IP, email, tên miền gửi nguyên.",
  claudeCmdLabel: "THÊM VÀO CLAUDE CODE",
  codexCmdLabel: "THÊM VÀO CODEX",
  cmdHint: (host: string) => `Chạy trên máy cá nhân. Thay <user> bằng tài khoản SSH của bạn trên ${host}.`,
  changeLabel: "ĐỔI THIẾT LẬP",
  changeHint: "Hai mục trên chỉ đổi được bằng lệnh này trên máy chủ, vì chế độ client cho agent tự chạy Plan:",
  changeCmd: "sudo tpan mcp config --approval panel|client --redact all|secrets",
};

export const strings = defineStrings(vi, {
  originAi: "Proposed by AI",
  originMcp: "Proposed by an agent via MCP",
  originHuman: "Created by a person",

  status: { pending: "waiting for approval", applied: "applied", rejected: "rejected" },

  listTitle: "Pending plans",
  listSummary: "Changes proposed by AI or an agent, and the plans already approved or rejected. The machine only changes when you approve.",
  filterLabel: "Filter by status",
  filterAll: "All",
  colPlan: "PLAN",
  colOrigin: "SOURCE",
  colTarget: "TARGET",
  colStatus: "STATUS",
  colTime: "PROPOSED AT",
  listCaption: "List of plans",
  emptyPending: "No plan is waiting for approval.",
  emptyApplied: "No plan has been applied yet.",
  emptyRejected: "No plan has been rejected.",
  emptyAll: "No plans yet.",
  listFailed: "Could not load the list of plans.",

  detailLoadFailed: "Could not load this plan.",
  backToList: "Back to plans",
  proposedAt: (time) => `Proposed at ${time}`,
  untrustedWarning: "This plan was proposed after the AI read content sent by strangers (logs). Read every step carefully before you approve.",
  untrustedLabel: "Warning",
  planChanged: "The machine changed since this was proposed. The preview was just rebuilt; review it and approve again.",
  appliedLine: "This plan has run.",
  openJob: "Open the job and its evidence",
  rejectedLine: (time) => `rejected this plan at ${time}.`,
  rejectedNoWho: (time) => `This plan was rejected at ${time}.`,
  rejectHeading: "Do not approve",
  rejectHint: "If you reject it, this plan does not run and the machine stays as it is.",
  reject: "Reject plan",
  rejectTitle: "Reject this plan?",
  rejectBody: "The plan will not run and cannot be approved later. The AI or agent can propose a new one.",
  rejectConfirm: "Reject",

  pendingBlockTitle: "Plans waiting for approval",
  pendingNone: "No plan is waiting for you.",
  pendingAll: "See all",
  pendingMore: (n) => `${n} more ${n === 1 ? "plan" : "plans"}`,
  pendingFailed: "Could not load the pending plans.",

  claudeCopy: "Copy command for Claude Code",
  claudeLabel: "COMMAND THAT WILL BE COPIED",
  claudeHint: "Paste it in a terminal on your own machine where Claude Code is connected to tpan. The plan it proposes shows up here for you to approve.",
  thisMachine: "this machine",
  diagnoseCheck: (check, where) => `Use the tpan tools to diagnose the failing check ${check} of ${where}, then propose a Plan to fix it.`,

  agentHeading: "Agent and MCP",
  agentIntro: "Let Claude Code, Codex or another agent use tpan through MCP, connected over SSH. An agent can only read and propose plans.",
  agentLoadFailed: "Could not load the agent settings.",
  approvalLabel: "APPROVAL MODE",
  approvalPanel: "The agent's plans wait here for you to approve",
  approvalClient: "The agent runs the plans it proposes after you agree inside the agent",
  redactLabel: "DATA REDACTION",
  redactAll: "Redact everything: IPs, emails and domains become aliases; passwords and keys are dropped.",
  redactSecrets: "Redact only passwords and keys; IPs, emails and domains are sent as they are.",
  claudeCmdLabel: "ADD TO CLAUDE CODE",
  codexCmdLabel: "ADD TO CODEX",
  cmdHint: (host) => `Run it on your own machine. Replace <user> with your SSH account on ${host}.`,
  changeLabel: "CHANGE THE SETTINGS",
  changeHint: "The two settings above can only be changed with this command on the server, because client mode lets an agent run plans by itself:",
  changeCmd: "sudo tpan mcp config --approval panel|client --redact all|secrets",
});
