// Chuỗi của Màn Bảo mật (mục 6.8 của KE-HOACH.md). Giọng chữ: mục 8 của
// docs/GIAO-DIEN.md. Đợt 0 chỉ có tiêu đề; thợ của màn viết phần còn lại.

import { defineStrings } from "./index";

const vi = {
  title: "Bảo mật",
  soon: "Màn này đang được làm.",
};

export const strings = defineStrings(vi, {
  title: "Security",
  soon: "This screen is being built.",
});
