// Thẻ "Tên miền của panel" của màn Cài đặt (admin, M20). GET /api/v1/panel cho tên
// đang đặt và phép kiểm chứng chỉ. Đặt tên: POST /api/v1/panel/domain?dry_run=1
// dựng bản xem trước (PlanReview) giống màn SSL, bấm chạy thì ra job và thẻ theo
// dõi job báo tiến độ. Xoá tên là cùng lời gọi với domain rỗng. Lời nhắc: tên miền
// hiện công khai trong nhật ký Certificate Transparency; vào bằng IP vẫn được.

import { useState, type FormEvent } from "react";
import { ApiError, errorMessage } from "../../api/client";
import { panelKey, usePanel } from "../../api/m20";
import { panelDomainPath } from "../../api/m20-types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { TextField } from "../../components/Field";
import { Notice } from "../../components/Notice";
import { usePreviewedOp } from "../../components/PreviewedOp";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/cai-dat";
import { strings as khung } from "../../i18n/khung";
import { CheckList } from "../site/CheckItem";
import { ErrorNote } from "../site/ErrorNote";

export function TenMien() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = usePanel();
  const op = usePreviewedOp([panelKey]);
  const [domain, setDomain] = useState("");
  const [empty, setEmpty] = useState(false);

  const ask = (id: "set" | "clear", value: string) =>
    void op.ask(
      {
        id,
        method: "POST",
        path: panelDomainPath,
        body: { domain: value },
        errorHeading: t.pdErrHeading,
        warning: id === "set" ? t.pdWarnSet(value) : t.pdWarnClear,
      },
      true,
    );

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (op.pending) return;
    const d = domain.trim().toLowerCase();
    if (d === "") {
      setEmpty(true);
      return;
    }
    setEmpty(false);
    ask("set", d);
  };

  const err = op.quietError;
  const known = err instanceof ApiError ? t.pdErr[err.code] : undefined;
  const checks = q.data ? [q.data.cert, q.data.self_signed].filter((c) => c !== undefined) : [];

  return (
    <>
      <Card title={t.pdHeading} titleId="ten-mien-panel-tieu-de">
        {q.isPending ? (
          <p className="m-0 text-muted">{k.loading}</p>
        ) : q.isError ? (
          <div className="flex flex-col gap-[12px]">
            <p className="m-0">
              {t.pdLoadFailed} {errorMessage(q.error, lang)}
            </p>
            <div>
              <Button onClick={() => void q.refetch()}>{k.retry}</Button>
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-[16px]">
            <p className="m-0 max-w-[72ch] text-muted">{t.pdIntro}</p>
            <div>
              <div className="mb-[4px] text-[12px] font-medium text-muted">{t.pdCurrent}</div>
              {q.data.domain ? <span className="font-medium [overflow-wrap:anywhere]">{q.data.domain}</span> : <span className="text-muted">{t.pdNone}</span>}
            </div>
            {checks.length > 0 && (
              <div>
                <div className="mb-[2px] text-[12px] font-medium text-muted">{t.pdCert}</div>
                <CheckList checks={checks} />
              </div>
            )}

            <form onSubmit={submit} noValidate className="flex max-w-[480px] flex-col gap-[16px]">
              <TextField
                id="ten-mien-panel"
                type="text"
                label={t.pdDomainLabel}
                hint={t.pdDomainHint}
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
                placeholder="panel.example.com"
                value={domain}
                onChange={(e) => {
                  setDomain(e.target.value);
                  setEmpty(false);
                }}
                invalid={empty}
              />
              {empty && (
                <Notice kind="fail" role="alert">
                  {t.pdEmpty}
                </Notice>
              )}
              <div className="flex flex-wrap gap-[8px]">
                <Button type="submit" variant="primary" disabled={op.pending}>
                  {op.busy("set") ? t.pdPreviewing : t.pdPreview}
                </Button>
                {q.data.domain && (
                  <Button onClick={() => ask("clear", "")} disabled={op.pending}>
                    {op.busy("clear") ? t.pdPreviewing : t.pdClear}
                  </Button>
                )}
              </div>
              {err !== null && (known ? <Notice kind="fail" role="alert">{known}</Notice> : <ErrorNote error={err} heading={t.pdErrHeading} />)}
            </form>

            <p className="m-0 max-w-[72ch] text-[13px] text-muted">{t.pdCT}</p>
            <p className="m-0 max-w-[72ch] text-[13px] text-muted">{t.pdSessions}</p>
          </div>
        )}
      </Card>
      {op.panel}
    </>
  );
}
