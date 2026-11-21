// Logic thuần của trang "Cài lên máy ảo mới" (mục 3.9 của
// docs/thay-doi/M18-thiet-ke.md): chuẩn hoá và kiểm ô nhập, dựng thân
// POST /api/v1/cloud-init/render. Chỉ `import type` để `make test-web` (node
// --test) chạy được file này không cần trình đóng gói.

import type { CloudInitRequest } from "../../api/m18-types";

/** Trần mặc định của user-data (max_bytes 0 ở máy chủ): giới hạn nhỏ nhất đã tra, AWS EC2. */
export const defaultMaxBytes = 16384;

export type Form = {
  web: "nginx" | "apache";
  private: boolean;
  base: string;
  amd64: string;
  arm64: string;
  /** Tên miền các site đã chọn, theo thứ tự hiện trên trang. */
  domains: string[];
  email: string;
};

/**
 * Máy chủ chỉ nhận 64 hex chữ thường (cloudinit.sumRe). Người ta hay dán kèm
 * khoảng trắng hoặc bản chữ hoa, nên bỏ khoảng trắng hai đầu và hạ chữ trước
 * khi kiểm và trước khi gửi: cùng một giá trị, không bắt người dùng sửa tay.
 */
export function normSum(s: string): string {
  return s.trim().toLowerCase();
}

export function isSum(s: string): boolean {
  return /^[0-9a-f]{64}$/.test(s);
}

export type SumProblem = "required" | "invalid" | null;

export function sumProblem(raw: string): SumProblem {
  const s = normSum(raw);
  if (s === "") return "required";
  return isSum(s) ? null : "invalid";
}

/** Ô nào chưa gửi được, theo thứ tự trên trang (ô đầu tiên nhận con trỏ). */
export function problems(f: Form): ("amd64" | "arm64" | "email")[] {
  const out: ("amd64" | "arm64" | "email")[] = [];
  if (sumProblem(f.amd64)) out.push("amd64");
  if (sumProblem(f.arm64)) out.push("arm64");
  if (f.domains.length > 0 && f.email.trim() === "") out.push("email");
  return out;
}

/**
 * Thân request. Không chọn site: sites null (máy mới không có site). Có site:
 * mỗi site một dòng chỉ có domain; email nằm ở defaults. Không gửi ssl vì máy
 * chủ từ chối ssl: true (DNS chưa trỏ về máy mới thì lấy chứng chỉ chắc hỏng).
 * max_bytes 0 là trần mặc định 16384.
 */
/**
 * Máy chủ từ chối base có "/" cuối (cloudinit.CheckBase); người ta hay chép
 * địa chỉ kèm "/". Bỏ ở một chỗ để thân gửi đi và lệnh curl gợi ý luôn khớp.
 */
export function normBase(s: string): string {
  return s.trim().replace(/\/+$/, "");
}

export function buildRequest(f: Form): CloudInitRequest {
  return {
    web: f.web,
    private: f.private,
    base: normBase(f.base),
    sha256_amd64: normSum(f.amd64),
    sha256_arm64: normSum(f.arm64),
    max_bytes: 0,
    sites: f.domains.length === 0 ? null : { defaults: { admin_email: f.email.trim() }, sites: f.domains.map((domain) => ({ domain })) },
  };
}

/**
 * Lệnh gợi ý lấy SHA256SUMS. Base trống thì để chữ giữ chỗ: địa chỉ mặc định
 * do máy chủ tính (cloudinit.DefaultBaseFormat), giao diện không chép lại để
 * khỏi có hai nơi phải sửa cùng lúc.
 */
export function sumsCommand(base: string): string {
  const b = normBase(base);
  return `curl -fsSL ${b === "" ? "<base>" : b}/SHA256SUMS`;
}

/** 16384 → "16 384": nhóm ba chữ số bằng dấu cách không ngắt, như trong đặc tả, ở cả hai ngôn ngữ. */
export function groupDigits(n: number): string {
  return String(Math.trunc(n)).replace(/\B(?=(\d{3})+(?!\d))/g, " ");
}
