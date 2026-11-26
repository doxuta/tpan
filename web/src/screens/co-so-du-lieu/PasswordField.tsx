// M36 (v0.2.17): ô nhập mật khẩu DB tự đặt, dùng ở hộp "Tạo DB" và khối "Đổi
// mật khẩu". Ô kiểu password, nút Hiện/Ẩn bên cạnh, dòng luật ở dưới. Lỗi luật
// chỉ hiện khi showError (người dùng đã bấm Xem trước), để không đỏ ô ngay lúc
// vừa gõ ký tự đầu.

import { useState } from "react";
import { Button } from "../../components/Button";
import { Field, Input } from "../../components/Field";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/co-so-du-lieu";
import { dbPasswordProblem } from "./password";

type Props = {
  id: string;
  label: string;
  /** Dòng gợi ý đặt trước luật (ví dụ "Để trống để TPAN tạo ngẫu nhiên"). */
  lead?: string;
  value: string;
  onChange: (v: string) => void;
  /** Tên user DB: mật khẩu không được chứa nó. */
  dbName: string;
  showError: boolean;
  /** Ô trống là hợp lệ (hộp Tạo DB: trống là để TPAN tạo). */
  optional?: boolean;
};

export function PasswordField({ id, label, lead, value, onChange, dbName, showError, optional = false }: Props) {
  const t = useStrings(strings);
  const [shown, setShown] = useState(false);
  const problem = optional && value === "" ? null : dbPasswordProblem(value, dbName);
  const error = showError && problem ? t.pwProblem[problem] : undefined;
  const hintId = `${id}-goi-y`;
  return (
    <Field
      label={label}
      htmlFor={id}
      hintId={hintId}
      hint={
        <>
          {lead && <span className="block">{lead}</span>}
          <span className="block">{t.pwRule}</span>
        </>
      }
      error={error}
    >
      <div className="flex max-w-[560px] items-center gap-[8px]">
        <Input
          id={id}
          mono
          type={shown ? "text" : "password"}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          aria-describedby={hintId}
          aria-invalid={error ? true : undefined}
          autoComplete="new-password"
          spellCheck={false}
          autoCapitalize="none"
          className="min-w-0 flex-1"
        />
        <Button aria-controls={id} aria-pressed={shown} onClick={() => setShown((v) => !v)}>
          {shown ? t.pwHide : t.pwShow}
        </Button>
      </div>
    </Field>
  );
}

/** Mật khẩu dùng được để gửi: trống (khi optional) hoặc đúng luật. */
export function passwordReady(value: string, dbName: string, optional = false): boolean {
  return (optional && value === "") || dbPasswordProblem(value, dbName) === null;
}
