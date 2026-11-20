// Màn Cập nhật (mục 6.11 và mục 10 của KE-HOACH.md). GET /api/v1/updates (không
// chạy apt-get update) trả UpdateStatus; màn có bốn khối:
//   (a) Packages  gói của stack có bản mới       → nút "Cập nhật N gói"
//   (b) Auto      bản vá bảo mật tự động         → nút "Bật bản vá tự động" khi chưa đạt
//   (c) Mariadb   dòng MariaDB nâng lên được     → mỗi dòng một nút "Nâng lên <dòng>"
//   (d) Self      cập nhật chính TPAN            → chỉ lệnh để tự gõ, không nút
// Ba việc đầu cùng một nhịp như màn SSL và Bảo mật: bấm → POST ?dry_run=1 trả
// Preview → PlanReview → POST thật trả job_id → chuyển tới /jobs/:id. Một bản
// xem trước tại một thời điểm, nằm dưới các khối. Máy chủ từ chối (409 kèm
// checks, ví dụ nothing_to_update, mariadb_not_newer) thì ErrorNote hiện câu
// của nó ngay dưới nút đã bấm.

import { useJobTracker } from "../../components/JobTracker";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { errorMessage, get, post } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { JobStarted, Preview, UpdateStatus } from "../../api/types";
import { Button } from "../../components/Button";
import { ScreenHeader } from "../../components/ScreenHeader";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/cap-nhat";
import { strings as khung } from "../../i18n/khung";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";
import { Auto } from "./Auto";
import { Mariadb } from "./Mariadb";
import { type Action, type Flow, updatesKey } from "./model";
import { Packages } from "./Packages";
import { Self } from "./Self";

export default function UpdatesScreen() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({ queryKey: updatesKey, queryFn: () => get<UpdateStatus>("/api/v1/updates") });

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />
      {q.isPending && <p className="mt-[26px] mb-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div className="pt-[26px]">
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </div>
      )}
      {q.isSuccess && <Loaded status={q.data} />}
    </>
  );
}

// Tách khỏi UpdatesScreen để trạng thái xem trước và mở/đóng của dòng bản vá
// tự động khởi tạo một lần từ dữ liệu đã có.
function Loaded({ status }: { status: UpdateStatus }) {
  const t = useStrings(strings);
  const qc = useQueryClient();
  const { track } = useJobTracker();

  /** Việc đang xem trước và bản xem trước của nó. "Chạy" gửi đúng việc này. */
  const [open, setOpen] = useState<{ action: Action; preview: Preview } | null>(null);
  const previewRef = useRef<HTMLDivElement>(null);

  const dry = useMutation({
    mutationFn: (a: Action) => post<Preview>(`${a.path}?dry_run=1`, a.body),
    onSuccess: (preview, action) => setOpen({ action, preview }),
  });
  const run = useMutation({
    mutationFn: (a: Action) => post<JobStarted>(a.path, a.body),
    onSuccess: (job) => {
      void qc.invalidateQueries({ queryKey: updatesKey });
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      // Ở lại trang đang dùng; thẻ theo dõi ở góc dưới báo tiến độ và tải lại dữ liệu khi job xong.
      track(job.job_id);
      setOpen(null);
    },
  });

  // Có bản xem trước thì đưa người dùng tới đó, vì nó nằm dưới các khối.
  useEffect(() => {
    if (open) {
      previewRef.current?.scrollIntoView({ block: "start" });
      previewRef.current?.focus();
    }
  }, [open]);

  function start(a: Action) {
    // Bản xem trước của việc kia không còn đúng nữa: bỏ nó trước khi dựng bản mới.
    setOpen(null);
    run.reset();
    dry.mutate(a);
  }

  function cancel() {
    const id = open?.action.id;
    setOpen(null);
    run.reset();
    dry.reset();
    if (id) document.getElementById(`act-${id}`)?.focus();
  }

  const flow: Flow = {
    start,
    pending: dry.isPending ? dry.variables.id : null,
    busy: dry.isPending || run.isPending,
    errorFor: (a) =>
      dry.isError && dry.variables.id === a.id ? (
        <ErrorNote error={dry.error} heading={a.body ? t.previewErrorLine(a.body.line) : t.previewError} checksHeading={t.checksHeading} />
      ) : undefined,
  };

  return (
    <>
      <Packages status={status} flow={flow} />
      <Auto status={status} flow={flow} />
      <Mariadb status={status.mariadb} flow={flow} />
      <Self status={status.self} />

      {open && (
        <div ref={previewRef} tabIndex={-1} className="outline-offset-0">
          <PlanReview preview={open.preview} onRun={() => run.mutate(open.action)} running={run.isPending} error={run.error} onCancel={cancel} />
        </div>
      )}
    </>
  );
}
