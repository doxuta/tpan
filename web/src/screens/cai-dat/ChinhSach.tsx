// Thẻ "Đăng nhập hai bước" của màn Cài đặt (admin, M20): chính sách của cả máy,
// off / admin / all. GET và POST /api/v1/settings/auth. Bật bắt buộc thì hỏi lại
// một lần vì nó áp ngay cho mọi phiên đang mở. Lỗi mfa_enroll_self_first (admin
// chưa bật hai bước cho chính mình) có câu riêng kèm liên kết tới trang Tài khoản.

import { useState } from "react";
import { Link } from "react-router";
import { ApiError, errorMessage } from "../../api/client";
import { useAuthSettings, useSaveAuthSettings } from "../../api/m20";
import type { Policy } from "../../api/m20-types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Dialog } from "../../components/Dialog";
import { SegmentButton } from "../../components/SegmentButton";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/cai-dat";
import { strings as khung } from "../../i18n/khung";
import { ReauthError, ReauthFields, useReauth } from "../nguoi-dung/Reauth";
import { Note } from "./Note";

// Mức chặt của chính sách: hạ xuống là nới lỏng.
const rank: Record<Policy, number> = { off: 0, admin: 1, all: 2 };

export function ChinhSach() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useAuthSettings();
  const save = useSaveAuthSettings();
  const [pick, setPick] = useState<Policy | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [justSaved, setJustSaved] = useState(false);
  const [local, setLocal] = useState<string | null>(null);
  const re = useReauth(save.error);

  const current = q.data?.require_2fa;
  const value = pick ?? current ?? "off";
  const dirty = current !== undefined && value !== current;
  // Nới lỏng (hạ mức) thì máy chủ đòi mật khẩu của admin đang thao tác; đòi mà ta chưa đoán ra thì hiện ô sau lần gửi đầu.
  const asked = save.error instanceof ApiError && (save.error.code === "reauth_required" || save.error.code === "mfa_code_required");
  const needReauth = dirty && ((current !== undefined && rank[value] < rank[current]) || asked);

  const send = () => {
    setConfirming(false);
    save.mutate(
      { require_2fa: value, ...(needReauth && re.body()) },
      {
        onSuccess: () => {
          setPick(null);
          setJustSaved(true);
          re.setPassword("");
          re.setCode("");
        },
        onError: re.afterError,
      },
    );
  };
  const submit = () => {
    const missing = needReauth ? re.problem() : null;
    setLocal(missing);
    if (missing) return;
    if (value === "off") send();
    else setConfirming(true);
  };

  const selfFirst = save.error instanceof ApiError && save.error.code === "mfa_enroll_self_first";

  return (
    <Card title={t.polHeading} titleId="chinh-sach-tieu-de">
      {q.isPending ? (
        <p className="m-0 text-muted">{k.loading}</p>
      ) : q.isError ? (
        <div className="flex flex-col gap-[12px]">
          <p className="m-0">
            {t.polLoadFailed} {errorMessage(q.error, lang)}
          </p>
          <div>
            <Button onClick={() => void q.refetch()}>{k.retry}</Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-[16px]">
          <p className="m-0 max-w-[72ch] text-muted">{t.polIntro}</p>
          <div>
            <div className="mb-[6px] text-[14px] font-medium">{t.polLabel}</div>
            <SegmentButton<Policy>
              label={t.polGroup}
              value={value}
              onChange={(v) => {
                setPick(v);
                setJustSaved(false);
                setLocal(null);
                save.reset();
              }}
              options={[
                { value: "off", label: t.polOff },
                { value: "admin", label: t.polAdmin },
                { value: "all", label: t.polAll },
              ]}
            />
          </div>
          {needReauth && <ReauthFields r={re} idPrefix="pol" why={t.polReauthWhy} error={save.error} />}
          {local && <Note kind="fail">{local}</Note>}
          {selfFirst ? (
            <div role="alert" className="flex flex-col gap-[8px]">
              <Note kind="fail">{t.polSelfFirst}</Note>
              <Link to="/account#hai-buoc" className="self-start">
                {t.polGoAccount}
              </Link>
            </div>
          ) : (
            save.isError && <ReauthError error={save.error} />
          )}
          <div className="flex flex-wrap items-center gap-x-[16px] gap-y-[8px]">
            <Button variant="primary" onClick={submit} disabled={!dirty || save.isPending}>
              {save.isPending ? t.polSaving : t.polSave}
            </Button>
            <div aria-live="polite">{justSaved && !dirty && <Note kind="pass">{t.polSaved}</Note>}</div>
          </div>
        </div>
      )}
      <Dialog
        open={confirming}
        onOpenChange={setConfirming}
        title={t.polConfirmTitle}
        description={value === "all" ? t.polConfirmAll : t.polConfirmAdmin}
        confirmLabel={t.polConfirmLabel}
        onConfirm={send}
        busy={save.isPending}
      />
    </Card>
  );
}
