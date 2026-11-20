// Khối "Đổi web server" của màn Stack (mục 6.6 và mục 10 của KE-HOACH.md): chuyển
// nginx ⇄ apache (LEMP ⇄ LAMP). Đọc web server đang chạy từ GET /api/v1/stack
// (chung khoá stackKey với Config nên chỉ một lần gọi). Cùng nhịp với Php.tsx:
// bấm → POST /api/v1/stack/switch?dry_run=1 → PlanReview → POST thật →
// /jobs/:id. Máy chủ từ chối (409: stack_same_web, precondition_failed kèm
// checks...) thì ErrorNote hiện câu của nó ngay dưới nút. Lệnh tương đương để
// tự gõ nằm trong CommandBlock, có --dry-run nên chỉ xem trước.

import { useJobTracker } from "../../components/JobTracker";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { errorMessage, get, post } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { JobStarted, Preview, StackStatus, StackSwitchBody } from "../../api/types";
import { Button } from "../../components/Button";
import { CommandBlock } from "../../components/CommandBlock";
import { MonoText } from "../../components/MonoText";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/stack";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";
import { phpKey, stackKey } from "./model";

const path = "/api/v1/stack/switch";
const buttonId = "act-switch";

export function Switch() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({ queryKey: stackKey, queryFn: () => get<StackStatus>("/api/v1/stack") });

  return (
    <section aria-labelledby="st-switch" className="pt-[26px]">
      <h2 id="st-switch" className="m-0 pb-[10px] text-[16px] font-semibold">
        {t.switchHeading}
      </h2>
      {q.isPending && <p className="m-0 border-t border-hair pt-[10px] text-muted">{k.loading}</p>}
      {q.isError && (
        <div className="border-t border-hair pt-[10px]">
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </div>
      )}
      {q.isSuccess && <Loaded status={q.data} />}
    </section>
  );
}

function Loaded({ status }: { status: StackStatus }) {
  const t = useStrings(strings);
  const qc = useQueryClient();
  const { track } = useJobTracker();
  const current = status.config.web;
  const installed = status.installed && (current === "nginx" || current === "apache");
  const target: StackSwitchBody["to"] = current === "nginx" ? "apache" : "nginx";
  const body: StackSwitchBody = { to: target };

  /** Bản xem trước đang mở. "Chạy" gửi đúng thân này. */
  const [open, setOpen] = useState<{ body: StackSwitchBody; preview: Preview } | null>(null);
  const previewRef = useRef<HTMLDivElement>(null);

  const dry = useMutation({
    mutationFn: (b: StackSwitchBody) => post<Preview>(`${path}?dry_run=1`, b),
    onSuccess: (preview, b) => setOpen({ body: b, preview }),
  });
  const run = useMutation({
    mutationFn: (b: StackSwitchBody) => post<JobStarted>(path, b),
    onSuccess: (job) => {
      void qc.invalidateQueries({ queryKey: stackKey });
      void qc.invalidateQueries({ queryKey: phpKey });
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      // Ở lại trang đang dùng; thẻ theo dõi ở góc dưới báo tiến độ và tải lại dữ liệu khi job xong.
      track(job.job_id);
      setOpen(null);
    },
  });

  // Bản xem trước nằm dưới nút: đưa người dùng tới đó.
  useEffect(() => {
    if (open) {
      previewRef.current?.scrollIntoView({ block: "start" });
      previewRef.current?.focus();
    }
  }, [open]);

  function start() {
    // Bản xem trước cũ không còn đúng nữa: bỏ nó trước khi dựng bản mới.
    setOpen(null);
    run.reset();
    dry.mutate(body);
  }

  function cancel() {
    setOpen(null);
    run.reset();
    dry.reset();
    document.getElementById(buttonId)?.focus();
  }

  if (!installed) return <p className="m-0 max-w-[720px] border-t border-hair pt-[10px] text-muted">{t.switchNotInstalled}</p>;

  return (
    <>
      <dl className="m-0 border-t border-hair pt-[12px]">
        <dt className="m-0 font-mono text-[11px] font-normal tracking-[0.08em] text-muted uppercase">{t.switchCurrent}</dt>
        <dd className="m-0 mt-[4px]">
          <MonoText>{current}</MonoText>
        </dd>
      </dl>

      <p className="mt-[14px] mb-[12px] max-w-[720px]">{t.switchHint(current, target)}</p>
      <Button id={buttonId} onClick={start} disabled={dry.isPending || run.isPending}>
        {dry.isPending ? t.previewing : t.switchTo(target)}
      </Button>
      <p className="mt-[10px] mb-0 max-w-[720px] text-muted">{t.previewHint}</p>

      {dry.isError && (
        <div className="pt-[12px]">
          <ErrorNote error={dry.error} heading={t.switchPreviewError(target)} checksHeading={t.checksHeading} />
        </div>
      )}

      <p className="mt-[14px] mb-[10px] max-w-[720px]">{t.switchCommandHint}</p>
      <CommandBlock commands={[t.switchCommand(target)]} />

      {open && (
        <div ref={previewRef} tabIndex={-1} className="outline-offset-0">
          <PlanReview preview={open.preview} onRun={() => run.mutate(open.body)} running={run.isPending} error={run.error} onCancel={cancel} />
        </div>
      )}
    </>
  );
}
