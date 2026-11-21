// /jobs: lịch sử máy, đọc như `git log` (mục 3 của KE-HOACH.md, mục 6 của GIAO-DIEN.md).
// Bản 2: ScreenHeader, một ô lọc (admin) trong thẻ, rồi một TableList tự vẽ thẻ
// (cùng cách vẽ với thẻ "Lịch sử máy" của Bảng điều khiển): mã job, giờ, việc,
// người làm, kết quả, liên kết tới trang job. Nút "hoàn tác" là việc của M6 nên chưa có.

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { errorMessage, get } from "../../api/client";
import { useUsers, useWho } from "../../api/m13-users";
import type { Job } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { SelectField } from "../../components/Field";
import { Code } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { TableList, type Column } from "../../components/TableList";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/lich-su";
import { formatDateTime, formatDuration } from "./format";
import { elapsedMs } from "./jobModel";

const listLimit = 100;

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

  const columns: Column<Job>[] = [
    {
      header: t.colJob,
      cell: (j) => (
        <span title={j.id}>
          <Code className="text-muted">{j.id.slice(0, 8)}</Code>
        </span>
      ),
    },
    {
      header: t.colWhen,
      cell: (j) => (
        <span title={j.started} className="text-[13px] text-muted">
          {formatDateTime(j.started, lang)}
        </span>
      ),
    },
    { header: t.colWhat, nowrap: false, cell: (j) => <span className="block min-w-[200px] [overflow-wrap:anywhere]">{tx(j.title, lang)}</span> },
    // Ai làm: user TPAN đã chạy; rỗng là dòng lệnh, MCP hay bộ hẹn giờ.
    { header: t.colWho, cell: (j) => (j.actor ? <span>{j.actor}</span> : <span className="text-muted">{t.whoCli}</span>) },
    {
      header: t.colResult,
      nowrap: false,
      cell: (j) => {
        const status = t.status[j.status];
        const ms = j.finished ? elapsedMs(j.started, j.finished) : null;
        return <span className="text-[13px] text-muted">{ms === null ? status : t.statusWithTime(status, formatDuration(ms, t, lang))}</span>;
      },
    },
    {
      header: t.colEvidence,
      align: "right",
      cell: (j) => (
        <Link to={`/jobs/${encodeURIComponent(j.id)}`} aria-label={t.evidenceOf(j.id)} className="inline-flex min-h-[44px] items-center sm:min-h-0">
          {t.evidenceLink}
        </Link>
      ),
    },
  ];

  return (
    <>
      <ScreenHeader crumbs={[{ label: k.groups.may }, { label: t.title }]} title={t.title} summary={t.summary} />
      {isAdmin && (
        <Card>
          <div className="max-w-[360px]">
            <SelectField id="loc-nguoi-lam" label={t.filterLabel} hint={t.filterHint} value={user} onChange={(e) => setUser(e.target.value)}>
              <option value="">{t.filterAll}</option>
              {users.data?.map((u) => (
                <option key={u.user.name} value={u.user.name}>
                  {u.user.name}
                </option>
              ))}
            </SelectField>
          </div>
        </Card>
      )}
      {jobs.isPending && (
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {jobs.isError && (
        <Card>
          <p className="mt-0 mb-[12px]">{errorMessage(jobs.error, lang)}</p>
          <Button onClick={() => void jobs.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {jobs.data && (
        <TableList
          label={t.listLabel}
          minWidth={820}
          rows={jobs.data}
          rowKey={(j) => j.id}
          columns={columns}
          empty={user ? t.emptyFiltered : t.empty}
          footer={jobs.data.length > 0 ? <span>{jobs.data.length >= listLimit ? t.countLatest(jobs.data.length) : t.count(jobs.data.length)}</span> : undefined}
        />
      )}
    </>
  );
}
