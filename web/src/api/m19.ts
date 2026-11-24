// Gọi API của M19 (mục 3.7 của docs/thay-doi/M19-thiet-ke.md): tên phụ và luật
// chuyển hướng của một site. Kiểu nằm ở m19-types.ts. Mọi thao tác ghi đi qua
// usePreviewedOp (components/PreviewedOp.tsx): xem trước, xác nhận, thẻ theo dõi job.
//
//   const names = useSiteNames(domain);          // GET .../names
//   op.ask({ id, method: "POST", path: siteNamesPath(domain), body: aliasAddBody(name, mode), ... })

import { useQuery } from "@tanstack/react-query";
import { ApiError, get } from "./client";
import type { AliasAddBody, AliasModeBody, NameMode, NameView, RedirectAddBody, RedirectCode, RedirectField, RedirectView } from "./m19-types";

const enc = encodeURIComponent;

export const siteNamesPath = (domain: string) => `/api/v1/sites/${enc(domain)}/names`;
export const siteAliasPath = (domain: string, name: string) => `${siteNamesPath(domain)}/${enc(name)}`;
export const siteAliasModePath = (domain: string, name: string) => `${siteAliasPath(domain, name)}/mode`;
export const siteRedirectsPath = (domain: string) => `/api/v1/sites/${enc(domain)}/redirects`;
export const siteRedirectPath = (domain: string, id: number) => `${siteRedirectsPath(domain)}/${id}`;

export const namesKey = (domain: string) => ["site", domain, "names"] as const;
export const redirectsKey = (domain: string) => ["site", domain, "redirects"] as const;

/** Một dòng của GET /api/v1/domains (internal/server/types_m19.go → DomainRow). Tên phụ active, không tra DNS. */
export type DomainRow = { site: string; names: string[]; redirects: number };

export const domainsKey = ["domains"] as const;

export function useDomains() {
  return useQuery({ queryKey: domainsKey, queryFn: () => get<DomainRow[]>("/api/v1/domains") });
}

export function useSiteNames(domain: string) {
  return useQuery({ queryKey: namesKey(domain), queryFn: () => get<NameView[]>(siteNamesPath(domain)) });
}

export function useSiteRedirects(domain: string) {
  return useQuery({ queryKey: redirectsKey(domain), queryFn: () => get<RedirectView[]>(siteRedirectsPath(domain)) });
}

export const aliasAddBody = (name: string, mode: NameMode): AliasAddBody => ({ name: name.trim(), mode });
export const aliasModeBody = (mode: NameMode): AliasModeBody => ({ mode });
export const redirectAddBody = (source: string, target: string, code: RedirectCode, keepQuery: boolean): RedirectAddBody => ({
  source: source.trim(),
  target: target.trim(),
  code,
  keep_query: keepQuery,
});

const fields: readonly string[] = ["source", "target", "code"] satisfies RedirectField[];

/**
 * Trường hỏng của lỗi luật chuyển hướng (400 kèm "field": redirect_invalid, redirect_reserved).
 * Lỗi khác (redirect_loop, redirect_exists, ...) không có field: người gọi hiện cạnh nút.
 * ErrorBody trong types.ts chưa khai trường này, nên đọc ở đây thay vì sửa file dùng chung.
 */
export function redirectErrorField(err: unknown): RedirectField | null {
  if (!(err instanceof ApiError)) return null;
  const f = (err.body?.error as { field?: unknown } | undefined)?.field;
  return typeof f === "string" && fields.includes(f) ? (f as RedirectField) : null;
}
