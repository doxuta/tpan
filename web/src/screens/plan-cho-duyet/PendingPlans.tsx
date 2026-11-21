// Khối "Chờ bạn duyệt" ở cột bên của Bảng điều khiển: một thẻ, tối đa 5 Plan,
// mỗi dòng là liên kết tới /plans/:id kèm nhãn nguồn gốc. Không có thì một câu
// ngắn, không ẩn khối (mục 7 của docs/GIAO-DIEN.md). Dữ liệu: GET
// /api/v1/plans?status=pending.
//
// Cột bên chỉ rộng cỡ 340 px nên "bảng" ở đây là danh sách dòng trong thẻ (như
// thẻ Cảnh báo cạnh nó), không phải TableList nhiều cột.

import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { errorMessage, get } from "../../api/client";
import type { PendingPlan } from "../../api/types";
import { Card } from "../../components/Card";
import { EmptyState } from "../../components/EmptyState";
import { Notice } from "../../components/Notice";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/agent";
import { strings as khung } from "../../i18n/khung";
import { OriginTag, plansKey } from "./OriginTag";

const maxRows = 5;

export function PendingPlans() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({
    queryKey: [...plansKey, "list", "pending"],
    queryFn: () => get<PendingPlan[]>("/api/v1/plans?status=pending"),
    // Agent đề xuất Plan lúc người dùng đang nhìn Bảng điều khiển: đọc lại định kỳ như overview.
    refetchInterval: 30_000,
  });
  const plans = q.data ?? [];

  return (
    <Card
      title={t.pendingBlockTitle}
      titleId="plan-cho-duyet-tieu-de"
      count={plans.length}
      footer={
        <Link to="/plans" className="inline-flex min-h-[44px] items-center sm:min-h-0">
          {t.pendingAll}
        </Link>
      }
    >
      {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div role="alert">
          <Notice kind="fail">
            {t.pendingFailed} {errorMessage(q.error, lang)}
          </Notice>
        </div>
      )}
      {q.isSuccess && plans.length === 0 && <EmptyState>{t.pendingNone}</EmptyState>}
      {plans.length > 0 && (
        <ul className="m-0 list-none p-0">
          {plans.slice(0, maxRows).map((p) => (
            <li key={p.id} className="flex flex-wrap items-center justify-between gap-x-[12px] gap-y-[6px] border-t border-hair py-[10px] first:border-t-0 first:pt-0 last:pb-0">
              <Link to={`/plans/${encodeURIComponent(p.id)}`} className="min-w-0 [overflow-wrap:anywhere]">
                {tx(p.title, lang)}
              </Link>
              <OriginTag origin={p.origin} />
            </li>
          ))}
        </ul>
      )}
      {plans.length > maxRows && <p className="mt-[10px] mb-0 text-[13px] text-muted">{t.pendingMore(plans.length - maxRows)}</p>}
    </Card>
  );
}
