// Màn Người dùng (M13, mục 6.12 và 10 của KE-HOACH.md; giao diện bản 2, mục 3.7
// của M16-thiet-ke.md), chỉ admin. Bảng như HestiaCP: tên (kèm tên đầy đủ, email),
// vai, gói, site và DB rời đang dùng trên giới hạn, dung lượng, trạng thái; mỗi
// hàng có Sửa, Đổi mật khẩu và menu ba chấm (Tạm khoá hoặc Mở khoá, Xem như, Xoá).
// Chọn nhiều hàng để khoá hoặc mở khoá hàng loạt.
//
// Việc đổi máy (tạm khoá, mở khoá, đưa mọi thứ của user vào thùng rác) đi ba
// chặng như mọi nơi: bấm nút → ?dry_run=1 → bản xem trước (PlanReview) → chạy →
// ở lại màn, thẻ theo dõi job báo tiến độ. Thêm, sửa, đổi mật khẩu, xoá user chỉ
// ghi vào tpan.db nên là hộp thoại một bước.
//
// Màn này là màn mẫu của bản 2: ScreenHeader (có đường dẫn) → một TableList (tự
// vẽ thẻ, dải "Đã chọn N" ở prop above, chân bảng) → các khối kết quả. Màn khác
// chép cách ghép này.

import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { errorMessage } from "../../api/client";
import { userPath, useUsers, usePackages, usePlanFlow, useViewAs, useWho } from "../../api/m13-users";
import type { UserRow } from "../../api/types";
import { Avatar } from "../../components/Avatar";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Checkbox } from "../../components/Field";
import { Notice } from "../../components/Notice";
import { RowActions, type MenuAction } from "../../components/RowActions";
import { ScreenHeader } from "../../components/ScreenHeader";
import { StatusLine } from "../../components/StatusMark";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { UsageMeter } from "../../components/UsageMeter";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/nguoi-dung";
import { PlanReview } from "../duyet-plan";
import { formatDateTime } from "../lich-su/format";
import { ErrorNote } from "../site/ErrorNote";
import { BulkPanel, type BulkAction } from "./Bulk";
import { diskLimitText, diskPercent, diskUsedText, percentOf } from "./model";
import { PasswordBox, type Secret } from "./PasswordBox";
import { DeleteDialog, PasswordDialog, SuspendDialog, UserFormDialog } from "./UserDialogs";

type Dialog =
  | { kind: "add" }
  | { kind: "edit" | "suspend" | "password" | "delete"; row: UserRow }
  | { kind: "bulk-suspend" };

/** Giới hạn 0 của gói là "không giới hạn": UsageMeter nhận null. */
const limitText = (n: number): string | null => (n > 0 ? String(n) : null);

