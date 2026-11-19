// Thẻ (mục 6): viền --card-border, nền --surface, đệm 18 px, góc vuông, không
// bóng. Thẻ sự cố có thêm vạch trên dày 4 px màu --fail.

import type { ReactNode } from "react";

type Props = {
  children: ReactNode;
  incident?: boolean;
  className?: string;
};

export function Card({ children, incident = false, className = "" }: Props) {
  return (
    <div className={`border border-card-border bg-surface p-[18px] ${incident ? "border-t-4 border-t-fail" : ""} ${className}`.trim()}>
      {children}
    </div>
  );
}
