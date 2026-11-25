// Khối "Tài khoản quản trị WordPress" trên trang một site (chủ site và admin):
// đặt lại mật khẩu khi quên. Nút → bản xem trước → chạy; mật khẩu mới hiện một
// lần ở trang chi tiết của việc đó (JobDetail.secret), như lúc tạo site.

import type { Site } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { usePreviewedOp } from "../../components/PreviewedOp";
import { useStrings } from "../../i18n";
import { strings as chung } from "../../i18n/m13-chung";
import { strings } from "../../i18n/site";

export function WpPasswordBlock({ site }: { site: Site }) {
  const t = useStrings(strings);
  const m = useStrings(chung);
  const d = site.domain;
  const op = usePreviewedOp([["site", d]]);
  return (
    <>
      <Card title={t.wpPassHeading} titleId="wp-mat-khau">
        <p className="m-0 max-w-[72ch] text-muted">{t.wpPassHint}</p>
        <div className="pt-[12px]">
          <Button
            disabled={op.pending}
            aria-label={t.wpPassFor(d)}
            onClick={() =>
              void op.ask({
                id: "wp-password",
                method: "POST",
                path: `/api/v1/sites/${encodeURIComponent(d)}/wp-password`,
                warning: t.wpPassWarning(d),
                errorHeading: t.wpPassErrorHeading(d),
              })
            }
          >
            {op.busy("wp-password") ? m.previewing : t.wpPassButton}
          </Button>
        </div>
      </Card>
      {op.panel}
    </>
  );
}
