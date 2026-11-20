// /jobs/:id: một job, từng bước, output chạy trực tiếp và bằng chứng (mục 10 của
// KE-HOACH.md, màn 6). Dữ liệu và log trực tiếp ở useJob.ts, cách dựng khối từ
// sự kiện ở jobModel.ts. Ở đây chỉ trình bày.

import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useParams } from "react-router";
import { useAi } from "../../ai/AiProvider";
import { errorMessage } from "../../api/client";
import { useWho } from "../../api/m13-users";
import type { CheckResult } from "../../api/types";
import { Button } from "../../components/Button";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { MonoText } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { StatusMark } from "../../components/StatusMark";
import { tx, useLang, useStrings, type Lang } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/lich-su";
import { formatClock, formatDateTime, formatDuration } from "./format";
import { buildView, elapsedMs, type Block, type JobView } from "./jobModel";
import { DbSecretBox } from "../../components/DbSecretBox";
import { SecretBox } from "./SecretBox";
import { UnitTable } from "./UnitTable";
import { useAdminSecret, useDbSecret, useJob } from "./useJob";

type T = (typeof strings)["vi"];

/** Số dòng log hiện cho mỗi khối; quá số này thì hiện đuôi và có nút "Hiện hết". */
const tailLines = 400;

const labelClass = "font-mono text-[11px] tracking-[0.08em] text-muted";

