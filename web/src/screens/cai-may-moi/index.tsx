// Trang "Cài lên máy ảo mới" (/tools/new-server, chỉ admin; mốc M18, mục 3.9
// và 3.11 của docs/thay-doi/M18-thiet-ke.md). Một form sinh user-data
// cloud-init cho một máy chưa tồn tại: máy mới tự tải đúng file chạy đã ghim
// sha256, cài TPAN, stack và các site đã chọn ở lần khởi động đầu.
//
// Không có bản xem trước: POST /api/v1/cloud-init/render không đổi gì trên
// máy này. Nút chính nằm cuối form (như màn Tạo site) vì người dùng điền từ
// trên xuống, danh sách site có thể dài.
//
// BÍ MẬT: admin_password chỉ có một lần trong câu trả lời. Nó chỉ nằm trong
// state của component: useMutation (không useQuery, nên không có queryKey),
// gcTime 0 và reset() ngay khi đã chép sang state, để MutationCache không giữ
// bản nào; không localStorage, không địa chỉ trang, không console. Rời trang là
// mất; đường dự phòng là `sudo tpan admin passwd` trên máy mới.
//
// Id ổn định cho e2e: form#ci-form, #ci-private, #ci-base, #ci-sha-amd64,
// #ci-sha-arm64, #ci-site-<thứ tự>, #ci-email, nút gửi `#ci-form button[type=submit]`.

import { useMutation } from "@tanstack/react-query";
import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { ApiError, post } from "../../api/client";
import { cloudInitRenderPath, type CloudInitRequest, type CloudInitResponse } from "../../api/m18-types";
import { useOverview, useSession } from "../../api/queries";
import { Button } from "../../components/Button";
import { Card, cardClass } from "../../components/Card";
import { CommandBlock } from "../../components/CommandBlock";
import { Checkbox, Field, Input } from "../../components/Field";
import { Code } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { SegmentButton } from "../../components/SegmentButton";
import { useCopy } from "../../components/useCopy";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/cai-may-moi";
import { strings as khung } from "../../i18n/khung";
import { ErrorNote } from "../site/ErrorNote";
import { buildRequest, defaultMaxBytes, groupDigits, normBase, problems, sumProblem, sumsCommand, type Form } from "./model";

type Web = Form["web"];
type Problem = ReturnType<typeof problems>[number];

const inputId: Record<Problem, string> = { amd64: "ci-sha-amd64", arm64: "ci-sha-arm64", email: "ci-email" };
const groupLabel = "mb-[6px] text-[14px] font-medium";
/** User quản trị mà lần khởi động đầu đặt mật khẩu (store.adminName); là dữ liệu, không dịch. */
const adminUser = "admin";

/** Kết quả của lần sinh gần nhất; withSites để biết có nhắc file mật khẩu WordPress không. */
type Result = { res: CloudInitResponse; withSites: boolean };

