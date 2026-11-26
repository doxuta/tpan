// Tab "Tên miền" của trang một site. Từ v0.2.14 gộp hai tab cũ (Tên miền và
// Chuyển hướng, M19) thành một: người dùng chỉ có một câu hỏi, "gõ địa chỉ nào
// thì ra gì", nên một bảng trả lời hết. Mỗi dòng là một địa chỉ:
//
//   - một tên miền (tên chính hoặc tên thêm): hiện site này, hoặc chuyển sang tên chính;
//   - một đường dẫn trên site: chuyển sang đường dẫn hay trang khác (301 hoặc 302).
//
// Bên dưới là một khối "Thêm địa chỉ" với hai lựa chọn tương ứng. API không đổi:
// tên miền đi qua /sites/{domain}/names, đường dẫn qua /sites/{domain}/redirects.
//
// Mọi thao tác ghi đi ba chặng như các tab khác (usePreviewedOp): xem trước →
// PlanReview → chạy. Lời từ chối của máy chủ hiện nguyên văn; lỗi 400 của ô đường
// dẫn kèm "field" thì hiện ngay dưới đúng ô sai.

import { useState, type FormEvent } from "react";
import { errorMessage } from "../../api/client";
import {
  aliasAddBody,
  aliasModeBody,
  namesKey,
  redirectAddBody,
  redirectErrorField,
  redirectsKey,
  siteAliasModePath,
  siteAliasPath,
  siteNamesPath,
  siteRedirectPath,
  siteRedirectsPath,
  useSiteNames,
  useSiteRedirects,
} from "../../api/m19";
import type { NameMode, NameView, RedirectCode, RedirectView } from "../../api/m19-types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { DomainText } from "../../components/DomainText";
import { EmptyState } from "../../components/EmptyState";
import { Checkbox, Field, Input, Select, SelectField, TextField } from "../../components/Field";
import { Code } from "../../components/MonoText";
import { usePreviewedOp, type Op } from "../../components/PreviewedOp";
import { RowActions } from "../../components/RowActions";
import { SegmentButton } from "../../components/SegmentButton";
import { StatusLine } from "../../components/StatusMark";
import { TableList, type Column } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings as chung } from "../../i18n/m13-chung";
import { strings } from "../../i18n/ten-mien";
import { ErrorNote } from "./ErrorNote";
import { addressRows, dnsStatus, isWwwServe, otherMode, type AddressRow } from "./names-model";

type Props = {
  domain: string;
  /** Site đang tạm khoá: máy chủ từ chối mọi thay đổi địa chỉ (site_suspended), nên nút vô hiệu kèm lời nói trước. */
  suspended: boolean;
};

type What = "name" | "path";

