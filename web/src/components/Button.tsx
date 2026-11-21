// Nút (mục 6 của docs/GIAO-DIEN.md, bản 2). Bốn kiểu:
//   primary    nền màu nhấn: hành động chính của màn hay của một khối xem trước
//   secondary  viền --line: hành động phụ
//   text       không viền, chữ màu nhấn: việc trong hàng bảng, chỗ cần nhẹ
//   danger     như secondary, chữ đỏ: CHỈ cho việc không hoàn tác được
// Liên kết trông như nút dùng buttonClass cho <a> hay <Link>, để vẫn là liên kết thật:
//
//   <Button variant="primary" icon="plus" onClick={add}>Thêm user</Button>
//   <Link to="/sites/new" className={buttonClass("primary")}>Tạo site</Link>
//
// Rê chuột chỉ đổi màu khi nút không bị khoá (not-disabled:), nên nút khoá
// không "sáng lên" dưới con trỏ.

import type { ButtonHTMLAttributes } from "react";
import { Icon, type IconName } from "./Icon";

export type ButtonVariant = "primary" | "secondary" | "text" | "danger";
export type ButtonSize = "md" | "sm";

const base =
  "inline-flex items-center justify-center gap-[8px] whitespace-nowrap rounded-ctl border text-[14px] font-medium leading-none no-underline transition-colors " +
  "disabled:cursor-default aria-disabled:cursor-default";

const sizes: Record<ButtonSize, string> = {
  // Dưới 640 px nút cao 44 px để bấm được bằng ngón tay (mục 6).
  md: "h-[44px] px-[14px] sm:h-[40px]",
  sm: "h-[44px] px-[10px] sm:h-[32px]",
};

const variants: Record<ButtonVariant, string> = {
  primary:
    "border-accent bg-accent text-on-accent not-disabled:hover:border-accent-hover not-disabled:hover:bg-accent-hover not-disabled:hover:text-on-accent " +
    "disabled:border-hair disabled:bg-soft disabled:text-muted",
  secondary: "border-line bg-surface text-ink not-disabled:hover:bg-soft not-disabled:hover:text-ink disabled:border-hair disabled:bg-soft disabled:text-muted",
  text: "border-transparent bg-transparent text-accent not-disabled:hover:bg-accent-tint not-disabled:hover:text-accent disabled:text-muted",
  danger: "border-line bg-surface text-fail-text not-disabled:hover:bg-soft not-disabled:hover:text-fail-text disabled:border-hair disabled:bg-soft disabled:text-muted",
};

export function buttonClass(variant: ButtonVariant = "secondary", extra = "", size: ButtonSize = "md"): string {
  return `${base} ${sizes[size]} ${variants[variant]} ${extra}`.trim();
}

type Props = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; size?: ButtonSize; icon?: IconName };

export function Button({ variant = "secondary", size = "md", icon, className = "", type = "button", children, ...rest }: Props) {
  return (
    <button type={type} className={buttonClass(variant, `${icon ? "pl-[12px]" : ""} ${className}`, size)} {...rest}>
      {icon && <Icon name={icon} size={16} />}
      {children}
    </button>
  );
}

type IconButtonProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children"> & {
  icon: IconName | "more";
  /** Nhãn đọc được (bắt buộc): thành aria-label và title. */
  label: string;
};

/** Nút chỉ có biểu tượng: ô vuông 40 px (44 px dưới 640 px), không viền. */
export function IconButton({ icon, label, className = "", type = "button", ...rest }: IconButtonProps) {
  return (
    <button
      type={type}
      aria-label={label}
      title={label}
      className={`inline-flex size-[44px] flex-none items-center justify-center rounded-ctl border border-transparent bg-transparent text-muted transition-colors not-disabled:hover:bg-soft not-disabled:hover:text-ink disabled:cursor-default disabled:text-hair sm:size-[40px] ${className}`.trim()}
      {...rest}
    >
      <Icon name={icon} size={18} />
    </button>
  );
}
