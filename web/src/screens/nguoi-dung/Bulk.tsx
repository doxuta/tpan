// Khoá hoặc mở khoá nhiều user một lần (mục 6.12: "chọn nhiều hàng"). Vẫn đủ ba
// chặng như mọi thao tác đổi máy, chỉ là lặp theo từng user:
//   1. dựng bản xem trước (?dry_run=1) của từng user, lần lượt;
//   2. người dùng bấm chạy một lần;
//   3. chạy thật từng user, đợi job này xong rồi mới tới user kế (máy chỉ chạy
//      một job một lúc, lệnh sau sẽ bị 409 job_running nếu job trước chưa xong).
// Kết quả hiện theo từng dòng, kèm liên kết tới trang job (bằng chứng).

import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { errorMessage } from "../../api/client";
import { previewOf, refreshAccounts, startOf, userPath, waitJob, type PlanRequest } from "../../api/m13-users";
import { Button } from "../../components/Button";
import { StatusMark } from "../../components/StatusMark";
import { TableList } from "../../components/TableList";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/nguoi-dung";

export type BulkAction = "suspend" | "unsuspend";

type State = "queued" | "previewing" | "ready" | "refused" | "running" | "done" | "failed" | "still";
type Item = { name: string; state: State; steps: number; error?: unknown; job?: string };

type Props = { action: BulkAction; names: string[]; reason: string; onClose: () => void };

export function BulkPanel({ action, names, reason, onClose }: Props) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const qc = useQueryClient();
  const [items, setItems] = useState<Item[]>(() => names.map((name) => ({ name, state: "queued", steps: 0 })));
  const [running, setRunning] = useState(false);
  const [finished, setFinished] = useState(false);
  const top = useRef<HTMLDivElement>(null);

  const reqOf = (name: string): PlanRequest => ({ method: "POST", path: userPath(name, action === "suspend" ? "/suspend" : "/unsuspend"), body: action === "suspend" ? { reason } : undefined });
  const patch = (name: string, p: Partial<Item>) => setItems((xs) => xs.map((x) => (x.name === name ? { ...x, ...p } : x)));

  // Chặng 1: bản xem trước của từng user. `stop` để đóng panel giữa chừng thì ngừng gọi tiếp.
  useEffect(() => {
    let stop = false;
    (async () => {
      for (const name of names) {
        if (stop) return;
        patch(name, { state: "previewing" });
        try {
          const p = await previewOf(reqOf(name));
          patch(name, { state: "ready", steps: p.steps.filter((s) => !s.done).length });
        } catch (err) {
          patch(name, { state: "refused", error: err });
        }
      }
    })();
    return () => {
      stop = true;
    };
    // Chạy một lần khi mở panel: names, reason, action không đổi trong đời panel.
  }, []);

  useEffect(() => {
    top.current?.scrollIntoView({ block: "start" });
    top.current?.focus({ preventScroll: true });
  }, []);

  const ready = items.filter((i) => i.state === "ready");
  const previewing = items.some((i) => i.state === "queued" || i.state === "previewing");

  // Chặng 3: chạy thật lần lượt.
  async function runAll() {
    setRunning(true);
    for (const it of ready) {
      patch(it.name, { state: "running" });
      try {
        const started = await startOf(reqOf(it.name));
        patch(it.name, { job: started.job_id });
        const job = await waitJob(started.job_id);
        patch(it.name, { state: job.status === "succeeded" ? "done" : job.status === "running" ? "still" : "failed" });
        // Job trước còn chạy thì job sau chắc chắn bị từ chối: dừng ở đây.
        if (job.status === "running") break;
      } catch (err) {
        patch(it.name, { state: "failed", error: err });
      }
    }
    setRunning(false);
    setFinished(true);
    refreshAccounts(qc);
  }

  const result = (i: Item): string => {
    switch (i.state) {
      case "queued":
        return t.bulkQueued;
      case "previewing":
        return t.bulkPreviewing;
      case "ready":
        return t.bulkReady(i.steps);
      case "refused":
        return `${t.bulkRefused}: ${errorMessage(i.error, lang)}`;
      case "running":
        return t.bulkRunning;
      case "done":
        return t.bulkDone;
      case "still":
        return t.bulkStillRunning;
      case "failed":
        return i.error ? `${t.bulkFailed}: ${errorMessage(i.error, lang)}` : t.bulkFailed;
    }
  };
  const mark = (s: State) => (s === "done" ? "pass" : s === "failed" || s === "refused" ? "fail" : s === "still" ? "warn" : "unknown");

  const title = action === "suspend" ? t.bulkTitleSuspend(names.length) : t.bulkTitleUnsuspend(names.length);
  const runLabel = action === "suspend" ? t.bulkRunSuspend(ready.length) : t.bulkRunUnsuspend(ready.length);

  return (
    // Khối ngoài giữ ref và focus: TableList tự vẽ thẻ nên không có chỗ nhận ref.
    <div ref={top} tabIndex={-1} aria-live="polite" className="outline-offset-0">
      <TableList
        titleId="bulk-heading"
        title={title}
        caption={title}
        actions={
          <>
            {!finished && (
              <Button variant="primary" disabled={previewing || running || ready.length === 0} onClick={() => void runAll()}>
                {running ? t.working : runLabel}
              </Button>
            )}
            <Button disabled={running} onClick={onClose}>
              {finished ? t.bulkClose : k.cancel}
            </Button>
          </>
        }
        above={
          <div className="flex flex-col gap-[6px] border-b border-hair px-[16px] py-[12px]">
            <p className="m-0 text-muted">{t.bulkIntro}</p>
            {action === "suspend" && reason && (
              <p className="m-0">
                <span className="text-[12px] font-medium text-muted">{t.bulkReasonLabel}</span>
                <br />
                {reason}
              </p>
            )}
          </div>
        }
        rows={items}
        rowKey={(i) => i.name}
        empty=""
        footer={!previewing && ready.length === 0 && !finished ? t.bulkNone : undefined}
        columns={[
          { header: t.bulkColUser, cell: (i) => <span className="font-semibold">{i.name}</span> },
          {
            header: t.bulkColResult,
            nowrap: false,
            cell: (i) => (
              <span className="flex items-start gap-[10px]">
                <span className="mt-[4px] flex-none">
                  <StatusMark status={mark(i.state)} label={k.status[mark(i.state)]} />
                </span>
                <span>
                  {result(i)}
                  {i.job && (
                    <>
                      {" "}
                      <Link to={`/jobs/${encodeURIComponent(i.job)}`}>{t.bulkEvidence}</Link>
                    </>
                  )}
                </span>
              </span>
            ),
          },
        ]}
      />
    </div>
  );
}
