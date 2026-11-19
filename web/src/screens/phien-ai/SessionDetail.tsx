// /ai/:id: một phiên AI (mục 8.1 ý 3 và mục 10 của KE-HOACH.md). Cho người vận
// hành thấy ĐÚNG thứ đã gửi cho AI (bản đã che) và thứ AI đã đọc, để kiểm lại
// khi có chuyện. Từ trên xuống: thông tin phiên, từng lượt (câu hỏi, ngữ cảnh,
// bản hướng dẫn, câu trả lời, các lần AI gọi công cụ trong lượt), lần gọi công
// cụ không thuộc lượt nào (phiên của agent nối thẳng), Plan đã đề xuất.
//
// Lượt đang chạy: nghe luồng SSE của lượt để chữ hiện dần; luồng đóng thì đọc
// lại phiên để mọi thứ khác (lần gọi công cụ, Plan, trạng thái) cũng mới.

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";
import { Link, useParams } from "react-router";
import { ApiError, errorMessage, get, subscribeAITurn } from "../../api/client";
import type { AISessionDetail, AITurn, PendingPlan } from "../../api/types";
import { AnswerText } from "../../ai/AnswerText";
import { extractLinks } from "../../ai/views";
import { Button, buttonClass } from "../../components/Button";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { MonoText } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { Tag } from "../../components/Tag";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/phien-ai";
import { formatDateTime } from "../lich-su/format";
import { CallList } from "./CallList";
import { entryText } from "./entry";

const labelClass = "font-mono text-[11px] tracking-[0.08em] text-muted";

export function SessionDetail() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const { id = "" } = useParams();
  const q = useQuery({
    queryKey: ["ai", "session", id],
    queryFn: () => get<AISessionDetail>(`/api/v1/ai/sessions/${encodeURIComponent(id)}`),
    // Còn lượt chạy thì lần gọi công cụ và Plan còn tăng: đọc lại cho người đang xem.
    refetchInterval: (query) => (query.state.data?.turns.some((x) => x.status === "running") ? 4000 : false),
  });

  if (q.isPending) {
    return (
      <>
        <ScreenHeader title={t.title} />
        <p className="mt-[26px] mb-0 text-muted">{k.loading}</p>
      </>
    );
  }
  if (q.isError) {
    const missing = q.error instanceof ApiError && q.error.status === 404;
    return (
      <>
        <ScreenHeader title={t.title} />
        <div className="pt-[26px]" role="alert">
          <p className="m-0">{missing ? t.notFound(id) : `${t.detailFailed} ${errorMessage(q.error, lang)}`}</p>
          <div className="mt-[10px]">
            {missing ? (
              <Link to="/ai" className={buttonClass("secondary")}>
                {t.backToList}
              </Link>
            ) : (
              <Button onClick={() => void q.refetch()}>{k.retry}</Button>
            )}
          </div>
        </div>
      </>
    );
  }
  return <DetailView data={q.data} />;
}

