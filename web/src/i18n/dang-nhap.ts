// Chuỗi của Màn Đăng nhập (không có khung trang). Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Đăng nhập",
  summary: "Nhập mật khẩu quản trị của máy này.",
  passwordLabel: "Mật khẩu",
  submit: "Đăng nhập",
  submitting: "Đang đăng nhập…",

  // Lỗi: chuyện gì xảy ra, rồi làm gì tiếp. Không nói còn mấy lần thử.
  wrongPassword: "Sai mật khẩu. Gõ lại. Quên mật khẩu thì xem lệnh ở dưới.",
  locked: (until: string, minutes: number) =>
    `Sai mật khẩu quá nhiều lần nên địa chỉ IP này bị khoá tới ${until} (còn khoảng ${minutes} phút). Chờ tới lúc đó rồi đăng nhập lại.`,
  notSetUp: "Máy này chưa có mật khẩu quản trị. Chạy lệnh này trên máy chủ để đặt:",
  noSession: "Mật khẩu đúng nhưng trình duyệt không giữ phiên đăng nhập. Kiểm tra trình duyệt có đang chặn cookie của trang này không, rồi đăng nhập lại.",

  forgotTitle: "Quên mật khẩu",
  forgotBody: "Chạy lệnh này trên máy chủ để đặt mật khẩu mới:",
  demoTitle: "Chế độ demo",
  demoBody: "Mật khẩu được in ra terminal đang chạy tpan serve --demo, ở dòng bắt đầu bằng “Mật khẩu đăng nhập demo:”.",
};

export const strings = defineStrings(vi, {
  title: "Log in",
  summary: "Enter the admin password of this machine.",
  passwordLabel: "Password",
  submit: "Log in",
  submitting: "Logging in…",

  wrongPassword: "Wrong password. Type it again. If you forgot it, see the command below.",
  locked: (until, minutes) =>
    `Too many wrong passwords, so this IP address is locked until ${until} (about ${minutes} more minutes). Wait until then and log in again.`,
  notSetUp: "This machine has no admin password yet. Run this command on the server to set one:",
  noSession: "The password is right but the browser did not keep the login session. Check whether the browser blocks cookies for this page, then log in again.",

  forgotTitle: "Forgot the password",
  forgotBody: "Run this command on the server to set a new password:",
  demoTitle: "Demo mode",
  demoBody: "The password is printed in the terminal running tpan serve --demo, on the line starting with “Demo login password:”.",
});
