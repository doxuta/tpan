// Màn Cơ sở dữ liệu (mục 6.9 và 6.12 của KE-HOACH.md; giao diện bản 2, mục 3.7
// của M16-thiet-ke.md): một bảng mọi cơ sở dữ liệu (GET /api/v1/databases), gồm
// DB đi kèm site và DB rời (M13).
//
//   DB của site: Đổi mật khẩu, Xuất .sql; menu ba chấm: Nhập từ bản đã xuất (M9),
//                Đổi charset, Tạm khoá / Mở khoá (admin).
//   DB rời:      Sửa (charset có xem trước, ghi chú lưu ngay), Đổi mật khẩu; menu
//                ba chấm: Tạm khoá / Mở khoá (admin), Đưa vào thùng rác.
//
// Mọi việc đi đúng ba chặng như màn Thùng rác (màn mẫu để theo): bấm nút → gọi
// ?dry_run=1 lấy bản xem trước (PlanReview) → bấm chạy → máy chủ trả job_id, ở
// lại màn, thẻ theo dõi job báo tiến độ. Ba chặng nằm trong hook usePreviewedOp
// nên cùng lúc chỉ mở một việc. Site trong thùng rác chỉ cho xuất: đổi mật khẩu
// và nhập sẽ ghi vào wp-config.php và DB của một site đã ngừng chạy. Site đang
// tạm khoá chưa nhập được (máy chủ từ chối site_suspended: nhập ghi lại pool PHP).
//
// Màn này là màn mẫu của bản 2 (cùng màn Người dùng): ScreenHeader → một
// TableList (tự vẽ thẻ, chân bảng) → các khối kết quả dưới bảng.

import { useQuery } from "@tanstack/react-query";
import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { errorMessage, get } from "../../api/client";
import { useIdentity } from "../../api/m13-dbs";
import { useOverview } from "../../api/queries";
import type { Backup, DatabaseRow } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { MonoText } from "../../components/MonoText";
import { usePreviewedOp } from "../../components/PreviewedOp";
import { RowActions, type MenuAction, type RowAction as RowButton } from "../../components/RowActions";
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
import { footerCounts, footerText } from "./footer";
import { ImportPicker } from "./ImportPicker";
import { databasesKey, databasesPath, rowOp, type RowAction } from "./model";

