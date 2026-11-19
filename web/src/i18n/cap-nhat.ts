// Chuỗi của màn Cập nhật (mục 6.11 và mục 10 của KE-HOACH.md). Màn giữ chỗ
// của đợt 0 M12; thợ giao diện viết tiếp.

import { defineStrings } from "./index";

const vi = {
  title: "Cập nhật",
  summary: "Gói của stack có bản mới, bản vá bảo mật tự động, nâng MariaDB và cập nhật chính TPAN.",
};

export const strings = defineStrings(vi, {
  title: "Updates",
  summary: "Stack packages with new versions, automatic security patches, MariaDB upgrades and updating TPAN itself.",
});
