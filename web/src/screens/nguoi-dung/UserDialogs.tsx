// Các hộp thoại của màn Người dùng: thêm hoặc sửa, đặt lý do tạm khoá, đổi mật
// khẩu, xoá. Mỗi hộp là một component riêng, chỉ được dựng khi đang mở, nên ô
// nhập tự về giá trị đầu mỗi lần mở. Việc đổi máy (tạm khoá, mở khoá, đưa vào
// thùng rác) KHÔNG chạy ở đây: hộp chỉ thu đầu vào rồi giao cho màn mở bản xem
// trước (PlanReview); thêm, sửa, đổi mật khẩu, xoá user chỉ ghi vào tpan.db
// nên không có job.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent, type ReactNode } from "react";
import { ApiError, del, post } from "../../api/client";
import { packagesKey, refreshAccounts, userPath, usersPath } from "../../api/m13-users";
import type { PasswordSetResult, Role, UserCreated, UserRow } from "../../api/types";
import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { SelectField, TextField } from "../../components/Field";
import { Notice } from "../../components/Notice";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/nguoi-dung";
import { ErrorNote } from "../site/ErrorNote";
import type { Secret } from "./PasswordBox";

/** Mật khẩu do người gõ: rỗng là để tpan sinh. Đếm theo ký tự như máy chủ. */
const tooShort = (pw: string, min: number) => pw !== "" && [...pw].length < min;

function Form({ onSubmit, children }: { onSubmit: () => void; children: ReactNode }) {
  const submit = (e: FormEvent) => {
    e.preventDefault();
    onSubmit();
  };
  return (
    <form onSubmit={submit} className="flex flex-col gap-[14px]">
      {children}
      {/* Nút gửi ẩn để bấm Enter trong ô nhập là gửi (nút thật của hộp nằm ngoài form). */}
      <button type="submit" tabIndex={-1} aria-hidden="true" className="sr-only" />
    </form>
  );
}

// ---- Thêm hoặc sửa ----

type FormProps = {
  /** Có thì là sửa user này; không có là thêm user mới. */
  row?: UserRow;
  /** Tên các gói để chọn (gói của user luôn có trong danh sách, dù danh sách gói chưa tải). */
  packages: string[];
  onClose: () => void;
  /** Gọi sau khi lưu xong: secret có khi tpan sinh mật khẩu. */
  onDone: (message: "added" | "edited", name: string, secret: Secret | null) => void;
};

export function UserFormDialog({ row, packages, onClose, onDone }: FormProps) {
  const t = useStrings(strings);
  const qc = useQueryClient();
  const edit = row !== undefined;
  const [name, setName] = useState(row?.user.name ?? "");
  const [full, setFull] = useState(row?.user.full_name ?? "");
  const [email, setEmail] = useState(row?.user.email ?? "");
  const [role, setRole] = useState<Role>(row?.user.role ?? "user");
  const [pkg, setPkg] = useState(row?.user.package ?? "default");
  const [password, setPassword] = useState("");
  const [local, setLocal] = useState<string | null>(null);

  const options = packages.includes(pkg) ? packages : [pkg, ...packages];

  const save = useMutation({
    mutationFn: (): Promise<UserCreated | UserRow> =>
      edit
        ? // Chỉ gửi trường đã đổi (UserEdit: trường vắng là giữ nguyên); gửi cả vai không đổi thì vô ích và dễ chạm luật admin cuối.
          post<UserRow>(userPath(row.user.name), {
            ...(full !== row.user.full_name && { full_name: full }),
            ...(email !== row.user.email && { email }),
            ...(role !== row.user.role && { role }),
            ...(pkg !== row.user.package && { package: pkg }),
          })
        : post<UserCreated>(usersPath, { name: name.trim(), full_name: full, email, role, package: pkg, password: password || undefined }),
    onSuccess: (res) => {
      refreshAccounts(qc);
      void qc.invalidateQueries({ queryKey: packagesKey });
      if (edit) onDone("edited", row.user.name, null);
      else {
        const made = res as UserCreated;
        onDone("added", made.user.user.name, made.password ? { name: made.user.user.name, password: made.password } : null);
      }
      onClose();
    },
  });

  const submit = () => {
    if (save.isPending) return;
    save.reset();
    if (!edit && name.trim() === "") return setLocal(t.nameMissing);
    if (!edit && tooShort(password, t.minPassword)) return setLocal(t.passwordTooShort(t.minPassword));
    setLocal(null);
    save.mutate();
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={edit ? t.editTitle(row.user.name) : t.addTitle} confirmLabel={save.isPending ? t.saving : edit ? t.editConfirm : t.addConfirm} onConfirm={submit} busy={save.isPending}>
      <Form onSubmit={submit}>
        {!edit && <TextField id="u-ten" label={t.fName} hint={t.fNameHint} autoComplete="off" autoCapitalize="none" spellCheck={false} autoFocus required value={name} onChange={(e) => setName(e.target.value)} />}
        <TextField id="u-day-du" label={t.fFull} autoComplete="off" autoFocus={edit} value={full} onChange={(e) => setFull(e.target.value)} />
        <TextField id="u-email" type="email" label={t.fEmail} autoComplete="off" value={email} onChange={(e) => setEmail(e.target.value)} />
        <SelectField id="u-vai" label={t.fRole} value={role} onChange={(e) => setRole(e.target.value as Role)}>
          <option value="user">{t.roleOptionUser}</option>
          <option value="admin">{t.roleOptionAdmin}</option>
        </SelectField>
        <SelectField id="u-goi" label={t.fPackage} value={pkg} onChange={(e) => setPkg(e.target.value)}>
          {options.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </SelectField>
        {!edit && <TextField id="u-mat-khau" type="password" label={t.fPassword} hint={t.fPasswordHint} autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />}
        {local && (
          <Notice kind="fail" role="alert">
            {local}
          </Notice>
        )}
        {save.isError && <ErrorNote error={save.error} />}
      </Form>
    </Dialog>
  );
}

// ---- Lý do tạm khoá ----

/** title: "Tạm khoá <tên>?" cho một user, hoặc "Tạm khoá 3 user?" khi khoá hàng loạt. */
export function SuspendDialog({ title, onClose, onAsk }: { title: string; onClose: () => void; onAsk: (reason: string) => void }) {
  const t = useStrings(strings);
  const [reason, setReason] = useState("");
  const ask = () => onAsk(reason.trim());
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={title} description={t.suspendDesc} confirmLabel={t.suspendConfirm} onConfirm={ask}>
      <Form onSubmit={ask}>
        <TextField id="u-ly-do" label={t.reasonLabel} hint={t.reasonHint} maxLength={200} autoFocus autoComplete="off" value={reason} onChange={(e) => setReason(e.target.value)} />
      </Form>
    </Dialog>
  );
}

