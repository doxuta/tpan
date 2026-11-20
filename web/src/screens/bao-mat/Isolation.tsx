// Khối "Cô lập giữa các site" của màn Bảo mật (mục 6.13 và 6.8 của
// KE-HOACH.md). Từ trên xuống:
//   - nút "Áp bản vá cô lập" (nút chính khi needs_harden), như mọi việc đổi
//     máy: bấm → bản xem trước → chạy → /jobs/:id
//   - ma trận site × site: hàng là site "from", cột là site "to", mỗi ô một
//     dấu trạng thái; bấm một ô mở lệnh và output của phép kiểm đó
//   - phép kiểm cấp máy và của từng site (CheckRow)
//   - khi web là apache: chế độ .htaccess của từng site
//
// Dữ liệu là kết quả health ĐÃ LƯU (GET /security/isolation không chạy lại
// phép kiểm), nên chỗ nào chưa có kết quả thì nói rõ "chưa kiểm" thay vì vẽ
// một bảng trống trông như "mọi thứ ổn".

import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Link } from "react-router";
import { errorMessage } from "../../api/client";
import { usePlanFlow } from "../../api/m13-users";
import { asHardenPreview, hardenRequest, useIsolation } from "../../api/m14";
import type { HtaccessRow, Isolation, IsolationCell } from "../../api/m14-types";
import type { CheckResult, Preview } from "../../api/types";
import { Button } from "../../components/Button";
import { DomainText } from "../../components/DomainText";
import { MonoText } from "../../components/MonoText";
import { StatusMark } from "../../components/StatusMark";
import { TableList, type Column } from "../../components/TableList";
import { useStatusName } from "../../components/useStatusName";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bao-mat";
import { strings as khung } from "../../i18n/khung";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";
import { Legend } from "../site/RouteMap";
import { CheckRow } from "./CheckRow";

const labelClass = "m-0 font-mono text-[11px] font-normal tracking-[0.08em] text-muted";

export function IsolationBlock() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useIsolation();

  return (
    <section aria-labelledby="bm-co-lap" className="pt-[34px]">
      <h2 id="bm-co-lap" className="m-0 text-[16px] font-semibold">
        {t.isoHeading}
      </h2>
      <p className="mt-[8px] mb-0 max-w-[720px] text-muted">{t.isoLead}</p>
      {q.isPending && <p className="mt-[14px] mb-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div className="pt-[14px]">
          <p className="mt-0 mb-[4px] font-semibold">{t.isoLoadFailed}</p>
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </div>
      )}
      {q.isSuccess && <IsolationView data={q.data} />}
    </section>
  );
}

function IsolationView({ data }: { data: Isolation }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const flow = usePlanFlow();
  const previewRef = useRef<HTMLDivElement>(null);

  // Bản xem trước (hoặc lỗi dựng nó) nằm cuối khối, dưới ma trận: đưa người dùng tới đó.
  const shown = flow.open ?? (flow.dry.isError ? flow.asked : null);
  useEffect(() => {
    if (shown) {
      previewRef.current?.scrollIntoView({ block: "start" });
      previewRef.current?.focus();
    }
  }, [shown]);

  const cancel = () => {
    flow.close();
    document.getElementById("act-harden")?.focus();
  };

  return (
    <>
      <div className="pt-[14px]">
        {data.needs_harden && <p className="mt-0 mb-[6px] max-w-[720px] font-medium">{t.hardenNeeded}</p>}
        <p className="mt-0 mb-[10px] max-w-[720px] text-muted">{t.hardenHint}</p>
        {/* Nút chính khi có việc để sửa; lúc bản xem trước đang mở thì nút chính là "Chạy" của nó (mỗi khối một nút chính). */}
        <Button id="act-harden" variant={data.needs_harden && !flow.open ? "primary" : "secondary"} onClick={() => flow.ask(hardenRequest)} disabled={flow.dry.isPending || flow.run.isPending}>
          {flow.dry.isPending ? t.previewing : t.hardenButton}
        </Button>
      </div>

      <Matrix sites={data.sites ?? []} cells={data.cells ?? []} />

      <CheckGroup id="bm-cl-may" heading={t.machineHeading} checks={data.machine ?? []} empty={t.machineEmpty} openFailed />
      <CheckGroup id="bm-cl-site" heading={t.perSiteHeading} checks={data.per_site ?? []} empty={t.perSiteEmpty} />

      {data.web === "apache" && <HtaccessTable rows={data.htaccess ?? []} />}

      {flow.dry.isError && flow.asked && (
        <div ref={previewRef} tabIndex={-1} className="pt-[22px] outline-offset-0">
          <ErrorNote error={flow.dry.error} heading={t.hardenPreviewError} checksHeading={t.checksHeading} />
          <div className="pt-[12px]">
            <Button onClick={cancel}>{k.cancel}</Button>
          </div>
        </div>
      )}
      {flow.open && (
        <div ref={previewRef} tabIndex={-1} className="outline-offset-0">
          <HardenSummary preview={flow.open.preview} />
          <PlanReview preview={flow.open.preview} onRun={flow.start} running={flow.run.isPending} error={flow.run.error} onCancel={cancel} />
        </div>
      )}
    </>
  );
}

