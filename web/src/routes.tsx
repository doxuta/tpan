// Danh sách DUY NHẤT các màn của giao diện. Mỗi dòng là một màn: đường dẫn,
// component (tải lười, mỗi màn một file JS riêng), và mục menu nếu có. Menu
// chính, trang Công cụ và thanh lệnh dựng từ danh sách này, nên menu chỉ hiện
// mục đã có màn.
//
// Thêm màn ở mốc sau:
//   1. Tạo web/src/screens/<tên>/index.tsx và web/src/i18n/<tên>.ts.
//   2. Thêm tên mục menu vào khung.ts (khoá trong menu).
//   3. Thêm một dòng dưới đây, đúng vị trí theo thứ tự ở mục 10 của KE-HOACH.md.
//   4. Màn nên có ô ở trang Công cụ thì thêm vào screens/cong-cu/tools.ts.

import type { ComponentType } from "react";
import type { Overview } from "./api/types";
import type { IconName } from "./components/Icon";
import type { strings as khung } from "./i18n/khung";

export type MenuGroup = "chinh" | "may" | "ai" | "taiKhoan";

export type MenuSpec = {
  /** Khoá tên mục trong khung.ts → menu. */
  label: keyof (typeof khung)["vi"]["menu"];
  /** "chinh" là nhóm việc hằng ngày, không có tên nhóm. */
  group: MenuGroup;
  /** Biểu tượng của mục (mục 3.5 của docs/thay-doi/M16-thiet-ke.md). */
  icon: IconName;
  /** Số đếm cạnh tên mục, tính từ /api/v1/overview; undefined thì không hiện. */
  count?: (o: Overview) => number | undefined;
  /** Số nằm trên nền --fail khi lớn hơn 0 (chỉ dùng cho Sự cố). */
  alert?: boolean;
  /** Đường dẫn của mục khi người xem là user thường (trang chủ theo vai, mục 3.6). */
  userPath?: string;
};

export type ScreenEntry = {
  /** Mẫu đường dẫn của react-router ("/sites/:domain"). */
  path: string;
  /** Tải component của màn. Không có load: mục chỉ là liên kết trong menu. */
  load?: () => Promise<ComponentType>;
  menu?: MenuSpec;
  /** Vẽ không có khung trang (thanh đầu, menu), ví dụ trang đăng nhập. */
  bare?: boolean;
  /**
   * M13: màn chỉ dành cho vai admin. User thường không thấy mục menu, không thấy
   * ô ở trang Công cụ, và gõ thẳng đường dẫn thì thấy trang "không có quyền"
   * (main.tsx bọc màn bằng AdminOnly). Đây chỉ là phần giao diện: quyền thật
   * do luật của route ở server giữ.
   */
  admin?: boolean;
};

/** Số site đang chạy trong phạm vi người xem (tổng quan đã lọc theo chủ). */
export const activeSites = (o: Overview) => o.sites.filter((r) => r.site.status === "active").length;

