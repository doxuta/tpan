// Chép chữ vào bộ nhớ tạm, dùng cho MonoText và EvidenceBlock (dữ liệu kỹ
// thuật bấm là chép, mục 10 của KE-HOACH.md). state cho biết vừa chép xong
// hay hỏng để hiện một dòng báo trong vùng aria-live.

import { useCallback, useEffect, useRef, useState } from "react";

export type CopyState = "idle" | "copied" | "failed";

export function useCopy(): { state: CopyState; copy: (text: string) => void } {
  const [state, setState] = useState<CopyState>("idle");
  const timer = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(timer.current), []);

  const copy = useCallback((text: string) => {
    // Người dùng đang bôi đen một đoạn để tự chép: không đè lên lựa chọn đó.
    if (window.getSelection()?.toString()) return;
    const done = (s: CopyState) => {
      setState(s);
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setState("idle"), 1500);
    };
    // navigator.clipboard chỉ có trong ngữ cảnh an toàn (HTTPS hoặc localhost); panel luôn chạy HTTPS.
    if (!navigator.clipboard) return done("failed");
    navigator.clipboard.writeText(text).then(
      () => done("copied"),
      () => done("failed"),
    );
  }, []);

  return { state, copy };
}
