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
};

export function SuspendTag({ how, label }: Props) {
  const m = useStrings(chung);
  return (
    <Tag title={m.suspendedByHint[how]}>
      {label ?? m.suspendedState}
      <span className="sr-only">: {m.suspendedBy[how]}</span>
    </Tag>
  );
}
