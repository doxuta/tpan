// Thẻ "Cấu hình" của màn SSL: email cho Let's Encrypt và công tắc staging.
// Lưu bằng POST /api/v1/ssl/settings. Đây là đổi cấu hình của TPAN chứ không
// đổi máy, nên không qua bản xem trước; riêng tắt staging (từ nay xin chứng
// chỉ thật, tốn hạn mức) thì hỏi lại một lần.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { post } from "../../api/client";
import type { SSLConfig, SSLOverview, SSLSettings } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Dialog } from "../../components/Dialog";
import { TextField } from "../../components/Field";
import { Notice } from "../../components/Notice";
import { SegmentButton } from "../../components/SegmentButton";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/ssl";
import { ErrorNote } from "../site/ErrorNote";
import { sslKey } from "./model";

export function Settings({ config }: { config: SSLConfig }) {
  const t = useStrings(strings);
  const qc = useQueryClient();
  const [email, setEmail] = useState(config.email);
  const [staging, setStaging] = useState(config.staging);
  const [confirming, setConfirming] = useState(false);
  const [justSaved, setJustSaved] = useState(false);

  const save = useMutation({
    mutationFn: (body: SSLSettings) => post<SSLConfig>("/api/v1/ssl/settings", body),
    onSuccess: (saved) => {
      // Máy chủ trả cấu hình đã lưu: ghi thẳng vào bộ nhớ đệm để "đã đổi" tắt ngay.
      qc.setQueryData<SSLOverview>(sslKey, (old) => (old ? { ...old, config: saved } : old));
      setEmail(saved.email);
      setStaging(saved.staging);
      setConfirming(false);
      setJustSaved(true);
    },
    onError: () => setConfirming(false),
  });

  const trimmed = email.trim();
  const dirty = trimmed !== config.email || staging !== config.staging;
  const turningOff = config.staging && !staging;

  function send() {
    // Chỉ gửi trường đã đổi: trường vắng mặt thì máy chủ giữ nguyên.
    const body: SSLSettings = {};
    if (trimmed !== config.email) body.email = trimmed;
    if (staging !== config.staging) body.staging = staging;
    save.mutate(body);
  }

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!dirty) return;
    if (turningOff) setConfirming(true);
    else send();
  }

  // Sửa gì thì "Đã lưu" và lỗi cũ hết đúng.
  const touch = () => {
    setJustSaved(false);
    save.reset();
  };

  return (
    <Card title={t.settingsHeading} titleId="ssl-cfg">
      <form onSubmit={onSubmit} noValidate className="flex max-w-[560px] flex-col gap-[20px]">
        <TextField
          id="o-ssl-email"
          name="email"
          type="email"
          label={t.emailLabel}
          hint={t.emailHint}
          value={email}
          onChange={(e) => {
            setEmail(e.target.value);
            touch();
          }}
          autoComplete="off"
          spellCheck={false}
          autoCapitalize="none"
          placeholder="admin@example.com"
        />

        <div>
          <div id="l-ssl-staging" className="mb-[6px] text-[14px] font-medium">
            {t.stagingLabel}
          </div>
          <SegmentButton
            label={t.stagingGroup}
            value={staging ? "on" : "off"}
            onChange={(v) => {
              setStaging(v === "on");
              touch();
            }}
            options={[
              { value: "on", label: t.stagingOn },
              { value: "off", label: t.stagingOff },
            ]}
          />
          <p id="h-ssl-staging" className="mt-[6px] mb-0 text-[13px] text-muted">
            {t.stagingHint}
          </p>
        </div>

        {save.isError && <ErrorNote error={save.error} heading={t.saveFailed} />}

        <div className="flex flex-wrap items-center gap-x-[16px] gap-y-[8px]">
          <Button variant="primary" type="submit" disabled={!dirty || save.isPending}>
            {save.isPending ? t.saving : t.save}
          </Button>
          {/* aria-live luôn có mặt trong cây để trình đọc màn hình đọc được chữ vừa hiện. */}
          <div aria-live="polite">{justSaved && !dirty && <Notice kind="pass">{t.saved}</Notice>}</div>
        </div>
      </form>

      <Dialog open={confirming} onOpenChange={setConfirming} title={t.confirmTitle} description={t.confirmText} confirmLabel={t.confirmLabel} onConfirm={send} busy={save.isPending} />
    </Card>
  );
}
