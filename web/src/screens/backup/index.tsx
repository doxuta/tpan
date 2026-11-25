// Màn Backup (mục 6.5, 10 của KE-HOACH.md; giao diện bản 2, mục 3.7 của
// M16-thiet-ke.md): thẻ "Backup ngay" cho một site, bảng các bản backup (có
// "Khôi phục" một bản full), rồi thẻ lịch và số bản giữ (chỉ admin). Ba việc
// đi đúng ba chặng như màn Thùng rác: bấm nút → gọi ?dry_run=1 lấy bản xem
// trước (PlanReview) → bấm chạy → máy chủ trả job_id, ở lại màn và thẻ theo
// dõi job báo tiến độ. Cả ba dùng chung một cặp mutation và một chỗ hiện bản
// xem trước, vì cùng lúc chỉ mở một việc.
//
// M18 thêm hai việc với kho ngoài máy, cũng đi qua bản xem trước: "Đẩy lên kho"
// (máy chủ nhận vào hàng chờ, không có job_id nên màn hiện câu `note` của máy
// chủ) và "Tải về máy" (thường có job_id; không có thì một lượt đẩy đang giữ
// khoá kho, và bảng tự tải lại mỗi 15 giây tới khi dòng hết là "chỉ trên kho",
// tối đa 30 phút). Khôi phục một bản chỉ
// còn trên kho thì hỏi tải về trước; tải xong màn chỉ báo, không tự khôi phục,
// vì khôi phục ghi đè site nên người dùng phải tự xem bản xem trước của nó.

import { useJobTracker } from "../../components/JobTracker";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { del, errorMessage, get, post } from "../../api/client";
import { useWho } from "../../api/m13-users";
import { pullPath, pushPath } from "../../api/m18-types";
import { queryKeys, useOverview } from "../../api/queries";
import type { Backup, BackupsOverview, BackupSettingsRequest, Preview } from "../../api/types";
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
import { backupPath, backupSettingsPath, backupsKey, backupsPath, restorePath, siteBackupPath, type SiteState } from "./model";
import { BackupSettingsForm } from "./Settings";
import { TargetCard } from "./Target";
import { useStorageFlow, type Started } from "./StorageFlow";

/**
 * Việc đang hỏi: đủ để gọi cả hai chặng (xem trước và chạy) và để chữ nói đúng việc.
 * fromRestore: tải về do người bấm "Khôi phục" một bản chỉ còn trên kho; xong thì báo "Đã tải về".
 */
type Req =
  | { action: "restore"; backup: Backup }
  | { action: "delete"; backup: Backup }
  | { action: "push"; backup: Backup }
  | { action: "pull"; backup: Backup; fromRestore?: boolean }
  | { action: "create"; domain: string }
  | { action: "settings"; body: BackupSettingsRequest };

const pathOf = (r: Req): string => {
  switch (r.action) {
    case "restore":
      return restorePath(r.backup.id);
    case "delete":
      return backupPath(r.backup.id);
    case "push":
      return pushPath(r.backup.id);
    case "pull":
      return pullPath(r.backup.id);
    case "create":
      return siteBackupPath(r.domain);
    case "settings":
      return backupSettingsPath;
  }
};
const bodyOf = (r: Req): BackupSettingsRequest | undefined => (r.action === "settings" ? r.body : undefined);

