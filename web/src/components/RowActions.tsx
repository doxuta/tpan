// Cột Việc của một hàng bảng (mục 6 của docs/GIAO-DIEN.md, bản 2): tối đa hai
// nút chữ cho việc hay dùng, phần còn lại vào menu ba chấm. Không còn mục nào
// cho menu thì không vẽ nút ba chấm.
//
//   <RowActions
//     primary={[{ label: "Sửa", ariaLabel: "Sửa khach_a", onClick: … }]}
//     more={[{ label: "Xoá", danger: true, onSelect: … }]}
//     moreLabel="Việc khác với khach_a: tạm khoá, xem như, xoá" />

import type { ReactNode } from "react";
import { Button, IconButton } from "./Button";
import { Menu, MenuItem } from "./Menu";

export type RowAction = {
  label: ReactNode;
  onClick: () => void;
  disabled?: boolean;
  /** Đang chạy: thay nhãn bằng chữ này và khoá nút. */
  busyLabel?: ReactNode;
  busy?: boolean;
  /** Nhãn đọc đủ ý ("Sửa khach_a"), vì nhiều hàng có cùng nút "Sửa". */
  ariaLabel: string;
  /** Gợi ý khi rê chuột (ví dụ vì sao nút bị khoá). */
  title?: string;
};

export type MenuAction = {
  label: ReactNode;
  onSelect: () => void;
  disabled?: boolean;
  danger?: boolean;
  hint?: ReactNode;
};

type Props = {
  primary: RowAction[];
  more?: MenuAction[];
  /** Nhãn đọc của nút ba chấm, dạng "Việc khác với X: a, b, c". */
  moreLabel: string;
};

export function RowActions({ primary, more = [], moreLabel }: Props) {
  return (
    <div className="flex items-center justify-end gap-[4px]">
      {primary.slice(0, 2).map((a, i) => (
        <Button key={i} variant="text" onClick={a.onClick} disabled={a.disabled || a.busy} aria-label={a.ariaLabel} title={a.title} className="px-[12px]">
          {a.busy && a.busyLabel ? a.busyLabel : a.label}
        </Button>
      ))}
      {more.length > 0 && (
        <Menu label={moreLabel} trigger={<IconButton icon="more" label={moreLabel} aria-haspopup="menu" />}>
          {more.map((m, i) => (
            <MenuItem key={i} onSelect={m.onSelect} disabled={m.disabled} danger={m.danger} hint={m.hint}>
              {m.label}
            </MenuItem>
          ))}
        </Menu>
      )}
    </div>
  );
}
