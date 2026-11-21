// Ô nhập, ô chọn, ô chữ dài (mục 6 của docs/GIAO-DIEN.md, bản 2): nhãn 14/500
// nằm trên; ô cao 40 px (44 px dưới 640 px), viền --field (đủ 3:1 với nền thẻ:
// viền là thứ duy nhất cho biết ô nằm ở đâu, WCAG 1.4.11), nền --surface, bo
// 8 px; vô hiệu: nền --soft, viền --hair.
//
// Hai cách dùng:
//   <TextField id="u-ten" label="Tên đăng nhập" hint="…" value={…} onChange={…} />   (gọn)
//   <Field label="Bộ ký tự" htmlFor="cs"><Select id="cs" mono>…</Select></Field>       (khi cần ghép)

import { forwardRef, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from "react";
import { StatusMark } from "./StatusMark";

/** Class của ô; mono cho giá trị kỹ thuật (tên DB, đường dẫn). */
export function inputClass(opts: { mono?: boolean; extra?: string } = {}): string {
  return (
    "box-border block h-[44px] w-full rounded-ctl border border-field bg-surface px-[10px] text-ink placeholder:text-muted sm:h-[40px] " +
    "disabled:border-hair disabled:bg-soft disabled:text-muted aria-invalid:border-fail " +
    `${opts.mono ? "font-mono text-[13px]" : "text-[14px]"} ${opts.extra ?? ""}`
  ).trim();
}

type FieldProps = {
  label: ReactNode;
  htmlFor: string;
  hint?: ReactNode;
  /** id của dòng gợi ý (để ô trỏ aria-describedby tới). */
  hintId?: string;
  /** Câu lỗi hiện dưới ô, có dấu lỗi. */
  error?: ReactNode;
  children: ReactNode;
};

export function Field({ label, htmlFor, hint, hintId, error, children }: FieldProps) {
  return (
    <div>
      <label htmlFor={htmlFor} className="mb-[6px] block text-[14px] font-medium">
        {label}
      </label>
      {children}
      {hint && (
        <p id={hintId} className="mt-[6px] mb-0 text-[13px] text-muted">
          {hint}
        </p>
      )}
      {error && (
        <p role="alert" className="mt-[8px] mb-0 flex items-start gap-[8px]">
          <span className="mt-[2px] flex-none">
            <StatusMark status="fail" size="sm" />
          </span>
          <span>{error}</span>
        </p>
      )}
    </div>
  );
}

type InputProps = Omit<InputHTMLAttributes<HTMLInputElement>, "className"> & { mono?: boolean; className?: string };

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input({ mono, className, ...rest }, ref) {
  return <input ref={ref} className={inputClass({ mono, extra: className })} {...rest} />;
});

type SelectProps = Omit<SelectHTMLAttributes<HTMLSelectElement>, "className"> & { mono?: boolean; className?: string };

export function Select({ mono, className, children, ...rest }: SelectProps) {
  return (
    <select className={inputClass({ mono, extra: `pr-[8px] ${className ?? ""}` })} {...rest}>
      {children}
    </select>
  );
}

type TextareaProps = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "className"> & { mono?: boolean; className?: string };

export function Textarea({ mono, className, ...rest }: TextareaProps) {
  // Ô chữ dài không cao cố định: bỏ h-[…] của ô một dòng.
  return <textarea className={inputClass({ mono, extra: `h-auto py-[8px] sm:h-auto ${className ?? ""}` })} {...rest} />;
}

// ---- Hai dạng gọn: nhãn, ô và dòng gợi ý trong một thẻ (chữ ký của ô nhập bản 1, nên màn đổi sang chỉ đổi dòng import) ----

type TextFieldProps = Omit<InputHTMLAttributes<HTMLInputElement>, "id" | "className"> & {
  id: string;
  label: string;
  /** Dòng gợi ý dưới ô, đọc kèm khi vào ô. */
  hint?: string;
  invalid?: boolean;
  mono?: boolean;
};

export const TextField = forwardRef<HTMLInputElement, TextFieldProps>(function TextField({ id, label, hint, invalid = false, mono, ...rest }, ref) {
  const hintId = `${id}-goi-y`;
  return (
    <Field label={label} htmlFor={id} hint={hint} hintId={hintId}>
      <Input ref={ref} id={id} mono={mono} aria-invalid={invalid || undefined} aria-describedby={hint ? hintId : undefined} {...rest} />
    </Field>
  );
});

type SelectFieldProps = Omit<SelectHTMLAttributes<HTMLSelectElement>, "id" | "className"> & {
  id: string;
  label: string;
  hint?: string;
  mono?: boolean;
  children: ReactNode;
};

export function SelectField({ id, label, hint, mono, children, ...rest }: SelectFieldProps) {
  const hintId = `${id}-goi-y`;
  return (
    <Field label={label} htmlFor={id} hint={hint} hintId={hintId}>
      <Select id={id} mono={mono} aria-describedby={hint ? hintId : undefined} {...rest}>
        {children}
      </Select>
    </Field>
  );
}

// ---- Ô chọn ----

type CheckboxProps = Omit<InputHTMLAttributes<HTMLInputElement>, "type" | "className"> & {
  /** Nhãn đọc được (aria-label), luôn có. */
  label: string;
  /** Có thì ô bị khoá, và lời này nói vì sao (vào nhãn đọc và title), không để ô trống không lời. */
  locked?: string;
};

export function Checkbox({ label, locked, ...rest }: CheckboxProps) {
  const name = locked ? `${label}: ${locked}` : label;
  return (
    // disabled đặt sau {...rest}: bên gọi truyền disabled={false} cũng không mở được ô đang khoá.
    <input
      type="checkbox"
      aria-label={name}
      title={locked ?? label}
      className="size-[16px] flex-none accent-[var(--accent)] disabled:cursor-not-allowed max-sm:size-[20px]"
      {...rest}
      disabled={locked !== undefined || rest.disabled}
    />
  );
}
