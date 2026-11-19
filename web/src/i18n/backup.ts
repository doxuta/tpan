// Chuỗi của màn Backup (mục 6.5 của KE-HOACH.md). Màn giữ chỗ của đợt 0; thợ
// giao diện thay bằng bản thật. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Backup",
  summary: "Bản sao của site và cơ sở dữ liệu, tạo tay hoặc theo lịch.",
};

export const strings = defineStrings(vi, {
  title: "Backups",
  summary: "Copies of sites and their databases, made by hand or on a schedule.",
});
