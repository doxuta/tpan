// Khối "Hạn mức gói" của user thường (M13, mục 6.12), thay cho khối "Máy": số
// site, số DB rời và dung lượng đang dùng, mỗi số một thanh như MetricBar. Thanh
// đầy là chạm giới hạn của gói; vạch 80% là ngưỡng báo sắp hết. Giới hạn 0 là
// không giới hạn nên không vẽ thanh. Dữ liệu: Overview.quota, đã đo ở server.

import type { Quota } from "../../api/types";
import { MetricBar } from "../../components/MetricBar";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bang-dieu-khien";
import { diskLimitText, diskUsedText, percentOf } from "../nguoi-dung/model";

export function QuotaBlock({ quota }: { quota: Quota }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const { package: pkg, usage } = quota;

  /** Dòng phụ dưới số: "trên 10 · vạch 80%", hoặc nói đã đủ, đã vượt, hoặc không giới hạn. */
  const sub = (used: number, limit: number, limitText: string | null) => {
    if (limitText === null) return t.quotaUnlimited;
    if (used > limit) return t.quotaOver(limitText);
    if (used === limit) return t.quotaFull(limitText);
    return t.quotaOf(limitText);
  };

  const diskUsed = diskUsedText(usage, lang);
  const diskLimit = diskLimitText(pkg.disk_mb, lang);
  const diskMeasured = usage.disk_bytes >= 0;

  return (
    <section className="pt-[28px]">
      <h2 className="m-0 mb-[14px] text-[16px] font-semibold">{t.quotaTitle(pkg.name)}</h2>
      <div className="flex flex-wrap gap-x-[32px] gap-y-[20px]">
        <MetricBar label={t.quotaSites} value={String(usage.sites)} sub={sub(usage.sites, pkg.max_sites, pkg.max_sites > 0 ? String(pkg.max_sites) : null)} percent={percentOf(usage.sites, pkg.max_sites)} />
        <MetricBar label={t.quotaDBs} value={String(usage.dbs)} sub={sub(usage.dbs, pkg.max_dbs, pkg.max_dbs > 0 ? String(pkg.max_dbs) : null)} percent={percentOf(usage.dbs, pkg.max_dbs)} />
        <MetricBar
          label={t.quotaDisk}
          value={diskUsed ?? t.quotaDiskNotMeasured}
          sub={diskMeasured ? sub(usage.disk_bytes, pkg.disk_mb * 2 ** 20, diskLimit) : t.quotaDiskNotMeasuredSub}
          percent={diskMeasured ? percentOf(usage.disk_bytes, pkg.disk_mb * 2 ** 20) : null}
        />
      </div>
      <p className="mt-[14px] mb-0 text-[13px] text-muted">
        {t.quotaBackups(pkg.max_backups > 0 ? String(pkg.max_backups) : t.quotaUnlimited)}. {t.quotaHint}
      </p>
    </section>
  );
}