// ---- Đổi mật khẩu ----

export function PasswordDialog({ name, onClose, onDone }: { name: string; onClose: () => void; onDone: (name: string, secret: Secret | null) => void }) {
  const t = useStrings(strings);
  const [password, setPassword] = useState("");
  const [local, setLocal] = useState<string | null>(null);
  const change = useMutation({
    mutationFn: () => post<PasswordSetResult>(userPath(name, "/password"), { password: password || undefined }),
    onSuccess: (res) => {
      onDone(name, res.password ? { name, password: res.password } : null);
      onClose();
    },
  });
  const submit = () => {
    if (change.isPending) return;
    change.reset();
    if (tooShort(password, t.minPassword)) return setLocal(t.passwordTooShort(t.minPassword));
    setLocal(null);
    change.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={t.pwTitle(name)} description={t.pwDesc} confirmLabel={change.isPending ? t.saving : t.pwConfirm} onConfirm={submit} busy={change.isPending}>
      <Form onSubmit={submit}>
        <TextField id="u-mk-moi" type="password" label={t.fPassword} hint={t.fPasswordHint} autoComplete="new-password" autoFocus value={password} onChange={(e) => setPassword(e.target.value)} />
        {local && (
          <Notice kind="fail" role="alert">
            {local}
          </Notice>
        )}
        {change.isError && <ErrorNote error={change.error} />}
      </Form>
    </Dialog>
  );
}

// ---- Xoá ----

export function DeleteDialog({ name, onClose, onDone, onTrashAll }: { name: string; onClose: () => void; onDone: (name: string) => void; onTrashAll: () => void }) {
  const t = useStrings(strings);
  const qc = useQueryClient();
  const remove = useMutation({
    mutationFn: () => del<void>(userPath(name)),
    onSuccess: () => {
      refreshAccounts(qc);
      onDone(name);
      onClose();
    },
  });
  // user_not_empty: còn site hoặc DB (hoặc đang có job). Đưa nút làm trước việc đó.
  const notEmpty = remove.error instanceof ApiError && remove.error.code === "user_not_empty";
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={t.delTitle(name)} description={t.delDesc} confirmLabel={remove.isPending ? t.working : t.delConfirm} onConfirm={() => remove.mutate()} busy={remove.isPending} danger>
      {remove.isError && (
        <div className="flex flex-col gap-[12px]">
          <ErrorNote error={remove.error} />
          {notEmpty && (
            <>
              <p className="m-0 text-muted">{t.notEmptyHint}</p>
              <div>
                <Button onClick={onTrashAll}>{t.trashAll}</Button>
              </div>
            </>
          )}
        </div>
      )}
    </Dialog>
  );
}
