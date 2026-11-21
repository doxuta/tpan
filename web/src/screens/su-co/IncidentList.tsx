// /incidents: sự cố đang mở trước, đã đóng sau (mục 6.7 của KE-HOACH.md). Bản 2:
// hai TableList, mỗi bảng tự vẽ thẻ có tiêu đề "Đang mở · N" và "Đã đóng · N". Một
// lần gọi ?state=all rồi tách hai khối ở trình duyệt, để không gọi hai lần cho cùng dữ liệu.

import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { errorMessage, get } from "../../api/client";
import { useOverview } from "../../api/queries";
import type { Incident } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { ScreenHeader } from "../../components/ScreenHeader";
import { StatusMark } from "../../components/StatusMark";
import { TableList, type Column } from "../../components/TableList";
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

  const columns = (isOpen: boolean): Column<Incident>[] => [
    {
      header: t.colIncident,
      nowrap: false,
      className: "min-w-[240px]",
      cell: (i) => {
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
    { header: t.colTarget, nowrap: false, className: "min-w-[150px]", cell: (i) => <Target target={i.target} /> },
    { header: t.colStage, cell: (i) => t.stages[i.stage] },
    {
      header: t.colOpened,
      cell: (i) => (
        <span title={i.opened} className="text-[13px] text-muted">
          {formatDateTime(i.opened, lang)}
        </span>
      ),
    },
    {
      header: t.colClosed,
      nowrap: false,
      className: "min-w-[150px]",
      cell: (i) =>
        isOpen || !i.closed ? (
          <span className="text-[13px]">{t.openFor(formatSpan(spanMs(i.opened), t))}</span>
        ) : (
          <span title={i.closed} className="text-[13px] text-muted">
            {formatDateTime(i.closed, lang)}
            {i.close_reason === "gone" && <span className="block text-[12px]">{t.closedGone}</span>}
          </span>
        ),
    },
  ];

  return (
    <>
      <ScreenHeader crumbs={[{ label: k.groups.may }, { label: t.title }]} title={t.title} summary={t.summary} />
      {q.isPending && (
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {q.isError && (
        <Card>
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {q.data && (
        <>
          <TableList title={t.openHeading(open.length)} titleId="dang-mo" minWidth={820} rows={open} rowKey={(i) => String(i.id)} empty={emptyOpen} columns={columns(true)} />
          <TableList title={t.closedHeading(closed.length)} titleId="da-dong" minWidth={820} rows={closed} rowKey={(i) => String(i.id)} empty={t.noClosed} columns={columns(false)} />
        </>
      )}
    </>
  );
}
