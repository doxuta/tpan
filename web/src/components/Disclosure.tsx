// Khối mở ra (mục 6 của docs/GIAO-DIEN.md, bản 2): một nút cả hàng có mũi tên
// và aria-expanded; nội dung CHỈ được dựng khi mở (50 site thì 98 dòng bằng
// chứng không nằm sẵn trong trang). Dùng cho bằng chứng, nhóm phép kiểm, lệnh
// cho Claude Code.
//
//   <Disclosure summary={<StatusLine status="pass">Cô lập với 6 site khác: đạt</StatusLine>}>…</Disclosure>
//   <Disclosure open={open} onOpenChange={setOpen} summary="…">…</Disclosure>   (giữ trạng thái ở cha)

import { useId, useState, type ReactNode } from "react";
import { Icon } from "./Icon";

type Props = {
  summary: ReactNode;
  children: ReactNode;
  /** Có thì do cha giữ trạng thái (ví dụ cần sống qua lần đổi tab). */
  open?: boolean;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
  /** id của nút (để trả focus về). */
  id?: string;
  /** Không kẻ trên (khối đầu tiên trong một nhóm). */
  flush?: boolean;
};

export function Disclosure({ summary, children, open, defaultOpen = false, onOpenChange, id, flush = false }: Props) {
  const [own, setOwn] = useState(defaultOpen);
  const isOpen = open ?? own;
  const panelId = useId();
  const toggle = () => {
    const next = !isOpen;
    if (open === undefined) setOwn(next);
    onOpenChange?.(next);
  };
  return (
    <div className={flush ? "" : "border-t border-hair first:border-t-0"}>
      <button
        type="button"
        id={id}
        aria-expanded={isOpen}
        aria-controls={isOpen ? panelId : undefined}
        onClick={toggle}
        className="flex min-h-[44px] w-full items-center gap-[12px] bg-transparent py-[8px] text-left text-ink not-disabled:hover:bg-soft"
      >
        <span className="min-w-0 flex-1">{summary}</span>
        <Icon name="chevron-down" size={16} className={`text-muted transition-transform ${isOpen ? "rotate-180" : ""}`} />
      </button>
      {isOpen && (
        <div id={panelId} className="pb-[14px]">
          {children}
        </div>
      )}
    </div>
  );
}
