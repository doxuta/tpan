// /incidents: sự cố đang mở trước, đã đóng sau (mục 6.7 của KE-HOACH.md, mục 7
// của GIAO-DIEN.md: màn danh sách là bảng kẻ mảnh, không thẻ). Một lần gọi
// ?state=all rồi tách hai khối ở trình duyệt, để không gọi hai lần cho cùng dữ liệu.

import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { errorMessage, get } from "../../api/client";
import { useOverview } from "../../api/queries";
import type { Incident } from "../../api/types";
import { Button } from "../../components/Button";
import { ScreenHeader } from "../../components/ScreenHeader";
import { StatusMark } from "../../components/StatusMark";
import { TableList } from "../../components/TableList";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/su-co";
import { formatClock, formatDateTime } from "../lich-su/format";
import { formatSpan, spanMs } from "./format";
import { incidentState, Target } from "./parts";

const listLimit = 200;

export function IncidentList() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({
    queryKey: ["incidents", "all"],
    queryFn: () => get<Incident[]>(`/api/v1/incidents?state=all&limit=${listLimit}`),
    // Sự cố đóng khi health chạy lại (5 phút một lần): đọc lại để người đang xem thấy.
    refetchInterval: 30_000,
  });
  const overview = useOverview();

  const open = (q.data ?? []).filter((i) => !i.closed);
  const closed = (q.data ?? []).filter((i) => i.closed);

  // Khối trống nói "máy đang khoẻ" kèm thời điểm kiểm gần nhất. Chỉ nói khi biết health đã chạy:
  // chưa có overview thì chỉ báo "không có sự cố đang mở", không khẳng định máy khoẻ.
  const lastRun = overview.data?.last_run;
  const emptyOpen = !overview.data ? t.noOpen : lastRun ? t.healthy(formatClock(lastRun, lang)) : t.healthyNeverRun;

  const columns = (isOpen: boolean) => [
    {
      header: t.colIncident,
      className: "min-w-[240px]",
      cell: (i: Incident) => {
        const s = incidentState(i, t);
        return (
          <span className="inline-flex items-start gap-[10px]">
            <span className="flex h-[21px] w-[20px] flex-none items-center justify-center">
              <StatusMark status={s.status} label={s.text} />
            </span>
            <Link to={`/incidents/${i.id}`}>{tx(i.title, lang)}</Link>
          </span>
        );
      },
    },
    { header: t.colTarget, className: "min-w-[150px]", cell: (i: Incident) => <Target target={i.target} /> },
    { header: t.colStage, className: "w-[130px] whitespace-nowrap", cell: (i: Incident) => t.stages[i.stage] },
    {
      header: t.colOpened,
      className: "w-[120px] whitespace-nowrap",
      cell: (i: Incident) => (
        <span title={i.opened} className="font-mono text-[12px] text-muted">
          {formatDateTime(i.opened, lang)}
        </span>
      ),
    },
    {
      header: t.colClosed,
      className: "min-w-[150px]",
      cell: (i: Incident) =>
        isOpen || !i.closed ? (
          <span className="text-[13px]">{t.openFor(formatSpan(spanMs(i.opened), t))}</span>
        ) : (
          <span title={i.closed} className="font-mono text-[12px] text-muted">
            {formatDateTime(i.closed, lang)}
            {i.close_reason === "gone" && <span className="block font-sans text-[12px]">{t.closedGone}</span>}
          </span>
        ),
    },
  ];

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />
      {q.isPending && <p className="mt-[26px] mb-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div className="pt-[26px]">
          <p className="m-0">{errorMessage(q.error, lang)}</p>
          <div className="mt-[10px]">
            <Button onClick={() => void q.refetch()}>{k.retry}</Button>
          </div>
        </div>
      )}
      {q.data && (
        <>
          <section aria-labelledby="dang-mo">
            <h2 id="dang-mo" className="m-0 pt-[26px] pb-[10px] text-[16px] font-semibold">
              {t.openHeading(open.length)}
            </h2>
            <TableList caption={t.openHeading(open.length)} rows={open} rowKey={(i) => String(i.id)} empty={emptyOpen} columns={columns(true)} />
          </section>
          <section aria-labelledby="da-dong">
            <h2 id="da-dong" className="m-0 pt-[26px] pb-[10px] text-[16px] font-semibold">
              {t.closedHeading(closed.length)}
            </h2>
            <TableList caption={t.closedHeading(closed.length)} rows={closed} rowKey={(i) => String(i.id)} empty={t.noClosed} columns={columns(false)} />
          </section>
        </>
      )}
    </>
  );
}
