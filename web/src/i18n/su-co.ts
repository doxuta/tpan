// Chuỗi của màn Sự cố: danh sách và trang một sự cố. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Sự cố",
  summary: "Những chỗ máy đang hỏng và những chỗ đã sửa xong. Mỗi sự cố giữ bằng chứng lúc mở và lúc đóng.",
  listLabel: "Danh sách sự cố",

  // ---- danh sách ----
  openHeading: (n: number) => `Đang mở · ${n}`,
  closedHeading: (n: number) => `Đã đóng · ${n}`,
  healthy: (lastRun: string) => `Không có sự cố đang mở. Máy đang khoẻ: lần kiểm gần nhất lúc ${lastRun}.`,
  healthyNeverRun: "Không có sự cố đang mở. Các phép kiểm chưa chạy lần nào, nên chưa biết máy có khoẻ không.",
  noOpen: "Không có sự cố đang mở.",
  noClosed: "Chưa có sự cố nào đã đóng.",
  colIncident: "Sự cố",
  colTarget: "Đối tượng",
  colStage: "Chặng",
  colOpened: "Mở lúc",
  colClosed: "Đóng lúc",
  stateOpen: "đang mở",
  stateClosed: "đã đóng",
  stateGone: "đã đóng, đối tượng không còn được kiểm",
  openFor: (span: string) => `đang mở · ${span}`,
  closedGone: "đối tượng không còn được kiểm",

  targetMachine: "Máy",
  targetTpan: "TPAN",
  stages: {
    dns: "DNS",
    tls: "TLS",
    web: "Web server",
    php: "Pool PHP",
    wordpress: "WordPress",
    db: "DB",
    e2e: "Request thật",
    machine: "Máy",
    tpan: "TPAN",
  },

  spanLessThanMinute: "chưa tới 1 phút",
  spanMinutes: (m: number) => `${m} phút`,
  spanHours: (h: number, m: number) => (m > 0 ? `${h} giờ ${m} phút` : `${h} giờ`),
  spanDays: (d: number, h: number) => (h > 0 ? `${d} ngày ${h} giờ` : `${d} ngày`),

  // ---- trang một sự cố ----
  backToList: "Về danh sách sự cố",
  notFound: (id: string) => `Không có sự cố ${id}. Số có thể gõ sai. Xem danh sách sự cố rồi chọn lại.`,
  detailOpen: (opened: string) => `Đang mở từ ${opened}.`,
  detailClosed: (opened: string, closed: string) => `Mở lúc ${opened}, đóng lúc ${closed}.`,

  fieldStatus: "TRẠNG THÁI",
  fieldTarget: "ĐỐI TƯỢNG",
  fieldStage: "CHẶNG",
  fieldCheck: "PHÉP KIỂM",
  fieldOpened: "MỞ LÚC",
  fieldClosed: "ĐÓNG LÚC",
  fieldDuration: "KÉO DÀI",

  whatHeading: "Chuyện gì xảy ra",
  causeLabel: "NGUYÊN NHÂN CÓ THỂ",

  recheck: "Kiểm lại",
  rechecking: "Đang kiểm…",
  rechecked: (time: string) => `Đã kiểm lại lúc ${time}.`,
  recheckHint: "Chạy lại các phép kiểm ngay bây giờ. Phép kiểm đạt thì sự cố tự đóng.",
  recheckFailed: "Chưa kiểm lại được. Bấm Kiểm lại thêm một lần; vẫn lỗi thì xem log của tpan.",

  evidenceOpenHeading: "Bằng chứng lúc mở",
  evidenceCloseHeading: "Bằng chứng lúc đóng",
  currentHeading: "Kết quả hiện tại",
  currentChecked: (time: string) => `Kiểm lúc ${time}`,
  noCurrent: "Phép kiểm này không còn được chạy, nên chưa có kết quả nào mới hơn lúc mở.",
  noEvidence: "Phép kiểm này không ghi lại lệnh nào, nên không có output để xem.",
  closedGoneNote: "Sự cố đóng vì đối tượng không còn được kiểm (ví dụ site đã bị xoá), nên không có bằng chứng lúc đóng.",

  jobsHeading: "Việc đã chạy để sửa",
  noJobs: "Chưa có việc sửa nào chạy cho sự cố này.",
  jobsLabel: "Các job sửa",
  jobStatus: {
    running: "đang chạy",
    succeeded: "xong",
    rolled_back: "lỗi, đã hoàn tác",
    undo_failed: "lỗi, hoàn tác cũng lỗi",
  },

  // ---- ba cách sửa ----
  fixHeading: "Cách sửa",
  fixHint: "Chọn cách bạn muốn. Máy chỉ đổi khi bạn duyệt bản xem trước.",
  // User thường (mục 6.12) chỉ có cách sửa tay: sửa tự động, nhận bản sửa tay và hỏi AI là việc của admin.
  fixHintUser: "Các lệnh dưới đây chạy trên máy chủ. Đưa chúng cho người quản trị máy chủ.",

  autoHeading: "Sửa tự động",
  autoBody: "TPAN dựng một Plan từ các bước có sẵn. Bạn xem từng bước trước khi máy đổi.",
  autoDrift: "File này khác bản TPAN đã ghi. Bản xem trước hiện từng dòng khác nhau giữa bản đang có và bản của TPAN.",
  autoPreview: "Sửa tự động · xem trước",
  autoPreviewing: "Đang dựng bản xem trước…",
  autoPreviewFailed: "Chưa dựng được bản xem trước của cách sửa này.",

  manualHeading: "Sửa tay",
  manualNone: "Phép kiểm này chưa đưa ra lệnh nào. Xem bằng chứng bên dưới để biết hỏng ở đâu.",

  acceptHeading: "Nhận bản sửa tay làm chuẩn",
  acceptBody: "Bạn đã sửa file này trên máy và muốn giữ bản đó. TPAN ghi nhớ nội dung hiện tại là bản chuẩn nên thôi báo lệch. File không đổi.",
  accept: "Nhận bản sửa tay",
  acceptTitle: "Nhận bản sửa tay làm chuẩn?",
  acceptDescription: "File giữ nguyên. TPAN chỉ ghi nhớ nội dung hiện tại để thôi báo lệch.",
  acceptConfirm: "Nhận bản này",
  accepted: "Đã nhận bản sửa tay. Bấm Kiểm lại để phép kiểm báo lại.",
  acceptFailed: "Chưa nhận được bản sửa tay.",

  aiHeading: "Hỏi AI",
  aiBody: "AI đọc phát hiện và bằng chứng đã che rồi chẩn đoán. Ngăn AI mở ra với câu hỏi soạn sẵn; bạn xem thứ sẽ gửi rồi tự bấm Gửi. AI chỉ đề xuất, máy chỉ đổi khi bạn duyệt.",
  aiButton: "Hỏi AI",

  // Nút "Chẩn đoán" ở đầu trang sự cố.
  diagnose: "Chẩn đoán",
  diagnoseHint: "Mở ngăn AI với câu hỏi soạn sẵn. Bạn xem thứ sẽ gửi rồi mới bấm Gửi.",
  diagnoseQuestion: (title: string) => `Chẩn đoán sự cố “${title}”: nguyên nhân có thể là gì, và nên sửa theo cách nào.`,
};

