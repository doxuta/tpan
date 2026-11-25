// Chế độ "Dán danh sách" của màn Tạo site (mục 7 của KE-HOACH.md): mỗi dòng một
// domain, một email quản trị chung, một ô chọn SSL chung, và (admin) một chủ
// chung cho cả lô, gửi trong defaults.owner như owner của sites.yaml.
//
// Ba chặng như mọi thao tác: "Xem trước" gọi POST /api/v1/apply?dry_run=1 và
// hiện bảng việc của từng domain (tạo, đã có thì bỏ qua, không làm được và vì
// sao) cùng bản xem trước (PlanReview); "chạy" gửi ĐÚNG nội dung đã xem trước,
// không phải chữ trong ô lúc bấm, rồi chuyển tới /jobs/:id. Không gửi prune từ
// giao diện: xoá hàng loạt là việc của lệnh `tpan apply --prune`.
// Bản 2: form trong một thẻ, bảng việc là TableList (tự vẽ thẻ).

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useNavigate } from "react-router";
import { ApiError, errorMessage, post } from "../../api/client";
import { useIdentity } from "../../api/m13-dbs";
import { useUsers } from "../../api/m13-users";
import { queryKeys } from "../../api/queries";
import type { ApplyItem, ApplyPreview, ApplyRequest, JobStarted } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Checkbox, Field, Input, Select, Textarea } from "../../components/Field";
import { StatusLine } from "../../components/StatusMark";
import { TableList } from "../../components/TableList";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/tao-site";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";

type FieldName = "list" | "email" | "owner";

// Mã lỗi 400 của máy chủ ứng với ô nào. Mã khác (nothing_to_do, job đang chạy...)
// không thuộc ô nào nên hiện cạnh nút.
const fieldOfCode: Record<string, FieldName> = {
  domain_invalid: "list",
  sites_file_invalid: "list",
  title_invalid: "list",
  email_invalid: "email",
  // Chủ không có hoặc đang bị khoá. Hạn mức gói (quota_*) hiện ở bảng việc của từng domain.
  user_not_found: "owner",
  user_suspended: "owner",
};

/** Mỗi dòng một domain: bỏ khoảng trắng hai đầu và dòng trống. Kiểm domain đúng sai là việc của máy chủ. */
function domainsOf(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter((l) => l !== "");
}

