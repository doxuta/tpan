// Thẻ "Đích ngoài máy" của màn Backup (chỉ admin; mục 3.3, 3.8, 3.11 của
// docs/thay-doi/M18-thiet-ke.md): xem đích S3 đang đặt, đặt hoặc đổi đích, thử
// kết nối, gỡ đích. Đặt và gỡ đi ba chặng như mọi thao tác đổi máy: ?dry_run=1
// lấy bản xem trước (PlanReview) → bấm chạy → thẻ theo dõi job báo tiến độ.
// Thử kết nối không có bản xem trước: nó chỉ ghi, đọc rồi xoá một object nhỏ
// trên kho, không đổi gì trên máy.
//
// Bí mật: hai khoá S3 chỉ nằm trong state của form và trong ref `body` giữa
// lúc xem trước và lúc chạy. Không đưa thân request làm đối số của mutate():
// TanStack giữ đối số đó trong bộ nhớ đệm của mutation (state.variables), mà
// hợp đồng ở m18-types.ts cấm khoá vào bộ nhớ đệm. Lượt chạy gửi lại đúng thân
// đã xem trước (người dùng có thể đã sửa ô sau đó). Hai ô khoá xoá ngay khi
// lượt xem trước xong, đạt hay lỗi.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { del, errorMessage, get, post } from "../../api/client";
import { targetPath, targetTestPath, type TargetRequest, type TargetView } from "../../api/m18-types";
import type { JobStarted, Preview } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { CommandBlock } from "../../components/CommandBlock";
import { Disclosure } from "../../components/Disclosure";
import { Checkbox, Field, Input, Textarea } from "../../components/Field";
import { useJobTracker } from "../../components/JobTracker";
import { Code } from "../../components/MonoText";
import { Notice } from "../../components/Notice";
import { SegmentButton } from "../../components/SegmentButton";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/backup-dich";
import { strings as khung } from "../../i18n/khung";
import { Row, Rows } from "../cai-dat/Rows";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";
import { formatWhen } from "../thung-rac/model";
import { buildRequest, checkForm, formFromView, keyHint, maxRemoteKeep, targetKey, type CaMode, type FormProblem, type TargetForm } from "./target-model";

type Op = "set" | "rm";
type T = (typeof strings)["vi"];

