// Màn Bảng điều khiển (trang chủ, "/"): mục 7 của docs/GIAO-DIEN.md và trang
// mẫu docs/mau-giao-dien/bang-dieu-khien-*.html. Hai cột bằng flex có xuống
// dòng: cột chính (tên màn, bản đồ tuyến, máy, lịch sử) và cột bên (sự cố,
// cảnh báo, chờ duyệt); màn hẹp thì cột bên rơi xuống dưới.
//
// Dữ liệu: GET /api/v1/overview qua useOverview, cùng truy vấn mà khung trang
// dùng cho menu và logo nên chỉ một request.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { errorMessage, post } from "../../api/client";
import { queryKeys, useOverview } from "../../api/queries";
import type { HealthReport, Status } from "../../api/types";
import { Button } from "../../components/Button";
import { ScreenHeader } from "../../components/ScreenHeader";
import { StatusMark } from "../../components/StatusMark";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bang-dieu-khien";
import { strings as khung } from "../../i18n/khung";
import { History } from "./History";
import { Machine } from "./Machine";
import { checksWith, fmtWhen } from "./model";
import { RouteMap } from "./RouteMap";
import { Side } from "./Side";
import { TpanChecks } from "./TpanChecks";

const legend: Status[] = ["pass", "warn", "fail", "unknown"];

export default function BangDieuKhien() {
  const t = useStrings(strings);
  const kt = useStrings(khung);
  const { lang } = useLang();
  const qc = useQueryClient();
  const q = useOverview();
  const o = q.data;

  // "Kiểm lại": chạy health đầy đủ ngay (như bộ hẹn giờ, có ghi kết quả và mở đóng sự cố) rồi đọc lại overview.
  const recheck = useMutation({
    mutationFn: () => post<HealthReport>("/api/v1/health/run"),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.overview }),
  });

  const summary = o
    ? t.summaryLine(
        o.sites.filter((r) => r.site.status === "active").length,
        o.last_run ? fmtWhen(o.last_run, lang, t.yesterday) : null,
        o.incidents.length,
        checksWith(o, "warn").length,
      )
    : t.summary;

  return (
    <div className="flex flex-wrap gap-x-[40px]">
      <div className="min-w-0 flex-[999_1_560px]">
        <ScreenHeader title={t.title} summary={summary} />

        {q.isPending && <p className="mt-[24px] mb-0 text-muted">{kt.loading}</p>}
        {!o && q.isError && (
          <div role="alert" className="mt-[24px]">
            <p className="m-0">
              {t.loadFailed} {errorMessage(q.error, lang)}
            </p>
            <Button className="mt-[12px]" onClick={() => void q.refetch()}>
              {t.retry}
            </Button>
          </div>
        )}

        {o && (
          <>
            <div className="flex flex-wrap items-end justify-between gap-x-[24px] gap-y-[10px] pt-[26px] pb-[14px]">
              <div className="flex flex-wrap items-center gap-x-[16px] gap-y-[8px]">
                <h2 className="m-0 text-[16px] font-semibold">{t.mapTitle}</h2>
                <Button onClick={() => recheck.mutate()} disabled={recheck.isPending}>
                  {recheck.isPending ? t.rechecking : t.recheck}
                </Button>
              </div>
              <ul aria-label={t.legend} className="m-0 flex list-none flex-wrap gap-x-[18px] gap-y-[8px] p-0">
                {legend.map((s) => (
                  <li key={s} className="flex items-center gap-[8px] text-[13px] text-muted">
                    <StatusMark status={s} />
                    <span>{kt.status[s]}</span>
                  </li>
                ))}
              </ul>
            </div>
            {recheck.isError && (
              <p role="alert" className="mt-0 mb-[12px]">
                {t.recheckFailed} {errorMessage(recheck.error, lang)}
              </p>
            )}
            <RouteMap o={o} />
            <Machine m={o.machine} />
            <TpanChecks checks={o.tpan_checks} />
            <History jobs={o.jobs} />
          </>
        )}
      </div>
      {o && <Side o={o} />}
    </div>
  );
}
