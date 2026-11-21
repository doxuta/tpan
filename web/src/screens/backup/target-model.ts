// Phần không dính giao diện của thẻ "Đích ngoài máy" (mục 3.3 của
// docs/thay-doi/M18-thiet-ke.md): dựng thân POST /api/v1/backups/target từ ô
// nhập, kiểm vài lỗi gõ mà máy chủ không nói rõ được, và cách hiện khoá cuối.
// Chỉ `import type` để `node --test` chạy thẳng file này (make test-web).
//
// Luật thật (regex của endpoint, bucket, tiền tố; khoá phải gõ lại khi đổi đích)
// nằm ở máy chủ: giao diện không chép lại, chỉ hiện đúng câu từ chối của nó.

import type { TargetRequest, TargetView } from "../../api/m18-types";

/** Khoá truy vấn GET target. Dưới backupsKey, nên màn invalidate ["backups"] là tải lại cả đích. */
export const targetKey = ["backups", "target"] as const;

/** Trần số bản giữ trên kho: khớp s3.CheckKeep (1..365). */
export const maxRemoteKeep = 365;

/** keep: giữ như cũ (""), dán PEM mới, hay bỏ CA ("none"). */
export type CaMode = "keep" | "custom" | "none";

/**
 * Ô nhập của form. accessKey, secretKey chỉ sống ở đây (state của form) và
 * trong một ref của thẻ giữa lúc xem trước và lúc chạy; không vào queryKey,
 * localStorage hay địa chỉ trang.
 */
export type TargetForm = {
  endpoint: string;
  region: string;
  bucket: string;
  prefix: string;
  keep: string;
  /** Ô "Dùng kiểu bucket.host"; tắt là kiểu đường dẫn (path_style: true). */
  vhost: boolean;
  ca: CaMode;
  caPem: string;
  accessKey: string;
  secretKey: string;
};

/** Ô nhập ban đầu: giá trị đích đang đặt (nếu có), hai ô khoá luôn trống. */
export function formFromView(v: TargetView): TargetForm {
  return {
    endpoint: v.endpoint ?? "",
    region: v.region ?? "",
    bucket: v.bucket ?? "",
    prefix: v.prefix ?? "",
    keep: v.keep ? String(v.keep) : "",
    vhost: v.configured ? !v.path_style : false,
    // Chưa đặt đích thì "giữ như cũ" không có nghĩa: mặc định không dùng CA riêng.
    ca: v.configured ? "keep" : "none",
    caPem: "",
    accessKey: "",
    secretKey: "",
  };
}

/** Số bản giữ: "" là 0 (máy chủ dùng mặc định 7); số nguyên 1..365; còn lại null. */
export function parseRemoteKeep(raw: string): number | null {
  const s = raw.trim();
  if (s === "") return 0;
  if (!/^\d{1,3}$/.test(s)) return null;
  const n = Number(s);
  return n >= 1 && n <= maxRemoteKeep ? n : null;
}

/** Lỗi phát hiện được ngay ở trình duyệt; mỗi ô một mã để màn đổi ra chữ. */
export type FormProblem = "endpoint" | "bucket" | "keep" | "keys" | "keysRequired" | "caPem";

export function checkForm(f: TargetForm, configured: boolean): FormProblem[] {
  const out: FormProblem[] = [];
  if (f.endpoint.trim() === "") out.push("endpoint");
  if (f.bucket.trim() === "") out.push("bucket");
  if (parseRemoteKeep(f.keep) === null) out.push("keep");
  const a = f.accessKey !== "";
  const s = f.secretKey !== "";
  // Một ô có, một ô trống: máy chủ coi là khoá sai; nói rõ ngay ở đây.
  if (a !== s) out.push("keys");
  // Chưa có khoá nào đang lưu thì không có gì để giữ.
  else if (!configured && !a) out.push("keysRequired");
  // "Dùng chứng chỉ gốc riêng" mà ô trống sẽ thành ca_pem "" (giữ như cũ): sai ý người dùng.
  if (f.ca === "custom" && f.caPem.trim() === "") out.push("caPem");
  return out;
}

/** Thân request. Khoá không cắt khoảng trắng: máy chủ tự từ chối, giao diện không đoán sửa bí mật. */
export function buildRequest(f: TargetForm): TargetRequest {
  return {
    endpoint: f.endpoint.trim(),
    region: f.region.trim(),
    bucket: f.bucket.trim(),
    prefix: f.prefix.trim(),
    path_style: !f.vhost,
    keep: parseRemoteKeep(f.keep) ?? 0,
    ca_pem: f.ca === "keep" ? "" : f.ca === "none" ? "none" : f.caPem,
    access_key: f.accessKey,
    secret_key: f.secretKey,
  };
}

/** Khoá truy cập để hiện: "…ABCD", hoặc số ký tự khi khoá ngắn (máy chủ không cho tail). */
export function keyHint(v: TargetView): { tail: string } | { len: number } {
  return v.access_key_tail ? { tail: `…${v.access_key_tail}` } : { len: v.access_key_len ?? 0 };
}
