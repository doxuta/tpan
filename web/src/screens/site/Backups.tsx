// Khối Backup trên trang một site (mục 3 của mục 10 trong KE-HOACH.md: "bên
// dưới là bằng chứng, log trực tiếp, backup, thao tác"): vài bản gần nhất của
// đúng site này, "Backup ngay", "Khôi phục" một bản full, và liên kết sang màn
// Backup để xem hết. Hai việc đi đúng ba chặng như màn Backup (màn mẫu để
// theo): bấm nút → gọi ?dry_run=1 lấy bản xem trước (PlanReview) → bấm chạy →
// máy chủ trả job_id, chuyển tới /jobs/:id. Khối giữ trạng thái riêng nên
// trang site chỉ gắn một dòng.
//
// Site trong thùng rác chỉ được xem danh sách: ghi đè file và DB của một site
// đã ngừng chạy là việc phải làm sau khi khôi phục nó khỏi thùng rác (cùng lý do
// với màn Backup).

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { errorMessage, get, post } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { Backup, BackupsOverview, JobStarted, Preview } from "../../api/types";
import { Button } from "../../components/Button";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings as backupStrings } from "../../i18n/backup";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/site";
import { backupsKey, backupsPath, formatSize, restorePath, shortSha, siteBackupPath } from "../backup/model";
import { PlanReview } from "../duyet-plan";
import { formatWhen } from "../thung-rac/model";
import { ErrorNote } from "./ErrorNote";

/** Số bản hiện trên trang site; muốn xem hết thì sang màn Backup. */
const shownCount = 5;

type Req = { action: "create" } | { action: "restore"; backup: Backup };

