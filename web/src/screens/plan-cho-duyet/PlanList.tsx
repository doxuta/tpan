// /plans: các Plan chờ duyệt, đã chạy và đã từ chối, mới nhất trước (thứ tự do
// máy chủ trả). Lọc theo trạng thái bằng GET /api/v1/plans?status=...; mặc
// định xem các Plan chờ duyệt vì đó là việc người dùng tới đây để làm.

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { errorMessage, get } from "../../api/client";
import type { PendingPlan, PlanStatus } from "../../api/types";
import { Button } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { SegmentButton } from "../../components/SegmentButton";
import { TableList } from "../../components/TableList";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/agent";
import { strings as khung } from "../../i18n/khung";
import { formatDateTime } from "../lich-su/format";
import { OriginTag, plansKey } from "./OriginTag";

type Filter = PlanStatus | "all";

export function PlanList() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const [filter, setFilter] = useState<Filter>("pending");
  const q = useQuery({
    queryKey: [...plansKey, "list", filter],
    queryFn: () => get<PendingPlan[]>(filter === "all" ? "/api/v1/plans" : `/api/v1/plans?status=${filter}`),
  });
  const empty = { pending: t.emptyPending, applied: t.emptyApplied, rejected: t.emptyRejected, all: t.emptyAll }[filter];

  return (
    <>
      <ScreenHeader title={t.listTitle} summary={t.listSummary} />
      <section aria-label={t.listCaption} className="pt-[26px]">
        <div className="mb-[16px]">
          <SegmentButton<Filter>
            label={t.filterLabel}
            value={filter}
            onChange={setFilter}
            options={[
              { value: "pending", label: t.status.pending },
              { value: "applied", label: t.status.applied },
              { value: "rejected", label: t.status.rejected },
              { value: "all", label: t.filterAll },
            ]}
          />
        </div>
        {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
        {q.isError && (
          <div role="alert">
            <p className="m-0">
              {t.listFailed} {errorMessage(q.error, lang)}
            </p>
            <Button className="mt-[12px]" onClick={() => void q.refetch()}>
              {k.retry}
            </Button>
          </div>
        )}
        {q.isSuccess && (
          <TableList
            caption={t.listCaption}
            rows={q.data}
            rowKey={(p) => p.id}
            empty={empty}
            columns={[
              {
                header: t.colPlan,
                cell: (p) => (
                  <Link to={`/plans/${encodeURIComponent(p.id)}`} className="[overflow-wrap:anywhere]">
                    {tx(p.title, lang)}
                  </Link>
                ),
              },
              { header: t.colOrigin, cell: (p) => <OriginTag origin={p.origin} /> },
              { header: t.colTarget, cell: (p) => <MonoText size={12}>{p.target}</MonoText> },
              { header: t.colStatus, cell: (p) => <span className="text-muted">{t.status[p.status]}</span>, className: "whitespace-nowrap" },
              { header: t.colTime, cell: (p) => <span className="font-mono text-[12px] whitespace-nowrap text-muted">{formatDateTime(p.created, lang)}</span> },
            ]}
          />
        )}
      </section>
    </>
  );
}
