// /incidents/:id: một sự cố (mục 6.7 và 10 của KE-HOACH.md). Bản 2, một dãy thẻ
// từ trên xuống: thông tin chính (kèm dòng hướng dẫn "Kiểm lại" ở chân thẻ),
// chuyện gì xảy ra và nguyên nhân, các cách sửa (chỉ khi còn mở), bằng chứng lúc
// mở, kết quả hiện tại hoặc bằng chứng lúc đóng, các job sửa. "Kiểm lại" và
// "Chẩn đoán" là hành động của đầu màn. Cách sửa đặt trước bằng chứng vì người
// vào trang một sự cố đang mở cần hành động trước; bằng chứng dài (clamp 20 dòng)
// nằm ngay bên dưới để đối chiếu.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { Link, useParams } from "react-router";
import { useAi } from "../../ai/AiProvider";
import { ApiError, errorMessage, get, post } from "../../api/client";
import { useWho } from "../../api/m13-users";
import type { CheckResult, IncidentDetail as IncidentDetailData, Job, HealthReport } from "../../api/types";
import { Button, buttonClass } from "../../components/Button";
import { Card } from "../../components/Card";
import { EmptyState } from "../../components/EmptyState";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { Code, MonoText } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { StatusLine, StatusMark } from "../../components/StatusMark";
import { TableList, type Column } from "../../components/TableList";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/su-co";
import { ErrorNote } from "../site/ErrorNote";
import { formatClock, formatDateTime } from "../lich-su/format";
import { FixPanel } from "./FixPanel";
import { formatSpan, spanMs } from "./format";
import { domainOf, incidentState, Target } from "./parts";
import { refreshIncidents } from "./refresh";

const clampLines = 20;

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

  const crumbs = [{ label: t.title, to: "/incidents" }, { label: id }];

  if (q.isPending) {
    return (
      <>
        <ScreenHeader crumbs={crumbs} title={t.detailTitle} />
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      </>
    );
  }
  if (q.isError) {
    const missing = q.error instanceof ApiError && q.error.code === "incident_not_found";
    return (
      <>
        <ScreenHeader crumbs={crumbs} title={t.detailTitle} />
        <Card>
          <p className="mt-0 mb-[12px]">{missing ? t.notFound(id) : errorMessage(q.error, lang)}</p>
          {missing ? (
            <Link to="/incidents" className={buttonClass("secondary")}>
              {t.backToList}
            </Link>
          ) : (
            <Button onClick={() => void q.refetch()}>{k.retry}</Button>
          )}
        </Card>
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
  // "Kiểm lại" (POST /health/run) và chẩn đoán bằng AI chỉ dành cho admin; user thường vẫn đọc sự cố của site mình.
  const { isAdmin } = useWho();
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

  const jobColumns: Column<Job>[] = [
    {
      header: t.jobColId,
      cell: (j) => (
        <Link to={`/jobs/${encodeURIComponent(j.id)}`} title={j.id}>
          <Code>{j.id.slice(0, 8)}</Code>
        </Link>
      ),
    },
    {
      header: t.jobColWhen,
      cell: (j) => (
        <span title={j.started} className="text-[13px] text-muted">
          {formatDateTime(j.started, lang)}
        </span>
      ),
    },
    { header: t.jobColWhat, nowrap: false, cell: (j) => <span className="block min-w-[200px] [overflow-wrap:anywhere]">{tx(j.title, lang)}</span> },
    { header: t.jobColResult, nowrap: false, cell: (j) => <span className="text-[13px] text-muted">{t.jobStatus[j.status]}</span> },
  ];

  return (
    <>
      <ScreenHeader
        crumbs={[{ label: t.title, to: "/incidents" }, { label: tx(inc.title, lang) }]}
        title={tx(inc.title, lang)}
        summary={inc.closed ? t.detailClosed(formatDateTime(inc.opened, lang), formatDateTime(inc.closed, lang)) : t.detailOpen(formatDateTime(inc.opened, lang))}
        actions={
          isAdmin ? (
            <>
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
            </>
          ) : undefined
        }
      />

      <Card
        footer={
          isAdmin && isOpen ? (
            <span aria-live="polite">{recheck.isSuccess ? t.rechecked(formatClock(recheck.data.finished, lang)) : t.recheckHint}</span>
          ) : undefined
        }
      >
        <dl className="m-0 grid grid-cols-2 gap-x-[16px] gap-y-[12px] sm:flex sm:flex-wrap sm:gap-x-[40px]">
          <Fact label={t.fieldStatus}>
            <StatusLine status={state.status}>{state.text}</StatusLine>
          </Fact>
          <Fact label={t.fieldTarget}>
            <Target target={inc.target} />
          </Fact>
          <Fact label={t.fieldStage}>{t.stages[inc.stage]}</Fact>
          <Fact label={t.fieldCheck}>
            <MonoText>{inc.check_id}</MonoText>
          </Fact>
          <Fact label={t.fieldOpened}>
            <span title={inc.opened}>{formatDateTime(inc.opened, lang)}</span>
          </Fact>
          {inc.closed && (
            <Fact label={t.fieldClosed}>
              <span title={inc.closed}>{formatDateTime(inc.closed, lang)}</span>
            </Fact>
          )}
          <Fact label={t.fieldDuration}>{duration}</Fact>
        </dl>
      </Card>

      {isAdmin && isOpen && recheck.isError && (
        <Card>
          <ErrorNote error={recheck.error} heading={t.recheckFailed} />
        </Card>
      )}

      <Card title={t.whatHeading} titleId="chuyen-gi">
        <p className="m-0 font-medium">{tx(inc.open_result.summary, lang)}</p>
        {cause && (
          <div className="pt-[12px]">
            <div className="mb-[6px] text-[12px] font-medium text-muted">{t.causeLabel}</div>
            <p className="m-0">{cause}</p>
          </div>
        )}
      </Card>

      {isOpen && <FixPanel key={inc.id} incident={inc} live={live} />}

      <Card title={t.evidenceOpenHeading} titleId="bang-chung-mo">
        <Evidence result={inc.open_result} />
      </Card>

      {isOpen ? (
        <Card title={t.currentHeading} titleId="hien-tai">
          {current ? (
            <div className="flex flex-col gap-[10px]">
              <ResultLine result={current} checked={t.currentChecked(formatClock(current.checked, lang))} />
              <Evidence result={current} />
            </div>
          ) : (
            <EmptyState>{t.noCurrent}</EmptyState>
          )}
        </Card>
      ) : (
        <Card title={t.evidenceCloseHeading} titleId="bang-chung-dong">
          {inc.close_result ? (
            <div className="flex flex-col gap-[10px]">
              <ResultLine result={inc.close_result} />
              <Evidence result={inc.close_result} />
            </div>
          ) : (
            <EmptyState>{t.closedGoneNote}</EmptyState>
          )}
        </Card>
      )}

      <TableList title={t.jobsHeading} titleId="job-sua" minWidth={620} rows={jobs} rowKey={(j) => j.id} columns={jobColumns} empty={t.noJobs} />
    </>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <dt className="text-[12px] font-medium text-muted">{label}</dt>
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
  return result.evidence && result.evidence.length > 0 ? <EvidenceBlock evidence={result.evidence} label={null} clamp={clampLines} /> : <p className="m-0 text-muted">{t.noEvidence}</p>;
}
