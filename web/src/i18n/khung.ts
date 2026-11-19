// Chuỗi của khung trang: thanh đầu trang, menu chính, dấu trạng thái, các
// thành phần dùng chung (nút chép, hộp hỏi lại) và câu lỗi chung. Giọng chữ
// theo mục 8 của docs/GIAO-DIEN.md. Tên mục menu cũng nằm ở đây vì menu là
// một phần của khung; mốc sau thêm màn thì thêm tên vào nhóm menu dưới đây.

import { defineStrings } from "./index";

const vi = {
  homeLabel: "TPAN, về Bảng điều khiển",
  machineState: (s: string) => `máy: ${s}`,
  demo: "demo",
  demoHint: "Chế độ demo: dữ liệu mẫu, không lệnh nào chạy trên máy thật",
  stackMissing: "chưa cài stack",
  php: (v: string) => `php ${v}`,

  command: "Gõ lệnh hoặc hỏi TPAN…",
  commandSoon: "Thanh lệnh chưa có trong bản này",
  langGroup: "Ngôn ngữ",
  themeGroup: "Giao diện sáng hoặc tối",
  light: "Sáng",
  dark: "Tối",
  tips: (on: boolean) => `Gợi ý: ${on ? "bật" : "tắt"}`,

  skipToContent: "Bỏ qua menu, tới nội dung",
  navLabel: "Menu chính",
  groups: { may: "MÁY", ai: "AI", taiKhoan: "TÀI KHOẢN" },
  menu: {
    dashboard: "Bảng điều khiển",
    sites: "Site",
    databases: "Cơ sở dữ liệu",
    ssl: "SSL",
    trash: "Thùng rác",
    incidents: "Sự cố",
    jobs: "Lịch sử",
    security: "Bảo mật",
    aiSessions: "Phiên AI",
    settings: "Cài đặt",
    logout: "Đăng xuất",
  },
  openIncidents: " đang mở",
  logoutFailed: "Chưa đăng xuất được. Thử bấm lại; vẫn không được thì đóng trình duyệt.",

  status: { pass: "đạt", warn: "cảnh báo", fail: "lỗi", unknown: "chưa kiểm được" },
  statusLabel: (what: string, status: string) => `${what}: ${status}`,

  evidence: "BẰNG CHỨNG",
  copy: "bấm để chép",
  copied: "Đã chép",
  copyFailed: "Không chép được. Chọn chữ rồi chép tay.",
  copyCommand: "Chép lệnh",
  copyAll: "Chép hết",
  commandHostFallback: "<máy>",
  commandWhere: (host: string) => `Chạy trên máy ${host} (vào máy bằng ssh <user>@${host}), quyền root (các lệnh đã có sudo), thư mục nào cũng được.`,
  commandWhereDemo: "Đây là máy giả của chế độ demo: lệnh chỉ để xem, không chạy ở đâu được.",
  close: "Đóng",
  cancel: "Huỷ",

  loading: "Đang tải…",
  errorNetwork: "Không tới được máy chủ tpan. Kiểm tra kết nối rồi tải lại trang.",
  errorHttp: (code: number) => `Máy chủ trả lỗi HTTP ${code}. Tải lại trang; vẫn lỗi thì xem log của tpan.`,
  retry: "Tải lại",

  notFoundTitle: "Không có trang này",
  notFoundBody: "Đường dẫn này không trỏ tới màn nào của TPAN.",
  notFoundHome: "Về Bảng điều khiển",
};

export const strings = defineStrings(vi, {
  homeLabel: "TPAN, go to the dashboard",
  machineState: (s) => `machine: ${s}`,
  demo: "demo",
  demoHint: "Demo mode: sample data, no command runs on a real machine",
  stackMissing: "no stack yet",
  php: (v) => `php ${v}`,

  command: "Type a command or ask TPAN…",
  commandSoon: "The command bar is not in this version yet",
  langGroup: "Language",
  themeGroup: "Light or dark",
  light: "Light",
  dark: "Dark",
  tips: (on) => `Tips: ${on ? "on" : "off"}`,

  skipToContent: "Skip the menu, go to content",
  navLabel: "Main menu",
  groups: { may: "MACHINE", ai: "AI", taiKhoan: "ACCOUNT" },
  menu: {
    dashboard: "Dashboard",
    sites: "Sites",
    databases: "Databases",
    ssl: "SSL",
    trash: "Trash",
    incidents: "Incidents",
    jobs: "History",
    security: "Security",
    aiSessions: "AI sessions",
    settings: "Settings",
    logout: "Log out",
  },
  openIncidents: " open",
  logoutFailed: "Could not log out yet. Try again; if it still fails, close the browser.",

  status: { pass: "pass", warn: "warning", fail: "failed", unknown: "not checked" },
  statusLabel: (what, status) => `${what}: ${status}`,

  evidence: "EVIDENCE",
  copy: "click to copy",
  copied: "Copied",
  copyFailed: "Could not copy. Select the text and copy it by hand.",
  copyCommand: "Copy command",
  copyAll: "Copy all",
  commandHostFallback: "<machine>",
  commandWhere: (host) => `Run on the machine ${host} (log in with ssh <user>@${host}), as root (the commands already have sudo), from any directory.`,
  commandWhereDemo: "This is the pretend machine of demo mode: the commands are for reading only and run nowhere.",
  close: "Close",
  cancel: "Cancel",

  loading: "Loading…",
  errorNetwork: "Cannot reach the tpan server. Check the connection, then reload the page.",
  errorHttp: (code) => `The server returned HTTP ${code}. Reload the page; if it keeps failing, check the tpan log.`,
  retry: "Reload",

  notFoundTitle: "No such page",
  notFoundBody: "This address does not lead to any TPAN screen.",
  notFoundHome: "Go to the dashboard",
});
