// /ai: các phiên AI, bảng kẻ mảnh như "Lịch sử máy" (mục 7 của GIAO-DIEN.md).
// Phiên của panel và phiên của agent nối thẳng qua MCP nằm chung một bảng, chỉ
// khác ở cột "Lối vào".

import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { errorMessage, get } from "../../api/client";
import type { AISessionSummary } from "../../api/types";
import { Button } from "../../components/Button";
import { ScreenHeader } from "../../components/ScreenHeader";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/phien-ai";
import { formatDateTime } from "../lich-su/format";
import { entryText } from "./entry";

/** Câu hỏi dài thì cắt ở đây; trang một phiên có đủ. */
const questionChars = 160;

export function SessionList() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({
    queryKey: ["ai", "sessions"],
    queryFn: () => get<AISessionSummary[]>("/api/v1/ai/sessions"),
    // Lượt đang chạy thì số lần gọi công cụ còn nhích: đọc lại cho người đang xem.
    refetchInterval: 15_000,
  });

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />
      <section aria-label={t.listLabel} className="pt-[26px]">
        {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
        {q.isError && (
          <div role="alert">
            <p className="m-0">
              {t.listFailed} {errorMessage(q.error, lang)}
            </p>
            <div className="mt-[10px]">
              <Button onClick={() => void q.refetch()}>{k.retry}</Button>
            </div>
          </div>
        )}
        {q.data && (
          <TableList
            caption={t.listLabel}
            rows={q.data}
            rowKey={(s) => s.id}
            empty={t.listEmpty}
            columns={[
              {
                header: t.colLast,
                cell: (s) => (
                  <span title={s.last} className="font-mono text-[12px] text-muted">
                    {formatDateTime(s.last, lang)}
                  </span>
                ),
                className: "w-[110px] whitespace-nowrap",
              },
              {
                header: t.colEntry,
                cell: (s) => <span className="text-[13px]">{entryText(t, s)}</span>,
                className: "min-w-[140px]",
              },
              {
                header: t.colQuestion,
                cell: (s) => (
                  <span className="flex flex-wrap items-baseline gap-x-[10px] gap-y-[4px]">
                    <Link to={`/ai/${encodeURIComponent(s.id)}`} className="min-w-0 [overflow-wrap:anywhere]">
                      {s.question ? (s.question.length > questionChars ? `${s.question.slice(0, questionChars)}…` : s.question) : t.noQuestion}
                    </Link>
                    {s.untrusted && <Tag title={t.untrustedHint}>{t.untrustedTag}</Tag>}
                  </span>
                ),
                className: "min-w-[220px]",
              },
              { header: t.colTurns, cell: (s) => <span className="font-mono text-[12px]">{s.turns}</span>, className: "w-[60px]" },
              { header: t.colCalls, cell: (s) => <span className="font-mono text-[12px]">{s.calls}</span>, className: "w-[110px]" },
              { header: t.colPlans, cell: (s) => <span className="font-mono text-[12px]">{s.plans}</span>, className: "w-[60px]" },
            ]}
          />
        )}
      </section>
    </>
  );
}
