// Nhãn "tạm khoá" cạnh tên site hay DB. Là nhãn đen trắng như mọi nhãn (Tag),
// không tô màu: màu chỉ dành cho dấu trạng thái. Rê chuột (title) nói vì sao bị khoá.

import type { Suspension } from "../api/types";
import { useStrings } from "../i18n";
import { strings as chung } from "../i18n/m13-chung";
import { Tag } from "./Tag";

type Props = {
  how: Suspension;
  /** Nhãn khác "tạm khoá", ví dụ "DB khoá" cho user DB của một site. */
  label?: string;
  /** "surface" khi đặt thẳng trên nền trang (ví dụ trong meta của ScreenHeader), như Tag. */
  tone?: "paper" | "surface";
};

export function SuspendTag({ how, label, tone }: Props) {
  const m = useStrings(chung);
  return (
    <Tag title={m.suspendedByHint[how]} tone={tone}>
      {label ?? m.suspendedState}
      <span className="sr-only">: {m.suspendedBy[how]}</span>
    </Tag>
  );
}
