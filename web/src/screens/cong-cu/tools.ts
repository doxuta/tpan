// Bảng ô của trang Công cụ (mục 3.7 của docs/thay-doi/M16-thiet-ke.md). Mỗi
// ô trỏ tới một màn có thật: path có kiểu ScreenPath (hợp mọi đường dẫn trong
// routes.tsx), nên viết sai đường dẫn là tsc báo lỗi (bất biến B9). Ô nào trỏ
// tới màn có cờ admin trong routes.tsx thì user thường không thấy: cờ được tra
// ngay trong routes.tsx lúc vẽ, không chép sang đây.
//
// File .ts thuần, chỉ import type.

import type { IconName } from "../../components/Icon";
import type { strings } from "../../i18n/cong-cu";
import type { ScreenPath } from "../../routes";

type S = (typeof strings)["vi"];

export type Tool = {
  path: ScreenPath;
  icon: IconName;
  /** Khoá tên và mô tả trong i18n/cong-cu.ts → tiles. */
  key: keyof S["tiles"];
  /** Số đang chạy trong phạm vi người xem, cùng nguồn với menu. */
  count?: "sites" | "dbs" | "incidents";
  /** Đích khi người xem là user thường (Bảng điều khiển của họ nằm ở /dashboard). */
  userPath?: ScreenPath;
  /** Tab của màn đích, nối vào đường dẫn thành ?tab=<tab> (màn Tên miền phụ đọc nó để biết dẫn sang tab nào). */
  tab?: string;
};

export type ToolGroup = { group: keyof S["groups"]; tools: Tool[] };

export const toolGroups: ToolGroup[] = [
  {
    group: "website",
    tools: [
      { path: "/sites", icon: "globe", key: "sites", count: "sites" },
      { path: "/sites/new", icon: "plus", key: "newSite" },
      // M19: cùng màn /domains; tab quyết định dòng nào dẫn sang tab Tên miền hay Chuyển hướng của site.
      { path: "/domains", icon: "globe", key: "domains", tab: "ten-mien" },
      { path: "/domains", icon: "chevron-right", key: "redirects", tab: "chuyen-huong" },
    ],
  },
  {
    group: "files",
    tools: [
      { path: "/backups", icon: "archive", key: "backups" },
      { path: "/trash", icon: "trash", key: "trash" },
    ],
  },
  { group: "db", tools: [{ path: "/databases", icon: "database", key: "databases", count: "dbs" }] },
  {
    group: "security",
    tools: [
      { path: "/ssl", icon: "lock", key: "ssl" },
      { path: "/security", icon: "shield", key: "security" },
    ],
  },
  {
    group: "monitor",
    tools: [
      // Không có màn riêng: request và lỗi 5xx nằm trên Bảng điều khiển của từng vai.
      { path: "/", userPath: "/dashboard", icon: "chart", key: "traffic" },
      { path: "/incidents", icon: "alert", key: "incidents", count: "incidents" },
      { path: "/jobs", icon: "list", key: "jobs" },
      { path: "/usage", icon: "activity", key: "usage" },
    ],
  },
  {
    group: "advanced",
    tools: [
      { path: "/cron", icon: "clock", key: "cron" },
      // Không có màn riêng: bản PHP đổi ở tab PHP của từng site.
      { path: "/sites", icon: "php", key: "php" },
      { path: "/stack", icon: "layers", key: "stack" },
      { path: "/updates", icon: "arrow-up-circle", key: "updates" },
      // M18: chưa có biểu tượng riêng cho "máy mới"; dấu cộng như ô Tạo site.
      { path: "/tools/new-server", icon: "plus", key: "newServer" },
    ],
  },
  {
    group: "ai",
    tools: [
      { path: "/ai", icon: "chat", key: "aiSessions" },
      { path: "/plans", icon: "inbox", key: "plans" },
    ],
  },
  {
    group: "account",
    tools: [
      { path: "/settings", icon: "sliders", key: "settings" },
      { path: "/users", icon: "users", key: "users" },
      { path: "/packages", icon: "package", key: "packages" },
    ],
  },
];

/** Các nhóm người xem thấy: bỏ ô trỏ tới màn admin khi không phải admin, bỏ nhóm rỗng. */
export function visibleGroups(groups: ToolGroup[], admin: boolean, isAdminPath: (p: string) => boolean): ToolGroup[] {
  return groups.map((g) => ({ ...g, tools: g.tools.filter((t) => admin || !isAdminPath(t.path)) })).filter((g) => g.tools.length > 0);
}
