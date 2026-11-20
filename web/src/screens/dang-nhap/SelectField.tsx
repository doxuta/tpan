// Ô chọn một trong nhiều giá trị, cùng số đo với Field (mục 6 của
// docs/GIAO-DIEN.md): cao 40 px (44 px ở màn hẹp), viền --ink, nền --surface,
// góc vuông. Dùng <select> của trình duyệt để có bàn phím và trình đọc màn hình
// sẵn. Nằm cạnh Field vì cùng một họ; dùng ở màn Người dùng, Gói, Lịch sử.

import type { ReactNode, SelectHTMLAttributes } from "react";

type Props = Omit<SelectHTMLAttributes<HTMLSelectElement>, "id" | "className"> & {
  id: string;
  label: string;
  hint?: string;
  children: ReactNode;
};

export function SelectField({ id, label, hint, children, ...rest }: Props) {
  const hintId = `${id}-goi-y`;
  return (
    <div>
      <label htmlFor={id} className="mb-[6px] block text-[14px] font-medium">
        {label}
      </label>
      <select
        id={id}
        aria-describedby={hint ? hintId : undefined}
        className="box-border block h-[44px] w-full border border-ink bg-surface px-[8px] text-[14px] text-ink sm:h-[40px]"
        {...rest}
      >
        {children}
      </select>
      {hint && (
        <p id={hintId} className="mt-[6px] mb-0 text-[13px] text-muted">
          {hint}
        </p>
      )}
    </div>
  );
}
