// Menu thả (mục 6 của docs/GIAO-DIEN.md, bản 2): menu tài khoản, menu ba chấm
// của hàng bảng. Bọc Radix DropdownMenu 2.2 và LUÔN đặt modal={false}: chế độ
// modal mặc định của Radix chèn thẻ <style> khoá cuộn mà CSP default-src 'self'
// của panel chặn (đã đo ở docs/bang-chung/M16-nghien-cuu/t2-radix-csp.ket-qua.txt),
// và menu của panel cũng không cần chặn con trỏ trên cả trang. Mọi menu thả đi
// qua file này (web/kieu_dang_test.go, luật 8).
//
//   <Menu label="Việc khác với tp_blog: nhập, đổi charset" trigger={<IconButton … />}>
//     <MenuItem onSelect={…}>Nhập từ bản đã xuất</MenuItem>
//     <MenuSeparator />
//     <MenuItem danger onSelect={…}>Xoá</MenuItem>
//   </Menu>
//
// Radix lo phần bàn phím: mở bằng Enter, Space, mũi tên xuống; đi bằng mũi tên;
// Esc đóng và trả focus về nút mở; mục bị khoá bị bỏ qua.

import * as DM from "@radix-ui/react-dropdown-menu";
import type { ReactElement, ReactNode } from "react";
import { Link } from "react-router";
import { Icon } from "./Icon";

type MenuProps = {
  /** Nút mở menu; phải nhận ref và thuộc tính của nút (Button, IconButton, <button>). */
  trigger: ReactElement;
  /** Tên đọc được của menu (aria-label của khung menu). */
  label: string;
  align?: "start" | "end";
  children: ReactNode;
};

export function Menu({ trigger, label, align = "end", children }: MenuProps) {
  return (
    <DM.Root modal={false}>
      <DM.Trigger asChild>{trigger}</DM.Trigger>
      <DM.Portal>
        <DM.Content
          aria-label={label}
          align={align}
          sideOffset={4}
          collisionPadding={8}
          className="z-50 max-h-[var(--radix-dropdown-menu-content-available-height)] min-w-[200px] max-w-[min(320px,calc(100vw-16px))] overflow-y-auto rounded-ctl border border-hair bg-surface p-[4px] text-[14px] text-ink shadow-pop"
        >
          {children}
        </DM.Content>
      </DM.Portal>
    </DM.Root>
  );
}

const item =
  "flex min-h-[44px] cursor-default items-center gap-[10px] rounded-item px-[10px] py-[6px] outline-none select-none sm:min-h-[40px] " +
  "data-[highlighted]:bg-soft data-[disabled]:text-muted";

type ItemProps = {
  onSelect?: () => void;
  disabled?: boolean;
  /** Việc không hoàn tác được: chữ --fail-text. */
  danger?: boolean;
  /** Dòng phụ dưới nhãn (ví dụ vì sao mục bị khoá). */
  hint?: ReactNode;
  children: ReactNode;
};

export function MenuItem({ onSelect, disabled, danger = false, hint, children }: ItemProps) {
  // Mục bị khoá mà có lời giải thích (ví dụ "Site đang tạm khoá…"): vẫn tới được bằng
  // mũi tên để trình đọc màn hình đọc lời đó, nhưng chọn thì không làm gì và menu không đóng.
  // Radix bỏ qua hẳn mục disabled khi đi bằng phím, nên lời giải thích sẽ không ai nghe thấy.
  const blocked = disabled === true && hint !== undefined;
  return (
    <DM.Item
      disabled={disabled === true && !blocked}
      aria-disabled={blocked || undefined}
      onSelect={blocked ? (e) => e.preventDefault() : onSelect}
      className={`${item} ${blocked ? "text-muted" : danger ? "text-fail-text" : ""}`}
    >
      <span className="flex min-w-0 flex-col">
        <span>{children}</span>
        {hint && <span className="text-[12.5px] text-muted">{hint}</span>}
      </span>
    </DM.Item>
  );
}

/** Mục là một liên kết trong app (đi tới màn khác). */
export function MenuLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <DM.Item asChild className={item}>
      <Link to={to} className="text-ink no-underline hover:text-ink">
        {children}
      </Link>
    </DM.Item>
  );
}

/** Nhãn nhóm (không chọn được), ví dụ tên và vai ở đầu menu tài khoản. */
export function MenuLabel({ children }: { children: ReactNode }) {
  return <DM.Label className="px-[10px] pt-[8px] pb-[4px] text-[12px] font-medium text-muted">{children}</DM.Label>;
}

export function MenuSeparator() {
  return <DM.Separator className="my-[4px] h-px bg-hair" />;
}

type RadioGroupProps<T extends string> = {
  /** Tên nhóm, hiện như MenuLabel và là tên đọc của nhóm. */
  label: string;
  value: T;
  onValueChange: (v: T) => void;
  children: ReactNode;
};

export function MenuRadioGroup<T extends string>({ label, value, onValueChange, children }: RadioGroupProps<T>) {
  return (
    <DM.Group aria-label={label}>
      <MenuLabel>{label}</MenuLabel>
      <DM.RadioGroup value={value} onValueChange={(v) => onValueChange(v as T)}>
        {children}
      </DM.RadioGroup>
    </DM.Group>
  );
}

/** Dấu chọn chiếm chỗ cố định để chữ các mục thẳng hàng. */
function Check() {
  return (
    <span className="flex size-[16px] flex-none items-center justify-center text-accent">
      <DM.ItemIndicator>
        <Icon name="check" size={16} />
      </DM.ItemIndicator>
    </span>
  );
}

export function MenuRadioItem({ value, children }: { value: string; children: ReactNode }) {
  return (
    // Không đóng menu khi chọn: người dùng thấy ngay dấu chọn đổi chỗ (Radix mặc định đóng).
    <DM.RadioItem value={value} onSelect={(e) => e.preventDefault()} className={item}>
      <Check />
      {children}
    </DM.RadioItem>
  );
}

export function MenuCheckboxItem({ checked, onCheckedChange, children }: { checked: boolean; onCheckedChange: (v: boolean) => void; children: ReactNode }) {
  return (
    <DM.CheckboxItem checked={checked} onCheckedChange={(v) => onCheckedChange(v === true)} onSelect={(e) => e.preventDefault()} className={item}>
      <Check />
      {children}
    </DM.CheckboxItem>
  );
}
