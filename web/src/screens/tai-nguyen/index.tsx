// Màn Tài nguyên (/usage, M17; mục 7 của docs/GIAO-DIEN.md). Số đọc ngay từ
// cgroup của từng site trong lồng, tự đọc lại mỗi 30 giây (useUsage). User
// thường chỉ thấy site của mình (server lọc), nên bảng "Theo user" và thẻ gợi ý
// chỉ vẽ cho admin. CPU là số tính giữa hai lần đọc liên tiếp (cpuBetween):
// giữ lần đọc trước trong một ref, nên lần đầu mở màn chưa có số.

import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import { errorMessage } from "../../api/client";
import { useIdentity } from "../../api/m13-dbs";
import { useSuggestions, useUsage } from "../../api/m17";
import type { CageUsage, SiteUsage } from "../../api/m17-types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { EmptyState } from "../../components/EmptyState";
import { ScreenHeader } from "../../components/ScreenHeader";
import { TableList } from "../../components/TableList";
import { UsageMeter } from "../../components/UsageMeter";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/tai-nguyen";
import { fmtNumber } from "../bang-dieu-khien/model";
import { byOwner, cap, cpuBetween, hits, mib, pct } from "./model";
import { Suggestions } from "./Suggestions";

/** Số đọc của lần trước, theo tên miền, để tính CPU giữa hai lần đọc. */
function usePrevious(data: SiteUsage[] | undefined): Map<string, CageUsage> {
  const last = useRef<SiteUsage[]>(undefined);
  const [prev, setPrev] = useState<Map<string, CageUsage>>(new Map());
  useEffect(() => {
    // Chạy lại với cùng một mảng (StrictMode, render lại) thì không dịch lần đọc.
    if (!data || last.current === data) return;
    if (last.current) setPrev(new Map(last.current.map((r) => [r.domain, r.usage])));
    last.current = data;
  }, [data]);
  return prev;
}

export default function Usage() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const { isAdmin } = useIdentity();
  const q = useUsage();
  const suggestions = useSuggestions(isAdmin);
  const prev = usePrevious(q.data);

  const rows = useMemo(() => [...(q.data ?? [])].sort((a, b) => a.domain.localeCompare(b.domain)), [q.data]);
  const owners = useMemo(() => byOwner(rows), [rows]);

  const num = (n: number) => fmtNumber(n, lang, 0);
  const mibText = (bytes: number) => t.mib(num(mib(bytes)));

  const siteColumns = [
    {
      header: t.colSite,
      cell: (r: SiteUsage) => (
        <span className="flex min-w-0 flex-col">
          {/* Tên miền dài nằm một dòng, cắt bằng dấu ba chấm; đủ chữ trong title. */}
          <Link to={`/sites/${encodeURIComponent(r.domain)}`} title={r.domain} className="inline-block max-w-[320px] truncate font-medium no-underline hover:underline">
            {r.domain}
          </Link>
          {!r.usage.running && <span className="text-[12.5px] text-muted">{t.notRunning}</span>}
        </span>
      ),
    },
    ...(isAdmin ? [{ header: t.colOwner, cell: (r: SiteUsage) => r.owner }] : []),
    {
      header: t.colRam,
      cell: (r: SiteUsage) => {
        const max = cap(r.limits.ram_mb);
        const used = mib(r.usage.mem_current);
        return r.usage.running ? (
          <UsageMeter used={t.mib(num(used))} limit={max === null ? null : t.mib(num(max))} percent={pct(used, max)} />
        ) : (
          <UsageMeter used={null} limit={max === null ? null : t.mib(num(max))} hint={t.notRunningHint} />
        );
      },
    },
    { header: t.colRamPeak, align: "right" as const, cell: (r: SiteUsage) => (r.usage.running ? mibText(r.usage.mem_peak) : <span className="text-muted">—</span>) },
    {
      header: t.colTasks,
      cell: (r: SiteUsage) => {
        const max = cap(r.limits.tasks);
        return r.usage.running ? (
          <UsageMeter used={num(r.usage.tasks)} limit={max === null ? null : num(max)} percent={pct(r.usage.tasks, max)} />
        ) : (
          <UsageMeter used={null} limit={max === null ? null : num(max)} hint={t.notRunningHint} />
        );
      },
    },
    {
      header: t.colHits,
      align: "right" as const,
      cell: (r: SiteUsage) =>
        !r.usage.running ? (
          <span className="text-muted">—</span>
        ) : hits(r.usage) === 0 ? (
          t.hitsNone
        ) : (
          <span title={t.hitsHint}>{t.hits(r.usage.oom_kills, r.usage.tasks_hits)}</span>
        ),
    },
    {
      header: t.colCpu,
      align: "right" as const,
      cell: (r: SiteUsage) => {
        const cpu = r.usage.running ? cpuBetween(prev.get(r.domain), r.usage) : null;
        return cpu === null ? (
          <span className="text-muted" title={r.usage.running ? t.cpuWait : t.notRunningHint}>
            —
          </span>
        ) : (
          <span title={t.cpuHint}>{t.percent(fmtNumber(cpu, lang, 1))}</span>
        );
      },
    },
  ];

  return (
    <>
      <ScreenHeader title={t.title} summary={q.isSuccess ? t.summary(rows.length) : undefined} />

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
      {q.isSuccess && rows.length === 0 && (
        <Card>
          <EmptyState action={isAdmin ? <Link to="/packages">{t.emptyLink}</Link> : undefined}>
            {t.emptyLead}
            {isAdmin && ` ${t.emptyAdmin}`}
          </EmptyState>
        </Card>
      )}
      {q.isSuccess && rows.length > 0 && (
        <>
          <TableList
            title={t.siteTitle}
            titleId="usage-sites"
            caption={t.siteCaption}
            count={rows.length}
            minWidth={isAdmin ? 960 : 860}
            rows={rows}
            rowKey={(r) => r.domain}
            empty={t.emptyLead}
            columns={siteColumns}
          />

          {isAdmin && (
            <TableList
              title={t.userTitle}
              titleId="usage-users"
              caption={t.userCaption}
              count={owners.length}
              minWidth={820}
              rows={owners}
              rowKey={(o) => o.owner}
              empty={t.emptyLead}
              footer={<span>{t.userNote}</span>}
              columns={[
                { header: t.colUser, cell: (o) => <span className="font-medium">{o.owner}</span> },
                { header: t.colSites, align: "right", cell: (o) => num(o.sites) },
                { header: t.colRamTotal, align: "right", cell: (o) => mibText(o.memCurrent) },
                { header: t.colPeakMax, align: "right", cell: (o) => mibText(o.memPeakMax) },
                { header: t.colTasks, align: "right", cell: (o) => num(o.tasks) },
                {
                  header: t.colHits,
                  align: "right",
                  cell: (o) => (o.oomKills + o.tasksHits === 0 ? t.hitsNone : <span title={t.hitsHint}>{t.hits(o.oomKills, o.tasksHits)}</span>),
                },
              ]}
            />
          )}

          {isAdmin && <Suggestions query={suggestions} />}
        </>
      )}
    </>
  );
}
