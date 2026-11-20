// Màn Gói (M13, mục 6.12 của KE-HOACH.md), chỉ admin: bảng gói với giới hạn
// (0 hiện là "không giới hạn") và số user đang dùng; thêm, sửa, xoá. Gói
// "default" có sẵn, không xoá được. Ghi gói chỉ đổi tpan.db (không có job nên
// không có bản xem trước); giới hạn có hiệu lực ngay cho mọi user dùng gói.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { del, errorMessage, post } from "../../api/client";
import { packagePath, packagesKey, packagesPath, refreshAccounts, usePackages } from "../../api/m13-users";
import type { Package, PackageRow } from "../../api/types";
import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { MonoText } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/goi";
import { strings as khung } from "../../i18n/khung";
import { Note } from "../cai-dat/Note";
import { Field } from "../dang-nhap/Field";
import { diskLimitText } from "../nguoi-dung/model";
import { ErrorNote } from "../site/ErrorNote";

type Limits = Omit<Package, "created" | "name">;
type Dlg = { kind: "add" } | { kind: "edit" | "delete"; row: PackageRow };

export default function Packages() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = usePackages();
  const [dlg, setDlg] = useState<Dlg | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const limit = (n: number) => (n > 0 ? String(n) : t.unlimited);

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />
      <div className="pt-[24px]">
        <Button variant="primary" onClick={() => setDlg({ kind: "add" })}>
          {t.add}
        </Button>
      </div>
      {notice && (
        <div className="pt-[16px]">
          <Note kind="pass">{notice}</Note>
        </div>
      )}

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
            rows={q.data}
            rowKey={(r) => r.package.name}
            empty={t.empty}
            columns={[
              {
                header: t.colName,
                cell: (r) => (
                  <span className="inline-flex flex-wrap items-baseline gap-x-[8px]">
                    <MonoText>{r.package.name}</MonoText>
                    {r.package.name === "default" && <Tag>{t.builtIn}</Tag>}
                  </span>
                ),
              },
              { header: t.colSites, cell: (r) => limit(r.package.max_sites) },
              { header: t.colDBs, cell: (r) => limit(r.package.max_dbs) },
              { header: t.colDisk, cell: (r) => diskLimitText(r.package.disk_mb, lang) ?? t.unlimited },
              { header: t.colBackups, cell: (r) => limit(r.package.max_backups) },
              { header: t.colUsers, cell: (r) => <span className="tabular-nums">{r.users}</span> },
              {
                header: t.colAction,
                className: "pr-0",
                cell: (r) => (
                  <span className="flex flex-wrap gap-[8px]">
                    <Button aria-label={t.forPackage(t.edit, r.package.name)} onClick={() => setDlg({ kind: "edit", row: r })}>
                      {t.edit}
                    </Button>
                    {r.package.name !== "default" && (
                      <Button aria-label={t.forPackage(t.remove, r.package.name)} onClick={() => setDlg({ kind: "delete", row: r })}>
                        {t.remove}
                      </Button>
                    )}
                  </span>
                ),
              },
            ]}
          />
        )}
      </section>

      {(dlg?.kind === "add" || dlg?.kind === "edit") && (
        <PackageDialog
          row={dlg.kind === "edit" ? dlg.row : undefined}
          onClose={() => setDlg(null)}
          onDone={(name, edited) => setNotice(edited ? t.editedDone(name) : t.addedDone(name))}
        />
      )}
      {dlg?.kind === "delete" && <DeleteDialog name={dlg.row.package.name} onClose={() => setDlg(null)} onDone={(n) => setNotice(t.deletedDone(n))} />}
    </>
  );
}

const toInt = (s: string): number | null => (/^\d{1,9}$/.test(s.trim()) ? Number(s) : null);

function PackageDialog({ row, onClose, onDone }: { row?: PackageRow; onClose: () => void; onDone: (name: string, edited: boolean) => void }) {
  const t = useStrings(strings);
  const qc = useQueryClient();
  const edit = row !== undefined;
  const p = row?.package;
  const [name, setName] = useState(p?.name ?? "");
  const [sites, setSites] = useState(String(p?.max_sites ?? 0));
  const [dbs, setDbs] = useState(String(p?.max_dbs ?? 0));
  const [disk, setDisk] = useState(String(p?.disk_mb ?? 0));
  const [backups, setBackups] = useState(String(p?.max_backups ?? 0));
  const [local, setLocal] = useState<string | null>(null);

  const save = useMutation({
    mutationFn: (body: Limits & { name: string }) => post<PackageRow>(edit ? packagePath(body.name) : packagesPath, body),
    onSuccess: (_r, body) => {
      refreshAccounts(qc);
      void qc.invalidateQueries({ queryKey: packagesKey });
      onDone(body.name, edit);
      onClose();
    },
  });

  const submit = () => {
    if (save.isPending) return;
    save.reset();
    const n = { max_sites: toInt(sites), max_dbs: toInt(dbs), disk_mb: toInt(disk), max_backups: toInt(backups) };
    if (!edit && name.trim() === "") return setLocal(t.nameMissing);
    if (Object.values(n).some((v) => v === null)) return setLocal(t.notNumber);
    setLocal(null);
    save.mutate({ name: edit ? p!.name : name.trim(), ...(n as Limits) });
  };
  const onForm = (e: FormEvent) => {
    e.preventDefault();
    submit();
  };

  const num = (id: string, label: string, hint: string, v: string, set: (s: string) => void) => (
    <Field id={id} type="number" min={0} step={1} inputMode="numeric" label={label} hint={hint} value={v} onChange={(e) => set(e.target.value)} />
  );

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={edit ? t.editTitle(p!.name) : t.addTitle} confirmLabel={save.isPending ? t.saving : edit ? t.editConfirm : t.addConfirm} onConfirm={submit} busy={save.isPending}>
      <form onSubmit={onForm} className="flex flex-col gap-[14px]">
        {!edit && <Field id="g-ten" label={t.fName} hint={t.fNameHint} autoComplete="off" autoCapitalize="none" spellCheck={false} autoFocus required value={name} onChange={(e) => setName(e.target.value)} />}
        {num("g-site", t.fSites, `${t.fSitesHint} ${t.fZeroHint}`, sites, setSites)}
        {num("g-db", t.fDBs, `${t.fDBsHint} ${t.fZeroHint}`, dbs, setDbs)}
        {num("g-dia", t.fDisk, `${t.fDiskHint} ${t.fZeroHint}`, disk, setDisk)}
        {num("g-backup", t.fBackups, t.fZeroHint, backups, setBackups)}
        {local && <Note kind="fail">{local}</Note>}
        {save.isError && <ErrorNote error={save.error} />}
        {/* Nút gửi ẩn để bấm Enter trong ô nhập là gửi (nút thật của hộp nằm ngoài form). */}
        <button type="submit" tabIndex={-1} aria-hidden="true" className="sr-only" />
      </form>
    </Dialog>
  );
}

function DeleteDialog({ name, onClose, onDone }: { name: string; onClose: () => void; onDone: (name: string) => void }) {
  const t = useStrings(strings);
  const qc = useQueryClient();
  const remove = useMutation({
    mutationFn: () => del<void>(packagePath(name)),
    onSuccess: () => {
      refreshAccounts(qc);
      onDone(name);
      onClose();
    },
  });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={t.delTitle(name)} description={t.delDesc} confirmLabel={remove.isPending ? t.working : t.delConfirm} onConfirm={() => remove.mutate()} busy={remove.isPending}>
      {remove.isError && <ErrorNote error={remove.error} />}
    </Dialog>
  );
}
