// Chuỗi của Bản xem trước của một Plan (dùng trong Tạo site và Xoá site). Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  heading: "Xem trước",
  summary: "Những gì sẽ đổi trên máy. Chưa có gì chạy cho tới khi bạn bấm chạy.",

  // ---- tóm tắt ----
  summaryLabel: "TÓM TẮT",
  stepsToRun: "SẼ CHẠY",
  stepsToSkip: "BỎ QUA",
  filesChanged: "FILE ĐỔI",
  targetLabel: "ĐỐI TƯỢNG",
  stepsUnit: (n: number) => `${n} bước`,
  filesUnit: (n: number) => `${n} file`,
  skipNote: "Máy đã đúng sẵn, không cần chạy lại.",

  // ---- phạm vi ảnh hưởng ----
  scopeLabel: "PHẠM VI ẢNH HƯỞNG",
  scopeReload: "Dịch vụ sẽ reload:",
  scopeNoReload: "Không reload dịch vụ nào.",
  scopeDanger: (n: number) => `${n} bước nguy hiểm, có thể làm gián đoạn dịch vụ hoặc xoá dữ liệu.`,
  scopeNoDanger: "Không có bước nguy hiểm.",

  // ---- từng bước ----
  stepsLabel: "CÁC BƯỚC",
  stepNumber: (n: number) => `Bước ${n}`,
  danger: "nguy hiểm",
  willSkip: "đã đúng, bỏ qua",
  noUndo: (why: string) => `Không hoàn tác được: ${why}`,
  checkError: (msg: string) => `Lúc xem trước chưa kiểm được bước này (${msg}). Nó vẫn sẽ chạy.`,
  commandsLabel: "LỆNH SẼ CHẠY",
  filesLabel: "FILE THAY ĐỔI",

  // ---- diff ----
  fileNew: "file mới",
  fileEdit: "sửa",
  fileDelete: "xoá file",
  diffStat: (added: number, removed: number) => `+${added} −${removed}`,
  fileEmptyNew: "File mới, nội dung rỗng.",
  fileUnchanged: "Nội dung không đổi.",
  fileBinary: "File này không phải chữ, nên không hiện diff.",
  gap: (n: number) => `… ${n} dòng không đổi`,
  diffOf: (path: string) => `Diff của ${path}`,

  // ---- nút ----
  run: (n: number) => `Chạy ${n} bước`,
  running: "Đang gửi lệnh…",
  nothingToRun: "Máy đã đúng hết, không có bước nào để chạy.",
  back: "Quay lại",
  confirmTitle: (n: number) => `Chạy ${n} bước này?`,
  confirmBody: (d: number) => `${d} bước dưới đây có thể làm gián đoạn dịch vụ hoặc xoá dữ liệu.`,
  confirmRun: (n: number) => `Chạy ${n} bước`,
};

export const strings = defineStrings(vi, {
  heading: "Preview",
  summary: "What will change on the machine. Nothing runs until you press run.",

  summaryLabel: "SUMMARY",
  stepsToRun: "WILL RUN",
  stepsToSkip: "SKIPPED",
  filesChanged: "FILES CHANGED",
  targetLabel: "TARGET",
  stepsUnit: (n) => `${n} ${n === 1 ? "step" : "steps"}`,
  filesUnit: (n) => `${n} ${n === 1 ? "file" : "files"}`,
  skipNote: "The machine is already right, so these do not run again.",

  scopeLabel: "WHAT THIS AFFECTS",
  scopeReload: "Services that will reload:",
  scopeNoReload: "No service reloads.",
  scopeDanger: (n) => `${n} dangerous ${n === 1 ? "step" : "steps"}, which can interrupt a service or delete data.`,
  scopeNoDanger: "No dangerous steps.",

  stepsLabel: "STEPS",
  stepNumber: (n) => `Step ${n}`,
  danger: "dangerous",
  willSkip: "already right, skipped",
  noUndo: (why) => `Cannot be undone: ${why}`,
  checkError: (msg) => `This step could not be checked during the preview (${msg}). It will still run.`,
  commandsLabel: "COMMANDS THAT WILL RUN",
  filesLabel: "FILES CHANGED",

  fileNew: "new file",
  fileEdit: "edit",
  fileDelete: "delete file",
  diffStat: (added, removed) => `+${added} −${removed}`,
  fileEmptyNew: "New file, empty.",
  fileUnchanged: "Content is the same.",
  fileBinary: "This file is not text, so no diff is shown.",
  gap: (n) => `… ${n} unchanged ${n === 1 ? "line" : "lines"}`,
  diffOf: (path) => `Diff of ${path}`,

  run: (n) => `Run ${n} ${n === 1 ? "step" : "steps"}`,
  running: "Sending the command…",
  nothingToRun: "The machine is already right, so there is nothing to run.",
  back: "Go back",
  confirmTitle: (n) => `Run ${n === 1 ? "this step" : `these ${n} steps`}?`,
  confirmBody: (d) => `${d === 1 ? "The step below" : `The ${d} steps below`} can interrupt a service or delete data.`,
  confirmRun: (n) => `Run ${n} ${n === 1 ? "step" : "steps"}`,
});
