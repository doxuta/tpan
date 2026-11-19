// Chuỗi của Màn Bảng điều khiển (trang chủ). Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Bảng điều khiển",
  summary: "Đường đi request của từng site, sự cố đang mở và số liệu máy.",
};

export const strings = defineStrings(vi, {
  title: "Dashboard",
  summary: "The request path of every site, open incidents and machine metrics.",
});
