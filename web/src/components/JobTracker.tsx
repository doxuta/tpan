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
import { IconButton } from "./Button";
import { stashJobSecrets } from "../screens/lich-su/useJob";
import { StatusMark } from "./StatusMark";

/** hasSecret: job trả về một mật khẩu chỉ hiện một lần (đã cất bằng stashJobSecrets); thẻ nhắc người dùng mở chi tiết và không tự ẩn. */
type Tracked = { id: string; title?: Text; status: JobStatus | "unknown"; error?: string; hasSecret?: boolean };

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

/** Đọc job và cất ngay mật khẩu (nếu có): lần GET này có thể là lần duy nhất máy chủ trả nó. */
async function readJob(id: string): Promise<JobDetail & { hasSecret: boolean }> {
  const d = await get<JobDetail>(`/api/v1/jobs/${encodeURIComponent(id)}`);
  return { ...d, hasSecret: stashJobSecrets(id, d) };
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
            setJobs((cur) => cur.map((j) => (j.id === id ? { ...j, title: d.job.title, status: d.job.status, error: d.job.error, hasSecret: j.hasSecret || d.hasSecret } : j)));
            if (d.job.status !== "running") {
              // Job xong: tải lại mọi dữ liệu đang hiện (bảng, số đếm ở menu, bản đồ tuyến).
              void qc.invalidateQueries();
              // Có mật khẩu chờ người dùng lấy thì thẻ ở lại cho tới khi họ đóng.
              if (d.job.status === "succeeded" && !d.hasSecret) {
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
        // lg:left-[248px]: từ 1024 px menu trái (232 px) hiện, thẻ lệch phải menu 16 px cho khỏi che mục cuối.
        // data-job-tracker: móc cho script chụp của M16 (ảnh lớp nổi "thẻ theo dõi job").
        <div data-job-tracker aria-live="polite" className="fixed bottom-[16px] left-[16px] z-30 flex w-[min(380px,calc(100vw-32px))] flex-col gap-[8px] lg:left-[248px]">
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
    <div className="rounded-card border border-hair bg-surface p-[12px] shadow-pop">
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
          {job.hasSecret && <p className="m-0 mt-[4px] text-[13px] font-medium">{t.jobSecret}</p>}
          <Link to={`/jobs/${encodeURIComponent(job.id)}`} className="mt-[4px] inline-block text-[13px]">
            {job.hasSecret ? t.jobSecretLink : t.jobDetails}
          </Link>
        </div>
        {/* Lề âm: nút 40 px không làm thẻ cao hơn nội dung. */}
        <IconButton icon="close" label={t.close} onClick={onClose} className="-m-[8px]" />
      </div>
    </div>
  );
}
