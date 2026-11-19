// Bảng site của màn SSL: mỗi hàng một site, có dấu trạng thái của phép kiểm
// chứng chỉ (bấm mở bằng chứng ngay dưới hàng), số ngày còn lại và nút xin
// chứng chỉ. Tự vẽ bảng thay vì dùng TableList vì cần hàng bằng chứng mở ra
// dưới hàng; kiểu chữ và đường kẻ giữ đúng như TableList.

import { Fragment, useState } from "react";
import { Link } from "react-router";
import type { CheckResult, SSLSiteRow } from "../../api/types";
import { Button } from "../../components/Button";
import { CommandBlock } from "../../components/CommandBlock";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { StatusMark } from "../../components/StatusMark";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/ssl";
import { certDays } from "./model";

const labelClass = "font-mono text-[11px] tracking-[0.08em] text-muted";
const thClass = `pr-[16px] pb-[6px] font-normal uppercase ${labelClass}`;

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
  const { lang } = useLang();
  const [open, setOpen] = useState<Set<string>>(() => new Set());
  const toggle = (d: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (!next.delete(d)) next.add(d);
      return next;
    });

  if (rows.length === 0) {
    return (
      <p className="border-t border-hair py-[10px] text-muted">
        {t.emptyLead}
        <Link to="/sites/new">{t.emptyLink}</Link>
      </p>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-left">
        <caption className="sr-only">{t.tableName}</caption>
        <thead>
          <tr>
            <th scope="col" className={thClass}>
              {t.colDomain}
            </th>
            <th scope="col" className={thClass}>
              {t.colSsl}
            </th>
            <th scope="col" className={thClass}>
              {t.colCert}
            </th>
            <th scope="col" className={`${thClass} text-right`}>
              {t.colDays}
            </th>
            <th scope="col" className={`${thClass} pr-0`}>
              <span className="sr-only">{t.colAction}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => {
            const domain = r.domain;
            const days = certDays(r.cert);
            const isOpen = open.has(domain);
            const busy = pending === domain;
            return (
              <Fragment key={domain}>
                <tr className="border-t border-hair">
                  <td className="py-[10px] pr-[16px] align-baseline">
                    <Link to={`/sites/${encodeURIComponent(domain)}`} className="block min-w-[170px] font-mono text-[13px] whitespace-nowrap">
                      {domain}
                    </Link>
                  </td>
                  <td className="py-[10px] pr-[16px] align-baseline whitespace-nowrap">{r.ssl ? t.sslYes : t.sslNo}</td>
                  <td className="py-[10px] pr-[16px] align-baseline">
                    {r.cert ? (
                      <button
                        type="button"
                        aria-expanded={isOpen}
                        aria-controls={panelId(domain)}
                        onClick={() => toggle(domain)}
                        className="flex min-h-[44px] min-w-[260px] items-start gap-[10px] bg-transparent py-[2px] text-left text-ink sm:min-h-0"
                      >
                        <span className="flex h-[21px] w-[20px] flex-none items-center justify-center">
                          <StatusMark status={r.cert.status} />
                        </span>
                        <span>
                          <span className="sr-only">{k.status[r.cert.status]}: </span>
                          {tx(r.cert.summary, lang)}
                        </span>
                      </button>
                    ) : (
                      <span className="text-muted">{t.noCert}</span>
                    )}
                  </td>
                  <td className="py-[10px] pr-[16px] text-right align-baseline whitespace-nowrap tabular-nums">{days === null ? <span className="text-muted">{t.noCert}</span> : t.days(days)}</td>
                  <td className="py-[10px] align-baseline">
                    <Button aria-label={r.ssl ? t.reissueFor(domain) : t.issueFor(domain)} onClick={() => onIssue(r)} disabled={pending !== null}>
                      {busy ? t.previewing : r.ssl ? t.reissue : t.issue}
                    </Button>
                  </td>
                </tr>
                {r.cert && isOpen && (
                  <tr>
                    <td colSpan={5} className="pb-[16px]">
                      <CertDetail id={panelId(domain)} check={r.cert} />
                    </td>
                  </tr>
                )}
              </Fragment>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

/** Bằng chứng của phép kiểm chứng chỉ: lệnh và output thật, nguyên nhân, cách sửa tay. */
function CertDetail({ id, check }: { id: string; check: CheckResult }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const cause = tx(check.cause, lang);
  const fix = check.manual_fix ?? [];
  return (
    <div id={id} className="flex flex-col gap-[14px] pl-[30px]">
      {check.evidence && check.evidence.length > 0 ? <EvidenceBlock evidence={check.evidence} /> : <p className="m-0 text-muted">{t.noEvidence}</p>}
      {cause && (
        <div>
          <div className={`mb-[6px] ${labelClass}`}>{t.cause}</div>
          <p className="m-0">{cause}</p>
        </div>
      )}
      {fix.length > 0 && (
        <div>
          <div className={`mb-[6px] ${labelClass}`}>{t.manualFix}</div>
          <CommandBlock commands={fix} />
        </div>
      )}
    </div>
  );
}
