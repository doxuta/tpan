// /jobs: lịch sử máy, đọc như `git log` (mục 3 của KE-HOACH.md, mục 6 của GIAO-DIEN.md).
// Hàng vẽ như trang mẫu bảng điều khiển ("Lịch sử máy"): mã job và giờ chữ mono màu
// --muted, việc và đối tượng, kết quả màu --muted, liên kết "bằng chứng". Nút
// "hoàn tác" là việc của M6 nên chưa có.

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { errorMessage, get } from "../../api/client";
import { useUsers, useWho } from "../../api/m13-users";
import type { Job } from "../../api/types";
import { Button } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { tx, useLang, useStrings, type Lang } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/lich-su";
import { SelectField } from "../dang-nhap/SelectField";
import { formatDateTime, formatDuration } from "./format";
import { elapsedMs } from "./jobModel";

const listLimit = 100;

// Bề rộng các cột của hàng (và của dòng nhãn cột ngay trên nó) để hai bên thẳng cột.
const wId = "flex-[0_0_72px]";
const wWhen = "flex-[0_0_110px]";
const wWho = "flex-[0_0_104px]";
const wResult = "flex-[0_1_220px]";

export function JobList() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const { isAdmin } = useWho();
  // Ô lọc theo người chạy (?user=) chỉ có với admin: user thường chỉ thấy job của mình.
  const [user, setUser] = useState("");
  const users = useUsers(isAdmin);
  const jobs = useQuery({
    queryKey: ["jobs", user],
    queryFn: () => get<Job[]>(`/api/v1/jobs?limit=${listLimit}${user ? `&user=${encodeURIComponent(user)}` : ""}`),
    // Có job đang chạy thì đọc lại để thấy nó xong; không thì thôi.
    refetchInterval: (q) => (q.state.data?.some((j) => j.status === "running") ? 3000 : false),
  });

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />
      {isAdmin && (
        <div className="max-w-[360px] pt-[20px]">
          <SelectField id="loc-nguoi-lam" label={t.filterLabel} hint={t.filterHint} value={user} onChange={(e) => setUser(e.target.value)}>
            <option value="">{t.filterAll}</option>
            {users.data?.map((u) => (
              <option key={u.user.name} value={u.user.name}>
                {u.user.name}
              </option>
            ))}
          </SelectField>
        </div>
      )}
      <section aria-label={t.listLabel} className="pt-[26px]">
        {jobs.isPending && <p className="m-0 text-muted">{k.loading}</p>}
        {jobs.isError && (
          <div>
            <p className="m-0">{errorMessage(jobs.error, lang)}</p>
            <div className="mt-[10px]">
              <Button onClick={() => void jobs.refetch()}>{k.retry}</Button>
            </div>
          </div>
        )}
        {jobs.data && jobs.data.length === 0 && <p className="m-0 border-t border-hair py-[10px] text-muted">{user ? t.emptyFiltered : t.empty}</p>}
        {jobs.data && jobs.data.length > 0 && (
          <>
            {/* Nhãn cột chỉ để nhìn (mỗi ô "Ai làm" của hàng có nhãn riêng cho trình đọc màn hình); ẩn ở màn hẹp vì hàng xuống dòng. */}
            <div aria-hidden="true" className="hidden gap-x-[16px] pb-[6px] font-mono text-[11px] tracking-[0.08em] text-muted sm:flex">
              <span className={wId}>{t.colJob}</span>
              <span className={wWhen}>{t.colWhen}</span>
              <span className="min-w-0 flex-[1_1_200px]">{t.colWhat}</span>
              <span className={wWho}>{t.colWho}</span>
              <span className={wResult}>{t.colResult}</span>
            </div>
          <ul className="m-0 list-none p-0">
            {jobs.data.map((j) => (
              <JobRow key={j.id} job={j} lang={lang} />
            ))}
          </ul>
          </>
        )}
      </section>
    </>
  );
}

function JobRow({ job, lang }: { job: Job; lang: Lang }) {
  const t = useStrings(strings);
  const status = t.status[job.status];
  const ms = job.finished ? elapsedMs(job.started, job.finished) : null;
  const result = ms === null ? status : t.statusWithTime(status, formatDuration(ms, t, lang));
  return (
    <li className="flex flex-wrap items-baseline gap-x-[16px] gap-y-[4px] border-t border-hair py-[10px]">
      <span className={`${wId} font-mono text-[12px] text-muted`}>{job.id.slice(0, 8)}</span>
      <span title={job.started} className={`${wWhen} font-mono text-[12px] text-muted`}>
        {formatDateTime(job.started, lang)}
      </span>
      <span className="min-w-0 flex-[1_1_200px]">
        <TitleWithTarget title={tx(job.title, lang)} target={job.target} />
      </span>
      {/* Ai làm: user TPAN đã chạy; rỗng là dòng lệnh, MCP hay bộ hẹn giờ. */}
      <span className={`${wWho} min-w-0 font-mono text-[12px] break-all ${job.actor ? "" : "text-muted"}`}>
        <span className="sr-only">{t.whoLabel}: </span>
        {job.actor || t.whoCli}
      </span>
      <span className={`${wResult} text-[13px] text-muted`}>{result}</span>
      <Link to={`/jobs/${encodeURIComponent(job.id)}`} aria-label={t.evidenceOf(job.id)} className="inline-flex min-h-[44px] items-center text-[13px] sm:min-h-0">
        {t.evidenceLink}
      </Link>
    </li>
  );
}

/** Tiêu đề job có đối tượng ở cuối ("Tạo site WordPress example.com"): tách đối tượng ra chữ mono như trang mẫu. */
function TitleWithTarget({ title, target }: { title: string; target: string }) {
  if (!target || !title.endsWith(target)) return <>{title}</>;
  return (
    <>
      {title.slice(0, title.length - target.length)}
      <MonoText>{target}</MonoText>
    </>
  );
}
