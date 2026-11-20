// Gọi API của M14 (mục 6.13 của KE-HOACH.md): cô lập giữa các site và đường đi
// thật của một request. Đường dẫn theo internal/server/types_m14.go; kiểu nằm
// ở m14-types.ts. Tách khỏi queries.ts để các mốc chạy song song không sửa
// chung một file.
//
//   const iso = useIsolation();                 // GET /security/isolation (chỉ admin)
//   const trace = useRouteTrace(domain);        // trace.mutate() khi bấm nút
//   const flow = usePlanFlow();                 // của m13-users.ts
//   flow.ask(hardenRequest);                    // "Áp bản vá cô lập"
//   flow.ask(htaccessRequest(domain, "tat"));   // đổi chế độ .htaccess

import { useMutation, useQuery } from "@tanstack/react-query";
import { get } from "./client";
import type { PlanRequest } from "./m13-users";
import type { HardenPreview, HtaccessBody, HtaccessMode, Isolation, RouteTrace } from "./m14-types";
import type { Preview } from "./types";

const enc = encodeURIComponent;

export const isolationPath = "/api/v1/security/isolation";
export const hardenPath = "/api/v1/security/harden";
export const siteRoutePath = (domain: string) => `/api/v1/sites/${enc(domain)}/route`;
export const siteHtaccessPath = (domain: string) => `/api/v1/sites/${enc(domain)}/htaccess`;

// Nằm dưới khoá ["security"] của màn Bảo mật: việc nào làm mới khoá đó (bật
// tường lửa, cài fail2ban) cũng đọc lại khối cô lập.
export const isolationKey = ["security", "isolation"] as const;

/** Khối "Cô lập giữa các site": kết quả health đã lưu, máy chủ không chạy lại phép kiểm. */
export function useIsolation(enabled = true) {
  return useQuery({ queryKey: isolationKey, queryFn: () => get<Isolation>(isolationPath), enabled });
}

/**
 * Đường đi thật của một request tới site. Là mutation chứ không phải query vì
 * máy chủ chạy lệnh thật mỗi lần gọi (nginx -T, curl) và không đệm: chỉ gọi
 * khi người dùng bấm nút, và bấm lại là chạy lại.
 */
export function useRouteTrace(domain: string) {
  return useMutation({ mutationFn: () => get<RouteTrace>(siteRoutePath(domain)) });
}

/** Plan "Áp bản vá cô lập" cho usePlanFlow. */
export const hardenRequest: PlanRequest = { method: "POST", path: hardenPath };

/** Plan đổi chế độ .htaccess của một site cho usePlanFlow. */
export function htaccessRequest(domain: string, mode: HtaccessMode): PlanRequest {
  const body: HtaccessBody = { mode };
  return { method: "POST", path: siteHtaccessPath(domain), body };
}

/**
 * Bản xem trước của hardenRequest. usePlanFlow chỉ biết kiểu Preview chung,
 * nhưng POST /security/harden?dry_run=1 trả thêm switched và kept trong cùng
 * đối tượng. Hai trường đó có thể là null (Go) hoặc vắng (server cũ), nên nơi
 * dùng luôn đọc với `?? []`.
 */
export function asHardenPreview(p: Preview): Preview & Partial<HardenPreview> {
  return p;
}