export default function CaiMayMoi() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const version = useSession().data?.version;
  const overview = useOverview().data;
  const active = overview?.sites.filter((r) => r.site.status === "active").map((r) => r.site.domain);

  const [web, setWeb] = useState<Web>("nginx");
  const [priv, setPriv] = useState(false);
  const [base, setBase] = useState("");
  const [amd64, setAmd64] = useState("");
  const [arm64, setArm64] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [email, setEmail] = useState("");
  /** Đã bấm gửi ít nhất một lần: từ đó mới hiện lỗi dưới ô, và lỗi tự mất khi sửa đúng. */
  const [tried, setTried] = useState(false);
  const [result, setResult] = useState<Result | null>(null);
  const [pwHidden, setPwHidden] = useState(false);

  const gen = useMutation({
    mutationFn: (body: CloudInitRequest) => post<CloudInitResponse>(cloudInitRenderPath, body),
    gcTime: 0,
  });

  // Site đã chọn theo thứ tự trên trang; site vừa thôi chạy (tổng quan tải lại) tự rơi khỏi danh sách.
  const form: Form = { web, private: priv, base, amd64, arm64, domains: (active ?? []).filter((d) => picked.includes(d)), email };
  const bad = tried ? problems(form) : [];

  // Sửa ô nào thì lỗi của lần gửi trước hết đúng. Kết quả đã sinh thì giữ: mật khẩu chưa chép mà mất là phải sinh lại.
  const edit =
    <V,>(set: (v: V) => void) =>
    (v: V) => {
      set(v);
      if (gen.isError) gen.reset();
    };

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    setTried(true);
    const p = problems(form);
    if (p.length > 0) {
      document.getElementById(inputId[p[0]])?.focus();
      return;
    }
    const body = buildRequest(form);
    gen.mutate(body, {
      onSuccess: (res) => {
        setResult({ res, withSites: body.sites !== null });
        setPwHidden(false);
        // Câu trả lời đã nằm trong state: bỏ bản trong MutationCache ngay (gcTime 0 thì nó bị xoá liền).
        gen.reset();
      },
    });
  }

  const sumError = (raw: string, which: "amd64" | "arm64") => {
    if (!bad.includes(which)) return null;
    return sumProblem(raw) === "required" ? t.sumRequired : t.sumInvalid(raw.trim().length);
  };

  const apiErr = gen.error instanceof ApiError ? gen.error : null;

  return (
    <>
      <ScreenHeader crumbs={[{ label: k.menu.tools, to: "/tools" }, { label: t.title }]} title={t.title} summary={t.summary} />

      <form id="ci-form" onSubmit={onSubmit} noValidate className="max-w-[560px]">
        <Card title={t.formHeading} titleId="ci-heading">
          <div className="flex flex-col gap-[20px]">
            <div>
              <div className={groupLabel}>{t.webLabel}</div>
              <SegmentButton<Web>
                label={t.webLabel}
                value={web}
                onChange={edit(setWeb)}
                options={[
                  { value: "nginx", label: t.webNginx },
                  { value: "apache", label: t.webApache },
                ]}
              />
            </div>

            <label className="flex items-start gap-[10px]">
              <span className="flex pt-[2px]">
                <Checkbox id="ci-private" label={t.privateLabel} checked={priv} aria-describedby="ci-private-hint" onChange={(e) => edit(setPriv)(e.target.checked)} />
              </span>
              <span className="min-w-0">
                <span className="block font-medium">{t.privateLabel}</span>
                <span id="ci-private-hint" className="block text-[13px] text-muted">
                  {t.privateHint} <Code>{t.privateCmd}</Code> {t.privateOff}
                </span>
              </span>
            </label>

            <Field label={t.baseLabel} htmlFor="ci-base" hint={t.baseHint(version || t.versionUnknown)} hintId="ci-base-hint">
              <Input
                id="ci-base"
                mono
                type="url"
                inputMode="url"
                spellCheck={false}
                autoCapitalize="none"
                autoComplete="off"
                value={base}
                onChange={(e) => edit(setBase)(e.target.value)}
                aria-describedby="ci-base-hint"
              />
            </Field>

            <SumField id="ci-sha-amd64" label={t.sumAmdLabel} hint={t.sumHint} value={amd64} onChange={edit(setAmd64)} error={sumError(amd64, "amd64")} />
            <SumField id="ci-sha-arm64" label={t.sumArmLabel} hint={t.sumHint} value={arm64} onChange={edit(setArm64)} error={sumError(arm64, "arm64")} />
            {/* Giao diện không tự tải SHA256SUMS (CSP default-src 'self', và máy chủ cũng không tải): người dùng tự lấy trên máy của họ. */}
            {/* Base trống: lệnh mang chữ giữ chỗ <base> (luật 7 cấm viết địa chỉ https:// mặc định trong mã), nên dòng đầu khối nói phải thay. */}
            <CommandBlock commands={[sumsCommand(base)]} addSudo={false} where={normBase(base) === "" ? t.sumsWhereEmpty : t.sumsWhere} />

            <fieldset className="m-0 min-w-0 border-0 p-0">
              <legend className={`${groupLabel} p-0`}>{t.sitesLegend}</legend>
              <p id="ci-sites-hint" className="m-0 text-[13px] text-muted">
                {t.sitesHint}
              </p>
              {active && active.length === 0 && <p className="mt-[10px] mb-0 text-muted">{t.sitesNone}</p>}
              {active && active.length > 0 && (
                <ul className="m-0 mt-[10px] flex list-none flex-col gap-[8px] p-0">
                  {active.map((d, i) => (
                    <li key={d}>
                      <label className="flex items-start gap-[10px]">
                        <span className="flex pt-[2px]">
                          <Checkbox
                            id={`ci-site-${i}`}
                            label={d}
                            checked={picked.includes(d)}
                            aria-describedby="ci-sites-hint"
                            onChange={(e) => edit(setPicked)(e.target.checked ? [...picked, d] : picked.filter((x) => x !== d))}
                          />
                        </span>
                        <span className="min-w-0 [overflow-wrap:anywhere]">{d}</span>
                      </label>
                    </li>
                  ))}
                </ul>
              )}
            </fieldset>

            <Field
              label={t.emailLabel}
              htmlFor="ci-email"
              hint={t.emailHint}
              hintId="ci-email-hint"
              error={bad.includes("email") && <span id="ci-email-error">{t.emailRequired}</span>}
            >
              <Input
                id="ci-email"
                type="email"
                spellCheck={false}
                autoCapitalize="none"
                autoComplete="off"
                placeholder="admin@example.com"
                value={email}
                onChange={(e) => edit(setEmail)(e.target.value)}
                aria-invalid={bad.includes("email") || undefined}
                aria-describedby={`ci-email-hint${bad.includes("email") ? " ci-email-error" : ""}`}
              />
            </Field>

            {gen.isError && (
              <div>
                <ErrorNote error={gen.error} heading={t.errorHeading} />
                {apiErr?.code === "cloudinit_too_large" && <p className="mt-[6px] mb-0 pl-[30px] text-[13px] text-muted">{t.tooLargeHint}</p>}
              </div>
            )}
            <div>
              <Button variant="primary" type="submit" disabled={gen.isPending}>
                {gen.isPending ? t.submitting : t.submit}
              </Button>
            </div>
          </div>
        </Card>
      </form>

      {result && (
        <section aria-label={t.resultHeading} className="flex max-w-[560px] flex-col gap-[20px]">
          {pwHidden ? <p className="m-0 text-muted">{t.secretHidden}</p> : <PasswordBox password={result.res.admin_password} onDismiss={() => setPwHidden(true)} />}
          <UserData res={result.res} />
          <HowTo withSites={result.withSites} />
        </section>
      )}
    </>
  );
}

