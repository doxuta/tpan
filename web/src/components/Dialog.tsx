// Hộp hỏi lại trước một việc (Radix Dialog 1.3: khoá focus trong hộp, Esc để
// đóng, trả focus về nút đã mở). Nút làm việc là động từ của đúng việc đó
// ("Xoá site", không "OK"), theo giọng chữ ở mục 8. danger: việc không hoàn tác
// được, nút làm việc là nút nguy hiểm (chữ đỏ) thay cho nút chính.
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
  /** Việc không hoàn tác được. */
  danger?: boolean;
};

export function Dialog({ open, onOpenChange, title, description, children, confirmLabel, onConfirm, busy = false, danger = false }: Props) {
  const t = useStrings(strings);
  return (
    <RadixDialog.Root open={open} onOpenChange={onOpenChange}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay className="fixed inset-0 z-40 bg-scrim" />
        <RadixDialog.Content
          {...(description ? {} : { "aria-describedby": undefined })}
          className="fixed top-1/2 left-1/2 z-50 max-h-[calc(100vh-32px)] w-[min(480px,calc(100vw-32px))] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-card border border-hair bg-surface p-[20px] text-ink shadow-pop"
        >
          <RadixDialog.Title className="m-0 text-[16px] leading-[1.4] font-semibold">{title}</RadixDialog.Title>
          {description && <RadixDialog.Description className="mt-[6px] mb-0 text-[14px] text-muted">{description}</RadixDialog.Description>}
          {children && <div className="mt-[14px]">{children}</div>}
          {/* Nút làm việc đứng trước như bản 1: kịch bản bấm thật của M15, M16 bấm nút đầu tiên trong hộp. */}
          <div className="mt-[20px] flex flex-wrap gap-[8px]">
            <Button variant={danger ? "danger" : "primary"} onClick={onConfirm} disabled={busy}>
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
