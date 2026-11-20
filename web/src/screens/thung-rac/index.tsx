// Màn Thùng rác (mục 6.4, 10 của KE-HOACH.md): các site đã xoá, mỗi site có
// Khôi phục và Xoá hẳn, cùng khối đổi số ngày giữ. Hai việc kia đi đúng ba
// chặng như mọi thao tác: bấm nút → gọi ?dry_run=1 lấy bản xem trước
// (PlanReview) → bấm chạy → máy chủ trả job_id, chuyển tới /jobs/:id.
// Xoá hẳn có thêm một dòng cảnh báo mất DB và file trên bản xem trước; hộp hỏi
// lại lúc bấm chạy là của PlanReview, hiện vì các bước xoá DB, file, user là
// Danger.

import { useJobTracker } from "../../components/JobTracker";
import { BulkBar, useSelection, type Selection } from "../../components/Bulk";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { del, errorMessage, get, post } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { JobStarted, Preview, TrashOverview } from "../../api/types";
import { Button } from "../../components/Button";
import { ScreenHeader } from "../../components/ScreenHeader";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/thung-rac";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";
import { purgePath, restorePath, trashKey, trashPath } from "./model";
import { Retention } from "./Retention";
import { TrashTable, type TrashAction } from "./Sites";

/** Bản xem trước đang mở: của việc gì, trên site nào. */
type Open = { action: TrashAction; domain: string; preview: Preview };

export default function Trash() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const qc = useQueryClient();
  const { track } = useJobTracker();
  const q = useQuery({ queryKey: trashKey, queryFn: () => get<TrashOverview>(trashPath) });

  const [open, setOpen] = useState<Open | null>(null);
  /** Việc vừa bấm: để chữ nút và lỗi nói đúng site, đúng việc. */
  const [asked, setAsked] = useState<{ action: TrashAction; domain: string } | null>(null);
  const resultRef = useRef<HTMLDivElement>(null);

  const dry = useMutation({
    mutationFn: ({ action, domain }: { action: TrashAction; domain: string }) =>
      action === "restore" ? post<Preview>(`${restorePath(domain)}?dry_run=1`) : del<Preview>(`${purgePath(domain)}?dry_run=1`),
    onSuccess: (preview, v) => setOpen({ ...v, preview }),
  });
  const run = useMutation({
    mutationFn: ({ action, domain }: { action: TrashAction; domain: string }) => (action === "restore" ? post<JobStarted>(restorePath(domain)) : del<JobStarted>(purgePath(domain))),
    onSuccess: (job) => {
      // Danh sách thùng rác và số đếm ở menu (đọc từ overview) đều đổi sau job.
      void qc.invalidateQueries({ queryKey: trashKey });
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      // Ở lại trang đang dùng; thẻ theo dõi ở góc dưới báo tiến độ và tải lại dữ liệu khi job xong.
      track(job.job_id);
      setOpen(null);
    },
  });

  // Có bản xem trước hoặc lỗi mới thì đưa người dùng tới đó, vì nó nằm dưới bảng.
  const shown = open ?? (dry.isError ? asked : null);
  useEffect(() => {
    if (shown) {
      resultRef.current?.scrollIntoView({ block: "start" });
      resultRef.current?.focus();
    }
  }, [shown]);

  function ask(action: TrashAction, domain: string) {
    setOpen(null);
    run.reset();
    setAsked({ action, domain });
    dry.mutate({ action, domain });
  }

  function close() {
    setOpen(null);
    setAsked(null);
    dry.reset();
    run.reset();
  }

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />

      {q.isPending && <p className="mt-[26px] mb-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div className="pt-[26px]">
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </div>
      )}
      {q.isSuccess && (
        <>
          <section aria-labelledby="trash-list">
            <h2 id="trash-list" className="m-0 pt-[26px] pb-[14px] text-[16px] font-semibold">
              {t.listHeading}
            </h2>
            <TrashBulk domains={q.data.sites.map((x) => x.domain)}>
              {(sel) => <TrashTable sites={q.data.sites} pending={dry.isPending ? asked : null} onAct={ask} sel={sel} />}
            </TrashBulk>
          </section>

          <Retention days={q.data.days} />

          {dry.isError && asked && (
            <div ref={resultRef} tabIndex={-1} className="pt-[20px] outline-offset-0">
              <ErrorNote
                error={dry.error}
                heading={asked.action === "restore" ? t.restoreErrorHeading(asked.domain) : t.purgeErrorHeading(asked.domain)}
                checksHeading={t.checksHeading}
              />
              <div className="pt-[12px]">
                <Button onClick={close}>{k.cancel}</Button>
              </div>
            </div>
          )}

          {open && (
            <div ref={resultRef} tabIndex={-1} className="outline-offset-0">
              {open.action === "purge" && (
                // Chữ đậm, không dấu đỏ: đỏ chỉ dành cho tình trạng máy (mục 2 của GIAO-DIEN.md), còn đây là lời cảnh báo.
                <p role="alert" className="m-0 pt-[26px] font-semibold">
                  {t.purgeWarning(open.domain)}
                </p>
              )}
              <PlanReview preview={open.preview} onRun={() => run.mutate({ action: open.action, domain: open.domain })} running={run.isPending} error={run.error} onCancel={close} />
            </div>
          )}
        </>
      )}
    </>
  );
}

/** Khôi phục hay xoá hẳn nhiều site trong thùng rác một lần (mỗi site một job, lần lượt). */
function TrashBulk({ domains, children }: { domains: string[]; children: (sel: Selection) => ReactNode }) {
  const t = useStrings(strings);
  const sel = useSelection(domains);
  return (
    <>
      <BulkBar
        sel={sel}
        unit={t.unitSite}
        actions={[
          {
            key: "restore",
            label: t.bulkRestore,
            confirmTitle: t.bulkRestoreTitle,
            confirmBody: <p className="m-0">{t.bulkRestoreBody}</p>,
            confirmLabel: t.bulkRestore,
            run: (d) => post<JobStarted>(restorePath(d)),
          },
          {
            key: "purge",
            label: t.bulkPurge,
            confirmTitle: t.bulkPurgeTitle,
            // Chữ đậm, không đỏ: đỏ chỉ dành cho tình trạng máy (mục 2 của GIAO-DIEN.md).
            confirmBody: <p className="m-0 font-semibold">{t.bulkPurgeBody}</p>,
            confirmLabel: t.bulkPurge,
            run: (d) => del<JobStarted>(purgePath(d)),
          },
        ]}
      />
      {children(sel)}
    </>
  );
}
