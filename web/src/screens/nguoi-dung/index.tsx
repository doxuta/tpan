// Màn Người dùng (M13, mục 6.12 và 10 của KE-HOACH.md), chỉ admin. Bảng như
// HestiaCP: tên (kèm tên đầy đủ, email), vai, gói, site và DB đang dùng trên
// giới hạn, dung lượng, trạng thái; mỗi hàng có Sửa, Tạm khoá hoặc Mở khoá, Đổi
// mật khẩu, Xoá, Xem như. Chọn nhiều hàng để khoá hoặc mở khoá hàng loạt.
//
// Việc đổi máy (tạm khoá, mở khoá, đưa mọi thứ của user vào thùng rác) đi ba
// chặng như mọi nơi: bấm nút → ?dry_run=1 → bản xem trước (PlanReview) → chạy →
// /jobs/:id. Thêm, sửa, đổi mật khẩu, xoá user chỉ ghi vào tpan.db nên là hộp
// thoại một bước.

import { useEffect, useRef, useState } from "react";
import { errorMessage } from "../../api/client";
import { userPath, useUsers, usePackages, usePlanFlow, useViewAs, useWho } from "../../api/m13-users";
import type { UserRow } from "../../api/types";
import { Button } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { StatusMark } from "../../components/StatusMark";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/nguoi-dung";
import { Note } from "../cai-dat/Note";
import { PlanReview } from "../duyet-plan";
import { formatDateTime } from "../lich-su/format";
import { ErrorNote } from "../site/ErrorNote";
import { BulkPanel, type BulkAction } from "./Bulk";
import { diskLimitText, diskUsedText } from "./model";
import { PasswordBox, type Secret } from "./PasswordBox";
import { DeleteDialog, PasswordDialog, SuspendDialog, UserFormDialog } from "./UserDialogs";

