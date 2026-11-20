// Khối "PHP" của trang một site: bản PHP site đang dùng, socket của pool, và
// việc đổi bản. Đổi bản đi qua ba chặng như mọi thao tác: chọn bản đã cài →
// POST /api/v1/sites/{domain}/php?dry_run=1 → PlanReview → POST thật → /jobs/:id.
// Danh sách bản đã cài lấy từ GET /api/v1/php, cùng khoá đệm với màn Stack.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { errorMessage, get, post } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { JobStarted, PHPList, PHPVersionBody, Preview } from "../../api/types";
import { Button } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/site";
import { PlanReview } from "../duyet-plan";
import { phpKey } from "../stack/model";
import { ErrorNote } from "./ErrorNote";

const labelClass = "m-0 font-mono text-[11px] font-normal tracking-[0.08em] text-muted uppercase";

type Props = {
  domain: string;
  /** SiteInfo.site.php_version */
  version: string;
  /** SiteInfo.socket; vắng khi máy chủ chưa trả. */
  socket?: string;
  /** M13: lý do không đổi bản PHP được (site hay user DB đang khoá). Có thì nút vô hiệu và lý do hiện ngay dưới. */
  blocked?: string;
};

export function PhpBlock({ domain, version, socket, blocked }: Props) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const list = useQuery({ queryKey: phpKey, queryFn: () => get<PHPList>("/api/v1/php") });

  // Chỉ bản đã cài mới chọn được. Go có thể trả null cho danh sách rỗng.
  const options = (list.data?.versions ?? []).filter((v) => v.installed).map((v) => v.version);
  const [choice, setChoice] = useState(version);
  // Nếu bản site đang dùng không có trong danh sách (ví dụ đã gỡ ngoài TPAN) thì chọn bản đầu tiên, để ô chọn và nút luôn nói cùng một bản.
  const value = options.includes(choice) ? choice : (options[0] ?? "");
  const others = options.filter((v) => v !== version);

  // Giữ cả thân đã gửi lúc xem trước: người dùng đổi ô chọn trong khi bản xem trước đang mở thì "Chạy" vẫn chạy đúng bản đã xem.
  const [open, setOpen] = useState<{ plan: Preview; body: PHPVersionBody } | null>(null);
  const previewRef = useRef<HTMLDivElement>(null);
  const path = `/api/v1/sites/${encodeURIComponent(domain)}/php`;
  const dry = useMutation({
    mutationFn: (body: PHPVersionBody) => post<Preview>(`${path}?dry_run=1`, body),
    onSuccess: (plan, body) => setOpen({ plan, body }),
  });
  const run = useMutation({
    mutationFn: (body: PHPVersionBody) => post<JobStarted>(path, body),
    onSuccess: (job) => {
      // Khoá của trang Site là ["site", domain] (siteKey trong index.tsx); không import được vì index.tsx import khối này.
      void qc.invalidateQueries({ queryKey: ["site", domain] });
      void qc.invalidateQueries({ queryKey: phpKey });
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      void navigate(`/jobs/${encodeURIComponent(job.job_id)}`);
    },
  });
  useEffect(() => {
    if (open) {
      previewRef.current?.scrollIntoView({ block: "start" });
      previewRef.current?.focus();
    }
  }, [open]);

  return (
    <section aria-labelledby="php-site">
      <h2 id="php-site" className="m-0 pt-[26px] pb-[10px] text-[16px] font-semibold">
        {t.phpHeading}
      </h2>
      <dl className="m-0 flex flex-wrap gap-x-[40px] gap-y-[12px] border-t border-hair pt-[12px]">
        <div>
          <dt className={labelClass}>{t.phpVersion}</dt>
          <dd className="m-0 mt-[4px]">
            <MonoText>{version}</MonoText>
          </dd>
        </div>
        {socket && (
          <div className="min-w-0">
            <dt className={labelClass}>{t.phpSocket}</dt>
            <dd className="m-0 mt-[4px]">
              <MonoText>{socket}</MonoText>
            </dd>
          </div>
        )}
      </dl>

      {list.isPending && <p className="mt-[14px] mb-0 text-muted">{k.loading}</p>}
      {list.isError && (
        <div className="pt-[14px]">
          <p className="mt-0 mb-[4px] font-semibold">{t.phpLoadFailed}</p>
          <p className="mt-0 mb-[12px]">{errorMessage(list.error, lang)}</p>
          <Button onClick={() => void list.refetch()}>{k.retry}</Button>
        </div>
      )}
      {list.isSuccess &&
        (others.length === 0 ? (
          <p className="mt-[14px] mb-0 max-w-[720px] text-muted">{t.phpNone}</p>
        ) : (
          <>
            <div className="flex flex-wrap items-end gap-x-[14px] gap-y-[10px] pt-[14px]">
              <div>
                <label htmlFor="php-site-version" className="mb-[6px] block text-[14px] font-medium">
                  {t.phpChoose}
                </label>
                <select
                  id="php-site-version"
                  value={value}
                  onChange={(e) => setChoice(e.target.value)}
                  className="box-border block h-[44px] min-w-[110px] border border-ink bg-surface px-[10px] font-mono text-[13px] text-ink sm:h-[40px]"
                >
                  {options.map((v) => (
                    <option key={v} value={v}>
                      {v}
                    </option>
                  ))}
                </select>
              </div>
              <Button
                onClick={() => {
                  // Bản xem trước cũ không còn đúng với lựa chọn mới: bỏ nó trước khi dựng bản mới.
                  setOpen(null);
                  run.reset();
                  dry.mutate({ version: value });
                }}
                aria-describedby={blocked ? "php-bi-chan" : undefined}
                disabled={dry.isPending || run.isPending || value === version || blocked !== undefined}>
                {dry.isPending ? t.phpPreviewing : t.phpChange}
              </Button>
            </div>
            {blocked && (
              <p id="php-bi-chan" className="mt-[10px] mb-0 max-w-[720px] text-[13px] text-muted">
                {blocked}
              </p>
            )}
            <p className="mt-[10px] mb-0 max-w-[720px] text-muted">{t.phpHint}</p>
          </>
        ))}

      {dry.isError && (
        <div className="pt-[12px]">
          <ErrorNote error={dry.error} heading={t.phpFailed(dry.variables.version)} checksHeading={t.phpChecksHeading} />
        </div>
      )}
      {open && (
        <div ref={previewRef} tabIndex={-1} className="outline-offset-0">
          <PlanReview
            preview={open.plan}
            onRun={() => run.mutate(open.body)}
            running={run.isPending}
            error={run.error}
            onCancel={() => {
              setOpen(null);
              run.reset();
              dry.reset();
            }}
          />
        </div>
      )}
    </section>
  );
}
