// Tab "Chuyển hướng" của trang một site (M19, mục 3.9 của docs/thay-doi/M19-thiet-ke.md):
// bảng luật chuyển hướng theo đường dẫn (nguồn, đích, 301 hay 302, giữ query hay
// không), ô thêm luật và nút gỡ. Luật khớp đúng đường dẫn, phân biệt hoa thường và
// dấu / cuối: dòng chú ý nói trước điều đó.
//
// Mọi thao tác ghi đi ba chặng như các tab khác (usePreviewedOp). Nguồn, đích và
// mã được máy chủ kiểm lại; lỗi 400 kèm "field" hiện ngay dưới đúng ô sai, lỗi
// khác (redirect_loop, redirect_exists, redirect_limit, site_suspended...) hiện
// nguyên văn dưới nút.

import { useState, type FormEvent } from "react";
import { errorMessage } from "../../api/client";
import { redirectAddBody, redirectErrorField, siteRedirectPath, redirectsKey, siteRedirectsPath, useSiteRedirects } from "../../api/m19";
import type { RedirectCode, RedirectView } from "../../api/m19-types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Code } from "../../components/MonoText";
import { EmptyState } from "../../components/EmptyState";
import { Checkbox, Field, Input, Select } from "../../components/Field";
import { usePreviewedOp } from "../../components/PreviewedOp";
import { RowActions } from "../../components/RowActions";
import { TableList, type Column } from "../../components/TableList";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings as chung } from "../../i18n/m13-chung";
import { strings } from "../../i18n/ten-mien";
import { ErrorNote } from "./ErrorNote";

type Props = {
  domain: string;
  /** Site đang tạm khoá: máy chủ từ chối mọi thay đổi luật (site_suspended), nên nút vô hiệu kèm lời nói trước. */
  suspended: boolean;
};

