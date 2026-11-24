// Thẻ "Lồng" ở tab Tổng quan của trang một site (M17, mục 6.15 của KE-HOACH.md):
// site đang trong lồng hay ngoài lồng, mức tối đa của gói, và khi trong lồng thì
// số đang dùng (RAM, đỉnh, tiến trình, số lần chạm mức tối đa). Admin có nút
// Vào lồng / Ra khỏi lồng, đi qua bản xem trước như mọi việc đổi máy.
//
// Trong lồng hay ngoài lồng không phải tình trạng của máy, nên không có xanh
// hay đỏ ở đây. Chỉ "chờ vào" và "chờ ra" (máy lệch với gói) mới có dấu cảnh báo.
//
// Dữ liệu: admin đọc GET /cage (có cả gói, cờ want, số đo); user thường không
// gọi được route đó, nên dùng site.caged và GET /sites/{domain}/usage (chỉ gọi
// khi site đang trong lồng, vì site ngoài lồng trả 404 usage_not_caged).

import { useEffect, useRef } from "react";
import { Link } from "react-router";
import { useCages, useSiteUsage, cageRequest } from "../../api/m17";
import type { CageState } from "../../api/m17-types";
import { usePlanFlow } from "../../api/m13-users";
import type { Site } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { StatusLine } from "../../components/StatusMark";
import { UsageMeter } from "../../components/UsageMeter";
import { useStrings } from "../../i18n";
import { strings as goi } from "../../i18n/goi";
import { strings as khung } from "../../i18n/khung";
import { strings as chung } from "../../i18n/m13-chung";
import { strings } from "../../i18n/site";
import { PlanReview } from "../duyet-plan";
import { limitSummary } from "../goi/model";
import { cap, hits, mib, pct } from "../tai-nguyen/model";
import { ErrorNote } from "./ErrorNote";

type Props = { site: Site; admin: boolean };

