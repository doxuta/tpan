// Đổi đường dẫn của panel thành tên mục đọc được (cho ngữ cảnh "Đang ở" và
// đường vừa đi), và đổi liên kết AI viết trong câu trả lời (tpan:site/<domain>)
// thành đường dẫn thật. Cả hai chỉ dựa vào danh sách màn ở routes.tsx, nên
// thêm màn mới ở đó là đủ.

import { matchPath } from "react-router";
import type { Lang } from "../i18n";
import { strings } from "../i18n/ai";
import { strings as khung } from "../i18n/khung";
import { screens } from "../routes";

/** Tham số của đường dẫn khớp mẫu (đã giải mã %xx), null nếu không khớp. */
function params(pattern: string, path: string): Record<string, string | undefined> | null {
  return matchPath({ path: pattern, end: true }, path)?.params ?? null;
}

/** Tên mục theo ngôn ngữ đang dùng. Đường dẫn lạ thì trả chính đường dẫn. */
export function viewLabel(path: string, lang: Lang): string {
  const t = strings[lang];
  if (path === "/sites/new") return t.viewCreateSite;
  if (path === "/plans") return t.viewPlans;

  // Mục có tham số: mẫu nào khớp thì dùng, id lấy từ đường dẫn.
  const site = params("/sites/:domain", path);
  if (site?.domain) return t.viewSite(site.domain);
  const incident = params("/incidents/:id", path);
  if (incident?.id) return t.viewIncident(incident.id);
  const job = params("/jobs/:id", path);
  if (job?.id) return t.viewJob(job.id);
  const plan = params("/plans/:id", path);
  if (plan?.id) return t.viewPlan(plan.id);
  const session = params("/ai/:id", path);
  if (session?.id) return t.viewAiSession(session.id);

  // Mục menu: lấy tên đúng như menu đang hiện. /dashboard (Bảng điều khiển của
  // user thường, M16) là userPath của mục "/" chứ không phải một dòng có menu.
  const s = screens.find((r) => r.menu && (params(r.path, path) !== null || r.menu.userPath === path));
  if (s?.menu) return khung[lang].menu[s.menu.label];
  return path;
}

export type AILink = { path: string; label: string };

/** Một liên kết tpan:<loại>/<đích>[/log] đã tách; chưa kiểm là route có thật. */
const linkPattern = /tpan:(site|incident|job|plan)\/([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)(\/log)?/g;

/** Có route thật trong routes.tsx khớp đường dẫn này không. */
function routeExists(path: string): boolean {
  return screens.some((r) => !r.bare && r.load !== undefined && params(r.path, path) !== null);
}

/**
 * Đổi một liên kết AI thành nút: null nếu loại lạ hoặc đích không trỏ tới
 * route có thật (khi đó bên gọi để nguyên chữ thường). AI không tự chuyển màn
 * của người dùng; nút chỉ có tác dụng khi người bấm.
 */
export function resolveLink(kind: string, id: string, log: boolean, lang: Lang): AILink | null {
  const t = strings[lang];
  let path: string;
  let label: string;
  switch (kind) {
    case "site":
      path = `/sites/${encodeURIComponent(id)}`;
      label = log ? t.linkSiteLog(id) : t.linkSite(id);
      break;
    case "incident":
      path = `/incidents/${encodeURIComponent(id)}`;
      label = t.linkIncident(id);
      break;
    case "job":
      path = `/jobs/${encodeURIComponent(id)}`;
      label = t.linkJob(id);
      break;
    case "plan":
      path = `/plans/${encodeURIComponent(id)}`;
      label = t.linkPlan(id);
      break;
    default:
      return null;
  }
  // "/sites/new" cũng khớp mẫu "/sites/:domain": tpan:site/new không phải một site.
  if (path === "/sites/new" || !routeExists(path)) return null;
  return { path, label };
}

/** Mọi liên kết hợp lệ trong câu trả lời, mỗi đích một lần, theo thứ tự xuất hiện. */
export function extractLinks(text: string, lang: Lang): AILink[] {
  const seen = new Set<string>();
  const out: AILink[] = [];
  for (const m of text.matchAll(linkPattern)) {
    const link = resolveLink(m[1]!, m[2]!, m[3] !== undefined, lang);
    if (link && !seen.has(link.path)) {
      seen.add(link.path);
      out.push(link);
    }
  }
  return out;
}

export { linkPattern };
