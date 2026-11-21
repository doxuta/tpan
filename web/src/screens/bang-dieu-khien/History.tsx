// Thẻ "Lịch sử máy" (mục 6, "Hàng lịch sử"): 10 job gần nhất, mỗi job một hàng
// của bảng: mã, giờ, việc, kết quả, liên kết tới bằng chứng. Chưa có nút "hoàn
// tác": API M3 chưa có đường dẫn hoàn tác một job đã xong. Lỗi dài của job
// (job.error) không hiện ở đây cho hàng gọn; trang job có đủ.

import { Link } from "react-router";
import type { Job } from "../../api/types";
import { Code } from "../../components/MonoText";
import { TableList, type Column } from "../../components/TableList";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bang-dieu-khien";
import { fmtWhen } from "./model";

export function History({ jobs }: { jobs: Job[] }) {
  const t = useStrings(strings);
  const { lang } = useLang();

  const columns: Column<Job>[] = [
    {
      header: t.colJob,
      cell: (j) => (
        <span title={j.id}>
          <Code className="text-muted">{j.id.slice(0, 8)}</Code>
        </span>
      ),
    },
    { header: t.colWhen, cell: (j) => <span className="text-[13px] text-muted">{fmtWhen(j.started, lang, t.yesterday)}</span> },
    { header: t.colWhat, nowrap: false, cell: (j) => <span className="block min-w-[200px] [overflow-wrap:anywhere]">{tx(j.title, lang)}</span> },
    { header: t.colResult, nowrap: false, cell: (j) => <span className="text-[13px] text-muted">{t.jobStatus[j.status]}</span> },
    {
      header: t.colEvidence,
      align: "right",
      cell: (j) => (
        <Link to={`/jobs/${encodeURIComponent(j.id)}`} aria-label={t.evidenceFor(j.id.slice(0, 8))} className="inline-flex min-h-[44px] items-center sm:min-h-0">
          {t.evidenceLink}
        </Link>
      ),
    },
  ];

  return (
    <TableList
      title={t.historyTitle}
      titleId="bdk-lich-su"
      minWidth={600}
      rows={jobs}
      rowKey={(j) => j.id}
      columns={columns}
      empty={t.historyEmpty}
      caption={t.historyTitle}
      footer={jobs.length > 0 ? <Link to="/jobs">{t.historyAll}</Link> : undefined}
    />
  );
}
