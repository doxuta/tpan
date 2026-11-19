// Tính trạng thái của đường đi request từ các phép kiểm của một site.
// Dùng cho danh sách (trạng thái chung) và trang site (từng ga).

import type { CheckResult, Stage, Status } from "../../api/types";

/** Sáu ga của tuyến, theo đúng thứ tự đường đi của request (mục 1 của GIAO-DIEN.md). */
export const routeStages = ["dns", "tls", "web", "php", "wordpress", "db"] as const satisfies readonly Stage[];

/** Thứ tự các nhóm phép kiểm ở trang site: sáu ga, rồi phép gọi HTTP thật, rồi phần còn lại nếu có. */
export const stageOrder: Stage[] = [...routeStages, "e2e", "machine", "tpan"];

// Xấu hơn thì số lớn hơn. "Chưa kiểm được" xấu hơn "đạt": một chặng mới đạt
// một nửa thì chưa được tô là đạt, vì giao diện không khẳng định điều chưa có bằng chứng.
const rank: Record<Status, number> = { pass: 0, unknown: 1, warn: 2, fail: 3 };

/** Trạng thái xấu nhất trong danh sách; danh sách rỗng là "chưa kiểm được". */
export function worstStatus(list: readonly CheckResult[]): Status {
  let worst: Status | null = null;
  for (const c of list) if (worst === null || rank[c.status] > rank[worst]) worst = c.status;
  return worst ?? "unknown";
}

export function checksOfStage(checks: readonly CheckResult[], stage: Stage): CheckResult[] {
  return checks.filter((c) => c.stage === stage);
}
