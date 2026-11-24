// Thẻ "Đăng nhập hai bước" của trang Tài khoản (neo #hai-buoc). Tình trạng từ
// GET /api/v1/account/2fa; chưa bật thì có luồng bật (EnrollFlow); đã bật thì có
// tạo lại mã dự phòng và tắt, cả hai qua hộp hỏi lại có mật khẩu và mã.
//
// Mười mã dự phòng vừa tạo nằm trong state `codes` của CHÍNH thẻ này (thẻ luôn có
// mặt), không suy ra từ kết quả truy vấn: truy vấn tự đọc lại khi cửa sổ lấy lại
// focus và đổi "chưa bật" thành "đã bật" giữa lúc người dùng chưa kịp lưu mã.

import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useRefreshMFA, useMFA, useMFADisable, useMFARecovery } from "../../api/m20";
import { useWho } from "../../api/m13-users";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Notice } from "../../components/Notice";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/tai-khoan";
import { Row, Rows } from "../cai-dat/Rows";
import { ConfirmCodeDialog } from "./ConfirmCode";
import { EnrollFlow } from "./EnrollFlow";
import { fmtTime, recoveryLowNow } from "./model";
import { RecoveryCodes } from "./RecoveryCodes";

export function HaiBuoc() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const { name } = useWho();
  const q = useMFA();
  const refresh = useRefreshMFA();
  const disable = useMFADisable();
  const recovery = useMFARecovery();
  const [codes, setCodes] = useState<string[] | null>(null);
  const [dialog, setDialog] = useState<"recovery" | "disable" | null>(null);

  const saved = async () => {
    await refresh();
    setCodes(null);
  };

  // Mười mã dự phòng là một thẻ riêng (RecoveryCodes), không đặt trong thẻ này.
  if (codes !== null) return <RecoveryCodes codes={codes} onSaved={() => void saved()} />;

  return (
    <Card id="hai-buoc" title={t.tsHeading} titleId="hai-buoc-tieu-de">
      {q.isPending ? (
        <p className="m-0 text-muted">{k.loading}</p>
      ) : q.isError ? (
        <div className="flex flex-col gap-[12px]">
          <p className="m-0">
            {t.tsLoadFailed} {errorMessage(q.error, lang)}
          </p>
          <div>
            <Button onClick={() => void q.refetch()}>{k.retry}</Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-[16px]">
          <p className="m-0 max-w-[72ch] text-muted">{t.tsIntro}</p>
          {!q.data.enabled ? (
            <EnrollFlow user={name} onEnabled={setCodes} />
          ) : (
            <>
              <Rows>
                <Row label={t.tsStatus}>
                  <Tag>{t.tsOn}</Tag>
                </Row>
                {q.data.enabled_at && <Row label={t.tsSince}>{fmtTime(q.data.enabled_at, lang)}</Row>}
                <Row label={t.tsLeft}>{t.tsLeftValue(q.data.recovery_left)}</Row>
                <Row label={t.tsPolicy}>{t.tsPolicyValue[q.data.policy]}</Row>
              </Rows>
              {recoveryLowNow(q.data.recovery_left) && (
                <Notice kind="warn" role="status">
                  {t.tsLowNote(q.data.recovery_left)}
                </Notice>
              )}
              <div className="flex flex-wrap gap-[8px]">
                <Button onClick={() => setDialog("recovery")}>{t.tsRecoveryBtn}</Button>
                <Button onClick={() => setDialog("disable")} disabled={q.data.required} title={q.data.required ? t.tsRequiredNote : undefined}>
                  {t.tsOffBtn}
                </Button>
              </div>
              {q.data.required && <p className="m-0 max-w-[72ch] text-[13px] text-muted">{t.tsRequiredNote}</p>}
            </>
          )}
        </div>
      )}

      {dialog === "recovery" && (
        <ConfirmCodeDialog
          title={t.recoTitle}
          description={t.recoDesc}
          confirmLabel={t.recoConfirm}
          user={name}
          run={recovery.mutateAsync}
          onDone={(r) => {
            setCodes(r.recovery_codes);
            setDialog(null);
          }}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog === "disable" && <ConfirmCodeDialog title={t.offTitle} description={t.offDesc} confirmLabel={t.offConfirm} user={name} run={disable.mutateAsync} onDone={() => setDialog(null)} onClose={() => setDialog(null)} />}
    </Card>
  );
}
