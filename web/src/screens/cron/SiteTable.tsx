// Bảng cron của từng site (bản 2): một TableList trong thẻ. Mỗi hàng: site,
// việc, nhịp chạy, lần chạy gần nhất (dấu trạng thái và nút mở output), file
// cron và lệnh (chữ mono, bấm là chép). Site chưa bật cron ghi rõ vì sao thay
// vì để trống.
//
// Output không nằm trong ô bảng (dòng dài làm bảng phình): nút chỉ báo hàng
// nào đang mở, màn hiện thẻ bằng chứng ngay dưới bảng.

import { Link } from "react-router";
import type { CronEntry, Suspension } from "../../api/types";
import { DomainText } from "../../components/DomainText";
import { Button } from "../../components/Button";
import { Code, MonoText } from "../../components/MonoText";
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
      title={t.sitesHeading}
      titleId="cron-sites"
      caption={t.tableName}
      minWidth={860}
      rows={items}
      rowKey={(c) => c.domain}
      above={<p className="m-0 max-w-[72ch] px-[16px] py-[12px] text-[13px] text-muted">{t.sitesHint}</p>}
      footer={<span>{t.readOnly}</span>}
      empty={
        <>
          {t.noSites}
          <Link to="/sites/new">{t.noSitesLink}</Link>
        </>
      }
      columns={[
        {
          header: t.colSite,
          // v0.2.14: tên miền dài xuống dòng sau dấu chấm thay vì kéo rộng bảng.
          nowrap: false,
          className: "min-w-[180px] max-w-[240px]",
          cell: (c) => (
            <span className="font-medium">
              <DomainText domain={c.domain} />
            </span>
          ),
        },
        { header: t.colJob, nowrap: false, className: "min-w-[160px]", cell: () => t.jobWp },
        {
          header: t.colEvery,
          cell: (c) => (
            <span className="block">
              {everyText(t, c.schedule)}
              {/* Dòng gốc luôn hiện để so với file cron; với dạng lạ nó là thứ duy nhất nói được nhịp. */}
              <span className="block text-muted" title={t.scheduleRaw}>
                <Code>{c.schedule}</Code>
              </span>
            </span>
          ),
        },
        {
          header: t.colLast,
          nowrap: false,
          className: "min-w-[220px]",
          cell: (c) => {
            const run = c.last;
            if (!run) return <span className="text-muted">{t.neverRun}</span>;
            const word = t.statusWord[run.status];
            const open = openDomain === c.domain;
            return (
              <span className="flex flex-col items-start gap-[4px]">
                <span className="flex flex-wrap items-center gap-x-[8px] gap-y-[2px]">
                  {/* Mỗi dấu nằm trong ô 20 px để hình nhỏ (chấm 12 px) không làm chữ bên cạnh lệch hàng. */}
                  <span className="inline-flex size-[20px] items-center justify-center">
                    <StatusMark status={markOf[run.status]} label={k.statusLabel(c.domain, word)} />
                  </span>
                  <span>{word}</span>
                  {run.status === "fail" && <span className="text-[12.5px] text-muted">{t.exitCode(run.exit_code)}</span>}
                </span>
                {run.status === "skipped" && <span className="text-[12.5px] text-muted">{suspended.has(c.domain) ? t.skippedSuspended : t.skippedNote}</span>}
                <span className="text-[12.5px] whitespace-nowrap text-muted tabular-nums">
                  {formatWhen(run.started, lang)} · {durationText(t, run.duration_ns, lang)}
                </span>
                <Button
                  size="sm"
                  variant="text"
                  className="-ml-[10px]"
                  aria-expanded={open}
                  aria-controls={open ? "cron-evidence" : undefined}
                  aria-label={open ? t.hideOutputFor(c.domain) : t.showOutputFor(c.domain)}
                  onClick={() => onToggle(c.domain)}
                >
                  {open ? t.hideOutput : t.showOutput}
                </Button>
              </span>
            );
          },
        },
        {
          header: t.colFile,
          nowrap: false,
          className: "min-w-[200px]",
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
                <span className="max-w-[320px] text-[12.5px] text-muted">
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