type Dialog =
  | { kind: "add" }
  | { kind: "edit" | "suspend" | "password" | "delete"; row: UserRow }
  | { kind: "bulk-suspend" };

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
  const asking = (path: string) => flow.dry.isPending && flow.asked?.path === path;

  const toggle = (name: string) => setSelected((s) => (s.includes(name) ? s.filter((n) => n !== name) : [...s, name]));
  const say = (m: string) => {
    setNotice(m);
    setSecret(null);
  };

  const ask = (row: UserRow, what: "suspend" | "unsuspend", reason = "") => {
    setBulk(null);
    flow.ask({ method: "POST", path: userPath(row.user.name, `/${what}`), body: what === "suspend" ? { reason } : undefined });
  };

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />

      <div className="flex flex-wrap items-center gap-[10px] pt-[24px]">
        <Button variant="primary" onClick={() => setDialog({ kind: "add" })}>
          {t.add}
        </Button>
        {selected.length > 0 && (
          <>
            <span className="text-muted" aria-live="polite">
              {t.selectedCount(selected.length)}
            </span>
            {selActive.length > 0 && (
              <Button disabled={busy} onClick={() => setDialog({ kind: "bulk-suspend" })}>
                {t.bulkSuspend(selActive.length)}
              </Button>
            )}
            {selSuspended.length > 0 && (
              <Button disabled={busy} onClick={() => setBulk({ action: "unsuspend", names: selSuspended, reason: "" })}>
                {t.bulkUnsuspend(selSuspended.length)}
              </Button>
            )}
            <Button disabled={busy} onClick={() => setSelected([])}>
              {t.clearSelection}
            </Button>
          </>
        )}
      </div>

      {notice && (
        <div className="pt-[16px]">
          <Note kind="pass">{notice}</Note>
        </div>
      )}
      {secret && <PasswordBox secret={secret} onDismiss={() => setSecret(null)} />}

      <section aria-label={t.tableName} className="pt-[22px]">
        {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
        {q.isError && (
          <div>
            <p className="mt-0 mb-[12px]">
              {t.loadFailed} {errorMessage(q.error, lang)}
            </p>
            <Button onClick={() => void q.refetch()}>{k.retry}</Button>
          </div>
        )}
        {q.isSuccess && (
          <TableList
            caption={t.tableName}
            rows={rows}
            rowKey={(r) => r.user.name}
            empty={t.empty}
            columns={[
              {
                header: t.colSelect,
                cell: (r) =>
                  r.user.name === who.name ? null : (
                    <input
                      type="checkbox"
                      aria-label={t.selectRow(r.user.name)}
                      checked={selected.includes(r.user.name)}
                      onChange={() => toggle(r.user.name)}
                      className="size-[20px] accent-ink"
                    />
                  ),
              },
              {
                header: t.colUser,
                cell: (r) => (
                  <div className="min-w-[160px]">
                    <span className="inline-flex flex-wrap items-baseline gap-x-[8px]">
                      <MonoText>{r.user.name}</MonoText>
                      {r.user.name === who.name && <Tag>{t.you}</Tag>}
                    </span>
                    {r.user.full_name && <div>{r.user.full_name}</div>}
                    {r.user.email && <div className="text-[13px] break-all text-muted">{r.user.email}</div>}
                  </div>
                ),
              },
              { header: t.colRole, cell: (r) => k.roles[r.user.role] },
              { header: t.colPackage, cell: (r) => <span className="font-mono text-[12px]">{r.user.package}</span> },
              { header: t.colSites, cell: (r) => <Usage used={String(r.usage.sites)} limit={r.limits.max_sites > 0 ? String(r.limits.max_sites) : null} /> },
              { header: t.colDBs, cell: (r) => <Usage used={String(r.usage.dbs)} limit={r.limits.max_dbs > 0 ? String(r.limits.max_dbs) : null} /> },
              {
                header: t.colDisk,
                cell: (r) => <Usage used={diskUsedText(r.usage, lang)} limit={diskLimitText(r.limits.disk_mb, lang)} hint={r.usage.disk_measured ? `${t.diskHint}: ${formatDateTime(r.usage.disk_measured, lang)}` : undefined} />,
              },
              {
                header: t.colStatus,
                cell: (r) => (
                  <div className="min-w-[130px]">
                    <span className="inline-flex items-center gap-[8px]">
                      <StatusMark status={r.user.status === "active" ? "pass" : "unknown"} />
                      {r.user.status === "active" ? t.statusActive : t.statusSuspended}
                    </span>
                    {r.user.status === "suspended" && <div className="text-[13px] text-muted">{t.suspendedMeta(r.user.suspended_at ? formatDateTime(r.user.suspended_at, lang) : "", r.user.suspend_reason ?? "")}</div>}
                    {r.over_quota && (
                      <span className="mt-[4px] inline-flex items-center gap-[8px]">
                        <StatusMark status="warn" />
                        {t.overQuota}
                      </span>
                    )}
                  </div>
                ),
              },
              {
                header: t.colAction,
                className: "pr-0",
                cell: (r) => {
                  const n = r.user.name;
                  const self = n === who.name;
                  const suspended = r.user.status === "suspended";
                  const what = suspended ? "/unsuspend" : "/suspend";
                  return (
                    <span className="flex max-w-[330px] flex-wrap gap-[8px]">
                      <Button disabled={busy} aria-label={t.forUser(t.edit, n)} onClick={() => setDialog({ kind: "edit", row: r })}>
                        {t.edit}
                      </Button>
                      {/* Không tự khoá hay xoá chính mình (core cũng từ chối); ẩn nút cho khỏi bấm nhầm. */}
                      {!self && (
                        <Button
                          disabled={busy}
                          aria-label={t.forUser(suspended ? t.unsuspend : t.suspend, n)}
                          onClick={() => (suspended ? ask(r, "unsuspend") : setDialog({ kind: "suspend", row: r }))}
                        >
                          {asking(userPath(n, what)) ? t.previewing : suspended ? t.unsuspend : t.suspend}
                        </Button>
                      )}
                      <Button disabled={busy} aria-label={t.forUser(t.password, n)} onClick={() => setDialog({ kind: "password", row: r })}>
                        {t.password}
                      </Button>
                      {!self && n !== "admin" && (
                        <Button disabled={busy} aria-label={t.forUser(t.remove, n)} onClick={() => setDialog({ kind: "delete", row: r })}>
                          {t.remove}
                        </Button>
                      )}
                      {!self && r.user.role === "user" && (
                        <Button disabled={busy || viewAs.isPending} aria-label={t.forUser(t.viewAs, n)} onClick={() => viewAs.mutate(n)}>
                          {viewAs.isPending && viewAs.variables === n ? t.viewAsStarting : t.viewAs}
                        </Button>
                      )}
                    </span>
                  );
                },
              },
            ]}
          />
        )}
        {viewAs.isError && (
          <div className="pt-[12px]">
            <ErrorNote error={viewAs.error} heading={t.viewAsFailed} />
          </div>
        )}
      </section>

      {flow.dry.isError && flow.asked && (
        <div ref={resultRef} tabIndex={-1} className="pt-[20px] outline-offset-0">
          <ErrorNote error={flow.dry.error} heading={t.previewFailed} checksHeading={t.checksHeading} />
          <div className="pt-[12px]">
            <Button onClick={flow.close}>{k.cancel}</Button>
          </div>
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

/** "3 / 10", "3 / không giới hạn", hoặc "chưa đo" khi chưa có số đã dùng. */
function Usage({ used, limit, hint }: { used: string | null; limit: string | null; hint?: string }) {
  const t = useStrings(strings);
  return (
    <span title={hint} className="whitespace-nowrap tabular-nums">
      {used === null ? <span className="text-muted">{t.notMeasured}</span> : used}
      <span className="text-muted"> / {limit ?? t.unlimited}</span>
    </span>
  );
}
