// Các lần gọi công cụ của một phiên (bảng ai_calls). Mỗi dòng mở ra thấy đúng
// đầu vào AI gửi và đầu ra tpan trả về, cả hai là bản đã che. Dòng đóng sẵn,
// trừ dòng đang lỗi; đầu ra dài chỉ hiện 20 dòng đầu (EvidenceBlock clamp): một
// lần đọc log có thể dài cả nghìn dòng.

import { Link } from "react-router";
import type { AICall } from "../../api/types";
import { Disclosure } from "../../components/Disclosure";
import { EmptyState } from "../../components/EmptyState";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { Code } from "../../components/MonoText";
import { StatusLine } from "../../components/StatusMark";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/phien-ai";
import { formatClock } from "../lich-su/format";

/** Đầu ra dài hơn chừng này dòng thì chỉ vẽ chừng đó dòng đầu, kèm nút "Xem hết N dòng". */
const clampLines = 20;

/** Tên công cụ bỏ tiền tố của MCP server (mcp__tpan__site_health thành site_health). */
function toolName(full: string): string {
  return full.replace(/^mcp__tpan__/, "");
}

/** Đầu vào hoặc đầu ra bất kỳ thành chữ: chuỗi giữ nguyên, còn lại là JSON thụt lề. */
function pretty(v: unknown): string {
  if (v === undefined || v === null) return "";
  if (typeof v === "string") return v;
  return JSON.stringify(v, null, 2) ?? "";
}

export function CallList({ calls }: { calls: AICall[] }) {
  const t = useStrings(strings);
  if (calls.length === 0) return <EmptyState>{t.noCalls}</EmptyState>;
  return (
    <ul aria-label={t.callsLabel} className="m-0 list-none p-0">
      {calls.map((c, i) => (
        <li key={`${c.time}|${c.tool}|${i}`} className="border-t border-hair first:border-t-0">
          <CallRow call={c} />
        </li>
      ))}
    </ul>
  );
}

function CallRow({ call }: { call: AICall }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const name = toolName(call.tool);
  const input = pretty(call.input);
  const output = pretty(call.output);
  return (
    <>
      <Disclosure
        flush
        defaultOpen={!!call.error}
        summary={
          <span className="flex min-w-0 flex-wrap items-center gap-x-[10px] gap-y-[4px]">
            <span title={call.time} className="text-[13px] text-muted">
              {formatClock(call.time, lang)}
            </span>
            <Tag>{name.startsWith("propose_") ? t.toolPlan : t.toolRead}</Tag>
            <Code>{name}</Code>
            {call.error && <StatusLine status="fail">{t.callFailed}</StatusLine>}
          </span>
        }
      >
        <div className="flex flex-col gap-[10px]">
          {input && <EvidenceBlock text={input} label={t.callInput} clamp={clampLines} />}
          {call.error && <EvidenceBlock text={call.error} label={t.callError} clamp={clampLines} />}
          {output ? <EvidenceBlock text={output} label={t.callOutput} clamp={clampLines} /> : !call.error && <p className="m-0 text-muted">{t.callNoOutput}</p>}
        </div>
      </Disclosure>
      {call.plan_id && (
        <p className="m-0 pb-[10px] text-[13px]">
          <Link to={`/plans/${encodeURIComponent(call.plan_id)}`}>{t.planOpen(call.plan_id)}</Link>
        </p>
      )}
    </>
  );
}
