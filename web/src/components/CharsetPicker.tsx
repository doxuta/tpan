// Hai ô chọn đi cặp: bộ ký tự (charset) rồi collation của nó. Chỉ chọn được cặp
// có trong GET /api/v1/collations (information_schema.COLLATIONS của MariaDB;
// mục 6.12), nên không có ô gõ tay. Đổi bộ ký tự thì collation nhảy về mặc
// định của bộ đó. Dùng cho hộp Tạo DB và khối Sửa, Đổi charset.

import { useId } from "react";
import type { Collation } from "../api/types";
import { useStrings } from "../i18n";
import { strings } from "../i18n/co-so-du-lieu";
import { Field, Select } from "./Field";

type Props = {
  collations: Collation[];
  charset: string;
  collation: string;
  onChange: (charset: string, collation: string) => void;
};

/** Cặp mặc định khi chưa chọn gì: utf8mb4 nếu máy có (WordPress cần), không thì bộ đầu tiên. */
export function defaultPair(collations: Collation[]): { charset: string; collation: string } {
  const pick = collations.find((c) => c.charset === "utf8mb4" && c.default) ?? collations.find((c) => c.default) ?? collations[0];
  return { charset: pick?.charset ?? "", collation: pick?.collation ?? "" };
}

export function CharsetPicker({ collations, charset, collation, onChange }: Props) {
  const t = useStrings(strings);
  const id = useId();
  const charsets = [...new Set(collations.map((c) => c.charset))].sort();
  const options = collations.filter((c) => c.charset === charset);

  function pickCharset(next: string) {
    const first = collations.find((c) => c.charset === next && c.default) ?? collations.find((c) => c.charset === next);
    onChange(next, first?.collation ?? "");
  }

  return (
    <div className="flex flex-wrap gap-x-[14px] gap-y-[12px]">
      {/* Bộ ký tự là sans (như cột "Bộ ký tự" của bảng), collation là giá trị cấu hình nên mono. */}
      <div className="min-w-[180px]">
        <Field label={t.charsetLabel} htmlFor={`${id}-cs`}>
          <Select id={`${id}-cs`} value={charset} onChange={(e) => pickCharset(e.target.value)}>
            {/* Charset hiện tại của DB không có trong danh sách (máy khác bản): vẫn hiện để ô chọn nói đúng thứ đang có. */}
            {!charsets.includes(charset) && charset !== "" && <option value={charset}>{charset}</option>}
            {charsets.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </Select>
        </Field>
      </div>
      <div className="min-w-[180px]">
        <Field label={t.collationLabel} htmlFor={`${id}-co`}>
          <Select id={`${id}-co`} mono value={collation} onChange={(e) => onChange(charset, e.target.value)}>
            {!options.some((c) => c.collation === collation) && collation !== "" && <option value={collation}>{collation}</option>}
            {options.map((c) => (
              <option key={c.collation} value={c.collation}>
                {c.collation}
                {c.default ? t.collationDefault : ""}
              </option>
            ))}
          </Select>
        </Field>
      </div>
    </div>
  );
}
