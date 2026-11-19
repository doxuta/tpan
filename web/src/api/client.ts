// Gọi API /api/v1 của tpan (bảng đường dẫn ở internal/server/types.go).
//
//   const sites = await get<Site[]>("/api/v1/sites");
//   const plan  = await post<Preview>("/api/v1/sites?dry_run=1", body);
//   await del<JobStarted>(`/api/v1/sites/${encodeURIComponent(domain)}`);
//
// Lỗi luôn là ApiError: status là mã HTTP (0 khi không tới được máy chủ), body
// là ErrorBody nếu máy chủ trả đúng dạng. Hiện cho người dùng bằng
// errorMessage(err, lang). Gặp 401 (phiên hết hạn) thì chuyển về trang đăng
// nhập, kèm ?next= để đăng nhập xong quay lại đúng chỗ.

import type { ErrorBody, EventKind, JobEvent, LogEnd, LogFile, LogLine } from "./types";
import { tx, type Lang } from "../i18n";
import { strings as khung } from "../i18n/khung";

export class ApiError extends Error {
  readonly status: number;
  readonly body: ErrorBody | null;
  /** Giây phải chờ theo header Retry-After (423 login_locked); null nếu không có. */
  readonly retryAfter: number | null;

  constructor(status: number, body: ErrorBody | null, message: string, retryAfter: number | null = null) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.body = body;
    this.retryAfter = retryAfter;
  }

  /** Mã lỗi máy đọc được (ErrorBody.error.code), rỗng nếu máy chủ không trả. */
  get code(): string {
    return this.body?.error.code ?? "";
  }
}

export const loginPath = "/dang-nhap";

/** Đường dẫn trang đăng nhập kèm chỗ cần quay lại. */
export function loginHref(next: string): string {
  return next && next !== "/" ? `${loginPath}?next=${encodeURIComponent(next)}` : loginPath;
}

/**
 * Lọc tham số ?next= trước khi chuyển tới: chỉ nhận đường dẫn trong panel
 * ("/sites"), không nhận "//example.com" hay "https://..." để trang đăng nhập
 * không thành chỗ chuyển hướng ra ngoài.
 */
export function safeNext(next: string | null): string {
  if (!next || !next.startsWith("/") || next.startsWith("//") || next.startsWith("/\\")) return "/";
  return next;
}

function isErrorBody(v: unknown): v is ErrorBody {
  if (typeof v !== "object" || v === null) return false;
  const e = (v as { error?: unknown }).error;
  return typeof e === "object" && e !== null && typeof (e as { code?: unknown }).code === "string";
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      credentials: "same-origin",
      headers: body === undefined ? { Accept: "application/json" } : { Accept: "application/json", "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch (err) {
    throw new ApiError(0, null, `${method} ${path}: ${String(err)}`);
  }

  if (res.ok) {
    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  }

  let parsed: unknown = null;
  try {
    parsed = await res.json();
  } catch {
    // Thân không phải JSON (ví dụ proxy trả trang lỗi): giữ body = null.
  }
  const errBody = isErrorBody(parsed) ? parsed : null;

  const code = errBody?.error.code ?? "";
  // 401 "wrong_password" (sai mật khẩu hiện tại khi đổi mật khẩu) không phải
  // hết phiên: để màn đó tự báo, không đá người dùng ra khỏi form.
  if (res.status === 401 && code !== "wrong_password" && window.location.pathname !== loginPath) {
    // Phiên hết hạn hoặc bị huỷ. Tải lại hẳn trang đăng nhập để bỏ mọi dữ
    // liệu đang giữ trong bộ nhớ của phiên cũ.
    window.location.assign(loginHref(window.location.pathname + window.location.search));
  }
  const secs = Number.parseInt(res.headers.get("Retry-After") ?? "", 10);
  throw new ApiError(res.status, errBody, `${method} ${path}: HTTP ${res.status} ${code}`.trim(), Number.isFinite(secs) && secs > 0 ? secs : null);
}

export function get<T>(path: string): Promise<T> {
  return request<T>("GET", path);
}

export function post<T>(path: string, body?: unknown): Promise<T> {
  return request<T>("POST", path, body);
}

export function del<T>(path: string): Promise<T> {
  return request<T>("DELETE", path);
}

/** Câu lỗi để hiện: lấy từ máy chủ nếu có, không thì câu chung của khung trang. */
export function errorMessage(err: unknown, lang: Lang): string {
  const t = khung[lang];
  if (err instanceof ApiError) {
    if (err.body) return tx(err.body.error.message, lang) || t.errorHttp(err.status);
    return err.status === 0 ? t.errorNetwork : t.errorHttp(err.status);
  }
  return t.errorNetwork;
}

