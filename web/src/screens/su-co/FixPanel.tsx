// Ba cách sửa một sự cố đang mở (mục 6.7 của KE-HOACH.md), cộng một lựa chọn
// riêng cho file lệch:
//   1. Sửa tự động: xem trước (dry_run) → PlanReview → chạy → chuyển sang /jobs/:id.
//   2. Sửa tay: các lệnh để người vận hành tự gõ, dòng cuối là lệnh kiểm lại.
//   3. Hỏi AI: chưa có ở bản này, chỉ một ghi chú, không nút giả.
//   +  Phép kiểm lệch ("drift:<path>"): nhận bản sửa tay làm chuẩn.
// Máy chỉ đổi qua bản xem trước đã duyệt (nguyên tắc ở mục 10 của KE-HOACH.md).

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useNavigate } from "react-router";
import { post } from "../../api/client";
import type { AcceptFileRequest, CheckResult, FixRequest, Incident, JobStarted, Preview } from "../../api/types";
import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { MonoText } from "../../components/MonoText";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/su-co";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";
import { refreshIncidents } from "./refresh";

const driftPrefix = "drift:";

type Props = {
  incident: Incident;
  /** Kết quả mới nhất của phép kiểm (hoặc lúc mở nếu không còn được kiểm): nguồn của auto_fix và manual_fix. */
  live: CheckResult;
};

export function FixPanel({ incident, live }: Props) {
  const t = useStrings(strings);
  const qc = useQueryClient();
  const navigate = useNavigate();

  const autoFix = live.auto_fix ?? "";
  const manual = live.manual_fix ?? [];
  const isDrift = incident.check_id.startsWith(driftPrefix);
  const driftPath = isDrift ? incident.check_id.slice(driftPrefix.length) : "";
  const body: FixRequest = { check_id: incident.check_id };

  // ---- sửa tự động ----
  const [preview, setPreview] = useState<Preview | null>(null);
  const previewRef = useRef<HTMLDivElement>(null);
  const dry = useMutation({
    mutationFn: () => post<Preview>("/api/v1/fixes?dry_run=1", body),
    onSuccess: setPreview,
  });
  const run = useMutation({
    mutationFn: () => post<JobStarted>("/api/v1/fixes", body),
    onSuccess: (job) => {
      void refreshIncidents(qc);
      void navigate(`/jobs/${encodeURIComponent(job.job_id)}`);
    },
  });
  useEffect(() => {
    if (preview) {
      previewRef.current?.scrollIntoView({ block: "start" });
      previewRef.current?.focus();
    }
  }, [preview]);

  // ---- nhận bản sửa tay làm chuẩn ----
  const [confirming, setConfirming] = useState(false);
  const accept = useMutation({
    mutationFn: () => post<void>("/api/v1/files/accept", { path: driftPath } satisfies AcceptFileRequest),
    onSuccess: () => {
      setConfirming(false);
      void refreshIncidents(qc);
    },
    // Lỗi thì đóng hộp để câu lỗi hiện ngay dưới nút, không nằm sau lớp mờ.
    onError: () => setConfirming(false),
  });

  return (
    <section aria-labelledby="cach-sua">
      <h2 id="cach-sua" className="m-0 pt-[26px] pb-[4px] text-[16px] font-semibold">
        {t.fixHeading}
      </h2>
      <p className="m-0 pb-[10px] text-muted">{t.fixHint}</p>

      {autoFix && (
        <Option heading={t.autoHeading} id="sua-tu-dong">
          <p className="m-0 text-muted">{t.autoBody}</p>
          {isDrift && <p className="mt-[6px] mb-0 text-muted">{t.autoDrift}</p>}
          <div className="pt-[10px]">
            {/* Nút chính của khối là nút "Chạy" của PlanReview khi đã có bản xem trước, nên lúc đó nút này hạ xuống phụ. */}
            <Button variant={preview ? "secondary" : "primary"} onClick={() => dry.mutate()} disabled={dry.isPending || run.isPending}>
              {dry.isPending ? t.autoPreviewing : t.autoPreview}
            </Button>
          </div>
          {dry.isError && (
            <div className="pt-[12px]">
              <ErrorNote error={dry.error} heading={t.autoPreviewFailed} />
            </div>
          )}
          {preview && (
            <div ref={previewRef} tabIndex={-1} className="outline-offset-0">
              <PlanReview
                preview={preview}
                onRun={() => run.mutate()}
                running={run.isPending}
                error={run.error}
                onCancel={() => {
                  setPreview(null);
                  run.reset();
                }}
              />
            </div>
          )}
        </Option>
      )}

      <Option heading={t.manualHeading} id="sua-tay">
        {manual.length === 0 ? (
          <p className="m-0 text-muted">{t.manualNone}</p>
        ) : (
          <>
            <p className="m-0 text-muted">{t.manualBody}</p>
            <ul className="mt-[8px] mb-0 flex list-none flex-col gap-[6px] p-0">
              {manual.map((line, i) => (
                <li key={i} className="flex flex-wrap items-baseline gap-x-[10px]">
                  <MonoText size={12}>{line}</MonoText>
                  {i === manual.length - 1 && <span className="font-mono text-[11px] tracking-[0.08em] text-muted uppercase">{t.manualRecheckTag}</span>}
                </li>
              ))}
            </ul>
          </>
        )}
      </Option>

      {isDrift && driftPath && (
        <Option heading={t.acceptHeading} id="nhan-ban-sua-tay">
          <p className="m-0 text-muted">{t.acceptBody}</p>
          <p className="mt-[6px] mb-0">
            <MonoText>{driftPath}</MonoText>
          </p>
          <div className="pt-[10px]">
            <Button onClick={() => setConfirming(true)} disabled={accept.isPending}>
              {t.accept}
            </Button>
          </div>
          {accept.isError && (
            <div className="pt-[12px]">
              <ErrorNote error={accept.error} heading={t.acceptFailed} />
            </div>
          )}
          {accept.isSuccess && (
            <p role="status" className="mt-[10px] mb-0 text-muted">
              {t.accepted}
            </p>
          )}
          <Dialog open={confirming} onOpenChange={setConfirming} title={t.acceptTitle} description={t.acceptDescription} confirmLabel={t.acceptConfirm} busy={accept.isPending} onConfirm={() => accept.mutate()}>
            <MonoText>{driftPath}</MonoText>
          </Dialog>
        </Option>
      )}

      {/* Hỏi AI: AI vào ở các mốc sau. Chỉ một ghi chú, không nút giả. */}
      <Option heading={t.aiHeading} id="hoi-ai" muted>
        <p className="m-0 text-muted">{t.aiBody}</p>
      </Option>
    </section>
  );
}

function Option({ heading, id, muted = false, children }: { heading: string; id: string; muted?: boolean; children: ReactNode }) {
  return (
    <div aria-labelledby={id} role="group" className="border-t border-hair py-[14px]">
      <h3 id={id} className={`m-0 mb-[6px] text-[14px] font-semibold ${muted ? "text-muted" : ""}`}>
        {heading}
      </h3>
      {children}
    </div>
  );
}
