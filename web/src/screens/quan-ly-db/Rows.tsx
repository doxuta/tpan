// Lưới dòng của một bảng (M32): 50 dòng mỗi trang, bấm tên cột để sắp, thêm dòng,
// sửa và xoá từng dòng. Sửa và xoá cần khoá chính (để chỉ đúng một dòng); bảng
// không có khoá chính thì có dòng chú giải chỉ sang ô Chạy SQL.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { errorMessage, get, post } from "../../api/client";
import { changedValues, dbInfoKey, dbRowsKey, rowKeyOf, rowKeyQuery, rowKeyText, type DBCell, type DBChanged, type DBColumn, type DBRowChange, type DBRowsPage } from "../../api/m32";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Dialog } from "../../components/Dialog";
import { Checkbox, Field, Input, Textarea } from "../../components/Field";
import { Notice } from "../../components/Notice";
import { RowActions } from "../../components/RowActions";
import { TableList, type Column } from "../../components/TableList";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/quan-ly-db";
import { Cell } from "./ResultTable";

const pageSize = 50;
/** Ô lớn hơn chừng này byte thì máy chủ từ chối ghi (core.DBDataMaxCell). */
const maxCell = 256 * 1024;

type Props = { base: string; table: string; onClose: () => void };

type FormState = { mode: "add" } | { mode: "edit"; key: Record<string, string> } | { mode: "delete"; key: Record<string, string> } | null;

