// Chuỗi của Màn Cài đặt. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Cài đặt",
  summary: "Mật khẩu quản trị, các phiên đang đăng nhập, ngôn ngữ, sáng hoặc tối.",

  // Mật khẩu quản trị
  pwHeading: "Mật khẩu quản trị",
  pwIntro: (min: number) => `Mật khẩu mới dài ít nhất ${min} ký tự. Đổi xong, mọi phiên khác bị đăng xuất.`,
  pwCurrent: "Mật khẩu hiện tại",
  pwNew: "Mật khẩu mới",
  pwRepeat: "Nhập lại mật khẩu mới",
  pwSubmit: "Đổi mật khẩu",
  pwBusy: "Đang đổi…",
  pwMismatch: "Hai ô mật khẩu mới chưa giống nhau. Gõ lại ô nhập lại.",
  pwTooShort: (min: number) => `Mật khẩu mới ngắn hơn ${min} ký tự. Thêm ký tự rồi bấm lại.`,
  pwWrongCurrent: "Mật khẩu hiện tại chưa đúng. Gõ lại. Gõ sai nhiều lần thì địa chỉ IP này bị khoá một lúc.",
  pwDone: "Đã đổi mật khẩu. Các phiên khác đã bị đăng xuất; phiên này vẫn đăng nhập.",

  // Các phiên
  ssHeading: "Các phiên đang đăng nhập",
  ssIntro: "Mỗi trình duyệt đã đăng nhập là một phiên. Phiên không dùng 12 giờ thì tự hết hạn.",
  ssTable: "Các phiên đang đăng nhập",
  ssColIp: "IP",
  ssColCreated: "Tạo lúc",
  ssColLastSeen: "Dùng lần cuối",
  ssColAction: "Thao tác",
  ssCurrent: "đang dùng",
  ssCurrentHint: "Đăng xuất bằng mục Đăng xuất ở menu.",
  ssSignOut: "Đăng xuất phiên này",
  ssSignOutLabel: (ip: string, created: string) => `Đăng xuất phiên từ ${ip}, tạo lúc ${created}`,
  ssSigningOut: "Đang đăng xuất…",
  ssEmpty: "Chưa có phiên nào.",

  // Ngôn ngữ, giao diện
  langHeading: "Ngôn ngữ",
  langVi: "Tiếng Việt",
  langEn: "English",
  themeHeading: "Giao diện",
  themeSystem: "Theo hệ điều hành",
  themeSystemNow: (dark: boolean) => `Đang theo hệ điều hành, hiện là ${dark ? "tối" : "sáng"}.`,

  // AI và MCP
  aiLater: "Có ở bản sau: nối bộ não AI và cho agent khác nối vào qua MCP.",
};

export const strings = defineStrings(vi, {
  title: "Settings",
  summary: "Admin password, active sessions, language, light or dark.",

  pwHeading: "Admin password",
  pwIntro: (min) => `The new password must be at least ${min} characters. After the change, every other session is logged out.`,
  pwCurrent: "Current password",
  pwNew: "New password",
  pwRepeat: "New password again",
  pwSubmit: "Change password",
  pwBusy: "Changing…",
  pwMismatch: "The two new passwords are different. Type the second one again.",
  pwTooShort: (min) => `The new password is shorter than ${min} characters. Add more characters and try again.`,
  pwWrongCurrent: "The current password is wrong. Type it again. Too many wrong tries lock this IP address for a while.",
  pwDone: "Password changed. The other sessions are logged out; this one stays logged in.",

  ssHeading: "Active sessions",
  ssIntro: "Each browser that is logged in is one session. A session unused for 12 hours expires.",
  ssTable: "Active sessions",
  ssColIp: "IP",
  ssColCreated: "Created",
  ssColLastSeen: "Last used",
  ssColAction: "Action",
  ssCurrent: "in use",
  ssCurrentHint: "Log out with Log out in the menu.",
  ssSignOut: "Log out this session",
  ssSignOutLabel: (ip, created) => `Log out the session from ${ip}, created ${created}`,
  ssSigningOut: "Logging out…",
  ssEmpty: "No sessions.",

  langHeading: "Language",
  langVi: "Tiếng Việt",
  langEn: "English",
  themeHeading: "Appearance",
  themeSystem: "Follow the system",
  themeSystemNow: (dark) => `Following the system, currently ${dark ? "dark" : "light"}.`,

  aiLater: "Coming in a later version: connect an AI brain and let other agents connect through MCP.",
});
