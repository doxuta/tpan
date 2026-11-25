// Một dòng trạng thái ngay dưới ô nhập (kiểm lúc gõ): đang kiểm, đạt (kèm chữ
// nói vì sao đạt), hoặc không đạt (câu của máy chủ). aria-live để trình đọc màn
// hình đọc kết quả mà người dùng không phải rời ô.

import type { ReactNode } from "react";
import type { LiveState } from "../api/site-check";
import { tx, useLang } from "../i18n";
import { StatusLine } from "./StatusMark";

export function LiveCheck({ id, state, okText, checkingText }: { id: string; state: LiveState; okText: ReactNode; checkingText: ReactNode }) {
  const { lang } = useLang();
  let body: ReactNode = null;
  if (state.status === "checking") body = <span className="text-muted">{checkingText}</span>;
  if (state.status === "ok") body = <StatusLine status="pass">{okText}</StatusLine>;
  if (state.status === "fail") body = <StatusLine status="fail">{state.message ? tx(state.message, lang) : ""}</StatusLine>;
  return (
    <p id={id} aria-live="polite" className="mt-[6px] mb-0 min-h-[20px] text-[13px]">
      {body}
    </p>
  );
}