export default function Users() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const who = useWho();
  const q = useUsers();
  const pk = usePackages();
  const flow = usePlanFlow();
  const viewAs = useViewAs();

  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [selected, setSelected] = useState<string[]>([]);
  const [bulk, setBulk] = useState<{ action: BulkAction; names: string[]; reason: string } | null>(null);
  const [secret, setSecret] = useState<Secret | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const resultRef = useRef<HTMLDivElement>(null);

  // Có bản xem trước hoặc lỗi mới thì đưa người dùng tới đó, vì nó nằm dưới bảng.
  const shown = flow.open ?? (flow.dry.isError ? flow.asked : null);
  useEffect(() => {
    if (shown) {
      resultRef.current?.scrollIntoView({ block: "start" });
      resultRef.current?.focus();
    }
  }, [shown]);

  const rows = q.data ?? [];
  const byName = (n: string) => rows.find((r) => r.user.name === n);
  const selActive = selected.filter((n) => byName(n)?.user.status === "active");
  const selSuspended = selected.filter((n) => byName(n)?.user.status === "suspended");
  const busy = flow.dry.isPending || bulk !== null;
  // Việc nằm trong menu ba chấm (menu đóng ngay khi chọn) nên nút không đổi chữ được: nói ở một dòng dưới bảng.
  const working = flow.dry.isPending ? t.previewing : viewAs.isPending ? t.viewAsStarting : null;

  const toggle = (name: string) => setSelected((s) => (s.includes(name) ? s.filter((n) => n !== name) : [...s, name]));
  const say = (m: string) => {
    setNotice(m);
    setSecret(null);
  };

  const ask = (row: UserRow, what: "suspend" | "unsuspend", reason = "") => {
    setBulk(null);
    flow.ask({ method: "POST", path: userPath(row.user.name, `/${what}`), body: what === "suspend" ? { reason } : undefined });
  };

  /** Cột Việc. Ba luật ẩn của bản 1: không tự khoá, xoá hay xem như chính mình; không xoá `admin`; chỉ xem như user mang vai user. */
  const actions = (r: UserRow) => {
    const n = r.user.name;
    const self = n === who.name;
    const suspended = r.user.status === "suspended";
    const more: (MenuAction & { label: string })[] = [];
    if (!self) {
      more.push({
        label: suspended ? t.unsuspend : t.suspend,
        disabled: busy,
        onSelect: () => (suspended ? ask(r, "unsuspend") : setDialog({ kind: "suspend", row: r })),
      });
      if (r.user.role === "user") more.push({ label: t.viewAs, disabled: busy || viewAs.isPending, onSelect: () => viewAs.mutate(n) });
      if (n !== "admin") more.push({ label: t.remove, danger: true, disabled: busy, onSelect: () => setDialog({ kind: "delete", row: r }) });
    }
    return (
      <RowActions
        primary={[
          { label: t.edit, ariaLabel: t.forUser(t.edit, n), disabled: busy, onClick: () => setDialog({ kind: "edit", row: r }) },
          { label: t.password, ariaLabel: t.forUser(t.password, n), disabled: busy, onClick: () => setDialog({ kind: "password", row: r }) },
        ]}
        more={more}
        moreLabel={t.moreFor(n, more.map((m) => m.label.toLowerCase()).join(", "))}
      />
    );
  };

  /** Dải "Đã chọn N" đặt ngay trên bảng. */
  const selection = selected.length > 0 && (
    <div className="m-[12px] flex flex-wrap items-center gap-[10px] rounded-ctl bg-accent-tint px-[12px] py-[8px]">
      <span className="font-medium" aria-live="polite">
        {t.selectedCount(selected.length)}
      </span>
      <span className="flex-1" />
      {selActive.length > 0 && (
        <Button size="sm" disabled={busy} onClick={() => setDialog({ kind: "bulk-suspend" })}>
          {t.bulkSuspend(selActive.length)}
        </Button>
      )}
      {selSuspended.length > 0 && (
        <Button size="sm" disabled={busy} onClick={() => setBulk({ action: "unsuspend", names: selSuspended, reason: "" })}>
          {t.bulkUnsuspend(selSuspended.length)}
        </Button>
      )}
      <Button size="sm" variant="text" disabled={busy} onClick={() => setSelected([])}>
        {t.clearSelection}
      </Button>
    </div>
  );

  return (
    <>
      <ScreenHeader
        crumbs={[{ label: k.groups.taiKhoan }, { label: t.title }]}
        title={t.title}
        summary={t.summary}
        actions={
          <Button variant="primary" icon="plus" onClick={() => setDialog({ kind: "add" })}>
            {t.add}
          </Button>
        }
      />

      {notice && (
        <Card>
          <Notice kind="pass" role="status">
            {notice}
          </Notice>
        </Card>
      )}
      {secret && <PasswordBox secret={secret} onDismiss={() => setSecret(null)} />}

      {q.isPending && (
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {q.isError && (
        <Card>
          <p className="mt-0 mb-[12px]">
            {t.loadFailed} {errorMessage(q.error, lang)}
          </p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {q.isSuccess && (
        <TableList
          label={t.tableName}
          caption={t.tableName}
          minWidth={900}
          rows={rows}
          rowKey={(r) => r.user.name}
          empty={t.empty}
          above={selection}
          footer={
            <>
              <span>{t.footerCount(rows.length)}</span>
              <span>
                {t.footerTrashRule} {t.footerUnlimited}{" "}
                <Link to="/packages" className="font-medium no-underline hover:underline">
                  {t.footerPlans}
                </Link>
              </span>
            </>
          }
          columns={[
            {
              header: t.colSelect,
              headerCell: <span className="sr-only">{t.colSelect}</span>,
              cell: (r) => {
                const n = r.user.name;
                const self = n === who.name;
                return <Checkbox label={t.selectRow(n)} locked={self ? t.selfLocked : undefined} checked={!self && selected.includes(n)} onChange={() => toggle(n)} />;
              },
            },
            {
              header: t.colUser,
              nowrap: false,
              cell: (r) => {
                const sub = [r.user.full_name, r.user.email].filter(Boolean).join(" · ");
                return (
                  <div className="flex min-w-[200px] items-center gap-[10px]">
                    <Avatar name={r.user.name} />
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-x-[8px]">
                        <span className="font-semibold [overflow-wrap:anywhere]">{r.user.name}</span>
                        {r.user.name === who.name && <Tag>{t.you}</Tag>}
                      </div>
                      {sub && <div className="text-[12.5px] text-muted [overflow-wrap:anywhere]">{sub}</div>}
                    </div>
                  </div>
                );
              },
            },
            { header: t.colRole, cell: (r) => t.roleName[r.user.role] },
            {
              header: t.colPackage,
              cell: (r) => (
                <Link to="/packages" className="font-medium no-underline hover:underline">
                  {r.user.package}
                </Link>
              ),
            },
            {
              header: t.colSites,
              cell: (r) => <UsageMeter used={String(r.usage.sites)} limit={limitText(r.limits.max_sites)} percent={percentOf(r.usage.sites, r.limits.max_sites)} />,
            },
            {
              header: t.colDBs,
              cell: (r) => <UsageMeter used={String(r.usage.dbs)} limit={limitText(r.limits.max_dbs)} percent={percentOf(r.usage.dbs, r.limits.max_dbs)} />,
            },
            {
              header: t.colDisk,
              cell: (r) => (
                <UsageMeter
                  used={diskUsedText(r.usage, lang)}
                  limit={diskLimitText(r.limits.disk_mb, lang)}
                  percent={diskPercent(r.usage, r.limits)}
                  hint={r.usage.disk_measured ? `${t.diskHint}: ${formatDateTime(r.usage.disk_measured, lang)}` : undefined}
                />
              ),
            },
            {
              header: t.colStatus,
              nowrap: false,
              cell: (r) => (
                <div className="flex min-w-[130px] flex-col gap-[4px]">
                  <StatusLine status={r.user.status === "active" ? "pass" : "unknown"}>{r.user.status === "active" ? t.statusActive : t.statusSuspended}</StatusLine>
                  {r.user.status === "suspended" && <span className="text-[12.5px] text-muted">{t.suspendedMeta(r.user.suspended_at ? formatDateTime(r.user.suspended_at, lang) : "", r.user.suspend_reason ?? "")}</span>}
                  {r.over_quota && <StatusLine status="warn">{t.overQuota}</StatusLine>}
                </div>
              ),
            },
            { header: t.colAction, align: "right", cell: actions },
          ]}
        />
      )}

      {/* Luôn có trong trang (sr-only khi trống): vùng báo được chèn cùng lúc với chữ thì trình đọc màn hình hay bỏ qua. */}
      <p role="status" className={working ? "m-0 text-muted" : "sr-only"}>
        {working ?? ""}
      </p>
      {viewAs.isError && (
        <Card>
          <ErrorNote error={viewAs.error} heading={t.viewAsFailed} />
        </Card>
      )}

      {flow.dry.isError && flow.asked && (
        <div ref={resultRef} tabIndex={-1} className="outline-offset-0">
          <Card>
            <ErrorNote error={flow.dry.error} heading={t.previewFailed} checksHeading={t.checksHeading} />
            <div className="pt-[12px]">
              <Button onClick={flow.close}>{k.cancel}</Button>
            </div>
          </Card>
        </div>
      )}
      {flow.open && (
        <div ref={resultRef} tabIndex={-1} className="outline-offset-0">
          <PlanReview preview={flow.open.preview} onRun={flow.start} running={flow.run.isPending} error={flow.run.error} onCancel={flow.close} />
        </div>
      )}
      {bulk && (
        <BulkPanel
          key={bulk.action + bulk.names.join(",")}
          action={bulk.action}
          names={bulk.names}
          reason={bulk.reason}
          onClose={() => {
            setBulk(null);
            setSelected([]);
          }}
        />
      )}

      {dialog?.kind === "add" && (
        <UserFormDialog
          packages={(pk.data ?? []).map((p) => p.package.name)}
          onClose={() => setDialog(null)}
          onDone={(what, name, s) => {
            say(what === "added" ? t.addedDone(name) : t.editedDone(name));
            setSecret(s);
          }}
        />
      )}
      {dialog?.kind === "edit" && (
        <UserFormDialog
          row={dialog.row}
          packages={(pk.data ?? []).map((p) => p.package.name)}
          onClose={() => setDialog(null)}
          onDone={(_what, name) => say(t.editedDone(name))}
        />
      )}
      {dialog?.kind === "suspend" && (
        <SuspendDialog
          title={t.suspendTitle(dialog.row.user.name)}
          onClose={() => setDialog(null)}
          onAsk={(reason) => {
            const row = dialog.row;
            setDialog(null);
            ask(row, "suspend", reason);
          }}
        />
      )}
      {dialog?.kind === "bulk-suspend" && (
        <SuspendDialog
          title={t.suspendManyTitle(selActive.length)}
          onClose={() => setDialog(null)}
          onAsk={(reason) => {
            setDialog(null);
            flow.close();
            setBulk({ action: "suspend", names: selActive, reason });
          }}
        />
      )}
      {dialog?.kind === "password" && (
        <PasswordDialog
          name={dialog.row.user.name}
          onClose={() => setDialog(null)}
          onDone={(name, s) => {
            say(t.pwDone(name));
            setSecret(s);
          }}
        />
      )}
      {dialog?.kind === "delete" && (
        <DeleteDialog
          name={dialog.row.user.name}
          onClose={() => setDialog(null)}
          onDone={(name) => {
            say(t.deletedDone(name));
            setSelected((s) => s.filter((n) => n !== name));
          }}
          onTrashAll={() => {
            const n = dialog.row.user.name;
            setDialog(null);
            setBulk(null);
            flow.ask({ method: "POST", path: userPath(n, "/trash-all") });
          }}
        />
      )}
    </>
  );
}
