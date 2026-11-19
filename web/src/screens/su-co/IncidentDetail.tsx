// /incidents/:id: một sự cố (mục 6.7 và 10 của KE-HOACH.md). Từ trên xuống:
// tên và dòng trạng thái, các thông tin chính, "Kiểm lại", chuyện gì xảy ra
// và nguyên nhân, ba cách sửa (chỉ khi còn mở), bằng chứng lúc mở, kết quả hiện
// tại hoặc bằng chứng lúc đóng, các job sửa. Cách sửa đặt trước bằng chứng vì
// người vào trang một sự cố đang mở cần hành động trước; bằng chứng dài và nằm
// ngay bên dưới để đối chiếu.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { Link, useParams } from "react-router";
import { useAi } from "../../ai/AiProvider";
import { ApiError, errorMessage, get, post } from "../../api/client";
import type { CheckResult, IncidentDetail as IncidentDetailData, Job, HealthReport } from "../../api/types";
import { Button, buttonClass } from "../../components/Button";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { MonoText } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { StatusMark } from "../../components/StatusMark";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/su-co";
import { ErrorNote } from "../site/ErrorNote";
import { formatClock, formatDateTime } from "../lich-su/format";
import { FixPanel } from "./FixPanel";
import { formatSpan, spanMs } from "./format";
import { domainOf, incidentState, Target } from "./parts";
import { refreshIncidents } from "./refresh";

const labelClass = "font-mono text-[11px] tracking-[0.08em] text-muted";

