// Màn Bảo mật (mục 6.8 và mục 10 của KE-HOACH.md): trang chỉ đọc là chính.
// Mỗi mục bảo mật một dòng trạng thái kèm lệnh đã dùng để kiểm (GET
// /api/v1/security, đúng thứ tự máy chủ trả). Chỉ hai dòng có việc làm được:
//   sec.ufw      chưa đạt → "Bật tường lửa"
//   sec.fail2ban chưa đạt → "Cài fail2ban"
// Cả hai theo một nhịp: bấm → POST ?dry_run=1 trả Preview → PlanReview (hỏi
// lại vì bước bật ufw nguy hiểm) → POST thật trả job_id → chuyển tới /jobs/:id.
// Hai dòng SSH chỉ cảnh báo, không có nút: sửa sai là mất đường vào máy.

import { useJobTracker } from "../../components/JobTracker";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { errorMessage, get, post } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { CheckResult, JobStarted, Preview, SecurityStatus } from "../../api/types";
import { Button } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bao-mat";
import { strings as khung } from "../../i18n/khung";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";
import { CheckRow, type RowAction } from "./CheckRow";

const securityKey = ["security"] as const;

/** Hai việc làm được từ trang này: id dòng kiểm → đường dẫn API. */
type Kind = "ufw" | "fail2ban";
const actionOf: Record<string, Kind> = { "sec.ufw": "ufw", "sec.fail2ban": "fail2ban" };

/** Hai dòng SSH: chỉ cảnh báo, không bao giờ có nút sửa. */
const readOnlyIds = new Set(["sec.ssh.root", "sec.ssh.password"]);

/**
 * Các cổng "ufw allow <cổng>/tcp" của bản xem trước. Lấy từ lệnh của bước, kể
 * cả bước đã đúng sẵn (done): người dùng cần thấy ĐỦ danh sách cổng được giữ
 * mở, không chỉ phần sẽ thêm.
 */
function portsOf(p: Preview): string[] {
  const ports: string[] = [];
  for (const s of p.steps) {
    for (const c of s.info.commands ?? []) {
      const m = /\bufw\s+allow\s+(\d{1,5}\/tcp)\b/.exec(c);
      if (m?.[1] && !ports.includes(m[1])) ports.push(m[1]);
    }
  }
  return ports;
}

export default function BaoMat() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({ queryKey: securityKey, queryFn: () => get<SecurityStatus>("/api/v1/security") });

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />
      {q.isPending && <p className="mt-[20px] mb-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div className="pt-[20px]">
          <p className="m-0">{errorMessage(q.error, lang)}</p>
          <Button className="mt-[12px]" onClick={() => void q.refetch()}>
            {k.retry}
          </Button>
        </div>
      )}
      {q.isSuccess && <Results results={q.data.results} refetching={q.isFetching} refetchFailed={q.isRefetchError} onRecheck={() => void q.refetch()} />}
    </>
  );
}

type ResultsProps = {
  results: CheckResult[];
  refetching: boolean;
  refetchFailed: boolean;
  onRecheck: () => void;
};

// Tách khỏi BaoMat để trạng thái mở/đóng của từng dòng khởi tạo một lần từ dữ
// liệu đã có (dòng chưa đạt mở sẵn), và giữ nguyên khi "Kiểm lại".
function Results({ results, refetching, refetchFailed, onRecheck }: ResultsProps) {
  const t = useStrings(strings);
  const qc = useQueryClient();
  const { track } = useJobTracker();

  const [open, setOpen] = useState<Set<string>>(() => new Set(results.filter((c) => c.status === "fail" || c.status === "warn").map((c) => c.id)));
  const toggle = (id: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  /** Việc đang xem trước. "Chạy" gửi đúng việc này. */
  const [kind, setKind] = useState<Kind | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const previewRef = useRef<HTMLDivElement>(null);

  const dry = useMutation({
    mutationFn: (k: Kind) => post<Preview>(`/api/v1/security/${k}?dry_run=1`),
    onSuccess: (p, k) => {
      setKind(k);
      setPreview(p);
    },
  });
  const run = useMutation({
    mutationFn: (k: Kind) => post<JobStarted>(`/api/v1/security/${k}`),
    onSuccess: (job) => {
      void qc.invalidateQueries({ queryKey: securityKey });
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
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

  function start(k: Kind) {
    // Bản xem trước của việc kia không còn đúng nữa: bỏ nó trước khi dựng bản mới.
    setPreview(null);
    setKind(null);
    run.reset();
    dry.mutate(k);
  }

  function cancel() {
    const k = kind;
    setPreview(null);
    setKind(null);
    run.reset();
    dry.reset();
    if (k) document.getElementById(`act-${k}`)?.focus();
  }

  function actionFor(c: CheckResult): RowAction | undefined {
    const k = actionOf[c.id];
    if (!k) return undefined;
    const errored = dry.isError && dry.variables === k;
    return {
      kind: k,
      label: k === "ufw" ? t.ufwButton : t.fail2banButton,
      hint: k === "ufw" ? t.ufwHint : t.fail2banHint,
      pending: dry.isPending && dry.variables === k,
      disabled: dry.isPending || run.isPending,
      onClick: () => start(k),
      error: errored ? <ErrorNote error={dry.error} heading={t.previewError} checksHeading={t.checksHeading} /> : undefined,
    };
  }

  const ports = kind === "ufw" && preview ? portsOf(preview) : [];

  return (
    <>
      <section aria-labelledby="bm-ds" className="pt-[26px]">
        <div className="flex flex-wrap items-center justify-between gap-x-[16px] gap-y-[8px]">
          <h2 id="bm-ds" className="m-0 text-[16px] font-semibold">
            {t.listHeading}
          </h2>
          <Button onClick={onRecheck} disabled={refetching}>
            {refetching ? t.rechecking : t.recheck}
          </Button>
        </div>
        <p className="mt-[8px] mb-0 max-w-[720px] text-muted">{t.readOnlyNote}</p>
        {refetchFailed && (
          <p role="alert" className="mt-[10px] mb-0">
            {t.recheckFailed}
          </p>
        )}
        {results.length === 0 ? (
          <p className="mt-[14px] mb-0 text-muted">{t.empty}</p>
        ) : (
          <ul aria-label={t.listLabel} className="m-0 mt-[10px] list-none border-t border-hair p-0">
            {results.map((c) => (
              <CheckRow key={c.id} check={c} open={open.has(c.id)} onToggle={() => toggle(c.id)} action={actionFor(c)} noFix={readOnlyIds.has(c.id)} />
            ))}
          </ul>
        )}
      </section>

      {preview && kind && (
        <div ref={previewRef} tabIndex={-1} className="outline-offset-0">
          {ports.length > 0 && (
            <section aria-labelledby="bm-cong" className="pt-[26px]">
              <h2 id="bm-cong" className="m-0 font-mono text-[11px] font-normal tracking-[0.08em] text-muted">
                {t.portsLabel}
              </h2>
              <p className="mt-[8px] mb-0">{t.portsLead}</p>
              <ul className="m-0 mt-[8px] flex list-none flex-wrap gap-x-[16px] gap-y-[6px] p-0">
                {ports.map((p) => (
                  <li key={p}>
                    <MonoText>{p}</MonoText>
                  </li>
                ))}
              </ul>
              <p className="mt-[8px] mb-0 font-medium">{t.portsWarn}</p>
            </section>
          )}
          <PlanReview preview={preview} onRun={() => run.mutate(kind)} running={run.isPending} error={run.error} onCancel={cancel} />
        </div>
      )}
    </>
  );
}
