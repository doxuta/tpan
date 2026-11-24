// Khung của các trang không có khung chung (Đăng nhập, Bật hai bước bắt buộc):
// logo, hai nút đoạn (ngôn ngữ, sáng hoặc tối), rồi nội dung giữa trang trên nền --paper.

import type { ReactNode } from "react";
import { Logo, Wordmark } from "../../components/Logo";
import { SegmentButton } from "../../components/SegmentButton";
import { useLang, useStrings, type Lang } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { useTheme, type Theme } from "../../theme";

export function BareFrame({ children, wide = false }: { children: ReactNode; wide?: boolean }) {
  const k = useStrings(khung);
  const { lang, setLang } = useLang();
  const { theme, setTheme } = useTheme();
  return (
    <main className={`mx-auto box-border flex min-h-screen w-full ${wide ? "max-w-[600px]" : "max-w-[460px]"} flex-col justify-center gap-[20px] px-[16px] py-[32px]`}>
      <div className="flex flex-wrap items-center justify-between gap-x-[16px] gap-y-[12px]">
        <div className="flex items-center gap-[10px]">
          <Logo status={null} />
          <Wordmark />
        </div>
        <div className="flex flex-wrap gap-[10px]">
          <SegmentButton<Lang>
            label={k.langGroup}
            value={lang}
            onChange={setLang}
            options={[
              { value: "vi", label: "VI" },
              { value: "en", label: "EN" },
            ]}
          />
          <SegmentButton<Theme>
            label={k.themeGroup}
            value={theme}
            onChange={setTheme}
            options={[
              { value: "light", label: k.light },
              { value: "dark", label: k.dark },
            ]}
          />
        </div>
      </div>
      {children}
    </main>
  );
}
