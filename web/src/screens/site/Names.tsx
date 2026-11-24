// Tab "Tên miền" của trang một site (M19, mục 3.9 của docs/thay-doi/M19-thiet-ke.md):
// bảng tên chính và tên phụ (kiểu phục vụ hay chuyển, DNS, có trong chứng chỉ chưa),
// ô thêm tên phụ, đổi kiểu, gỡ. Tên chính không có nút nào: máy chủ cũng từ chối
// gỡ (alias_primary).
//
// Mọi thao tác ghi đi ba chặng như các tab khác (usePreviewedOp): xem trước →
// PlanReview → chạy, rồi thẻ theo dõi job ở góc dưới báo tiến độ và tải lại bảng.
// Lời từ chối của máy chủ (name_unavailable, alias_limit, site_suspended...) hiện
// nguyên văn, kể cả câu chung của name_unavailable: giao diện không đoán thêm lý do,
// để user thường không dò được tên nào đang ở máy (S1 của M19).

import { useState, type FormEvent } from "react";
import { aliasAddBody, aliasModeBody, namesKey, siteAliasModePath, siteAliasPath, siteNamesPath, useSiteNames } from "../../api/m19";
import type { NameMode, NameView } from "../../api/m19-types";
import { errorMessage } from "../../api/client";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { DomainText } from "../../components/DomainText";
import { EmptyState } from "../../components/EmptyState";
import { SelectField, TextField } from "../../components/Field";
import { usePreviewedOp, type Op } from "../../components/PreviewedOp";
import { RowActions } from "../../components/RowActions";
import { StatusLine } from "../../components/StatusMark";
import { TableList, type Column } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings as chung } from "../../i18n/m13-chung";
import { strings } from "../../i18n/ten-mien";
import { ErrorNote } from "./ErrorNote";
import { dnsStatus, isWwwServe, otherMode } from "./names-model";

type Props = {
  domain: string;
  /** Site đang tạm khoá: máy chủ từ chối mọi thay đổi tên (site_suspended), nên nút vô hiệu kèm lời nói trước. */
  suspended: boolean;
};

