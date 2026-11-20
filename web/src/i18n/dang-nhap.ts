// Chuỗi của Màn Đăng nhập (không có khung trang). Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Đăng nhập",
  summary: "Nhập tên đăng nhập và mật khẩu của bạn trên máy này.",
  nameLabel: "Tên đăng nhập",
  nameHint: "Để trống là admin, tài khoản quản trị của máy.",
  passwordLabel: "Mật khẩu",
  submit: "Đăng nhập",
  submitting: "Đang đăng nhập…",

  // Lỗi: chuyện gì xảy ra, rồi làm gì tiếp. Không nói còn mấy lần thử.
  wrongPassword: "Sai tên đăng nhập hoặc mật khẩu. Gõ lại. Quên mật khẩu thì xem phần ở dưới.",
  suspended: "Tài khoản này đang bị tạm khoá nên chưa đăng nhập được. Nhờ quản trị viên mở khoá rồi đăng nhập lại.",
  locked: (until: string, minutes: number) =>
    `Sai mật khẩu quá nhiều lần nên địa chỉ IP này bị khoá tới ${until} (còn khoảng ${minutes} phút). Chờ tới lúc đó rồi đăng nhập lại.`,
  notSetUp: "Máy này chưa có mật khẩu quản trị. Chạy lệnh này trên máy chủ để đặt:",
  noSession: "Mật khẩu đúng nhưng trình duyệt không giữ phiên đăng nhập. Kiểm tra trình duyệt có đang chặn cookie của trang này không, rồi đăng nhập lại.",

  forgotTitle: "Quên mật khẩu",
  forgotBody: "Tài khoản admin: chạy lệnh này trên máy chủ để đặt mật khẩu mới:",
  forgotUser: "Tài khoản khác: nhờ quản trị viên vào màn Người dùng và bấm Đổi mật khẩu.",
  demoTitle: "Chế độ demo",
  demoBody: "Mật khẩu được in ra terminal đang chạy tpan serve --demo, ở dòng bắt đầu bằng “Mật khẩu đăng nhập demo:”.",
};

export const strings = defineStrings(vi, {
  title: "Log in",
  summary: "Enter your username and password for this machine.",
  nameLabel: "Username",
  nameHint: "Leave it empty for admin, the admin account of this machine.",
  passwordLabel: "Password",
  submit: "Log in",
  submitting: "Logging in…",

  wrongPassword: "Wrong username or password. Type them again. If you forgot the password, see the section below.",
  suspended: "This account is suspended, so you cannot log in yet. Ask an admin to unsuspend it, then log in again.",
  locked: (until, minutes) =>
    `Too many wrong passwords, so this IP address is locked until ${until} (about ${minutes} more minutes). Wait until then and log in again.`,
  notSetUp: "This machine has no admin password yet. Run this command on the server to set one:",
  noSession: "The password is right but the browser did not keep the login session. Check whether the browser blocks cookies for this page, then log in again.",

  forgotTitle: "Forgot the password",
  forgotBody: "The admin account: run this command on the server to set a new password:",
  forgotUser: "Any other account: ask an admin to open the Users screen and press Change password.",
  demoTitle: "Demo mode",
  demoBody: "The password is printed in the terminal running tpan serve --demo, on the line starting with “Demo login password:”.",
});
