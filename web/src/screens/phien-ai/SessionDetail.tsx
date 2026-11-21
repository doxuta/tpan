// /ai/:id: một phiên AI (mục 8.1 ý 3 và mục 10 của KE-HOACH.md). Cho người vận
// hành thấy ĐÚNG thứ đã gửi cho AI (bản đã che) và thứ AI đã đọc, để kiểm lại
// khi có chuyện. Từ trên xuống: thông tin phiên, từng lượt (câu hỏi, ngữ cảnh,
// bản hướng dẫn, câu trả lời, các lần AI gọi công cụ trong lượt), lần gọi công
// cụ không thuộc lượt nào (phiên của agent nối thẳng), Plan đã đề xuất. Mỗi
// nhóm là một thẻ (bản 2, mục 3.7 của docs/thay-doi/M16-thiet-ke.md).
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
import { Card } from "../../components/Card";
import { Disclosure } from "../../components/Disclosure";
import { EmptyState } from "../../components/EmptyState";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { Code, MonoText } from "../../components/MonoText";
import { Notice } from "../../components/Notice";
import { ScreenHeader } from "../../components/ScreenHeader";
import { Tag } from "../../components/Tag";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/phien-ai";
import { formatDateTime } from "../lich-su/format";
import { CallList } from "./CallList";
import { entryText } from "./entry";

const labelClass = "m-0 text-[12px] font-medium text-muted";

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
  // Trang chi tiết: "Phiên AI / <mã>"; "Phiên AI" là liên kết về danh sách.
  const crumbs = [{ label: t.title, to: "/ai" }, { label: <Code key="ma">{id}</Code> }];

  if (q.isPending) {
    return (
      <>
        <ScreenHeader crumbs={crumbs} title={t.title} />
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      </>
    );
  }
  if (q.isError) {
    const missing = q.error instanceof ApiError && q.error.status === 404;
    return (
      <>
        <ScreenHeader crumbs={crumbs} title={t.title} />
        <Card>
          <div role="alert">
            <p className="mt-0 mb-[12px]">{missing ? t.notFound(id) : `${t.detailFailed} ${errorMessage(q.error, lang)}`}</p>
            {missing ? (
              <Link to="/ai" className={buttonClass("secondary")}>
                {t.backToList}
              </Link>
            ) : (
              <Button onClick={() => void q.refetch()}>{k.retry}</Button>
            )}
          </div>
        </Card>
      </>
    );
  }
  return <DetailView data={q.data} crumbs={crumbs} />;
}