// ---------------------------------------------------------------- ma trận

const cellId = (r: number, c: number) => `bm-o-${r}-${c}`;
const pairKey = (from: string, to: string) => `${from} ${to}`; // tên miền không có dấu cách

const arrows: Record<string, readonly [number, number]> = {
  ArrowUp: [-1, 0],
  ArrowDown: [1, 0],
  ArrowLeft: [0, -1],
  ArrowRight: [0, 1],
};

function Matrix({ sites, cells }: { sites: string[]; cells: IsolationCell[] }) {
  const t = useStrings(strings);
  const statusName = useStatusName();
  const [sel, setSel] = useState<{ r: number; c: number } | null>(null);

  const byPair = new Map(cells.map((c) => [pairKey(c.from, c.to), c.result]));
  const resultOf = (r: number, c: number) => byPair.get(pairKey(sites[r] ?? "", sites[c] ?? ""));

  const total = sites.length * (sites.length - 1);
  let pass = 0;
  let fail = 0;
  for (let r = 0; r < sites.length; r++) {
    for (let c = 0; c < sites.length; c++) {
      if (r === c) continue;
      const s = resultOf(r, c)?.status;
      if (s === "pass") pass++;
      else if (s === "fail") fail++;
    }
  }
  const anyResult = cells.some((c) => c.result);

  const close = () => {
    const s = sel;
    setSel(null);
    // Trả focus về đúng ô đã mở, để người dùng bàn phím đi tiếp sang ô bên cạnh.
    if (s) document.getElementById(cellId(s.r, s.c))?.focus();
  };

  // Tab vẫn đi qua từng ô; phím mũi tên là đường tắt, vì 12 site đã là 132 ô.
  function onKeyDown(e: KeyboardEvent<HTMLTableElement>) {
    const d = arrows[e.key];
    const at = (e.target as HTMLElement).dataset;
    if (!d || at.r === undefined || at.c === undefined) return;
    let r = Number(at.r) + d[0];
    let c = Number(at.c) + d[1];
    // Ô đường chéo để trống: nhảy qua nó.
    if (r === c) {
      r += d[0];
      c += d[1];
    }
    const next = document.getElementById(cellId(r, c));
    if (next) {
      e.preventDefault();
      next.focus();
    }
  }

  return (
    <div className="pt-[22px]">
      <div className="flex flex-wrap items-end justify-between gap-x-[24px] gap-y-[8px]">
        <h3 id="bm-cl-ma-tran" className={labelClass}>
          {t.matrixHeading}
        </h3>
        <Legend />
      </div>
      <p className="mt-[8px] mb-0 max-w-[720px] text-muted">{t.matrixLead}</p>

      {sites.length < 2 ? (
        <p className="mt-[10px] mb-0 text-muted">{t.matrixTooFew(sites.length)}</p>
      ) : !anyResult ? (
        <p className="mt-[10px] mb-0 max-w-[720px]">{t.matrixNotChecked}</p>
      ) : (
        <>
          <p className="mt-[8px] mb-0 text-[13px] text-muted">
            {t.matrixCount(total, pass, fail, total - pass - fail)} {t.matrixKeys}
          </p>
          {/* Khung cuộn riêng: nhiều site thì bảng cuộn trong khung, cả trang không cuộn ngang. relative để chữ sr-only bị khung cắt. */}
          <div className="relative mt-[10px] max-h-[620px] max-w-full overflow-auto">
            {/* border-separate: với border-collapse, kẻ của ô dính (sticky) không đi theo ô khi cuộn. */}
            <table onKeyDown={onKeyDown} className="border-separate border-spacing-0 text-left">
              <caption className="sr-only">{t.matrixCaption}</caption>
              <thead>
                <tr>
                  <td className="sticky top-0 left-0 z-20 min-w-[150px] border-r border-b border-hair bg-paper p-[8px] align-bottom">
                    <span className={`block text-right ${labelClass}`}>{t.matrixTo}</span>
                    <span className={`block ${labelClass}`}>{t.matrixFrom}</span>
                  </td>
                  {sites.map((s) => (
                    <th key={s} scope="col" className="sticky top-0 z-10 max-w-[120px] min-w-[72px] border-b border-hair bg-paper px-[6px] py-[8px] text-center align-bottom font-mono text-[12px] font-normal [overflow-wrap:anywhere]">
                      <DomainText domain={s} />
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {sites.map((from, r) => (
                  <tr key={from}>
                    <th scope="row" className="sticky left-0 z-10 max-w-[220px] min-w-[150px] border-r border-b border-hair bg-paper py-[8px] pr-[12px] pl-[8px] font-mono text-[13px] font-normal [overflow-wrap:anywhere]">
                      <DomainText domain={from} />
                    </th>
                    {sites.map((to, c) => {
                      if (r === c) return <td key={to} className="border-b border-hair" />;
                      const res = resultOf(r, c);
                      const label = t.cellLabel(from, to, statusName(res?.status ?? "unknown", res?.suspended));
                      const on = sel?.r === r && sel.c === c;
                      return (
                        <td key={to} className="border-b border-hair p-0">
                          {/* Viền focus vẽ vào trong ô (-2 px): ô sát mép khung cuộn và ô cạnh hàng dính vẫn thấy đủ viền. */}
                          <button
                            type="button"
                            id={cellId(r, c)}
                            data-r={r}
                            data-c={c}
                            aria-label={label}
                            title={label}
                            aria-expanded={on}
                            aria-controls="bm-cl-o"
                            onClick={() => (on ? close() : setSel({ r, c }))}
                            className={`flex h-[44px] w-full items-center justify-center border -outline-offset-2 hover:bg-pre ${on ? "border-ink bg-pre" : "border-transparent bg-transparent"}`}
                          >
                            <StatusMark status={res?.status ?? "unknown"} />
                          </button>
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {sel && <Detail id="bm-cl-o" heading={t.cellHeading(sites[sel.r] ?? "", sites[sel.c] ?? "")} result={resultOf(sel.r, sel.c)} onClose={close} />}
        </>
      )}
    </div>
  );
}

/**
 * Bằng chứng của ô (hoặc dòng) vừa bấm: dùng lại CheckRow ở trạng thái mở, nên
 * lệnh, output, nguyên nhân và cách sửa tay trông như mọi dòng khác của màn.
 * Bấm vào dòng đó là đóng.
 */
function Detail({ id, heading, result, onClose }: { id: string; heading: string; result: CheckResult | undefined; onClose: () => void }) {
  const t = useStrings(strings);
  const ref = useRef<HTMLDivElement>(null);
  // Khối nằm dưới bảng: đưa focus tới nó mỗi lần chọn ô khác, nếu không người dùng bàn phím phải Tab qua hết các ô còn lại.
  useEffect(() => {
    ref.current?.scrollIntoView({ block: "nearest" });
    ref.current?.focus({ preventScroll: true });
  }, [heading]);

  return (
    <div id={id} ref={ref} tabIndex={-1} className="mt-[12px] outline-offset-0">
      <div className="font-mono text-[13px] font-semibold [overflow-wrap:anywhere]">{heading}</div>
      {result ? (
        <ul className="m-0 mt-[4px] list-none border-t border-hair p-0">
          <CheckRow check={result} open onToggle={onClose} />
        </ul>
      ) : (
        <>
          <p className="mt-[6px] mb-0 max-w-[720px] text-muted">{t.cellNoResult}</p>
          <div className="pt-[10px]">
            <Button onClick={onClose}>{t.cellClose}</Button>
          </div>
        </>
      )}
    </div>
  );
}

// ------------------------------------------------- phép kiểm dưới ma trận

type GroupProps = {
  id: string;
  heading: string;
  checks: CheckResult[];
  empty: string;
  /** Mở sẵn dòng chưa đạt. Nhóm của từng site để đóng: máy nâng từ bản cũ có thể có hai dòng lỗi cho mỗi site. */
  openFailed?: boolean;
};

function CheckGroup({ id, heading, checks, empty, openFailed = false }: GroupProps) {
  const [open, setOpen] = useState<Set<string>>(() => new Set(openFailed ? checks.filter((c) => c.status === "fail" || c.status === "warn").map((c) => c.id) : []));
  const toggle = (cid: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (!next.delete(cid)) next.add(cid);
      return next;
    });

  return (
    <div className="pt-[22px]">
      <h3 id={id} className={labelClass}>
        {heading}
      </h3>
      {checks.length === 0 ? (
        <p className="mt-[8px] mb-0 max-w-[720px]">{empty}</p>
      ) : (
        <ul aria-labelledby={id} className="m-0 mt-[8px] list-none border-t border-hair p-0">
          {checks.map((c) => (
            <CheckRow key={c.id} check={c} open={open.has(c.id)} onToggle={() => toggle(c.id)} />
          ))}
        </ul>
      )}
    </div>
  );
}

// ------------------------------------------------------ chế độ .htaccess

const htButtonId = (domain: string) => `bm-ht-${domain}`;

function HtaccessTable({ rows }: { rows: HtaccessRow[] }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const statusName = useStatusName();
  const { lang } = useLang();
  const [sel, setSel] = useState<string | null>(null);
  const selected = rows.find((r) => r.domain === sel);

  const close = () => {
    const d = sel;
    setSel(null);
    if (d) document.getElementById(htButtonId(d))?.focus();
  };

  const columns: Column<HtaccessRow>[] = [
    {
      header: t.htColDomain,
      className: "min-w-[160px] font-mono text-[13px]",
      cell: (r) => <DomainText domain={r.domain} />,
    },
    { header: t.htColMode, className: "w-[110px] font-mono text-[13px]", cell: (r) => r.mode },
    {
      header: t.htColResult,
      className: "min-w-[220px]",
      cell: (r) => {
        const res = r.result;
        if (!res) {
          return (
            <span className="inline-flex items-center gap-[8px] text-muted">
              <span className="flex h-[20px] w-[20px] flex-none items-center justify-center">
                <StatusMark status="unknown" />
              </span>
              <span className="sr-only">{k.status.unknown}: </span>
              {t.htNoResult}
            </span>
          );
        }
        return (
          <button
            type="button"
            id={htButtonId(r.domain)}
            aria-expanded={sel === r.domain}
            aria-controls="bm-ht-o"
            onClick={() => (sel === r.domain ? close() : setSel(r.domain))}
            className="inline-flex items-start gap-[8px] bg-transparent p-0 text-left text-ink hover:underline max-sm:min-h-[24px]"
          >
            <span className="flex h-[20px] w-[20px] flex-none items-center justify-center">
              <StatusMark status={res.status} />
            </span>
            <span>
              <span className="sr-only">{statusName(res.status, res.suspended)}: </span>
              {tx(res.summary, lang)}
            </span>
          </button>
        );
      },
    },
    {
      header: t.htColAction,
      className: "pr-0 whitespace-nowrap",
      cell: (r) => (
        <Link to={`/sites/${encodeURIComponent(r.domain)}`} aria-label={t.htOpenSiteFor(r.domain)}>
          {t.htOpenSite}
        </Link>
      ),
    },
  ];

  return (
    <div className="pt-[22px]">
      <h3 className={labelClass}>{t.htHeading}</h3>
      <p className="mt-[8px] mb-[10px] max-w-[720px] text-muted">{t.htLead}</p>
      <TableList caption={t.htCaption} rows={rows} rowKey={(r) => r.domain} empty={t.htEmpty} columns={columns} />
      {selected && <Detail id="bm-ht-o" heading={selected.domain} result={selected.result} onClose={close} />}
    </div>
  );
}

// ------------------------------------- phía trên bản xem trước của bản vá

/** Site nào đổi chế độ .htaccess, site nào giữ day-du và vướng dòng nào: người duyệt cần biết trước khi bấm Chạy. */
function HardenSummary({ preview }: { preview: Preview }) {
  const t = useStrings(strings);
  const p = asHardenPreview(preview);
  const switched = p.switched ?? [];
  const kept = p.kept ?? [];

  return (
    <section aria-labelledby="bm-cl-che-do" className="pt-[26px]">
      <h3 id="bm-cl-che-do" className={labelClass}>
        {t.hardenModesLabel}
      </h3>
      <p className="mt-[8px] mb-0 font-medium">{t.hardenSwitched(switched.length)}</p>
      {switched.length > 0 && (
        <ul className="m-0 mt-[6px] flex list-none flex-wrap gap-x-[16px] gap-y-[6px] p-0">
          {switched.map((d) => (
            <li key={d}>
              <MonoText>{d}</MonoText>
            </li>
          ))}
        </ul>
      )}
      {kept.length > 0 && (
        <>
          <p className="mt-[14px] mb-0 font-medium">{t.hardenKept(kept.length)}</p>
          <ul className="m-0 mt-[6px] list-none border-t border-hair p-0">
            {kept.map((s) => (
              <li key={s.domain} className="border-b border-hair py-[8px]">
                <MonoText bold>{s.domain}</MonoText>
                {(s.findings ?? []).length === 0 ? (
                  <p className="mt-[4px] mb-0 text-muted">{t.hardenNoFindings}</p>
                ) : (
                  <ul className="m-0 mt-[4px] list-none p-0">
                    {(s.findings ?? []).map((f) => (
                      <li key={`${f.file}:${f.line}:${f.directive}`} className="flex flex-wrap items-baseline gap-x-[12px] gap-y-[2px] py-[2px]">
                        <MonoText size={12}>{`${f.file}:${f.line}`}</MonoText>
                        <span className="font-mono text-[12px] font-semibold [overflow-wrap:anywhere]">{f.directive}</span>
                      </li>
                    ))}
                  </ul>
                )}
              </li>
            ))}
          </ul>
          <p className="mt-[8px] mb-0 max-w-[720px] text-muted">{t.hardenKeptHint}</p>
        </>
      )}
    </section>
  );
}
