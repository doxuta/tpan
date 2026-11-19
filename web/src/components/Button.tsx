// Nút chính và nút phụ (mục 6 của docs/GIAO-DIEN.md). Mỗi khối chỉ có một nút
// chính. Liên kết trông như nút (ví dụ "Hỏi AI") dùng buttonClass cho <a> hay
// <Link> để vẫn là liên kết thật:
//
//   <Button variant="primary" onClick={run}>Tạo 3 site</Button>
//   <Link to="/sites/new" className={buttonClass("secondary")}>Tạo site</Link>

import type { ButtonHTMLAttributes } from "react";

export type ButtonVariant = "primary" | "secondary";

const base =
  "inline-flex items-center justify-center gap-[8px] h-[44px] sm:h-[40px] px-[14px] border text-[14px] font-medium leading-none no-underline whitespace-nowrap transition-colors " +
  // Vô hiệu: chữ --muted, viền --hair, con trỏ không đổi thành bàn tay.
  "disabled:cursor-default disabled:border-hair disabled:bg-surface disabled:text-muted aria-disabled:cursor-default aria-disabled:border-hair aria-disabled:bg-surface aria-disabled:text-muted";

const variants: Record<ButtonVariant, string> = {
  primary: "border-ink bg-ink text-on-ink hover:text-on-ink",
  secondary: "border-ink bg-surface text-ink hover:bg-pre hover:text-ink",
};

export function buttonClass(variant: ButtonVariant = "secondary", extra = ""): string {
  return `${base} ${variants[variant]} ${extra}`.trim();
}

type Props = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant };

export function Button({ variant = "secondary", className = "", type = "button", ...rest }: Props) {
  return <button type={type} className={buttonClass(variant, className)} {...rest} />;
}
