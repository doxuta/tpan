// Hộp hỏi lại có mật khẩu và mã (tắt hai bước, tạo lại mã dự phòng): mật khẩu và
// mã chỉ nằm trong state của hộp, mất khi hộp đóng. Hộp chỉ được dựng khi đang mở,
// nên ô nhập tự về trống mỗi lần mở. Việc thật do màn gọi truyền vào qua run
// (mutateAsync của hook); thành công thì onDone nhận kết quả và màn đóng hộp.

import { useState, type FormEvent } from "react";
import type { MFAConfirm } from "../../api/m20-types";
import { Dialog } from "../../components/Dialog";
import { TextField } from "../../components/Field";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/tai-khoan";
import { MfaError } from "./MfaError";
import { cleanCode, codeReady } from "./model";

type Props<R> = {
  title: string;
  description: string;
  confirmLabel: string;
  user: string;
  run: (b: MFAConfirm) => Promise<R>;
  onDone: (result: R) => void;
  onClose: () => void;
};

export function ConfirmCodeDialog<R>({ title, description, confirmLabel, user, run, onDone, onClose }: Props<R>) {
  const t = useStrings(strings);
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const ready = password !== "" && codeReady(code);

  const submit = async () => {
    if (busy || !ready) return;
    setBusy(true);
    setError(null);
    try {
      onDone(await run({ password, code: cleanCode(code) }));
    } catch (err) {
      setError(err);
      setCode("");
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={title} description={description} confirmLabel={busy ? t.working : confirmLabel} onConfirm={() => void submit()} busy={busy || !ready}>
      <form
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          void submit();
        }}
        className="flex flex-col gap-[14px]"
      >
        <TextField id="cc-mat-khau" type="password" label={t.passwordLabel} autoComplete="current-password" autoFocus value={password} onChange={(e) => setPassword(e.target.value)} />
        <TextField
          id="cc-ma"
          label={t.confirmCodeLabel}
          hint={t.confirmCodeHint}
          autoComplete="one-time-code"
          autoCapitalize="none"
          spellCheck={false}
          mono
          value={code}
          onChange={(e) => setCode(e.target.value)}
          invalid={error !== null}
        />
        {error !== null && <MfaError error={error} user={user} />}
        {/* Nút gửi ẩn để bấm Enter trong ô nhập là gửi (nút thật của hộp nằm ngoài form). */}
        <button type="submit" tabIndex={-1} aria-hidden="true" className="sr-only" />
      </form>
    </Dialog>
  );
}
