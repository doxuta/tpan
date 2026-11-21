// Bảng các bản PHP của màn Stack (GET /api/v1/php): bản nào đã cài, bản nào là
// mặc định, site nào đang dùng, nguồn, hạn hỗ trợ, bản WordPress khuyên dùng.
// Thao tác: bản chưa cài có nút "Cài"; bản đã cài, không site nào dùng và không
// phải mặc định có nút "Gỡ". Cùng nhịp với màn Cập nhật: bấm → POST ?dry_run=1
// → PlanReview → POST thật → /jobs/:id. Một bản xem trước tại một thời điểm,
// nằm dưới bảng. Máy chủ từ chối (409 kèm checks, ví dụ php_in_use) thì ErrorNote
// hiện câu của nó ngay dưới bảng.
// Bản 2: bảng trong thẻ (TableList), các ghi chú nằm ở chân bảng.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { useJobTracker } from "../../components/JobTracker";
import { errorMessage, get, post } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { JobStarted, PHPList, PHPSupport, PHPVersionRow, Preview } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Icon } from "../../components/Icon";
import { Code } from "../../components/MonoText";
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

  if (q.isSuccess) return <Loaded list={q.data} />;
  return (
    <Card title={t.phpHeading} titleId="st-php">
      {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div>
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </div>
      )}
    </Card>
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

  // Ghi chú ở chân bảng: cách dùng hai nút, rồi hai lời báo khi thiếu dữ liệu ngoài.
  const notes = [
    versions.length > 0 ? `${t.actionHint} ${t.previewHint}` : null,
    list.notes_ok ? null : t.notesMissing,
    list.sury_enabled ? null : t.suryOff,
  ].filter((n): n is string => n !== null);

  return (
    <>
      <TableList
        title={t.phpHeading}
        titleId="st-php"
        caption={t.phpTable}
        minWidth={860}
        rows={versions}
        rowKey={(r) => r.version}
        empty={t.phpEmpty}
        footer={
          notes.length > 0 ? (
            <div className="flex max-w-[72ch] flex-col gap-[4px]">
              {notes.map((n) => (
                <span key={n}>{n}</span>
              ))}
            </div>
          ) : undefined
        }
        columns={[
          {
            header: t.colVersion,
            cell: (r) => (
              <span className="inline-flex flex-wrap items-baseline gap-x-[8px] gap-y-[4px]">
                <span className="font-semibold">
                  <Code>{r.version}</Code>
                </span>
                {r.default && <Tag>{t.defaultTag}</Tag>}
              </span>
            ),
          },
          { header: t.colInstalled, cell: (r) => (r.installed ? t.installed : <span className="text-muted">{t.notInstalledPhp}</span>) },
          {
            header: t.colSites,
            nowrap: false,
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
            align: "right",
            cell: (r) => {
              const canRemove = r.installed && !r.default && (r.sites ?? []).length === 0;
              if (r.installed && !canRemove) return null;
              const action = r.installed ? removeAction(r.version) : installAction(r.version);
              const pending = dry.isPending && dry.variables.id === action.id;
              return (
                <div className="flex justify-end">
                  <Button
                    id={`act-${action.id}`}
                    variant="text"
                    aria-label={pending ? undefined : r.installed ? t.removeLabel(r.version) : t.installLabel(r.version)}
                    onClick={() => start(action)}
                    disabled={busy}
                    className="px-[12px]"
                  >
                    {pending ? t.previewing : r.installed ? t.remove : t.install}
                  </Button>
                </div>
              );
            },
          },
        ]}
      />

      {dry.isError && (
        <Card>
          <ErrorNote error={dry.error} heading={t.previewError(dry.variables.body.version, dry.variables.kind === "install")} checksHeading={t.checksHeading} />
        </Card>
      )}

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
      <Button
        variant="text"
        size="sm"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        aria-label={t.sitesToggle(row.version, sites.length, open)}
        onClick={onToggle}
        className="px-[8px]"
      >
        <span className="tabular-nums">{sites.length}</span>
        <Icon name="chevron-down" size={16} className={`transition-transform ${open ? "rotate-180" : ""}`} />
      </Button>
      {open && (
        <ul id={listId} aria-label={t.sitesList(row.version)} className="m-0 mt-[6px] list-none p-0">
          {sites.map((d) => (
            <li key={d} className="py-[2px] [overflow-wrap:anywhere]">
              <Link to={`/sites/${encodeURIComponent(d)}`} className="no-underline hover:underline">
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
      {dates.length > 0 && <span className="block text-[12.5px] text-muted tabular-nums">{dates.join(" · ")}</span>}
    </>
  );
}
