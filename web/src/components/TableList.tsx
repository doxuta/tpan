// Bảng kẻ mảnh cho các màn danh sách (Site, Cơ sở dữ liệu, Lịch sử...), như
// "Lịch sử máy" của trang mẫu: nhãn cột mono 11 px chữ hoa màu --muted, mỗi
// hàng kẻ trên --hair, không thẻ, không màu nền xen kẽ (mục 7).
//
//   <TableList
//     rows={sites}
//     rowKey={(s) => s.domain}
//     columns={[
//       { header: t.colSite, cell: (s) => <Link to={...}>{s.domain}</Link> },
//       { header: t.colPhp, cell: (s) => s.php_version, className: "w-[80px]" },
//     ]}
//     empty={t.noSites}
//   />
//
// Bảng nằm trong khung cuộn ngang riêng để cả trang không cuộn ngang ở màn hẹp.

import type { ReactNode } from "react";

export type Column<T> = {
  header: string;
  cell: (row: T) => ReactNode;
  /** Thêm class cho ô (độ rộng, canh phải...). */
  className?: string;
};

type Props<T> = {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  /** Câu khi chưa có hàng nào, theo giọng chữ: "Chưa có site nào. Tạo site đầu tiên". */
  empty: ReactNode;
  /** Tên bảng cho trình đọc màn hình khi trên bảng không có tiêu đề gần. */
  caption?: string;
};

export function TableList<T>({ columns, rows, rowKey, empty, caption }: Props<T>) {
  if (rows.length === 0) return <p className="border-t border-hair py-[10px] text-muted">{empty}</p>;
  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-left">
        {caption && <caption className="sr-only">{caption}</caption>}
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c.header} scope="col" className={`pr-[16px] pb-[6px] font-mono text-[11px] font-normal tracking-[0.08em] text-muted uppercase ${c.className ?? ""}`}>
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={rowKey(r)} className="border-t border-hair">
              {columns.map((c) => (
                <td key={c.header} className={`py-[10px] pr-[16px] align-baseline ${c.className ?? ""}`}>
                  {c.cell(r)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