export function TargetCard() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const qc = useQueryClient();
  const { track } = useJobTracker();
  const q = useQuery({ queryKey: targetKey, queryFn: () => get<TargetView>(targetPath) });

  /** Thân POST target đã xem trước; null khi không có lượt đặt nào đang mở. */
  const body = useRef<TargetRequest | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [open, setOpen] = useState<{ op: Op; preview: Preview } | null>(null);
  const [asked, setAsked] = useState<Op | null>(null);
  const resultRef = useRef<HTMLDivElement>(null);

  const dry = useMutation({
    mutationFn: (op: Op) => (op === "set" ? post<Preview>(`${targetPath}?dry_run=1`, body.current) : del<Preview>(`${targetPath}?dry_run=1`)),
    onSuccess: (preview, op) => setOpen({ op, preview }),
    onError: () => {
      body.current = null;
    },
  });
  const run = useMutation({
    mutationFn: (op: Op) => (op === "set" ? post<JobStarted>(targetPath, body.current) : del<JobStarted>(targetPath)),
    onSuccess: (job) => {
      body.current = null;
      void qc.invalidateQueries({ queryKey: targetKey });
      track(job.job_id);
      setOpen(null);
      setAsked(null);
      setFormOpen(false);
    },
    // Chạy lỗi thì giữ body: PlanReview vẫn mở với nút chạy, bấm lại phải gửi
    // đúng thân cũ. Gửi null hay khoá rỗng lúc đó là "giữ khoá đang lưu", sai ý người dùng.
  });
  const test = useMutation({
    mutationFn: () => post<JobStarted>(targetTestPath),
    onSuccess: (job) => track(job.job_id),
  });

  // Có bản xem trước hoặc lỗi thì đưa người dùng tới đó: nó nằm dưới thẻ.
  const shown = open ?? (dry.isError ? asked : null);
  useEffect(() => {
    if (shown) {
      resultRef.current?.scrollIntoView({ block: "start" });
      resultRef.current?.focus();
    }
  }, [shown]);

  /** Chặng xem trước. Không ném lỗi: lỗi hiện bằng dry.error. */
  function ask(op: Op): Promise<void> {
    if (op === "rm") body.current = null;
    setOpen(null);
    run.reset();
    setAsked(op);
    return dry.mutateAsync(op).then(
      () => undefined,
      () => undefined,
    );
  }

  function previewSet(b: TargetRequest): Promise<void> {
    body.current = b;
    return ask("set");
  }

  function close() {
    body.current = null;
    setOpen(null);
    setAsked(null);
    dry.reset();
    run.reset();
  }

  const v = q.data;
  const pending = dry.isPending ? asked : null;

  return (
    <>
      <Card
        title={t.heading}
        titleId="backup-target"
        actions={
          v?.configured && (
            <>
              <Button onClick={() => test.mutate()} disabled={test.isPending}>
                {test.isPending ? t.testing : t.test}
              </Button>
              <Button onClick={() => void ask("rm")} disabled={dry.isPending}>
                {pending === "rm" ? t.previewing : t.remove}
              </Button>
            </>
          )
        }
      >
        {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
        {q.isError && (
          <>
            <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
            <Button onClick={() => void q.refetch()}>{k.retry}</Button>
          </>
        )}
        {v && (
          <div className="flex flex-col gap-[16px]">
            {v.configured ? <TargetNotices v={v} t={t} /> : <p className="m-0 max-w-[72ch]">{t.unsetLead}</p>}
            {test.isError && <ErrorNote error={test.error} heading={t.testErrorHeading} checksHeading={t.checksHeading} />}
            {v.configured && <TargetRows v={v} t={t} />}
            {v.configured ? (
              <Disclosure open={formOpen} onOpenChange={setFormOpen} summary={<span className="font-medium">{t.change}</span>}>
                <TargetFormBlock key={formKey(v)} view={v} pending={pending === "set"} busy={dry.isPending} onPreview={previewSet} />
              </Disclosure>
            ) : (
              <TargetFormBlock key={formKey(v)} view={v} pending={pending === "set"} busy={dry.isPending} onPreview={previewSet} />
            )}
          </div>
        )}
      </Card>

      {dry.isError && asked && (
        <div ref={resultRef} tabIndex={-1} className="outline-offset-0">
          <ErrorNote error={dry.error} heading={asked === "rm" ? t.removeErrorHeading : t.setErrorHeading} checksHeading={t.checksHeading} />
          <div className="pt-[12px]">
            <Button onClick={close}>{k.cancel}</Button>
          </div>
        </div>
      )}

      {open && (
        <div ref={resultRef} tabIndex={-1} className="flex flex-col gap-[12px] outline-offset-0">
          {open.op === "rm" && <p className="m-0 max-w-[72ch] font-semibold">{t.removeWarning(v?.remote_only ?? 0)}</p>}
          <PlanReview preview={open.preview} onRun={() => run.mutate(open.op)} running={run.isPending} error={run.error} onCancel={close} />
        </div>
      )}
    </>
  );
}

/**
 * Form dựng lại (key) khi đích đang lưu đổi, để ô nhập bắt đầu từ giá trị mới.
 * Chỉ ghép trường ổn định: số đếm, lỗi, giờ thử lại đổi theo lượt đẩy nền và
 * sẽ xoá chữ người dùng đang gõ nếu nằm trong key.
 */
function formKey(v: TargetView): string {
  return [v.configured, v.endpoint, v.region, v.bucket, v.prefix, v.keep, v.path_style, v.has_ca].join("|");
}

function TargetNotices({ v, t }: { v: TargetView; t: T }) {
  const { lang } = useLang();
  return (
    <>
      {v.target_error && (
        <Notice kind="fail">
          <p className="m-0 font-semibold">{t.targetError}</p>
          <p className="m-0 whitespace-pre-line [overflow-wrap:anywhere]">{v.target_error}</p>
          {v.retry_at && <p className="m-0">{t.retryAt(formatWhen(v.retry_at, lang))}</p>}
        </Notice>
      )}
      {!v.has_backup_key && (
        <Notice kind="warn">
          <p className="m-0 font-semibold">{t.noBackupKey}</p>
          <p className="mt-[4px] mb-[10px] max-w-[72ch]">{t.noBackupKeyMore}</p>
          {/* CommandBlock tự thêm sudo. API không bao giờ sinh khoá sao lưu (S5): chỉ lệnh này. */}
          <CommandBlock commands={["tpan backup key new"]} />
        </Notice>
      )}
      {v.panel_error && (
        <Notice kind="warn">
          <p className="m-0 [overflow-wrap:anywhere]">{t.panelError(v.panel_error)}</p>
        </Notice>
      )}
    </>
  );
}