function DetailView({ data }: { data: AISessionDetail }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const { session, turns, calls, plans } = data;
  // Lần gọi công cụ của lượt nào hiện dưới lượt đó; phần còn lại (agent nối thẳng không có lượt) hiện riêng.
  const seqs = new Set(turns.map((x) => x.seq));
  const loose = calls.filter((c) => !seqs.has(c.seq));

  return (
    <>
      <ScreenHeader title={t.title} summary={t.detailSummary} />

      <dl className="m-0 mt-[26px] grid grid-cols-2 gap-x-[16px] gap-y-[12px] border-t border-hair pt-[14px] sm:flex sm:flex-wrap sm:gap-x-[32px]">
        <Fact label={t.fieldEntry}>{entryText(t, session)}</Fact>
        <Fact label={t.fieldSession}>
          <MonoText>{session.id}</MonoText>
        </Fact>
        <Fact label={t.fieldCreated}>
          <span title={session.created} className="font-mono text-[13px]">
            {formatDateTime(session.created, lang)}
          </span>
        </Fact>
        <Fact label={t.fieldLast}>
          <span title={session.last} className="font-mono text-[13px]">
            {formatDateTime(session.last, lang)}
          </span>
        </Fact>
      </dl>

      {session.untrusted && (
        <p role="note" className="mt-[14px] mb-0 max-w-[760px] border border-ink p-[12px]">
          {t.untrustedWarning}
        </p>
      )}
      {session.entry === "mcp" && <p className="mt-[14px] mb-0 max-w-[760px] text-muted">{t.mcpNote}</p>}

      {session.entry === "panel" && turns.length === 0 && <p className="mt-[26px] mb-0 text-muted">{t.noTurns}</p>}
      {turns.map((turn) => (
        <TurnBlock key={turn.seq} sessionId={session.id} turn={turn} calls={calls.filter((c) => c.seq === turn.seq)} />
      ))}

      {(session.entry === "mcp" || loose.length > 0) && (
        <section aria-labelledby="goi-cong-cu" className="pt-[26px]">
          <h2 id="goi-cong-cu" className="m-0 text-[16px] font-semibold">
            {t.callsHeading}
          </h2>
          <p className="mt-[6px] mb-[10px] text-muted">{t.callsHint}</p>
          <CallList calls={loose} />
        </section>
      )}

      <section aria-labelledby="plan-de-xuat" className="pt-[26px]">
        <h2 id="plan-de-xuat" className="m-0 pb-[10px] text-[16px] font-semibold">
          {t.plansHeading}
        </h2>
        {plans.length === 0 ? (
          <p className="m-0 border-t border-hair py-[10px] text-muted">{t.noPlans}</p>
        ) : (
          <ul aria-label={t.plansLabel} className="m-0 list-none border-b border-hair p-0">
            {plans.map((p) => (
              <PlanRow key={p.id} plan={p} />
            ))}
          </ul>
        )}
      </section>
    </>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <dt className={labelClass}>{label}</dt>
      <dd className="m-0 mt-[4px]">{children}</dd>
    </div>
  );
}

/** Câu trả lời đang hiện dần của một lượt "running"; null khi lượt không chạy hoặc chưa có chữ nào. */
function useLiveAnswer(sessionId: string, seq: number, running: boolean): string | null {
  const qc = useQueryClient();
  const [answer, setAnswer] = useState<string | null>(null);
  useEffect(() => {
    if (!running) {
      setAnswer(null);
      return;
    }
    // text và done mang TOÀN BỘ câu trả lời tới lúc đó nên chỉ cần thay, nối lại luồng cũng vô hại.
    // Mọi sự kiện cuối (done, error, refused) đóng luồng: đọc lại phiên để lấy trạng thái và lời lỗi.
    return subscribeAITurn(sessionId, seq, {
      onEvent: (ev) => {
        if ((ev.kind === "text" || ev.kind === "done") && ev.text !== undefined) setAnswer(ev.text);
      },
      onClose: () => void qc.invalidateQueries({ queryKey: ["ai", "session", sessionId] }),
    });
  }, [sessionId, seq, running, qc]);
  return answer;
}

