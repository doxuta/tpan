// Màn Cron (mục 6.10, 10 của KE-HOACH.md): lịch chạy việc định kỳ của WordPress
// ở từng site, lần chạy gần nhất kèm output làm bằng chứng, và lịch backup.
// Màn chỉ đọc: cron chỉ chạy các việc TPAN định nghĩa sẵn, chưa cho gõ lệnh
// tuỳ ý (đó là một đường chạy lệnh tự do), nên không có ô nhập hay nút đổi máy.

import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { errorMessage, get } from "../../api/client";
import { useOverview } from "../../api/queries";
import type { CronOverview } from "../../api/types";
import { Button } from "../../components/Button";
import { CommandBlock } from "../../components/CommandBlock";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { ScreenHeader } from "../../components/ScreenHeader";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/cron";
import { strings as khung } from "../../i18n/khung";
import { BackupSchedule } from "./BackupSchedule";
import { cronKey, cronPath } from "./model";
import { CronSiteTable } from "./SiteTable";

const h2Class = "m-0 pt-[26px] pb-[14px] text-[16px] font-semibold";
const hintClass = "mt-0 mb-[12px] max-w-[720px] text-[13px] text-muted";

export default function Cron() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({ queryKey: cronKey, queryFn: () => get<CronOverview>(cronPath) });
  // Cùng khoá overview với khung trang: không gọi thêm API. Chỉ để biết site nào đang ở thùng rác.
  const overview = useOverview();
  const [openDomain, setOpenDomain] = useState<string | null>(null);

  const trashed = useMemo(() => new Set((overview.data?.sites ?? []).filter((r) => r.site.status === "trashed").map((r) => r.site.domain)), [overview.data]);

  const sites = q.data?.sites ?? [];
  const opened = sites.find((c) => c.domain === openDomain && c.last);
  // Site trong thùng rác không có file cron là đúng ý (đã gỡ khi bỏ vào thùng rác), nên không nhắc chạy lại `site add`.
  const missing = sites.filter((c) => !c.enabled && !trashed.has(c.domain));

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />
      <p className="mt-[12px] mb-0 max-w-[720px] text-[13px] text-muted">{t.readOnly}</p>

      {q.isPending && <p className="mt-[26px] mb-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div className="pt-[26px]">
          <p className="mt-0 mb-[4px] font-semibold">{t.sitesFailed}</p>
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </div>
      )}
      {q.isSuccess && (
        <>
          <section aria-labelledby="cron-sites">
            <h2 id="cron-sites" className={h2Class}>
              {t.sitesHeading}
            </h2>
            <p className={hintClass}>{t.sitesHint}</p>
            <CronSiteTable items={sites} trashed={trashed} openDomain={opened?.domain ?? null} onToggle={(d) => setOpenDomain((cur) => (cur === d ? null : d))} />
            {opened?.last && (
              <div id="cron-evidence" className="pt-[14px]">
                <EvidenceBlock evidence={[{ command: opened.command, exit_code: opened.last.exit_code, output: opened.last.output }]} label={t.evidenceLabel(opened.domain, opened.last.exit_code)} />
                {opened.last.output.trim() === "" && <p className="mt-[6px] mb-0 text-[13px] text-muted">{t.emptyOutput}</p>}
              </div>
            )}
          </section>

          {missing.length > 0 && (
            <section aria-labelledby="cron-missing">
              <h2 id="cron-missing" className={h2Class}>
                {t.missingHeading}
              </h2>
              <p className={hintClass}>{t.missingHint}</p>
              <CommandBlock commands={missing.map((c) => `tpan site add ${c.domain}`)} />
            </section>
          )}

          <section aria-labelledby="cron-backup">
            <h2 id="cron-backup" className={h2Class}>
              {t.backupHeading}
            </h2>
            <p className={hintClass}>{t.backupHint}</p>
            <BackupSchedule settings={q.data.backup} file={q.data.backup_file} />
          </section>
        </>
      )}
    </>
  );
}
