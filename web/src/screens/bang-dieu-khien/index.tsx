// Màn Bảng điều khiển (trang chủ của admin "/", và "/dashboard" của user
// thường): giao diện bản 2, mục 3.7 của docs/thay-doi/M16-thiet-ke.md. Hai cột
// bằng flex có xuống dòng: cột chính (bản đồ tuyến, máy, TPAN tự kiểm, lịch sử)
// và cột bên (sự cố, cảnh báo, chờ duyệt); màn hẹp thì cột bên rơi xuống dưới.
// Mỗi khối là một thẻ; các thẻ cách nhau 20 px như mọi màn (main#noi-dung là cột
// flex, màn không tự thêm khoảng trên).
//
// Dữ liệu: GET /api/v1/overview qua useOverview, cùng truy vấn mà khung trang
// dùng cho menu và logo nên chỉ một request.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { errorMessage, post } from "../../api/client";
import { useWho } from "../../api/m13-users";
import { queryKeys, useOverview } from "../../api/queries";
import type { HealthReport, StackSummary } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Notice } from "../../components/Notice";
import { ScreenHeader } from "../../components/ScreenHeader";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bang-dieu-khien";
import { strings as khung } from "../../i18n/khung";
import { History } from "./History";
import { Machine } from "./Machine";
import { checksWith, fmtWhen } from "./model";
import { QuotaBlock } from "./Quota";
import { RouteMap } from "./RouteMap";
import { Side } from "./Side";
import { TpanChecks } from "./TpanChecks";

/** Nhãn stack (LEMP hoặc LAMP, web server, PHP) hoặc "chưa cài stack": mọi vai thấy, như bản 1 vẽ trên thanh đầu trang. */
function StackTags({ stack }: { stack: StackSummary }) {
  const t = useStrings(strings);
  if (!stack.installed) return <Tag tone="surface">{t.stackNone}</Tag>;
  return (
    <>
      {stack.web && <Tag tone="surface">{stack.web === "nginx" ? "LEMP" : "LAMP"}</Tag>}
      {stack.web && <Tag tone="surface">{stack.web}</Tag>}
      {stack.php_version && <Tag tone="surface">{t.phpTag(stack.php_version)}</Tag>}
    </>
  );
}

export default function BangDieuKhien() {
  const t = useStrings(strings);
  const kt = useStrings(khung);
  const { lang } = useLang();
  const { isAdmin } = useWho();
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
    <>
      <ScreenHeader
        title={t.title}
        summary={isAdmin ? summary : t.summaryUser}
        meta={o && <StackTags stack={o.stack} />}
        actions={
          // POST /health/run chỉ dành cho admin (docs/KE-HOACH.md mục 6.12 ("Đã chốt khi làm M13")): user thường vẫn thấy sự cố của mình nhưng không bấm kiểm lại.
          isAdmin && o ? (
            <Button onClick={() => recheck.mutate()} disabled={recheck.isPending}>
              {recheck.isPending ? t.rechecking : t.recheck}
            </Button>
          ) : undefined
        }
      />

      {recheck.isError && (
        <Notice kind="fail" role="alert">
          {t.recheckFailed} {errorMessage(recheck.error, lang)}
        </Notice>
      )}
      {q.isPending && (
        <Card>
          <p className="m-0 text-muted">{kt.loading}</p>
        </Card>
      )}
      {!o && q.isError && (
        <Card>
          <div role="alert">
            <p className="mt-0 mb-[12px]">
              {t.loadFailed} {errorMessage(q.error, lang)}
            </p>
            <Button onClick={() => void q.refetch()}>{t.retry}</Button>
          </div>
        </Card>
      )}

      {o && (
        <div className="flex flex-wrap items-start gap-[20px]">
          <div className="flex min-w-0 flex-[999_1_560px] flex-col gap-[20px]">
            <RouteMap o={o} />
            {/* User thường: hạn mức gói thay cho số liệu máy, và không có khối TPAN tự kiểm (mục 6.12). */}
            {isAdmin ? (
              <>
                <Machine m={o.machine} />
                <TpanChecks checks={o.tpan_checks} />
              </>
            ) : (
              o.quota && <QuotaBlock quota={o.quota} />
            )}
            <History jobs={o.jobs} />
          </div>
          <Side o={o} admin={isAdmin} />
        </div>
      )}
    </>
  );
}
