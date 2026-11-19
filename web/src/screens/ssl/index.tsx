// Màn SSL (mục 6.3 và mục 10 của KE-HOACH.md): cấu hình Let's Encrypt, rồi bảng
// site kèm tình trạng chứng chỉ. Xin chứng chỉ đi đúng ba chặng: bấm nút ở hàng
// site → POST /sites/{domain}/ssl?dry_run=1 trả bản xem trước (PlanReview) →
// bấm chạy → POST thật trả job_id, chuyển tới /jobs/:id. Máy chủ từ chối (409
// kèm checks: thiếu email, DNS chưa trỏ về...) thì hiện câu của nó và các phép
// kiểm chưa đạt ngay dưới bảng.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { errorMessage, get, post } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { JobStarted, Preview, SSLOverview, SSLSiteRow } from "../../api/types";
import { Button } from "../../components/Button";
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
  const navigate = useNavigate();
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
      void navigate(`/jobs/${encodeURIComponent(job.job_id)}`);
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

      {q.isPending && <p className="mt-[26px] mb-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div className="pt-[26px]">
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </div>
      )}
      {q.isSuccess && (
        <>
          <Settings config={q.data.config} />

          <section aria-labelledby="ssl-sites">
            <h2 id="ssl-sites" className="m-0 pt-[26px] pb-[14px] text-[16px] font-semibold">
              {t.sitesHeading}
            </h2>
            <SiteTable rows={q.data.sites} pending={dry.isPending ? failed : null} onIssue={issue} />
          </section>

          {dry.isError && failed && (
            <div ref={resultRef} tabIndex={-1} className="pt-[20px] outline-offset-0">
              <ErrorNote error={dry.error} heading={t.errorHeading(failed)} checksHeading={t.checksHeading} />
              <div className="pt-[12px]">
                <Button onClick={close}>{k.cancel}</Button>
              </div>
            </div>
          )}

          {open && (
            <div ref={resultRef} tabIndex={-1} className="outline-offset-0">
              {open.reissue && <p className="m-0 pt-[26px] text-muted">{t.reissueNote}</p>}
              <PlanReview preview={open.preview} onRun={() => run.mutate(open.domain)} running={run.isPending} error={run.error} onCancel={close} />
            </div>
          )}
        </>
      )}
    </>
  );
}
