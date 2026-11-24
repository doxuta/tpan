// Kiểu của API M19 (mục 3.7 của docs/thay-doi/M19-thiet-ke.md): tên phụ và luật
// chuyển hướng của một site. Cùng nghĩa với internal/core/types_m19.go (NameView)
// và internal/server/types_m19.go (AliasAdd, AliasMode, RedirectAdd, RedirectView);
// đổi một bên thì đổi cả bên kia. Tách khỏi types.ts để các mốc chạy song song
// không sửa chung một file.
//
//   GET    /api/v1/sites/{domain}/names                      → NameView[]
//   POST   /api/v1/sites/{domain}/names[?dry_run=1]          AliasAddBody → Preview | JobStarted
//   POST   /api/v1/sites/{domain}/names/{name}/mode[?dry_run=1]  AliasModeBody → Preview | JobStarted
//   DELETE /api/v1/sites/{domain}/names/{name}[?dry_run=1]   → Preview | JobStarted
//   GET    /api/v1/sites/{domain}/redirects                  → RedirectView[]
//   POST   /api/v1/sites/{domain}/redirects[?dry_run=1]      RedirectAddBody → Preview | JobStarted
//   DELETE /api/v1/sites/{domain}/redirects/{id}[?dry_run=1] → Preview | JobStarted

/** Kiểu phục vụ của tên phụ: "serve" mở ra nội dung của site, "redirect" chuyển 301 về tên chính. */
export type NameMode = "serve" | "redirect";

export type NameView = {
  name: string;
  kind: "primary" | "alias";
  /** Tên chính không có kiểu (chuỗi rỗng hoặc vắng). */
  mode?: NameMode | "";
  /** "pending": đã giữ chỗ, job đang chạy. */
  state: "pending" | "active";
  /** Tra lúc gọi: ok (trỏ về máy), warn (chưa tra được), missing (không trỏ về máy). */
  dns: "ok" | "warn" | "missing";
  in_cert: boolean;
};

export type AliasAddBody = { name: string; mode: NameMode };

export type AliasModeBody = { mode: NameMode };

export type RedirectCode = 301 | 302;

export type RedirectView = {
  id: number;
  source: string;
  target: string;
  code: RedirectCode;
  keep_query: boolean;
  state: string;
};

export type RedirectAddBody = { source: string; target: string; code: RedirectCode; keep_query: boolean };

/** Tên trường mà redirect_invalid chỉ ra (ErrorBody.error.field). */
export type RedirectField = "source" | "target" | "code";
