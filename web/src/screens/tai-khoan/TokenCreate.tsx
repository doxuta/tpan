// Hộp "Tạo token API" và khối hiện token MỘT lần (M20). Hộp thu: tên, bảng chọn
// phạm vi (nhóm × đọc, ghi), số ngày, mật khẩu, và mã khi user đã bật hai bước
// (thiếu thì máy chủ trả 400 mfa_code_required). Tạo xong hộp đóng và màn gọi
// giữ TokenCreated trong state để vẽ TokenReveal; token không vào cache hay
// localStorage, và biến mất khi người dùng bấm "Đã lưu token" hoặc rời trang.

import { useEffect, useId, useRef, useState } from "react";
import { ApiError } from "../../api/client";
import { useCreateToken, useMFA } from "../../api/m20";
import type { MyTokens, TokenCreated, TokenGroup } from "../../api/m20-types";
import { Button } from "../../components/Button";
import { cardClass } from "../../components/Card";
import { Dialog } from "../../components/Dialog";
import { Checkbox, TextField } from "../../components/Field";
import { Notice } from "../../components/Notice";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/tai-khoan";
import { CopyBox } from "./CopyBox";
import { MfaError } from "./MfaError";
import { cleanCode, curlExample, defaultDays, fmtDate, scopesOf, setRead, setWrite, tokenProblem, type Level } from "./model";

type Pick = Partial<Record<TokenGroup, Level>>;

