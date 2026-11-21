// Màn Cron (mục 6.10, 10 của KE-HOACH.md; giao diện bản 2, mục 3.7 của
// M16-thiet-ke.md): lịch chạy việc định kỳ của WordPress ở từng site, lần chạy
// gần nhất kèm output làm bằng chứng, và lịch backup. Màn chỉ đọc: cron chỉ
// chạy các việc TPAN định nghĩa sẵn, chưa cho gõ lệnh tuỳ ý (đó là một đường
// chạy lệnh tự do), nên không có ô nhập hay nút đổi máy.

import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { errorMessage, get } from "../../api/client";
import { useOverview } from "../../api/queries";
import type { CronOverview } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { CommandBlock } from "../../components/CommandBlock";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { ScreenHeader } from "../../components/ScreenHeader";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/cron";
import { strings as khung } from "../../i18n/khung";
import { BackupSchedule } from "./BackupSchedule";
import { cronKey, cronPath } from "./model";
import { CronSiteTable } from "./SiteTable";

export default function Cron() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({ queryKey: cronKey, queryFn: () => get<CronOverview>(cronPath) });
  // Cùng khoá overview với khung trang: không gọi thêm API. Chỉ để biết site nào đang ở thùng rác.
  const overview = useOverview();
  /** undefined: người dùng chưa bấm gì (lần chạy lỗi đầu tiên mở sẵn); null: đã đóng hết. */
  const [picked, setPicked] = useState<string | null | undefined>(undefined);

  const trashed = useMemo(() => new Set((overview.data?.sites ?? []).filter((r) => r.site.status === "trashed").map((r) => r.site.domain)), [overview.data]);
  // M13: site đang tạm khoá (kèm nguồn khoá). Cron của nó tắt có chủ ý, không phải thiếu file.
  const suspended = useMemo(() => new Map((overview.data?.sites ?? []).flatMap((r) => (r.site.suspended ? [[r.site.domain, r.site.suspended] as const] : []))), [overview.data]);

  const sites = q.data?.sites ?? [];
  // Bằng chứng đóng sẵn, trừ lần chạy lỗi (mục 3.7): chưa bấm gì thì mở site lỗi đầu tiên.
  const openDomain = picked === undefined ? (sites.find((c) => c.last?.status === "fail")?.domain ?? null) : picked;
  const opened = sites.find((c) => c.domain === openDomain && c.last);
  // Site trong thùng rác hay đang tạm khoá không có file cron là đúng ý (đã gỡ hoặc đổi tên .suspended), nên không nhắc chạy lại `site add`.
  const missing = sites.filter((c) => !c.enabled && !trashed.has(c.domain) && !suspended.has(c.domain));

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />

      {q.isPending && (
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {q.isError && (
        <Card>
          <p className="mt-0 mb-[4px] font-semibold">{t.sitesFailed}</p>
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {q.isSuccess && (
        <>
          <CronSiteTable items={sites} trashed={trashed} suspended={suspended} openDomain={opened?.domain ?? null} onToggle={(d) => setPicked(opened?.domain === d ? null : d)} />

          {opened?.last && (
            <Card id="cron-evidence" title={t.evidenceTitle(opened.domain, opened.last.exit_code)}>
              <EvidenceBlock evidence={[{ command: opened.command, exit_code: opened.last.exit_code, output: opened.last.output }]} label={null} clamp={20} />
              {opened.last.output.trim() === "" && <p className="mt-[8px] mb-0 text-[13px] text-muted">{t.emptyOutput}</p>}
            </Card>
          )}

          {missing.length > 0 && (
            <Card title={t.missingHeading} titleId="cron-missing">
              <p className="mt-0 mb-[12px] max-w-[72ch] text-[13px] text-muted">{t.missingHint}</p>
              <CommandBlock commands={missing.map((c) => `tpan site add ${c.domain}`)} />
            </Card>
          )}

          <BackupSchedule settings={q.data.backup} file={q.data.backup_file} />
        </>
      )}
    </>
  );
}
