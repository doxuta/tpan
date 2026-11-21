// Ba cách sửa một sự cố đang mở (mục 6.7 của KE-HOACH.md), cộng một lựa chọn
// riêng cho file lệch:
//   1. Sửa tự động: xem trước (dry_run) → PlanReview → chạy → chuyển sang /jobs/:id.
//   2. Sửa tay: các lệnh để người vận hành tự gõ, dòng cuối là lệnh kiểm lại.
//   3. Hỏi AI (M8): mở ngăn AI với câu hỏi chẩn đoán soạn sẵn, người xem trước rồi tự gửi.
//   +  Phép kiểm lệch ("drift:<path>"): nhận bản sửa tay làm chuẩn.
// Máy chỉ đổi qua bản xem trước đã duyệt (nguyên tắc ở mục 10 của KE-HOACH.md).

import { useJobTracker } from "../../components/JobTracker";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useAi } from "../../ai/AiProvider";
import { post } from "../../api/client";
import { useWho } from "../../api/m13-users";
import type { AcceptFileRequest, CheckResult, FixRequest, Incident, JobStarted, Preview } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { CommandBlock } from "../../components/CommandBlock";
import { Dialog } from "../../components/Dialog";
import { MonoText } from "../../components/MonoText";
import { Notice } from "../../components/Notice";
import { tx, useLang, useStrings } from "../../i18n";
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
  const { lang } = useLang();
  const { openAI } = useAi();
  // Sửa tự động, nhận bản sửa tay và hỏi AI là route chỉ admin (docs/KE-HOACH.md mục 6.12 ("Đã chốt khi làm M13")): user thường chỉ có sửa tay.
  const { isAdmin } = useWho();
  const qc = useQueryClient();
  const { track } = useJobTracker();

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
      // Ở lại trang đang dùng; thẻ theo dõi ở góc dưới báo tiến độ và tải lại dữ liệu khi job xong.
      track(job.job_id);
      setPreview(null);
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

  // Bản 2: mỗi cách sửa là một thẻ (tiêu đề h2 mang đúng id cũ). Bản xem trước (PlanReview) tự là
  // một thẻ, nên đứng ngay dưới thẻ "Sửa tự động" chứ không nằm trong nó.
  return (
    <>
      <div>
        <h2 id="cach-sua" className="m-0 text-[16px] font-semibold">
          {t.fixHeading}
        </h2>
        <p className="mt-[4px] mb-0 text-muted">{isAdmin ? t.fixHint : t.fixHintUser}</p>
      </div>

      {isAdmin && autoFix && (
        <>
          <Card title={t.autoHeading} titleId="sua-tu-dong">
            <p className="m-0 text-muted">{t.autoBody}</p>
            {isDrift && <p className="mt-[6px] mb-0 text-muted">{t.autoDrift}</p>}
            <div className="pt-[12px]">
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
          </Card>
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
        </>
      )}

      <Card title={t.manualHeading} titleId="sua-tay">
        {manual.length === 0 ? <p className="m-0 text-muted">{t.manualNone}</p> : <CommandBlock commands={manual} />}
      </Card>

      {isAdmin && isDrift && driftPath && (
        <Card title={t.acceptHeading} titleId="nhan-ban-sua-tay">
          <p className="m-0 text-muted">{t.acceptBody}</p>
          <p className="mt-[8px] mb-0">
            <MonoText>{driftPath}</MonoText>
          </p>
          <div className="pt-[12px]">
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
            <div className="pt-[12px]">
              <Notice kind="pass" role="status">
                {t.accepted}
              </Notice>
            </div>
          )}
          <Dialog open={confirming} onOpenChange={setConfirming} title={t.acceptTitle} description={t.acceptDescription} confirmLabel={t.acceptConfirm} busy={accept.isPending} onConfirm={() => accept.mutate()}>
            <MonoText>{driftPath}</MonoText>
          </Dialog>
        </Card>
      )}

      {/* Hỏi AI: cùng lối vào với nút "Chẩn đoán" ở đầu trang. Ngăn AI mở ra ở bước xem trước, người xem rồi tự gửi. */}
      {isAdmin && (
        <Card title={t.aiHeading} titleId="hoi-ai">
          <p className="m-0 text-muted">{t.aiBody}</p>
          <div className="pt-[12px]">
            <Button
              onClick={() =>
                openAI({
                  mode: "diagnose",
                  question: t.diagnoseQuestion(tx(incident.title, lang)),
                  pointer: { kind: "incident", ref: String(incident.id), label: tx(incident.title, lang) },
                })
              }
            >
              {t.aiButton}
            </Button>
          </div>
        </Card>
      )}
    </>
  );
}
