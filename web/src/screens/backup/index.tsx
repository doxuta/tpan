// Màn Backup (mục 6.5, 10 của KE-HOACH.md; giao diện bản 2, mục 3.7 của
// M16-thiet-ke.md): thẻ "Backup ngay" cho một site, bảng các bản backup (có
// "Khôi phục" một bản full), rồi thẻ lịch và số bản giữ (chỉ admin). Ba việc
// đi đúng ba chặng như màn Thùng rác: bấm nút → gọi ?dry_run=1 lấy bản xem
// trước (PlanReview) → bấm chạy → máy chủ trả job_id, ở lại màn và thẻ theo
// dõi job báo tiến độ. Cả ba dùng chung một cặp mutation và một chỗ hiện bản
// xem trước, vì cùng lúc chỉ mở một việc.

import { useJobTracker } from "../../components/JobTracker";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { errorMessage, get, post } from "../../api/client";
import { useWho } from "../../api/m13-users";
import { queryKeys, useOverview } from "../../api/queries";
import type { Backup, BackupsOverview, BackupSettingsRequest, JobStarted, Preview } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { ScreenHeader } from "../../components/ScreenHeader";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/backup";
import { strings as khung } from "../../i18n/khung";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";
import { formatWhen } from "../thung-rac/model";
import { CreateNow } from "./Create";
import { BackupList } from "./List";
import { backupSettingsPath, backupsKey, backupsPath, restorePath, siteBackupPath, type SiteState } from "./model";
import { BackupSettingsForm } from "./Settings";

/** Việc đang hỏi: đủ để gọi cả hai chặng (xem trước và chạy) và để chữ nói đúng việc. */
type Req =
  | { action: "restore"; backup: Backup }
  | { action: "create"; domain: string }
  | { action: "settings"; body: BackupSettingsRequest };

const pathOf = (r: Req): string => (r.action === "restore" ? restorePath(r.backup.id) : r.action === "create" ? siteBackupPath(r.domain) : backupSettingsPath);
const bodyOf = (r: Req): BackupSettingsRequest | undefined => (r.action === "settings" ? r.body : undefined);

export default function Backups() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const qc = useQueryClient();
  const { track } = useJobTracker();
  const q = useQuery({ queryKey: backupsKey, queryFn: () => get<BackupsOverview>(backupsPath) });
  // Cùng khoá overview với khung trang: không gọi thêm API. Cho biết site nào đang chạy, đang ở thùng rác.
  const overview = useOverview();
  // Cài đặt lịch và số bản giữ (POST /backups/settings) là route chỉ admin.
  const { isAdmin } = useWho();

  const [filter, setFilter] = useState("");
  const [open, setOpen] = useState<{ req: Req; preview: Preview } | null>(null);
  /** Việc vừa bấm: để chữ nút và lỗi nói đúng việc. */
  const [asked, setAsked] = useState<Req | null>(null);
  const resultRef = useRef<HTMLDivElement>(null);

  const dry = useMutation({
    mutationFn: (req: Req) => post<Preview>(`${pathOf(req)}?dry_run=1`, bodyOf(req)),
    onSuccess: (preview, req) => setOpen({ req, preview }),
  });
  const run = useMutation({
    mutationFn: (req: Req) => post<JobStarted>(pathOf(req), bodyOf(req)),
    onSuccess: (job) => {
      // Danh sách, cài đặt và số đếm ở menu đều đổi sau job.
      void qc.invalidateQueries({ queryKey: backupsKey });
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      // Ở lại trang đang dùng; thẻ theo dõi ở góc dưới báo tiến độ và tải lại dữ liệu khi job xong.
      track(job.job_id);
      setOpen(null);
    },
  });

  // Có bản xem trước hoặc lỗi mới thì đưa người dùng tới đó, vì nó nằm dưới các khối.
  const shown = open ?? (dry.isError ? asked : null);
  useEffect(() => {
    if (shown) {
      resultRef.current?.scrollIntoView({ block: "start" });
      resultRef.current?.focus();
    }
  }, [shown]);

  // Site đang chạy (cho "Backup ngay") và tình trạng của từng domain (cho "Khôi phục").
  // Chưa đọc được overview thì coi mọi domain là đang chạy: nút vẫn hiện và máy chủ tự từ chối nếu sai.
  const { active, stateOf } = useMemo(() => {
    const rows = overview.data?.sites ?? null;
    const status = new Map(rows?.map((r) => [r.site.domain, r.site.status]) ?? []);
    return {
      active: (rows ?? []).filter((r) => r.site.status === "active").map((r) => r.site.domain).sort(),
      stateOf: (domain: string): SiteState => (rows === null ? "active" : status.get(domain) === "active" ? "active" : status.get(domain) === "trashed" ? "trashed" : "gone"),
    };
  }, [overview.data]);

  function ask(req: Req) {
    setOpen(null);
    run.reset();
    setAsked(req);
    dry.mutate(req);
  }

  function close() {
    setOpen(null);
    setAsked(null);
    dry.reset();
    run.reset();
  }

  const pendingReq = dry.isPending ? asked : null;

  const errorHeading = (r: Req): string => (r.action === "restore" ? t.restoreErrorHeading(r.backup.domain) : r.action === "create" ? t.createErrorHeading(r.domain) : t.settingsErrorHeading);

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />

      {(q.isPending || overview.isPending) && (
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {q.isError && (
        <Card>
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {q.isSuccess && !overview.isPending && (
        <>
          <CreateNow domains={active} pendingDomain={pendingReq?.action === "create" ? pendingReq.domain : null} anyPending={dry.isPending} onCreate={(domain) => ask({ action: "create", domain })} />

          <BackupList
            items={q.data.items}
            filter={filter}
            onFilter={setFilter}
            stateOf={stateOf}
            pendingId={pendingReq?.action === "restore" ? pendingReq.backup.id : null}
            anyPending={dry.isPending}
            onRestore={(backup) => ask({ action: "restore", backup })}
          />

          {dry.isError && asked && (
            <div ref={resultRef} tabIndex={-1} className="outline-offset-0">
              <ErrorNote error={dry.error} heading={errorHeading(asked)} checksHeading={t.checksHeading} />
              <div className="pt-[12px]">
                <Button onClick={close}>{k.cancel}</Button>
              </div>
            </div>
          )}

          {open && (
            <div ref={resultRef} tabIndex={-1} className="flex flex-col gap-[12px] outline-offset-0">
              {open.req.action === "restore" && <p className="m-0 max-w-[72ch] font-semibold">{t.restoreWarning(open.req.backup.domain, formatWhen(open.req.backup.created, lang))}</p>}
              <PlanReview preview={open.preview} onRun={() => run.mutate(open.req)} running={run.isPending} error={run.error} onCancel={close} />
            </div>
          )}

          {isAdmin && (
            <BackupSettingsForm
              key={`${q.data.settings.keep}|${q.data.settings.at}`}
              settings={q.data.settings}
              pending={pendingReq?.action === "settings"}
              anyPending={dry.isPending}
              onPreview={(body) => ask({ action: "settings", body })}
            />
          )}
        </>
      )}
    </>
  );
}
