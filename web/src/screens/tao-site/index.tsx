// Màn Tạo site (mục 10 của KE-HOACH.md, màn 4): nhập → xem trước → chạy.
// Ba ô: domain, tiêu đề (tuỳ chọn), email quản trị. "Xem trước" gọi
// POST /api/v1/sites?dry_run=1; có Preview thì nhúng PlanReview, và "chạy"
// gửi ĐÚNG nội dung đã xem trước (không phải nội dung trong ô lúc bấm).
// M6: có thêm chế độ "Dán danh sách" (DanhSach.tsx, gọi POST /api/v1/apply).
// Hai chế độ là hai thành phần riêng, chỉ một cái được vẽ tại một lúc: bản xem
// trước của hai bên đều là PlanReview (cùng id trong trang), nên không vẽ song
// song. Đổi chế độ là bắt đầu lại; đổi lại là mất chữ đã gõ, chấp nhận vì người
// dùng hiếm khi đổi giữa chừng.
//
// Bản 2: đầu màn có đường dẫn "Site / Tạo site"; form nằm trong một thẻ rộng
// tối đa 560 px. Giữ nguyên id các ô (#o-domain, #o-title, #o-email, #o-owner)
// và nút gửi là `form button[type=submit]`: script chụp và e2e dùng.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type FormEvent, type InputHTMLAttributes, type ReactNode } from "react";
import { useNavigate } from "react-router";
import { ApiError, post } from "../../api/client";
import { useIdentity } from "../../api/m13-dbs";
import { useUsers } from "../../api/m13-users";
import { queryKeys } from "../../api/queries";
import { useSiteCheck } from "../../api/site-check";
import type { JobStarted, Preview, SiteCreate } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Field, Input, Select } from "../../components/Field";
import { LiveCheck } from "../../components/LiveCheck";
import { ScreenHeader } from "../../components/ScreenHeader";
import { SegmentButton } from "../../components/SegmentButton";
import { useLang, useStrings, tx } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/tao-site";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";
import { DanhSach } from "./DanhSach";

type Mode = "one" | "list";

export default function TaoSite() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const [mode, setMode] = useState<Mode>("one");
  return (
    <>
      <ScreenHeader crumbs={[{ label: k.menu.sites, to: "/sites" }, { label: t.title }]} title={t.title} summary={t.summary} />
      <div>
        <SegmentButton<Mode>
          label={t.modeGroup}
          value={mode}
          onChange={setMode}
          options={[
            { value: "one", label: t.modeOne },
            { value: "list", label: t.modeList },
          ]}
        />
      </div>
      {mode === "one" ? <OneSite /> : <DanhSach />}
    </>
  );
}

type FieldName = "domain" | "title" | "email" | "owner";

// Mã lỗi của máy chủ ứng với ô nhập nào. Mã khác (stack chưa cài, job đang
// chạy, điều kiện chưa đạt...) không thuộc ô nào nên hiện cạnh nút.
const fieldOfCode: Record<string, FieldName> = {
  domain_invalid: "domain",
  site_exists_unmanaged: "domain",
  site_trashed: "domain",
  title_invalid: "title",
  email_invalid: "email",
  // M13: chủ không có hoặc đang bị khoá. Hạn mức gói (quota_*) không thuộc ô nào nên hiện cạnh nút, kèm câu từ chối của máy chủ.
  user_not_found: "owner",
  user_suspended: "owner",
};

