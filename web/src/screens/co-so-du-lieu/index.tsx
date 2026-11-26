// Màn Cơ sở dữ liệu (mục 6.9 và 6.12 của KE-HOACH.md; giao diện bản 2, mục 3.7
// của M16-thiet-ke.md): một bảng mọi cơ sở dữ liệu (GET /api/v1/databases), gồm
// DB đi kèm site và DB rời (M13).
//
//   DB của site: Đổi mật khẩu, Xuất .sql; menu ba chấm: Nhập từ bản đã xuất (M9),
//                Đổi charset, Tạm khoá / Mở khoá (admin).
//   DB rời:      Quản lý, Xoá; menu ba chấm: Sao lưu ngay và Nhập từ bản sao lưu (M33),
//                Đổi mật khẩu, Sửa (charset có xem trước, ghi chú lưu ngay), Tạm khoá / Mở khoá (admin).
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
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Link, useNavigate } from "react-router";
import { dbManagePath } from "../../api/m32";
import { errorMessage, get } from "../../api/client";
import { useIdentity } from "../../api/m13-dbs";
import { useOverview } from "../../api/queries";
import type { Backup, DatabaseRow } from "../../api/types";
import { DomainText } from "../../components/DomainText";
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
  const navigate = useNavigate();
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
  const op = usePreviewedOp([databasesKey, backupsKey, trashKey, ["sites"]]);
  const [creating, setCreating] = useState(false);
  /** Khối đang mở dưới bảng: chọn bản nhập (DB của site) hoặc Sửa, Đổi charset. Theo tên DB để dữ liệu luôn mới. */
  const [view, setView] = useState<{ kind: "import" | "edit"; db: string } | null>(null);
  // Khối mở từ menu ba chấm nằm dưới bảng: vừa mở thì đưa focus vào (trình duyệt tự cuộn tới).
  // setTimeout: Radix trả focus về nút ba chấm trong một setTimeout lúc menu đóng; việc này phải chạy sau nó.
  const panelRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!view) return;
    const id = setTimeout(() => panelRef.current?.focus(), 0);
    return () => clearTimeout(id);
  }, [view]);

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
      // M34: DB đã bị xoá ngoài TPAN. Chỉ còn một việc làm được: gỡ dòng của nó (bản sao lưu được giữ).
      if (r.missing) {
        return (
          <div className="flex flex-col items-end gap-[2px]">
            {row([button("trash", t.forget, t.forgetFor(r.db))])}
            <span className="max-w-[260px] text-[12.5px] text-muted">{t.missingStandalone}</span>
          </div>
        );
      }
      // v0.2.14: nút đầu là "Quản lý" (mở màn xem bảng, sửa dữ liệu, chạy SQL: thứ người dùng
      // chờ ở chữ "Sửa"); đổi bộ ký tự và ghi chú chuyển vào menu ba chấm.
      return row(
        [{ label: t.manage, ariaLabel: t.manageFor(r.db), onClick: () => navigate(dbManagePath("standalone", r.db)) }, button("trash", t.del, t.trashFor(r.db))],
        [
          // M33: DB rời có bản sao lưu riêng (bản backup của site không chứa nó).
          { label: t.backupNow, disabled: op.pending, onSelect: () => act("backup", r) },
          { label: t.importOpenDb, disabled: op.pending || Boolean(r.suspended), hint: r.suspended ? t.importDbBlocked : undefined, onSelect: () => open("import", r) },
          { label: t.password, disabled: op.pending, onSelect: () => act("password", r) },
          { label: t.edit, disabled: op.pending, onSelect: () => open("edit", r) },
          ...lock,
        ],
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
    // M34: DB của site đã bị xoá ngoài TPAN. Hai đường có lại dữ liệu đều tạo lại DB từ file:
    // khôi phục site từ một bản backup (màn Backup), hoặc nhập một bản xuất .sql (menu ba chấm).
    if (r.missing) {
      return (
        <div className="flex flex-col items-end gap-[2px]">
          {row(
            [button("trash-site", t.del, t.delSiteDbFor(r.db, r.domain))],
            [{ label: t.importOpen, disabled: op.pending || siteSuspended, hint: siteSuspended ? t.importBlocked : undefined, onSelect: () => open("import", r) }],
          )}
          <span className="max-w-[260px] text-[12.5px] text-muted">
            {t.missingSiteLead}
            <Link to="/backups">{t.missingSiteLink}</Link>
            {t.missingSiteTail}
          </span>
        </div>
      );
    }
    return row(
      [{ label: t.manage, ariaLabel: t.manageFor(r.db), onClick: () => navigate(dbManagePath("site", r.domain)) }, button("trash-site", t.del, t.delSiteDbFor(r.db, r.domain))],
      [
        { label: t.password, disabled: op.pending, onSelect: () => act("password", r) },
        { label: t.edit, disabled: op.pending, onSelect: () => open("edit", r) },
        { label: t.dump, disabled: op.pending, onSelect: () => act("dump", r) },
        { label: t.importOpen, disabled: op.pending || siteSuspended, hint: siteSuspended ? t.importBlocked : undefined, onSelect: () => open("import", r) },
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
        <div className="flex min-w-[190px] flex-col gap-[2px]">
          <span className="flex flex-wrap items-center gap-x-[10px]">
            <MonoText>{r.db}</MonoText>
            {r.suspended && <SuspendTag how={r.suspended} />}
            {r.missing && <Tag title={t.missingHint}>{t.missingTag}</Tag>}
          </span>
          {r.user === r.db ? <span className="text-[12.5px] text-muted">{t.sameUser}</span> : <MonoText size={12.5} muted>{r.user}</MonoText>}
          {r.note && <span className="max-w-[320px] text-[12.5px] text-muted [overflow-wrap:anywhere]">{r.note}</span>}
        </div>
      ),
    },
    {
      header: t.colOfSite,
      // v0.2.14: tên miền dài xuống dòng sau dấu chấm (DomainText) thay vì kéo rộng bảng.
      nowrap: false,
      className: "min-w-[180px] max-w-[260px]",
      cell: (r) =>
        r.kind === "site" ? (
          <Link to={`/sites/${encodeURIComponent(r.domain)}`} className="no-underline hover:underline">
            <DomainText domain={r.domain} />
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
      // v0.2.14: số bảng nằm dưới cỡ, một cột thay vì hai, để bảng vừa khung ở 1280 px.
      header: t.colSize,
      align: "right",
      cell: (r) => (
        <span className="flex flex-col items-end">
          <span>{r.size_bytes < 0 ? <Unreadable label={t.unreadable} /> : formatSize(r.size_bytes, lang)}</span>
          {r.tables >= 0 && <span className="text-[12.5px] text-muted">{t.tablesCount(r.tables)}</span>}
        </span>
      ),
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
            minWidth={860}
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

          {view && viewRow && !op.hasPreview && (
            <div ref={panelRef} tabIndex={-1} className="outline-offset-0">
              {view.kind === "import" && (
                <ImportPicker
                  key={viewRow.db}
                  domain={viewRow.kind === "site" ? viewRow.domain : viewRow.db}
                  standalone={viewRow.kind !== "site"}
                  pending={op.busy(`import:${viewRow.db}`)}
                  anyPending={op.pending}
                  onPreview={(backup) => act("import", viewRow, { backup })}
                  onCancel={closeAll}
                />
              )}
              {view.kind === "edit" && <EditPanel key={viewRow.db} row={viewRow} op={op} onClose={closeAll} />}
            </div>
          )}

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
