// Luồng bật đăng nhập hai bước (M20), dùng chung cho thẻ ở trang Tài khoản và
// cho trang /bat-hai-buoc: "Bật" → POST /account/2fa/setup (QR, chuỗi bí mật, một
// lần) → người dùng gõ mã 6 số và mật khẩu → POST /account/2fa/enable → mười mã
// dự phòng, giao cho màn gọi qua onEnabled (màn giữ và hiện bằng RecoveryCodes).
//
// Bí mật (chuỗi TOTP, mật khẩu, mã) chỉ ở state của component này và mất khi nó
// bị gỡ khỏi cây (xong, hoặc bấm Huỷ). Chỉ dùng hook thường của m20.ts, không
// dùng thứ gì cần provider của khung (job, AI): trang /bat-hai-buoc nằm ngoài khung.

import { useState, type FormEvent } from "react";
import { useMFAEnable, useMFASetup } from "../../api/m20";
import type { MFASetup } from "../../api/m20-types";
import { Button } from "../../components/Button";
import { TextField } from "../../components/Field";
import { QrCode } from "../../components/QrCode";
import { Code } from "../../components/MonoText";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/tai-khoan";
import { CopySecret } from "./CopyBox";
import { MfaError } from "./MfaError";
import { cleanTotp, groupSecret, totpComplete } from "./model";

type Props = {
  /** Tên đăng nhập, để lỗi nói đúng lệnh `sudo tpan user 2fa reset <tên>`. */
  user: string;
  /** Mười mã dự phòng vừa tạo; sau lời gọi này luồng không còn giữ bí mật nào. */
  onEnabled: (codes: string[]) => void;
};

export function EnrollFlow({ user, onEnabled }: Props) {
  const t = useStrings(strings);
  const start = useMFASetup();
  const enable = useMFAEnable();
  const [setup, setSetup] = useState<MFASetup | null>(null);
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");

  const begin = async () => {
    setCode("");
    setPassword("");
    enable.reset();
    try {
      setSetup(await start.mutateAsync());
    } catch {
      // Lỗi hiện qua start.error.
    }
  };
  const discard = () => {
    setSetup(null);
    setCode("");
    setPassword("");
    start.reset();
    enable.reset();
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (enable.isPending || !totpComplete(code) || password === "") return;
    try {
      const done = await enable.mutateAsync({ password, code: cleanTotp(code) });
      onEnabled(done.recovery_codes);
    } catch {
      // Lỗi hiện qua enable.error; giữ mã QR để người dùng gõ lại, chỉ xoá ô mã.
      setCode("");
    }
  };

  if (setup === null) {
    return (
      <div className="flex flex-col gap-[12px]">
        <div>
          <Button variant="primary" icon="smartphone" onClick={() => void begin()} disabled={start.isPending}>
            {start.isPending ? t.starting : t.tsStart}
          </Button>
        </div>
        {start.isError && <MfaError error={start.error} user={user} />}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-[20px]">
      <div>
        <h3 className="m-0 mb-[10px] text-[14px] font-semibold">{t.setupScan}</h3>
        <div className="flex flex-wrap items-start gap-[20px]">
          <QrCode rows={setup.qr} label={t.qrLabel} />
          <div className="flex min-w-0 flex-[1_1_240px] flex-col gap-[10px]">
            <p className="m-0 text-muted">{t.setupManual}</p>
            <CopySecret raw={setup.secret} shown={groupSecret(setup.secret)} label={t.setupSecretLabel} />
            <dl className="m-0 flex flex-col gap-[4px] text-[13px]">
              <div className="flex gap-[8px]">
                <dt className="flex-[0_0_96px] text-muted">{t.setupIssuer}</dt>
                <dd className="m-0 min-w-0">{setup.issuer}</dd>
              </div>
              <div className="flex gap-[8px]">
                <dt className="flex-[0_0_96px] text-muted">{t.setupAccount}</dt>
                <dd className="m-0 min-w-0">
                  <Code>{setup.account}</Code>
                </dd>
              </div>
            </dl>
          </div>
        </div>
      </div>

      <form onSubmit={submit} className="flex max-w-[420px] flex-col gap-[16px]">
        <h3 className="m-0 text-[14px] font-semibold">{t.setupConfirm}</h3>
        <TextField
          id="hb-ma"
          label={t.codeLabel}
          hint={t.codeHint}
          inputMode="numeric"
          autoComplete="one-time-code"
          autoCapitalize="none"
          spellCheck={false}
          mono
          value={code}
          onChange={(e) => setCode(e.target.value)}
          invalid={enable.isError}
        />
        <TextField id="hb-mat-khau" type="password" label={t.passwordLabel} autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        {enable.isError && <MfaError error={enable.error} user={user} />}
        <div className="flex flex-wrap gap-[8px]">
          <Button type="submit" variant="primary" disabled={enable.isPending || !totpComplete(code) || password === ""}>
            {enable.isPending ? t.enabling : t.enableBtn}
          </Button>
          <Button onClick={discard} disabled={enable.isPending}>
            {t.setupDiscard}
          </Button>
        </div>
      </form>
    </div>
  );
}
