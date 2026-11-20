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
import type { usePreviewedOp } from "../../components/PreviewedOp";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/co-so-du-lieu";
import { ErrorNote } from "../site/ErrorNote";
import { noteTooLong } from "./EditPanel";

const inputClass =
  "box-border h-[44px] w-full border border-ink bg-surface px-[10px] text-[14px] text-ink placeholder:text-muted disabled:border-hair disabled:text-muted sm:h-[40px]";

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
          <div>
            <label htmlFor="db-chu" className="mb-[4px] block font-medium">
              {t.createOwnerLabel}
            </label>
            <p className="mt-0 mb-[6px] text-[13px] text-muted">{t.createOwnerHint}</p>
            <select id="db-chu" value={owner} onChange={(e) => setOwnerSel(e.target.value)} className={`${inputClass} font-mono text-[13px]`}>
              {/* Danh sách chưa tải xong hoặc lỗi: vẫn có đúng một lựa chọn là chính mình, nên tạo DB cho mình không bị chặn. */}
              {!owners.includes(me) && <option value={me}>{t.createOwnerSelf(me)}</option>}
              {owners.map((n) => (
                <option key={n} value={n}>
                  {n === me ? t.createOwnerSelf(n) : n}
                </option>
              ))}
            </select>
          </div>
        )}

        <div>
          <label htmlFor="db-ten" className="mb-[4px] block font-medium">
            {t.createNameLabel}
          </label>
          <p id="db-ten-goi-y" className="mt-0 mb-[6px] text-[13px] text-muted">
            {t.createNameHint(prefix)}
          </p>
          <div className="flex items-stretch">
            <span className="flex flex-none items-center border border-r-0 border-ink bg-pre px-[10px] font-mono text-[13px] text-muted" aria-hidden="true">
              {prefix}
            </span>
            <input
              id="db-ten"
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                setMissing(false);
              }}
              aria-describedby="db-ten-goi-y db-ten-day-du"
              aria-invalid={missing ? true : undefined}
              autoComplete="off"
              spellCheck={false}
              autoCapitalize="none"
              placeholder="shop"
              className={`${inputClass} min-w-0 font-mono text-[13px]`}
            />
          </div>
          <p id="db-ten-day-du" className="mt-[6px] mb-0 text-[13px] text-muted">
            {t.createFullName} <span className="font-mono text-ink">{full}</span> · {t.createFullNameLength(full.length)}
          </p>
          {missing && (
            <p role="alert" className="mt-[6px] mb-0 font-medium">
              {t.createNameRequired}
            </p>
          )}
        </div>

        {collations.isPending && <p className="m-0 text-muted">{t.collationsLoading}</p>}
        {collations.isError && <p className="m-0">{t.collationsFailed}</p>}
        {collations.isSuccess && <CharsetPicker collations={collations.data} charset={pair.charset} collation={pair.collation} onChange={(charset, collation) => setPicked({ charset, collation })} />}

        <div>
          <label htmlFor="db-ghi-chu" className="mb-[4px] block font-medium">
            {t.noteLabel}
          </label>
          <p id="db-ghi-chu-goi-y" className="mt-0 mb-[6px] text-[13px] text-muted">
            {t.createNoteHint}
          </p>
          <input id="db-ghi-chu" value={note} onChange={(e) => setNote(e.target.value)} aria-describedby="db-ghi-chu-goi-y" aria-invalid={noteBad ? true : undefined} autoComplete="off" className={inputClass} />
          {noteBad && (
            <p role="alert" className="mt-[6px] mb-0 font-medium">
              {t.noteTooLong}
            </p>
          )}
        </div>

        {op.quietError !== null && <ErrorNote error={op.quietError} heading={t.createErrorHeading} checksHeading={t.checksHeading} />}
      </div>
    </Dialog>
  );
}
