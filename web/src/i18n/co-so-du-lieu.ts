// Chuỗi của Màn Cơ sở dữ liệu. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Cơ sở dữ liệu",
  summary: "Mỗi site có cơ sở dữ liệu và user riêng.",
};

export const strings = defineStrings(vi, {
  title: "Databases",
  summary: "Each site has its own database and user.",
});
