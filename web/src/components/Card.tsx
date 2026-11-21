// Thẻ (mục 6 của docs/GIAO-DIEN.md, bản 2): bo 10 px, viền --hair, nền
// --surface, đệm 16 px, không bóng. Có tiêu đề thì vẽ h2 16/600 và các nút
// bên phải; chân thẻ là một dải nền --soft chữ nhỏ.
//
//   <Card title="Máy" actions={<Button>Kiểm lại</Button>}>…</Card>
//
// Bảng không bọc trong Card: TableList tự vẽ thẻ của nó.

import type { ReactNode } from "react";

type Props = {
  title?: ReactNode;
  /** id của h2, cho aria-labelledby và cho neo (#…). */
  titleId?: string;
  /** Số đếm cạnh tiêu đề (chữ phụ). */
  count?: ReactNode;
  actions?: ReactNode;
  footer?: ReactNode;
  as?: "section" | "div";
  /** Chỉ class bố cục (khoảng cách, bề rộng, flex). */
  className?: string;
  /** id của cả thẻ (neo). */
  id?: string;
  children?: ReactNode;
  /**
   * ponytail: thuộc tính của bản 1 (thẻ sự cố có vạch đỏ), bản 2 bỏ vạch và
   * không dùng nó nữa. Chỉ giữ để màn chưa đổi (đợt 2, 3) còn biên dịch; gỡ ở
   * bước dọn cuối mốc M16.
   */
  incident?: boolean;
};

export const cardClass = "overflow-hidden rounded-card border border-hair bg-surface";

export function Card({ title, titleId, count, actions, footer, as = "section", className = "", id, children }: Props) {
  const Tag = as;
  const labelled = title && titleId ? { "aria-labelledby": titleId } : {};
  return (
    <Tag id={id} tabIndex={id ? -1 : undefined} className={`${cardClass} ${className}`.trim()} {...labelled}>
      {(title || actions) && (
        <div className="flex flex-wrap items-center justify-between gap-x-[16px] gap-y-[8px] px-[16px] pt-[14px]">
          {title && (
            <h2 id={titleId} className="m-0 flex min-w-0 items-baseline gap-[8px] text-[16px] leading-[1.4] font-semibold">
              {title}
              {count !== undefined && <span className="text-[13px] font-normal text-muted tabular-nums">{count}</span>}
            </h2>
          )}
          {actions && <div className="ml-auto flex flex-wrap items-center gap-[8px]">{actions}</div>}
        </div>
      )}
      {children !== undefined && children !== null && children !== false && <div className={title || actions ? "p-[16px] pt-[12px]" : "p-[16px]"}>{children}</div>}
      {footer && <div className="border-t border-hair bg-soft px-[16px] py-[10px] text-[13px] text-muted">{footer}</div>}
    </Tag>
  );
}
