// Mục "Agent và MCP" ở màn Cài đặt (mục 8.3 và mục 10 của KE-HOACH.md):
// GET /api/v1/agent. Chỉ để xem: chế độ duyệt và mức che đổi bằng
// `sudo tpan mcp config` trên máy chủ, vì chế độ client cho agent tự chạy Plan
// nên không để một cú bấm trên web bật được nó.

import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { errorMessage, get } from "../../api/client";
import type { AgentInfo } from "../../api/types";
import { Button } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/agent";
import { strings as khung } from "../../i18n/khung";

const labelClass = "font-mono text-[11px] tracking-[0.08em] text-muted";

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="border-t border-hair py-[12px]">
      <dt className={labelClass}>{label}</dt>
      <dd className="m-0 mt-[6px]">{children}</dd>
    </div>
  );
}

export function Agent() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({ queryKey: ["agent"], queryFn: () => get<AgentInfo>("/api/v1/agent") });
  const a = q.data;

  return (
    <section aria-labelledby="agent-tieu-de" className="pt-[28px]">
      <h2 id="agent-tieu-de" className="m-0 text-[16px] font-semibold">
        {t.agentHeading}
      </h2>
      <p className="mt-[6px] mb-[14px] text-muted">{t.agentIntro}</p>

      {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div role="alert">
          <p className="m-0">
            {t.agentLoadFailed} {errorMessage(q.error, lang)}
          </p>
          <Button className="mt-[12px]" onClick={() => void q.refetch()}>
            {k.retry}
          </Button>
        </div>
      )}
      {a && (
        <dl className="m-0 max-w-[760px]">
          <Row label={t.approvalLabel}>
            <Tag>{a.approval}</Tag>
            <p className="mt-[6px] mb-0">{a.approval === "client" ? t.approvalClient : t.approvalPanel}</p>
          </Row>
          <Row label={t.redactLabel}>
            <Tag>{a.redact}</Tag>
            <p className="mt-[6px] mb-0">{a.redact === "all" ? t.redactAll : t.redactSecrets}</p>
          </Row>
          <Row label={t.claudeCmdLabel}>
            <MonoText size={12}>{a.claude_command}</MonoText>
          </Row>
          <Row label={t.codexCmdLabel}>
            <MonoText size={12}>{a.codex_command}</MonoText>
            <p className="mt-[8px] mb-0 text-[13px] text-muted">{t.cmdHint(a.hostname)}</p>
          </Row>
          <Row label={t.changeLabel}>
            <p className="mt-0 mb-[6px] text-muted">{t.changeHint}</p>
            <MonoText size={12}>{t.changeCmd}</MonoText>
          </Row>
        </dl>
      )}
    </section>
  );
}
