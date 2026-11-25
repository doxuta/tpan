// Mục "AI và bộ não" ở màn Cài đặt (mục 8.2 và mục 10 của KE-HOACH.md):
// GET /api/v1/ai/brain. Chỉ để xem và để đọc cách nối. Bật AI là lệnh trên máy
// chủ (`sudo tpan mcp config --brain bridge`), nối bộ não là lệnh trên máy cá
// nhân của người vận hành; trang web không làm hai việc đó thay họ, và không
// bao giờ chạm vào đăng nhập Claude. Nút "Nối bộ não" chỉ mở link
// tpan-brain://connect: app TPAN Brain do `tpan brain-install` cài trên Mac
// mới là thứ mở Terminal chạy tpan brain (web/src/ai/BrainLink.tsx).

import { useQuery } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { BrainLinkButton } from "../../ai/BrainLink";
import { errorMessage, get } from "../../api/client";
import type { BrainStatus } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { CommandBlock } from "../../components/CommandBlock";
import { TextField } from "../../components/Field";
import { MonoText } from "../../components/MonoText";
import { Notice } from "../../components/Notice";
import { StatusLine, StatusMark } from "../../components/StatusMark";
import { Tag } from "../../components/Tag";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/cai-dat";
import { strings as khung } from "../../i18n/khung";
import { formatDateTime } from "../lich-su/format";
import { Row, Rows } from "./Rows";

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
    <div className="mt-[16px] flex max-w-[760px] flex-col gap-[20px] border-t border-hair pt-[16px]">
      {modeOff ? (
        <div>
          <h3 className="m-0 text-[14px] font-semibold">{t.brainEnableTitle}</h3>
          <p className="mt-[4px] mb-[8px] text-muted">{t.brainEnableBody}</p>
          <CommandBlock commands={enableCommands} />
        </div>
      ) : (
        <p className="m-0 font-semibold">
          <StatusLine status="pass">{t.brainEnableDone}</StatusLine>
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
            <TextField
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
            <div className="flex h-[44px] items-center sm:h-[40px]">
              <MonoText>{host}</MonoText>
            </div>
            <p className="mt-[6px] mb-0 text-[13px] text-muted">{t.brainHostHint}</p>
          </div>
        </div>
        {/* AI đã bật thì nối bằng một lần bấm (cần tpan brain-install trên Mac); AI còn tắt thì làm bước 1 trước. */}
        {!modeOff && (
          <div className="mb-[16px]">
            <BrainLinkButton target={target} />
          </div>
        )}
        <CommandBlock commands={connectCommands} addSudo={false} where={t.brainConnectWhere} />
      </div>
    </div>
  );
}

/** Claude Code đăng nhập bằng gói claude.ai thì không tính tiền theo API; mọi cách khác thì có. */
const planAuth = "claude.ai";

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
    <Card title={t.brainHeading} titleId="bo-nao-tieu-de">
      <p className="mt-0 mb-[14px] max-w-[72ch] text-muted">{t.brainIntro}</p>

      {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div role="alert">
          <Notice kind="fail">
            {t.brainLoadFailed} {errorMessage(q.error, lang)}
          </Notice>
          <Button className="mt-[12px]" onClick={() => void q.refetch()}>
            {k.retry}
          </Button>
        </div>
      )}
      {b && (
        <>
          <Rows>
            <Row label={t.brainModeLabel}>
              <Tag>{b.mode}</Tag>
              <p className="mt-[6px] mb-0">{modeText}</p>
            </Row>
            <Row label={t.brainStateLabel}>
              <StatusLine status={ready ? "pass" : "unknown"}>{b.mode === "demo" ? t.brainDemoReady : b.connected ? t.brainConnected : t.brainNotConnected}</StatusLine>
            </Row>
            {b.connected && b.host && (
              <Row label={t.brainHostLabel}>
                <MonoText>{b.host}</MonoText>
              </Row>
            )}
            {b.connected && b.claude_version && (
              <Row label={t.brainVersionLabel}>
                <MonoText>{b.claude_version}</MonoText>
                {versionDiffers && <Warn>{t.brainVersionWarn(b.claude_version, b.tested_version ?? "")}</Warn>}
              </Row>
            )}
            {b.connected && b.model && (
              <Row label={t.brainModelLabel}>
                <MonoText>{b.model}</MonoText>
              </Row>
            )}
            {b.connected && b.auth_method && (
              <Row label={t.brainAuthLabel}>
                <MonoText>{b.auth_method}</MonoText>
                {authIsApi && <Warn>{t.brainAuthWarn(b.auth_method)}</Warn>}
              </Row>
            )}
            {b.connected && b.since && (
              <Row label={t.brainSinceLabel}>
                <span title={b.since}>{formatDateTime(b.since, lang)}</span>
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
          </Rows>

          {/* Chưa nối: ba bước ở hai máy khác nhau, nên mỗi bước nói rõ chạy ở đâu. Demo thì không có gì để nối. */}
          {!b.connected && b.mode !== "demo" && <ConnectSteps modeOff={b.mode === "off"} />}
        </>
      )}
    </Card>
  );
}
