// Màn Cài đặt (mục 10 của KE-HOACH.md, màn 10): mật khẩu quản trị, các phiên
// đang đăng nhập, ngôn ngữ, sáng hoặc tối. AI và MCP chưa có (M7, M8) nên chỉ
// có một dòng "có ở bản sau".

import { ScreenHeader } from "../../components/ScreenHeader";
import { SegmentButton } from "../../components/SegmentButton";
import { useLang, useStrings, type Lang } from "../../i18n";
import { strings } from "../../i18n/cai-dat";
import { strings as khung } from "../../i18n/khung";
import { useTheme, type ThemeChoice } from "../../theme";
import { MatKhau } from "./MatKhau";
import { Phien } from "./Phien";

export default function CaiDat() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang, setLang } = useLang();
  const { theme, choice, setTheme } = useTheme();

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />
      <MatKhau />
      <Phien />

      <section aria-labelledby="ngon-ngu-tieu-de" className="pt-[28px]">
        <h2 id="ngon-ngu-tieu-de" className="m-0 mb-[10px] text-[16px] font-semibold">
          {t.langHeading}
        </h2>
        <SegmentButton<Lang>
          label={t.langHeading}
          value={lang}
          onChange={setLang}
          options={[
            { value: "vi", label: t.langVi },
            { value: "en", label: t.langEn },
          ]}
        />
      </section>

      <section aria-labelledby="giao-dien-tieu-de" className="pt-[28px]">
        <h2 id="giao-dien-tieu-de" className="m-0 mb-[10px] text-[16px] font-semibold">
          {t.themeHeading}
        </h2>
        <SegmentButton<ThemeChoice>
          label={t.themeHeading}
          value={choice}
          onChange={setTheme}
          options={[
            { value: "light", label: k.light },
            { value: "dark", label: k.dark },
            { value: "system", label: t.themeSystem },
          ]}
        />
        {choice === "system" && <p className="mt-[10px] mb-0 text-muted">{t.themeSystemNow(theme === "dark")}</p>}
      </section>

      <section aria-labelledby="ai-tieu-de" className="pt-[28px]">
        <h2 id="ai-tieu-de" className="m-0 text-[16px] font-semibold">
          {t.aiHeading}
        </h2>
        <p className="mt-[6px] mb-0 text-muted">{t.aiLater}</p>
      </section>
    </>
  );
}
