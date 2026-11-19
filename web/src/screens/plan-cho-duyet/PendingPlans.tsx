// Khối "Plan đang chờ duyệt" ở cột bên của Bảng điều khiển: tối đa 5 Plan, mỗi
// dòng là liên kết tới /plans/:id kèm nhãn nguồn gốc. Không có thì một câu
// ngắn, không ẩn khối (mục 7 của docs/GIAO-DIEN.md). Dữ liệu: GET
// /api/v1/plans?status=pending.

import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { errorMessage, get } from "../../api/client";
import type { PendingPlan } from "../../api/types";
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
    <section aria-labelledby="plan-cho-duyet-tieu-de">
      <h2 id="plan-cho-duyet-tieu-de" className="m-0 mb-[10px] flex items-baseline gap-[8px] text-[16px] font-semibold">
        {t.pendingBlockTitle}
        <span className="font-mono text-[13px] font-normal text-muted">{plans.length}</span>
      </h2>
      {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <p role="alert" className="m-0">
          {t.pendingFailed} {errorMessage(q.error, lang)}
        </p>
      )}
      {q.isSuccess && plans.length === 0 && <p className="m-0 text-muted">{t.pendingNone}</p>}
      {plans.length > 0 && (
        <ul className="m-0 list-none p-0">
          {plans.slice(0, maxRows).map((p) => (
            <li key={p.id} className="flex flex-wrap items-center justify-between gap-x-[12px] gap-y-[6px] border-t border-hair py-[10px]">
              <Link to={`/plans/${encodeURIComponent(p.id)}`} className="min-w-0 [overflow-wrap:anywhere]">
                {tx(p.title, lang)}
              </Link>
              <OriginTag origin={p.origin} />
            </li>
          ))}
        </ul>
      )}
      {plans.length > maxRows && <p className="mt-[6px] mb-0 text-[13px] text-muted">{t.pendingMore(plans.length - maxRows)}</p>}
      <p className="mt-[10px] mb-0">
        <Link to="/plans" className="inline-flex min-h-[44px] items-center sm:min-h-0">
          {t.pendingAll}
        </Link>
      </p>
    </section>
  );
}