export function TokenCreateDialog({ data, user, onClose, onCreated }: { data: MyTokens; user: string; onClose: () => void; onCreated: (c: TokenCreated) => void }) {
  const t = useStrings(strings);
  const mfa = useMFA();
  const create = useCreateToken();
  const [name, setName] = useState("");
  const [pick, setPick] = useState<Pick>({});
  const [days, setDays] = useState(String(defaultDays(data.max_days)));
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [local, setLocal] = useState<string | null>(null);

  // Máy chủ đòi mã dù ta chưa biết user đã bật hai bước (truy vấn chưa về): hiện ô mã.
  const codeAsked = create.error instanceof ApiError && create.error.code === "mfa_code_required";
  const needCode = mfa.data?.enabled === true || codeAsked;
  const scopes = scopesOf(data.groups, pick);

  const submit = async () => {
    if (create.isPending) return;
    create.reset();
    const p = tokenProblem({ name, scopes, days, maxDays: data.max_days, password, needCode, code });
    if (p) {
      setLocal(p === "days" ? t.problem.days(data.max_days) : t.problem[p]);
      return;
    }
    setLocal(null);
    try {
      const made = await create.mutateAsync({ name: name.trim(), scopes, days: Number(days), password, ...(needCode && { code: cleanCode(code) }) });
      onCreated(made);
      onClose();
    } catch {
      // Lỗi hiện qua create.error; mã đã dùng hay sai thì gõ lại.
      setCode("");
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={t.createTitle}
      description={t.createDesc(data.max_days)}
      confirmLabel={create.isPending ? t.creating : t.createConfirm}
      onConfirm={() => void submit()}
      busy={create.isPending}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
        className="flex flex-col gap-[14px]"
      >
        <TextField id="tk-ten" label={t.fName} hint={t.fNameHint} maxLength={64} autoComplete="off" autoFocus value={name} onChange={(e) => setName(e.target.value)} />

        <fieldset className="m-0 min-w-0 border-0 p-0">
          <legend className="mb-[6px] p-0 text-[14px] font-medium">{t.fScopes}</legend>
          <div className="grid grid-cols-[1fr_auto_auto] items-center gap-x-[16px] gap-y-[2px] rounded-ctl border border-hair px-[12px] py-[8px]">
            <span className="text-[12px] font-medium text-muted">{t.scopeGroupCol}</span>
            <span className="text-[12px] font-medium text-muted">{t.scopeRead}</span>
            <span className="text-[12px] font-medium text-muted">{t.scopeWrite}</span>
            {data.groups.map((g) => {
              const level = pick[g] ?? "none";
              const info = t.groups[g];
              return (
                <ScopeRow
                  key={g}
                  name={info.name}
                  desc={info.desc}
                  read={level !== "none"}
                  write={level === "write"}
                  readLabel={t.scopeCell(info.name, t.scopeRead)}
                  writeLabel={t.scopeCell(info.name, t.scopeWrite)}
                  onRead={(on) => setPick((p) => ({ ...p, [g]: setRead(level, on) }))}
                  onWrite={(on) => setPick((p) => ({ ...p, [g]: setWrite(on) }))}
                />
              );
            })}
          </div>
          <p className="mt-[6px] mb-0 text-[13px] text-muted">{t.fScopesHint}</p>
        </fieldset>

        <TextField id="tk-ngay" type="number" inputMode="numeric" min={1} max={data.max_days} step={1} label={t.fDays} hint={t.fDaysHint(data.max_days)} value={days} onChange={(e) => setDays(e.target.value)} />
        <TextField id="tk-mat-khau" type="password" label={t.fPassword} hint={t.fPasswordHint} autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        {needCode && (
          <TextField id="tk-ma" label={t.fCode} hint={t.confirmCodeHint} autoComplete="one-time-code" autoCapitalize="none" spellCheck={false} mono value={code} onChange={(e) => setCode(e.target.value)} />
        )}

        {local && (
          <Notice kind="fail" role="alert">
            {local}
          </Notice>
        )}
        {create.isError && <MfaError error={create.error} user={user} />}
        <button type="submit" tabIndex={-1} aria-hidden="true" className="sr-only" />
      </form>
    </Dialog>
  );
}

function ScopeRow({ name, desc, read, write, readLabel, writeLabel, onRead, onWrite }: { name: string; desc: string; read: boolean; write: boolean; readLabel: string; writeLabel: string; onRead: (on: boolean) => void; onWrite: (on: boolean) => void }) {
  return (
    <>
      <span className="min-w-0 py-[4px]">
        <span className="block font-medium">{name}</span>
        <span className="block text-[12.5px] text-muted">{desc}</span>
      </span>
      <span className="flex justify-center">
        <Checkbox label={readLabel} checked={read} onChange={(e) => onRead(e.target.checked)} />
      </span>
      <span className="flex justify-center">
        <Checkbox label={writeLabel} checked={write} onChange={(e) => onWrite(e.target.checked)} />
      </span>
    </>
  );
}

/** Token vừa tạo, hiện một lần: chép được, kèm ví dụ gọi API. */
export function TokenReveal({ created, onDismiss }: { created: TokenCreated; onDismiss: () => void }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const ref = useRef<HTMLElement>(null);
  const headingId = useId();
  // Token về khi người dùng đang ở giữa trang: đưa khối vào tầm mắt và cho trình đọc màn hình nhảy tới.
  useEffect(() => {
    ref.current?.scrollIntoView({ block: "start" });
    ref.current?.focus({ preventScroll: true });
  }, []);
  return (
    <section ref={ref} tabIndex={-1} aria-labelledby={headingId} className={`${cardClass} p-[16px] outline-offset-4`}>
      <div className="text-[12px] font-medium text-muted">{t.revealLabel}</div>
      <h2 id={headingId} className="m-0 mt-[4px] text-[16px] font-semibold [overflow-wrap:anywhere]">
        {t.revealTitle(created.row.name)}
      </h2>
      <p className="mt-[6px] mb-0 max-w-[72ch]">
        {t.revealBody} {t.revealExpires(fmtDate(created.row.expires, lang))}
      </p>
      <div className="mt-[14px] flex flex-col gap-[14px]">
        <div>
          <div className="mb-[6px] text-[12px] font-medium text-muted">{t.revealToken}</div>
          <CopyBox text={created.token} buttonLabel={t.revealCopy} label={`${t.revealCopy}: ${created.row.name}`} />
        </div>
        <div>
          <div className="mb-[6px] text-[12px] font-medium text-muted">{t.revealExample}</div>
          <CopyBox text={curlExample(window.location.origin, created.token)} buttonLabel={t.revealCopyExample} label={t.revealCopyExample} />
        </div>
      </div>
      <div className="mt-[14px]">
        <Button variant="primary" onClick={onDismiss}>
          {t.revealDismiss}
        </Button>
      </div>
    </section>
  );
}