function TargetRows({ v, t }: { v: TargetView; t: T }) {
  const { lang } = useLang();
  const hint = keyHint(v);
  return (
    <Rows>
      <Row label={t.endpoint}>
        <Code>{v.endpoint}</Code>
      </Row>
      <Row label={t.region}>{v.region}</Row>
      <Row label={t.bucket}>
        <Code>{v.bucket}</Code>
      </Row>
      <Row label={t.prefix}>
        <Code>{v.prefix}</Code>
      </Row>
      <Row label={t.keep}>{t.keepValue(v.keep ?? 0)}</Row>
      <Row label={t.addressing}>{v.path_style ? t.pathStyle : t.hostStyle}</Row>
      <Row label={t.ca}>{v.has_ca ? t.caYes : t.caNo}</Row>
      <Row label={t.accessKey}>{"tail" in hint ? <Code>{hint.tail}</Code> : t.keyLen(hint.len)}</Row>
      <Row label={t.backupKey}>{v.has_backup_key && v.key_fingerprint ? <Code>{v.key_fingerprint}</Code> : t.backupKeyNone}</Row>
      <Row label={t.panel}>
        {v.panel_last ? (
          <>
            <Code>{v.panel_last.name}</Code> {t.panelAt(formatWhen(v.panel_last.at, lang))}
          </>
        ) : (
          t.panelNone
        )}
      </Row>
      <Row label={t.copies}>{t.counts(v.remote_only, v.other_target, v.failed)}</Row>
    </Rows>
  );
}

type FormProps = {
  view: TargetView;
  /** Lượt xem trước của form này đang chạy (chữ nút). */
  pending: boolean;
  /** Có lượt xem trước nào đang chạy (khoá nút). */
  busy: boolean;
  /** Xong (đạt hay lỗi) thì promise trả về; form xoá hai ô khoá lúc đó. */
  onPreview: (body: TargetRequest) => Promise<void>;
};

