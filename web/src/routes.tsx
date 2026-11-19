// Danh sách DUY NHẤT các màn của giao diện. Mỗi dòng là một màn: đường dẫn,
// component (tải lười, mỗi màn một file JS riêng), và mục menu nếu có. Menu
// chính dựng từ danh sách này theo đúng thứ tự, nên menu chỉ hiện mục đã có màn.
//
// Thêm màn ở mốc sau:
//   1. Tạo web/src/screens/<tên>/index.tsx và web/src/i18n/<tên>.ts.
//   2. Thêm tên mục menu vào khung.ts (khoá trong menu).
//   3. Thêm một dòng dưới đây, đúng vị trí theo thứ tự ở mục 10 của KE-HOACH.md.

import type { ComponentType } from "react";
import type { Overview } from "./api/types";
import { allChecks } from "./api/queries";
import type { strings as khung } from "./i18n/khung";

export type MenuGroup = "chinh" | "may" | "ai" | "taiKhoan";

export type MenuSpec = {
  /** Khoá tên mục trong khung.ts → menu. */
  label: keyof (typeof khung)["vi"]["menu"];
  /** "chinh" là nhóm việc hằng ngày, không có tên nhóm. */
  group: MenuGroup;
  /** Số đếm cạnh tên mục, tính từ /api/v1/overview; undefined thì không hiện. */
  count?: (o: Overview) => number | undefined;
  /** Số nằm trên nền --fail khi lớn hơn 0 (chỉ dùng cho Sự cố). */
  alert?: boolean;
};

export type ScreenEntry = {
  /** Mẫu đường dẫn của react-router ("/sites/:domain"). */
  path: string;
  /** Tải component của màn. Không có load: mục chỉ là liên kết trong menu (xem Sự cố). */
  load?: () => Promise<ComponentType>;
  menu?: MenuSpec;
  /** Vẽ không có khung trang (thanh đầu, menu), ví dụ trang đăng nhập. */
  bare?: boolean;
};

const activeSites = (o: Overview) => o.sites.filter((r) => r.site.status === "active").length;

export const screens: ScreenEntry[] = [
  {
    path: "/",
    load: () => import("./screens/bang-dieu-khien").then((m) => m.default),
    menu: { label: "dashboard", group: "chinh" },
  },
  {
    path: "/sites",
    load: () => import("./screens/site").then((m) => m.SiteList),
    menu: { label: "sites", group: "chinh", count: activeSites },
  },
  { path: "/sites/new", load: () => import("./screens/tao-site").then((m) => m.default) },
  { path: "/sites/:domain", load: () => import("./screens/site").then((m) => m.SiteDetail) },
  {
    path: "/databases",
    load: () => import("./screens/co-so-du-lieu").then((m) => m.default),
    // Mỗi site đang chạy có đúng một cơ sở dữ liệu riêng, nên đếm theo site;
    // khỏi gọi thêm /api/v1/databases chỉ để lấy một con số.
    menu: { label: "databases", group: "chinh", count: activeSites },
  },
  {
    // M3 chưa có màn Sự cố riêng (mốc sau). Mục này dẫn về Bảng điều khiển,
    // nơi có khối "Sự cố đang mở", và đếm số phép kiểm đang lỗi. Khi có màn
    // Sự cố thì đổi path thành "/incidents" và thêm load.
    path: "/",
    menu: {
      label: "incidents",
      group: "may",
      alert: true,
      count: (o) => allChecks(o).filter((c) => c.status === "fail").length || undefined,
    },
  },
  {
    path: "/jobs",
    load: () => import("./screens/lich-su").then((m) => m.JobList),
    menu: { label: "jobs", group: "may" },
  },
  { path: "/jobs/:id", load: () => import("./screens/lich-su").then((m) => m.JobDetail) },
  { path: "/plans", load: () => import("./screens/plan-cho-duyet").then((m) => m.PlanList) },
  { path: "/plans/:id", load: () => import("./screens/plan-cho-duyet").then((m) => m.PlanDetail) },
  {
    path: "/settings",
    load: () => import("./screens/cai-dat").then((m) => m.default),
    menu: { label: "settings", group: "taiKhoan" },
  },
  { path: "/dang-nhap", load: () => import("./screens/dang-nhap").then((m) => m.default), bare: true },
];