function SumField({ id, label, hint, value, onChange, error }: { id: string; label: string; hint: string; value: string; onChange: (v: string) => void; error: string | null }) {
  const hintId = `${id}-hint`;
  const errId = `${id}-error`;
  return (
    <Field label={label} htmlFor={id} hint={hint} hintId={hintId} error={error && <span id={errId}>{error}</span>}>
      <Input
        id={id}
        mono
        spellCheck={false}
        autoCapitalize="none"
        autoComplete="off"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        aria-invalid={error ? true : undefined}
        aria-describedby={`${hintId}${error ? ` ${errId}` : ""}`}
      />
    </Field>
  );
}

/** Mật khẩu quản trị của máy mới, một lần: cùng hình thức PasswordBox (nguoi-dung) và DbSecretBox. */
function PasswordBox({ password, onDismiss }: { password: string; onDismiss: () => void }) {
  const t = useStrings(strings);
  const ref = useRef<HTMLElement>(null);
  const headingId = useId();
  // Kết quả về khi người dùng đang ở nút gửi cuối form: đưa khối vào tầm mắt và cho trình đọc màn hình nhảy tới.
  useEffect(() => {
    ref.current?.scrollIntoView({ block: "start" });
    ref.current?.focus({ preventScroll: true });
  }, [password]);
  return (
    <section ref={ref} tabIndex={-1} aria-labelledby={headingId} className={`${cardClass} p-[16px] outline-offset-4`}>
      <div className="text-[12px] font-medium text-muted">{t.secretLabel}</div>
      <h2 id={headingId} className="m-0 mt-[4px] text-[16px] font-semibold">
        {t.secretTitle}
      </h2>
      <p className="mt-[6px] mb-0">{t.secretBody}</p>
      <dl className="m-0 mt-[14px]">
        {/* Tên user TPAN dùng sans (mục 4 của GIAO-DIEN.md); mật khẩu mono để so từng ký tự. */}
        <SecretRow label={t.secretUser} value={adminUser} mono={false} />
        <SecretRow label={t.secretPassword} value={password} />
      </dl>
      <div className="mt-[14px]">
        <Button onClick={onDismiss}>{t.secretDismiss}</Button>
      </div>
    </section>
  );
}

