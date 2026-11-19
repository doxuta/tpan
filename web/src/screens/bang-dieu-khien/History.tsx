// Khối "Lịch sử máy" (mục 6, "Hàng lịch sử"): 10 job gần nhất, mỗi job một hàng
// kẻ mảnh: mã, giờ, việc, kết quả, liên kết tới bằng chứng. Chưa có nút "hoàn
// tác": API M3 chưa có đường dẫn hoàn tác một job đã xong. Lỗi dài của job
// (job.error) không hiện ở đây cho hàng gọn; trang job có đủ.

import { Link } from "react-router";
import type { Job } from "../../api/types";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bang-dieu-khien";
import { fmtWhen } from "./model";

export function History({ jobs }: { jobs: Job[] }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  return (
    <section className="pt-[28px]">
      <h2 className="m-0 mb-[6px] text-[16px] font-semibold">{t.historyTitle}</h2>
      {jobs.length === 0 ? (
        <p className="m-0 border-t border-hair py-[10px] text-muted">{t.historyEmpty}</p>
      ) : (
        <ul className="m-0 list-none p-0">
          {jobs.map((j) => (
            <li key={j.id} className="flex flex-wrap items-baseline gap-x-[16px] gap-y-[4px] border-t border-hair py-[10px]">
              <span title={j.id} className="flex-[0_0_72px] font-mono text-[12px] text-muted">
                {j.id.slice(0, 8)}
              </span>
              <span className="flex-[0_0_110px] font-mono text-[12px] text-muted">{fmtWhen(j.started, lang, t.yesterday)}</span>
              <span className="min-w-0 flex-[1_1_200px] [overflow-wrap:anywhere]">{tx(j.title, lang)}</span>
              <span className="min-w-0 flex-[0_1_180px] text-[13px] text-muted [overflow-wrap:anywhere]">
                {t.jobStatus[j.status]}
              </span>
              <Link to={`/jobs/${encodeURIComponent(j.id)}`} className="text-[13px]">
                {t.evidenceLink}
              </Link>
            </li>
          ))}
        </ul>
      )}
      {jobs.length > 0 && (
        <p className="mt-[8px] mb-0 border-t border-hair pt-[10px] text-[13px]">
          <Link to="/jobs">{t.historyAll}</Link>
        </p>
      )}
    </section>
  );
}