export function IncidentDetail() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const { id = "" } = useParams();
  const q = useQuery({
    queryKey: ["incident", id],
    queryFn: () => get<IncidentDetailData>(`/api/v1/incidents/${encodeURIComponent(id)}`),
    // Còn mở thì health có thể đóng nó bất cứ lúc nào: đọc lại cho người đang xem thấy.
    refetchInterval: (query) => (query.state.data && !query.state.data.incident.closed ? 30_000 : false),
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
    const missing = q.error instanceof ApiError && q.error.code === "incident_not_found";
    return (
      <>
        <ScreenHeader title={t.title} />
        <div className="pt-[26px]">
          <p className="m-0">{missing ? t.notFound(id) : errorMessage(q.error, lang)}</p>
          <div className="mt-[10px]">
            {missing ? (
              <Link to="/incidents" className={buttonClass("secondary")}>
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
  // key theo id: sang sự cố khác thì bản xem trước và trạng thái nút của sự cố cũ không theo sang.
  return <DetailView key={id} data={q.data} />;
}

function DetailView({ data }: { data: IncidentDetailData }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const qc = useQueryClient();
  const { openAI } = useAi();
  const { incident: inc, current, jobs } = data;

  const isOpen = !inc.closed;
  const state = incidentState(inc, t);
  // Kết quả mới nhất nếu còn được kiểm, không thì kết quả lúc mở: nguồn của cách sửa và nguyên nhân.
  const live: CheckResult = current ?? inc.open_result;
  const cause = tx(live.cause, lang) || tx(inc.open_result.cause, lang);
  const duration = formatSpan(spanMs(inc.opened, inc.closed), t);

  const recheck = useMutation({
    mutationFn: () => {
      const domain = domainOf(inc.target);
      return post<HealthReport>(`/api/v1/health/run${domain ? `?domain=${encodeURIComponent(domain)}` : ""}`);
    },
    onSuccess: () => void refreshIncidents(qc),
  });

  return (
    <>
      <ScreenHeader title={tx(inc.title, lang)} summary={inc.closed ? t.detailClosed(formatDateTime(inc.opened, lang), formatDateTime(inc.closed, lang)) : t.detailOpen(formatDateTime(inc.opened, lang))} />

      <dl className="m-0 mt-[26px] grid grid-cols-2 gap-x-[16px] gap-y-[12px] border-t border-hair pt-[14px] sm:flex sm:flex-wrap sm:gap-x-[32px]">
        <Fact label={t.fieldStatus}>
          <span className="inline-flex items-center gap-[8px]">
            <StatusMark status={state.status} label={state.text} />
            {state.text}
          </span>
        </Fact>
        <Fact label={t.fieldTarget}>
          <Target target={inc.target} />
        </Fact>
        <Fact label={t.fieldStage}>{t.stages[inc.stage]}</Fact>
        <Fact label={t.fieldCheck}>
          <MonoText size={12}>{inc.check_id}</MonoText>
        </Fact>
        <Fact label={t.fieldOpened}>
          <span title={inc.opened} className="font-mono text-[13px]">
            {formatDateTime(inc.opened, lang)}
          </span>
        </Fact>
        {inc.closed && (
          <Fact label={t.fieldClosed}>
            <span title={inc.closed} className="font-mono text-[13px]">
              {formatDateTime(inc.closed, lang)}
            </span>
          </Fact>
        )}
        <Fact label={t.fieldDuration}>
          <span className="font-mono text-[13px]">{duration}</span>
        </Fact>
      </dl>

      <div className="pt-[14px]">
        <div className="flex flex-wrap items-center gap-x-[14px] gap-y-[8px]">
          {isOpen && (
            <Button onClick={() => recheck.mutate()} disabled={recheck.isPending}>
              {recheck.isPending ? t.rechecking : t.recheck}
            </Button>
          )}
          {/* Sự cố đã đóng cũng chẩn đoán được ("vì sao nó xảy ra"). Ngăn AI mở ra với câu soạn sẵn, người xem trước rồi tự gửi. Nút vẫn hiện khi AI tắt; ngăn AI tự chỉ cách nối. */}
          <Button
            title={t.diagnoseHint}
            onClick={() =>
              openAI({
                mode: "diagnose",
                question: t.diagnoseQuestion(tx(inc.title, lang)),
                pointer: { kind: "incident", ref: String(inc.id), label: tx(inc.title, lang) },
              })
            }
          >
            {t.diagnose}
          </Button>
          {isOpen && (
            <span aria-live="polite" className="text-[13px] text-muted">
              {recheck.isSuccess ? t.rechecked(formatClock(recheck.data.finished, lang)) : t.recheckHint}
            </span>
          )}
        </div>
        {isOpen && recheck.isError && (
          <div className="pt-[12px]">
            <ErrorNote error={recheck.error} heading={t.recheckFailed} />
          </div>
        )}
      </div>

      <section aria-labelledby="chuyen-gi">
        <h2 id="chuyen-gi" className="m-0 pt-[26px] pb-[6px] text-[16px] font-semibold">
          {t.whatHeading}
        </h2>
        <p className="m-0 font-medium">{tx(inc.open_result.summary, lang)}</p>
        {cause && (
          <div className="pt-[12px]">
            <div className={`mb-[6px] ${labelClass}`}>{t.causeLabel}</div>
            <p className="m-0">{cause}</p>
          </div>
        )}
      </section>

      {isOpen && <FixPanel key={inc.id} incident={inc} live={live} />}

      <section aria-labelledby="bang-chung-mo">
        <h2 id="bang-chung-mo" className="m-0 pt-[26px] pb-[10px] text-[16px] font-semibold">
          {t.evidenceOpenHeading}
        </h2>
        <Evidence result={inc.open_result} />
      </section>

      {isOpen ? (
        <section aria-labelledby="hien-tai">
          <h2 id="hien-tai" className="m-0 pt-[26px] pb-[10px] text-[16px] font-semibold">
            {t.currentHeading}
          </h2>
          {current ? (
            <>
              <ResultLine result={current} checked={t.currentChecked(formatClock(current.checked, lang))} />
              <div className="pt-[10px]">
                <Evidence result={current} />
              </div>
            </>
          ) : (
            <p className="m-0 text-muted">{t.noCurrent}</p>
          )}
        </section>
      ) : (
        <section aria-labelledby="bang-chung-dong">
          <h2 id="bang-chung-dong" className="m-0 pt-[26px] pb-[10px] text-[16px] font-semibold">
            {t.evidenceCloseHeading}
          </h2>
          {inc.close_result ? (
            <>
              <ResultLine result={inc.close_result} />
              <div className="pt-[10px]">
                <Evidence result={inc.close_result} />
              </div>
            </>
          ) : (
            <p className="m-0 text-muted">{t.closedGoneNote}</p>
          )}
        </section>
      )}

      <section aria-labelledby="job-sua">
        <h2 id="job-sua" className="m-0 pt-[26px] pb-[10px] text-[16px] font-semibold">
          {t.jobsHeading}
        </h2>
        {jobs.length === 0 ? (
          <p className="m-0 border-t border-hair py-[10px] text-muted">{t.noJobs}</p>
        ) : (
          <ul aria-label={t.jobsLabel} className="m-0 list-none p-0">
            {jobs.map((j) => (
              <JobRow key={j.id} job={j} />
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

/** Dấu trạng thái và câu kết quả của một lần kiểm, kèm giờ kiểm nếu có. */
function ResultLine({ result, checked }: { result: CheckResult; checked?: string }) {
  const k = useStrings(khung);
  const { lang } = useLang();
  return (
    <p className="m-0 flex items-start gap-[10px]">
      <span className="flex h-[21px] w-[20px] flex-none items-center justify-center">
        <StatusMark status={result.status} label={k.statusLabel(tx(result.title, lang), k.status[result.status])} />
      </span>
      <span className="min-w-0">
        <span className="font-medium">{tx(result.summary, lang)}</span>
        {checked && <span className="ml-[10px] text-[13px] text-muted">{checked}</span>}
      </span>
    </p>
  );
}

function Evidence({ result }: { result: CheckResult }) {
  const t = useStrings(strings);
  return result.evidence && result.evidence.length > 0 ? <EvidenceBlock evidence={result.evidence} label={null} /> : <p className="m-0 text-muted">{t.noEvidence}</p>;
}

function JobRow({ job }: { job: Job }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  return (
    <li className="flex flex-wrap items-baseline gap-x-[16px] gap-y-[4px] border-t border-hair py-[10px]">
      <Link to={`/jobs/${encodeURIComponent(job.id)}`} className="flex-[0_0_72px] font-mono text-[12px]">
        {job.id.slice(0, 8)}
      </Link>
      <span title={job.started} className="flex-[0_0_110px] font-mono text-[12px] text-muted">
        {formatDateTime(job.started, lang)}
      </span>
      <span className="min-w-0 flex-[1_1_200px]">{tx(job.title, lang)}</span>
      <span className="flex-[0_1_220px] text-[13px] text-muted">{t.jobStatus[job.status]}</span>
    </li>
  );
}
