// /plans/:id: một Plan do AI, agent hay người đề xuất. Plan chờ duyệt nhúng
// PlanReview có sẵn (màn xem trước dùng chung, không sửa) và "chạy" ở đây là
// duyệt: POST /api/v1/plans/{id}/approve rồi ở lại trang, thẻ theo dõi job báo
// tiến độ.
//
// Lúc duyệt mà máy đã khác lúc đề xuất, máy chủ dựng lại bản xem trước và trả
// 409 plan_changed. Ta đọc lại Plan để PlanReview vẽ bản mới và báo người dùng
// xem lại; không tự duyệt lại, vì bản người dùng vừa xem không còn đúng.
//
// Bản 2: mỗi nhóm là một thẻ; đường dẫn "Plan chờ duyệt / <tiêu đề>"; nhãn nguồn,
// trạng thái và đối tượng nằm ở hàng nhãn nhỏ dưới tóm tắt.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useParams } from "react-router";
import { useJobTracker } from "../../components/JobTracker";
import { ApiError, errorMessage, get, post } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { JobStarted, PendingPlan } from "../../api/types";
import { Button, buttonClass } from "../../components/Button";
import { Card } from "../../components/Card";
import { Dialog } from "../../components/Dialog";
import { Notice } from "../../components/Notice";
import { ScreenHeader } from "../../components/ScreenHeader";
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
      {!plan && <ScreenHeader crumbs={[{ label: k.groups.ai }, { label: t.listTitle }]} title={t.listTitle} />}
      {q.isPending && (
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {!plan && q.isError && (
        <Card>
          <div role="alert">
            <p className="mt-0 mb-[12px]">
              {t.detailLoadFailed} {errorMessage(q.error, lang)}
            </p>
            <div className="flex flex-wrap gap-[8px]">
              <Button onClick={() => void q.refetch()}>{k.retry}</Button>
              <Link to="/plans" className={buttonClass("secondary")}>
                {t.backToList}
              </Link>
            </div>
          </div>
        </Card>
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
  const title = tx(plan.title, lang);

  return (
    <>
      <ScreenHeader
        crumbs={[{ label: t.listTitle, to: "/plans" }, { label: title }]}
        title={title}
        summary={t.proposedAt(formatDateTime(plan.created, lang))}
        meta={
          <>
            <OriginTag origin={plan.origin} tone="surface" />
            <Tag tone="surface">{t.status[plan.status]}</Tag>
            <Tag tone="surface">{plan.target}</Tag>
          </>
        }
      />

      {plan.untrusted && (
        <Card>
          <div role="note">
            <Notice kind="warn">
              <p className="m-0 font-medium">
                <span className="sr-only">{t.untrustedLabel}: </span>
                {t.untrustedWarning}
              </p>
            </Notice>
          </div>
        </Card>
      )}

      {changed && (
        <Card>
          <Notice kind="warn" role="alert">
            <p className="m-0 font-medium">
              <span className="sr-only">{t.untrustedLabel}: </span>
              {t.planChanged}
            </p>
          </Notice>
        </Card>
      )}

      {pending && (
        <>
          <PlanReview preview={plan.preview} onRun={() => approve.mutate()} running={approve.isPending} error={approveError} />
          <Card title={t.rejectHeading} titleId="tu-choi-tieu-de">
            <p className="mt-0 mb-[12px] text-muted">{t.rejectHint}</p>
            <Button onClick={() => setRejecting(true)} disabled={approve.isPending}>
              {t.reject}
            </Button>
          </Card>
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
        <Card>
          <p className="m-0">{t.appliedLine}</p>
          {plan.job_id && (
            <p className="mt-[10px] mb-0">
              <Link to={`/jobs/${encodeURIComponent(plan.job_id)}`} className="inline-flex min-h-[44px] items-center sm:min-h-0">
                {t.openJob}
              </Link>
            </p>
          )}
        </Card>
      )}

      {plan.status === "rejected" && (
        <Card>
          <p className="m-0">
            {plan.decided_by ? (
              <>
                <span className="font-medium">{plan.decided_by}</span> {t.rejectedLine(when)}
              </>
            ) : (
              t.rejectedNoWho(when)
            )}
          </p>
        </Card>
      )}

      <p className="m-0">
        <Link to="/plans" className="inline-flex min-h-[44px] items-center sm:min-h-0">
          {t.backToList}
        </Link>
      </p>
    </>
  );
}
