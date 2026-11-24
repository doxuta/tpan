// Trang /bat-hai-buoc (M20, bare như Đăng nhập): khi chính sách của máy bắt user bật
// hai bước mà họ chưa bật (SessionInfo.mfa_enroll_required), Layout chuyển mọi màn
// tới đây. Có lời giải thích, luồng bật dùng chung với trang Tài khoản, và nút
// Đăng xuất. Bật xong và bấm "Đã lưu mã" thì đọc lại phiên rồi về trang chủ.
//
// Trang nằm ngoài khung nên tự lo chuyện chưa đăng nhập, và không dùng gì cần
// provider của khung (job, AI). Mười mã dự phòng nằm trong state `codes` của trang:
// phiên tự đọc lại khi cửa sổ lấy lại focus (người dùng đi chép mã sang chỗ khác
// rồi quay lại), lúc đó mfa_enroll_required đã sai, nên trang CHỈ về trang chủ
// khi người dùng bấm "Đã lưu mã", không theo phiên.

import { useState } from "react";
import { Navigate, useNavigate } from "react-router";
import { loginHref, loginPath, post } from "../../api/client";
import { useRefreshMFA } from "../../api/m20";
import { useSession } from "../../api/queries";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { ScreenHeader } from "../../components/ScreenHeader";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/bat-hai-buoc";
import { BareFrame } from "../dang-nhap/Frame";
import { EnrollFlow } from "../tai-khoan/EnrollFlow";
import { RecoveryCodes } from "../tai-khoan/RecoveryCodes";

export default function BatHaiBuoc() {
  const t = useStrings(strings);
  const session = useSession();
  const refresh = useRefreshMFA();
  const navigate = useNavigate();
  const [codes, setCodes] = useState<string[] | null>(null);
  const [saving, setSaving] = useState(false);
  const [outBusy, setOutBusy] = useState(false);
  const [outFailed, setOutFailed] = useState(false);

  if (session.isPending) return <p className="px-[32px] py-[24px] text-muted">{t.loading}</p>;
  if (session.data?.logged_in !== true) return <Navigate to={loginHref("/bat-hai-buoc")} replace />;
  // Không còn bị bắt bật (đã bật rồi, hoặc chính sách đã đổi) và không đang giữ mã: về trang chủ.
  if (session.data.mfa_enroll_required !== true && codes === null) return <Navigate to="/" replace />;

  const name = session.data.user?.name ?? "";

  const saved = async () => {
    setSaving(true);
    await refresh();
    navigate("/", { replace: true });
  };

  const logout = async () => {
    setOutBusy(true);
    setOutFailed(false);
    try {
      await post<void>("/api/v1/logout");
      // Tải lại hẳn để bỏ mọi dữ liệu của phiên cũ đang giữ trong bộ nhớ.
      window.location.assign(loginPath);
    } catch {
      setOutFailed(true);
      setOutBusy(false);
    }
  };

  // Mười mã dự phòng là một thẻ riêng (RecoveryCodes), không đặt trong thẻ khác.
  if (codes !== null) {
    return (
      <BareFrame wide>
        <RecoveryCodes codes={codes} onSaved={() => void saved()} busy={saving} />
        <p className="m-0 max-w-[72ch] text-[13px] text-muted">{t.savedHint}</p>
      </BareFrame>
    );
  }

  return (
    <BareFrame wide>
      <Card
        footer={
          <div className="flex flex-wrap items-center justify-between gap-x-[16px] gap-y-[8px]">
            <span>{t.loggedAs(name)}</span>
            <Button size="sm" onClick={() => void logout()} disabled={outBusy}>
              {outBusy ? t.loggingOut : t.logout}
            </Button>
          </div>
        }
      >
        <ScreenHeader title={t.title} summary={t.summary} />
        {outFailed && (
          <p role="alert" className="mt-[12px] mb-0">
            {t.logoutFailed}
          </p>
        )}
        <div className="mt-[16px] flex flex-col gap-[20px]">
          <div className="flex flex-col gap-[12px]">
            <div>
              <h2 className="m-0 text-[14px] font-semibold">{t.whyTitle}</h2>
              <p className="mt-[4px] mb-0 max-w-[72ch] text-muted">{t.whyBody}</p>
            </div>
            <div>
              <h2 className="m-0 text-[14px] font-semibold">{t.howTitle}</h2>
              <p className="mt-[4px] mb-0 max-w-[72ch] text-muted">{t.howBody}</p>
            </div>
          </div>
          <EnrollFlow user={name} onEnabled={setCodes} />
        </div>
      </Card>
    </BareFrame>
  );
}
