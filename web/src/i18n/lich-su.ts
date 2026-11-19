// Chuỗi của Màn Lịch sử: danh sách job và trang một job. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Lịch sử",
  summary: "Các việc đã chạy trên máy, mới nhất ở trên.",
  jobSummary: "Từng bước của job này, output và bằng chứng.",

  // ---- danh sách ----
  empty: "Chưa có việc nào chạy trên máy. Việc đầu tiên bạn chạy sẽ hiện ở đây.",
  listLabel: "Các job đã chạy",
  evidenceLink: "bằng chứng",
  evidenceOf: (id: string) => `Bằng chứng của job ${id}`,

  // ---- trạng thái job ----
  status: {
    running: "đang chạy",
    succeeded: "xong",
    rolled_back: "lỗi, đã hoàn tác",
    undo_failed: "lỗi, hoàn tác cũng lỗi",
  },
  statusWithTime: (status: string, duration: string) => `${status} · ${duration}`,
  seconds: (s: string) => `${s} giây`,
  minutes: (m: number, s: number) => `${m} phút ${s} giây`,
  millis: (ms: number) => `${ms} ms`,

  // ---- trang một job ----
  fieldStatus: "TRẠNG THÁI",
  fieldJob: "MÃ JOB",
  fieldTarget: "ĐỐI TƯỢNG",
  fieldStarted: "BẮT ĐẦU",
  fieldDuration: "THỜI GIAN",
  liveNote: "Đang chạy. Log tự cập nhật, bạn không cần tải lại trang.",
  jobDoneAnnounce: (status: string) => `Job đã dừng: ${status}`,
  stepAnnounce: (n: number, id: string) => `Bước ${n}: ${id}`,

  failedAt: (n: number, id: string) =>
    `Bước ${n} hỏng: ${id}. tpan đã hoàn tác các bước đã chạy, máy về như trước khi chạy. Xem output của bước đó, sửa nguyên nhân rồi chạy lại.`,
  failedUndoAt: (n: number, id: string) =>
    `Bước ${n} hỏng: ${id}, và hoàn tác cũng lỗi nên máy có thể đang dở dang. Xem phần Hoàn tác bên dưới, kiểm máy rồi hãy chạy lại.`,
  failedVerify: "Các bước chạy xong nhưng phép kiểm chứng báo lỗi, nên tpan đã hoàn tác. Xem phần Kiểm chứng bên dưới.",
  failedUndoVerify: "Phép kiểm chứng báo lỗi, và hoàn tác cũng lỗi nên máy có thể đang dở dang. Xem phần Hoàn tác bên dưới, kiểm máy rồi hãy chạy lại.",
  failedOther: "Job dừng giữa chừng. Lý do ghi ở dưới.",
  errorLabel: "LÝ DO DỪNG",

  stepsHeading: "Các bước",
  noSteps: "Job này chưa chạy bước nào.",
  openAll: "Mở hết",
  closeAll: "Gập hết",
  stepNumber: (n: number) => `Bước ${n}`,
  stepState: {
    running: "đang chạy",
    skipped: "đã đúng, bỏ qua",
    done: "xong",
    failed: "hỏng",
  },
  stepLabel: (n: number, id: string, state: string) => `Bước ${n}, ${id}: ${state}`,
  noLog: "Bước này không chạy lệnh nào.",
  hiddenLines: (n: number) => `${n} dòng đầu đã ẩn.`,
  showAllLines: "Hiện hết",

  undoHeading: "Hoàn tác, chạy ngược",
  undoHint: "Có bước hỏng, nên tpan gỡ từng bước đã chạy, từ cuối về đầu.",
  undoState: {
    running: "đang hoàn tác",
    done: "đã hoàn tác",
    failed: "hoàn tác lỗi",
  },
  undoLabel: (id: string, state: string) => `Hoàn tác ${id}: ${state}`,

  checksHeading: "Kiểm chứng",
  checksHint: "Sau khi các bước chạy xong, tpan kiểm lại máy. Bấm vào dấu để xem lệnh và output.",
  checkLabel: (what: string, status: string) => `${what}: ${status}`,
  noEvidence: "Phép kiểm này không chạy lệnh nào, nên không có output để xem.",

  streamLost: "Mất kết nối với luồng log. Job vẫn chạy trên máy; nối lại để xem tiếp.",
  reconnect: "Nối lại",

  // ---- tài khoản quản trị WordPress (hiện một lần) ----
  secretLabel: "TÀI KHOẢN QUẢN TRỊ WORDPRESS",
  secretTitle: "Lưu tài khoản này ngay",
  secretBody: "tpan không giữ mật khẩu này và sẽ không hiện lại. Chép vào trình quản lý mật khẩu trước khi rời trang.",
  secretUrl: "Địa chỉ",
  secretUser: "Tài khoản",
  secretPassword: "Mật khẩu",
  secretCopy: "Chép",
  secretCopyOf: (what: string) => `Chép ${what}`,
  secretDismiss: "Đã lưu, ẩn đi",

  // Nút "Giải thích" trên bước lỗi: mở ngăn AI với câu hỏi soạn sẵn, người xem trước rồi tự gửi.
  explain: "Giải thích",
  explainStepHint: "Mở ngăn AI với câu hỏi soạn sẵn. Bạn xem thứ sẽ gửi rồi mới bấm Gửi.",
  explainStepQuestion: (step: string) => `Giải thích vì sao bước ${step} lỗi và mình nên làm gì.`,
  explainStepLabel: (step: string) => `Bước ${step}`,
};

