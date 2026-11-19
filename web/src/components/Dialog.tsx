// Hộp hỏi lại trước một việc (Radix Dialog 1.3: khoá focus trong hộp, Esc để
// đóng, trả focus về nút đã mở). Nút làm việc là động từ của đúng việc đó
// ("Xoá site", không "OK"), theo giọng chữ ở mục 8.
//
//   <Dialog open={open} onOpenChange={setOpen}
//     title="Chạy 11 bước này?" confirmLabel="Chạy" onConfirm={run}>
//     <p>...</p>
//   </Dialog>

import * as RadixDialog from "@radix-ui/react-dialog";
import type { ReactNode } from "react";
import { useStrings } from "../i18n";
import { strings } from "../i18n/khung";
import { Button } from "./Button";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  /** Một câu nói rõ việc sắp xảy ra; đọc kèm tiêu đề cho trình đọc màn hình. */
  description?: string;
  children?: ReactNode;
  confirmLabel: string;
  onConfirm: () => void;
  /** Đang chạy việc: khoá nút để không bấm hai lần. */
  busy?: boolean;
};

export function Dialog({ open, onOpenChange, title, description, children, confirmLabel, onConfirm, busy = false }: Props) {
  const t = useStrings(strings);
  return (
    <RadixDialog.Root open={open} onOpenChange={onOpenChange}>
      <RadixDialog.Portal>
        {/* Làm mờ trang bằng chính màu nền, không bóng đổ (mục 9). */}
        <RadixDialog.Overlay className="fixed inset-0 bg-paper/80" />
        <RadixDialog.Content
          {...(description ? {} : { "aria-describedby": undefined })}
          className="fixed top-1/2 left-1/2 w-[min(480px,calc(100vw-32px))] max-h-[calc(100vh-32px)] -translate-x-1/2 -translate-y-1/2 overflow-y-auto border border-ink bg-surface p-[18px] text-ink"
        >
          <RadixDialog.Title className="m-0 text-[17px] leading-[1.3] font-semibold">{title}</RadixDialog.Title>
          {description && <RadixDialog.Description className="mt-[6px] mb-0 text-[14px] text-muted">{description}</RadixDialog.Description>}
          {children && <div className="mt-[14px]">{children}</div>}
          <div className="mt-[16px] flex flex-wrap gap-[8px]">
            <Button variant="primary" onClick={onConfirm} disabled={busy}>
              {confirmLabel}
            </Button>
            <RadixDialog.Close asChild>
              <Button>{t.cancel}</Button>
            </RadixDialog.Close>
          </div>
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}
