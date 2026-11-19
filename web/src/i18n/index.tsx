// Song ngữ (mục 10 của KE-HOACH.md): chuỗi giao diện chia theo khu, mỗi khu
// một file trong web/src/i18n/, để nhiều người làm nhiều màn cùng lúc không
// sửa chung một file:
//
//   web/src/i18n/khung.ts       khung trang: thanh đầu, menu, nút chung, dấu trạng thái
//   web/src/i18n/bang-dieu-khien.ts, site.ts, lich-su.ts, dang-nhap.ts, ...
//
// Mỗi file viết bản tiếng Việt trước, rồi bản tiếng Anh cùng kiểu:
//
//   const vi = { title: "Bảng điều khiển", sites: (n: number) => `${n} site` };
//   export const strings = defineStrings(vi, { title: "Dashboard", sites: (n) => `${n} sites` });
//
// Thiếu khoá hay thừa khoá ở bản tiếng Anh là lỗi biên dịch. Trong component:
//
//   const t = useStrings(strings);   // t.title
//   const { lang } = useLang();      // tx(check.title, lang) cho i18n.Text từ API
//
// Chuỗi không bao giờ nằm thẳng trong component (skill tpan-giao-dien).

import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import type { Text } from "../api/types";

export type Lang = "vi" | "en";

const storageKey = "tpan-lang";

type LangState = { lang: Lang; setLang: (l: Lang) => void };

const LangContext = createContext<LangState>({ lang: "vi", setLang: () => {} });

function initialLang(): Lang {
  try {
    const v = localStorage.getItem(storageKey);
    if (v === "vi" || v === "en") return v;
  } catch {
    // localStorage bị chặn: dùng mặc định.
  }
  return "vi";
}

export function LangProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(initialLang);
  const setLang = useCallback((l: Lang) => {
    setLangState(l);
    document.documentElement.lang = l;
    try {
      localStorage.setItem(storageKey, l);
    } catch {
      // Không nhớ được thì thôi, lần sau về mặc định.
    }
  }, []);
  const value = useMemo(() => ({ lang, setLang }), [lang, setLang]);
  return <LangContext.Provider value={value}>{children}</LangContext.Provider>;
}

export function useLang(): LangState {
  return useContext(LangContext);
}

/** Gói hai bản của một khu. en phải đúng kiểu của vi. */
export function defineStrings<T>(vi: T, en: NoInfer<T>): { vi: T; en: T } {
  return { vi, en };
}

/** Chuỗi của một khu theo ngôn ngữ đang chọn. */
export function useStrings<T>(s: { vi: T; en: T }): T {
  return s[useLang().lang];
}

/** Chọn bản của một câu song ngữ từ API (i18n.Text). Bản tiếng Anh trống thì dùng tiếng Việt, như Text.In bên Go. */
export function tx(t: Text | undefined, lang: Lang): string {
  if (!t) return "";
  return lang === "en" && t.en ? t.en : t.vi;
}
