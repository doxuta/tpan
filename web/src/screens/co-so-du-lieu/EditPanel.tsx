// Khối "Sửa" của một DB, hiện dưới bảng (như khối chọn bản nhập ImportPicker):
//
//   - Bộ ký tự (charset và collation): chọn cặp từ danh sách máy chủ cho phép,
//     "Xem trước" gọi ?dry_run=1 rồi PlanReview (việc của màn). Cả DB rời lẫn DB
//     của site đều có phần này.
//   - Ghi chú (chỉ DB rời): lưu ngay bằng POST /api/v1/dbs/{name} {note}, không
//     qua xem trước vì nó chỉ là chữ, không đổi gì trên máy.
//
// Hai phần là hai việc riêng, mỗi phần một nút: gộp một nút thì "ghi chú lưu
// ngay" và "charset có xem trước" lẫn vào nhau.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { post } from "../../api/client";
import { dbPath, useCollations } from "../../api/m13-dbs";
import type { DatabaseRow, DBEdit } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { CharsetPicker } from "../../components/CharsetPicker";
import { Field, Input } from "../../components/Field";
import { MonoText } from "../../components/MonoText";
import type { usePreviewedOp } from "../../components/PreviewedOp";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/co-so-du-lieu";
import { strings as chung } from "../../i18n/m13-chung";
import { ErrorNote } from "../site/ErrorNote";
import { databasesKey, rowOp } from "./model";

/** Ghi chú tối đa 500 byte (máy chủ: note_invalid). Đếm byte chứ không đếm ký tự: tiếng Việt có dấu tốn 2 đến 3 byte. */
export const noteMaxBytes = 500;
export const noteTooLong = (note: string): boolean => new TextEncoder().encode(note).length > noteMaxBytes;

type Props = {
  row: DatabaseRow;
  op: ReturnType<typeof usePreviewedOp>;
  onClose: () => void;
};

export function EditPanel({ row, op, onClose }: Props) {
  const t = useStrings(strings);
  const m = useStrings(chung);
  const qc = useQueryClient();
  const standalone = row.kind === "standalone";
  const collations = useCollations();

  const [pair, setPair] = useState({ charset: row.charset, collation: row.collation });
  const unchanged = pair.charset === row.charset && pair.collation === row.collation;
  const [note, setNote] = useState(row.note ?? "");
  const noteBad = noteTooLong(note);

  const saveNote = useMutation({
    mutationFn: () => post<DatabaseRow>(dbPath(row.db), { note: note.trim() } satisfies DBEdit),
    onSuccess: () => void qc.invalidateQueries({ queryKey: databasesKey }),
  });

  return (
    <Card
      title={standalone ? t.editHeading(row.db) : t.charsetHeading(row.domain)}
      titleId="sua-db"
      actions={<Button onClick={onClose}>{t.editClose}</Button>}
    >
      <div className="flex flex-col gap-[16px]">
        <MonoText>{row.db}</MonoText>

        <div className="flex flex-col gap-[12px]">
          <p className="m-0 max-w-[72ch] text-[13px] text-muted">{t.charsetIntro}</p>
          {collations.isPending && <p className="m-0 text-muted">{t.collationsLoading}</p>}
          {collations.isError && <p className="m-0">{t.collationsFailed}</p>}
          {collations.isSuccess && (
            <>
              <CharsetPicker collations={collations.data} charset={pair.charset} collation={pair.collation} onChange={(charset, collation) => setPair({ charset, collation })} />
              <div className="flex flex-wrap items-center gap-x-[14px] gap-y-[8px]">
                <Button
                  variant="primary"
                  disabled={op.pending || unchanged}
                  aria-describedby={unchanged ? "doi-charset-giong" : undefined}
                  onClick={() => void op.ask(rowOp(t, "charset", row, pair))}
                >
                  {op.busy(`charset:${row.db}`) ? m.previewing : t.charsetPreview}
                </Button>
                {unchanged && (
                  <span id="doi-charset-giong" className="text-[13px] text-muted">
                    {t.charsetSame}
                  </span>
                )}
              </div>
            </>
          )}
        </div>

        {standalone && (
          <div className="flex max-w-[560px] flex-col gap-[10px] border-t border-hair pt-[16px]">
            <Field label={t.noteHeading} htmlFor="sua-ghi-chu" hint={t.noteIntro} hintId="sua-ghi-chu-goi-y" error={noteBad ? t.noteTooLong : undefined}>
              <Input
                id="sua-ghi-chu"
                value={note}
                onChange={(e) => {
                  setNote(e.target.value);
                  saveNote.reset();
                }}
                aria-describedby="sua-ghi-chu-goi-y"
                aria-invalid={noteBad ? true : undefined}
                autoComplete="off"
              />
            </Field>
            <div className="flex flex-wrap items-center gap-x-[14px] gap-y-[8px]">
              <Button disabled={saveNote.isPending || noteBad || note.trim() === (row.note ?? "")} onClick={() => saveNote.mutate()}>
                {saveNote.isPending ? t.noteSaving : t.noteSave}
              </Button>
              <span aria-live="polite" className="text-[13px] text-muted">
                {saveNote.isSuccess ? t.noteSaved : ""}
              </span>
            </div>
            {saveNote.isError && <ErrorNote error={saveNote.error} heading={t.noteFailed} checksHeading={m.checksHeading} />}
          </div>
        )}
      </div>
    </Card>
  );
}