export function Names({ domain, suspended }: Props) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const m = useStrings(chung);
  const { lang } = useLang();
  const q = useSiteNames(domain);
  const op = usePreviewedOp([namesKey(domain)]);

  const [name, setName] = useState("");
  const [mode, setMode] = useState<NameMode>("redirect");
  /** Tên đã gửi lúc xem trước: dòng lỗi nói đúng tên đó dù ô đã sửa tiếp. */
  const [asked, setAsked] = useState("");

  const rows = q.data ?? [];
  const wwwServe = rows.some((r) => r.kind === "alias" && isWwwServe(r.name, r.mode));

  const modeOp = (r: NameView): Op => {
    const to = otherMode(r.mode);
    return {
      id: `mode:${r.name}`,
      method: "POST",
      path: siteAliasModePath(domain, r.name),
      body: aliasModeBody(to),
      errorHeading: t.modeErrorHeading(r.name),
    };
  };
  const removeOp = (r: NameView): Op => ({
    id: `rm:${r.name}`,
    method: "DELETE",
    path: siteAliasPath(domain, r.name),
    errorHeading: t.removeNameErrorHeading(r.name),
  });

  const columns: Column<NameView>[] = [
    {
      header: t.colName,
      className: "min-w-[200px]",
      // Tên dài xuống dòng sau dấu chấm (DomainText), nên bảng không bị cắt chữ.
      nowrap: false,
      cell: (r) => (
        <span className="flex flex-wrap items-center gap-x-[10px] gap-y-[4px]">
          <span className="font-medium">
            <DomainText domain={r.name} />
          </span>
          {r.kind === "primary" && <Tag>{t.primaryTag}</Tag>}
          {r.state === "pending" && <Tag>{t.pendingTag}</Tag>}
        </span>
      ),
    },
    {
      header: t.colMode,
      cell: (r) => (r.kind === "primary" ? <span className="text-muted">—</span> : r.mode === "serve" ? t.modeServe : t.modeRedirect),
    },
    {
      header: t.colDns,
      cell: (r) => {
        const status = dnsStatus(r.dns);
        return <StatusLine status={status}>{r.dns === "ok" ? t.dnsOk : r.dns === "missing" ? t.dnsMissing : t.dnsUnknown}</StatusLine>;
      },
    },
    { header: t.colCert, cell: (r) => (r.in_cert ? t.certIn : <span className="text-muted">{t.certNot}</span>) },
    {
      header: t.colAction,
      align: "right",
      cell: (r) => {
        // Tên chính không đổi kiểu và không gỡ được; tên đang xử lý (pending) chờ job xong.
        if (r.kind === "primary") return null;
        const off = op.pending || suspended || r.state === "pending";
        const to = otherMode(r.mode);
        return (
          <RowActions
            moreLabel={t.removeNameFor(r.name)}
            primary={[
              {
                label: to === "serve" ? t.toServe : t.toRedirect,
                ariaLabel: to === "serve" ? t.toServeFor(r.name) : t.toRedirectFor(r.name),
                disabled: off,
                title: suspended ? t.suspendedBlock : undefined,
                busy: op.busy(`mode:${r.name}`),
                busyLabel: m.previewing,
                onClick: () => void op.ask(modeOp(r)),
              },
              {
                label: t.removeName,
                ariaLabel: t.removeNameFor(r.name),
                disabled: off,
                title: suspended ? t.suspendedBlock : undefined,
                busy: op.busy(`rm:${r.name}`),
                busyLabel: m.previewing,
                onClick: () => void op.ask(removeOp(r)),
              },
            ]}
          />
        );
      },
    },
  ];

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const n = name.trim();
    setAsked(n);
    // quiet: lỗi hiện ngay dưới ô nhập, ô giữ nguyên để sửa rồi gửi lại.
    void op.ask({ id: "add", method: "POST", path: siteNamesPath(domain), body: aliasAddBody(n, mode), errorHeading: t.addErrorHeading(n) }, true);
  };

  return (
    <>
      {q.isPending && (
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {q.isError && (
        <Card>
          <p className="mt-0 mb-[4px] font-semibold">{t.namesLoadFailed}</p>
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {q.data && (
        <TableList
          title={t.namesHeading}
          titleId="ten-mien-bang"
          count={t.namesCount(rows.length)}
          caption={t.namesCaption(domain)}
          minWidth={760}
          rows={rows}
          rowKey={(r) => r.name}
          empty={<EmptyState>{t.namesEmpty}</EmptyState>}
          columns={columns}
          footer={wwwServe ? <span>{t.wwwServeNote}</span> : undefined}
        />
      )}

      <Card title={t.addNameHeading} titleId="ten-mien-them">
        <form onSubmit={submit}>
          <div className="flex flex-wrap items-start gap-x-[14px] gap-y-[10px]">
            <div className="min-w-[240px] flex-[2_1_280px]">
              <TextField
                id="ten-mien-ten"
                label={t.addNameLabel}
                hint={t.addNameHint}
                value={name}
                onChange={(e) => setName(e.target.value)}
                invalid={op.quietError !== null}
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            </div>
            <div className="min-w-[200px] flex-[1_1_220px]">
              <SelectField id="ten-mien-kieu" label={t.addModeLabel} value={mode} onChange={(e) => setMode(e.target.value as NameMode)}>
                <option value="redirect">{t.addModeRedirect}</option>
                <option value="serve">{t.addModeServe}</option>
              </SelectField>
            </div>
          </div>
          <p className="mt-[10px] mb-0 max-w-[72ch] text-[13px] text-muted">{t.addModeHint}</p>
          {isWwwServe(name, mode) && <p className="mt-[6px] mb-0 max-w-[72ch] text-[13px] text-muted">{t.wwwServeNote}</p>}
          <div className="pt-[12px]">
            <Button type="submit" disabled={op.pending || suspended || name.trim() === ""} aria-describedby={suspended ? "ten-mien-bi-chan" : undefined}>
              {op.busy("add") ? m.previewing : t.addPreview}
            </Button>
          </div>
          {suspended && (
            <p id="ten-mien-bi-chan" className="mt-[10px] mb-0 max-w-[72ch] text-[13px] text-muted">
              {t.suspendedBlock}
            </p>
          )}
          {op.quietError !== null && (
            <div className="pt-[12px]">
              <ErrorNote error={op.quietError} heading={t.addErrorHeading(asked)} checksHeading={m.checksHeading} />
            </div>
          )}
        </form>
      </Card>
      {op.panel}
    </>
  );
}