export const strings = defineStrings(vi, {
  title: "History",
  summary: "Jobs that ran on this machine, newest first.",
  jobSummary: "Every step of this job, its output and evidence.",

  empty: "Nothing has run on this machine yet. The first thing you run will show up here.",
  listLabel: "Jobs that ran",
  evidenceLink: "evidence",
  evidenceOf: (id) => `Evidence for job ${id}`,

  status: {
    running: "running",
    succeeded: "done",
    rolled_back: "failed, rolled back",
    undo_failed: "failed, rollback failed too",
  },
  statusWithTime: (status, duration) => `${status} · ${duration}`,
  seconds: (s) => `${s}s`,
  minutes: (m, s) => `${m}m ${s}s`,
  millis: (ms) => `${ms} ms`,

  fieldStatus: "STATUS",
  fieldJob: "JOB ID",
  fieldTarget: "TARGET",
  fieldStarted: "STARTED",
  fieldDuration: "DURATION",
  liveNote: "Running. The log updates by itself, no need to reload the page.",
  jobDoneAnnounce: (status) => `Job stopped: ${status}`,
  stepAnnounce: (n, id) => `Step ${n}: ${id}`,

  failedAt: (n, id) =>
    `Step ${n} failed: ${id}. tpan rolled back the steps that had run, so the machine is as it was before. Read that step's output, fix the cause, then run again.`,
  failedUndoAt: (n, id) =>
    `Step ${n} failed: ${id}, and the rollback failed too, so the machine may be half-changed. Read the Rollback section below and check the machine before running again.`,
  failedVerify: "The steps finished but a verification check failed, so tpan rolled back. See the Verification section below.",
  failedUndoVerify: "A verification check failed and the rollback failed too, so the machine may be half-changed. Read the Rollback section below and check the machine before running again.",
  failedOther: "The job stopped part-way. The reason is below.",
  errorLabel: "WHY IT STOPPED",

  stepsHeading: "Steps",
  noSteps: "This job has not run any step.",
  openAll: "Open all",
  closeAll: "Close all",
  stepNumber: (n) => `Step ${n}`,
  stepState: {
    running: "running",
    skipped: "already right, skipped",
    done: "done",
    failed: "failed",
  },
  stepLabel: (n, id, state) => `Step ${n}, ${id}: ${state}`,
  noLog: "This step ran no command.",
  hiddenLines: (n) => `${n} earlier lines hidden.`,
  showAllLines: "Show all",

  undoHeading: "Rollback, running backwards",
  undoHint: "A step failed, so tpan undoes each step that ran, from the last back to the first.",
  undoState: {
    running: "rolling back",
    done: "rolled back",
    failed: "rollback failed",
  },
  undoLabel: (id, state) => `Rollback ${id}: ${state}`,

  checksHeading: "Verification",
  checksHint: "Once the steps finish, tpan checks the machine again. Press a mark to see the command and its output.",
  checkLabel: (what, status) => `${what}: ${status}`,
  noEvidence: "This check ran no command, so there is no output to show.",

  streamLost: "Lost the connection to the log stream. The job keeps running on the machine; reconnect to follow it.",
  reconnect: "Reconnect",

  secretLabel: "WORDPRESS ADMIN ACCOUNT",
  secretTitle: "Save this account now",
  secretBody: "tpan does not keep this password and will not show it again. Copy it into your password manager before you leave this page.",
  secretUrl: "Address",
  secretUser: "Username",
  secretPassword: "Password",
  secretCopy: "Copy",
  secretCopyOf: (what) => `Copy ${what}`,
  secretDismiss: "Saved, hide it",

  explain: "Explain",
  explainStepHint: "Opens the AI drawer with a prepared question. You see what will be sent, then press Send yourself.",
  explainStepQuestion: (step) => `Explain why step ${step} failed and what I should do.`,
  explainStepLabel: (step) => `Step ${step}`,
});
