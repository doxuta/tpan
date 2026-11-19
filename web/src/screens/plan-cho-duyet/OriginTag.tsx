// Nhãn nguồn gốc của một Plan (mục 8.1 của KE-HOACH.md: Plan chờ duyệt có nhãn
// "AI đề xuất"). Dùng chung cho danh sách, trang chi tiết và khối ở Bảng điều
// khiển, nên một chỗ duy nhất quyết định chữ của ba nguồn.

import type { PlanOrigin } from "../../api/types";
import { Tag } from "../../components/Tag";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/agent";

/** Khoá truy vấn của mọi thứ về Plan; invalidate khoá "plans" là làm mới cả danh sách lẫn chi tiết. */
export const plansKey = ["plans"] as const;

export function OriginTag({ origin }: { origin: PlanOrigin }) {
  const t = useStrings(strings);
  return <Tag>{origin === "ai" ? t.originAi : origin === "mcp" ? t.originMcp : t.originHuman}</Tag>;
}