function TargetFormBlock({ view, pending, busy, onPreview }: FormProps) {
  const t = useStrings(strings);
  const [f, setF] = useState<TargetForm>(() => formFromView(view));
  // Lỗi ô nhập chỉ hiện sau lần bấm đầu: form trống lúc mới mở không phải lỗi.
  const [tried, setTried] = useState(false);
  const put = <K extends keyof TargetForm>(key: K, val: TargetForm[K]) => setF((p) => ({ ...p, [key]: val }));

  const problems = checkForm(f, view.configured);
  const bad = (p: FormProblem) => tried && problems.includes(p);
  const keysBad = bad("keys") || bad("keysRequired");

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    if (problems.length > 0) {
      setTried(true);
      return;
    }
    setTried(false);
    void onPreview(buildRequest(f)).finally(() => setF((p) => ({ ...p, accessKey: "", secretKey: "" })));
  }

  // Chưa đặt đích thì không có CA nào để giữ.
  const caOptions: { value: CaMode; label: string }[] = [
    ...(view.configured ? [{ value: "keep" as const, label: t.caKeep }] : []),
    { value: "custom", label: t.caCustom },
    { value: "none", label: t.caNone },
  ];

  return (
    <form onSubmit={onSubmit} noValidate className="flex max-w-[560px] flex-col gap-[20px]">
      <Field label={t.endpoint} htmlFor="o-dich-endpoint" hint={t.endpointHint} hintId="h-dich-endpoint" error={bad("endpoint") ? t.endpointMissing : undefined}>
        <Input
          id="o-dich-endpoint"
          name="endpoint"
          mono
          inputMode="url"
          autoComplete="off"
          spellCheck={false}
          value={f.endpoint}
          onChange={(e) => put("endpoint", e.target.value)}
          aria-describedby="h-dich-endpoint"
          aria-invalid={bad("endpoint") || undefined}
        />
      </Field>

      <Field label={t.region} htmlFor="o-dich-region" hint={t.regionHint} hintId="h-dich-region">
        <Input id="o-dich-region" name="region" autoComplete="off" spellCheck={false} placeholder="us-east-1" value={f.region} onChange={(e) => put("region", e.target.value)} aria-describedby="h-dich-region" />
      </Field>

      <Field label={t.bucket} htmlFor="o-dich-bucket" error={bad("bucket") ? t.bucketMissing : undefined}>
        <Input id="o-dich-bucket" name="bucket" mono autoComplete="off" spellCheck={false} value={f.bucket} onChange={(e) => put("bucket", e.target.value)} aria-invalid={bad("bucket") || undefined} />
      </Field>

      <Field label={t.prefix} htmlFor="o-dich-prefix" hint={t.prefixHint} hintId="h-dich-prefix">
        <Input id="o-dich-prefix" name="prefix" mono autoComplete="off" spellCheck={false} value={f.prefix} onChange={(e) => put("prefix", e.target.value)} aria-describedby="h-dich-prefix" />
      </Field>

      <Field label={t.keepLabel} htmlFor="o-dich-keep" hint={t.keepHint} hintId="h-dich-keep" error={bad("keep") ? t.keepInvalid : undefined}>
        {/* Ô số chỉ cần hẹp: Input luôn rộng hết khung nên bọc trong khung 120 px. */}
        <div className="w-[120px]">
          <Input
            id="o-dich-keep"
            name="keep"
            type="number"
            inputMode="numeric"
            min={1}
            max={maxRemoteKeep}
            step={1}
            placeholder="7"
            value={f.keep}
            onChange={(e) => put("keep", e.target.value)}
            aria-describedby="h-dich-keep"
            aria-invalid={bad("keep") || undefined}
          />
        </div>
      </Field>

      <label className="flex max-w-[72ch] items-start gap-[10px]">
        <span className="flex pt-[2px]">
          <Checkbox id="o-dich-vhost" label={t.vhostLabel} checked={f.vhost} onChange={(e) => put("vhost", e.target.checked)} aria-describedby="h-dich-vhost" />
        </span>
        <span className="min-w-0">
          <span className="block font-medium">{t.vhostLabel}</span>
          <span id="h-dich-vhost" className="block text-[13px] text-muted">
            {t.vhostHint}
          </span>
        </span>
      </label>

      <div className="flex flex-col gap-[12px]">
        <div>
          <p className="mt-0 mb-[6px] font-medium">{t.caLabel}</p>
          <SegmentButton<CaMode> label={t.caLabel} options={caOptions} value={f.ca} onChange={(ca) => put("ca", ca)} />
        </div>
        {f.ca === "custom" && (
          <Field label={t.caPemLabel} htmlFor="o-dich-ca-pem" hint={t.caPemHint} hintId="h-dich-ca-pem" error={bad("caPem") ? t.caPemMissing : undefined}>
            <Textarea
              id="o-dich-ca-pem"
              name="ca_pem"
              rows={6}
              autoComplete="off"
              spellCheck={false}
              value={f.caPem}
              onChange={(e) => put("caPem", e.target.value)}
              aria-describedby="h-dich-ca-pem"
              aria-invalid={bad("caPem") || undefined}
            />
          </Field>
        )}
      </div>

      <Field label={t.accessKeyLabel} htmlFor="o-dich-access">
        <Input
          id="o-dich-access"
          name="access_key"
          autoComplete="off"
          spellCheck={false}
          value={f.accessKey}
          onChange={(e) => put("accessKey", e.target.value)}
          aria-describedby="h-dich-keys"
          aria-invalid={keysBad || undefined}
        />
      </Field>

      <Field
        label={t.secretKeyLabel}
        htmlFor="o-dich-secret"
        hint={view.configured ? t.keysHint : t.keysHintNew}
        hintId="h-dich-keys"
        error={bad("keys") ? t.keysPair : bad("keysRequired") ? t.keysRequired : undefined}
      >
        <Input
          id="o-dich-secret"
          name="secret_key"
          type="password"
          // "new-password" thay vì "off": Chrome bỏ qua "off" ở ô mật khẩu và có
          // thể tự điền mật khẩu đăng nhập panel vào đây, rồi gửi nó lên kho.
          autoComplete="new-password"
          spellCheck={false}
          value={f.secretKey}
          onChange={(e) => put("secretKey", e.target.value)}
          aria-describedby="h-dich-keys"
          aria-invalid={keysBad || undefined}
        />
      </Field>

      <div>
        <Button variant="primary" type="submit" disabled={busy}>
          {pending ? t.previewing : t.preview}
        </Button>
      </div>
    </form>
  );
}
