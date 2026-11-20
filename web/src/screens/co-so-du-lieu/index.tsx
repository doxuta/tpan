// Màn Cơ sở dữ liệu (mục 6.9 và 6.12 của KE-HOACH.md): bảng kẻ mảnh mọi cơ sở
// dữ liệu (GET /api/v1/databases), gồm DB đi kèm site và DB rời (M13).
//
//   DB của site: Đổi mật khẩu, Xuất .sql, Nhập từ bản đã xuất (M9), Đổi charset,
//                Tạm khoá / Mở khoá (admin).
//   DB rời:      Sửa (charset có xem trước, ghi chú lưu ngay), Đổi mật khẩu,
//                Tạm khoá / Mở khoá (admin), Xoá (vào thùng rác).
//
// Mọi việc đi đúng ba chặng như màn Thùng rác (màn mẫu để theo): bấm nút → gọi
// ?dry_run=1 lấy bản xem trước (PlanReview) → bấm chạy → máy chủ trả job_id,
// chuyển tới /jobs/:id. Ba chặng nằm trong hook usePreviewedOp nên cùng lúc chỉ
// mở một việc. Site trong thùng rác chỉ cho xuất: đổi mật khẩu và nhập sẽ ghi vào
// wp-config.php và DB của một site đã ngừng chạy. Site đang tạm khoá chưa nhập
// được (máy chủ từ chối site_suspended: nhập ghi lại pool PHP).

import { useQuery } from "@tanstack/react-query";
import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { errorMessage, get } from "../../api/client";
import { useIdentity } from "../../api/m13-dbs";
import { useOverview } from "../../api/queries";
import type { Backup, DatabaseRow } from "../../api/types";
import { Button } from "../../components/Button";
import { DomainText } from "../../components/DomainText";
import { MonoText } from "../../components/MonoText";
import { usePreviewedOp } from "../../components/PreviewedOp";
import { ScreenHeader } from "../../components/ScreenHeader";
import { SuspendTag } from "../../components/SuspendTag";
import { TableList, type Column } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings, type Lang } from "../../i18n";
import { strings } from "../../i18n/co-so-du-lieu";
import { strings as khung } from "../../i18n/khung";
import { strings as chung } from "../../i18n/m13-chung";
import { backupsKey } from "../backup/model";
import { formatWhen, trashKey } from "../thung-rac/model";
import { CreateDialog } from "./CreateDialog";
import { EditPanel } from "./EditPanel";
import { ImportPicker } from "./ImportPicker";
import { databasesKey, databasesPath, rowOp, type RowAction } from "./model";

/** Byte thành KB, MB hoặc GB, một số lẻ, dấu thập phân theo ngôn ngữ. */
function formatSize(bytes: number, lang: Lang): string {
  const num = new Intl.NumberFormat(lang === "vi" ? "vi-VN" : "en-GB", { maximumFractionDigits: 1 });
  if (bytes < 1024 * 1024) return `${num.format(bytes / 1024)} KB`;
  if (bytes < 1024 ** 3) return `${num.format(bytes / 1024 ** 2)} MB`;
  return `${num.format(bytes / 1024 ** 3)} GB`;
}

/**
 * Độ rộng tối thiểu cho chữ mono dài (tên DB, domain). min-width đặt lên ô <td>
 * không được trình duyệt tôn trọng, và MonoText cho ngắt giữa chữ, nên không có
 * khối này thì cột bị ép hẹp và tên bị cắt ra nhiều dòng. Hẹp quá thì cả bảng
 * cuộn ngang trong khung riêng của TableList.
 */
function Min({ w, children }: { w: string; children: ReactNode }) {
  return <span className={`block ${w}`}>{children}</span>;
}