// "as const" giữ nguyên chữ của từng đường dẫn để có kiểu ScreenPath bên dưới;
// "satisfies" vẫn bắt lỗi nếu một dòng sai khuôn ScreenEntry.
const table = [
  {
    // Trang chủ theo vai: admin thấy Bảng điều khiển, user thường được chuyển sang /tools.
    path: "/",
    load: () => import("./screens/trang-chu").then((m) => m.Home),
    menu: { label: "dashboard", group: "chinh", icon: "home", userPath: "/dashboard" },
  },
  // Bảng điều khiển của user thường (admin mở thì về /).
  { path: "/dashboard", load: () => import("./screens/trang-chu").then((m) => m.DashboardAlias) },
  {
    path: "/tools",
    load: () => import("./screens/cong-cu").then((m) => m.default),
    menu: { label: "tools", group: "chinh", icon: "grid" },
  },
  // M18: sinh user-data cloud-init cho máy mới. Không có mục menu; vào từ ô ở trang Công cụ.
  { path: "/tools/new-server", load: () => import("./screens/cai-may-moi").then((m) => m.default), admin: true },
  {
    path: "/sites",
    load: () => import("./screens/site").then((m) => m.SiteList),
    menu: { label: "sites", group: "chinh", icon: "globe", count: activeSites },
  },
  { path: "/sites/new", load: () => import("./screens/tao-site").then((m) => m.default) },
  // M19: tên phụ và luật chuyển hướng của mọi site trong phạm vi người xem. Không có mục menu; vào từ hai ô ở trang Công cụ.
  { path: "/domains", load: () => import("./screens/ten-mien").then((m) => m.default) },
  { path: "/sites/:domain", load: () => import("./screens/site").then((m) => m.SiteDetail) },
  {
    path: "/databases",
    load: () => import("./screens/co-so-du-lieu").then((m) => m.default),
    // db_count (M16): DB của site đang chạy cộng DB rời đang chạy, đúng bằng số
    // hàng đang chạy của màn Cơ sở dữ liệu. Server cũ không gửi thì không hiện số.
    menu: { label: "databases", group: "chinh", icon: "database", count: (o) => o.db_count },
  },
  {
    path: "/ssl",
    load: () => import("./screens/ssl").then((m) => m.default),
    menu: { label: "ssl", group: "chinh", icon: "lock" },
  },
  {
    path: "/backups",
    load: () => import("./screens/backup").then((m) => m.default),
    menu: { label: "backups", group: "chinh", icon: "archive" },
  },
  {
    path: "/cron",
    load: () => import("./screens/cron").then((m) => m.default),
    menu: { label: "cron", group: "chinh", icon: "clock" },
  },
  {
    path: "/trash",
    load: () => import("./screens/thung-rac").then((m) => m.default),
    // Mục 10: mục đếm được thì có số; thùng rác đếm site đang nằm trong đó.
    menu: { label: "trash", group: "chinh", icon: "trash", count: (o) => o.sites.filter((r) => r.site.status === "trashed").length || undefined },
  },
  {
    // M17: số liệu của site trong lồng. Mọi vai xem được (user thường chỉ thấy site của mình).
    path: "/usage",
    load: () => import("./screens/tai-nguyen").then((m) => m.default),
    menu: { label: "usage", group: "chinh", icon: "activity" },
  },
  {
    path: "/incidents",
    load: () => import("./screens/su-co").then((m) => m.IncidentList),
    menu: {
      label: "incidents",
      group: "may",
      icon: "alert",
      alert: true,
      count: (o) => o.incidents.length || undefined,
    },
  },
  { path: "/incidents/:id", load: () => import("./screens/su-co").then((m) => m.IncidentDetail) },
  {
    path: "/jobs",
    load: () => import("./screens/lich-su").then((m) => m.JobList),
    menu: { label: "jobs", group: "may", icon: "list" },
  },
  { path: "/jobs/:id", load: () => import("./screens/lich-su").then((m) => m.JobDetail) },
  { path: "/plans", load: () => import("./screens/plan-cho-duyet").then((m) => m.PlanList), admin: true },
  { path: "/plans/:id", load: () => import("./screens/plan-cho-duyet").then((m) => m.PlanDetail), admin: true },
  {
    path: "/stack",
    load: () => import("./screens/stack").then((m) => m.default),
    menu: { label: "stack", group: "may", icon: "layers" },
    admin: true,
  },
  {
    path: "/updates",
    load: () => import("./screens/cap-nhat").then((m) => m.default),
    menu: { label: "updates", group: "may", icon: "arrow-up-circle" },
    admin: true,
  },
  {
    path: "/security",
    load: () => import("./screens/bao-mat").then((m) => m.default),
    menu: { label: "security", group: "may", icon: "shield" },
    admin: true,
  },
  {
    path: "/ai",
    load: () => import("./screens/phien-ai").then((m) => m.SessionList),
    menu: { label: "aiSessions", group: "ai", icon: "chat" },
    admin: true,
  },
  { path: "/ai/:id", load: () => import("./screens/phien-ai").then((m) => m.SessionDetail), admin: true },
  {
    path: "/users",
    load: () => import("./screens/nguoi-dung").then((m) => m.default),
    menu: { label: "users", group: "taiKhoan", icon: "users" },
    admin: true,
  },
  {
    path: "/packages",
    load: () => import("./screens/goi").then((m) => m.default),
    menu: { label: "packages", group: "taiKhoan", icon: "package" },
    admin: true,
  },
  // M20: hai bước và token API của chính mình; mọi vai đều có.
  {
    path: "/account",
    load: () => import("./screens/tai-khoan").then((m) => m.default),
    menu: { label: "account", group: "taiKhoan", icon: "key" },
  },
  {
    path: "/settings",
    load: () => import("./screens/cai-dat").then((m) => m.default),
    menu: { label: "settings", group: "taiKhoan", icon: "sliders" },
  },
  { path: "/dang-nhap", load: () => import("./screens/dang-nhap").then((m) => m.default), bare: true },
  // M20: trang bật hai bước bắt buộc; nằm ngoài khung nên tự lo chuyện chưa đăng nhập.
  { path: "/bat-hai-buoc", load: () => import("./screens/bat-hai-buoc").then((m) => m.default), bare: true },
] as const satisfies readonly ScreenEntry[];

/** Mọi đường dẫn có trong bảng trên. Ô của trang Công cụ khai đích bằng kiểu này, viết sai là tsc báo lỗi. */
export type ScreenPath = (typeof table)[number]["path"];

export const screens: readonly ScreenEntry[] = table;

/** Màn chỉ dành cho admin (tra theo đường dẫn mẫu, không chép cờ sang nơi khác). */
export function isAdminPath(path: string): boolean {
  return screens.some((s) => s.path === path && s.admin === true);
}

/** Đường dẫn của một mục menu theo vai: user thường đi userPath nếu mục có. */
export function menuPath(entry: ScreenEntry, admin: boolean): string {
  return admin ? entry.path : (entry.menu?.userPath ?? entry.path);
}
