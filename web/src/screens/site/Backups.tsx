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
//
// M18: cùng hành vi kho ngoài máy như màn Backup, dùng chung mã của nó (cột "Ở
// đâu" và luật chỉ hiện khi cần ở backup/model.ts; hộp hỏi tải về trước khi
// khôi phục một bản chỉ còn trên kho ở backup/StorageFlow.tsx), để người dùng
// không gặp lỗi 409 backup_not_on_machine trơ trọi. Tab này không có nút "Đẩy
// lên kho": việc đó làm ở màn Backup.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { useJobTracker } from "../../components/JobTracker";
import { errorMessage, get, post } from "../../api/client";
import { pullPath } from "../../api/m18-types";
import { queryKeys } from "../../api/queries";
import type { Backup, BackupsOverview, Preview } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Code } from "../../components/MonoText";
import { RowActions } from "../../components/RowActions";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings as backupStrings } from "../../i18n/backup";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/site";
import { Where } from "../backup/List";
import { backupsKey, backupsPath, formatSize, restorePath, shortSha, showWhere, siteBackupPath } from "../backup/model";
import { useStorageFlow, type Started } from "../backup/StorageFlow";
import { PlanReview } from "../duyet-plan";
import { formatWhen } from "../thung-rac/model";
import { ErrorNote } from "./ErrorNote";

/** Số bản hiện trên trang site; muốn xem hết thì sang màn Backup. */
const shownCount = 5;

/** pull: tải về máy, chỉ đi tới từ hộp hỏi khi khôi phục một bản chỉ còn trên kho (fromRestore). */
type Req = { action: "create" } | { action: "restore"; backup: Backup } | { action: "pull"; backup: Backup; fromRestore?: boolean };