export default function CoSoDuLieu() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const m = useStrings(chung);
  const { lang } = useLang();
  const { isAdmin } = useIdentity();
  const q = useQuery({ queryKey: databasesKey, queryFn: () => get<DatabaseRow[]>(databasesPath) });
  // Cùng khoá overview với khung trang: không gọi thêm API. Cho biết site nào đang ở thùng rác hay tạm khoá.
  const overview = useOverview();
  const siteState = useMemo(() => {
    const trashed = new Set<string>();
    const suspended = new Set<string>();
    for (const r of overview.data?.sites ?? []) {
      if (r.site.status === "trashed") trashed.add(r.site.domain);
      if (r.site.suspended) suspended.add(r.site.domain);
    }
    return { trashed, suspended };
  }, [overview.data]);

  // Dung lượng, danh sách bản xuất, thùng rác và số đếm ở menu đều đổi sau job.
  const op = usePreviewedOp([databasesKey, backupsKey, trashKey]);
  const [creating, setCreating] = useState(false);
  /** Khối đang mở dưới bảng: chọn bản nhập (DB của site) hoặc Sửa, Đổi charset. Theo tên DB để dữ liệu luôn mới. */
  const [view, setView] = useState<{ kind: "import" | "edit"; db: string } | null>(null);

  const rows = q.data ?? [];
  const viewRow = view ? rows.find((r) => r.db === view.db) : undefined;

  function act(action: RowAction, row: DatabaseRow, extra?: { backup?: Backup }) {
    if (action !== "import") setView(null);
    void op.ask(rowOp(t, action, row, { ...extra, when: extra?.backup ? formatWhen(extra.backup.created, lang) : undefined }));
  }
  function open(kind: "import" | "edit", row: DatabaseRow) {
    op.close();
    setView({ kind, db: row.db });
  }
  function closeAll() {
    op.close();
    setView(null);
  }

  const btn = (action: RowAction, row: DatabaseRow, label: string, aria: string) => (
    <Button aria-label={aria} disabled={op.pending} onClick={() => act(action, row)}>
      {op.busy(`${action}:${row.db}`) ? m.previewing : label}
    </Button>
  );

  /** Việc của một hàng. Ba việc đầu của DB site là của M9 và giữ nguyên. */
  function actions(r: DatabaseRow): ReactNode {
    const site = r.kind === "site";
    const trashed = r.status === "trashed" || (site && siteState.trashed.has(r.domain));
    const siteSuspended = site && siteState.suspended.has(r.domain);
    const lock = isAdmin ? btn(r.suspended ? "unsuspend" : "suspend", r, r.suspended ? t.unsuspend : t.suspend, r.suspended ? t.unsuspendFor(r.db) : t.suspendFor(r.db)) : null;

    if (!site) {
      if (trashed) {
        return (
          <span className="text-[13px] text-muted">
            {t.inTrashDb} <Link to="/trash">{t.trashedLink}</Link>
          </span>
        );
      }
      return (
        <span className="flex min-w-[150px] flex-wrap items-center gap-[8px]">
          <Button aria-label={t.editFor(r.db)} disabled={op.pending} onClick={() => open("edit", r)}>
            {t.edit}
          </Button>
          {btn("password", r, t.password, t.passwordForDb(r.db))}
          {lock}
          {btn("trash", r, t.trash, t.trashFor(r.db))}
        </span>
      );
    }
    return (
      <span className="flex min-w-[150px] flex-wrap items-center gap-[8px]">
        {!trashed && btn("password", r, t.password, t.passwordFor(r.domain))}
        {btn("dump", r, t.dump, t.dumpFor(r.domain))}
        {!trashed && (
          <Button aria-label={t.importFor(r.domain)} disabled={op.pending || siteSuspended} onClick={() => open("import", r)}>
            {t.importOpen}
          </Button>
        )}
        {!trashed && (
          <Button aria-label={t.charsetFor(r.domain)} disabled={op.pending} onClick={() => open("edit", r)}>
            {t.charset}
          </Button>
        )}
        {!trashed && lock}
        {trashed && (
          <span className="text-[13px] text-muted">
            {t.trashedLead}
            <Link to="/trash">{t.trashedLink}</Link>
            {t.trashedTail}
          </span>
        )}
        {siteSuspended && !trashed && <span className="basis-full text-[13px] text-muted">{t.importBlocked}</span>}
      </span>
    );
  }

  const columns: Column<DatabaseRow>[] = [
    {
      // DB và user chung một cột (thường cùng tên): bảng bớt một cột rộng, vừa khung khi cửa sổ hẹp.
      header: t.colDbUser,
      cell: (r) => (
        <Min w="min-w-[216px]">
          <span className="flex flex-wrap items-baseline gap-x-[10px] gap-y-[4px]">
            <MonoText>{r.db}</MonoText>
            {r.suspended && <SuspendTag how={r.suspended} />}
          </span>
          {r.user === r.db ? (
            <span className="block text-[12px] text-muted">{t.sameUser}</span>
          ) : (
            <span className="block">
              <MonoText>{r.user}</MonoText>
            </span>
          )}
          {r.note && <span className="mt-[2px] block max-w-[320px] text-[13px] text-muted [overflow-wrap:anywhere]">{r.note}</span>}
        </Min>
      ),
    },
    {
      header: t.colType,
      cell: (r) =>
        r.kind === "site" ? (
          <Min w="min-w-[130px]">
            <span className="block text-[13px]">{t.kindSite}</span>
            <Link to={`/sites/${encodeURIComponent(r.domain)}`} className="font-mono text-[13px]">
              <DomainText domain={r.domain} />
            </Link>
          </Min>
        ) : (
          <Tag>{t.kindStandalone}</Tag>
        ),
    },
    ...(isAdmin ? [{ header: m.colOwner, className: "font-mono text-[13px]", cell: (r: DatabaseRow) => r.owner } satisfies Column<DatabaseRow>] : []),
    {
      header: t.colCharset,
      cell: (r) =>
        r.charset ? (
          <Min w="min-w-[130px]">
            <span className="block font-mono text-[13px]">{r.charset}</span>
            <span className="block font-mono text-[12px] text-muted">{r.collation}</span>
          </Min>
        ) : (
          <span className="text-muted">{t.none}</span>
        ),
    },
    {
      header: t.colSize,
      className: "text-right whitespace-nowrap tabular-nums",
      cell: (r) => (r.size_bytes < 0 ? <span className="text-muted">{t.unreadable}</span> : formatSize(r.size_bytes, lang)),
    },
    {
      header: t.colTables,
      className: "text-right whitespace-nowrap tabular-nums",
      cell: (r) => (r.tables < 0 ? <span className="text-muted">{t.unreadable}</span> : r.tables),
    },
    { header: t.colAction, className: "pr-0", cell: actions },
  ];

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />
      <div className="pt-[20px]">
        {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
        {q.isError && (
          <div>
            <p className="m-0">{errorMessage(q.error, lang)}</p>
            <Button className="mt-[12px]" onClick={() => void q.refetch()}>
              {k.retry}
            </Button>
          </div>
        )}
        {q.isSuccess && (
          <>
            <div className="flex flex-wrap items-end justify-between gap-x-[24px] gap-y-[10px] pb-[14px]">
              <h2 className="m-0 text-[16px] font-semibold">{t.listHeading(rows.length)}</h2>
              <Button variant="primary" onClick={() => setCreating(true)}>
                {t.createButton}
              </Button>
            </div>
            <TableList
              caption={t.tableName}
              rows={rows}
              rowKey={(r) => r.db}
              empty={
                <>
                  {t.emptyText} <Link to="/sites/new">{t.emptyLink}</Link>
                </>
              }
              columns={columns}
            />
            <p className="mt-[16px] mb-0 max-w-[720px] text-[13px] text-muted">{t.hint}</p>
            <p className="mt-[8px] mb-0 max-w-[720px] text-[13px] text-muted">{t.hintM13}</p>

            {view?.kind === "import" && viewRow?.kind === "site" && !op.hasPreview && (
              <div tabIndex={-1} className="outline-offset-0">
                <ImportPicker
                  key={viewRow.domain}
                  domain={viewRow.domain}
                  pending={op.busy(`import:${viewRow.db}`)}
                  anyPending={op.pending}
                  onPreview={(backup) => act("import", viewRow, { backup })}
                  onCancel={closeAll}
                />
              </div>
            )}

            {view?.kind === "edit" && viewRow && !op.hasPreview && <EditPanel key={viewRow.db} row={viewRow} op={op} onClose={closeAll} />}

            {op.panel}
          </>
        )}
      </div>

      <CreateDialog open={creating} onOpenChange={setCreating} op={op} />
    </>
  );
}
