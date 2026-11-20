// Mục "AI và bộ não" ở màn Cài đặt (mục 8.2 và mục 10 của KE-HOACH.md):
// GET /api/v1/ai/brain. Chỉ để xem và để đọc cách nối. Bật AI là lệnh trên máy
// chủ (`sudo tpan mcp config --brain bridge`), nối bộ não là lệnh trên máy cá
// nhân của người vận hành; trang web không làm hai việc đó thay họ, và không
// bao giờ chạm vào đăng nhập Claude.

import { useQuery } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { errorMessage, get } from "../../api/client";
import type { BrainStatus } from "../../api/types";
import { Button } from "../../components/Button";
import { CommandBlock } from "../../components/CommandBlock";
import { MonoText } from "../../components/MonoText";
import { StatusMark } from "../../components/StatusMark";
import { Tag } from "../../components/Tag";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/cai-dat";
import { strings as khung } from "../../i18n/khung";
import { Field } from "../dang-nhap/Field";
import { formatDateTime } from "../lich-su/format";

/** Bật AI trên máy chủ: đổi cấu hình rồi khởi động lại (tpan serve chỉ đọc ai.brain lúc khởi động). CommandBlock tự thêm sudo. */
const enableCommands = ["tpan mcp config --brain bridge", "systemctl restart tpan"];

/**
 * Sudoers cho user SSH (mục 3.9 của docs/HUONG-DAN-CHU-DU-AN.md). $USER do
 * shell của người gõ tự thay bằng tên user đang đăng nhập, nên lệnh chép là
 * chạy được ngay, không có chỗ nào phải tự sửa. Mỗi dòng đã có sudo đúng chỗ.
 */
const sudoersFile = "/etc/sudoers.d/tpan-mcp";
const sudoCommands = [
  `echo "$USER ALL=(root) NOPASSWD: /usr/local/bin/tpan mcp, /usr/local/bin/tpan mcp --session *, /usr/local/bin/tpan brain-link" | sudo tee ${sudoersFile}`,
  `sudo chmod 440 ${sudoersFile}`,
  "sudo visudo -c",
];

/** User SSH gõ ở ô nhập được nhớ trong trình duyệt này (chỉ tiện lợi; trình duyệt chặn bộ nhớ thì thôi). */
const userKey = "tpan.brain.sshUser";
function loadUser(): string {
  try {
    return window.localStorage.getItem(userKey) ?? "";
  } catch {
    return "";
  }
}
function saveUser(v: string) {
  try {
    window.localStorage.setItem(userKey, v);
  } catch {
    /* bỏ qua */
  }
}

/** Ba bước nối bộ não khi chưa nối. Bước 1 chỉ hiện lệnh khi AI còn tắt. */
function ConnectSteps({ modeOff }: { modeOff: boolean }) {
  const t = useStrings(strings);
  const [user, setUser] = useState(loadUser);
  const host = window.location.hostname;
  const u = user.trim();
  const target = `${u === "" ? "<user>" : u}@${host}`;
  const connectCommands = ["make build", `bin/tpan brain ${target}`];

  return (
    <div className="mt-[14px] flex max-w-[760px] flex-col gap-[20px] border-t border-hair pt-[14px]">
      {modeOff ? (
        <div>
          <h3 className="m-0 text-[14px] font-semibold">{t.brainEnableTitle}</h3>
          <p className="mt-[4px] mb-[8px] text-muted">{t.brainEnableBody}</p>
          <CommandBlock commands={enableCommands} />
        </div>
      ) : (
        <p className="m-0 flex items-center gap-[10px] text-[14px] font-semibold">
          <StatusMark status="pass" />
          {t.brainEnableDone}
        </p>
      )}
      <div>
        <h3 className="m-0 text-[14px] font-semibold">{t.brainSudoTitle}</h3>
        <p className="mt-[4px] mb-[8px] text-muted">{t.brainSudoBody}</p>
        <CommandBlock commands={sudoCommands} addSudo={false} />
      </div>
      <div>
        <h3 className="m-0 text-[14px] font-semibold">{t.brainConnectTitle}</h3>
        <p className="mt-[4px] mb-[12px] text-muted">{t.brainConnectBody}</p>
        <div className="mb-[12px] flex flex-wrap gap-[16px]">
          <div className="min-w-[200px] flex-1">
            <Field
              id="bo-nao-user"
              label={t.brainUserLabel}
              hint={t.brainUserHint}
              autoComplete="off"
              spellCheck={false}
              placeholder="ubuntu"
              value={user}
              onChange={(e) => {
                setUser(e.target.value);
                saveUser(e.target.value);
              }}
            />
          </div>
          <div className="min-w-[200px] flex-1">
            <div className="mb-[6px] text-[14px] font-medium">{t.brainHostLabel2}</div>
            <div className="flex h-[44px] items-center font-mono text-[13px] sm:h-[40px]">{host}</div>
            <p className="mt-[6px] mb-0 text-[13px] text-muted">{t.brainHostHint}</p>
          </div>
        </div>
        <CommandBlock commands={connectCommands} addSudo={false} where={t.brainConnectWhere} />
      </div>
    </div>
  );
}