export function CageBlock({ site, admin }: Props) {
  const t = useStrings(strings).cage;
  const g = useStrings(goi);
  const k = useStrings(khung);
  const m = useStrings(chung);
  const d = site.domain;
  const flow = usePlanFlow();
  const resultRef = useRef<HTMLDivElement>(null);

  const cages = useCages(admin);
  const row = admin ? cages.data?.find((r) => r.domain === d) : undefined;
  const caged = admin ? (row ? row.caged : Boolean(site.caged)) : Boolean(site.caged);
  const detail = useSiteUsage(d, "day", !admin && caged);
  const usage = admin ? row?.usage : detail.data?.usage;
  const limits = admin ? row?.limits : detail.data?.limits;
  // User thường không biết gói có muốn lồng không (server không cho đọc), nên chỉ có hai trạng thái.
  const state: CageState = row?.state ?? (caged ? "trong" : "ngoai");
  const pkg = row?.package ?? "";

  const shown = flow.open ?? (flow.dry.isError ? flow.asked : null);
  useEffect(() => {
    if (shown) {
      resultRef.current?.scrollIntoView({ block: "start" });
      resultRef.current?.focus();
    }
  }, [shown]);

  const enable = !caged;
  const noWant = admin && row !== undefined && !caged && !row.want;
  const showLimits = caged || row?.want === true;
  const waiting = state === "can-vao" || state === "can-ra";

  const ramCap = cap(limits?.ram_mb);
  const tasksCap = cap(limits?.tasks);

  return (
    <>
      <Card title={t.heading} titleId="long-site">
        {admin && cages.isPending ? (
          <p className="m-0 text-muted">{k.loading}</p>
        ) : admin && cages.isError ? (
          <div>
            <ErrorNote error={cages.error} heading={t.loadFailed} />
            <div className="pt-[12px]">
              <Button onClick={() => void cages.refetch()}>{k.retry}</Button>
            </div>
          </div>
        ) : (
          <>
            <p className="m-0 font-medium">{waiting ? <StatusLine status="warn">{t.state[state]}</StatusLine> : t.state[state]}</p>
            <p className="mt-[4px] mb-0 max-w-[72ch] text-muted">{t.stateNote[state](pkg)}</p>

            <dl className="m-0 mt-[12px] flex flex-wrap gap-x-[32px] gap-y-[12px]">
              {admin && pkg && (
                <div>
                  <dt className="text-[12px] font-medium text-muted">{t.package}</dt>
                  <dd className="m-0 mt-[2px]">{pkg}</dd>
                </div>
              )}
              {limits && showLimits && (
                <div className="min-w-0 basis-full">
                  <dt className="text-[12px] font-medium text-muted">{t.limits}</dt>
                  <dd className="m-0 mt-[2px] [overflow-wrap:anywhere]">{limitSummary(limits, g.sum)}</dd>
                </div>
              )}
              {caged && usage?.running && (
                <>
                  <div>
                    <dt className="text-[12px] font-medium text-muted">{t.ram}</dt>
                    <dd className="m-0 mt-[2px]">
                      <UsageMeter used={`${mib(usage.mem_current)} MiB`} limit={ramCap === null ? null : `${ramCap} MiB`} percent={pct(mib(usage.mem_current), ramCap)} />
                    </dd>
                  </div>
                  <div>
                    <dt className="text-[12px] font-medium text-muted">{t.peak}</dt>
                    <dd className="m-0 mt-[2px] tabular-nums">{mib(usage.mem_peak)} MiB</dd>
                  </div>
                  <div>
                    <dt className="text-[12px] font-medium text-muted">{t.tasks}</dt>
                    <dd className="m-0 mt-[2px]">
                      <UsageMeter used={String(usage.tasks)} limit={tasksCap === null ? null : String(tasksCap)} percent={pct(usage.tasks, tasksCap)} />
                    </dd>
                  </div>
                  <div>
                    <dt className="text-[12px] font-medium text-muted">{t.hits}</dt>
                    <dd className="m-0 mt-[2px] tabular-nums" title={t.hitsTitle(usage.oom_kills, usage.tasks_hits)}>
                      {hits(usage)}
                    </dd>
                  </div>
                </>
              )}
            </dl>
            {caged && usage && !usage.running && <p className="mt-[12px] mb-0 text-muted">{t.notRunning}</p>}
            {caged && !admin && detail.isError && (
              <div className="pt-[12px]">
                <ErrorNote error={detail.error} heading={t.usageFailed} />
              </div>
            )}
            {caged && (
              <p className="mt-[12px] mb-0">
                <Link to="/usage">{t.usageLink}</Link>
              </p>
            )}

            {admin && (
              <div className="pt-[12px]">
                <Button
                  disabled={flow.dry.isPending || flow.run.isPending || noWant || row === undefined}
                  aria-describedby={noWant ? "long-khong-bat" : undefined}
                  title={noWant ? t.noWantLead(pkg).trim() : undefined}
                  onClick={() => flow.ask(cageRequest(d, enable))}
                >
                  {flow.dry.isPending ? m.previewing : enable ? t.enable : t.disable}
                </Button>
                {noWant && (
                  <p id="long-khong-bat" className="mt-[10px] mb-0 max-w-[72ch] text-[13px] text-muted">
                    {t.noWantLead(pkg)}
                    <Link to="/packages">{t.noWantLink}</Link>
                  </p>
                )}
                {flow.dry.isError && flow.asked && (
                  <div ref={resultRef} tabIndex={-1} className="pt-[12px] outline-offset-0">
                    <ErrorNote error={flow.dry.error} heading={enable ? t.enableErrorHeading(d) : t.disableErrorHeading(d)} checksHeading={m.checksHeading} />
                  </div>
                )}
              </div>
            )}
          </>
        )}
      </Card>
      {flow.open && (
        <div ref={resultRef} tabIndex={-1} className="flex flex-col gap-[20px] outline-offset-0">
          <p role="alert" className="m-0 max-w-[72ch] font-semibold">
            {enable ? t.enableWarning(d, pkg) : t.disableWarning(d)}
          </p>
          <PlanReview preview={flow.open.preview} onRun={flow.start} running={flow.run.isPending} error={flow.run.error} onCancel={flow.close} />
        </div>
      )}
    </>
  );
}