function SecretRow({ label, value, mono = true }: { label: string; value: string; mono?: boolean }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { state, copy } = useCopy();
  return (
    <div className="flex flex-wrap items-center gap-x-[12px] gap-y-[6px] border-t border-hair py-[8px]">
      <dt className="flex-[0_0_120px] text-muted">{label}</dt>
      <dd className="m-0 min-w-0 flex-[1_1_220px] select-text">{mono ? <Code>{value}</Code> : value}</dd>
      <Button size="sm" icon={state === "copied" ? "check" : "copy"} onClick={() => copy(value)} aria-label={t.secretCopyOf(label)}>
        {t.secretCopy}
      </Button>
      <span aria-live="polite" className={`text-[12px] ${state === "idle" ? "sr-only" : "text-muted"}`}>
        {state === "copied" ? k.copied : state === "failed" ? k.copyFailed : ""}
      </span>
    </div>
  );
}

function UserData({ res }: { res: CloudInitResponse }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { state, copy } = useCopy();
  return (
    <Card
      title={t.userDataLabel}
      titleId="ci-user-data"
      actions={
        <Button size="sm" icon={state === "copied" ? "check" : "copy"} onClick={() => copy(res.user_data)}>
          {t.userDataCopy}
        </Button>
      }
    >
      <p className="m-0 text-[13px] text-muted tabular-nums">
        {t.userDataSize(groupDigits(res.bytes), groupDigits(defaultMaxBytes))}
        <span aria-live="polite" className={state === "idle" ? "sr-only" : "ml-[8px]"}>
          {state === "copied" ? k.copied : state === "failed" ? k.copyFailed : ""}
        </span>
      </p>
      {/* Chép đúng chuỗi gốc bằng nút trên; khối chỉ để đọc (EvidenceBlock bỏ "$ " đầu dòng khi chép, không hợp với user-data). */}
      <Code block className="mt-[10px]">
        {res.user_data}
      </Code>
    </Card>
  );
}

function HowTo({ withSites }: { withSites: boolean }) {
  const t = useStrings(strings);
  return (
    <Card title={t.howHeading} titleId="ci-how">
      <ol className="m-0 flex list-decimal flex-col gap-[12px] pl-[20px]">
        <li>{t.howPaste}</li>
        <li>
          {t.howBoot}
          {withSites && (
            <span className="mt-[4px] block">
              {t.howSitePasswords} <Code>/root/tpan-first-boot-passwords.txt</Code>
            </span>
          )}
        </li>
        <li>
          <span className="mb-[8px] block">{t.howStatus}</span>
          <CommandBlock commands={["tpan cloud-init status"]} where={t.howStatusWhere} />
        </li>
      </ol>
    </Card>
  );
}
