// Chuỗi của màn Thùng rác (mục 6.4 của KE-HOACH.md). Giọng chữ: mục 8 của
// docs/GIAO-DIEN.md. Hợp đồng M6 đợt 0: chỉ có tiêu đề; thợ giao diện thêm phần còn lại.

import { defineStrings } from "./index";

const vi = {
  title: "Thùng rác",
  summary: "Site đã xoá nằm ở đây, khôi phục được cho tới hạn xoá hẳn.",
};

const en: typeof vi = {
  title: "Trash",
  summary: "Deleted sites stay here and can be restored until they are permanently deleted.",
};

export const strings = defineStrings(vi, en);