export function Redirects({ domain, suspended }: Props) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const m = useStrings(chung);
  const { lang } = useLang();
  const q = useSiteRedirects(domain);
  const op = usePreviewedOp([redirectsKey(domain)]);

  const [source, setSource] = useState("");
  const [target, setTarget] = useState("");
  const [code, setCode] = useState<RedirectCode>(301);
  const [keepQuery, setKeepQuery] = useState(false);
  /** Nguồn đã gửi lúc xem trước: dòng lỗi nói đúng luật đó dù ô đã sửa tiếp. */
  const [asked, setAsked] = useState("");

  const rows = q.data ?? [];

  const columns: Column<RedirectView>[] = [
    { header: t.colSource, className: "min-w-[160px]", nowrap: false, cell: (r) => <Code>{r.source}</Code> },
    { header: t.colTarget, className: "min-w-[200px]", nowrap: false, cell: (r) => <Code>{r.target}</Code> },
    { header: t.colCode, className: "tabular-nums", cell: (r) => r.code },
    { header: t.colKeepQuery, cell: (r) => (r.keep_query ? t.keepYes : <span className="text-muted">{t.keepNo}</span>) },
    {
      header: t.colAction,
      align: "right",
      cell: (r) => (
        <RowActions
          moreLabel={t.removeRedirectFor(r.source)}
          primary={[
            {
              label: t.removeRedirect,
              ariaLabel: t.removeRedirectFor(r.source),
              disabled: op.pending || suspended,
              title: suspended ? t.suspendedBlock : undefined,
              busy: op.busy(`rm:${r.id}`),
              busyLabel: m.previewing,
              onClick: () =>
                void op.ask({
                  id: `rm:${r.id}`,
                  method: "DELETE",
                  path: siteRedirectPath(domain, r.id),
                  errorHeading: t.removeRedirectErrorHeading(r.source),
                }),
            },
          ]}
        />
      ),
    },
  ];

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const s = source.trim();
    setAsked(s);
    // quiet: lỗi hiện ngay dưới ô sai, các ô giữ nguyên để sửa rồi gửi lại.
    void op.ask({ id: "add", method: "POST", path: siteRedirectsPath(domain), body: redirectAddBody(s, target, code, keepQuery), errorHeading: t.redirectAddErrorHeading(s) }, true);
  };

  const err = op.quietError;
  const bad = redirectErrorField(err);
  /** Lời của máy chủ dưới đúng ô sai (Field.error có dấu lỗi và role=alert). */
  const fieldError = (f: "source" | "target" | "code") => (bad === f ? errorMessage(err, lang) : undefined);

  return (
    <>
      {q.isPending && (
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {q.isError && (
        <Card>
          <p className="mt-0 mb-[4px] font-semibold">{t.redirectsLoadFailed}</p>
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {q.data && (
        <TableList
          title={t.redirectsHeading}
          titleId="chuyen-huong-bang"
          count={t.redirectsCount(rows.length)}
          caption={t.redirectsCaption(domain)}
          minWidth={760}
          rows={rows}
          rowKey={(r) => String(r.id)}
          empty={<EmptyState>{t.redirectsEmpty}</EmptyState>}
          columns={columns}
          footer={<span>{t.redirectMatchNote}</span>}
        />
      )}

      <Card title={t.addRedirectHeading} titleId="chuyen-huong-them">
        <form onSubmit={submit}>
          <div className="flex flex-wrap items-start gap-x-[14px] gap-y-[10px]">
            <div className="min-w-[200px] flex-[1_1_240px]">
              <Field label={t.sourceLabel} htmlFor="chuyen-huong-nguon" hint={t.sourceHint} hintId="chuyen-huong-nguon-goi-y" error={fieldError("source")}>
                <Input
                  id="chuyen-huong-nguon"
                  mono
                  value={source}
                  onChange={(e) => setSource(e.target.value)}
                  aria-invalid={bad === "source" || undefined}
                  aria-describedby="chuyen-huong-nguon-goi-y"
                  autoComplete="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </Field>
            </div>
            <div className="min-w-[200px] flex-[1_1_280px]">
              <Field label={t.targetLabel} htmlFor="chuyen-huong-dich" hint={t.targetHint} hintId="chuyen-huong-dich-goi-y" error={fieldError("target")}>
                <Input
                  id="chuyen-huong-dich"
                  mono
                  value={target}
                  onChange={(e) => setTarget(e.target.value)}
                  aria-invalid={bad === "target" || undefined}
                  aria-describedby="chuyen-huong-dich-goi-y"
                  autoComplete="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </Field>
            </div>
            <div className="min-w-[200px] flex-[0_1_220px]">
              <Field label={t.codeLabel} htmlFor="chuyen-huong-ma" error={fieldError("code")}>
                <Select id="chuyen-huong-ma" value={code} onChange={(e) => setCode(e.target.value === "302" ? 302 : 301)} aria-invalid={bad === "code" || undefined}>
                  <option value="301">{t.code301}</option>
                  <option value="302">{t.code302}</option>
                </Select>
              </Field>
            </div>
          </div>
          <label className="mt-[10px] flex min-h-[44px] items-center gap-[10px] sm:min-h-[32px]">
            <Checkbox label={t.keepQueryLabel} checked={keepQuery} onChange={(e) => setKeepQuery(e.target.checked)} />
            <span>{t.keepQueryLabel}</span>
          </label>
          <div className="pt-[8px]">
            <Button type="submit" disabled={op.pending || suspended || source.trim() === "" || target.trim() === ""} aria-describedby={suspended ? "chuyen-huong-bi-chan" : undefined}>
              {op.busy("add") ? m.previewing : t.redirectPreview}
            </Button>
          </div>
          {suspended && (
            <p id="chuyen-huong-bi-chan" className="mt-[10px] mb-0 max-w-[72ch] text-[13px] text-muted">
              {t.suspendedBlock}
            </p>
          )}
          {/* Lỗi không chỉ ra ô nào (vòng lặp, luật trùng, quá nhiều luật, site tạm khoá): hiện dưới nút. */}
          {err !== null && bad === null && (
            <div className="pt-[12px]">
              <ErrorNote error={err} heading={t.redirectAddErrorHeading(asked)} checksHeading={m.checksHeading} />
            </div>
          )}
        </form>
      </Card>
      {op.panel}
    </>
  );
}
