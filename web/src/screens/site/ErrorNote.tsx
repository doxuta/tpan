// Lỗi của một lần gọi API, hiện ngay cạnh chỗ gây ra nó: dấu lỗi, câu của máy
// chủ theo ngôn ngữ đang chọn, và các điều kiện chưa đạt (ErrorBody.checks) nếu có.
// Màu đỏ chỉ nằm ở dấu lỗi; chữ vẫn là màu thường (mục 9 của GIAO-DIEN.md).

import { ApiError, errorMessage } from "../../api/client";
import { Notice } from "../../components/Notice";
import { useLang } from "../../i18n";
import { CheckList } from "./CheckItem";

type Props = {
  error: unknown;
  /** Một dòng đậm nói chuyện gì xảy ra, ví dụ "Chưa dựng được bản xem trước". */
  heading?: string;
  /** Tên bảng điều kiện chưa đạt. */
  checksHeading?: string;
  /** Không in câu lỗi vì nó đã hiện cạnh ô nhập; vẫn in bảng điều kiện nếu có. */
  omitMessage?: boolean;
};

export function ErrorNote({ error, heading, checksHeading, omitMessage = false }: Props) {
  const { lang } = useLang();
  const checks = error instanceof ApiError ? (error.body?.error.checks ?? []) : [];
  if (omitMessage && checks.length === 0) return null;
  const hasText = !!heading || !omitMessage;
  return (
    <div role="alert">
      {hasText && (
        <Notice kind="fail">
          {heading && <p className="m-0 font-semibold">{heading}</p>}
          {/* pre-line: lời từ chối nhiều dòng (htaccess_blocked liệt kê từng "file:dòng: chỉ thị") giữ nguyên chỗ xuống dòng. */}
          {!omitMessage && <p className="m-0 whitespace-pre-line [overflow-wrap:anywhere]">{errorMessage(error, lang)}</p>}
        </Notice>
      )}
      {/* Bảng điều kiện nằm ngoài Notice: ô chữ của Notice co theo nội dung, còn dòng phép kiểm cần cả bề ngang. pl-[30px] = ô dấu 20 + khoảng 10, thẳng cột với chữ. */}
      {checks.length > 0 && (
        <div className={`mt-[10px] ${hasText ? "pl-[30px]" : ""}`}>
          {checksHeading && <div className="mb-[2px] text-[12px] font-medium text-muted">{checksHeading}</div>}
          <CheckList checks={checks} />
        </div>
      )}
    </div>
  );
}
