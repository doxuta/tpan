// /plans/:id: một Plan do AI, agent hay người đề xuất. Plan chờ duyệt nhúng
// PlanReview có sẵn (màn xem trước dùng chung, không sửa) và "chạy" ở đây là
// duyệt: POST /api/v1/plans/{id}/approve rồi chuyển sang /jobs/{job_id}.
//
// Lúc duyệt mà máy đã khác lúc đề xuất, máy chủ dựng lại bản xem trước và trả
// 409 plan_changed. Ta đọc lại Plan để PlanReview vẽ bản mới và báo người dùng
// xem lại; không tự duyệt lại, vì bản người dùng vừa xem không còn đúng.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useParams } from "react-router";
import { useJobTracker } from "../../components/JobTracker";
import { ApiError, errorMessage, get, post } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { JobStarted, PendingPlan } from "../../api/types";
import { Button, buttonClass } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { MonoText } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { StatusMark } from "../../components/StatusMark";
import { Tag } from "../../components/Tag";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/agent";
import { strings as khung } from "../../i18n/khung";
import { PlanReview } from "../duyet-plan";
import { formatDateTime } from "../lich-su/format";
import { ErrorNote } from "../site/ErrorNote";
import { OriginTag, plansKey } from "./OriginTag";

export function PlanDetail() {
  const { id = "" } = useParams();
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({
    queryKey: [...plansKey, "detail", id],
    queryFn: () => get<PendingPlan>(`/api/v1/plans/${encodeURIComponent(id)}`),
  });
  const plan = q.data;

  return (
    <>
      {q.isPending && <p className="m-0 pt-[24px] text-muted">{k.loading}</p>}
      {!plan && q.isError && (
        <div role="alert" className="pt-[24px]">
          <p className="m-0">
            {t.detailLoadFailed} {errorMessage(q.error, lang)}
          </p>
          <div className="mt-[12px] flex flex-wrap gap-[8px]">
            <Button onClick={() => void q.refetch()}>{k.retry}</Button>
            <Link to="/plans" className={buttonClass("secondary")}>
              {t.backToList}
            </Link>
          </div>
        </div>
      )}
      {plan && <Body plan={plan} />}
    </>
  );
}

function Body({ plan }: { plan: PendingPlan }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const qc = useQueryClient();
  const { track } = useJobTracker();
  const [changed, setChanged] = useState(false);
  const [rejecting, setRejecting] = useState(false);
  const idPath = encodeURIComponent(plan.id);

  const approve = useMutation({
    mutationFn: () => post<JobStarted>(`/api/v1/plans/${idPath}/approve`),
    onMutate: () => setChanged(false),
    onSuccess: (job) => {
      void qc.invalidateQueries({ queryKey: plansKey });
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      // Ở lại trang đang dùng; thẻ theo dõi ở góc dưới báo tiến độ và tải lại dữ liệu khi job xong.
      track(job.job_id);
    },
    onError: (err) => {
      if (err instanceof ApiError && err.code === "plan_changed") setChanged(true);
      // Lỗi nào cũng đọc lại Plan: plan_changed thì có bản xem trước mới, plan_not_pending thì trạng thái đã khác.
      void qc.invalidateQueries({ queryKey: plansKey });
    },
  });
  const reject = useMutation({
    mutationFn: () => post<void>(`/api/v1/plans/${idPath}/reject`),
    onSuccess: () => setRejecting(false),
    onSettled: () => qc.invalidateQueries({ queryKey: plansKey }),
  });

  const pending = plan.status === "pending";
  const when = formatDateTime(plan.decided ?? plan.created, lang);
  // plan_changed đã có câu riêng bên dưới; chỉ lỗi khác mới đưa cho PlanReview hiện.
  const approveError = changed ? undefined : approve.error;

  return (
    <>
      <ScreenHeader title={tx(plan.title, lang)} summary={t.proposedAt(formatDateTime(plan.created, lang))} />

      <div className="mt-[14px] flex flex-wrap items-center gap-x-[12px] gap-y-[8px]">
        <OriginTag origin={plan.origin} />
        <Tag>{t.status[plan.status]}</Tag>
        <MonoText size={12}>{plan.target}</MonoText>
      </div>

      {plan.untrusted && (
        <div role="note" className="mt-[18px] flex items-start gap-[10px] border border-ink p-[12px]">
          <span className="mt-[2px] flex w-[20px] flex-none justify-center">
            <StatusMark status="warn" label={t.untrustedLabel} />
          </span>
          <p className="m-0 font-medium">{t.untrustedWarning}</p>
        </div>
      )}

      {changed && (
        <div role="alert" className="mt-[18px] flex items-start gap-[10px] border border-ink p-[12px]">
          <span className="mt-[2px] flex w-[20px] flex-none justify-center">
            <StatusMark status="warn" label={t.untrustedLabel} />
          </span>
          <p className="m-0 font-medium">{t.planChanged}</p>
        </div>
      )}

      {pending && (
        <>
          <PlanReview preview={plan.preview} onRun={() => approve.mutate()} running={approve.isPending} error={approveError} />
          <section aria-labelledby="tu-choi-tieu-de" className="mt-[22px] border-t border-hair pt-[18px]">
            <h2 id="tu-choi-tieu-de" className="m-0 text-[16px] font-semibold">
              {t.rejectHeading}
            </h2>
            <p className="mt-[6px] mb-[12px] text-muted">{t.rejectHint}</p>
            <Button onClick={() => setRejecting(true)} disabled={approve.isPending}>
              {t.reject}
            </Button>
          </section>
          <Dialog
            open={rejecting}
            onOpenChange={(o) => {
              setRejecting(o);
              if (!o) reject.reset();
            }}
            title={t.rejectTitle}
            description={t.rejectBody}
            confirmLabel={t.rejectConfirm}
            busy={reject.isPending}
            onConfirm={() => reject.mutate()}
          >
            {reject.error != null && <ErrorNote error={reject.error} />}
          </Dialog>
        </>
      )}

      {plan.status === "applied" && (
        <section className="mt-[22px] border-t border-hair pt-[18px]">
          <p className="m-0">{t.appliedLine}</p>
          {plan.job_id && (
            <p className="mt-[10px] mb-0">
              <Link to={`/jobs/${encodeURIComponent(plan.job_id)}`} className="inline-flex min-h-[44px] items-center sm:min-h-0">
                {t.openJob}
              </Link>
            </p>
          )}
        </section>
      )}

      {plan.status === "rejected" && (
        <section className="mt-[22px] border-t border-hair pt-[18px]">
          <p className="m-0">
            {plan.decided_by ? (
              <>
                <MonoText size={13}>{plan.decided_by}</MonoText> {t.rejectedLine(when)}
              </>
            ) : (
              t.rejectedNoWho(when)
            )}
          </p>
        </section>
      )}

      <p className="mt-[26px] mb-0">
        <Link to="/plans" className="inline-flex min-h-[44px] items-center sm:min-h-0">
          {t.backToList}
        </Link>
      </p>
    </>
  );
}
