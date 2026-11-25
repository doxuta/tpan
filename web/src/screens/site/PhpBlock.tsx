// Khối "PHP" của trang một site (tab PHP): bản PHP site đang dùng, socket của pool,
// và việc đổi bản. Đổi bản đi qua ba chặng như mọi thao tác: chọn bản đã cài →
// POST /api/v1/sites/{domain}/php?dry_run=1 → PlanReview → POST thật → /jobs/:id.
// Danh sách bản đã cài lấy từ GET /api/v1/php, cùng khoá đệm với màn Stack.

import { useJobTracker } from "../../components/JobTracker";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { errorMessage, get, post } from "../../api/client";
import { useIdentity } from "../../api/m13-dbs";
import { queryKeys } from "../../api/queries";
import type { JobStarted, PHPList, PHPVersionBody, Preview } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Field, Select } from "../../components/Field";
import { MonoText } from "../../components/MonoText";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/site";
import { PlanReview } from "../duyet-plan";
import { phpKey } from "../stack/model";
import { ErrorNote } from "./ErrorNote";

const labelClass = "m-0 text-[12px] font-medium text-muted";

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
  const { track } = useJobTracker();
  const { isAdmin } = useIdentity();
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
      // Ở lại trang đang dùng; thẻ theo dõi ở góc dưới báo tiến độ và tải lại dữ liệu khi job xong.
      track(job.job_id);
      setOpen(null);
    },
  });
  useEffect(() => {
    if (open) {
      previewRef.current?.scrollIntoView({ block: "start" });
      previewRef.current?.focus();
    }
  }, [open]);

  return (
    <>
      <Card title={t.phpHeading} titleId="php-site">
        <dl className="m-0 flex flex-wrap gap-x-[40px] gap-y-[12px]">
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
            <p className="mt-[14px] mb-0 max-w-[72ch] text-muted">
              {isAdmin ? (
                <>
                  {t.phpNone}
                  <Link to="/stack">{t.phpNoneLink}</Link>
                  {t.phpNoneTail}
                </>
              ) : (
                t.phpNoneUser
              )}
            </p>
          ) : (
            <div className="mt-[14px] border-t border-hair pt-[14px]">
              <div className="flex flex-wrap items-end gap-x-[14px] gap-y-[10px]">
                <div className="min-w-[140px]">
                  <Field label={t.phpChoose} htmlFor="php-site-version">
                    <Select id="php-site-version" mono value={value} onChange={(e) => setChoice(e.target.value)}>
                      {options.map((v) => (
                        <option key={v} value={v}>
                          {v}
                        </option>
                      ))}
                    </Select>
                  </Field>
                </div>
                <Button
                  onClick={() => {
                    // Bản xem trước cũ không còn đúng với lựa chọn mới: bỏ nó trước khi dựng bản mới.
                    setOpen(null);
                    run.reset();
                    dry.mutate({ version: value });
                  }}
                  aria-describedby={blocked ? "php-bi-chan" : undefined}
                  disabled={dry.isPending || run.isPending || value === version || blocked !== undefined}
                >
                  {dry.isPending ? t.phpPreviewing : t.phpChange}
                </Button>
              </div>
              {blocked && (
                <p id="php-bi-chan" className="mt-[10px] mb-0 max-w-[72ch] text-[13px] text-muted">
                  {blocked}
                </p>
              )}
              <p className="mt-[10px] mb-0 max-w-[72ch] text-muted">{t.phpHint}</p>
            </div>
          ))}

        {dry.isError && (
          <div className="pt-[12px]">
            <ErrorNote error={dry.error} heading={t.phpFailed(dry.variables.version)} checksHeading={t.phpChecksHeading} />
          </div>
        )}
      </Card>
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
    </>
  );
}
