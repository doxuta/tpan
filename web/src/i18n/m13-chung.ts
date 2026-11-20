// Chuỗi dùng chung của phần M13 trên site, DB, thùng rác, cron: nhãn "tạm
// khoá", lý do khoá, tên bảng điều kiện chưa đạt. Giọng chữ: mục 8 của
// docs/GIAO-DIEN.md. Chuỗi riêng của từng màn nằm ở file chuỗi của màn đó.

import { defineStrings } from "./index";

const vi = {
  /** Nhãn của dấu xám khi site hay DB đang tạm khoá (thay "chưa kiểm được"). */
  suspendedState: "tạm khoá",
  /** Vì sao bị khoá: "self" là admin khoá riêng, "user" là khoá theo user chủ. */
  suspendedBy: { self: "khoá riêng", user: "khoá theo user" },
  suspendedByHint: {
    self: "Admin đã khoá riêng thứ này.",
    user: "Thứ này bị khoá vì user chủ của nó đang bị tạm khoá.",
  },
  dbLocked: "DB khoá",
  colOwner: "Chủ",
  checksHeading: "Điều kiện chưa đạt",
  previewing: "Đang dựng bản xem trước…",
  ownerLabel: (name: string) => `chủ: ${name}`,
};

export const strings = defineStrings(vi, {
  suspendedState: "suspended",
  suspendedBy: { self: "locked on its own", user: "locked with its user" },
  suspendedByHint: {
    self: "An admin locked this one on its own.",
    user: "This is locked because its owner is suspended.",
  },
  dbLocked: "DB locked",
  colOwner: "Owner",
  checksHeading: "Conditions not met",
  previewing: "Building the preview…",
  ownerLabel: (name) => `owner: ${name}`,
});