export function Rows({ base, table, onClose }: Props) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const qc = useQueryClient();
  const [offset, setOffset] = useState(0);
  const [order, setOrder] = useState<{ col: string; desc: boolean } | null>(null);
  const [form, setForm] = useState<FormState>(null);
  const [done, setDone] = useState<number | null>(null);

  const path = `${base}/data/${encodeURIComponent(table)}`;
  const params = new URLSearchParams({ offset: String(offset), limit: String(pageSize) });
  if (order) {
    params.set("order", order.col);
    if (order.desc) params.set("dir", "desc");
  }
  const q = useQuery({ queryKey: [...dbRowsKey(base, table), offset, order?.col ?? "", order?.desc ?? false], queryFn: () => get<DBRowsPage>(`${path}?${params.toString()}`) });
  const page = q.data;

  const change = useMutation({
    mutationFn: (body: DBRowChange) => post<DBChanged>(path, body),
    onSuccess: (res) => {
      setDone(res.affected);
      setForm(null);
      void qc.invalidateQueries({ queryKey: dbRowsKey(base, table) });
      void qc.invalidateQueries({ queryKey: dbInfoKey(base) });
    },
  });

  const sortBy = (col: string) => {
    setOffset(0);
    setOrder((cur) => (cur && cur.col === col ? (cur.desc ? null : { col, desc: true }) : { col, desc: false }));
  };

  const columns: Column<DBCell[]>[] = page
    ? [
        ...page.columns.map(
          (c, i): Column<DBCell[]> => ({
            header: c.name,
            headerCell: (
              <button type="button" onClick={() => sortBy(c.name)} title={`${c.type}${c.key === "PRI" ? " · PRIMARY KEY" : ""}`} aria-label={t.sortBy(c.name)} className="bg-transparent p-0 text-left font-medium hover:underline">
                {c.name}
                {order?.col === c.name ? (order.desc ? " ↓" : " ↑") : ""}
              </button>
            ),
            cell: (r) => <Cell value={r[i] ?? null} clipped={page.clipped[page.rows.indexOf(r)]?.[i]} />,
          }),
        ),
        ...(page.primary.length > 0
          ? [
              {
                header: t.colAction,
                align: "right" as const,
                cell: (r: DBCell[]) => {
                  const key = rowKeyOf(page, r);
                  if (!key) return null;
                  const text = rowKeyText(key);
                  return (
                    <RowActions
                      moreLabel={t.deleteRowFor(text)}
                      primary={[
                        { label: t.editRow, ariaLabel: t.editRowFor(text), disabled: change.isPending, onClick: () => setForm({ mode: "edit", key }) },
                        { label: t.deleteRow, ariaLabel: t.deleteRowFor(text), disabled: change.isPending, onClick: () => setForm({ mode: "delete", key }) },
                      ]}
                    />
                  );
                },
              },
            ]
          : []),
      ]
    : [];

  return (
    <>
      {q.isPending && (
        <Card title={t.rowsHeading(table)} titleId="db-dong" actions={<Button onClick={onClose}>{t.rowsClose}</Button>}>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {q.isError && (
        <Card title={t.rowsHeading(table)} titleId="db-dong" actions={<Button onClick={onClose}>{t.rowsClose}</Button>}>
          <Notice kind="fail" role="alert">
            {errorMessage(q.error, lang)}
          </Notice>
        </Card>
      )}
      {page && (
        <TableList<DBCell[]>
          title={t.rowsHeading(table)}
          titleId="db-dong"
          count={page.rows.length > 0 ? t.rowsRange(page.offset + 1, page.offset + page.rows.length) : undefined}
          caption={t.rowsCaption(table)}
          actions={
            <>
              <Button icon="plus" onClick={() => setForm({ mode: "add" })} disabled={change.isPending}>
                {t.addRow}
              </Button>
              <Button onClick={onClose}>{t.rowsClose}</Button>
            </>
          }
          rows={page.rows}
          rowKey={(r) => String(page.rows.indexOf(r))}
          empty={page.offset === 0 ? t.rowsEmpty : t.rowsNone}
          columns={columns}
          above={
            done !== null ? (
              <div className="px-[16px] pt-[12px]">
                <Notice kind="pass" role="status">
                  {t.changed(done)}
                </Notice>
              </div>
            ) : undefined
          }
          footer={
            <span className="flex flex-col gap-[8px]">
              {page.primary.length === 0 && <span>{t.noKeyNote}</span>}
              <span className="flex flex-wrap gap-[8px]">
                <Button onClick={() => setOffset(Math.max(0, offset - pageSize))} disabled={offset === 0 || q.isFetching}>
                  {t.prev}
                </Button>
                <Button onClick={() => setOffset(offset + pageSize)} disabled={!page.more || q.isFetching}>
                  {t.next}
                </Button>
              </span>
            </span>
          }
        />
      )}

      {page && form?.mode === "delete" && (
        <Dialog
          open
          onOpenChange={(o) => !o && setForm(null)}
          title={t.deleteTitle(table)}
          description={t.deleteDesc(rowKeyText(form.key))}
          confirmLabel={change.isPending ? t.deleting : t.deleteConfirm}
          onConfirm={() => change.mutate({ op: "delete", key: form.key })}
          busy={change.isPending}
          danger
        >
          {change.isError && (
            <Notice kind="fail" role="alert">
              {errorMessage(change.error, lang)}
            </Notice>
          )}
        </Dialog>
      )}
      {page && (form?.mode === "add" || form?.mode === "edit") && (
        <RowForm
          key={form.mode === "edit" ? rowKeyText(form.key) : "add"}
          path={path}
          table={table}
          columns={page.columns}
          editKey={form.mode === "edit" ? form.key : null}
          busy={change.isPending}
          error={change.isError ? errorMessage(change.error, lang) : null}
          onCancel={() => {
            change.reset();
            setForm(null);
          }}
          onSubmit={(values) => change.mutate(form.mode === "edit" ? { op: "update", key: form.key, values } : { op: "insert", values })}
        />
      )}
    </>
  );
}

type FormProps = {
  path: string;
  table: string;
  columns: DBColumn[];
  /** null: thêm dòng mới. */
  editKey: Record<string, string> | null;
  busy: boolean;
  error: string | null;
  onCancel: () => void;
  onSubmit: (values: Record<string, DBCell>) => void;
};

/** Form một dòng: mỗi cột một ô. Sửa thì đọc lại nguyên dòng (ô không cắt) rồi chỉ gửi ô đã đổi. */
function RowForm({ path, table, columns, editKey, busy, error, onCancel, onSubmit }: FormProps) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const row = useQuery({
    queryKey: ["dbdata-row", path, editKey ? rowKeyQuery(editKey) : ""],
    queryFn: () => get<DBRowsPage>(`${path}?${rowKeyQuery(editKey ?? {})}`),
    enabled: editKey !== null,
    gcTime: 0,
  });
  const [edits, setEdits] = useState<Record<string, DBCell | undefined>>({});
  const before: DBCell[] = editKey ? (row.data?.rows[0] ?? []) : [];
  const ready = editKey === null || (row.isSuccess && row.data.rows.length === 1);
  const values = editKey ? changedValues(columns, before, edits) : (Object.fromEntries(Object.entries(edits).filter(([, v]) => v !== undefined)) as Record<string, DBCell>);
  const nothing = Object.keys(values).length === 0;

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onCancel()}
      title={editKey ? t.editTitle(table) : t.addTitle(table)}
      description={editKey ? t.formHint : t.addHint}
      confirmLabel={busy ? t.saving : editKey ? t.save : t.add}
      onConfirm={() => {
        if (ready && !(editKey && nothing)) onSubmit(values);
      }}
      busy={busy || !ready || (editKey !== null && nothing)}
    >
      {editKey && row.isPending && <p className="m-0 text-muted">{t.loadingRow}</p>}
      {editKey && row.isError && (
        <Notice kind="fail" role="alert">
          {errorMessage(row.error, lang)}
        </Notice>
      )}
      {ready && (
        <div className="flex max-h-[60vh] flex-col gap-[14px] overflow-y-auto pr-[4px]">
          {columns.map((c, i) => {
            const cur = edits[c.name] !== undefined ? (edits[c.name] as DBCell) : editKey ? (before[i] ?? null) : null;
            const touched = edits[c.name] !== undefined;
            const isNull = cur === null && (editKey !== null || touched);
            const text = cur ?? "";
            const big = text.length > maxCell;
            const long = /text|json|blob/i.test(c.type) || text.length > 80 || text.includes("\n");
            const id = `db-o-${i}`;
            const set = (v: DBCell) => setEdits((e) => ({ ...e, [c.name]: v }));
            return (
              <Field key={c.name} label={c.name} htmlFor={id} hint={big ? t.tooBig : `${c.type}${c.key === "PRI" ? " · PRIMARY KEY" : ""}${c.extra ? ` · ${c.extra}` : ""}${c.binary ? ` · ${t.binaryHint}` : ""}`} hintId={`${id}-goi-y`}>
                <div className="flex flex-col gap-[6px]">
                  {long ? (
                    <Textarea id={id} mono rows={4} value={text} disabled={isNull || big} onChange={(e) => set(e.target.value)} aria-describedby={`${id}-goi-y`} spellCheck={false} />
                  ) : (
                    <Input id={id} mono value={text} disabled={isNull || big} onChange={(e) => set(e.target.value)} aria-describedby={`${id}-goi-y`} autoComplete="off" spellCheck={false} />
                  )}
                  {c.nullable && (
                    <label className="flex items-center gap-[8px] text-[13px]">
                      <Checkbox label={t.setNullFor(c.name)} checked={isNull} onChange={(e) => set(e.target.checked ? null : "")} />
                      <span>{t.setNull}</span>
                    </label>
                  )}
                </div>
              </Field>
            );
          })}
        </div>
      )}
      {error !== null && (
        <div className="pt-[12px]">
          <Notice kind="fail" role="alert">
            {error}
          </Notice>
        </div>
      )}
    </Dialog>
  );
}
