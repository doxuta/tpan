// Chuỗi của Màn Đăng nhập (không có khung trang). Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Đăng nhập",
  summary: "Nhập mật khẩu quản trị của máy này.",
};

export const strings = defineStrings(vi, {
  title: "Log in",
  summary: "Enter the admin password of this machine.",
});