/** Claude Code đăng nhập bằng gói claude.ai thì không tính tiền theo API; mọi cách khác thì có. */
const planAuth = "claude.ai";

const labelClass = "font-mono text-[11px] tracking-[0.08em] text-muted";

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="border-t border-hair py-[12px]">
      <dt className={labelClass}>{label}</dt>
      <dd className="m-0 mt-[6px]">{children}</dd>
    </div>
  );
}

/** Cảnh báo bằng dấu trạng thái "cảnh báo" và chữ, không chỉ màu (mục 2 của GIAO-DIEN.md). */
function Warn({ children }: { children: ReactNode }) {
  const k = useStrings(khung);
  return (
    <p className="mt-[8px] mb-0 flex items-start gap-[10px]">
      <span className="mt-[3px] flex-none">
        <StatusMark status="warn" label={k.status.warn} />
      </span>
      <span className="min-w-0">{children}</span>
    </p>
  );
}

export function BoNao() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  // Cùng khoá với ngăn AI nên hai nơi dùng chung một lần tải.
  const q = useQuery({ queryKey: ["ai", "brain"], queryFn: () => get<BrainStatus>("/api/v1/ai/brain"), refetchInterval: 10_000, retry: false });
  const b = q.data;
  const modeText = b ? { off: t.brainModeOff, bridge: t.brainModeBridge, demo: t.brainModeDemo }[b.mode] : "";
  const ready = b !== undefined && (b.connected || b.mode === "demo");
  const versionDiffers = b?.connected === true && !!b.claude_version && !!b.tested_version && b.claude_version !== b.tested_version;
  const authIsApi = b?.connected === true && !!b.auth_method && b.auth_method !== planAuth;

  return (
    <section aria-labelledby="bo-nao-tieu-de" className="pt-[28px]">
      <h2 id="bo-nao-tieu-de" className="m-0 text-[16px] font-semibold">
        {t.brainHeading}
      </h2>
      <p className="mt-[6px] mb-[14px] text-muted">{t.brainIntro}</p>

      {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div role="alert">
          <p className="m-0">
            {t.brainLoadFailed} {errorMessage(q.error, lang)}
          </p>
          <Button className="mt-[12px]" onClick={() => void q.refetch()}>
            {k.retry}
          </Button>
        </div>
      )}
      {b && (
        <>
          <dl className="m-0 max-w-[760px]">
            <Row label={t.brainModeLabel}>
              <Tag>{b.mode}</Tag>
              <p className="mt-[6px] mb-0">{modeText}</p>
            </Row>
            <Row label={t.brainStateLabel}>
              <span className="inline-flex items-center gap-[10px]">
                <StatusMark status={ready ? "pass" : "unknown"} label={ready ? t.brainConnected : t.brainNotConnected} />
                {b.mode === "demo" ? t.brainDemoReady : b.connected ? t.brainConnected : t.brainNotConnected}
              </span>
            </Row>
            {b.connected && b.host && (
              <Row label={t.brainHostLabel}>
                <MonoText size={12}>{b.host}</MonoText>
              </Row>
            )}
            {b.connected && b.claude_version && (
              <Row label={t.brainVersionLabel}>
                <MonoText size={12}>{b.claude_version}</MonoText>
                {versionDiffers && <Warn>{t.brainVersionWarn(b.claude_version, b.tested_version ?? "")}</Warn>}
              </Row>
            )}
            {b.connected && b.model && (
              <Row label={t.brainModelLabel}>
                <MonoText size={12}>{b.model}</MonoText>
              </Row>
            )}
            {b.connected && b.auth_method && (
              <Row label={t.brainAuthLabel}>
                <MonoText size={12}>{b.auth_method}</MonoText>
                {authIsApi && <Warn>{t.brainAuthWarn(b.auth_method)}</Warn>}
              </Row>
            )}
            {b.connected && b.since && (
              <Row label={t.brainSinceLabel}>
                <span title={b.since} className="font-mono text-[13px]">
                  {formatDateTime(b.since, lang)}
                </span>
              </Row>
            )}
            {b.warnings && b.warnings.length > 0 && (
              <Row label={t.brainWarningsLabel}>
                {b.warnings.map((w, i) => (
                  <p key={i} className="m-0">
                    {tx(w, lang)}
                  </p>
                ))}
              </Row>
            )}
          </dl>

          {/* Chưa nối: ba bước ở hai máy khác nhau, nên mỗi bước nói rõ chạy ở đâu. Demo thì không có gì để nối. */}
          {!b.connected && b.mode !== "demo" && <ConnectSteps modeOff={b.mode === "off"} />}
        </>
      )}
    </section>
  );
}