export function Addresses({ domain, suspended }: Props) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const m = useStrings(chung);
  const { lang } = useLang();
  const names = useSiteNames(domain);
  const rules = useSiteRedirects(domain);
  const op = usePreviewedOp([namesKey(domain), redirectsKey(domain)]);

  const [what, setWhat] = useState<What>("name");
  const [name, setName] = useState("");
  const [mode, setMode] = useState<NameMode>("redirect");
  const [source, setSource] = useState("");
  const [target, setTarget] = useState("");
  const [code, setCode] = useState<RedirectCode>(301);
  const [keepQuery, setKeepQuery] = useState(false);
  /** Thứ đã gửi lúc xem trước: dòng lỗi nói đúng thứ đó dù ô đã sửa tiếp hay đã đổi lựa chọn. */
  const [asked, setAsked] = useState<{ what: What; text: string }>({ what: "name", text: "" });

  const nameRows = names.data ?? [];
  const ruleRows = rules.data ?? [];
  const rows = addressRows(nameRows, ruleRows);
  const wwwServe = nameRows.some((r) => r.kind === "alias" && isWwwServe(r.name, r.mode));
  const failed = names.isError ? names : rules.isError ? rules : null;

  const modeOp = (r: NameView): Op => ({
    id: `mode:${r.name}`,
    method: "POST",
    path: siteAliasModePath(domain, r.name),
    body: aliasModeBody(otherMode(r.mode)),
    errorHeading: t.modeErrorHeading(r.name),
  });
  const removeNameOp = (r: NameView): Op => ({
    id: `rm:${r.name}`,
    method: "DELETE",
    path: siteAliasPath(domain, r.name),
    errorHeading: t.removeNameErrorHeading(r.name),
  });
  const removePathOp = (r: RedirectView): Op => ({
    id: `rmp:${r.id}`,
    method: "DELETE",
    path: siteRedirectPath(domain, r.id),
    errorHeading: t.removePathErrorHeading(r.source),
  });

  /** Cột "Thì": một câu thường nói điều xảy ra, không dùng chữ kỹ thuật. */
  const thenCell = (row: AddressRow) => {
    if (row.kind === "name") {
      const n = row.name;
      if (n.kind === "primary") return t.thenShow;
      return n.mode === "serve" ? t.thenShowAlias : t.thenToPrimary(domain);
    }
    const r = row.rule;
    return (
      <span className="flex flex-wrap items-baseline gap-x-[8px] gap-y-[2px]">
        <span>{t.thenTo}</span>
        <Code>{r.target}</Code>
        <span className="text-[13px] text-muted">
          {r.code === 302 ? t.temporary : t.permanent}
          {r.keep_query ? ` · ${t.keepsQuery}` : ""}
        </span>
      </span>
    );
  };

  const stateCell = (row: AddressRow) => {
    if (row.kind === "path") return <span className="text-muted">{t.pathState}</span>;
    const n = row.name;
    return (
      <span className="flex flex-col gap-[2px]">
        <StatusLine status={dnsStatus(n.dns)}>{n.dns === "ok" ? t.dnsOk : n.dns === "missing" ? t.dnsMissing : t.dnsUnknown}</StatusLine>
        <span className="text-[13px] text-muted">{n.in_cert ? t.certIn : t.certNot}</span>
      </span>
    );
  };

  const actionCell = (row: AddressRow) => {
    const blocked = suspended ? t.suspendedBlock : undefined;
    if (row.kind === "path") {
      const r = row.rule;
      return (
        <RowActions
          moreLabel={t.removePathFor(r.source)}
          primary={[
            { label: t.remove, ariaLabel: t.removePathFor(r.source), disabled: op.pending || suspended, title: blocked, busy: op.busy(`rmp:${r.id}`), busyLabel: m.previewing, onClick: () => void op.ask(removePathOp(r)) },
          ]}
        />
      );
    }
    const n = row.name;
    // Tên chính không đổi cách xử lý và không gỡ được; tên đang xử lý (pending) chờ job xong.
    if (n.kind === "primary") return null;
    const off = op.pending || suspended || n.state === "pending";
    const to = otherMode(n.mode);
    return (
      <RowActions
        moreLabel={t.removeNameFor(n.name)}
        primary={[
          { label: to === "serve" ? t.toServe : t.toRedirect, ariaLabel: to === "serve" ? t.toServeFor(n.name) : t.toRedirectFor(n.name), disabled: off, title: blocked, busy: op.busy(`mode:${n.name}`), busyLabel: m.previewing, onClick: () => void op.ask(modeOp(n)) },
          { label: t.remove, ariaLabel: t.removeNameFor(n.name), disabled: off, title: blocked, busy: op.busy(`rm:${n.name}`), busyLabel: m.previewing, onClick: () => void op.ask(removeNameOp(n)) },
        ]}
      />
    );
  };

  const columns: Column<AddressRow>[] = [
    {
      header: t.colWhen,
      className: "min-w-[220px]",
      // Tên dài xuống dòng sau dấu chấm (DomainText), nên bảng không bị cắt chữ.
      nowrap: false,
      cell: (row) =>
        row.kind === "name" ? (
          <span className="flex flex-wrap items-center gap-x-[10px] gap-y-[4px]">
            <span className="font-medium">
              <DomainText domain={row.name.name} />
            </span>
            {row.name.kind === "primary" && <Tag>{t.primaryTag}</Tag>}
            {row.name.state === "pending" && <Tag>{t.pendingTag}</Tag>}
          </span>
        ) : (
          <span className="break-all">
            <span className="text-muted">
              <Code>{domain}</Code>
            </span>
            <span className="font-medium">
              <Code>{row.rule.source}</Code>
            </span>
          </span>
        ),
    },
    { header: t.colThen, className: "min-w-[240px]", nowrap: false, cell: thenCell },
    { header: t.colState, className: "min-w-[230px]", nowrap: false, cell: stateCell },
    { header: t.colAction, align: "right", cell: actionCell },
  ];

  const submitName = (e: FormEvent) => {
    e.preventDefault();
    const n = name.trim();
    setAsked({ what: "name", text: n });
    // quiet: lỗi hiện ngay dưới ô nhập, ô giữ nguyên để sửa rồi gửi lại.
    void op.ask({ id: "add", method: "POST", path: siteNamesPath(domain), body: aliasAddBody(n, mode), errorHeading: t.addNameErrorHeading(n) }, true);
  };
  const submitPath = (e: FormEvent) => {
    e.preventDefault();
    const s = source.trim();
    setAsked({ what: "path", text: s });
    void op.ask({ id: "add", method: "POST", path: siteRedirectsPath(domain), body: redirectAddBody(s, target, code, keepQuery), errorHeading: t.addPathErrorHeading(s) }, true);
  };

  // Lỗi của lần xem trước gần nhất chỉ hiện ở đúng khối đã gửi nó.
  const err = op.quietError;
  const nameErr = asked.what === "name" ? err : null;
  const pathErr = asked.what === "path" ? err : null;
  const bad = redirectErrorField(pathErr);
  /** Lời của máy chủ dưới đúng ô sai (Field.error có dấu lỗi và role=alert). */
  const fieldError = (f: "source" | "target" | "code") => (bad === f ? errorMessage(pathErr, lang) : undefined);

  return (
    <>
      {(names.isPending || rules.isPending) && !failed && (
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {failed && (
        <Card>
          <p className="mt-0 mb-[4px] font-semibold">{t.loadFailed}</p>
          <p className="mt-0 mb-[12px]">{errorMessage(failed.error, lang)}</p>
          <Button onClick={() => void failed.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {names.data && rules.data && (
        <TableList
          title={t.heading}
          titleId="dia-chi-bang"
          count={t.count(nameRows.length, ruleRows.length)}
          caption={t.caption(domain)}
          minWidth={820}
          rows={rows}
          rowKey={(r) => r.key}
          empty={<EmptyState>{t.empty}</EmptyState>}
          columns={columns}
          footer={
            <span className="flex flex-col gap-[4px]">
              <span>{t.intro}</span>
              {ruleRows.length > 0 && <span>{t.matchNote}</span>}
              {wwwServe && <span>{t.wwwServeNote}</span>}
            </span>
          }
        />
      )}

      <Card title={t.addHeading} titleId="dia-chi-them">
        <div className="pb-[12px]">
          <SegmentButton<What>
            label={t.addWhat}
            value={what}
            onChange={setWhat}
            options={[
              { value: "name", label: t.addWhatName },
              { value: "path", label: t.addWhatPath },
            ]}
          />
        </div>
        {what === "name" ? (
          <form onSubmit={submitName}>
            <p className="mt-0 mb-[12px] max-w-[72ch] text-[13px] text-muted">{t.addNameIntro}</p>
            <div className="flex flex-wrap items-start gap-x-[14px] gap-y-[10px]">
              <div className="min-w-[240px] flex-[2_1_280px]">
                <TextField id="dia-chi-ten" label={t.nameLabel} hint={t.nameHint} value={name} onChange={(e) => setName(e.target.value)} invalid={nameErr !== null} autoComplete="off" autoCapitalize="none" spellCheck={false} />
              </div>
              <div className="min-w-[240px] flex-[1_1_260px]">
                <SelectField id="dia-chi-kieu" label={t.modeLabel} value={mode} onChange={(e) => setMode(e.target.value as NameMode)}>
                  <option value="redirect">{t.modeRedirect}</option>
                  <option value="serve">{t.modeServe}</option>
                </SelectField>
              </div>
            </div>
            <p className="mt-[10px] mb-0 max-w-[72ch] text-[13px] text-muted">{t.modeHint}</p>
            {isWwwServe(name, mode) && <p className="mt-[6px] mb-0 max-w-[72ch] text-[13px] text-muted">{t.wwwServeNote}</p>}
            <div className="pt-[12px]">
              <Button type="submit" disabled={op.pending || suspended || name.trim() === ""} aria-describedby={suspended ? "dia-chi-bi-chan" : undefined}>
                {op.busy("add") ? m.previewing : t.preview}
              </Button>
            </div>
            {nameErr !== null && (
              <div className="pt-[12px]">
                <ErrorNote error={nameErr} heading={t.addNameErrorHeading(asked.text)} checksHeading={m.checksHeading} />
              </div>
            )}
          </form>
        ) : (
          <form onSubmit={submitPath}>
            <p className="mt-0 mb-[12px] max-w-[72ch] text-[13px] text-muted">{t.addPathIntro}</p>
            <div className="flex flex-wrap items-start gap-x-[14px] gap-y-[10px]">
              <div className="min-w-[200px] flex-[1_1_240px]">
                <Field label={t.sourceLabel} htmlFor="dia-chi-cu" hint={t.sourceHint} hintId="dia-chi-cu-goi-y" error={fieldError("source")}>
                  <Input id="dia-chi-cu" mono value={source} onChange={(e) => setSource(e.target.value)} aria-invalid={bad === "source" || undefined} aria-describedby="dia-chi-cu-goi-y" autoComplete="off" autoCapitalize="none" spellCheck={false} />
                </Field>
              </div>
              <div className="min-w-[200px] flex-[1_1_280px]">
                <Field label={t.targetLabel} htmlFor="dia-chi-moi" hint={t.targetHint} hintId="dia-chi-moi-goi-y" error={fieldError("target")}>
                  <Input id="dia-chi-moi" mono value={target} onChange={(e) => setTarget(e.target.value)} aria-invalid={bad === "target" || undefined} aria-describedby="dia-chi-moi-goi-y" autoComplete="off" autoCapitalize="none" spellCheck={false} />
                </Field>
              </div>
              <div className="min-w-[240px] flex-[0_1_320px]">
                <Field label={t.codeLabel} htmlFor="dia-chi-ma" error={fieldError("code")}>
                  <Select id="dia-chi-ma" value={code} onChange={(e) => setCode(e.target.value === "302" ? 302 : 301)} aria-invalid={bad === "code" || undefined}>
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
              <Button type="submit" disabled={op.pending || suspended || source.trim() === "" || target.trim() === ""} aria-describedby={suspended ? "dia-chi-bi-chan" : undefined}>
                {op.busy("add") ? m.previewing : t.preview}
              </Button>
            </div>
            {/* Lỗi không chỉ ra ô nào (vòng lặp, trùng, quá nhiều, site tạm khoá): hiện dưới nút. */}
            {pathErr !== null && bad === null && (
              <div className="pt-[12px]">
                <ErrorNote error={pathErr} heading={t.addPathErrorHeading(asked.text)} checksHeading={m.checksHeading} />
              </div>
            )}
          </form>
        )}
        {suspended && (
          <p id="dia-chi-bi-chan" className="mt-[10px] mb-0 max-w-[72ch] text-[13px] text-muted">
            {t.suspendedBlock}
          </p>
        )}
      </Card>
      {op.panel}
    </>
  );
}
