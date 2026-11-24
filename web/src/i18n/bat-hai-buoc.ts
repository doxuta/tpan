// Chuỗi của trang Bật đăng nhập hai bước bắt buộc (/bat-hai-buoc, M20). Trang này
// không có khung chung: người dùng bị chính sách của máy bắt bật hai bước chỉ vào
// được đây (và Đăng nhập). Chuỗi của chính luồng bật (mã QR, ô mã, mã dự phòng) ở
// tai-khoan.ts vì trang Tài khoản dùng chung. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Bật đăng nhập hai bước",
  summary: "Máy này bắt buộc hai bước với tài khoản của bạn. Bật xong mới dùng được TPAN.",
  loggedAs: (name: string) => `Đang đăng nhập là ${name}.`,
  whyTitle: "Vì sao phải bật",
  whyBody: "Quản trị viên đã đặt chính sách: tài khoản như của bạn phải có thêm mã từ điện thoại khi đăng nhập. Mật khẩu một mình không còn đủ để vào.",
  howTitle: "Cần gì",
  howBody: "Một app xác thực trên điện thoại (Google Authenticator, Aegis, 1Password...). Chưa có thì cài trước. Làm hai bước dưới đây mất chừng một phút.",
  logout: "Đăng xuất",
  loggingOut: "Đang đăng xuất…",
  logoutFailed: "Chưa đăng xuất được. Thử bấm lại; vẫn không được thì đóng trình duyệt.",
  loading: "Đang tải…",
  savedHint: "Bấm Đã lưu mã khi bạn đã cất mười mã này vào chỗ an toàn. TPAN sẽ đưa bạn về trang chủ.",
};

export const strings = defineStrings(vi, {
  title: "Turn on two-step login",
  summary: "This machine requires two-step login for your account. You can use TPAN once it is on.",
  loggedAs: (name) => `Logged in as ${name}.`,
  whyTitle: "Why you must turn it on",
  whyBody: "An admin set a policy: accounts like yours need a code from a phone as well when logging in. The password alone no longer gets you in.",
  howTitle: "What you need",
  howBody: "An authenticator app on your phone (Google Authenticator, Aegis, 1Password...). Install one first if you have none. The two steps below take about a minute.",
  logout: "Log out",
  loggingOut: "Logging out…",
  logoutFailed: "Could not log out. Try again; if it still fails, close the browser.",
  loading: "Loading…",
  savedHint: "Press I saved the codes once you have kept these ten codes somewhere safe. TPAN then takes you to the home page.",
});
