// Các hộp thoại của màn Người dùng: thêm hoặc sửa, đặt lý do tạm khoá, đổi mật
// khẩu, xoá. Mỗi hộp là một component riêng, chỉ được dựng khi đang mở, nên ô
// nhập tự về giá trị đầu mỗi lần mở. Việc đổi máy (tạm khoá, mở khoá, đưa vào
// thùng rác) KHÔNG chạy ở đây: hộp chỉ thu đầu vào rồi giao cho màn mở bản xem
// trước (PlanReview); thêm, sửa, đổi mật khẩu, xoá user chỉ ghi vào tpan.db
// nên không có job.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent, type ReactNode } from "react";
import { ApiError, del, errorMessage, post } from "../../api/client";
import { packagesKey, previewOf, refreshAccounts, useUsers, userPath, usersPath } from "../../api/m13-users";
import { useSiteCheck } from "../../api/site-check";
import { LiveCheck } from "../../components/LiveCheck";
import { StatusLine } from "../../components/StatusMark";
import type { JobStarted, PasswordSetResult, Role, UserCreated, UserRow } from "../../api/types";
import { useJobTracker } from "../../components/JobTracker";
import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { SelectField, TextField } from "../../components/Field";
import { Notice } from "../../components/Notice";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/nguoi-dung";
import { ErrorNote } from "../site/ErrorNote";
import type { Secret } from "./PasswordBox";
import { ReauthError, ReauthFields, useReauth } from "./Reauth";

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
  const { lang } = useLang();
  const qc = useQueryClient();
  const edit = row !== undefined;
  const [name, setName] = useState(row?.user.name ?? "");
  const [full, setFull] = useState(row?.user.full_name ?? "");
  const [email, setEmail] = useState(row?.user.email ?? "");
  const [role, setRole] = useState<Role>(row?.user.role ?? "user");
  const [pkg, setPkg] = useState(row?.user.package ?? "default");
  const [password, setPassword] = useState("");
  const [local, setLocal] = useState<string | null>(null);
  // Kiểm ngay lúc gõ: tên đã có user chưa (danh sách user admin đang xem), email hợp lệ (máy chủ).
  const all = useUsers(!edit);
  const nameTrim = name.trim();
  const nameTaken = !edit && nameTrim !== "" && (all.data ?? []).some((u) => u.user.name.toLowerCase() === nameTrim.toLowerCase());
  const liveEmail = useSiteCheck("", email).email;
  // Thêm user, hoặc nâng người chưa là admin lên admin: máy chủ hỏi lại mật khẩu (và mã hai bước) của admin đang thao tác.
  const promote = edit && role === "admin" && row.user.role !== "admin";
  const needReauth = !edit || promote;
  const [saveError, setSaveError] = useState<unknown>(null);
  const re = useReauth(saveError);

  const options = packages.includes(pkg) ? packages : [pkg, ...packages];

  // Chỉ gửi trường đã đổi (UserEdit: trường vắng là giữ nguyên); gửi cả vai không đổi thì vô ích và dễ chạm luật admin cuối.
  const editBody = () =>
    row && {
      ...(full !== row.user.full_name && { full_name: full }),
      ...(email !== row.user.email && { email }),
      ...(role !== row.user.role && { role }),
      ...(pkg !== row.user.package && { package: pkg }),
    };
  // M17: đổi gói của user có site trong lồng mà mức tối đa khác thì server chạy
  // ngay job cage.sync (202). Hộp hỏi bản xem trước (?dry_run=1) trước, báo số
  // bước, và chỉ chạy khi bấm lần hai; thẻ theo dõi bám job đó.
  const { track } = useJobTracker();
  const [planned, setPlanned] = useState<number | null>(null);
  const [checking, setChecking] = useState(false);

  const save = useMutation({
    mutationFn: (): Promise<UserCreated | UserRow | JobStarted> =>
      edit
        ? // Mật khẩu hỏi lại chỉ đi kèm lần lưu thật; bản xem trước (?dry_run=1) không cần nó.
          post<UserRow | JobStarted>(userPath(row.user.name), { ...editBody(), ...(needReauth && re.body()) })
        : post<UserCreated>(usersPath, { name: name.trim(), full_name: full, email, role, package: pkg, password: password || undefined, ...re.body() }),
    onSuccess: (res) => {
      refreshAccounts(qc);
      void qc.invalidateQueries({ queryKey: packagesKey });
      if ("job_id" in res) track(res.job_id);
      if (edit) onDone("edited", row.user.name, null);
      else {
        const made = res as UserCreated;
        onDone("added", made.user.user.name, made.password ? { name: made.user.user.name, password: made.password } : null);
      }
      onClose();
    },
    onError: (err) => {
      setSaveError(err);
      re.afterError(err);
    },
  });

  const submit = () => {
    if (save.isPending || checking) return;
    save.reset();
    if (!edit && name.trim() === "") return setLocal(t.nameMissing);
    if (nameTaken) return setLocal(t.liveNameTaken(nameTrim));
    if (liveEmail.status === "fail") return document.getElementById("u-email")?.focus();
    if (!edit && tooShort(password, t.minPassword)) return setLocal(t.passwordTooShort(t.minPassword));
    const missing = needReauth ? re.problem() : null;
    if (missing) return setLocal(missing);
    setLocal(null);
    if (edit && pkg !== row.user.package && planned === null) {
      setChecking(true);
      previewOf({ method: "POST", path: userPath(row.user.name), body: editBody() })
        .then((p) => {
          const n = p.steps.filter((s) => !s.done).length;
          if (n > 0) setPlanned(n);
          else save.mutate();
        })
        .catch((e: unknown) => setLocal(errorMessage(e, lang)))
        .finally(() => setChecking(false));
      return;
    }
    save.mutate();
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={edit ? t.editTitle(row.user.name) : t.addTitle} confirmLabel={save.isPending || checking ? t.saving : planned !== null ? t.cageSyncConfirm : edit ? t.editConfirm : t.addConfirm} onConfirm={submit} busy={save.isPending || checking}>
      <Form onSubmit={submit}>
        {!edit && <TextField id="u-ten" label={t.fName} hint={t.fNameHint} autoComplete="off" autoCapitalize="none" spellCheck={false} autoFocus required value={name} onChange={(e) => setName(e.target.value)} invalid={nameTaken} />}
        {!edit && nameTrim !== "" && (
          <p aria-live="polite" className="-mt-[8px] mb-0 text-[13px]">
            {nameTaken ? <StatusLine status="fail">{t.liveNameTaken(nameTrim)}</StatusLine> : all.data ? <StatusLine status="pass">{t.liveNameFree}</StatusLine> : null}
          </p>
        )}
        <TextField id="u-day-du" label={t.fFull} autoComplete="off" autoFocus={edit} value={full} onChange={(e) => setFull(e.target.value)} />
        <div>
          <TextField id="u-email" type="email" label={t.fEmail} autoComplete="off" value={email} onChange={(e) => setEmail(e.target.value)} invalid={liveEmail.status === "fail"} />
          <LiveCheck id="u-email-kiem" state={liveEmail} okText={t.liveEmailOk} checkingText={t.liveChecking} />
        </div>
        <SelectField id="u-vai" label={t.fRole} value={role} onChange={(e) => setRole(e.target.value as Role)}>
          <option value="user">{t.roleOptionUser}</option>
          <option value="admin">{t.roleOptionAdmin}</option>
        </SelectField>
        <SelectField
          id="u-goi"
          label={t.fPackage}
          value={pkg}
          onChange={(e) => {
            setPkg(e.target.value);
            setPlanned(null);
          }}
        >
          {options.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </SelectField>
        {!edit && <TextField id="u-mat-khau" type="password" label={t.fPassword} hint={t.fPasswordHint} autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />}
        {needReauth && <ReauthFields r={re} idPrefix="u" why={edit ? t.reauthWhyAdmin : t.reauthWhyCreate} error={save.error} />}
        {planned !== null && <Notice kind="warn">{t.cageSyncNote(pkg, planned)}</Notice>}
        {local && (
          <Notice kind="fail" role="alert">
            {local}
          </Notice>
        )}
        {save.isError && <ReauthError error={save.error} />}
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
  const [changeError, setChangeError] = useState<unknown>(null);
  const re = useReauth(changeError);
  const change = useMutation({
    mutationFn: () => post<PasswordSetResult>(userPath(name, "/password"), { password: password || undefined, ...re.body() }),
    onSuccess: (res) => {
      onDone(name, res.password ? { name, password: res.password } : null);
      onClose();
    },
    onError: (err) => {
      setChangeError(err);
      re.afterError(err);
    },
  });
  const submit = () => {
    if (change.isPending) return;
    change.reset();
    if (tooShort(password, t.minPassword)) return setLocal(t.passwordTooShort(t.minPassword));
    const missing = re.problem();
    if (missing) return setLocal(missing);
    setLocal(null);
    change.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={t.pwTitle(name)} description={t.pwDesc} confirmLabel={change.isPending ? t.saving : t.pwConfirm} onConfirm={submit} busy={change.isPending}>
      <Form onSubmit={submit}>
        <TextField id="u-mk-moi" type="password" label={t.fPassword} hint={t.fPasswordHint} autoComplete="new-password" autoFocus value={password} onChange={(e) => setPassword(e.target.value)} />
        <ReauthFields r={re} idPrefix="u-mk" why={t.reauthWhyPassword} error={change.error} />
        {local && (
          <Notice kind="fail" role="alert">
            {local}
          </Notice>
        )}
        {change.isError && <ReauthError error={change.error} />}
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
