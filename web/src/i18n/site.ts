// Chuỗi của Màn Site: danh sách site và trang một site. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  listTitle: "Site",
  listSummary: "Các site WordPress trên máy này.",
  detailSummary: "Đường đi request của site này và bằng chứng của từng chặng.",
};

export const strings = defineStrings(vi, {
  listTitle: "Sites",
  listSummary: "The WordPress sites on this machine.",
  detailSummary: "The request path of this site and the evidence for each stage.",
});
