// Biểu tượng nét của TPAN (mục 6 của docs/GIAO-DIEN.md): khung 24, nét 1,75,
// đầu nét tròn, màu theo chữ (currentColor). Vẽ riêng cho TPAN, lấy từ ba trang
// mẫu ở docs/mau-giao-dien/v2/; không chép biểu tượng của sản phẩm nào.
// Luôn đi kèm chữ hoặc nhãn đọc được, nên tự ẩn khỏi trình đọc màn hình.
//
//   <Icon name="database" />            18 px (mặc định, mục menu)
//   <Icon name="plus" size={16} />      trong nút

import type { ReactNode } from "react";

const paths = {
  home: <path d="M4 11l8-7 8 7v8a1 1 0 0 1-1 1h-4v-6H9v6H5a1 1 0 0 1-1-1z" />,
  grid: (
    <>
      <rect x="4" y="4" width="6.5" height="6.5" rx="1.2" />
      <rect x="13.5" y="4" width="6.5" height="6.5" rx="1.2" />
      <rect x="4" y="13.5" width="6.5" height="6.5" rx="1.2" />
      <rect x="13.5" y="13.5" width="6.5" height="6.5" rx="1.2" />
    </>
  ),
  globe: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M3.5 12h17M12 3.5c2.6 2.8 2.6 14.2 0 17M12 3.5c-2.6 2.8-2.6 14.2 0 17" />
    </>
  ),
  database: (
    <>
      <ellipse cx="12" cy="6" rx="7" ry="2.8" />
      <path d="M5 6v12c0 1.5 3.1 2.8 7 2.8s7-1.3 7-2.8V6M5 12c0 1.5 3.1 2.8 7 2.8s7-1.3 7-2.8" />
    </>
  ),
  lock: (
    <>
      <rect x="5" y="10.5" width="14" height="9.5" rx="1.8" />
      <path d="M8 10.5V8a4 4 0 0 1 8 0v2.5" />
    </>
  ),
  archive: (
    <>
      <rect x="3.5" y="4.5" width="17" height="4.5" rx="1" />
      <path d="M5 9v9.5a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V9M10 13h4" />
    </>
  ),
  clock: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 7.5V12l3 2" />
    </>
  ),
  trash: <path d="M4.5 7h15M9.5 7V5a1 1 0 0 1 1-1h3a1 1 0 0 1 1 1v2M6.5 7l.8 12a1 1 0 0 0 1 .9h7.4a1 1 0 0 0 1-.9L17.5 7" />,
  alert: (
    <>
      <path d="M12 4.5l8.5 14.5h-17z" />
      <path d="M12 10v4.2M12 16.8v.2" />
    </>
  ),
  list: <path d="M8.5 7h11M8.5 12h11M8.5 17h11M4.5 7h.01M4.5 12h.01M4.5 17h.01" />,
  layers: (
    <>
      <path d="M12 4.5l8.5 4.3-8.5 4.3-8.5-4.3z" />
      <path d="M3.5 13.2l8.5 4.3 8.5-4.3" />
    </>
  ),
  "arrow-up-circle": (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 16V8.5M8.5 11.5L12 8l3.5 3.5" />
    </>
  ),
  shield: <path d="M12 3.5l7 2.7v5.3c0 4.3-2.9 7.4-7 9-4.1-1.6-7-4.7-7-9V6.2z" />,
  chat: <path d="M5 5.5h14a1 1 0 0 1 1 1v9a1 1 0 0 1-1 1h-7l-4.5 3.5v-3.5H5a1 1 0 0 1-1-1v-9a1 1 0 0 1 1-1z" />,
  users: (
    <>
      <circle cx="9" cy="8.5" r="3.2" />
      <path d="M3 19.5c.6-3.2 3-5 6-5s5.4 1.8 6 5M16 5.6a3.2 3.2 0 0 1 0 5.8M17.5 14.8c2 .6 3.2 2.2 3.6 4.7" />
    </>
  ),
  package: (
    <>
      <path d="M12 3.5l8 4.2v8.6l-8 4.2-8-4.2V7.7z" />
      <path d="M4 7.7l8 4.2 8-4.2M12 11.9v8.6" />
    </>
  ),
  sliders: (
    <>
      <path d="M4 7h9M17 7h3M4 12h3M11 12h9M4 17h11M19 17h1" />
      <circle cx="15" cy="7" r="2" />
      <circle cx="9" cy="12" r="2" />
      <circle cx="17" cy="17" r="2" />
    </>
  ),
  plus: <path d="M12 5v14M5 12h14" />,
  search: (
    <>
      <circle cx="11" cy="11" r="6.5" />
      <path d="M16 16l4.5 4.5" />
    </>
  ),
  "chevron-down": <path d="M6 9l6 6 6-6" />,
  "chevron-right": <path d="M9 6l6 6-6 6" />,
  chart: <path d="M4 19.5h16M7 16v-5M12 16V6.5M17 16v-8" />,
  menu: <path d="M4 7h16M4 12h16M4 17h16" />,
  close: <path d="M6 6l12 12M18 6L6 18" />,
  expand: <path d="M14 4h6v6M10 20H4v-6M20 4l-7 7M4 20l7-7" />,
  collapse: <path d="M20 10h-6V4M4 14h6v6M14 10l7-7M10 14l-7 7" />,
  check: <path d="M5 12l5 5 9-10" />,
  copy: (
    <>
      <rect x="9" y="9" width="11" height="11" rx="1.5" />
      <path d="M15 9V5.5A1.5 1.5 0 0 0 13.5 4h-8A1.5 1.5 0 0 0 4 5.5v8A1.5 1.5 0 0 0 5.5 15H9" />
    </>
  ),
  inbox: (
    <>
      <path d="M4 13l2.5-7.5h11L20 13v5.5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1z" />
      <path d="M4 13h4.5l1.5 2.5h4l1.5-2.5H20" />
    </>
  ),
  php: (
    <>
      <rect x="3.5" y="6" width="17" height="12" rx="2" />
      <path d="M8 15V9h2a1.5 1.5 0 0 1 0 3H8M14 15V9h2a1.5 1.5 0 0 1 0 3h-2" />
    </>
  ),
  activity: <path d="M3.5 12h4l2.5-6 4 12 2.5-6h4" />,
} satisfies Record<string, ReactNode>;

export type IconName = keyof typeof paths;

/** "more" (ba chấm) vẽ bằng hình đặc, không phải nét. */
const filled: Partial<Record<IconName | "more", ReactNode>> = {
  more: (
    <>
      <circle cx="5" cy="12" r="1.7" />
      <circle cx="12" cy="12" r="1.7" />
      <circle cx="19" cy="12" r="1.7" />
    </>
  ),
};

export function Icon({ name, size = 18, className = "" }: { name: IconName | "more"; size?: 14 | 16 | 18 | 20; className?: string }) {
  if (name === "more") {
    return (
      <svg aria-hidden="true" width={size} height={size} viewBox="0 0 24 24" fill="currentColor" className={`flex-none ${className}`.trim()}>
        {filled.more}
      </svg>
    );
  }
  return (
    <svg
      aria-hidden="true"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.75"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={`flex-none ${className}`.trim()}
    >
      {paths[name]}
    </svg>
  );
}