export const strings = defineStrings(vi, {
  title: "Incidents",
  summary: "What is broken on this machine and what has been fixed. Each incident keeps the evidence from when it opened and when it closed.",
  listLabel: "List of incidents",

  openHeading: (n) => `Open · ${n}`,
  closedHeading: (n) => `Closed · ${n}`,
  healthy: (lastRun) => `No open incidents. The machine is healthy: last check at ${lastRun}.`,
  healthyNeverRun: "No open incidents. The checks have not run yet, so there is no way to tell whether the machine is healthy.",
  noOpen: "No open incidents.",
  noClosed: "No closed incidents yet.",
  colIncident: "Incident",
  colTarget: "Target",
  colStage: "Stage",
  colOpened: "Opened",
  colClosed: "Closed",
  stateOpen: "open",
  stateClosed: "closed",
  stateGone: "closed, target no longer checked",
  openFor: (span) => `open · ${span}`,
  closedGone: "target no longer checked",

  targetMachine: "Machine",
  targetTpan: "TPAN",
  stages: {
    dns: "DNS",
    tls: "TLS",
    web: "Web server",
    php: "PHP pool",
    wordpress: "WordPress",
    db: "DB",
    e2e: "Real request",
    machine: "Machine",
    tpan: "TPAN",
  },

  spanLessThanMinute: "under 1 minute",
  spanMinutes: (m) => `${m} min`,
  spanHours: (h, m) => (m > 0 ? `${h} h ${m} min` : `${h} h`),
  spanDays: (d, h) => (h > 0 ? `${d} d ${h} h` : `${d} d`),

  backToList: "Back to incidents",
  notFound: (id) => `There is no incident ${id}. The number may be mistyped. Open the incident list and pick again.`,
  detailOpen: (opened) => `Open since ${opened}.`,
  detailClosed: (opened, closed) => `Opened at ${opened}, closed at ${closed}.`,

  fieldStatus: "STATUS",
  fieldTarget: "TARGET",
  fieldStage: "STAGE",
  fieldCheck: "CHECK",
  fieldOpened: "OPENED",
  fieldClosed: "CLOSED",
  fieldDuration: "LASTED",

  whatHeading: "What happened",
  causeLabel: "POSSIBLE CAUSE",

  recheck: "Check again",
  rechecking: "Checking…",
  rechecked: (time) => `Checked again at ${time}.`,
  recheckHint: "Runs the checks right now. If the check passes, the incident closes by itself.",
  recheckFailed: "Could not check again. Press Check again once more; if it still fails, look at the tpan log.",

  evidenceOpenHeading: "Evidence when it opened",
  evidenceCloseHeading: "Evidence when it closed",
  currentHeading: "Current result",
  currentChecked: (time) => `Checked at ${time}`,
  noCurrent: "This check no longer runs, so there is no result newer than the one from when it opened.",
  noEvidence: "This check recorded no command, so there is no output to show.",
  closedGoneNote: "The incident closed because its target is no longer checked (for example the site was deleted), so there is no evidence from when it closed.",

  jobsHeading: "Jobs that ran to fix it",
  noJobs: "No fix has run for this incident yet.",
  jobsLabel: "Fix jobs",
  jobStatus: {
    running: "running",
    succeeded: "done",
    rolled_back: "failed, rolled back",
    undo_failed: "failed, rollback failed too",
  },

  fixHeading: "Ways to fix it",
  fixHint: "Pick the way you prefer. The machine only changes when you approve the preview.",
  fixHintUser: "The commands below run on the server. Give them to whoever administers the server.",

  autoHeading: "Automatic fix",
  autoBody: "TPAN builds a plan from steps it already has. You see every step before the machine changes.",
  autoDrift: "This file differs from what TPAN wrote. The preview shows each line that differs between the file on the machine and TPAN's version.",
  autoPreview: "Automatic fix · preview",
  autoPreviewing: "Building the preview…",
  autoPreviewFailed: "Could not build the preview of this fix.",

  manualHeading: "Fix by hand",
  manualNone: "This check gave no commands. Read the evidence below to see where it breaks.",

  acceptHeading: "Keep the hand edit as the standard",
  acceptBody: "You edited this file on the machine and want to keep it. TPAN remembers its current content as the standard and stops reporting drift. The file does not change.",
  accept: "Keep the hand edit",
  acceptTitle: "Keep the hand edit as the standard?",
  acceptDescription: "The file stays as it is. TPAN only remembers its current content so it stops reporting drift.",
  acceptConfirm: "Keep this version",
  accepted: "Hand edit kept. Press Check again so the check reports again.",
  acceptFailed: "Could not keep the hand edit.",

  aiHeading: "Ask AI",
  aiBody: "AI reads the finding and the masked evidence, then diagnoses. The AI drawer opens with a prepared question; you see what will be sent and press Send yourself. AI only proposes, the machine changes only when you approve.",
  aiButton: "Ask AI",

  diagnose: "Diagnose",
  diagnoseHint: "Opens the AI drawer with a prepared question. You see what will be sent, then press Send yourself.",
  diagnoseQuestion: (title) => `Diagnose the incident “${title}”: what could the cause be, and how should I fix it.`,
});
