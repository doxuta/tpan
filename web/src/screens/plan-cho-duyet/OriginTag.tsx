// Nhãn nguồn gốc của một Plan (mục 8.1 của KE-HOACH.md: Plan chờ duyệt có nhãn
// "AI đề xuất"). Dùng chung cho danh sách, trang chi tiết và khối ở Bảng điều
// khiển, nên một chỗ duy nhất quyết định chữ của ba nguồn.

import type { PlanOrigin } from "../../api/types";
import { Tag } from "../../components/Tag";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/agent";

/** Khoá truy vấn của mọi thứ về Plan; invalidate khoá "plans" là làm mới cả danh sách lẫn chi tiết. */
export const plansKey = ["plans"] as const;

/** tone="surface" khi đặt thẳng trên nền trang (đầu màn), mặc định "paper" khi đặt trong thẻ. */
export function OriginTag({ origin, tone }: { origin: PlanOrigin; tone?: "paper" | "surface" }) {
  const t = useStrings(strings);
  return <Tag tone={tone}>{origin === "ai" ? t.originAi : origin === "mcp" ? t.originMcp : t.originHuman}</Tag>;
}
