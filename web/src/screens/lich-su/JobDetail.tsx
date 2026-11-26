// /jobs/:id: một job, từng bước, output chạy trực tiếp và bằng chứng (mục 10 của
// KE-HOACH.md, màn 6). Dữ liệu và log trực tiếp ở useJob.ts, cách dựng khối từ
// sự kiện ở jobModel.ts. Ở đây chỉ trình bày.
//
// Bản 2: một dãy thẻ. Thẻ thông tin (dòng "Đang chạy" nằm ở chân thẻ), thẻ lỗi
// khi job hỏng, bảng từng site (job hàng loạt), rồi ba thẻ danh sách: Các bước,
// Hoàn tác, Kiểm chứng. Mỗi bước là một Disclosure: bước đang chạy và bước hỏng
// mở sẵn, bước đạt đóng sẵn. Output của bước đang chạy không clamp (người xem
// đang đọc nó chạy); bước đã xong clamp 20 dòng.

import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Link, useParams } from "react-router";
import { useAi } from "../../ai/AiProvider";
import { errorMessage } from "../../api/client";
import { useWho } from "../../api/m13-users";
import type { CheckResult, Status } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { DbSecretBox } from "../../components/DbSecretBox";
import { Disclosure } from "../../components/Disclosure";
import { EmptyState } from "../../components/EmptyState";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { Code, MonoText } from "../../components/MonoText";
import { Notice } from "../../components/Notice";
import { ScreenHeader } from "../../components/ScreenHeader";
import { StatusLine, StatusMark } from "../../components/StatusMark";
import { tx, useLang, useStrings, type Lang } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/lich-su";
import { formatClock, formatDateTime, formatDuration } from "./format";
import { buildView, elapsedMs, type Block, type JobView } from "./jobModel";
import { SecretBox } from "./SecretBox";
import { UnitTable } from "./UnitTable";
import { useAdminSecret, useDbSecrets, useJob } from "./useJob";

type T = (typeof strings)["vi"];

/** Số dòng log hiện cho mỗi khối; quá số này thì hiện đuôi và có nút "Hiện hết". */
const tailLines = 400;

/** Bằng chứng dài hơn chừng này dòng thì chỉ vẽ chừng đó dòng đầu (trừ output đang chạy). */
const clampLines = 20;

/** Dấu của trạng thái job: đang chạy là "chưa kiểm được" (như các bước đang chạy), hai trạng thái lỗi là lỗi. */
function jobMark(status: string): Status {
  if (status === "succeeded") return "pass";
  if (status === "rolled_back" || status === "undo_failed") return "fail";
  return "unknown";
}

