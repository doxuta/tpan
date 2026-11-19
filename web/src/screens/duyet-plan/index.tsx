// Bản xem trước của một Plan, trình bày như một pull request (mục 7 của
// docs/GIAO-DIEN.md). Không có route riêng: màn Tạo site và thao tác Xoá site
// nhúng component này với Preview lấy từ ?dry_run=1.
//
// Khung của đợt 1: chỉ có tiêu đề và dòng tóm tắt. Thợ của màn này thay hẳn
// nội dung, giữ tên export PlanReview.

import type { Preview } from "../../api/types";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/duyet-plan";

// Chữ ký đã chốt cho đợt 2 (màn Tạo site và Site dùng song song với màn này):
export type PlanReviewProps = {
  preview: Preview;
  /** Gửi lệnh chạy thật (POST/DELETE không có dry_run). Màn gọi lo chuyển sang /jobs/:id. */
  onRun: () => void;
  /** Đang gửi lệnh chạy: nút chạy bị khoá. */
  running?: boolean;
  /** Lỗi của lần gửi lệnh chạy (ApiError hoặc lỗi khác), hiện ngay dưới nút. */
  error?: unknown;
  /** Quay lại bước nhập. Không có thì không hiện nút. */
  onCancel?: () => void;
};

export function PlanReview({ preview }: PlanReviewProps) {
  const t = useStrings(strings);
  const { lang } = useLang();
  return (
    <section className="pt-[26px]">
      <h2 className="m-0 text-[16px] font-semibold">
        {t.heading}: {tx(preview.title, lang)}
      </h2>
      <p className="mt-[6px] mb-0 text-muted">{t.summary}</p>
    </section>
  );
}
