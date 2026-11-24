// Thẻ "Thử tấn công" của màn Bảo mật (M17; mục 6.15 của KE-HOACH.md, mục 7 của
// GIAO-DIEN.md). Từ trên xuống, trong một thẻ:
//   - lời dẫn, nút chọn lượt (cả hai, ngoài lồng, trong lồng) và nút "Thử tấn công"
//     (bấm → bản xem trước nói hai tên miền site thử → chạy → thẻ theo dõi job)
//   - lượt chưa dọn: câu báo và nút "Dọn lượt thử còn sót"
//   - kết quả lượt gần nhất: một dòng tóm tắt, câu báo lộ hay không, rồi bảng
//     mỗi đường một hàng, hai cột Ngoài lồng và Trong lồng. Mỗi ô là dấu trạng
//     thái kèm chữ; bấm ô thì bằng chứng hiện ngay dưới hàng. Ô lộ mở sẵn.
//   - các đường không thuộc stack gập vào một khối đóng sẵn
// Việc đổi máy chỉ có hai (chạy lượt thử, dọn lượt sót), dùng chung một
// usePlanFlow nên cùng lúc chỉ có một bản xem trước.

import { Fragment, useEffect, useRef, useState } from "react";
import { errorMessage } from "../../api/client";
import { usePlanFlow } from "../../api/m13-users";
import { probeCleanupPath, probeCleanupRequest, probeRequest, useProbe } from "../../api/m17";
import type { ProbeOnly, ProbeReport, ProbeResult } from "../../api/m17-types";
import type { Preview } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Disclosure } from "../../components/Disclosure";
import { DomainText } from "../../components/DomainText";
import { EmptyState } from "../../components/EmptyState";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { Code } from "../../components/MonoText";
import { Notice } from "../../components/Notice";
import { SegmentButton } from "../../components/SegmentButton";
import { StatusMark } from "../../components/StatusMark";
import { Tag } from "../../components/Tag";
import { tx, useLang, useStrings, type Lang } from "../../i18n";
import { strings } from "../../i18n/bao-mat";
import { strings as khung } from "../../i18n/khung";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";
import { asProbePreview, cellKey, cellOpen, cellStatus, countCells, countParts, probeDomains, splitPending, splitResults, type ProbeColumn } from "./probe-model";

const subHeadingClass = "m-0 text-[14px] font-semibold";
const thClass = "px-[12px] py-[8px] text-left text-[12px] font-medium text-muted";

const locale: Record<Lang, string> = { vi: "vi-VN", en: "en-GB" };

function formatWhen(iso: string, lang: Lang): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString(locale[lang], { hour12: false });
}