// Đủ mọi EventKind: thiếu hay thừa một khoá là lỗi biên dịch, nên thêm loại
// sự kiện bên types.ts mà quên ở đây thì tsc báo.
const eventKinds: Record<EventKind, true> = {
  "job.start": true,
  "step.start": true,
  "step.skip": true,
  command: true,
  output: true,
  "step.done": true,
  "step.fail": true,
  "undo.start": true,
  "undo.done": true,
  "undo.fail": true,
  check: true,
  "job.done": true,
  "unit.done": true,
};

/**
 * Nghe tiến độ một job qua SSE (/api/v1/jobs/{id}/events). Máy chủ gửi mỗi sự
 * kiện với "event: <kind>", nên phải nghe theo từng tên chứ không dùng
 * onmessage. Rớt mạng thì EventSource tự nối lại, gửi kèm Last-Event-ID nên
 * không mất hay lặp sự kiện. Gặp job.done thì đóng luồng; nếu không, máy chủ
 * đóng và EventSource sẽ nối lại mãi.
 *
 * onClose("done") khi job xong, onClose("error") khi luồng hỏng hẳn (401,
 * 404, máy chủ từ chối). after: chỉ nhận sự kiện có seq lớn hơn (đã có phần
 * trước từ GET /api/v1/jobs/{id}). Trả về hàm huỷ để gọi khi rời màn.
 */
export function subscribeJobEvents(
  id: string,
  onEvent: (e: JobEvent) => void,
  onClose?: (reason: "done" | "error") => void,
  after = 0,
): () => void {
  const q = after > 0 ? `?after=${after}` : "";
  const es = new EventSource(`/api/v1/jobs/${encodeURIComponent(id)}/events${q}`); // cùng origin: cookie phiên tự đi kèm
  const handle = (msg: Event) => {
    let ev: JobEvent;
    try {
      ev = JSON.parse((msg as MessageEvent<string>).data) as JobEvent;
    } catch {
      return; // dòng hỏng thì bỏ, sự kiện sau vẫn tới
    }
    onEvent(ev);
    if (ev.kind === "job.done") {
      es.close();
      onClose?.("done");
    }
  };
  for (const kind of Object.keys(eventKinds)) es.addEventListener(kind, handle);
  es.onerror = () => {
    // CONNECTING: đang tự nối lại, cứ để. CLOSED: trình duyệt bỏ cuộc.
    if (es.readyState === EventSource.CLOSED) onClose?.("error");
  };
  return () => es.close();
}

/** Hàm nhận sự kiện của luồng log trực tiếp. */
export type SiteLogHandlers = {
  /** Luồng (nối lại) đã mở. Server gửi lại 100 dòng cuối mỗi lần mở, nên bên nhận bỏ dòng cũ trước khi nhận tiếp. */
  onOpen: () => void;
  onLine: (line: string) => void;
  /** Sự kiện "end": tail đã thoát. Luồng đã đóng và không tự nối lại; error rỗng nếu thoát không lỗi. */
  onEnd: (error: string) => void;
  /** Rớt luồng. closed false: trình duyệt đang tự nối lại. closed true: trình duyệt bỏ cuộc (401, 404, 400). */
  onError: (closed: boolean) => void;
};

/**
 * Nghe log trực tiếp của một site (/api/v1/sites/{domain}/log?file=...). Cùng
 * origin nên cookie phiên tự đi kèm. Mỗi dòng log tới trong sự kiện "line";
 * nó là chữ không tin cậy, nơi hiện phải coi như chữ thường. Server tự đóng
 * luồng sau 10 phút và EventSource tự nối lại; riêng sự kiện "end" thì ta đóng
 * tay, nếu không EventSource nối lại mãi một tail đã chết. Trả về hàm huỷ.
 */
export function subscribeSiteLog(domain: string, file: LogFile, h: SiteLogHandlers): () => void {
  const es = new EventSource(`/api/v1/sites/${encodeURIComponent(domain)}/log?file=${file}`);
  es.onopen = h.onOpen;
  es.addEventListener("line", (msg) => {
    try {
      const d = JSON.parse((msg as MessageEvent<string>).data) as LogLine;
      if (typeof d.line === "string") h.onLine(d.line);
    } catch {
      // dòng hỏng thì bỏ, dòng sau vẫn tới
    }
  });
  es.addEventListener("end", (msg) => {
    let reason = "";
    try {
      reason = (JSON.parse((msg as MessageEvent<string>).data) as LogEnd).error ?? "";
    } catch {
      // không đọc được lý do thì coi như rỗng
    }
    es.close();
    h.onEnd(reason);
  });
  es.onerror = () => h.onError(es.readyState === EventSource.CLOSED);
  return () => es.close();
}
