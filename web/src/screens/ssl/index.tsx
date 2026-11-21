// Màn SSL (mục 6.3 và mục 10 của KE-HOACH.md; giao diện bản 2, mục 3.7 của
// M16-thiet-ke.md): bảng site kèm tình trạng chứng chỉ, rồi thẻ cấu hình Let's
// Encrypt (chỉ admin). Xin chứng chỉ đi đúng ba chặng: bấm nút ở hàng site →
// POST /sites/{domain}/ssl?dry_run=1 trả bản xem trước (PlanReview) → bấm chạy →
// POST thật trả job_id, ở lại màn và thẻ theo dõi job báo tiến độ. Máy chủ từ
// chối (409 kèm checks: thiếu email, DNS chưa trỏ về...) thì hiện câu của nó và
// các phép kiểm chưa đạt ngay dưới bảng.

import { useJobTracker } from "../../components/JobTracker";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { errorMessage, get, post } from "../../api/client";
import { useWho } from "../../api/m13-users";
import { queryKeys } from "../../api/queries";
import type { JobStarted, Preview, SSLOverview, SSLSiteRow } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { ScreenHeader } from "../../components/ScreenHeader";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/ssl";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";
import { sslIssuePath, sslKey } from "./model";
import { Settings } from "./Settings";
import { SiteTable } from "./Sites";

/** Bản xem trước đang mở: của site nào, và là xin lần đầu hay xin lại. */
type Open = { domain: string; reissue: boolean; preview: Preview };

export default function SSLScreen() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const qc = useQueryClient();
  const { isAdmin } = useWho();
  const { track } = useJobTracker();
  const q = useQuery({ queryKey: sslKey, queryFn: () => get<SSLOverview>("/api/v1/ssl") });

  const [open, setOpen] = useState<Open | null>(null);
  /** Site vừa bấm mà bị từ chối, để lỗi nói đúng tên site. */
  const [failed, setFailed] = useState<string | null>(null);
  const resultRef = useRef<HTMLDivElement>(null);

  const dry = useMutation({
    mutationFn: (row: SSLSiteRow) => post<Preview>(`${sslIssuePath(row.domain)}?dry_run=1`),
    onSuccess: (preview, row) => setOpen({ domain: row.domain, reissue: row.ssl, preview }),
  });
  const run = useMutation({
    mutationFn: (domain: string) => post<JobStarted>(sslIssuePath(domain)),
    onSuccess: (job) => {
      void qc.invalidateQueries({ queryKey: sslKey });
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      // Ở lại trang đang dùng; thẻ theo dõi ở góc dưới báo tiến độ và tải lại dữ liệu khi job xong.
      track(job.job_id);
      setOpen(null);
    },
  });

  // Có bản xem trước hoặc lỗi mới thì đưa người dùng tới đó, vì nó nằm dưới bảng.
  const shown = open ?? (dry.isError ? failed : null);
  useEffect(() => {
    if (shown) {
      resultRef.current?.scrollIntoView({ block: "start" });
      resultRef.current?.focus();
    }
  }, [shown]);

  function issue(row: SSLSiteRow) {
    setOpen(null);
    run.reset();
    setFailed(row.domain);
    dry.mutate(row);
  }

  function close() {
    setOpen(null);
    setFailed(null);
    dry.reset();
    run.reset();
  }

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
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {q.isSuccess && (
        <>
          <SiteTable rows={q.data.sites} pending={dry.isPending ? failed : null} onIssue={issue} />

          {dry.isError && failed && (
            <div ref={resultRef} tabIndex={-1} className="outline-offset-0">
              <ErrorNote error={dry.error} heading={t.errorHeading(failed)} checksHeading={t.checksHeading} />
              <div className="pt-[12px]">
                <Button onClick={close}>{k.cancel}</Button>
              </div>
            </div>
          )}

          {open && (
            <div ref={resultRef} tabIndex={-1} className="flex flex-col gap-[12px] outline-offset-0">
              {open.reissue && <p className="m-0 text-muted">{t.reissueNote}</p>}
              <PlanReview preview={open.preview} onRun={() => run.mutate(open.domain)} running={run.isPending} error={run.error} onCancel={close} />
            </div>
          )}

          {/* POST /ssl/settings là route chỉ admin. */}
          {isAdmin && <Settings config={q.data.config} />}
        </>
      )}
    </>
  );
}
