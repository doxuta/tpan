// Hộp "Tạo DB" (DB rời, mục 6.12 của KE-HOACH.md). Admin chọn chủ (mặc định
// chính mình), user thường không thấy ô này. Tên DB thật là "<chủ>_<tên>": ô
// nhập chỉ là phần sau, kèm dòng xem trước tên đầy đủ. Bộ ký tự chọn từ danh
// sách máy chủ cho phép. "Xem trước" gọi POST /api/v1/dbs?dry_run=1; có bản xem
// trước thì hộp đóng và PlanReview hiện dưới bảng (việc của màn). Lỗi (tên sai,
// vượt gói, tên đã có...) hiện ngay trong hộp để sửa ô sai mà không mất chữ đã gõ.
// Mật khẩu của DB mới không hiện ở đây: nó hiện một lần ở trang job (DbSecretBox).

import { useEffect, useState } from "react";
import { dbsPath, useCollations, useIdentity } from "../../api/m13-dbs";
import { useUsers } from "../../api/m13-users";
import type { DBCreate } from "../../api/types";
import { CharsetPicker, defaultPair } from "../../components/CharsetPicker";
import { Dialog } from "../../components/Dialog";
import { Field, Input, Select } from "../../components/Field";
import { Code } from "../../components/MonoText";
import type { usePreviewedOp } from "../../components/PreviewedOp";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/co-so-du-lieu";
import { ErrorNote } from "../site/ErrorNote";
import { noteTooLong } from "./EditPanel";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  op: ReturnType<typeof usePreviewedOp>;
};

export function CreateDialog({ open, onOpenChange, op }: Props) {
  const t = useStrings(strings);
  const { isAdmin, name: me } = useIdentity();
  const collations = useCollations(open);
  const users = useUsers(isAdmin && open);

  const [ownerSel, setOwnerSel] = useState("");
  const [name, setName] = useState("");
  const [note, setNote] = useState("");
  const [picked, setPicked] = useState<{ charset: string; collation: string } | null>(null);
  const [missing, setMissing] = useState(false);

  // Mở lại là bắt đầu lại, không giữ chữ của lần trước.
  useEffect(() => {
    if (open) {
      op.close();
      setOwnerSel("");
      setName("");
      setNote("");
      setPicked(null);
      setMissing(false);
    }
  }, [open]);

  const owner = ownerSel || me;
  const pair = picked ?? defaultPair(collations.data ?? []);
  const prefix = `${owner}_`;
  const full = `${prefix}${name.trim()}`;
  const noteBad = noteTooLong(note);
  // Chỉ user đang hoạt động mới nhận DB mới (máy chủ từ chối user bị khoá).
  const owners = (users.data ?? []).filter((u) => u.user.status === "active").map((u) => u.user.name);

  async function submit() {
    if (name.trim() === "") {
      setMissing(true);
      document.getElementById("db-ten")?.focus();
      return;
    }
    if (noteBad) return;
    const body: DBCreate = {
      name: name.trim(),
      ...(isAdmin && owner !== me ? { owner } : {}),
      ...(pair.charset ? { charset: pair.charset, collation: pair.collation } : {}),
      ...(note.trim() ? { note: note.trim() } : {}),
    };
    const ok = await op.ask({ id: "create", method: "POST", path: dbsPath, body, warning: t.createWarning(full), errorHeading: t.createErrorHeading }, true);
    if (ok) onOpenChange(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange} title={t.createTitle} description={t.createDescription} confirmLabel={t.createPreview} onConfirm={() => void submit()} busy={op.pending}>
      <div className="flex flex-col gap-[16px]">
        {isAdmin && (
          <Field label={t.createOwnerLabel} htmlFor="db-chu" hint={t.createOwnerHint} hintId="db-chu-goi-y">
            <Select id="db-chu" value={owner} onChange={(e) => setOwnerSel(e.target.value)} aria-describedby="db-chu-goi-y">
              {/* Danh sách chưa tải xong hoặc lỗi: vẫn có đúng một lựa chọn là chính mình, nên tạo DB cho mình không bị chặn. */}
              {!owners.includes(me) && <option value={me}>{t.createOwnerSelf(me)}</option>}
              {owners.map((n) => (
                <option key={n} value={n}>
                  {n === me ? t.createOwnerSelf(n) : n}
                </option>
              ))}
            </Select>
          </Field>
        )}

        {/* Dòng "Tên đầy đủ" nằm trong phần gợi ý để ô nhập đọc cả hai khi vào ô. */}
        <Field
          label={t.createNameLabel}
          htmlFor="db-ten"
          hintId="db-ten-goi-y"
          hint={
            <>
              <span className="block">{t.createNameHint(prefix)}</span>
              <span className="mt-[4px] block">
                {t.createFullName} <Code>{full}</Code> · {t.createFullNameLength(full.length)}
              </span>
            </>
          }
          error={missing ? t.createNameRequired : undefined}
        >
          <Input
            id="db-ten"
            mono
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              setMissing(false);
            }}
            aria-describedby="db-ten-goi-y"
            aria-invalid={missing ? true : undefined}
            autoComplete="off"
            spellCheck={false}
            autoCapitalize="none"
            placeholder="shop"
          />
        </Field>

        {collations.isPending && <p className="m-0 text-muted">{t.collationsLoading}</p>}
        {collations.isError && <p className="m-0">{t.collationsFailed}</p>}
        {collations.isSuccess && <CharsetPicker collations={collations.data} charset={pair.charset} collation={pair.collation} onChange={(charset, collation) => setPicked({ charset, collation })} />}

        <Field label={t.noteLabel} htmlFor="db-ghi-chu" hint={t.createNoteHint} hintId="db-ghi-chu-goi-y" error={noteBad ? t.noteTooLong : undefined}>
          <Input id="db-ghi-chu" value={note} onChange={(e) => setNote(e.target.value)} aria-describedby="db-ghi-chu-goi-y" aria-invalid={noteBad ? true : undefined} autoComplete="off" />
        </Field>

        {op.quietError !== null && <ErrorNote error={op.quietError} heading={t.createErrorHeading} checksHeading={t.checksHeading} />}
      </div>
    </Dialog>
  );
}
