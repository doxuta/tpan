// Theo dõi job ngay trên trang đang dùng. Trước đây bấm "Chạy" xong là chuyển
// sang trang chi tiết job ở Lịch sử, người dùng mất chỗ đang làm. Giờ màn gọi
// track(job_id): góc dưới bên trái (bên phải là khung chat AI) hiện một thẻ nhỏ cho mỗi job (đang chạy,
// xong, lỗi), có liên kết "Xem chi tiết" khi cần đọc từng bước. Job xong thì
// mọi dữ liệu trên trang được tải lại, nên bảng tự cập nhật.
//
//   const { track } = useJobTracker();
//   onSuccess: (job) => track(job.job_id)

import { useQueryClient } from "@tanstack/react-query";
import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { get } from "../api/client";
import type { JobDetail, JobStatus, Text } from "../api/types";
import { tx, useLang, useStrings } from "../i18n";
import { strings } from "../i18n/khung";
import { StatusMark } from "./StatusMark";

type Tracked = { id: string; title?: Text; status: JobStatus | "unknown"; error?: string };

type Ctx = {
  track: (id: string) => void;
  /** Chờ một job chạy xong (dùng cho thao tác hàng loạt chạy lần lượt). */
  wait: (id: string) => Promise<JobStatus>;
};

const JobCtx = createContext<Ctx | null>(null);

export function useJobTracker(): Ctx {
  const c = useContext(JobCtx);
  if (!c) throw new Error("useJobTracker phải nằm trong JobTrackerProvider");
  return c;
}

const pollMs = 1200;
/** Thẻ của job xong tốt tự ẩn sau chừng này; job lỗi thì giữ tới khi người dùng đóng. */
const doneHideMs = 8000;

async function readJob(id: string): Promise<JobDetail> {
  return get<JobDetail>(`/api/v1/jobs/${encodeURIComponent(id)}`);
}

export function JobTrackerProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const [jobs, setJobs] = useState<Tracked[]>([]);
  const timers = useRef(new Map<string, number>());

  useEffect(() => {
    const ts = timers.current;
    return () => ts.forEach((t) => window.clearTimeout(t));
  }, []);

  const wait = useCallback(async (id: string): Promise<JobStatus> => {
    for (;;) {
      try {
        const d = await readJob(id);
        if (d.job.status !== "running") return d.job.status;
      } catch {
        // Mạng chập chờn: thử lại ở vòng sau thay vì bỏ cuộc.
      }
      await new Promise((r) => window.setTimeout(r, pollMs));
    }
  }, []);

  const track = useCallback(
    (id: string) => {
      setJobs((cur) => (cur.some((j) => j.id === id) ? cur : [...cur, { id, status: "running" }]));
      void (async () => {
        for (;;) {
          try {
            const d = await readJob(id);
            setJobs((cur) => cur.map((j) => (j.id === id ? { ...j, title: d.job.title, status: d.job.status, error: d.job.error } : j)));
            if (d.job.status !== "running") {
              // Job xong: tải lại mọi dữ liệu đang hiện (bảng, số đếm ở menu, bản đồ tuyến).
              void qc.invalidateQueries();
              if (d.job.status === "succeeded") {
                timers.current.set(
                  id,
                  window.setTimeout(() => setJobs((cur) => cur.filter((j) => j.id !== id)), doneHideMs),
                );
              }
              return;
            }
          } catch {
            /* thử lại ở vòng sau */
          }
          await new Promise((r) => window.setTimeout(r, pollMs));
        }
      })();
    },
    [qc],
  );

  const close = (id: string) => setJobs((cur) => cur.filter((j) => j.id !== id));

  return (
    <JobCtx.Provider value={{ track, wait }}>
      {children}
      {jobs.length > 0 && (
        <div aria-live="polite" className="fixed bottom-[16px] left-[16px] z-30 flex w-[min(380px,calc(100vw-32px))] flex-col gap-[8px]">
          {jobs.map((j) => (
            <JobCard key={j.id} job={j} onClose={() => close(j.id)} />
          ))}
        </div>
      )}
    </JobCtx.Provider>
  );
}

function JobCard({ job, onClose }: { job: Tracked; onClose: () => void }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const mark = job.status === "running" || job.status === "unknown" ? "unknown" : job.status === "succeeded" ? "pass" : "fail";
  const text =
    job.status === "running" || job.status === "unknown"
      ? t.jobRunning
      : job.status === "succeeded"
        ? t.jobDone
        : job.status === "rolled_back"
          ? t.jobRolledBack
          : t.jobUndoFailed;
  return (
    <div className={`border bg-surface p-[12px] ${mark === "fail" ? "border-fail border-t-[4px]" : "border-ink"}`}>
      <div className="flex items-start gap-[10px]">
        <span className="mt-[3px] flex-none">
          <StatusMark status={mark} label={text} />
        </span>
        <div className="min-w-0 flex-1">
          <p className="m-0 text-[14px] font-medium [overflow-wrap:anywhere]">{job.title ? tx(job.title, lang) : t.jobStarting}</p>
          <p className="m-0 mt-[2px] text-[13px] text-muted">
            {text}
            {job.error ? `: ${job.error}` : ""}
          </p>
          <Link to={`/jobs/${encodeURIComponent(job.id)}`} className="mt-[4px] inline-block text-[13px]">
            {t.jobDetails}
          </Link>
        </div>
        <button type="button" aria-label={t.close} title={t.close} onClick={onClose} className="flex h-[28px] w-[28px] flex-none items-center justify-center bg-transparent text-muted hover:text-ink">
          <svg aria-hidden="true" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round">
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </button>
      </div>
    </div>
  );
}
