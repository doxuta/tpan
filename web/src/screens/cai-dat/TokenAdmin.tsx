// Thẻ "Token API" của màn Cài đặt (admin, M20): mức tối đa số ngày của token mới
// (POST /api/v1/settings/auth {token_max_days}) và bảng token của mọi người dùng
// (GET /api/v1/tokens), có cột Chủ và thu hồi (DELETE /api/v1/tokens/{id}).

import { useState, type FormEvent } from "react";
import { errorMessage } from "../../api/client";
import { useAllTokens, useAuthSettings, useSaveAuthSettings } from "../../api/m20";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { TextField } from "../../components/Field";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/cai-dat";
import { strings as khung } from "../../i18n/khung";
import { strings as tk } from "../../i18n/tai-khoan";
import { TokenTable } from "../tai-khoan/TokenTable";
import { ErrorNote } from "../site/ErrorNote";
import { Note } from "./Note";

export function TokenAdmin() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const a = useStrings(tk);
  const { lang } = useLang();
  const settings = useAuthSettings();
  const all = useAllTokens();
  const save = useSaveAuthSettings();
  const [days, setDays] = useState<string | null>(null);
  const [bad, setBad] = useState(false);
  const [justSaved, setJustSaved] = useState(false);

  const current = settings.data?.token_max_days;
  const shown = days ?? (current === undefined ? "" : String(current));
  const dirty = current !== undefined && shown !== String(current);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const n = Number(shown);
    if (shown.trim() === "" || !Number.isInteger(n) || n < 1 || n > 365) {
      setBad(true);
      return;
    }
    setBad(false);
    save.mutate(
      { token_max_days: n },
      {
        onSuccess: () => {
          setDays(null);
          setJustSaved(true);
        },
      },
    );
  };

  return (
    <>
      <Card title={t.tkHeading} titleId="token-admin-tieu-de">
        <p className="mt-0 mb-[14px] max-w-[72ch] text-muted">{t.tkIntro}</p>
        {settings.isPending ? (
          <p className="m-0 text-muted">{k.loading}</p>
        ) : settings.isError ? (
          <div className="flex flex-col gap-[12px]">
            <p className="m-0">{errorMessage(settings.error, lang)}</p>
            <div>
              <Button onClick={() => void settings.refetch()}>{k.retry}</Button>
            </div>
          </div>
        ) : (
          <form onSubmit={submit} noValidate className="flex max-w-[420px] flex-col gap-[16px]">
            <TextField
              id="tk-toi-da"
              type="number"
              inputMode="numeric"
              min={1}
              max={365}
              step={1}
              label={t.tkDays}
              hint={t.tkDaysHint}
              value={shown}
              onChange={(e) => {
                setDays(e.target.value);
                setBad(false);
                setJustSaved(false);
                save.reset();
              }}
              invalid={bad}
            />
            {bad && <Note kind="fail">{t.tkBad}</Note>}
            {save.isError && <ErrorNote error={save.error} />}
            <div className="flex flex-wrap items-center gap-x-[16px] gap-y-[8px]">
              <Button type="submit" variant="primary" disabled={!dirty || save.isPending}>
                {save.isPending ? t.tkSaving : t.tkSave}
              </Button>
              <div aria-live="polite">{justSaved && !dirty && <Note kind="pass">{t.tkSaved}</Note>}</div>
            </div>
          </form>
        )}
      </Card>
      {all.isError && (
        <Card title={t.tkAllTitle} titleId="token-moi-nguoi-tieu-de">
          <p className="mt-0 mb-[12px]">
            {a.tokLoadFailed} {errorMessage(all.error, lang)}
          </p>
          <Button onClick={() => void all.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {all.isSuccess && <TokenTable scope="all" title={t.tkAllTitle} titleId="token-moi-nguoi-tieu-de" caption={a.tokCaptionAll} rows={all.data} />}
    </>
  );
}