export default function Backups() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const qc = useQueryClient();
  const { track } = useJobTracker();
  // Phần kho ngoài máy dùng chung với tab Backup của trang site (StorageFlow.tsx): hộp hỏi tải về trước,
  // câu báo, tự tải lại bảng, và việc đã đặt đích chưa.
  const flow = useStorageFlow((backup) => ask({ action: "pull", backup, fromRestore: true }));
  const q = useQuery({ queryKey: backupsKey, queryFn: () => get<BackupsOverview>(backupsPath), refetchInterval: flow.poll });
  // Cùng khoá overview với khung trang: không gọi thêm API. Cho biết site nào đang chạy, đang ở thùng rác.
  const overview = useOverview();
  // Cài đặt lịch và số bản giữ (POST /backups/settings) và mọi việc với kho là route chỉ admin.
  const { isAdmin } = useWho();

  const [filter, setFilter] = useState("");
  const [open, setOpen] = useState<{ req: Req; preview: Preview } | null>(null);
  /** Việc vừa bấm: để chữ nút và lỗi nói đúng việc. */
  const [asked, setAsked] = useState<Req | null>(null);
  const resultRef = useRef<HTMLDivElement>(null);

  const dry = useMutation({
    mutationFn: (req: Req) => (req.action === "delete" ? del<Preview>(`${pathOf(req)}?dry_run=1`) : post<Preview>(`${pathOf(req)}?dry_run=1`, bodyOf(req))),
    onSuccess: (preview, req) => setOpen({ req, preview }),
  });
  const run = useMutation({
    mutationFn: (req: Req) => (req.action === "delete" ? del<Started>(pathOf(req)) : post<Started>(pathOf(req), bodyOf(req))),
    onSuccess: (res, req) => {
      // Danh sách, cài đặt và số đếm ở menu đều đổi sau job.
      void qc.invalidateQueries({ queryKey: backupsKey });
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      // Ở lại trang đang dùng; thẻ theo dõi ở góc dưới báo tiến độ và tải lại dữ liệu khi job xong.
      if (res.job_id) track(res.job_id);
      setOpen(null);
      if (req.action === "push" || req.action === "pull") flow.ran(req, res);
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
    flow.clear();
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

  /** "Khôi phục" một hàng. Bản chỉ còn trên kho thì flow.restore lo (hộp hỏi tải về, hoặc nhắn nhờ admin) và không gọi API. */
  function restore(backup: Backup) {
    if (flow.restore(backup)) close();
    else ask({ action: "restore", backup });
  }

  const pendingReq = dry.isPending ? asked : null;

  const errorHeading = (r: Req): string => {
    switch (r.action) {
      case "restore":
        return t.restoreErrorHeading(r.backup.domain);
      case "delete":
        return t.deleteErrorHeading(r.backup.domain);
      case "push":
        return t.pushErrorHeading(r.backup.domain);
      case "pull":
        return t.pullErrorHeading(r.backup.domain);
      case "create":
        return t.createErrorHeading(r.domain);
      case "settings":
        return t.settingsErrorHeading;
    }
  };
  /** Một dòng đậm trên bản xem trước nói việc sẽ làm với bản nào. */
  const previewLead = (r: Req): string | null => {
    switch (r.action) {
      case "restore":
        return stateOf(r.backup.domain) === "gone"
          ? t.restoreRecreateWarning(r.backup.domain, formatWhen(r.backup.created, lang))
          : t.restoreWarning(r.backup.domain, formatWhen(r.backup.created, lang));
      case "delete":
        return t.deleteWarning(r.backup.domain, formatWhen(r.backup.created, lang), r.backup.remote === "stored");
      case "push":
        return t.pushWarning(r.backup.domain, formatWhen(r.backup.created, lang));
      case "pull":
        return t.pullWarning(r.backup.domain, formatWhen(r.backup.created, lang));
      default:
        return null;
    }
  };
  const lead = open ? previewLead(open.req) : null;

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
            pending={pendingReq && (pendingReq.action === "restore" || pendingReq.action === "delete" || pendingReq.action === "push" || pendingReq.action === "pull") ? { action: pendingReq.action, id: pendingReq.backup.id } : null}
            anyPending={dry.isPending}
            onRestore={restore}
            onDelete={(backup) => ask({ action: "delete", backup })}
            isAdmin={isAdmin}
            targetConfigured={flow.configured}
            onPush={(backup) => ask({ action: "push", backup })}
            onPull={(backup) => ask({ action: "pull", backup })}
          />

          {flow.render(q.data.items)}

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
              {lead && <p className="m-0 max-w-[72ch] font-semibold">{lead}</p>}
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

          {/* M18: đích ngoài máy (kho S3), chỉ admin: thẻ tự lo dữ liệu, bản xem trước và job. */}
          {isAdmin && <TargetCard />}
        </>
      )}
    </>
  );
}
