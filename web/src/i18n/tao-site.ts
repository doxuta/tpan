// Chuỗi của màn Tạo site. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Tạo site",
  summary: "Nhập domain, xem trước từng bước, rồi chạy.",

  formHeading: "Site mới",
  domainLabel: "Domain",
  domainHint: "Tên miền đầy đủ của site, ví dụ shop.example.com.",
  domainRequired: "Nhập domain của site, ví dụ shop.example.com.",
  titleLabel: "Tiêu đề site (tuỳ chọn)",
  titleHint: "Hiện trên đầu trang WordPress. Để trống thì dùng domain.",
  emailLabel: "Email quản trị",
  emailHint: "Tài khoản quản trị WordPress dùng email này. Mật khẩu của nó hiện một lần sau khi tạo xong.",
  emailRequired: "Nhập email quản trị, ví dụ admin@example.com.",

  preview: "Xem trước",
  previewing: "Đang dựng bản xem trước…",
  errorHeading: "Chưa dựng được bản xem trước",
  checksHeading: "Điều kiện chưa đạt",
};

export const strings = defineStrings(vi, {
  title: "Create a site",
  summary: "Enter a domain, preview every step, then run it.",

  formHeading: "New site",
  domainLabel: "Domain",
  domainHint: "The full domain name of the site, for example shop.example.com.",
  domainRequired: "Enter the domain of the site, for example shop.example.com.",
  titleLabel: "Site title (optional)",
  titleHint: "Shown at the top of the WordPress pages. Leave it empty to use the domain.",
  emailLabel: "Admin email",
  emailHint: "The WordPress admin account uses this email. Its password is shown once, after the site is created.",
  emailRequired: "Enter the admin email, for example admin@example.com.",

  preview: "Preview",
  previewing: "Building the preview…",
  errorHeading: "Could not build the preview",
  checksHeading: "Conditions not met",
});
