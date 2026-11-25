// Kiểm ngay lúc gõ (GET /api/v1/site-check): domain hợp lệ và chưa ai dùng,
// email hợp lệ. Chờ người dùng ngừng gõ 400 ms mới hỏi máy chủ. Kết quả chỉ
// dùng khi nó đúng là của giá trị đang có trong ô (khỏi hiện kết quả cũ).

import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { get } from "./client";
import type { Text } from "./types";

export type FieldCheck = { value: string; ok: boolean; code?: string; message?: Text };
export type SiteCheck = { domain?: FieldCheck; email?: FieldCheck };

/** Trạng thái hiện dưới ô: chưa gõ, đang kiểm, đạt, không đạt. */
export type LiveState = { status: "idle" } | { status: "checking" } | { status: "ok" } | { status: "fail"; message?: Text };

function useDebounced(value: string, ms: number): string {
  const [v, setV] = useState(value);
  useEffect(() => {
    const id = window.setTimeout(() => setV(value), ms);
    return () => window.clearTimeout(id);
  }, [value, ms]);
  return v;
}

export function useSiteCheck(domainRaw: string, emailRaw: string): { domain: LiveState; email: LiveState } {
  const domain = domainRaw.trim();
  const email = emailRaw.trim();
  const d = useDebounced(domain, 400);
  const e = useDebounced(email, 400);
  const q = useQuery({
    queryKey: ["site-check", d, e],
    queryFn: () => {
      const p = new URLSearchParams();
      if (d) p.set("domain", d);
      if (e) p.set("email", e);
      return get<SiteCheck>(`/api/v1/site-check?${p.toString()}`);
    },
    enabled: d !== "" || e !== "",
    staleTime: 5_000,
    retry: false,
  });
  const state = (now: string, sent: string, r: FieldCheck | undefined): LiveState => {
    if (now === "") return { status: "idle" };
    if (now !== sent || q.isFetching || !r) return q.isError && now === sent ? { status: "idle" } : { status: "checking" };
    return r.ok ? { status: "ok" } : { status: "fail", message: r.message };
  };
  return { domain: state(domain, d, q.data?.domain), email: state(email, e, q.data?.email) };
}