/** restoreBlocked (M13): lý do không khôi phục được (site đang tạm khoá); có thì các nút Khôi phục vô hiệu. */
export function Backups({ domain, active, restoreBlocked }: { domain: string; active: boolean; restoreBlocked?: string }) {
  const t = useStrings(strings);
  const b = useStrings(backupStrings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const qc = useQueryClient();
  const navigate = useNavigate();
  // Khoá bắt đầu bằng backupsKey: màn invalidate backupsKey sau job thì khoá này cũng được tính lại.
  const q = useQuery({ queryKey: [...backupsKey, domain], queryFn: () => get<BackupsOverview>(`${backupsPath}?domain=${encodeURIComponent(domain)}`) });

  const [open, setOpen] = useState<{ req: Req; preview: Preview } | null>(null);
  /** Việc vừa bấm: để chữ nút và lỗi nói đúng việc. */
  const [asked, setAsked] = useState<Req | null>(null);
  const resultRef = useRef<HTMLDivElement>(null);

  const pathOf = (r: Req) => (r.action === "create" ? siteBackupPath(domain) : restorePath(r.backup.id));
  const dry = useMutation({
    mutationFn: (req: Req) => post<Preview>(`${pathOf(req)}?dry_run=1`),
    onSuccess: (preview, req) => setOpen({ req, preview }),
  });
  const run = useMutation({
    mutationFn: (req: Req) => post<JobStarted>(pathOf(req)),
    onSuccess: (job) => {
      void qc.invalidateQueries({ queryKey: backupsKey });
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      void navigate(`/jobs/${encodeURIComponent(job.job_id)}`);
    },
  });

  // Có bản xem trước hoặc lỗi mới thì đưa người dùng tới đó, vì nó nằm dưới bảng.
  const shown = open ?? (dry.isError ? asked : null);
  useEffect(() => {
    if (shown) {
      resultRef.current?.scrollIntoView({ block: "start" });
      resultRef.current?.focus();
    }
  }, [shown]);

  function ask(req: Req) {
    setOpen(null);
    run.reset();
    setAsked(req);
    dry.mutate(req);
  }

  function close() {
    setOpen(null);
    setAsked(null);
    dry.reset();
    run.reset();
  }

  const pending = dry.isPending ? asked : null;
  const items = q.data?.items ?? [];
  const errorHeading = (r: Req): string => (r.action === "create" ? t.bkCreateErrorHeading : t.bkRestoreErrorHeading(formatWhen(r.backup.created, lang)));

  return (
    <section aria-labelledby="backup-site">
      <div className="flex flex-wrap items-end justify-between gap-x-[24px] gap-y-[10px] pt-[26px] pb-[6px]">
        <h2 id="backup-site" className="m-0 text-[16px] font-semibold">
          {t.bkHeading}
        </h2>
        {active && (
          <Button variant="primary" disabled={dry.isPending} onClick={() => ask({ action: "create" })}>
            {pending?.action === "create" ? t.bkPreviewing : t.bkNow}
          </Button>
        )}
      </div>
      <p className="mt-0 mb-[12px] max-w-[720px] text-[13px] text-muted">{active ? t.bkHint : t.bkTrashed}</p>
      {restoreBlocked && (
        <p id="khoi-phuc-bi-chan" className="mt-0 mb-[12px] max-w-[720px] text-[13px] text-muted">
          {restoreBlocked}
        </p>
      )}

      {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div>
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </div>
      )}
      {q.isSuccess && (
        <>
          <TableList
            caption={t.bkTableName(domain)}
            rows={items.slice(0, shownCount)}
            rowKey={(x) => x.id}
            empty={active ? t.bkEmpty : t.bkEmptyTrashed}
            columns={[
              { header: t.bkColCreated, cell: (x) => <span className="font-mono text-[12px] whitespace-nowrap text-muted">{formatWhen(x.created, lang)}</span> },
              { header: t.bkColKind, cell: (x) => <Tag title={x.kind === "full" ? b.kindFull : b.kindSql}>{x.kind}</Tag> },
              { header: t.bkColReason, cell: (x) => b.reasons[x.reason] ?? x.reason },
              { header: t.bkColSize, cell: (x) => <span className="font-mono text-[12px] whitespace-nowrap">{formatSize(x.size, lang)}</span> },
              { header: t.bkColSha, cell: (x) => <span className="font-mono text-[12px] text-muted">{shortSha(x.sha256)}</span> },
              {
                header: t.bkColAction,
                className: "pr-0",
                cell: (x) => {
                  if (x.kind === "sql") {
                    return (
                      <span className="text-[13px] text-muted">
                        {t.bkSqlLead}
                        <Link to="/databases">{t.bkSqlLink}</Link>
                        {t.bkSqlTail}
                      </span>
                    );
                  }
                  if (!active) return null;
                  return (
                    <Button
                      aria-label={t.bkRestoreFor(formatWhen(x.created, lang))}
                      aria-describedby={restoreBlocked ? "khoi-phuc-bi-chan" : undefined}
                      disabled={dry.isPending || restoreBlocked !== undefined}
                      onClick={() => ask({ action: "restore", backup: x })}>
                      {pending?.action === "restore" && pending.backup.id === x.id ? t.bkPreviewing : t.bkRestore}
                    </Button>
                  );
                },
              },
            ]}
          />
          <p className="mt-[12px] mb-0 text-[13px]">
            <Link to="/backups">{t.bkViewAll(items.length)}</Link>
          </p>
        </>
      )}

      {dry.isError && asked && (
        <div ref={resultRef} tabIndex={-1} className="pt-[20px] outline-offset-0">
          <ErrorNote error={dry.error} heading={errorHeading(asked)} checksHeading={t.bkChecksHeading} />
          <div className="pt-[12px]">
            <Button onClick={close}>{k.cancel}</Button>
          </div>
        </div>
      )}

      {open && (
        <div ref={resultRef} tabIndex={-1} className="outline-offset-0">
          {open.req.action === "restore" && <p className="m-0 max-w-[720px] pt-[26px] font-semibold">{t.bkRestoreWarning(domain, formatWhen(open.req.backup.created, lang))}</p>}
          <PlanReview preview={open.preview} onRun={() => run.mutate(open.req)} running={run.isPending} error={run.error} onCancel={close} />
        </div>
      )}
    </section>
  );
}