export function JobDetail() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const { id = "" } = useParams();
  const { query, job, events, running, stream, reconnect } = useJob(id);
  const { secrets, dismiss } = useAdminSecret(id);
  const dbSecret = useDbSecret(id);
  const view = useMemo(() => buildView(events), [events]);
  const now = useNow(running);
  const [open, setOpen] = useState<Record<string, boolean>>({});
  useFollowLog(running, events.length);

  if (query.isPending) {
    return (
      <>
        <ScreenHeader title={<span className="font-mono">{id}</span>} summary={t.jobSummary} />
        <p className="mt-[26px] mb-0 text-muted">{k.loading}</p>
      </>
    );
  }
  if (query.isError || !job) {
    return (
      <>
        <ScreenHeader title={<span className="font-mono">{id}</span>} summary={t.jobSummary} />
        <div className="pt-[26px]">
          <p className="m-0">{errorMessage(query.error, lang)}</p>
          <div className="mt-[10px]">
            <Button onClick={() => void query.refetch()}>{k.retry}</Button>
          </div>
        </div>
      </>
    );
  }

  const status = view.doneStatus ?? job.status;
  const statusText = status in t.status ? t.status[status as keyof T["status"]] : status;
  const end = job.finished ?? view.doneTime ?? (running ? new Date(now).toISOString() : undefined);
  const ms = end ? elapsedMs(job.started, end) : null;
  const failed = status === "rolled_back" || status === "undo_failed";
  const lastStep = view.steps.at(-1);
  // Job hàng loạt: có sự kiện unit.done. Bảng từng site hiện thêm, các phần còn lại giữ nguyên.
  const batch = view.units.length > 0;

  const blockKeys = [...view.steps.map((_, i) => `s${i}`), ...view.undo.map((_, i) => `u${i}`)];
  const isOpen = (key: string, b: Block<string>) => open[key] ?? (b.state === "running" || b.state === "failed");
  const allBlocks = [...view.steps, ...view.undo];
  const allOpen = allBlocks.length > 0 && blockKeys.every((key, i) => isOpen(key, allBlocks[i]!));
  const toggleAll = () => setOpen(Object.fromEntries(blockKeys.map((key) => [key, !allOpen])));

  // Trình đọc màn hình: chỉ báo khi đổi bước và khi job dừng, không đọc cả log.
  const announce = view.doneStatus
    ? t.jobDoneAnnounce(statusText)
    : running && lastStep
      ? t.stepAnnounce(view.steps.length, lastStep.id)
      : "";

  return (
    <>
      <ScreenHeader title={tx(job.title, lang)} summary={t.jobSummary} />
      <p role="status" className="sr-only">
        {announce}
      </p>

      {secrets.map((s, i) => (
        <SecretBox key={s.url} secret={s} many={secrets.length > 1} focus={i === 0} onDismiss={() => dismiss(i)} />
      ))}
      {dbSecret.secret && <DbSecretBox secret={dbSecret.secret} onDismiss={dbSecret.dismiss} />}

      <dl className="m-0 mt-[26px] grid grid-cols-2 gap-x-[16px] gap-y-[12px] border-t border-hair pt-[14px] sm:flex sm:flex-wrap sm:gap-x-[32px]">
        <Fact label={t.fieldStatus}>{statusText}</Fact>
        <Fact label={t.fieldJob}>
          <MonoText>{job.id}</MonoText>
        </Fact>
        <Fact label={t.fieldTarget}>
          <MonoText>{job.target}</MonoText>
        </Fact>
        <Fact label={t.colWho}>
          <span className={`font-mono text-[13px] ${job.actor ? "" : "text-muted"}`}>{job.actor || t.whoCli}</span>
        </Fact>
        <Fact label={t.fieldStarted}>
          <span title={job.started} className="font-mono text-[13px]">
            {formatDateTime(job.started, lang)}
          </span>
        </Fact>
        {ms !== null && (
          <Fact label={t.fieldDuration}>
            <span className="font-mono text-[13px]">{formatDuration(ms, t, lang)}</span>
          </Fact>
        )}
      </dl>

      {running && stream !== "lost" && <p className="mt-[14px] mb-0 text-muted">{t.liveNote}</p>}
      {stream === "lost" && (
        <div className="mt-[14px]">
          <p className="m-0">{t.streamLost}</p>
          <div className="mt-[10px]">
            <Button onClick={reconnect}>{t.reconnect}</Button>
          </div>
        </div>
      )}

      {failed && (
        <div className="mt-[14px]">
          <p className="m-0 font-medium">{batch ? (status === "undo_failed" ? t.unitsFailedUndo : t.unitsFailedAll) : failureText(t, view, status, failedStepOf(view))}</p>
          {/* Bước hỏng đã mang đúng thông báo này trong khối của nó; chỉ hiện ở đây khi không có bước nào để chỉ tới.
              Job hàng loạt thì luôn hiện: lỗi của lần reload hoàn tác cuối chỉ nằm ở đây, không thuộc site nào. */}
          {job.error && (batch || !failedStepOf(view)) && (
            <div className="mt-[10px]">
              <EvidenceBlock text={job.error} label={t.errorLabel} />
            </div>
          )}
        </div>
      )}

      {batch && <UnitTable units={view.units} />}

      <section aria-labelledby="steps-heading" className="pt-[26px]">
        <div className="flex flex-wrap items-baseline justify-between gap-[12px]">
          <h2 id="steps-heading" className="m-0 text-[16px] font-semibold">
            {t.stepsHeading}
          </h2>
          {allBlocks.length > 0 && <Button onClick={toggleAll}>{allOpen ? t.closeAll : t.openAll}</Button>}
        </div>
        {view.steps.length === 0 ? (
          <p className="mt-[10px] mb-0 text-muted">{t.noSteps}</p>
        ) : (
          <ol className="m-0 mt-[10px] list-none border-b border-hair p-0">
            {view.steps.map((b, i) => (
              <BlockItem
                key={i}
                domId={`s${i}`}
                jobId={job.id}
                label={t.stepNumber(i + 1)}
                block={b}
                text={t.stepState[b.state]}
                mark={b.state === "running" ? "unknown" : b.state === "failed" ? "fail" : "pass"}
                open={isOpen(`s${i}`, b)}
                onToggle={() => setOpen((o) => ({ ...o, [`s${i}`]: !isOpen(`s${i}`, b) }))}
                lang={lang}
              />
            ))}
          </ol>
        )}
      </section>

      {view.undo.length > 0 && (
        <section aria-labelledby="undo-heading" className="pt-[26px]">
          <h2 id="undo-heading" className="m-0 text-[16px] font-semibold">
            {t.undoHeading}
          </h2>
          <p className="mt-[6px] mb-0 text-muted">{t.undoHint}</p>
          <ol className="m-0 mt-[10px] list-none border-b border-hair p-0">
            {view.undo.map((b, i) => {
              const n = view.steps.findIndex((s) => s.id === b.id) + 1;
              return (
                <BlockItem
                  key={i}
                  domId={`u${i}`}
                  jobId={job.id}
                  label={n > 0 ? t.stepNumber(n) : ""}
                  block={b}
                  text={t.undoState[b.state]}
                  mark={b.state === "running" ? "unknown" : b.state === "failed" ? "fail" : "pass"}
                  open={isOpen(`u${i}`, b)}
                  onToggle={() => setOpen((o) => ({ ...o, [`u${i}`]: !isOpen(`u${i}`, b) }))}
                  lang={lang}
                />
              );
            })}
          </ol>
        </section>
      )}

      {view.checks.length > 0 && (
        <section aria-labelledby="checks-heading" className="pt-[26px]">
          <h2 id="checks-heading" className="m-0 text-[16px] font-semibold">
            {t.checksHeading}
          </h2>
          <p className="mt-[6px] mb-0 text-muted">{t.checksHint}</p>
          <ul className="m-0 mt-[10px] list-none border-b border-hair p-0">
            {view.checks.map((c, i) => (
              <CheckItem key={`${c.id}-${i}`} check={c} domId={`c${i}`} lang={lang} />
            ))}
          </ul>
        </section>
      )}
    </>
  );
}

