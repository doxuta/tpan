// Bảng các bản PHP của màn Stack (GET /api/v1/php): bản nào đã cài, bản nào là
// mặc định, site nào đang dùng, nguồn, hạn hỗ trợ, bản WordPress khuyên dùng.
// Thao tác: bản chưa cài có nút "Cài"; bản đã cài, không site nào dùng và không
// phải mặc định có nút "Gỡ". Cùng nhịp với màn Cập nhật: bấm → POST ?dry_run=1
// → PlanReview → POST thật → /jobs/:id. Một bản xem trước tại một thời điểm,
// nằm dưới bảng. Máy chủ từ chối (409 kèm checks, ví dụ php_in_use) thì ErrorNote
// hiện câu của nó ngay dưới bảng.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { useJobTracker } from "../../components/JobTracker";
import { errorMessage, get, post } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { JobStarted, PHPList, PHPSupport, PHPVersionRow, Preview } from "../../api/types";
import { Button } from "../../components/Button";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/stack";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";
import { type Action, installAction, phpKey, removeAction, sourceName, stackKey } from "./model";

export function Php() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({ queryKey: phpKey, queryFn: () => get<PHPList>("/api/v1/php") });

  return (
    <section aria-labelledby="st-php" className="pt-[26px] pb-[8px]">
      <h2 id="st-php" className="m-0 pb-[10px] text-[16px] font-semibold">
        {t.phpHeading}
      </h2>
      {q.isPending && <p className="m-0 border-t border-hair pt-[10px] text-muted">{k.loading}</p>}
      {q.isError && (
        <div className="border-t border-hair pt-[10px]">
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </div>
      )}
      {q.isSuccess && <Loaded list={q.data} />}
    </section>
  );
}

function Loaded({ list }: { list: PHPList }) {
  const t = useStrings(strings);
  const qc = useQueryClient();
  const { track } = useJobTracker();
  // Go có thể trả null cho danh sách rỗng.
  const versions = list.versions ?? [];
  const names = { ubuntu: t.sourceUbuntu, sury: t.sourcePhpSury };

  /** Việc đang xem trước và bản xem trước của nó. "Chạy" gửi đúng việc này. */
  const [open, setOpen] = useState<{ action: Action; preview: Preview } | null>(null);
  /** Các bản đang mở danh sách site. */
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const previewRef = useRef<HTMLDivElement>(null);

  const dry = useMutation({
    mutationFn: (a: Action) => post<Preview>(`${a.path}?dry_run=1`, a.body),
    onSuccess: (preview, action) => setOpen({ action, preview }),
  });
  const run = useMutation({
    mutationFn: (a: Action) => post<JobStarted>(a.path, a.body),
    onSuccess: (job) => {
      void qc.invalidateQueries({ queryKey: phpKey });
      void qc.invalidateQueries({ queryKey: stackKey });
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      // Ở lại trang đang dùng; thẻ theo dõi ở góc dưới báo tiến độ và tải lại dữ liệu khi job xong.
      track(job.job_id);
      setOpen(null);
    },
  });

  // Có bản xem trước thì đưa người dùng tới đó, vì nó nằm dưới bảng.
  useEffect(() => {
    if (open) {
      previewRef.current?.scrollIntoView({ block: "start" });
      previewRef.current?.focus();
    }
  }, [open]);

  function start(a: Action) {
    // Bản xem trước của việc kia không còn đúng nữa: bỏ nó trước khi dựng bản mới.
    setOpen(null);
    run.reset();
    dry.mutate(a);
  }

  function cancel() {
    const id = open?.action.id;
    setOpen(null);
    run.reset();
    dry.reset();
    if (id) document.getElementById(`act-${id}`)?.focus();
  }

  const toggleSites = (version: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (!next.delete(version)) next.add(version);
      return next;
    });

  const busy = dry.isPending || run.isPending;

  return (
    <>
      <TableList
        caption={t.phpTable}
        rows={versions}
        rowKey={(r) => r.version}
        empty={t.phpEmpty}
        columns={[
          {
            header: t.colVersion,
            className: "whitespace-nowrap",
            cell: (r) => (
              <span className="inline-flex flex-wrap items-baseline gap-x-[8px] gap-y-[4px]">
                <span className="font-mono text-[13px] font-semibold">{r.version}</span>
                {r.default && <Tag>{t.defaultTag}</Tag>}
              </span>
            ),
          },
          { header: t.colInstalled, className: "whitespace-nowrap", cell: (r) => (r.installed ? t.installed : <span className="text-muted">{t.notInstalledPhp}</span>) },
          {
            header: t.colSites,
            className: "min-w-[110px]",
            cell: (r) => <SitesCell row={r} open={expanded.has(r.version)} onToggle={() => toggleSites(r.version)} />,
          },
          { header: t.colSource, className: "min-w-[140px]", cell: (r) => sourceName(r.source, names) },
          { header: t.colSupport, className: "min-w-[170px]", cell: (r) => <Support support={r.support} /> },
          {
            header: t.colWp,
            cell: (r) => (list.wp_recommended && r.version === list.wp_recommended ? <Tag title={t.wpRecommendedTitle}>{t.wpRecommended}</Tag> : null),
          },
          {
            header: t.colAction,
            className: "whitespace-nowrap",
            cell: (r) => {
              const canRemove = r.installed && !r.default && (r.sites ?? []).length === 0;
              if (r.installed && !canRemove) return null;
              const action = r.installed ? removeAction(r.version) : installAction(r.version);
              const pending = dry.isPending && dry.variables.id === action.id;
              return (
                <Button
                  id={`act-${action.id}`}
                  aria-label={pending ? undefined : r.installed ? t.removeLabel(r.version) : t.installLabel(r.version)}
                  onClick={() => start(action)}
                  disabled={busy}
                >
                  {pending ? t.previewing : r.installed ? t.remove : t.install}
                </Button>
              );
            },
          },
        ]}
      />

      {dry.isError && (
        <div className="pt-[12px]">
          <ErrorNote error={dry.error} heading={t.previewError(dry.variables.body.version, dry.variables.kind === "install")} checksHeading={t.checksHeading} />
        </div>
      )}

      {versions.length > 0 && <p className="mt-[10px] mb-0 max-w-[720px] text-muted">{t.actionHint} {t.previewHint}</p>}
      {!list.notes_ok && <p className="mt-[6px] mb-0 max-w-[720px] text-muted">{t.notesMissing}</p>}
      {!list.sury_enabled && <p className="mt-[6px] mb-0 max-w-[720px] text-muted">{t.suryOff}</p>}

      {open && (
        <div ref={previewRef} tabIndex={-1} className="outline-offset-0">
          <PlanReview preview={open.preview} onRun={() => run.mutate(open.action)} running={run.isPending} error={run.error} onCancel={cancel} />
        </div>
      )}
    </>
  );
}