function TurnBlock({ sessionId, turn, calls }: { sessionId: string; turn: AITurn; calls: AISessionDetail["calls"] }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const [full, setFull] = useState(false);
  const running = turn.status === "running";
  const live = useLiveAnswer(sessionId, turn.seq, running);
  const answer = running ? (live ?? turn.answer) : turn.answer;
  const links = turn.status === "done" ? extractLinks(answer, lang) : [];
  const headId = `luot-${turn.seq}`;
  const promptId = `luot-${turn.seq}-prompt`;

  return (
    <section aria-labelledby={headId} className="mt-[26px] flex flex-col gap-[18px] border-t border-ink pt-[16px]">
      <h2 id={headId} className="m-0 flex flex-wrap items-baseline gap-x-[12px] text-[16px] font-semibold">
        {t.turnHeading(turn.seq)}
        <span className="text-[13px] font-normal text-muted">{t.turnStatus[turn.status]}</span>
        <span title={turn.started} className="font-mono text-[12px] font-normal text-muted">
          {formatDateTime(turn.started, lang)}
        </span>
      </h2>

      <div>
        <p className={`m-0 mb-[4px] ${labelClass}`}>{t.questionLabel}</p>
        <p className="m-0 text-[14px] leading-[1.5] whitespace-pre-wrap [overflow-wrap:anywhere]">{turn.question}</p>
        <p className="mt-[4px] mb-0 text-[12px] text-muted">{t.maskedNote}</p>
      </div>

      <div>
        <p className={`m-0 mb-[6px] ${labelClass}`}>{t.chipsLabel}</p>
        {turn.chips.length === 0 ? (
          <p className="m-0 text-muted">{t.noChips}</p>
        ) : (
          <ul className="m-0 list-none border-b border-hair p-0">
            {turn.chips.map((c) => (
              <li key={c.key} className="border-t border-hair">
                <details>
                  <summary className="flex min-h-[44px] cursor-pointer list-none items-center py-[7px] font-mono text-[12px] sm:min-h-[36px] [&::-webkit-details-marker]:hidden">{tx(c.label, lang)}</summary>
                  <div className="pb-[10px]">
                    <EvidenceBlock text={c.content} label={null} />
                  </div>
                </details>
              </li>
            ))}
          </ul>
        )}
        {turn.playbook && (
          <p className="mt-[10px] mb-0 flex flex-wrap items-baseline gap-x-[10px] gap-y-[4px]">
            <span className={labelClass}>{t.playbookLabel}</span>
            <Tag>{turn.playbook}</Tag>
          </p>
        )}
        <div className="mt-[10px]">
          <button type="button" aria-expanded={full} aria-controls={promptId} onClick={() => setFull(!full)} className={buttonClass("secondary")}>
            {full ? t.hidePrompt : t.viewPrompt}
          </button>
          {full && (
            <div id={promptId} className="mt-[8px]">
              <EvidenceBlock text={turn.prompt} label={t.promptLabel} />
            </div>
          )}
        </div>
      </div>

      {calls.length > 0 && (
        <div>
          <p className={`m-0 mb-[6px] ${labelClass}`}>{t.callsOfTurn}</p>
          <CallList calls={calls} />
        </div>
      )}

      {turn.status !== "refused" && (
        <div>
          <p className={`m-0 mb-[4px] ${labelClass}`}>{t.answerLabel}</p>
          {answer ? <AnswerText text={answer} /> : <p className="m-0 text-muted">{running ? t.working : t.noAnswer}</p>}
          <p className="mt-[4px] mb-0 text-[12px] text-muted">{t.answerNote}</p>
        </div>
      )}
      {turn.status === "refused" && (
        <p role="status" className="m-0 max-w-[760px] border border-ink p-[12px]">
          {t.refused(turn.error ?? "")}
        </p>
      )}
      {turn.status === "error" && (
        <p role="alert" className="m-0 max-w-[760px]">
          {t.failed(turn.error ?? "")}
        </p>
      )}

      {links.length > 0 && (
        <div>
          <p className={`m-0 mb-[8px] ${labelClass}`}>{t.goTo}</p>
          <div className="flex flex-wrap gap-[8px]">
            {links.map((l) => (
              <Link key={l.path} to={l.path} className={buttonClass("secondary")}>
                {l.label}
              </Link>
            ))}
          </div>
        </div>
      )}
    </section>
  );
}

function PlanRow({ plan }: { plan: PendingPlan }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  return (
    <li className="flex flex-wrap items-baseline gap-x-[16px] gap-y-[4px] border-t border-hair py-[10px]">
      <span title={plan.created} className="flex-[0_0_110px] font-mono text-[12px] text-muted">
        {formatDateTime(plan.created, lang)}
      </span>
      <span className="min-w-0 flex-[1_1_240px]">
        <Link to={`/plans/${encodeURIComponent(plan.id)}`} aria-label={`${t.planOpen(plan.id)}: ${tx(plan.title, lang)}`}>
          {tx(plan.title, lang)}
        </Link>
        {plan.untrusted && <span className="ml-[10px] text-[12px] text-muted">{t.planUntrusted}</span>}
      </span>
      <Tag>{t.planStatus[plan.status]}</Tag>
    </li>
  );
}
