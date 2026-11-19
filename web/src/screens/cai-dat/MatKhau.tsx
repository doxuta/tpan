// Đổi mật khẩu quản trị: POST /api/v1/account/password. Máy chủ kiểm mật khẩu
// hiện tại (sai thì đếm như đăng nhập sai), đổi mật khẩu rồi huỷ mọi phiên
// khác. Độ dài tối thiểu (12) phải khớp authMinPassword ở internal/server/auth.go;
// máy chủ đếm theo ký tự (rune), nên ở đây cũng đếm theo ký tự chứ không theo
// đơn vị UTF-16 của string.length.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRef, useState, type FormEvent } from "react";
import { ApiError, errorMessage, post } from "../../api/client";
import type { PasswordChange } from "../../api/types";
import { Button } from "../../components/Button";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/cai-dat";
import { Field } from "../dang-nhap/Field";
import { Note } from "./Note";
import { sessionsKey } from "./Phien";

const minLength = 12;

export function MatKhau() {
  const t = useStrings(strings);
  const { lang } = useLang();
  const qc = useQueryClient();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [repeat, setRepeat] = useState("");
  // Lỗi bắt được ngay trong trình duyệt, trước khi gửi.
  const [localError, setLocalError] = useState<string | null>(null);
  const currentRef = useRef<HTMLInputElement>(null);
  const nextRef = useRef<HTMLInputElement>(null);
  const repeatRef = useRef<HTMLInputElement>(null);

  const change = useMutation({
    mutationFn: (body: PasswordChange) => post<void>("/api/v1/account/password", body),
    onSuccess: () => {
      setCurrent("");
      setNext("");
      setRepeat("");
      // Các phiên khác vừa bị huỷ: đọc lại danh sách.
      void qc.invalidateQueries({ queryKey: sessionsKey });
    },
    onError: (err) => {
      if (err instanceof ApiError && err.code === "wrong_password") {
        setCurrent("");
        currentRef.current?.focus();
      }
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    change.reset();
    if ([...next].length < minLength) {
      setLocalError(t.pwTooShort(minLength));
      nextRef.current?.focus();
      return;
    }
    if (next !== repeat) {
      setLocalError(t.pwMismatch);
      repeatRef.current?.focus();
      return;
    }
    setLocalError(null);
    change.mutate({ current, new: next });
  };

  const serverError = change.error ? (change.error instanceof ApiError && change.error.code === "wrong_password" ? t.pwWrongCurrent : errorMessage(change.error, lang)) : null;
  const error = localError ?? serverError;

  return (
    <section aria-labelledby="mat-khau-tieu-de" className="pt-[28px]">
      <h2 id="mat-khau-tieu-de" className="m-0 text-[16px] font-semibold">
        {t.pwHeading}
      </h2>
      <p className="mt-[6px] mb-[14px] text-muted">{t.pwIntro(minLength)}</p>

      <form onSubmit={submit} className="flex max-w-[420px] flex-col gap-[16px]">
        <Field
          ref={currentRef}
          id="mk-hien-tai"
          type="password"
          label={t.pwCurrent}
          autoComplete="current-password"
          required
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          invalid={change.error instanceof ApiError && change.error.code === "wrong_password"}
        />
        <Field
          ref={nextRef}
          id="mk-moi"
          type="password"
          label={t.pwNew}
          autoComplete="new-password"
          required
          value={next}
          onChange={(e) => setNext(e.target.value)}
        />
        <Field
          ref={repeatRef}
          id="mk-nhap-lai"
          type="password"
          label={t.pwRepeat}
          autoComplete="new-password"
          required
          value={repeat}
          onChange={(e) => setRepeat(e.target.value)}
        />
        <div>
          <Button type="submit" variant="primary" disabled={change.isPending}>
            {change.isPending ? t.pwBusy : t.pwSubmit}
          </Button>
        </div>
        {error && <Note kind="fail">{error}</Note>}
        {change.isSuccess && !error && <Note kind="pass">{t.pwDone}</Note>}
      </form>
    </section>
  );
}
