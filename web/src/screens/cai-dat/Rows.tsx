// Hai cách xếp "nhãn rồi giá trị" dùng chung cho mọi thẻ của màn Cài đặt và màn
// Cập nhật (cả khối "Phiên bản và cập nhật" nằm ở hai màn): nhãn 12/500 --muted
// viết thường, giá trị bên dưới hoặc bên phải. Chỉ là class bố cục, không có kiểu
// dáng riêng.
//
//   <Rows><Row label="Chế độ">…</Row></Rows>      các dòng kẻ ngang, nhãn bên trái, giá trị bên phải
//   <Facts><Fact label="Bản">…</Fact></Facts>     vài giá trị ngắn xếp ngang, nhãn trên giá trị

import type { ReactNode } from "react";

const labelClass = "m-0 text-[12px] font-medium text-muted";

export function Rows({ children }: { children: ReactNode }) {
  return <dl className="m-0">{children}</dl>;
}

export function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-wrap gap-x-[16px] gap-y-[2px] border-t border-hair py-[10px] first:border-t-0 first:pt-0 last:pb-0">
      <dt className={`flex-[0_0_200px] pt-[1px] ${labelClass}`}>{label}</dt>
      <dd className="m-0 min-w-0 flex-[1_1_280px]">{children}</dd>
    </div>
  );
}

export function Facts({ children }: { children: ReactNode }) {
  return <dl className="m-0 flex flex-wrap gap-x-[40px] gap-y-[12px]">{children}</dl>;
}

export function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className={labelClass}>{label}</dt>
      <dd className="m-0 mt-[4px]">{children}</dd>
    </div>
  );
}
