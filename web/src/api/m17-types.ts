// Kiểu JSON của các API M17 (lồng theo site, mức tối đa theo gói, số liệu,
// thử tấn công; mục 6.15 của docs/KE-HOACH.md). Viết theo
// internal/server/types_m17.go, internal/core/types_m17.go và
// internal/cage/types.go. Gọi API ở m17.ts.

import type { Evidence, Preview, Text } from "./types";

/** account.Limits: mức tối đa của gói áp lên mỗi site trong lồng. 0 là không giới hạn (php_workers: 0 là mặc định). */
export type Limits = {
  cpu_percent: number; // % của một nhân
  ram_mb: number; // MiB
  tasks: number; // tiến trình và luồng
  io_read_mb: number; // MB/giây
  io_write_mb: number; // MB/giây
  php_workers: number;
};

/** cage.Usage: số đọc ngay từ cgroup của slice. running false thì mọi số là 0. */
export type CageUsage = {
  running: boolean;
  at: string;
  mem_current: number; // byte
  mem_peak: number; // byte
  oom_kills: number; // số lần bị giết vì chạm mức RAM
  mem_max_hits: number;
  cpu_usec: number;
  throttled_usec: number;
  nr_throttled: number;
  tasks: number;
  tasks_peak: number;
  tasks_hits: number; // số lần chạm mức tiến trình
  io_rbytes: number;
  io_wbytes: number;
  /** Mức đang áp, đọc từ cgroup. 0 là "max". */
  mem_max: number; // byte
  swap_max: number; // byte, -1 khi "max"
  cpu_quota_us: number; // 0 khi "max"
  cpu_period_us: number;
  tasks_max: number;
  io_rbps_max: number;
  io_wbps_max: number;
};

/**
 * Trạng thái lồng của một site: "trong" (trong lồng, gói muốn lồng), "ngoai"
 * (ngoài lồng, gói không muốn), "can-vao" (gói muốn mà site chưa vào),
 * "can-ra" (site trong lồng mà gói không còn muốn).
 */
export type CageState = "trong" | "ngoai" | "can-vao" | "can-ra";

/** core.CageRow: GET /api/v1/cage (admin). */
export type CageRow = {
  domain: string;
  owner: string;
  package: string;
  caged: boolean;
  /** Gói của chủ bật lồng. */
  want: boolean;
  /** File hàng rào có trên máy. */
  files: boolean;
  state: CageState;
  suspended: boolean;
  limits: Limits;
  usage?: CageUsage;
};

/** CageBody: POST /api/v1/sites/{domain}/cage. */
export type CageBody = { enabled: boolean };

/** core.SiteUsage: GET /api/v1/usage (user thường chỉ thấy site của mình). */
export type SiteUsage = {
  domain: string;
  owner: string;
  usage: CageUsage;
  limits: Limits;
};

export type UsageRange = "day" | "week" | "month";

/** core.UsagePoint: một điểm (mẫu 5 phút cho "day", dòng giờ cho "week", "month"). */
export type UsagePoint = {
  at: string;
  mem_avg: number; // byte
  mem_max: number; // byte
  cpu_pct: number; // % một nhân
  tasks_max: number;
  oom_kills: number; // tăng trong khoảng
};

export type UsageHistory = { domain: string; range: UsageRange; points: UsagePoint[] };

/** SiteUsageDetail: GET /api/v1/sites/{domain}/usage?range=. Site ngoài lồng: 404 usage_not_caged. */
export type SiteUsageDetail = SiteUsage & { history: UsageHistory };

/** core.Suggestion: gợi ý mức tối đa cho một site, kèm mọi số đã dùng để tính. */
export type Suggestion = {
  domain: string;
  package: string;
  samples: number;
  from: string;
  to: string;
  /** Dưới 24 giờ số liệu. */
  low_data: boolean;
  mem_peak: number; // byte
  cpu_p95: number; // % một nhân
  tasks_peak: number;
  oom_kills: number; // trong 7 ngày
  tasks_hits: number; // trong 7 ngày
  throttled_pct: number; // % thời gian bị bóp CPU
  current: Limits;
  /** Chỉ ram_mb, cpu_percent, tasks được đặt. */
  suggested: Limits;
  /** Mỗi trường một câu nói đã tính ra sao. */
  reasons: Text[];
};

export type PackageSuggestion = { package: string; sites: string[]; current: Limits; suggested: Limits };

/** core.SuggestionReport: GET /api/v1/usage/suggestions (admin). */
export type SuggestionReport = { sites: Suggestion[]; packages: PackageSuggestion[] };

/** Trạng thái một ô của phép thử tấn công. */
export type ProbeStatusValue = "blocked" | "exposed" | "needs_cage" | "skipped" | "na";

export type ProbeCell = { status: ProbeStatusValue; detail: Text; evidence?: Evidence[] };

/** Một đường tấn công (L1 đến L9 của M14, C1 đến C5 của lồng), ngoài lồng và trong lồng. */
export type ProbeResult = { path: string; title: Text; outside: ProbeCell; inside: ProbeCell };

/** "" là cả hai lượt. */
export type ProbeOnly = "" | "ngoai" | "trong";

/** core.ProbeReport. */
export type ProbeReport = {
  id: string;
  started: string;
  finished?: string;
  web: "nginx" | "apache";
  domain_a: string;
  domain_b: string;
  only: ProbeOnly;
  results: ProbeResult[];
  /** Số ô "exposed": đường lẽ ra phải bị chặn mà lộ. */
  exposed: number;
  cleaned: boolean;
};

/** cage.ProbeRun: một dòng của sổ lượt thử. */
export type ProbeRun = {
  id: string;
  started: string;
  finished?: string;
  state: "running" | "done" | "failed";
  domain_a: string;
  domain_b: string;
  mode: ProbeOnly;
  cleaned: boolean;
};

/** ProbeStatus: GET /api/v1/security/probe (admin). */
export type ProbeStatus = { last: ProbeReport | null; pending: ProbeRun[] };

export type ProbeBody = { only: ProbeOnly };

/** ProbePreview: POST /api/v1/security/probe?dry_run=1. */
export type ProbePreview = Preview & { run: { id: string; domain_a: string; domain_b: string; only: ProbeOnly } };

/** PackageEdit: POST /api/v1/packages/{name}. Bảy trường M17 vắng mặt là giữ nguyên. */
export type PackageEdit = {
  max_sites: number;
  max_dbs: number;
  disk_mb: number;
  max_backups: number;
  cage?: boolean;
} & Partial<Limits>;

/** Khoảng hợp lệ của từng mức (core/types_m17.go). 0 luôn hợp lệ. */
export const limitRange = {
  cpu_percent: [5, 6400],
  ram_mb: [64, 1048576],
  tasks: [32, 100000],
  io_read_mb: [1, 100000],
  io_write_mb: [1, 100000],
  php_workers: [1, 200],
} as const satisfies Record<keyof Limits, readonly [number, number]>;

/** Mức tiến trình phải từ số worker + chừng này trở lên (core.CageTasksSlack). */
export const tasksSlack = 15;