export function JobDetail() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const { id = "" } = useParams();
  const { query, job, chain, events, running, stream, reconnect } = useJob(id);
  const { secrets, dismiss } = useAdminSecret(id);
  const dbSecret = useDbSecrets(id);
  const view = useMemo(() => buildView(events), [events]);
  const now = useNow(running);
  const [open, setOpen] = useState<Record<string, boolean>>({});
  useFollowLog(running, events.length);

  const crumbs = [{ label: t.title, to: "/jobs" }, { label: <Code>{id.slice(0, 8)}</Code> }];

  if (query.isPending) {
    return (
      <>
        <ScreenHeader crumbs={crumbs} title={t.jobTitle} summary={t.jobSummary} />
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      </>
    );
  }
  if (query.isError || !job) {
    return (
      <>
        <ScreenHeader crumbs={crumbs} title={t.jobTitle} summary={t.jobSummary} />
        <Card>
          <p className="mt-0 mb-[12px]">{errorMessage(query.error, lang)}</p>
          <Button onClick={() => void query.refetch()}>{k.retry}</Button>
        </Card>
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
      <ScreenHeader crumbs={crumbs} title={tx(job.title, lang)} summary={t.jobSummary} />
      <p role="status" className="sr-only">
        {announce}
      </p>

      {secrets.map((s, i) => (
        <SecretBox key={s.url} secret={s} many={secrets.length > 1} focus={i === 0} onDismiss={() => dismiss(i)} />
      ))}
      {dbSecret.secrets.map((s, i) => (
        <DbSecretBox key={s.db} secret={s} focus={i === 0 && secrets.length === 0} onDismiss={() => dbSecret.dismiss(i)} />
      ))}

      {chain.length > 0 && (
        <Card title={t.chainHeading} titleId="chain-heading">
          <p className="m-0 mb-[10px] text-[13px] text-muted">{t.chainHint}</p>
          <ul className="m-0 flex list-none flex-col gap-[8px] p-0">
            {chain.map((c) => (
              <li key={c.id} className="flex flex-wrap items-baseline gap-x-[12px] gap-y-[2px]">
                <StatusLine status={jobMark(c.status)}>{c.status in t.status ? t.status[c.status as keyof T["status"]] : c.status}</StatusLine>
                <Link to={`/jobs/${encodeURIComponent(c.id)}`} className="[overflow-wrap:anywhere]">
                  {tx(c.title, lang)}
                </Link>
              </li>
            ))}
          </ul>
        </Card>
      )}

      <Card footer={running && stream !== "lost" ? t.liveNote : undefined}>
        <dl className="m-0 grid grid-cols-2 gap-x-[16px] gap-y-[12px] sm:flex sm:flex-wrap sm:gap-x-[40px]">
          <Fact label={t.fieldStatus}>
            <StatusLine status={jobMark(status)}>{statusText}</StatusLine>
          </Fact>
          <Fact label={t.fieldJob}>
            <MonoText>{job.id}</MonoText>
          </Fact>
          <Fact label={t.fieldTarget}>
            {/* Tên miền dùng chữ thường (GIAO-DIEN mục 4); tên DB, user hệ thống vẫn mono, bấm là chép. */}
            {/^[a-z0-9.-]+\.[a-z]{2,}$/i.test(job.target) ? <span className="[overflow-wrap:anywhere]">{job.target}</span> : <MonoText>{job.target}</MonoText>}
          </Fact>
          <Fact label={t.colWho}>
            <span className={job.actor ? "" : "text-muted"}>{job.actor || t.whoCli}</span>
          </Fact>
          <Fact label={t.fieldStarted}>
            <span title={job.started}>{formatDateTime(job.started, lang)}</span>
          </Fact>
          {ms !== null && <Fact label={t.fieldDuration}>{formatDuration(ms, t, lang)}</Fact>}
        </dl>
      </Card>

      {stream === "lost" && (
        <Card>
          <Notice kind="warn">
            <p className="m-0">{t.streamLost}</p>
            <div className="mt-[10px]">
              <Button onClick={reconnect}>{t.reconnect}</Button>
            </div>
          </Notice>
        </Card>
      )}

      {failed && (
        <Card>
          <Notice kind="fail">
            <p className="m-0 font-medium">{batch ? (status === "undo_failed" ? t.unitsFailedUndo : t.unitsFailedAll) : failureText(t, view, status, failedStepOf(view))}</p>
            {/* Bước hỏng đã mang đúng thông báo này trong khối của nó; chỉ hiện ở đây khi không có bước nào để chỉ tới.
                Job hàng loạt thì luôn hiện: lỗi của lần reload hoàn tác cuối chỉ nằm ở đây, không thuộc site nào. */}
            {job.error && (batch || !failedStepOf(view)) && (
              <div className="mt-[10px]">
                <EvidenceBlock text={job.error} label={t.errorLabel} clamp={clampLines} />
              </div>
            )}
          </Notice>
        </Card>
      )}

      {batch && <UnitTable units={view.units} />}

      <Card
        title={t.stepsHeading}
        titleId="steps-heading"
        actions={allBlocks.length > 0 ? <Button size="sm" onClick={toggleAll}>{allOpen ? t.closeAll : t.openAll}</Button> : undefined}
      >
        {view.steps.length === 0 ? (
          <EmptyState>{t.noSteps}</EmptyState>
        ) : (
          <ol className="m-0 list-none p-0">
            {view.steps.map((b, i) => (
              <BlockItem
                key={i}
                jobId={job.id}
                label={t.stepNumber(i + 1)}
                block={b}
                text={t.stepState[b.state]}
                mark={b.state === "running" ? "unknown" : b.state === "failed" ? "fail" : "pass"}
                open={isOpen(`s${i}`, b)}
                onOpenChange={(v) => setOpen((o) => ({ ...o, [`s${i}`]: v }))}
                lang={lang}
              />
            ))}
          </ol>
        )}
      </Card>

      {view.undo.length > 0 && (
        <Card title={t.undoHeading} titleId="undo-heading">
          <p className="mt-0 mb-[8px] text-muted">{t.undoHint}</p>
          <ol className="m-0 list-none p-0">
            {view.undo.map((b, i) => {
              const n = view.steps.findIndex((s) => s.id === b.id) + 1;
              return (
                <BlockItem
                  key={i}
                  jobId={job.id}
                  label={n > 0 ? t.stepNumber(n) : ""}
                  block={b}
                  text={t.undoState[b.state]}
                  mark={b.state === "running" ? "unknown" : b.state === "failed" ? "fail" : "pass"}
                  open={isOpen(`u${i}`, b)}
                  onOpenChange={(v) => setOpen((o) => ({ ...o, [`u${i}`]: v }))}
                  lang={lang}
                />
              );
            })}
          </ol>
        </Card>
      )}

      {view.checks.length > 0 && (
        <Card title={t.checksHeading} titleId="checks-heading">
          <p className="mt-0 mb-[8px] text-muted">{t.checksHint}</p>
          <ul className="m-0 list-none p-0">
            {view.checks.map((c, i) => (
              <CheckItem key={`${c.id}-${i}`} check={c} lang={lang} />
            ))}
          </ul>
        </Card>
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
      <dt className="text-[12px] font-medium text-muted">{label}</dt>
      <dd className="m-0 mt-[4px]">{children}</dd>
    </div>
  );
}

type BlockItemProps = {
  /** Để nút "Giải thích" chỉ đúng bước: ref là "<job>/<bước>". */
  jobId: string;
  /** "Bước 3"; rỗng nếu không biết. */
  label: string;
  block: Block<string>;
  /** Chữ nói trạng thái, nằm cạnh dấu. */
  text: string;
  mark: "pass" | "fail" | "unknown";
  open: boolean;
  onOpenChange: (open: boolean) => void;
  lang: Lang;
};

/** Một bước (hoặc một lần hoàn tác): một dòng mở ra lệnh và output của nó. */
function BlockItem({ jobId, label, block, text, mark, open, onOpenChange, lang }: BlockItemProps) {
  const t = useStrings(strings);
  const { openAI } = useAi();
  const { isAdmin } = useWho();
  const ms = block.ended ? elapsedMs(block.started, block.ended) : null;
  // Chữ trạng thái nằm ngay cạnh nên dấu để ẩn khỏi trình đọc màn hình (StatusMark.tsx).
  const summary = (
    <span className="flex flex-wrap items-center gap-x-[12px] gap-y-[2px]">
      <span className="inline-flex w-[20px] flex-none justify-center">
        <StatusMark status={mark} />
      </span>
      {label && <span className="text-[13px] text-muted">{label}</span>}
      <Code className="min-w-0">{block.id}</Code>
      <span className="text-[13px] text-muted">{text}</span>
      <span className="ml-auto text-[13px] text-muted tabular-nums">
        {formatClock(block.started, lang)}
        {ms !== null && ` · ${formatDuration(ms, t, lang)}`}
      </span>
    </span>
  );
  return (
    <li className="border-t border-hair first:border-t-0">
      <Disclosure flush heading={3} open={open} onOpenChange={onOpenChange} summary={summary}>
        <div className="pl-[32px]">
          <BlockLog block={block} />
          {/* Chỉ bước hỏng: ngăn AI mở ra với câu soạn sẵn, người xem trước rồi tự gửi. Nút vẫn hiện khi AI tắt; ngăn AI tự chỉ cách nối. */}
          {/* User thường không có ngăn AI (mục 6.12), nên không có nút này. */}
          {isAdmin && block.state === "failed" && (
            <div className="mt-[10px]">
              <Button
                size="sm"
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
      </Disclosure>
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
              <Button size="sm" onClick={() => setAll(true)}>
                {t.showAllLines}
              </Button>
            </div>
          )}
          {/* Output đang chạy không clamp: người xem đang đọc nó chạy. Xong rồi mới thu gọn. */}
          <EvidenceBlock text={shown.join("\n")} label={null} clamp={block.state === "running" ? undefined : clampLines} />
        </div>
      )}
      {block.error && <EvidenceBlock text={block.error} label={t.errorLabel} clamp={clampLines} />}
    </div>
  );
}

/** Một phép kiểm chứng: một dòng mở ra bằng chứng của đúng phép kiểm đó; phép kiểm lỗi mở sẵn. */
function CheckItem({ check, lang }: { check: CheckResult; lang: Lang }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const has = (check.evidence?.length ?? 0) > 0;
  const summary = (
    <span className="flex items-start gap-[12px]">
      <span className="flex h-[21px] w-[20px] flex-none items-center justify-center">
        <StatusMark status={check.status} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block [overflow-wrap:anywhere]">
          <span className="font-medium">{tx(check.title, lang)}</span> <span className="text-[13px] text-muted">{k.status[check.status]}</span>
        </span>
        <span className="block text-[13px] text-muted [overflow-wrap:anywhere]">{tx(check.summary, lang)}</span>
      </span>
    </span>
  );
  return (
    <li className="border-t border-hair first:border-t-0">
      <Disclosure flush heading={3} defaultOpen={check.status === "fail"} summary={summary}>
        <div className="pl-[32px]">{has ? <EvidenceBlock evidence={check.evidence} label={null} clamp={clampLines} /> : <p className="m-0 text-muted">{t.noEvidence}</p>}</div>
      </Disclosure>
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
