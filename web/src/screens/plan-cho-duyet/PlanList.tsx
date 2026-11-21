// /plans: các Plan chờ duyệt, đã chạy và đã từ chối, mới nhất trước (thứ tự do
// máy chủ trả). Lọc theo trạng thái bằng GET /api/v1/plans?status=...; mặc
// định xem các Plan chờ duyệt vì đó là việc người dùng tới đây để làm.
//
// Bản 2: bảng trong thẻ; nút lọc nằm ở đầu màn (không nằm trong bảng, vì bảng
// biến mất lúc đang tải bộ lọc mới và nút sẽ mất focus theo).

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { errorMessage, get } from "../../api/client";
import type { PendingPlan, PlanStatus } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
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
      <ScreenHeader
        crumbs={[{ label: k.groups.ai }, { label: t.listTitle }]}
        title={t.listTitle}
        summary={t.listSummary}
        actions={
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
        }
      />
      {q.isPending && (
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {q.isError && (
        <Card>
          <div role="alert">
            <p className="mt-0 mb-[12px]">
              {t.listFailed} {errorMessage(q.error, lang)}
            </p>
            <Button onClick={() => void q.refetch()}>{k.retry}</Button>
          </div>
        </Card>
      )}
      {q.isSuccess && (
        <TableList
          label={t.listCaption}
          caption={t.listCaption}
          minWidth={820}
          rows={q.data}
          rowKey={(p) => p.id}
          empty={empty}
          columns={[
            {
              header: t.colPlan,
              nowrap: false,
              className: "min-w-[240px]",
              cell: (p) => (
                <Link to={`/plans/${encodeURIComponent(p.id)}`} className="[overflow-wrap:anywhere]">
                  {tx(p.title, lang)}
                </Link>
              ),
            },
            { header: t.colOrigin, cell: (p) => <OriginTag origin={p.origin} /> },
            // Đối tượng thường là tên miền (sans) hoặc "2 site"; không phải thứ để chép từng ký tự.
            { header: t.colTarget, nowrap: false, cell: (p) => <span className="[overflow-wrap:anywhere]">{p.target}</span> },
            { header: t.colStatus, cell: (p) => <span className="text-muted">{t.status[p.status]}</span> },
            { header: t.colTime, cell: (p) => <span className="text-[13px] text-muted">{formatDateTime(p.created, lang)}</span> },
          ]}
        />
      )}
    </>
  );
}