function OneSite() {
  const t = useStrings(strings);
  const { lang } = useLang();
  const qc = useQueryClient();
  const navigate = useNavigate();

  const [domain, setDomain] = useState("");
  const [title, setTitle] = useState("");
  const [email, setEmail] = useState("");
  // M13: chỉ admin chọn chủ. Rỗng là chính mình; user thường không có ô này và máy chủ tự đặt chủ là người gọi.
  const { isAdmin, name: me } = useIdentity();
  const users = useUsers(isAdmin);
  const [ownerSel, setOwnerSel] = useState("");
  const owner = ownerSel || me;
  const owners = (users.data ?? []).filter((u) => u.user.status === "active").map((u) => u.user.name);
  // Kiểm ngay lúc gõ: domain hợp lệ và chưa ai dùng, email hợp lệ (GET /api/v1/site-check).
  const live = useSiteCheck(domain, email);
  /** Lỗi nhập thiếu, bắt ngay ở trình duyệt để khỏi gọi máy chủ chỉ để nghe "thiếu". */
  const [missing, setMissing] = useState<FieldName | null>(null);
  /** Nội dung đã gửi đi xem trước. "Chạy" gửi đúng bản này. */
  const [sent, setSent] = useState<SiteCreate | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const previewRef = useRef<HTMLDivElement>(null);

  const dry = useMutation({
    mutationFn: (body: SiteCreate) => post<Preview>("/api/v1/sites?dry_run=1", body),
    onSuccess: (p, body) => {
      setSent(body);
      setPreview(p);
    },
    onError: (err) => {
      const f = err instanceof ApiError ? fieldOfCode[err.code] : undefined;
      if (f) document.getElementById(`o-${f}`)?.focus();
    },
  });
  const run = useMutation({
    mutationFn: (body: SiteCreate) => post<JobStarted>("/api/v1/sites", body),
    onSuccess: (job) => {
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      void navigate(`/jobs/${encodeURIComponent(job.job_id)}`);
    },
  });

  useEffect(() => {
    if (preview) {
      previewRef.current?.scrollIntoView({ block: "start" });
      previewRef.current?.focus();
    }
  }, [preview]);

  // Sửa ô nào thì bản xem trước cũ hết đúng: bỏ nó, người dùng xem trước lại.
  const edit = (set: (v: string) => void, field: FieldName) => (v: string) => {
    set(v);
    if (missing === field) setMissing(null);
    if (preview) {
      setPreview(null);
      setSent(null);
      run.reset();
    }
    dry.reset();
  };

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    const body: SiteCreate = { domain: domain.trim(), title: title.trim(), email: email.trim(), ...(isAdmin && owner !== me ? { owner } : {}) };
    const empty: FieldName | null = body.domain === "" ? "domain" : body.email === "" ? "email" : null;
    setMissing(empty);
    if (empty) {
      document.getElementById(`o-${empty}`)?.focus();
      return;
    }
    // Ô đang báo sai thì khỏi gọi xem trước: câu lỗi đã nằm ngay dưới ô.
    const bad = live.domain.status === "fail" ? "domain" : live.email.status === "fail" ? "email" : null;
    if (bad) {
      document.getElementById(`o-${bad}`)?.focus();
      return;
    }
    dry.mutate(body);
  }

  // Lỗi của lần xem trước, tách theo ô: câu lỗi hiện ngay dưới ô sai.
  const apiErr = dry.error instanceof ApiError ? dry.error : null;
  const errField: FieldName | undefined = apiErr ? fieldOfCode[apiErr.code] : undefined;
  const fieldError = (f: FieldName): string | null => {
    if (missing === f) return f === "domain" ? t.domainRequired : t.emailRequired;
    if (errField === f && apiErr?.body) return tx(apiErr.body.error.message, lang);
    return null;
  };

  // Một ô một dòng: nhãn, ô, gợi ý, câu lỗi. Câu lỗi mang id e-<ô> để ô trỏ aria-describedby tới.
  const field = (f: Exclude<FieldName, "owner">, label: string, hint: string, value: string, set: (v: string) => void, extra: Omit<InputHTMLAttributes<HTMLInputElement>, "className">, after?: ReactNode) => {
    const err = fieldError(f);
    const liveId = after ? ` l-${f}` : "";
    return (
      <Field label={label} htmlFor={`o-${f}`} hint={hint} hintId={`h-${f}`} error={err && <span id={`e-${f}`}>{err}</span>}>
        <Input
          id={`o-${f}`}
          name={f}
          value={value}
          onChange={(e) => edit(set, f)(e.target.value)}
          aria-describedby={`h-${f}${err ? ` e-${f}` : ""}${liveId}`}
          aria-invalid={err ? true : undefined}
          autoComplete="off"
          {...extra}
        />
        {after}
      </Field>
    );
  };

  const ownerErr = fieldError("owner");

  return (
    <>
      <form onSubmit={onSubmit} noValidate className="max-w-[560px]">
        <Card title={t.formHeading} titleId="moi">
          <div className="flex flex-col gap-[20px]">
            {field("domain", t.domainLabel, t.domainHint, domain, setDomain, {
              type: "text",
              inputMode: "url",
              spellCheck: false,
              autoCapitalize: "none",
              placeholder: "shop.example.com",
            }, <LiveCheck id="l-domain" state={live.domain} okText={t.liveDomainOk} checkingText={t.liveChecking} />)}
            {field("title", t.titleLabel, t.titleHint, title, setTitle, { type: "text" })}
            {field("email", t.emailLabel, t.emailHint, email, setEmail, {
              type: "email",
              spellCheck: false,
              autoCapitalize: "none",
              placeholder: "admin@example.com",
            }, <LiveCheck id="l-email" state={live.email} okText={t.liveEmailOk} checkingText={t.liveChecking} />)}
            {isAdmin && (
              <Field label={t.ownerLabel} htmlFor="o-owner" hint={t.ownerHint} hintId="h-owner" error={ownerErr && <span id="e-owner">{ownerErr}</span>}>
                <Select
                  id="o-owner"
                  name="owner"
                  value={owner}
                  onChange={(e) => edit(setOwnerSel, "owner")(e.target.value)}
                  aria-describedby={`h-owner${ownerErr ? " e-owner" : ""}`}
                  aria-invalid={ownerErr ? true : undefined}
                >
                  {/* Danh sách chưa tải xong hoặc lỗi: vẫn có đúng một lựa chọn là chính mình, nên tạo site cho mình không bị chặn. */}
                  {!owners.includes(me) && <option value={me}>{t.ownerSelf(me)}</option>}
                  {owners.map((n) => (
                    <option key={n} value={n}>
                      {n === me ? t.ownerSelf(n) : n}
                    </option>
                  ))}
                </Select>
              </Field>
            )}
            {apiErr && !errField && <ErrorNote error={apiErr} heading={t.errorHeading} checksHeading={t.checksHeading} />}
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

      {preview && sent && (
        <div ref={previewRef} tabIndex={-1} className="outline-offset-0">
          <PlanReview
            preview={preview}
            onRun={() => run.mutate(sent)}
            running={run.isPending}
            error={run.error}
            onCancel={() => {
              setPreview(null);
              setSent(null);
              run.reset();
              document.getElementById("o-domain")?.focus();
            }}
          />
        </div>
      )}
    </>
  );
}
