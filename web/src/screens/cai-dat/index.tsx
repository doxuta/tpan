// Màn Cài đặt (mục 10 của KE-HOACH.md, màn 10; giao diện bản 2, mục 3.7 của
// M16-thiet-ke.md): mỗi mục một thẻ. Mật khẩu quản trị, các phiên đang đăng
// nhập, khối "Phiên bản và cập nhật" (M15, mục 6.14; chỉ admin, dùng chung với
// màn Cập nhật), giao diện (ngôn ngữ, sáng hoặc tối, Gợi ý), mục "AI và bộ não"
// (M8) và mục Agent và MCP (M7). M20 thêm cho admin ba thẻ: chính sách hai bước,
// token API, tên miền của panel. Mục Địa chỉ IP (admin, chỉ đọc) đứng sau tên miền.
//
// Mục Giao diện dùng đúng state của menu tài khoản (useLang, useTheme, useTips),
// nên đổi ở đây hay ở menu là cùng một thay đổi.

import { useWho } from "../../api/m13-users";
import { Card } from "../../components/Card";
import { Checkbox } from "../../components/Field";
import { ScreenHeader } from "../../components/ScreenHeader";
import { SegmentButton } from "../../components/SegmentButton";
import { useLang, useStrings, type Lang } from "../../i18n";
import { strings } from "../../i18n/cai-dat";
import { strings as khung } from "../../i18n/khung";
import { useTheme, type ThemeChoice } from "../../theme";
import { useTips } from "../../tips";
import { Self } from "../cap-nhat/Self";
import { MatKhau } from "./MatKhau";
import { Phien } from "./Phien";
import { Agent } from "./Agent";
import { BoNao } from "./BoNao";
import { ChinhSach } from "./ChinhSach";
import { DiaChiIP } from "./DiaChiIP";
import { TenMien } from "./TenMien";
import { TokenAdmin } from "./TokenAdmin";

const groupLabel = "mb-[6px] text-[14px] font-medium";

export default function CaiDat() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang, setLang } = useLang();
  const { theme, choice, setTheme } = useTheme();
  const { tips, setTips } = useTips();
  const { isAdmin } = useWho();

  return (
    <>
      <ScreenHeader crumbs={[{ label: k.groups.taiKhoan }, { label: t.title }]} title={t.title} summary={isAdmin ? t.summary : t.summaryUser} />
      <MatKhau />
      <Phien />
      {/* M20: hai bước (chính sách), token API (mức tối đa, mọi token) và tên miền của panel: route chỉ admin. */}
      {isAdmin && (
        <>
          <ChinhSach />
          <TokenAdmin />
          <TenMien />
          <DiaChiIP />
        </>
      )}
      {/* Route /api/v1/self chỉ admin: user thường không thấy khối này. */}
      {isAdmin && <Self />}

      <Card title={t.themeHeading} titleId="giao-dien-tieu-de">
        <div className="flex flex-col gap-[20px]">
          <div>
            <div className={groupLabel}>{t.langHeading}</div>
            <SegmentButton<Lang>
              label={t.langHeading}
              value={lang}
              onChange={setLang}
              options={[
                { value: "vi", label: t.langVi },
                { value: "en", label: t.langEn },
              ]}
            />
          </div>
          <div>
            <div className={groupLabel}>{t.themeLabel}</div>
            <SegmentButton<ThemeChoice>
              label={t.themeLabel}
              value={choice}
              onChange={setTheme}
              options={[
                { value: "light", label: k.light },
                { value: "dark", label: k.dark },
                { value: "system", label: t.themeSystem },
              ]}
            />
            {choice === "system" && <p className="mt-[10px] mb-0 text-muted">{t.themeSystemNow(theme === "dark")}</p>}
          </div>
          <label className="flex max-w-[72ch] items-start gap-[10px]">
            <span className="flex pt-[2px]">
              <Checkbox label={k.tipsItem} checked={tips} onChange={(e) => setTips(e.target.checked)} />
            </span>
            <span className="min-w-0">
              <span className="block font-medium">{k.tipsItem}</span>
              <span className="block text-[13px] text-muted">{t.tipsHint}</span>
            </span>
          </label>
        </div>
      </Card>

      {/* AI, bộ não và MCP chỉ dành cho admin (mục 6.12): user thường không thấy hai mục này. */}
      {isAdmin && (
        <>
          <BoNao />
          <Agent />
        </>
      )}
    </>
  );
}
