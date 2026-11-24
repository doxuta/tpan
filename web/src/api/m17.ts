// Gọi API của M17 (mục 6.15 của KE-HOACH.md): lồng theo site, số liệu đọc từ
// cgroup, gợi ý mức tối đa, thử tấn công. Đường dẫn theo
// internal/server/types_m17.go; kiểu ở m17-types.ts. Mọi việc đổi máy đi qua
// usePlanFlow (m13-users.ts): ask(request) → bản xem trước → chạy.
//
//   const cages = useCages(who.isAdmin);              // GET /cage (admin)
//   const usage = useUsage();                         // GET /usage (user thấy site của mình)
//   const one = useSiteUsage(domain, "day", caged);   // GET /sites/{d}/usage
//   const sug = useSuggestions(who.isAdmin);          // GET /usage/suggestions (admin)
//   const probe = useProbe(who.isAdmin);              // GET /security/probe (admin)
//   flow.ask(cageRequest(domain, true));              // vào lồng
//   flow.ask(probeRequest(""));                       // thử tấn công cả hai lượt
//   flow.ask(probeCleanupRequest);                    // dọn lượt thử còn sót

import { useQuery } from "@tanstack/react-query";
import { get } from "./client";
import type { PlanRequest } from "./m13-users";
import type { CageBody, CageRow, ProbeBody, ProbeOnly, ProbeStatus, SiteUsage, SiteUsageDetail, SuggestionReport, UsageRange } from "./m17-types";

const enc = encodeURIComponent;

export const cagePath = "/api/v1/cage";
export const usagePath = "/api/v1/usage";
export const suggestionsPath = "/api/v1/usage/suggestions";
export const probePath = "/api/v1/security/probe";
export const probeCleanupPath = "/api/v1/security/probe/cleanup";
export const siteCagePath = (domain: string) => `/api/v1/sites/${enc(domain)}/cage`;
export const siteUsagePath = (domain: string, range: UsageRange) => `/api/v1/sites/${enc(domain)}/usage?range=${range}`;

// Khoá ["usage"] gom mọi số liệu: job xong (thẻ theo dõi tải lại) thì đọc lại hết.
export const cageKey = ["cage"] as const;
export const usageKey = ["usage"] as const;
export const suggestionsKey = ["usage", "suggestions"] as const;
export const siteUsageKey = (domain: string, range: UsageRange) => ["usage", "site", domain, range] as const;
// Dưới ["security"] như khối cô lập của M14: việc nào làm mới màn Bảo mật cũng đọc lại.
export const probeKey = ["security", "probe"] as const;

/** Trạng thái lồng của mọi site đang chạy (chỉ admin). Có số đọc ngay từ cgroup cho site trong lồng. */
export function useCages(enabled = true) {
  return useQuery({ queryKey: cageKey, queryFn: () => get<CageRow[]>(cagePath), enabled });
}

/** Số liệu đọc ngay của các site trong lồng; user thường chỉ thấy site của mình. Đọc lại mỗi 30 giây. */
export function useUsage(enabled = true) {
  return useQuery({ queryKey: usageKey, queryFn: () => get<SiteUsage[]>(usagePath), enabled, refetchInterval: 30_000 });
}

/** Số đọc ngay cộng lịch sử của một site trong lồng. Site ngoài lồng: lỗi usage_not_caged, nên chỉ bật khi biết site đang trong lồng. */
export function useSiteUsage(domain: string, range: UsageRange, enabled = true) {
  return useQuery({
    queryKey: siteUsageKey(domain, range),
    queryFn: () => get<SiteUsageDetail>(siteUsagePath(domain, range)),
    enabled,
  });
}

/** Gợi ý mức tối đa từ 7 ngày số liệu, theo site và theo gói (chỉ admin). */
export function useSuggestions(enabled = true) {
  return useQuery({ queryKey: suggestionsKey, queryFn: () => get<SuggestionReport>(suggestionsPath), enabled });
}

/** Lượt thử tấn công gần nhất đã xong và các lượt chưa dọn (chỉ admin). */
export function useProbe(enabled = true) {
  return useQuery({ queryKey: probeKey, queryFn: () => get<ProbeStatus>(probePath), enabled });
}

/** Plan vào lồng (enabled true) hoặc ra lồng cho usePlanFlow. */
export function cageRequest(domain: string, enabled: boolean): PlanRequest {
  const body: CageBody = { enabled };
  return { method: "POST", path: siteCagePath(domain), body };
}

/** Plan một lượt thử tấn công. Bản xem trước có thêm trường run (ProbePreview). */
export function probeRequest(only: ProbeOnly): PlanRequest {
  const body: ProbeBody = { only };
  return { method: "POST", path: probePath, body };
}

/** Plan dọn các lượt thử còn sót. Không có gì để dọn: 409 nothing_to_do. */
export const probeCleanupRequest: PlanRequest = { method: "POST", path: probeCleanupPath };
