// Khối "Tạm khoá" trên trang một site (mục 6.12 của KE-HOACH.md): nói site có
// đang khoá không, khoá vì sao (riêng hay theo user), khách thấy gì, và nút Tạm
// khoá / Mở khoá cho admin. Như mọi việc đổi máy: nút → bản xem trước → chạy.
//
// User thường chỉ đọc: thấy site của mình đang khoá và hậu quả, không có nút.
// Site không khoá mà người xem không phải admin thì khối này không hiện gì.

import type { Site } from "../../api/types";
import { Button } from "../../components/Button";
import { SuspendTag } from "../../components/SuspendTag";
import { usePreviewedOp, type Op } from "../../components/PreviewedOp";
import { useLang, useStrings } from "../../i18n";
import { strings as chung } from "../../i18n/m13-chung";
import { strings } from "../../i18n/site";
import { siteDbSuspendPath, siteSuspendPath } from "../../api/m13-dbs";
import { formatWhen } from "../thung-rac/model";

type Props = { site: Site; admin: boolean };

export function SuspendBlock({ site, admin }: Props) {
  const t = useStrings(strings);
  const m = useStrings(chung);
  const { lang } = useLang();
  const d = site.domain;
  // Trang site đọc lại GET /sites/{domain} sau khi job xong (khoá ["site", domain]).
  const op = usePreviewedOp([["site", d]]);

  const siteOp = (on: boolean): Op => ({
    id: on ? "suspend" : "unsuspend",
    method: "POST",
    path: siteSuspendPath(d, on),
    warning: on ? t.suspendWarning(d) : t.unsuspendWarning(d),
    errorHeading: on ? t.suspendErrorHeading(d) : t.unsuspendErrorHeading(d),
  });
  const dbOp: Op = {
    id: "db-unsuspend",
    method: "POST",
    path: siteDbSuspendPath(d, false),
    warning: t.dbUnsuspendWarning(d),
    errorHeading: t.dbUnsuspendErrorHeading(d),
  };

  const suspended = site.suspended;
  const dbSuspended = site.db_suspended;
  if (!suspended && !dbSuspended && !admin) return null;

  return (
    <section aria-labelledby="tam-khoa">
      <h2 id="tam-khoa" className="m-0 pt-[26px] pb-[10px] text-[16px] font-semibold">
        {t.suspendHeading}
      </h2>

      {suspended ? (
        <div className="border-t border-hair pt-[12px]">
          <p className="m-0 flex flex-wrap items-center gap-x-[10px] gap-y-[6px] font-medium">
            <SuspendTag how={suspended} />
            {t.suspendedNow}
            {site.suspended_at && <span className="font-normal text-muted">{t.suspendedSince(formatWhen(site.suspended_at, lang))}</span>}
          </p>
          <p className="mt-[8px] mb-0 max-w-[720px]">{suspended === "self" ? t.suspendedSelf : t.suspendedUser}</p>
          <p className="mt-[6px] mb-0 max-w-[720px] text-muted">{t.suspendedEffect}</p>
          {admin && (
            <div className="pt-[12px]">
              <Button disabled={op.pending} aria-label={t.unsuspendFor(d)} onClick={() => void op.ask(siteOp(false))}>
                {op.busy("unsuspend") ? m.previewing : t.unsuspend}
              </Button>
            </div>
          )}
        </div>
      ) : (
        admin && (
          <div className="border-t border-hair pt-[12px]">
            <p className="m-0 max-w-[720px] text-muted">{t.suspendHint}</p>
            <div className="pt-[12px]">
              <Button disabled={op.pending} aria-label={t.suspendFor(d)} onClick={() => void op.ask(siteOp(true))}>
                {op.busy("suspend") ? m.previewing : t.suspend}
              </Button>
            </div>
          </div>
        )
      )}

      {dbSuspended && (
        <div className="mt-[12px] border-t border-hair pt-[12px]">
          <p className="m-0 flex flex-wrap items-center gap-x-[10px] gap-y-[6px] font-medium">
            <SuspendTag how={dbSuspended} label={m.dbLocked} />
            {t.dbSuspendedLine}
          </p>
          {admin && (
            <div className="pt-[12px]">
              <Button disabled={op.pending} aria-label={t.dbUnsuspendFor(d)} onClick={() => void op.ask(dbOp)}>
                {op.busy("db-unsuspend") ? m.previewing : t.dbUnsuspend}
              </Button>
            </div>
          )}
        </div>
      )}

      {op.panel}
    </section>
  );
}