/** Bước hỏng nếu có, không thì undefined. */
function failedStepOf(view: JobView): Block<string> | undefined {
  return view.failedIndex >= 0 ? view.steps[view.failedIndex] : undefined;
}

function failureText(t: T, view: JobView, status: string, failedStep: Block<string> | undefined): string {
  const undoFailed = status === "undo_failed";
  if (failedStep) return (undoFailed ? t.failedUndoAt : t.failedAt)(view.failedIndex + 1, failedStep.id);
  if (view.checks.some((c) => c.status === "fail")) return undoFailed ? t.failedUndoVerify : t.failedVerify;
  return t.failedOther;
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <dt className={labelClass}>{label}</dt>
      <dd className="m-0 mt-[4px]">{children}</dd>
    </div>
  );
}

type BlockItemProps = {
  domId: string;
  /** Để nút "Giải thích" chỉ đúng bước: ref là "<job>/<bước>". */
  jobId: string;
  /** "Bước 3"; rỗng nếu không biết. */
  label: string;
  block: Block<string>;
  /** Chữ nói trạng thái, nằm cạnh dấu. */
  text: string;
  mark: "pass" | "fail" | "unknown";
  open: boolean;
  onToggle: () => void;
  lang: Lang;
};

/** Một bước (hoặc một lần hoàn tác): dấu trạng thái là nút mở ra lệnh và output của nó. */
function BlockItem({ domId, jobId, label, block, text, mark, open, onToggle, lang }: BlockItemProps) {
  const t = useStrings(strings);
  const { openAI } = useAi();
  const { isAdmin } = useWho();
  const ms = block.ended ? elapsedMs(block.started, block.ended) : null;
  return (
    <li className="border-t border-hair">
      <h3 className="m-0 text-[14px] font-normal">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={`${domId}-panel`}
          onClick={onToggle}
          className="flex min-h-[44px] w-full flex-wrap items-center gap-x-[12px] gap-y-[2px] py-[8px] text-left sm:min-h-[40px]"
        >
          {/* Chữ trạng thái nằm ngay cạnh nên dấu để ẩn khỏi trình đọc màn hình (StatusMark.tsx). */}
          <span className="inline-flex w-[20px] flex-none justify-center">
            <StatusMark status={mark} />
          </span>
          {label && <span className="font-mono text-[12px] text-muted">{label}</span>}
          <span className="min-w-0 font-mono text-[13px] [overflow-wrap:anywhere]">{block.id}</span>
          <span className="text-[13px] text-muted">{text}</span>
          <span className="ml-auto font-mono text-[12px] text-muted">
            {formatClock(block.started, lang)}
            {ms !== null && ` · ${formatDuration(ms, t, lang)}`}
          </span>
        </button>
      </h3>
      {open && (
        <div id={`${domId}-panel`} className="pb-[12px]">
          <BlockLog block={block} />
          {/* Chỉ bước hỏng: ngăn AI mở ra với câu soạn sẵn, người xem trước rồi tự gửi. Nút vẫn hiện khi AI tắt; ngăn AI tự chỉ cách nối. */}
          {/* User thường không có ngăn AI (mục 6.12), nên không có nút này. */}
          {isAdmin && block.state === "failed" && (
            <div className="mt-[10px]">
              <Button
                title={t.explainStepHint}
                onClick={() =>
                  openAI({
                    mode: "explain",
                    question: t.explainStepQuestion(block.id),
                    pointer: { kind: "job_step", ref: `${jobId}/${block.id}`, label: t.explainStepLabel(block.id) },
                  })
                }
              >
                {t.explain}
              </Button>
            </div>
          )}
        </div>
      )}
    </li>
  );
}

