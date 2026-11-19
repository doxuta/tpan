// Chế độ "Dán danh sách" của màn Tạo site (mục 7 của KE-HOACH.md): mỗi dòng một
// domain, một email quản trị chung, một công tắc SSL chung.
//
// Ba chặng như mọi thao tác: "Xem trước" gọi POST /api/v1/apply?dry_run=1 và
// hiện bảng việc của từng domain (tạo, đã có thì bỏ qua, không làm được và vì
// sao) cùng bản xem trước (PlanReview); "chạy" gửi ĐÚNG nội dung đã xem trước,
// không phải chữ trong ô lúc bấm, rồi chuyển tới /jobs/:id. Không gửi prune từ
// giao diện: xoá hàng loạt là việc của lệnh `tpan apply --prune`.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useNavigate } from "react-router";
import { ApiError, errorMessage, post } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { ApplyItem, ApplyPreview, ApplyRequest, JobStarted } from "../../api/types";
import { Button } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
import { StatusMark } from "../../components/StatusMark";
import { TableList } from "../../components/TableList";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/tao-site";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";

type Field = "list" | "email";

// Mã lỗi 400 của máy chủ ứng với ô nào. Mã khác (nothing_to_do, job đang chạy...)
// không thuộc ô nào nên hiện cạnh nút.
const fieldOfCode: Record<string, Field> = {
  domain_invalid: "list",
  sites_file_invalid: "list",
  title_invalid: "list",
  email_invalid: "email",
};

const inputClass =
  "box-border w-full border border-ink bg-surface px-[10px] text-[14px] text-ink placeholder:text-muted disabled:border-hair disabled:text-muted";

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
  /** Thiếu ô nào, bắt ngay ở trình duyệt. */
  const [missing, setMissing] = useState<Field | null>(null);
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
  const changed = (field?: Field) => {
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
    const empty: Field | null = domains.length === 0 ? "list" : mail === "" ? "email" : null;
    setMissing(empty);
    if (empty) {
      document.getElementById(empty === "list" ? "o-list" : "o-list-email")?.focus();
      return;
    }
    dry.mutate({ defaults: { admin_email: mail, ssl }, sites: domains.map((domain) => ({ domain })) });
  }

  // Lỗi của lần xem trước: lỗi thuộc một ô thì hiện ngay dưới ô đó.
  const apiErr = dry.error instanceof ApiError ? dry.error : null;
  const errField: Field | undefined = apiErr ? fieldOfCode[apiErr.code] : undefined;
  const fieldError = (f: Field): string | null => {
    if (missing === f) return f === "list" ? t.listRequired : t.emailRequired;
    if (errField === f && dry.error) return errorMessage(dry.error, lang);
    return null;
  };
  const errorLine = (f: Field, text: string | null) =>
    text && (
      <div id={`e-list-${f}`} role="alert" className="mt-[8px] flex items-start gap-[10px]">
        <span className="flex h-[21px] w-[20px] flex-none items-center justify-center">
          <StatusMark status="fail" />
        </span>
        <span className="font-medium">{text}</span>
      </div>
    );
  const listErr = fieldError("list");
  const emailErr = fieldError("email");

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
      <form onSubmit={onSubmit} noValidate aria-labelledby="moi-ds" className="max-w-[560px]">
        <h2 id="moi-ds" className="m-0 pt-[26px] pb-[14px] text-[16px] font-semibold">
          {t.listHeading}
        </h2>
        <div className="flex flex-col gap-[20px]">
          <div>
            <label htmlFor="o-list" className="mb-[4px] block font-medium">
              {t.listLabel}
            </label>
            <p id="h-list" className="mt-0 mb-[6px] text-[13px] text-muted">
              {t.listHint}
            </p>
            <textarea
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
              className={`${inputClass} min-h-[160px] resize-y py-[8px] font-mono text-[13px]`}
            />
            {errorLine("list", listErr)}
          </div>
          <div>
            <label htmlFor="o-list-email" className="mb-[4px] block font-medium">
              {t.emailLabel}
            </label>
            <p id="h-list-email" className="mt-0 mb-[6px] text-[13px] text-muted">
              {t.listEmailHint}
            </p>
            <input
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
              className={`${inputClass} h-[44px] font-mono text-[13px] sm:h-[40px]`}
            />
            {errorLine("email", emailErr)}
          </div>
          <div>
            <button
              type="button"
              role="switch"
              aria-checked={ssl}
              aria-describedby="h-list-ssl"
              onClick={() => {
                setSsl(!ssl);
                changed();
              }}
              className="inline-flex h-[44px] items-center border border-ink bg-surface px-[14px] text-[14px] font-medium text-ink transition-colors hover:bg-pre sm:h-[40px]"
            >
              {t.sslSwitch(ssl)}
            </button>
            <p id="h-list-ssl" className="mt-[6px] mb-0 text-[13px] text-muted">
              {t.sslHint}
            </p>
          </div>
        </div>
        {apiErr && !errField && (
          <div className="pt-[16px]">
            <ErrorNote error={apiErr} heading={apiErr.code === "nothing_to_do" ? t.nothingHeading : t.errorHeading} checksHeading={t.checksHeading} />
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

      {result && sent && (
        <div ref={resultRef} tabIndex={-1} className="outline-offset-0">
          <section aria-labelledby="viec-ds" className="pt-[26px]">
            <h2 id="viec-ds" className="m-0 text-[16px] font-semibold">
              {t.itemsHeading}
            </h2>
            <p className="mt-[6px] mb-[14px] text-muted">{t.itemsSummary(counts.create, counts.skip, counts.refused)}</p>
            <TableList
              caption={t.itemsTable}
              rows={result.items}
              rowKey={(i) => i.domain}
              empty={t.nothingToDo}
              columns={[
                { header: t.colDomain, className: "min-w-[200px]", cell: (i) => <MonoText>{i.domain}</MonoText> },
                {
                  header: t.colAction,
                  className: "whitespace-nowrap",
                  cell: (i) => (
                    <span className="inline-flex items-center gap-[8px]">
                      {/* Dấu lỗi chỉ cho dòng không làm được: "tạo" hay "bỏ qua" chưa phải tình trạng của máy. */}
                      {i.action === "refused" && (
                        <span className="flex h-[20px] w-[20px] items-center justify-center">
                          <StatusMark status="fail" />
                        </span>
                      )}
                      {t.actions[i.action]}
                    </span>
                  ),
                },
                { header: t.colReason, cell: (i) => <span className="text-muted [overflow-wrap:anywhere]">{tx(i.reason, lang)}</span> },
              ]}
            />
          </section>
          {result.preview ? (
            <PlanReview preview={result.preview} onRun={() => run.mutate(sent)} running={run.isPending} error={run.error} onCancel={close} />
          ) : (
            // Không có gì để tạo: nói rõ, không hiện nút chạy. Bảng ở trên nói vì sao từng dòng.
            result.items.length > 0 && (
              <div className="pt-[16px]">
                <p role="status" className="m-0 font-medium">
                  {t.nothingToDo}
                </p>
              </div>
            )
          )}
        </div>
      )}
    </>
  );
}
