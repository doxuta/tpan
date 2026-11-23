// Thẻ "Token API" của trang Tài khoản (neo #token): GET /api/v1/account/tokens →
// MyTokens {tokens, max_days, groups}. Nút "Tạo token" mở hộp; token vừa tạo hiện
// MỘT lần ở đầu thẻ (state của chính màn này, không vào cache). Khối đó không dựng
// có điều kiện theo truy vấn: danh sách tự đọc lại cũng không làm nó biến mất.

import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useMyTokens } from "../../api/m20";
import type { TokenCreated } from "../../api/m20-types";
import { useWho } from "../../api/m13-users";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/tai-khoan";
import { TokenCreateDialog, TokenReveal } from "./TokenCreate";
import { TokenTable } from "./TokenTable";

export function Token() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const { name } = useWho();
  const q = useMyTokens();
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<TokenCreated | null>(null);

  return (
    <>
      {created && <TokenReveal created={created} onDismiss={() => setCreated(null)} />}
      {q.isPending && (
        <Card id="token" title={t.tokHeading} titleId="token-tieu-de">
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {q.isError && (
        <Card id="token" title={t.tokHeading} titleId="token-tieu-de">
          <p className="mt-0 mb-[12px]">
            {t.tokLoadFailed} {errorMessage(q.error, lang)}
          </p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {q.isSuccess && (
        <>
          <TokenTable
            id="token"
            scope="mine"
            title={t.tokHeading}
            titleId="token-tieu-de"
            caption={t.tokCaption}
            rows={q.data.tokens}
            above={<p className="m-0 max-w-[72ch] px-[16px] py-[12px] text-muted">{t.tokIntro}</p>}
            actions={
              <Button variant="primary" icon="key" onClick={() => setCreating(true)}>
                {t.tokCreate}
              </Button>
            }
          />
          {creating && <TokenCreateDialog data={q.data} user={name} onClose={() => setCreating(false)} onCreated={setCreated} />}
        </>
      )}
    </>
  );
}
