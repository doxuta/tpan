// Màn Tạo site (mục 10 của KE-HOACH.md, màn 4): nhập → xem trước → chạy.
// Ba ô: domain, tiêu đề (tuỳ chọn), email quản trị. "Xem trước" gọi
// POST /api/v1/sites?dry_run=1; có Preview thì nhúng PlanReview, và "chạy"
// gửi ĐÚNG nội dung đã xem trước (không phải nội dung trong ô lúc bấm).
// M6: có thêm chế độ "Dán danh sách" (DanhSach.tsx, gọi POST /api/v1/apply).
// Hai chế độ là hai thành phần riêng, chỉ một cái được vẽ tại một lúc: bản xem
// trước của hai bên đều là PlanReview (cùng id trong trang), nên không vẽ song
// song. Đổi chế độ là bắt đầu lại; đổi lại là mất chữ đã gõ, chấp nhận vì người
// dùng hiếm khi đổi giữa chừng.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useNavigate } from "react-router";
import { ApiError, post } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { JobStarted, Preview, SiteCreate } from "../../api/types";
import { Button } from "../../components/Button";
import { ScreenHeader } from "../../components/ScreenHeader";
import { SegmentButton } from "../../components/SegmentButton";
import { StatusMark } from "../../components/StatusMark";
import { useLang, useStrings, tx } from "../../i18n";
import { strings } from "../../i18n/tao-site";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";
import { DanhSach } from "./DanhSach";

type Mode = "one" | "list";

export default function TaoSite() {
  const t = useStrings(strings);
  const [mode, setMode] = useState<Mode>("one");
  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />
      <div className="pt-[22px]">
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

type Field = "domain" | "title" | "email";

// Mã lỗi của máy chủ ứng với ô nhập nào. Mã khác (stack chưa cài, job đang
// chạy, điều kiện chưa đạt...) không thuộc ô nào nên hiện cạnh nút.
const fieldOfCode: Record<string, Field> = {
  domain_invalid: "domain",
  site_exists_unmanaged: "domain",
  site_trashed: "domain",
  title_invalid: "title",
  email_invalid: "email",
};

const inputClass =
  "box-border h-[44px] w-full border border-ink bg-surface px-[10px] text-[14px] text-ink placeholder:text-muted disabled:border-hair disabled:text-muted sm:h-[40px]";

function OneSite() {
  const t = useStrings(strings);
  const { lang } = useLang();
  const qc = useQueryClient();
  const navigate = useNavigate();

  const [domain, setDomain] = useState("");
  const [title, setTitle] = useState("");
  const [email, setEmail] = useState("");
  /** Lỗi nhập thiếu, bắt ngay ở trình duyệt để khỏi gọi máy chủ chỉ để nghe "thiếu". */
  const [missing, setMissing] = useState<Field | null>(null);
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
  const edit = (set: (v: string) => void, field: Field) => (v: string) => {
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
    const body: SiteCreate = { domain: domain.trim(), title: title.trim(), email: email.trim() };
    const empty: Field | null = body.domain === "" ? "domain" : body.email === "" ? "email" : null;
    setMissing(empty);
    if (empty) {
      document.getElementById(`o-${empty}`)?.focus();
      return;
    }
    dry.mutate(body);
  }

  // Lỗi của lần xem trước, tách theo ô: câu lỗi hiện ngay dưới ô sai.
  const apiErr = dry.error instanceof ApiError ? dry.error : null;
  const errField: Field | undefined = apiErr ? fieldOfCode[apiErr.code] : undefined;
  const fieldError = (f: Field): string | null => {
    if (missing === f) return f === "domain" ? t.domainRequired : t.emailRequired;
    if (errField === f && apiErr?.body) return tx(apiErr.body.error.message, lang);
    return null;
  };

  const field = (f: Field, label: string, hint: string, value: string, set: (v: string) => void, extra: Record<string, unknown>) => {
    const err = fieldError(f);
    const describedBy = `h-${f}${err ? ` e-${f}` : ""}`;
    return (
      <div>
        <label htmlFor={`o-${f}`} className="mb-[4px] block font-medium">
          {label}
        </label>
        <p id={`h-${f}`} className="mt-0 mb-[6px] text-[13px] text-muted">
          {hint}
        </p>
        <input
          id={`o-${f}`}
          name={f}
          value={value}
          onChange={(e) => edit(set, f)(e.target.value)}
          aria-describedby={describedBy}
          aria-invalid={err ? true : undefined}
          autoComplete="off"
          className={inputClass}
          {...extra}
        />
        {err && (
          <div id={`e-${f}`} role="alert" className="mt-[8px] flex items-start gap-[10px]">
            <span className="flex h-[21px] w-[20px] flex-none items-center justify-center">
              <StatusMark status="fail" />
            </span>
            <span className="font-medium">{err}</span>
          </div>
        )}
      </div>
    );
  };

  return (
    <>
      <form onSubmit={onSubmit} noValidate aria-labelledby="moi" className="max-w-[560px]">
        <h2 id="moi" className="m-0 pt-[26px] pb-[14px] text-[16px] font-semibold">
          {t.formHeading}
        </h2>
        <div className="flex flex-col gap-[20px]">
          {field("domain", t.domainLabel, t.domainHint, domain, setDomain, {
            type: "text",
            inputMode: "url",
            spellCheck: false,
            autoCapitalize: "none",
            placeholder: "shop.example.com",
            className: `${inputClass} font-mono text-[13px]`,
          })}
          {field("title", t.titleLabel, t.titleHint, title, setTitle, { type: "text" })}
          {field("email", t.emailLabel, t.emailHint, email, setEmail, {
            type: "email",
            spellCheck: false,
            autoCapitalize: "none",
            placeholder: "admin@example.com",
            className: `${inputClass} font-mono text-[13px]`,
          })}
        </div>
        {apiErr && !errField && (
          <div className="pt-[16px]">
            <ErrorNote error={apiErr} heading={t.errorHeading} checksHeading={t.checksHeading} />
          </div>
        )}
        {apiErr && errField && (apiErr.body?.error.checks?.length ?? 0) > 0 && (
          <div className="pt-[16px]">
            <ErrorNote error={apiErr} omitMessage checksHeading={t.checksHeading} />
          </div>
        )}
        {!apiErr && dry.isError && (
          <div className="pt-[16px]">
            <ErrorNote error={dry.error} heading={t.errorHeading} />
          </div>
        )}
        <div className="pt-[20px]">
          <Button variant="primary" type="submit" disabled={dry.isPending}>
            {dry.isPending ? t.previewing : t.preview}
          </Button>
        </div>
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