/** Số site dùng bản này; bấm để mở danh sách domain, mỗi domain là liên kết tới trang site. */
function SitesCell({ row, open, onToggle }: { row: PHPVersionRow; open: boolean; onToggle: () => void }) {
  const t = useStrings(strings);
  const sites = row.sites ?? [];
  if (sites.length === 0) return <span className="text-muted">0</span>;
  const listId = `st-sites-${row.version.replace(/[^a-zA-Z0-9_-]/g, "-")}`;
  return (
    <>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={listId}
        aria-label={t.sitesToggle(row.version, sites.length, open)}
        onClick={onToggle}
        className="inline-flex min-h-[44px] items-center gap-[6px] bg-transparent p-0 text-ink underline sm:min-h-0"
      >
        <span className="tabular-nums">{sites.length}</span>
        <svg aria-hidden="true" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={`flex-none text-muted ${open ? "rotate-180" : ""}`}>
          <path d="M6 9l6 6 6-6" />
        </svg>
      </button>
      {open && (
        <ul id={listId} aria-label={t.sitesList(row.version)} className="m-0 mt-[6px] list-none p-0">
          {sites.map((d) => (
            <li key={d} className="py-[2px]">
              <Link to={`/sites/${encodeURIComponent(d)}`} className="font-mono text-[13px] text-ink no-underline hover:text-ink hover:underline">
                {d}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}

/** Hạn hỗ trợ: tên trạng thái, rồi các ngày (YYYY-MM-DD, nên viết nguyên văn cho cả hai ngôn ngữ). */
function Support({ support }: { support?: PHPSupport }) {
  const t = useStrings(strings);
  if (!support) return <span className="text-muted">{t.noValue}</span>;
  const label = (t.supportState as Record<string, string>)[support.state] ?? support.state;
  const dates: string[] = [];
  if (support.state === "eol") {
    const ended = support.security_until ?? support.active_until;
    if (ended) dates.push(t.supportEnded(ended));
  } else {
    if (support.state === "stable" && support.active_until) dates.push(t.supportActive(support.active_until));
    if (support.security_until) dates.push(t.supportSecurity(support.security_until));
  }
  return (
    <>
      <span>{label}</span>
      {dates.length > 0 && <span className="block text-[13px] text-muted tabular-nums">{dates.join(" · ")}</span>}
    </>
  );
}