/** restoreBlocked (M13): lý do không khôi phục được (site đang tạm khoá); có thì các nút Khôi phục vô hiệu. */
export function Backups({ domain, active, restoreBlocked }: { domain: string; active: boolean; restoreBlocked?: string }) {
  const t = useStrings(strings);
  const b = useStrings(backupStrings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const qc = useQueryClient();
  const { track } = useJobTracker();
  const flow = useStorageFlow((backup) => ask({ action: "pull", backup, fromRestore: true }));
  // Khoá bắt đầu bằng backupsKey: màn invalidate backupsKey sau job thì khoá này cũng được tính lại.
  const q = useQuery({ queryKey: [...backupsKey, domain], queryFn: () => get<BackupsOverview>(`${backupsPath}?domain=${encodeURIComponent(domain)}`), refetchInterval: flow.poll });

  const [open, setOpen] = useState<{ req: Req; preview: Preview } | null>(null);
  /** Việc vừa bấm: để chữ nút và lỗi nói đúng việc. */
  const [asked, setAsked] = useState<Req | null>(null);
  const resultRef = useRef<HTMLDivElement>(null);

  const pathOf = (r: Req) => (r.action === "create" ? siteBackupPath(domain) : r.action === "pull" ? pullPath(r.backup.id) : restorePath(r.backup.id));
  const dry = useMutation({
    mutationFn: (req: Req) => post<Preview>(`${pathOf(req)}?dry_run=1`),
    onSuccess: (preview, req) => setOpen({ req, preview }),
  });
  const run = useMutation({
    mutationFn: (req: Req) => post<Started>(pathOf(req)),
    onSuccess: (res, req) => {
      void qc.invalidateQueries({ queryKey: backupsKey });
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      // Ở lại trang đang dùng; thẻ theo dõi ở góc dưới báo tiến độ và tải lại dữ liệu khi job xong.
      if (res.job_id) track(res.job_id);
      setOpen(null);
      if (req.action === "pull") flow.ran(req, res);
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
    flow.clear();
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
  // Tính trên cả danh sách của site chứ không chỉ năm hàng đang hiện.
  const where = showWhere(items, flow.configured);
  const errorHeading = (r: Req): string =>
    r.action === "create" ? t.bkCreateErrorHeading : r.action === "pull" ? b.pullErrorHeading(domain) : t.bkRestoreErrorHeading(formatWhen(r.backup.created, lang));
  /** "Khôi phục" một hàng: bản chỉ còn trên kho thì flow lo (hộp hỏi tải về, hoặc nhắn nhờ admin), không gọi API khôi phục. */
  function restore(x: Backup) {
    if (flow.restore(x)) close();
    else ask({ action: "restore", backup: x });
  }

  const createButton = active && (
    <Button variant="primary" disabled={dry.isPending} onClick={() => ask({ action: "create" })}>
      {pending?.action === "create" ? t.bkPreviewing : t.bkNow}
    </Button>
  );

  return (
    <>
      {q.isSuccess ? (
        <TableList
          title={t.bkHeading}
          titleId="backup-site"
          actions={createButton}
          caption={t.bkTableName(domain)}
          rows={items.slice(0, shownCount)}
          rowKey={(x) => x.id}
          empty={active ? t.bkEmpty : t.bkEmptyTrashed}
          minWidth={where ? 840 : 720}
          // Lời dẫn nằm giữa đầu thẻ và bảng; câu "vì sao chưa khôi phục được" đứng ngay dưới, nút Khôi phục trỏ tới nó.
          above={
            <div className="flex flex-col gap-[4px] px-[16px] py-[10px] text-[13px] text-muted">
              <p className="m-0 max-w-[72ch]">{active ? t.bkHint : t.bkTrashed}</p>
              {restoreBlocked && (
                <p id="khoi-phuc-bi-chan" className="m-0 max-w-[72ch]">
                  {restoreBlocked}
                </p>
              )}
            </div>
          }
          footer={<Link to="/backups">{t.bkViewAll(items.length)}</Link>}
          columns={[
            { header: t.bkColCreated, className: "tabular-nums", cell: (x) => formatWhen(x.created, lang) },
            { header: t.bkColKind, cell: (x) => <Tag title={x.kind === "full" ? b.kindFull : b.kindSql}>{x.kind}</Tag> },
            { header: t.bkColReason, cell: (x) => b.reasons[x.reason] ?? x.reason },
            { header: t.bkColSize, align: "right", cell: (x) => formatSize(x.size, lang) },
            ...(where ? [{ header: b.colWhere, cell: (x: Backup) => <Where b={x} /> }] : []),
            { header: t.bkColSha, cell: (x) => <Code>{shortSha(x.sha256)}</Code> },
            {
              header: t.bkColAction,
              align: "right",
              nowrap: false,
              cell: (x) => {
                if (x.kind === "sql") {
                  return (
                    <span className="text-[12.5px] text-muted">
                      {t.bkSqlLead}
                      <Link to="/databases">{t.bkSqlLink}</Link>
                      {t.bkSqlTail}
                    </span>
                  );
                }
                if (!active) return null;
                const label = t.bkRestoreFor(formatWhen(x.created, lang));
                return (
                  <RowActions
                    moreLabel={label}
                    primary={[
                      {
                        label: t.bkRestore,
                        ariaLabel: label,
                        title: restoreBlocked,
                        disabled: dry.isPending || restoreBlocked !== undefined,
                        busy: pending?.action === "restore" && pending.backup.id === x.id,
                        busyLabel: t.bkPreviewing,
                        onClick: () => restore(x),
                      },
                    ]}
                  />
                );
              },
            },
          ]}
        />
      ) : (
        <Card title={t.bkHeading} titleId="backup-site" actions={createButton}>
          {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
          {q.isError && (
            <div>
              <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
              <Button onClick={() => void q.refetch()}>{k.retry}</Button>
            </div>
          )}
        </Card>
      )}

      {flow.render(q.data?.items)}

      {dry.isError && asked && (
        <div ref={resultRef} tabIndex={-1} className="outline-offset-0">
          <ErrorNote error={dry.error} heading={errorHeading(asked)} checksHeading={t.bkChecksHeading} />
          <div className="pt-[12px]">
            <Button onClick={close}>{k.cancel}</Button>
          </div>
        </div>
      )}

      {open && (
        <div ref={resultRef} tabIndex={-1} className="flex flex-col gap-[20px] outline-offset-0">
          {open.req.action === "restore" && <p className="m-0 max-w-[72ch] font-semibold">{t.bkRestoreWarning(domain, formatWhen(open.req.backup.created, lang))}</p>}
          {open.req.action === "pull" && <p className="m-0 max-w-[72ch] font-semibold">{b.pullWarning(domain, formatWhen(open.req.backup.created, lang))}</p>}
          <PlanReview preview={open.preview} onRun={() => run.mutate(open.req)} running={run.isPending} error={run.error} onCancel={close} />
        </div>
      )}
    </>
  );
}