/** Byte thành KB, MB hoặc GB, một số lẻ, dấu thập phân theo ngôn ngữ. */
function formatSize(bytes: number, lang: Lang): string {
  const num = new Intl.NumberFormat(lang === "vi" ? "vi-VN" : "en-GB", { maximumFractionDigits: 1 });
  if (bytes < 1024 * 1024) return `${num.format(bytes / 1024)} KB`;
  if (bytes < 1024 ** 3) return `${num.format(bytes / 1024 ** 2)} MB`;
  return `${num.format(bytes / 1024 ** 3)} GB`;
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

  /** Việc của một hàng. Điều kiện của từng việc giữ đúng bản 1; ba việc đầu của DB site là của M9. */
  function actions(r: DatabaseRow): ReactNode {
    const site = r.kind === "site";
    const trashed = r.status === "trashed" || (site && siteState.trashed.has(r.domain));
    const siteSuspended = site && siteState.suspended.has(r.domain);

    const button = (action: RowAction, label: string, ariaLabel: string): RowButton => ({ label, ariaLabel, disabled: op.pending, onClick: () => act(action, r) });
    const lock: (MenuAction & { label: string })[] = isAdmin ? [{ label: r.suspended ? t.unsuspend : t.suspend, disabled: op.pending, onSelect: () => act(r.suspended ? "unsuspend" : "suspend", r) }] : [];
    const row = (primary: RowButton[], more: (MenuAction & { label: string })[] = []) => (
      <RowActions primary={primary} more={more} moreLabel={t.moreFor(r.db, more.map((x) => x.label.toLowerCase()).join(", "))} />
    );

    if (!site) {
      if (trashed) {
        return (
          <span className="text-[12.5px] text-muted">
            {t.inTrashDb} <Link to="/trash">{t.openTrash}</Link>
          </span>
        );
      }
      return row(
        [{ label: t.edit, ariaLabel: t.editFor(r.db), disabled: op.pending, onClick: () => open("edit", r) }, button("password", t.password, t.passwordForDb(r.db))],
        [...lock, { label: t.trash, disabled: op.pending, onSelect: () => act("trash", r) }],
      );
    }
    if (trashed) {
      return (
        <div className="flex flex-col items-end gap-[2px]">
          {row([button("dump", t.dump, t.dumpFor(r.domain))])}
          <span className="max-w-[260px] text-[12.5px] text-muted">
            {t.trashedLead}
            <Link to="/trash">{t.trashedLink}</Link>
            {t.trashedTail}
          </span>
        </div>
      );
    }
    return row(
      [button("password", t.password, t.passwordFor(r.domain)), button("dump", t.dump, t.dumpFor(r.domain))],
      [
        { label: t.importOpen, disabled: op.pending || siteSuspended, hint: siteSuspended ? t.importBlocked : undefined, onSelect: () => open("import", r) },
        { label: t.charset, disabled: op.pending, onSelect: () => open("edit", r) },
        ...lock,
      ],
    );
  }

  const columns: Column<DatabaseRow>[] = [
    {
      // DB và user chung một cột (thường cùng tên): bảng bớt một cột rộng, vừa khung khi cửa sổ hẹp.
      header: t.colDbUser,
      nowrap: false,
      cell: (r) => (
        <div className="flex min-w-[220px] flex-col gap-[2px]">
          <span className="flex flex-wrap items-center gap-x-[10px]">
            <MonoText>{r.db}</MonoText>
            {r.suspended && <SuspendTag how={r.suspended} />}
          </span>
          {r.user === r.db ? <span className="text-[12.5px] text-muted">{t.sameUser}</span> : <MonoText size={12.5} muted>{r.user}</MonoText>}
          {r.note && <span className="max-w-[320px] text-[12.5px] text-muted [overflow-wrap:anywhere]">{r.note}</span>}
        </div>
      ),
    },
    {
      header: t.colOfSite,
      cell: (r) =>
        r.kind === "site" ? (
          // Tên miền dài (44 ký tự trở lên) nằm một dòng, cắt bằng dấu ba chấm; đủ chữ trong title.
          <Link to={`/sites/${encodeURIComponent(r.domain)}`} title={r.domain} className="inline-block max-w-[320px] truncate align-bottom no-underline hover:underline">
            {r.domain}
          </Link>
        ) : (
          <Tag>{t.kindStandalone}</Tag>
        ),
    },
    ...(isAdmin ? [{ header: m.colOwner, cell: (r: DatabaseRow) => r.owner } satisfies Column<DatabaseRow>] : []),
    {
      header: t.colCharset,
      cell: (r) => (r.charset ? <span title={r.collation}>{r.charset}</span> : <span className="text-muted">{t.none}</span>),
    },
    {
      header: t.colSize,
      align: "right",
      cell: (r) => (r.size_bytes < 0 ? <Unreadable label={t.unreadable} /> : formatSize(r.size_bytes, lang)),
    },
    {
      header: t.colTables,
      align: "right",
      cell: (r) => (r.tables < 0 ? <Unreadable label={t.unreadable} /> : r.tables),
    },
    { header: t.colAction, align: "right", nowrap: false, cell: actions },
  ];

  return (
    <>
      <ScreenHeader
        title={t.title}
        summary={t.summary}
        actions={
          <Button variant="primary" icon="plus" onClick={() => setCreating(true)}>
            {t.createButton}
          </Button>
        }
      />

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
      {q.isSuccess && (
        <>
          <TableList
            label={t.tableName}
            caption={t.tableName}
            minWidth={960}
            rows={rows}
            rowKey={(r) => r.db}
            empty={
              <>
                {t.emptyText} <Link to="/sites/new">{t.emptyLink}</Link>
              </>
            }
            columns={columns}
            footer={rows.length > 0 ? <span>{footerText(t, footerCounts(rows))}</span> : undefined}
          />
          {/* Luôn có trong trang (sr-only khi trống): vùng báo được chèn cùng lúc với chữ thì trình đọc màn hình hay bỏ qua. */}
          <p role="status" className={op.pending ? "m-0 text-muted" : "sr-only"}>
            {op.pending ? m.previewing : ""}
          </p>
          <p className="m-0 max-w-[720px] text-[13px] text-muted">{t.hint}</p>
          <p className="m-0 max-w-[720px] text-[13px] text-muted">{t.hintM13}</p>

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

      <CreateDialog open={creating} onOpenChange={setCreating} op={op} />
    </>
  );
}

/** Cỡ hay số bảng không đọc được: một gạch ngang cho cột số gọn, đủ lời trong title và cho trình đọc màn hình. */
function Unreadable({ label }: { label: string }) {
  return (
    <span title={label} className="text-muted">
      <span aria-hidden="true">—</span>
      <span className="sr-only">{label}</span>
    </span>
  );
}