function BlockLog({ block }: { block: Block<string> }) {
  const t = useStrings(strings);
  const [all, setAll] = useState(false);
  const hidden = all ? 0 : Math.max(0, block.lines.length - tailLines);
  const shown = hidden > 0 ? block.lines.slice(hidden) : block.lines;
  return (
    <div className="flex flex-col gap-[10px]">
      {block.lines.length === 0 ? (
        !block.error && <p className="m-0 text-muted">{t.noLog}</p>
      ) : (
        <div>
          {hidden > 0 && (
            <div className="mb-[6px] flex flex-wrap items-center gap-[10px] text-muted">
              <span>{t.hiddenLines(hidden)}</span>
              <Button onClick={() => setAll(true)}>{t.showAllLines}</Button>
            </div>
          )}
          <EvidenceBlock text={shown.join("\n")} label={null} />
        </div>
      )}
      {block.error && <EvidenceBlock text={block.error} label={t.errorLabel} />}
    </div>
  );
}

/** Một phép kiểm chứng: dấu trạng thái là nút mở ra bằng chứng của đúng phép kiểm đó. */
function CheckItem({ check, domId, lang }: { check: CheckResult; domId: string; lang: Lang }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const [open, setOpen] = useState(check.status === "fail");
  const has = (check.evidence?.length ?? 0) > 0;
  return (
    <li className="border-t border-hair">
      <h3 className="m-0 text-[14px] font-normal">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={`${domId}-panel`}
          onClick={() => setOpen((o) => !o)}
          className="flex min-h-[44px] w-full flex-wrap items-center gap-x-[12px] gap-y-[2px] py-[8px] text-left sm:min-h-[40px]"
        >
          <span className="inline-flex w-[20px] flex-none justify-center">
            <StatusMark status={check.status} />
          </span>
          <span className="font-medium">{tx(check.title, lang)}</span>
          <span className="text-[13px] text-muted">{k.status[check.status]}</span>
          <span className="min-w-0 basis-full pl-[32px] text-[13px] text-muted">{tx(check.summary, lang)}</span>
        </button>
      </h3>
      {open && (
        <div id={`${domId}-panel`} className="pb-[12px]">
          {has ? <EvidenceBlock evidence={check.evidence} label={null} /> : <p className="m-0 text-muted">{t.noEvidence}</p>}
        </div>
      )}
    </li>
  );
}

/** Mốc thời gian chạy mỗi giây khi job còn chạy, để "thời gian" nhích theo. */
function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    const h = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(h);
  }, [active]);
  return now;
}

/**
 * Tự cuộn theo log khi đang ở cuối trang (cửa sổ là chỗ cuộn). Người dùng cuộn
 * lên đọc thì dừng theo; cuộn xuống cuối lại thì theo tiếp. Ghi nhớ vị trí
 * TRƯỚC khi nội dung dài ra, cuộn SAU khi vẽ xong.
 */
function useFollowLog(active: boolean, eventCount: number) {
  const atBottom = useRef(true);
  useEffect(() => {
    const update = () => {
      atBottom.current = window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 48;
    };
    update();
    window.addEventListener("scroll", update, { passive: true });
    return () => window.removeEventListener("scroll", update);
  }, []);
  useLayoutEffect(() => {
    if (active && atBottom.current) window.scrollTo(0, document.documentElement.scrollHeight);
  }, [active, eventCount]);
}
