// Bảng cron của từng site: bảng kẻ mảnh như "Lịch sử máy" (mục 7 của
// GIAO-DIEN.md). Mỗi hàng: site, việc, nhịp chạy, lần chạy gần nhất (dấu trạng
// thái và nút mở output), file cron và lệnh (chữ mono, bấm là chép). Site chưa
// bật cron ghi rõ vì sao thay vì để trống.
//
// Output không nằm trong ô bảng (dòng dài làm bảng phình): nút chỉ báo hàng
// nào đang mở, màn hiện EvidenceBlock ngay dưới bảng.

import { Link } from "react-router";
import type { CronEntry, Suspension } from "../../api/types";
import { MonoText } from "../../components/MonoText";
import { StatusMark } from "../../components/StatusMark";
import { SuspendTag } from "../../components/SuspendTag";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings, type Lang } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/cron";
import { formatWhen } from "../thung-rac/model";
import { markOf, parseSchedule, splitDuration } from "./model";

type Props = {
  items: CronEntry[];
  /** Domain đang trong thùng rác: để nói đúng vì sao file cron không có. */
  trashed: ReadonlySet<string>;
  /** M13: domain đang tạm khoá. File cron của site khoá đã đổi tên .suspended, nên hiện "tạm khoá" thay cho gợi ý chạy lại site add. */
  suspended: ReadonlyMap<string, Suspension>;
  /** Domain đang mở output (một lúc một hàng). */
  openDomain: string | null;
  onToggle: (domain: string) => void;
};

type Strings = (typeof strings)["vi"];

function everyText(t: Strings, schedule: string): string {
  const e = parseSchedule(schedule);
  switch (e.kind) {
    case "minutes":
      return e.n === 1 ? t.everyMinute : t.everyMinutes(e.n);
    case "hourly":
      return t.everyHour(e.minute);
    case "daily":
      return t.everyDay(e.hhmm);
    default:
      return t.everyOther;
  }
}

function durationText(t: Strings, ns: number, lang: Lang): string {
  const d = splitDuration(ns);
  const f = new Intl.NumberFormat(lang === "vi" ? "vi-VN" : "en-GB", { maximumFractionDigits: 1 });
  if (d.kind === "ms") return t.durationMs(f.format(d.n));
  if (d.kind === "sec") return t.durationSec(f.format(d.n));
  return t.durationMin(d.m, d.s);
}

export function CronSiteTable({ items, trashed, suspended, openDomain, onToggle }: Props) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();

  return (
    <TableList
      caption={t.tableName}
      rows={items}
      rowKey={(c) => c.domain}
      empty={
        <>
          {t.noSites}
          <Link to="/sites/new">{t.noSitesLink}</Link>
        </>
      }
      columns={[
        { header: t.colSite, cell: (c) => <MonoText>{c.domain}</MonoText> },
        { header: t.colJob, cell: () => t.jobWp },
        {
          header: t.colEvery,
          cell: (c) => (
            <span className="block">
              {everyText(t, c.schedule)}
              {/* Dòng gốc luôn hiện để so với file cron; với dạng lạ nó là thứ duy nhất nói được nhịp. */}
              <span className="block font-mono text-[12px] text-muted" title={t.scheduleRaw}>
                {c.schedule}
              </span>
            </span>
          ),
        },
        {
          header: t.colLast,
          cell: (c) => {
            const run = c.last;
            if (!run) return <span className="text-muted">{t.neverRun}</span>;
            const word = t.statusWord[run.status];
            const open = openDomain === c.domain;
            return (
              <span className="flex flex-col items-start gap-[6px]">
                <span className="flex flex-wrap items-center gap-x-[8px] gap-y-[2px]">
                  {/* Mỗi dấu nằm trong ô 20 px để hình nhỏ (chấm 12 px) không làm chữ bên cạnh lệch hàng. */}
                  <span className="inline-flex size-[20px] items-center justify-center">
                    <StatusMark status={markOf[run.status]} label={k.statusLabel(c.domain, word)} />
                  </span>
                  <span>{word}</span>
                  {run.status === "fail" && <span className="font-mono text-[12px] text-fail-text">{t.exitCode(run.exit_code)}</span>}
                  {run.status === "skipped" && <span className="text-[13px] text-muted">{suspended.has(c.domain) ? t.skippedSuspended : t.skippedNote}</span>}
                </span>
                <span className="font-mono text-[12px] whitespace-nowrap text-muted">
                  {formatWhen(run.started, lang)} · {durationText(t, run.duration_ns, lang)}
                </span>
                <button
                  type="button"
                  aria-expanded={open}
                  aria-controls="cron-evidence"
                  aria-label={open ? t.hideOutputFor(c.domain) : t.showOutputFor(c.domain)}
                  onClick={() => onToggle(c.domain)}
                  className="h-[44px] border border-ink bg-surface px-[10px] text-[12px] font-medium text-ink hover:bg-pre sm:h-[28px]"
                >
                  {open ? t.hideOutput : t.showOutput}
                </button>
              </span>
            );
          },
        },
        {
          header: t.colFile,
          cell: (c) => {
            const how = suspended.get(c.domain);
            if (c.enabled) {
              return (
                <span className="flex flex-col items-start gap-[4px]">
                  <MonoText size={12}>{c.file}</MonoText>
                  <MonoText size={12} muted>
                    {c.command}
                  </MonoText>
                </span>
              );
            }
            return (
              <span className="flex flex-col items-start gap-[6px]">
                {how ? <SuspendTag how={how} /> : <Tag>{t.notEnabled}</Tag>}
                <span className="text-[13px] text-muted">
                  {how ? (
                    t.suspendedWhy
                  ) : trashed.has(c.domain) ? (
                    <>
                      {t.trashedLead}
                      <Link to="/trash">{t.trashedLink}</Link>
                      {t.trashedTail}
                    </>
                  ) : (
                    t.missingWhy
                  )}
                </span>
              </span>
            );
          },
        },
      ]}
    />
  );
}