export function ProbeBlock() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useProbe();
  const flow = usePlanFlow();
  const [only, setOnly] = useState<ProbeOnly>("");
  const previewRef = useRef<HTMLDivElement>(null);

  const { running, stale } = splitPending(q.data?.pending);
  const busy = flow.dry.isPending || flow.run.isPending;

  // Bản xem trước (hoặc lỗi dựng nó) nằm dưới thẻ: đưa người dùng tới đó.
  const shown = flow.open ?? (flow.dry.isError ? flow.asked : null);
  useEffect(() => {
    if (shown) {
      previewRef.current?.scrollIntoView({ block: "start" });
      previewRef.current?.focus();
    }
  }, [shown]);

  const cleaning = flow.asked?.path === probeCleanupPath;
  const cancel = () => {
    const id = cleaning ? "act-probe-cleanup" : "act-probe";
    flow.close();
    document.getElementById(id)?.focus();
  };
  // Bản xem trước đang mở là của lượt cũ: đổi lượt thì bỏ nó.
  const pick = (v: ProbeOnly) => {
    if (flow.open || flow.asked) flow.close();
    setOnly(v);
  };

  return (
    <>
      <Card
        id="bm-thu-tan-cong"
        title={t.probeHeading}
        titleId="bm-thu-tan-cong-ten"
        actions={
          <>
            <SegmentButton
              label={t.probeOnlyLabel}
              value={only}
              onChange={pick}
              options={[
                { value: "", label: t.probeOnlyBoth },
                { value: "ngoai", label: t.probeOnlyOutside },
                { value: "trong", label: t.probeOnlyInside },
              ]}
            />
            <Button id="act-probe" onClick={() => flow.ask(probeRequest(only))} disabled={busy || running.length > 0}>
              {flow.dry.isPending && !cleaning ? t.previewing : t.probeButton}
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-[16px]">
          <p className="m-0 max-w-[72ch] text-muted">{t.probeLead}</p>

          {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
          {q.isError && (
            <div>
              <p className="mt-0 mb-[4px] font-semibold">{t.probeLoadFailed}</p>
              <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
              <Button onClick={() => void q.refetch()}>{k.retry}</Button>
            </div>
          )}

          {q.isSuccess && (
            <>
              {running.length > 0 && <p className="m-0 max-w-[72ch]">{t.probeRunning(probeDomains(running).join(", "))}</p>}
              {stale.length > 0 && (
                <div className="flex flex-wrap items-center justify-between gap-x-[16px] gap-y-[8px]">
                  <div className="min-w-0 flex-1">
                    <Notice kind="warn">
                      <span className="[overflow-wrap:anywhere]">{t.probePending(stale.length, probeDomains(stale).join(", "))}</span>
                    </Notice>
                  </div>
                  <Button id="act-probe-cleanup" onClick={() => flow.ask(probeCleanupRequest)} disabled={busy}>
                    {flow.dry.isPending && cleaning ? t.previewing : t.probeCleanupButton}
                  </Button>
                </div>
              )}
              {q.data.last ? <LastReport key={q.data.last.id} report={q.data.last} /> : <EmptyState>{t.probeEmpty}</EmptyState>}
            </>
          )}
        </div>
      </Card>

      {(flow.open || (flow.dry.isError && flow.asked)) && (
        <div ref={previewRef} tabIndex={-1} className="flex flex-col gap-[20px] outline-offset-0">
          {flow.open ? (
            <>
              {flow.open.req.path !== probeCleanupPath && <ProbeSummary preview={flow.open.preview} only={only} />}
              <PlanReview preview={flow.open.preview} onRun={flow.start} running={flow.run.isPending} error={flow.run.error} onCancel={cancel} />
            </>
          ) : (
            <Card>
              <ErrorNote error={flow.dry.error} heading={cleaning ? t.probeCleanupPreviewError : t.probePreviewError} checksHeading={t.checksHeading} />
              <div className="pt-[12px]">
                <Button onClick={cancel}>{k.cancel}</Button>
              </div>
            </Card>
          )}
        </div>
      )}
    </>
  );
}

// ------------------------------------------- phía trên bản xem trước

/** Hai tên miền site thử sẽ được tạo: người duyệt thấy đúng thứ sẽ có trên máy trước khi bấm Chạy. */
function ProbeSummary({ preview, only }: { preview: Preview; only: ProbeOnly }) {
  const t = useStrings(strings);
  const run = asProbePreview(preview).run;
  const passes = { "": t.probeOnlyBoth, ngoai: t.probeOnlyOutside, trong: t.probeOnlyInside }[only];

  return (
    <Card title={t.probePreviewHeading} titleId="bm-thu-xem-truoc">
      <p className="m-0 max-w-[72ch] text-muted">{t.probePreviewLead}</p>
      <dl className="m-0 mt-[12px] grid grid-cols-[auto_1fr] gap-x-[16px] gap-y-[6px]">
        {run && (
          <>
            <dt className="text-muted">{t.probePreviewA}</dt>
            <dd className="m-0 min-w-0 font-medium">
              <DomainText domain={run.domain_a} />
            </dd>
            <dt className="text-muted">{t.probePreviewB}</dt>
            <dd className="m-0 min-w-0 font-medium">
              <DomainText domain={run.domain_b} />
            </dd>
          </>
        )}
        <dt className="text-muted">{t.probePreviewPasses}</dt>
        <dd className="m-0">{passes}</dd>
      </dl>
    </Card>
  );
}

// ------------------------------------------------ kết quả lượt gần nhất

function LastReport({ report }: { report: ProbeReport }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const { shown, na } = splitResults(report.results);
  const parts = countParts(countCells(shown));

  return (
    <div className="flex flex-col gap-[12px] border-t border-hair pt-[16px]">
      <div className="flex flex-wrap items-center gap-x-[12px] gap-y-[6px]">
        <h3 className={subHeadingClass}>{t.probeLastHeading}</h3>
        <span className="text-[13px] text-muted">{t.probeWhen(formatWhen(report.started, lang))}</span>
        <Tag>{report.web}</Tag>
        <span className="text-[13px] text-muted">{t.probePaths(shown.length)}</span>
        {report.only !== "" && <Tag>{t.probeOnlyDone[report.only]}</Tag>}
      </div>

      {report.exposed > 0 ? (
        <Notice kind="fail" role="status">
          <span className="font-medium">{t.probeExposedNotice(report.exposed)}</span>
        </Notice>
      ) : (
        <Notice kind="pass" role="status">
          <span className="font-medium">{t.probeSafeNotice}</span>
        </Notice>
      )}
      {parts.length > 0 && <p className="m-0 text-[13px] text-muted">{parts.map((p) => t.probeCount(p.n, t.probeStatus[p.key])).join(" · ")}</p>}

      <ResultTable results={shown} />

      {na.length > 0 && (
        <div className="-mt-[4px]">
          <Disclosure flush summary={<span className="font-medium">{t.probeNaSummary(na.length)}</span>}>
            <ul className="m-0 list-none p-0">
              {na.map((r) => (
                <li key={r.path} className="flex flex-wrap items-baseline gap-x-[12px] gap-y-[2px] py-[3px]">
                  <Code>{r.path}</Code>
                  <span>{tx(r.title, lang)}</span>
                  <span className="text-[13px] text-muted">{tx(r.outside.detail, lang)}</span>
                </li>
              ))}
            </ul>
          </Disclosure>
        </div>
      )}
    </div>
  );
}

const cellKeyOf = (path: string, col: ProbeColumn) => `${path}:${col}`;
const panelId = (path: string, col: ProbeColumn) => `bm-thu-o-${path}-${col}`;

function ResultTable({ results }: { results: ProbeResult[] }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  // Phần người dùng đã bấm; ô nào chưa bấm thì theo mặc định (ô lộ mở, ô khác đóng).
  const [over, setOver] = useState<Record<string, boolean>>({});

  const colName: Record<ProbeColumn, string> = { outside: t.probeColOutside, inside: t.probeColInside };
  const cols: ProbeColumn[] = ["outside", "inside"];

  return (
    // Khung cuộn riêng: màn hẹp thì bảng cuộn trong khung, cả trang không cuộn ngang.
    <div className="max-w-full overflow-x-auto rounded-ctl border border-hair">
      <table className="w-full min-w-[600px] border-collapse text-left">
        <caption className="sr-only">{t.probeCaption}</caption>
        <thead>
          <tr className="bg-soft">
            <th scope="col" className={`${thClass} w-[64px]`}>
              {t.probeColPath}
            </th>
            <th scope="col" className={thClass}>
              {t.probeColName}
            </th>
            {cols.map((c) => (
              <th key={c} scope="col" className={`${thClass} w-[190px]`}>
                {colName[c]}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {results.map((r) => {
            const opened = cols.filter((c) => cellOpen(over, cellKeyOf(r.path, c), r[c].status));
            const labelOf = (c: ProbeColumn) => t.probeCellLabel(r.path, colName[c], t.probeStatus[cellKey(r[c].status)]);
            return (
              <Fragment key={r.path}>
                <tr className="border-t border-hair">
                  <th scope="row" className="w-[64px] px-[12px] py-[6px] text-left align-top font-normal">
                    <Code>{r.path}</Code>
                  </th>
                  <td className="px-[12px] py-[6px] align-top [overflow-wrap:anywhere]">{tx(r.title, lang)}</td>
                  {cols.map((c) => {
                    const on = opened.includes(c);
                    return (
                      <td key={c} className="p-0 align-top">
                        {/* Viền focus vẽ vào trong ô (-2 px): ô sát mép khung cuộn vẫn thấy đủ viền. */}
                        <button
                          type="button"
                          aria-expanded={on}
                          aria-controls={on ? panelId(r.path, c) : undefined}
                          aria-label={labelOf(c)}
                          onClick={() => setOver((o) => ({ ...o, [cellKeyOf(r.path, c)]: !on }))}
                          className="flex w-full items-center gap-[8px] bg-transparent px-[12px] py-[6px] max-sm:min-h-[44px] text-left text-ink -outline-offset-2 hover:bg-soft"
                        >
                          <StatusMark status={cellStatus(r[c].status)} size="sm" />
                          <span className={cellKey(r[c].status) === "exposed" ? "font-semibold" : ""}>{t.probeStatus[cellKey(r[c].status)]}</span>
                        </button>
                      </td>
                    );
                  })}
                </tr>
                {opened.length > 0 && (
                  <tr>
                    <td colSpan={4} className="border-t border-hair bg-soft p-[12px]">
                      <div className="flex flex-col gap-[16px]">
                        {opened.map((c) => (
                          <div key={c} id={panelId(r.path, c)} role="group" aria-label={labelOf(c)}>
                            <div className="mb-[4px] text-[13px] font-medium">{labelOf(c)}</div>
                            <p className="m-0 mb-[8px] max-w-[72ch] text-[13px] [overflow-wrap:anywhere]">{tx(r[c].detail, lang)}</p>
                            {r[c].evidence && r[c].evidence.length > 0 ? <EvidenceBlock evidence={r[c].evidence} clamp={20} /> : <p className="m-0 text-[13px] text-muted">{t.probeNoEvidence}</p>}
                          </div>
                        ))}
                      </div>
                    </td>
                  </tr>
                )}
              </Fragment>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