export function DanhSach() {
  const t = useStrings(strings);
  const { lang } = useLang();
  const qc = useQueryClient();
  const navigate = useNavigate();

  const [list, setList] = useState("");
  const [email, setEmail] = useState("");
  const [ssl, setSsl] = useState(false);
  // Chủ của cả lô (chỉ admin chọn được); mặc định là chính mình như tab Một site.
  const { isAdmin, name: me } = useIdentity();
  const users = useUsers(isAdmin);
  const [ownerSel, setOwnerSel] = useState("");
  const owner = ownerSel || me;
  const owners = (users.data ?? []).filter((u) => u.user.status === "active").map((u) => u.user.name);
  /** Thiếu ô nào, bắt ngay ở trình duyệt. */
  const [missing, setMissing] = useState<FieldName | null>(null);
  /** Nội dung đã gửi đi xem trước. "Chạy" gửi đúng bản này. */
  const [sent, setSent] = useState<ApplyRequest | null>(null);
  const [result, setResult] = useState<ApplyPreview | null>(null);
  const resultRef = useRef<HTMLDivElement>(null);

  const dry = useMutation({
    mutationFn: (body: ApplyRequest) => post<ApplyPreview>("/api/v1/apply?dry_run=1", body),
    onSuccess: (r, body) => {
      setSent(body);
      setResult(r);
    },
    onError: (err) => {
      const f = err instanceof ApiError ? fieldOfCode[err.code] : undefined;
      document.getElementById(f === "email" ? "o-list-email" : "o-list")?.focus();
    },
  });
  const run = useMutation({
    mutationFn: (body: ApplyRequest) => post<JobStarted>("/api/v1/apply", body),
    onSuccess: (job) => {
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      void navigate(`/jobs/${encodeURIComponent(job.job_id)}`);
    },
  });

  useEffect(() => {
    if (result) {
      resultRef.current?.scrollIntoView({ block: "start" });
      resultRef.current?.focus();
    }
  }, [result]);

  // Sửa gì thì bản xem trước cũ hết đúng: bỏ nó, người dùng xem trước lại.
  const changed = (field?: FieldName) => {
    if (field && missing === field) setMissing(null);
    if (result) {
      setResult(null);
      setSent(null);
      run.reset();
    }
    dry.reset();
  };

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    const domains = domainsOf(list);
    const mail = email.trim();
    const empty: FieldName | null = domains.length === 0 ? "list" : mail === "" ? "email" : null;
    setMissing(empty);
    if (empty) {
      document.getElementById(empty === "list" ? "o-list" : "o-list-email")?.focus();
      return;
    }
    dry.mutate({ defaults: { admin_email: mail, ssl, ...(isAdmin && owner !== me ? { owner } : {}) }, sites: domains.map((domain) => ({ domain })) });
  }

  // Lỗi của lần xem trước: lỗi thuộc một ô thì hiện ngay dưới ô đó.
  const apiErr = dry.error instanceof ApiError ? dry.error : null;
  const errField: FieldName | undefined = apiErr ? fieldOfCode[apiErr.code] : undefined;
  const fieldError = (f: FieldName): string | null => {
    if (missing === f) return f === "list" ? t.listRequired : t.emailRequired;
    if (errField === f && dry.error) return errorMessage(dry.error, lang);
    return null;
  };
  const listErr = fieldError("list");
  const emailErr = fieldError("email");
  const ownerErr = fieldError("owner");

  const n = (a: ApplyItem["action"]) => result?.items.filter((i) => i.action === a).length ?? 0;
  const counts = { create: n("create"), skip: n("skip") + n("trash"), refused: n("refused") };

  const close = () => {
    setResult(null);
    setSent(null);
    run.reset();
    document.getElementById("o-list")?.focus();
  };

  return (
    <>
      <form onSubmit={onSubmit} noValidate className="max-w-[560px]">
        <Card title={t.listHeading} titleId="moi-ds">
          <div className="flex flex-col gap-[20px]">
            <Field label={t.listLabel} htmlFor="o-list" hint={t.listHint} hintId="h-list" error={listErr && <span id="e-list-list">{listErr}</span>}>
              <Textarea
                id="o-list"
                name="domains"
                value={list}
                onChange={(e) => {
                  setList(e.target.value);
                  changed("list");
                }}
                rows={8}
                aria-describedby={`h-list${listErr ? " e-list-list" : ""}`}
                aria-invalid={listErr ? true : undefined}
                spellCheck={false}
                autoCapitalize="none"
                autoComplete="off"
                placeholder={"shop.example.com\nblog.example.com"}
                className="min-h-[160px] resize-y"
              />
            </Field>
            <Field label={t.emailLabel} htmlFor="o-list-email" hint={t.listEmailHint} hintId="h-list-email" error={emailErr && <span id="e-list-email">{emailErr}</span>}>
              <Input
                id="o-list-email"
                name="email"
                type="email"
                value={email}
                onChange={(e) => {
                  setEmail(e.target.value);
                  changed("email");
                }}
                aria-describedby={`h-list-email${emailErr ? " e-list-email" : ""}`}
                aria-invalid={emailErr ? true : undefined}
                spellCheck={false}
                autoCapitalize="none"
                autoComplete="off"
                placeholder="admin@example.com"
              />
            </Field>
            {isAdmin && (
              <Field label={t.ownerLabel} htmlFor="o-list-owner" hint={t.listOwnerHint} hintId="h-list-owner" error={ownerErr && <span id="e-list-owner">{ownerErr}</span>}>
                <Select
                  id="o-list-owner"
                  name="owner"
                  value={owner}
                  onChange={(e) => {
                    setOwnerSel(e.target.value);
                    changed("owner");
                  }}
                  aria-describedby={`h-list-owner${ownerErr ? " e-list-owner" : ""}`}
                  aria-invalid={ownerErr ? true : undefined}
                >
                  {/* Danh sách user chưa tải xong hoặc lỗi: vẫn có đúng một lựa chọn là chính mình. */}
                  {!owners.includes(me) && <option value={me}>{t.ownerSelf(me)}</option>}
                  {owners.map((n) => (
                    <option key={n} value={n}>
                      {n === me ? t.ownerSelf(n) : n}
                    </option>
                  ))}
                </Select>
              </Field>
            )}
            <label className="flex items-start gap-[10px]">
              <span className="flex pt-[2px]">
                <Checkbox
                  label={t.sslLabel}
                  checked={ssl}
                  aria-describedby="h-list-ssl"
                  onChange={() => {
                    setSsl(!ssl);
                    changed();
                  }}
                />
              </span>
              <span className="min-w-0">
                <span className="block font-medium">{t.sslLabel}</span>
                <span id="h-list-ssl" className="block text-[13px] text-muted">
                  {t.sslHint}
                </span>
              </span>
            </label>
            {apiErr && !errField && <ErrorNote error={apiErr} heading={apiErr.code === "nothing_to_do" ? t.nothingHeading : t.errorHeading} checksHeading={t.checksHeading} />}
            {apiErr && errField && (apiErr.body?.error.checks?.length ?? 0) > 0 && <ErrorNote error={apiErr} omitMessage checksHeading={t.checksHeading} />}
            {!apiErr && dry.isError && <ErrorNote error={dry.error} heading={t.errorHeading} />}
            <div>
              <Button variant="primary" type="submit" disabled={dry.isPending}>
                {dry.isPending ? t.previewing : t.preview}
              </Button>
            </div>
          </div>
        </Card>
      </form>

      {result && sent && (
        <div ref={resultRef} tabIndex={-1} className="flex flex-col gap-[20px] outline-offset-0">
          <TableList
            title={t.itemsHeading}
            titleId="viec-ds"
            caption={t.itemsTable}
            minWidth={560}
            above={<p className="m-0 max-w-[72ch] px-[16px] py-[12px] text-muted">{t.itemsSummary(counts.create, counts.skip, counts.refused)}</p>}
            rows={result.items}
            rowKey={(i) => i.domain}
            empty={t.nothingToDo}
            columns={[
              { header: t.colDomain, nowrap: false, className: "min-w-[200px] [overflow-wrap:anywhere]", cell: (i) => i.domain },
              {
                header: t.colAction,
                // Dấu lỗi chỉ cho dòng không làm được: "tạo" hay "bỏ qua" chưa phải tình trạng của máy.
                cell: (i) => (i.action === "refused" ? <StatusLine status="fail">{t.actions[i.action]}</StatusLine> : t.actions[i.action]),
              },
              { header: t.colReason, nowrap: false, cell: (i) => <span className="text-muted [overflow-wrap:anywhere]">{tx(i.reason, lang)}</span> },
            ]}
          />
          {result.preview ? (
            <PlanReview preview={result.preview} onRun={() => run.mutate(sent)} running={run.isPending} error={run.error} onCancel={close} />
          ) : (
            // Không có gì để tạo: nói rõ, không hiện nút chạy. Bảng ở trên nói vì sao từng dòng.
            result.items.length > 0 && (
              <Card>
                <p role="status" className="m-0 font-medium">
                  {t.nothingToDo}
                </p>
              </Card>
            )
          )}
        </div>
      )}
    </>
  );
}
