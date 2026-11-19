// Chuỗi của Màn Cơ sở dữ liệu. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Cơ sở dữ liệu",
  summary: "Mỗi site có cơ sở dữ liệu và user riêng.",
  tableName: "Các cơ sở dữ liệu",

  colDb: "DB",
  colUser: "User",
  colSite: "Site",
  colSize: "Dung lượng",
  colTables: "Số bảng",
  unreadable: "không đọc được",

  emptyText: "Chưa có cơ sở dữ liệu nào vì chưa có site.",
  emptyLink: "Tạo site đầu tiên",

  later: "Đổi mật khẩu, xuất và nhập cơ sở dữ liệu có ở bản sau. Bản này chỉ xem.",
};

export const strings = defineStrings(vi, {
  title: "Databases",
  summary: "Each site has its own database and user.",
  tableName: "Databases",

  colDb: "DB",
  colUser: "User",
  colSite: "Site",
  colSize: "Size",
  colTables: "Tables",
  unreadable: "cannot be read",

  emptyText: "No databases yet because there are no sites.",
  emptyLink: "Create the first site",

  later: "Changing passwords, exporting and importing databases come in a later version. This version is view only.",
});
