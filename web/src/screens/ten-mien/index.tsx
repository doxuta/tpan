// /domains (M19, mục 3.9 của docs/thay-doi/M19-thiet-ke.md): các site trong phạm
// vi người xem kèm số tên phụ và số luật chuyển hướng. Vào từ hai ô "Tên miền phụ"
// và "Chuyển hướng" của trang Công cụ; ?tab=chuyen-huong chọn tiêu đề và đích của
// dòng (tab Chuyển hướng của site), mặc định là tab Tên miền.
// Số đếm lấy từ bảng, không tra DNS (xem handleDomains): chi tiết từng tên nằm ở trang site.

import { Link, useSearchParams } from "react-router";
import { errorMessage } from "../../api/client";
import { useDomains, type DomainRow } from "../../api/m19";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { DomainText } from "../../components/DomainText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { TableList, type Column } from "../../components/TableList";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/ten-mien-ds";

export default function Domains() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useDomains();
  const redirects = useSearchParams()[0].get("tab") === "chuyen-huong";
  const tab = redirects ? "chuyen-huong" : "ten-mien";

  const columns: Column<DomainRow>[] = [
    {
      header: t.colSite,
      className: "min-w-[200px]",
      // Tên miền dài xuống dòng ngay sau dấu chấm, như bảng danh sách site.
      nowrap: false,
      cell: (r) => (
        <Link
          to={`/sites/${encodeURIComponent(r.site)}?tab=${tab}`}
          aria-label={redirects ? t.openRedirects(r.site) : t.openNames(r.site)}
          className="font-medium no-underline hover:underline"
        >
          <DomainText domain={r.site} />
        </Link>
      ),
    },
    { header: t.colNames, align: "right", className: "tabular-nums", cell: (r) => r.names.length },
    { header: t.colRules, align: "right", className: "tabular-nums", cell: (r) => r.redirects },
  ];

  return (
    <>
      <ScreenHeader title={redirects ? t.redirectsTitle : t.namesTitle} summary={redirects ? t.redirectsSummary : t.namesSummary} />
      {q.isPending && (
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {q.isError && (
        <Card>
          <p className="mt-0 mb-[12px]">
            {t.loadFailed} {errorMessage(q.error, lang)}
          </p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {q.data && (
        <TableList
          label={t.caption}
          caption={t.caption}
          minWidth={560}
          rows={q.data}
          rowKey={(r) => r.site}
          empty={
            <>
              {t.emptyLead}
              <Link to="/sites/new">{t.emptyLink}</Link>
            </>
          }
          columns={columns}
          footer={q.data.length > 0 ? <span>{t.count(q.data.length)}</span> : undefined}
        />
      )}
    </>
  );
}
