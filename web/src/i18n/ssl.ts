// Chuỗi của Màn SSL (mục 6.3 của KE-HOACH.md). Giọng chữ: mục 8 của
// docs/GIAO-DIEN.md. Đợt 0 chỉ có tiêu đề; thợ của màn viết phần còn lại.

import { defineStrings } from "./index";

const vi = {
  title: "SSL",
  soon: "Màn này đang được làm.",
};

export const strings = defineStrings(vi, {
  title: "SSL",
  soon: "This screen is being built.",
});
