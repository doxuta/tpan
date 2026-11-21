// Bảng của các màn danh sách (mục 6 của docs/GIAO-DIEN.md, bản 2). Bảng TỰ VẼ
// THẺ của nó (bo 10 px, viền --hair, nền --surface): đừng bọc trong Card. Có
// title thì đầu thẻ là h2 và các nút; đầu bảng nền --soft chữ 12/500 --muted;
// ô đệm 8 × 12 px canh giữa theo chiều dọc (hàng có nút cao 56 px); kẻ hàng
// --hair; footer là dải nền --soft dưới bảng.
//
//   <TableList
//     label="Danh sách người dùng" minWidth={900}
//     rows={users} rowKey={(u) => u.name}
//     columns={[
//       { header: t.colUser, cell: (u) => u.name },
//       { header: t.colSize, cell: (u) => u.size, align: "right" },
//     ]}
//     empty={t.noUsers}
//     footer={<span>3 người dùng</span>}
//   />
//
// Bảng nằm trong khung cuộn ngang riêng để cả trang không cuộn ngang ở màn hẹp.

import type { ReactNode } from "react";
import { cardClass } from "./Card";
import { EmptyState } from "./EmptyState";

export type Column<T> = {
  /** Tên cột, cũng là khoá React của cột. */
  header: string;
  /** Hiện thay cho chữ ở đầu cột (ví dụ ô "chọn tất cả"); header vẫn là khoá. */
  headerCell?: ReactNode;
  cell: (row: T) => ReactNode;
  /** Thêm class bố cục cho ô (độ rộng...). */
  className?: string;
  /** Canh phải (cột số, cột Việc). */
  align?: "right";
  /** Không xuống dòng trong ô (mặc định true). false cho ô có chữ dài cần ngắt (tên miền dài, ghi chú). */
  nowrap?: boolean;
};

type Props<T> = {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  /** Câu khi chưa có hàng nào, theo giọng chữ: "Chưa có site nào. Tạo site đầu tiên". */
  empty: ReactNode;
  /** Tên bảng cho trình đọc màn hình (caption ẩn). */
  caption?: string;
  /** Tiêu đề h2 ở đầu thẻ. */
  title?: ReactNode;
  titleId?: string;
  /** Số đếm cạnh tiêu đề. */
  count?: ReactNode;
  /** Nút ở đầu thẻ, bên phải tiêu đề. */
  actions?: ReactNode;
  /** Dải chân bảng (số hàng, chú thích). */
  footer?: ReactNode;
  /** Bề rộng tối thiểu của bảng (px); hẹp hơn thì khung cuộn ngang. */
  minWidth?: number;
  /** Tên đọc của cả vùng (aria-label của section) khi không có title. */
  label?: string;
  /** Khối đặt giữa đầu thẻ và bảng (ví dụ dải "Đã chọn N"). */
  above?: ReactNode;
  /** id của thẻ (neo). */
  id?: string;
  /** Hàng đang chọn: tô nền --accent-tint. */
  selected?: (row: T) => boolean;
};

export function TableList<T>({ columns, rows, rowKey, empty, caption, title, titleId, count, actions, footer, minWidth, label, above, id, selected }: Props<T>) {
  const name = title && titleId ? { "aria-labelledby": titleId } : label ? { "aria-label": label } : {};
  const last = columns.length - 1;
  // Ô đầu đệm trái 16 px, ô cuối đệm phải 8 px (nút chữ cuối hàng đã có đệm riêng): thẳng với chữ ở đầu thẻ.
  const edge = (i: number) => `${i === 0 ? "pl-[16px]" : ""} ${i === last ? "pr-[8px]" : ""}`;
  return (
    <section id={id} tabIndex={id ? -1 : undefined} className={cardClass} {...name}>
      {(title || actions) && (
        <div className="flex flex-wrap items-center justify-between gap-x-[16px] gap-y-[8px] border-b border-hair px-[16px] py-[12px]">
          {title && (
            <h2 id={titleId} className="m-0 flex min-w-0 items-baseline gap-[8px] text-[16px] leading-[1.4] font-semibold">
              {title}
              {count !== undefined && <span className="text-[13px] font-normal text-muted tabular-nums">{count}</span>}
            </h2>
          )}
          {actions && <div className="ml-auto flex flex-wrap items-center gap-[8px]">{actions}</div>}
        </div>
      )}
      {above}
      {rows.length === 0 ? (
        <div className="px-[16px] py-[14px]">
          <EmptyState>{empty}</EmptyState>
        </div>
      ) : (
        // relative: span sr-only (absolute) trong ô bảng phải bị khung cuộn cắt, nếu
        // không nó kéo cả trang cuộn ngang ở 390 px.
        <div className="relative overflow-x-auto">
          <table className="w-full border-collapse text-left" style={minWidth ? { minWidth } : undefined}>
            {caption && <caption className="sr-only">{caption}</caption>}
            <thead>
              <tr className="bg-soft">
                {columns.map((c, i) => (
                  <th
                    key={c.header}
                    scope="col"
                    className={`border-b border-hair px-[12px] py-[10px] text-[12px] font-medium whitespace-nowrap text-muted ${c.align === "right" ? "text-right" : "text-left"} ${edge(i)} ${i === last && c.align === "right" ? "pr-[20px]" : ""} ${c.className ?? ""}`}
                  >
                    {c.headerCell ?? c.header}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={rowKey(r)} className={`border-t border-hair first:border-t-0 ${selected?.(r) ? "bg-accent-tint" : ""}`}>
                  {columns.map((c, i) => (
                    <td
                      key={c.header}
                      className={`px-[12px] py-[8px] align-middle ${c.nowrap === false ? "" : "whitespace-nowrap"} ${c.align === "right" ? "text-right tabular-nums" : ""} ${edge(i)} ${c.className ?? ""}`}
                    >
                      {c.cell(r)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {footer && (
        <div className="flex flex-wrap justify-between gap-x-[24px] gap-y-[8px] border-t border-hair bg-soft px-[16px] py-[10px] text-[13px] text-muted">{footer}</div>
      )}
    </section>
  );
}
