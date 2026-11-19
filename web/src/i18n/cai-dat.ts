// Chuỗi của Màn Cài đặt. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Cài đặt",
  summary: "Mật khẩu quản trị, các phiên đang đăng nhập, ngôn ngữ, sáng hoặc tối.",
};

export const strings = defineStrings(vi, {
  title: "Settings",
  summary: "Admin password, active sessions, language, light or dark.",
});
