// Chuỗi của Bản xem trước của một Plan (dùng trong Tạo site và Xoá site). Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  heading: "Xem trước",
  summary: "Những gì sẽ đổi trên máy. Chưa có gì chạy cho tới khi bạn bấm chạy.",
};

export const strings = defineStrings(vi, {
  heading: "Preview",
  summary: "What will change on the machine. Nothing runs until you press run.",
});
