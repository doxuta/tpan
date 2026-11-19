// Công tắc "Gợi ý" (mục 10 của KE-HOACH.md): bật thì các màn hiện thêm câu
// "vì sao" viết sẵn trong mã (trường Why của Step). Lưu trong localStorage để
// lần sau mở lại vẫn giữ. Trong màn:
//
//   const { tips } = useTips();
//   {tips && <p className="text-muted">{tx(step.info.why, lang)}</p>}

import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";

const storageKey = "tpan-tips";

type TipsState = { tips: boolean; setTips: (on: boolean) => void };

const TipsContext = createContext<TipsState>({ tips: false, setTips: () => {} });

function initialTips(): boolean {
  try {
    return localStorage.getItem(storageKey) === "1";
  } catch {
    return false;
  }
}

export function TipsProvider({ children }: { children: ReactNode }) {
  const [tips, setState] = useState(initialTips);
  const setTips = useCallback((on: boolean) => {
    setState(on);
    try {
      localStorage.setItem(storageKey, on ? "1" : "0");
    } catch {
      // Không nhớ được thì thôi.
    }
  }, []);
  const value = useMemo(() => ({ tips, setTips }), [tips, setTips]);
  return <TipsContext.Provider value={value}>{children}</TipsContext.Provider>;
}

export function useTips(): TipsState {
  return useContext(TipsContext);
}
