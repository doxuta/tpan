// Bảng site của màn SSL (bản 2): một TableList trong thẻ; mỗi hàng một site, có
// dấu trạng thái của phép kiểm chứng chỉ, số ngày còn lại và nút xin chứng chỉ.
// Bằng chứng của chứng chỉ không nằm trong ô bảng (lệnh và output dài): bấm vào
// ô Chứng chỉ thì một thẻ bằng chứng hiện ngay dưới bảng. Đóng sẵn, trừ site có
// chứng chỉ đang lỗi (mục 3.7 của M16-thiet-ke.md).

import { useState } from "react";
import { Link } from "react-router";
import type { CheckResult, SSLSiteRow } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { CommandBlock } from "../../components/CommandBlock";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { RowActions } from "../../components/RowActions";
import { StatusMark } from "../../components/StatusMark";
import { TableList } from "../../components/TableList";
import { useStatusName } from "../../components/useStatusName";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/ssl";
import { certDays } from "./model";

const labelClass = "mb-[6px] text-[12px] font-medium text-muted";

type Props = {
  rows: SSLSiteRow[];
  /** Site đang dựng bản xem trước (khoá nút của hàng đó). */
  pending: string | null;
  onIssue: (row: SSLSiteRow) => void;
};

const panelId = (domain: string) => "ssl-cert-" + domain.replace(/[^a-zA-Z0-9_-]/g, "-");

export function SiteTable({ rows, pending, onIssue }: Props) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const statusName = useStatusName();
  const { lang } = useLang();
  // Chứng chỉ lỗi mở sẵn bằng chứng; còn lại đóng.
  const [open, setOpen] = useState<Set<string>>(() => new Set(rows.filter((r) => r.cert?.status === "fail").map((r) => r.domain)));
  const toggle = (d: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (!next.delete(d)) next.add(d);
      return next;
    });
  const shown = rows.filter((r) => r.cert && open.has(r.domain));

  return (
    <>
      <TableList
        title={t.sitesHeading}
        titleId="ssl-sites"
        caption={t.tableName}
        minWidth={820}
        rows={rows}
        rowKey={(r) => r.domain}
        empty={
          <>
            {t.emptyLead}
            <Link to="/sites/new">{t.emptyLink}</Link>
          </>
        }
        columns={[
          {
            header: t.colDomain,
            cell: (r) => (
              // Tên miền dài nằm một dòng, cắt bằng dấu ba chấm; đủ chữ trong title.
              <Link to={`/sites/${encodeURIComponent(r.domain)}`} title={r.domain} className="inline-block max-w-[320px] truncate align-bottom font-medium no-underline hover:underline">
                {r.domain}
              </Link>
            ),
          },
          { header: t.colSsl, cell: (r) => (r.ssl ? t.sslYes : t.sslNo) },
          {
            header: t.colCert,
            nowrap: false,
            cell: (r) => {
              const cert = r.cert;
              if (!cert) return <span className="text-muted">{t.noCert}</span>;
              const isOpen = open.has(r.domain);
              return (
                <button
                  type="button"
                  aria-expanded={isOpen}
                  aria-controls={isOpen ? panelId(r.domain) : undefined}
                  onClick={() => toggle(r.domain)}
                  className="flex min-h-[44px] min-w-[200px] items-start gap-[10px] bg-transparent py-[2px] text-left text-ink hover:underline sm:min-h-0"
                >
                  <span className="flex h-[21px] w-[20px] flex-none items-center justify-center">
                    <StatusMark status={cert.status} />
                  </span>
                  <span>
                    <span className="sr-only">{statusName(cert.status)}: </span>
                    {tx(cert.summary, lang)}
                  </span>
                </button>
              );
            },
          },
          {
            header: t.colDays,
            align: "right",
            cell: (r) => {
              const days = certDays(r.cert);
              return days === null ? <span className="text-muted">{t.noCert}</span> : t.days(days);
            },
          },
          {
            header: t.colAction,
            align: "right",
            cell: (r) => {
              const label = r.ssl ? t.reissueFor(r.domain) : t.issueFor(r.domain);
              return (
                <RowActions
                  moreLabel={label}
                  primary={[
                    {
                      label: r.ssl ? t.reissue : t.issue,
                      ariaLabel: label,
                      disabled: pending !== null,
                      busy: pending === r.domain,
                      busyLabel: t.previewing,
                      onClick: () => onIssue(r),
                    },
                  ]}
                />
              );
            },
          },
        ]}
      />
      {shown.map((r) =>
        r.cert ? (
          <Card
            key={r.domain}
            id={panelId(r.domain)}
            title={t.certHeading(r.domain)}
            actions={
              <Button size="sm" variant="text" aria-label={t.certCloseFor(r.domain)} onClick={() => toggle(r.domain)}>
                {k.close}
              </Button>
            }
          >
            <CertDetail check={r.cert} />
          </Card>
        ) : null,
      )}
    </>
  );
}

/** Bằng chứng của phép kiểm chứng chỉ: lệnh và output thật, nguyên nhân, cách sửa tay. */
function CertDetail({ check }: { check: CheckResult }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const cause = tx(check.cause, lang);
  const fix = check.manual_fix ?? [];
  return (
    <div className="flex flex-col gap-[14px]">
      {check.evidence && check.evidence.length > 0 ? <EvidenceBlock evidence={check.evidence} clamp={20} /> : <p className="m-0 text-muted">{t.noEvidence}</p>}
      {cause && (
        <div>
          <div className={labelClass}>{t.cause}</div>
          <p className="m-0">{cause}</p>
        </div>
      )}
      {fix.length > 0 && (
        <div>
          <div className={labelClass}>{t.manualFix}</div>
          <CommandBlock commands={fix} />
        </div>
      )}
    </div>
  );
}
