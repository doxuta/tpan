// Phần không dính giao diện của màn SSL: khoá truy vấn, đường dẫn API và cách
// đọc số ngày còn lại từ câu tóm tắt của phép kiểm chứng chỉ.

import type { CheckResult } from "../../api/types";

export const sslKey = ["ssl"] as const;

export const sslIssuePath = (domain: string) => `/api/v1/sites/${encodeURIComponent(domain)}/ssl`;

// Phép kiểm chứng chỉ (checks.TLSCert) không có trường riêng cho số ngày, chỉ có
// trong Summary: "... còn 89 ngày, ..." (đạt), "... chỉ còn 9 ngày" (cảnh báo);
// bản tiếng Anh "..., 89 days left, ..." và "... has only 9 days left". Hết hạn
// thì câu nói ngày hết hạn, không có số ngày còn lại. Nếu máy chủ đổi câu thì
// chỉ cột "Còn lại" hiện "—"; dấu trạng thái và câu tóm tắt vẫn đúng.
const daysLeft = { vi: /còn (\d+) ngày/, en: /(\d+) days left/ };

/** Số ngày còn lại của chứng chỉ; null khi câu tóm tắt không nói (lỗi, chưa kiểm được). */
export function certDays(check: CheckResult | undefined): number | null {
  if (!check) return null;
  const m = daysLeft.vi.exec(check.summary.vi) ?? daysLeft.en.exec(check.summary.en);
  return m ? Number(m[1]) : null;
}
