// Ô nhập mật khẩu có nhãn (<label>) nối bằng id. Chưa có thành phần ô nhập
// dùng chung trong components/, nên viết ở đây; dùng chung cho Đăng nhập và
// Cài đặt. Số đo theo mục 6 của docs/GIAO-DIEN.md: cao 40 px (44 px ở màn
// hẹp), viền --ink, nền --surface, góc vuông.

import { forwardRef, type InputHTMLAttributes } from "react";

type Props = Omit<InputHTMLAttributes<HTMLInputElement>, "id" | "className"> & {
  id: string;
  label: string;
  /** Dòng gợi ý dưới ô, đọc kèm khi vào ô. */
  hint?: string;
  invalid?: boolean;
};

export const Field = forwardRef<HTMLInputElement, Props>(function Field({ id, label, hint, invalid = false, ...rest }, ref) {
  const hintId = `${id}-goi-y`;
  return (
    <div>
      <label htmlFor={id} className="mb-[6px] block text-[14px] font-medium">
        {label}
      </label>
      <input
        ref={ref}
        id={id}
        aria-invalid={invalid || undefined}
        aria-describedby={hint ? hintId : undefined}
        className="box-border block h-[44px] w-full border border-ink bg-surface px-[10px] text-[14px] text-ink sm:h-[40px]"
        {...rest}
      />
      {hint && (
        <p id={hintId} className="mt-[6px] mb-0 text-[13px] text-muted">
          {hint}
        </p>
      )}
    </div>
  );
});