function DetailView({ data, crumbs }: { data: AISessionDetail; crumbs: { label: ReactNode; to?: string }[] }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const { session, turns, calls, plans } = data;
  // Lần gọi công cụ của lượt nào hiện dưới lượt đó; phần còn lại (agent nối thẳng không có lượt) hiện riêng.
  const seqs = new Set(turns.map((x) => x.seq));
  const loose = calls.filter((c) => !seqs.has(c.seq));

  return (
    <>
      <ScreenHeader crumbs={crumbs} title={t.title} summary={t.detailSummary} />

      <Card>
        <dl className="m-0 grid grid-cols-2 gap-x-[16px] gap-y-[12px] sm:flex sm:flex-wrap sm:gap-x-[32px]">
          <Fact label={t.fieldEntry}>{entryText(t, session)}</Fact>
          <Fact label={t.fieldSession}>
            <MonoText>{session.id}</MonoText>
          </Fact>
          <Fact label={t.fieldCreated}>
            <span title={session.created}>{formatDateTime(session.created, lang)}</span>
          </Fact>
          <Fact label={t.fieldLast}>
            <span title={session.last}>{formatDateTime(session.last, lang)}</span>
          </Fact>
        </dl>
      </Card>

      {session.untrusted && (
        <Card>
          <div role="note">
            <Notice kind="warn">{t.untrustedWarning}</Notice>
          </div>
        </Card>
      )}
      {session.entry === "mcp" && (
        <Card>
          <p className="m-0 text-muted">{t.mcpNote}</p>
        </Card>
      )}

      {session.entry === "panel" && turns.length === 0 && (
        <Card>
          <EmptyState>{t.noTurns}</EmptyState>
        </Card>
      )}
      {turns.map((turn) => (
        <TurnBlock key={turn.seq} sessionId={session.id} turn={turn} calls={calls.filter((c) => c.seq === turn.seq)} />
      ))}

      {(session.entry === "mcp" || loose.length > 0) && (
        <Card title={t.callsHeading} titleId="goi-cong-cu">
          <p className="mt-0 mb-[10px] text-muted">{t.callsHint}</p>
          <CallList calls={loose} />
        </Card>
      )}

      <Card title={t.plansHeading} titleId="plan-de-xuat">
        {plans.length === 0 ? (
          <EmptyState>{t.noPlans}</EmptyState>
        ) : (
          <ul aria-label={t.plansLabel} className="m-0 list-none p-0">
            {plans.map((p) => (
              <PlanRow key={p.id} plan={p} />
            ))}
          </ul>
        )}
      </Card>
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

  return (
    <Card
      title={t.turnHeading(turn.seq)}
      titleId={headId}
      count={
        <>
          {t.turnStatus[turn.status]} · <span title={turn.started}>{formatDateTime(turn.started, lang)}</span>
        </>
      }
    >
      <div className="flex flex-col gap-[18px]">
        <div>
          <p className={`${labelClass} mb-[4px]`}>{t.questionLabel}</p>
          <p className="m-0 text-[14px] leading-[1.5] whitespace-pre-wrap [overflow-wrap:anywhere]">{turn.question}</p>
          <p className="mt-[4px] mb-0 text-[12px] text-muted">{t.maskedNote}</p>
        </div>

        <div>
          <p className={`${labelClass} mb-[2px]`}>{t.chipsLabel}</p>
          {turn.chips.length === 0 ? (
            <p className="m-0 text-muted">{t.noChips}</p>
          ) : (
            <ul className="m-0 list-none p-0">
              {turn.chips.map((c) => (
                <li key={c.key} className="border-t border-hair first:border-t-0">
                  <Disclosure flush summary={<span className="text-[13px]">{tx(c.label, lang)}</span>}>
                    <EvidenceBlock text={c.content} label={null} clamp={20} />
                  </Disclosure>
                </li>
              ))}
            </ul>
          )}
          {turn.playbook && (
            <p className="mt-[10px] mb-0 flex flex-wrap items-center gap-x-[10px] gap-y-[4px]">
              <span className={labelClass}>{t.playbookLabel}</span>
              <Tag>{turn.playbook}</Tag>
            </p>
          )}
          <div className="mt-[6px] border-t border-hair">
            <Disclosure flush open={full} onOpenChange={setFull} summary={<span className="font-medium">{full ? t.hidePrompt : t.viewPrompt}</span>}>
              <EvidenceBlock text={turn.prompt} label={t.promptLabel} clamp={20} />
            </Disclosure>
          </div>
        </div>

        {calls.length > 0 && (
          <div>
            <p className={`${labelClass} mb-[2px]`}>{t.callsOfTurn}</p>
            <CallList calls={calls} />
          </div>
        )}

        {turn.status !== "refused" && (
          <div>
            <p className={`${labelClass} mb-[4px]`}>{t.answerLabel}</p>
            {answer ? <AnswerText text={answer} /> : <p className="m-0 text-muted">{running ? t.working : t.noAnswer}</p>}
            <p className="mt-[4px] mb-0 text-[12px] text-muted">{t.answerNote}</p>
          </div>
        )}
        {turn.status === "refused" && (
          <Notice kind="warn" role="status">
            {t.refused(turn.error ?? "")}
          </Notice>
        )}
        {turn.status === "error" && (
          <Notice kind="fail" role="alert">
            {t.failed(turn.error ?? "")}
          </Notice>
        )}

        {links.length > 0 && (
          <div>
            <p className={`${labelClass} mb-[8px]`}>{t.goTo}</p>
            <div className="flex flex-wrap gap-[8px]">
              {links.map((l) => (
                <Link key={l.path} to={l.path} className={buttonClass("secondary")}>
                  {l.label}
                </Link>
              ))}
            </div>
          </div>
        )}
      </div>
    </Card>
  );
}

function PlanRow({ plan }: { plan: PendingPlan }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  return (
    <li className="flex flex-wrap items-center gap-x-[16px] gap-y-[4px] border-t border-hair py-[10px] first:border-t-0">
      <span title={plan.created} className="flex-[0_0_110px] text-[13px] text-muted">
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
