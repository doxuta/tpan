// Các lần gọi công cụ của một phiên (bảng ai_calls). Mỗi dòng mở ra thấy đúng
// đầu vào AI gửi và đầu ra tpan trả về, cả hai là bản đã che. Đầu ra dài thì
// thu gọn: một lần đọc log có thể dài cả nghìn dòng.

import { useState } from "react";
import { Link } from "react-router";
import type { AICall } from "../../api/types";
import { Button } from "../../components/Button";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/phien-ai";
import { formatClock } from "../lich-su/format";

/** Quá số dòng này thì thu gọn, chỉ hiện collapsedLines dòng đầu. */
const longLines = 20;
const collapsedLines = 12;

const tag = "border border-ink px-[5px] font-mono text-[11px] tracking-[0.06em] text-ink";

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
  if (calls.length === 0) return <p className="m-0 border-t border-hair py-[10px] text-muted">{t.noCalls}</p>;
  return (
    <ul aria-label={t.callsLabel} className="m-0 list-none border-b border-hair p-0">
      {calls.map((c, i) => (
        <CallRow key={`${c.time}|${c.tool}|${i}`} call={c} />
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
    <li className="border-t border-hair">
      <details>
        <summary className="flex min-h-[44px] cursor-pointer list-none flex-wrap items-center gap-x-[10px] gap-y-[4px] py-[7px] sm:min-h-[36px] [&::-webkit-details-marker]:hidden">
          <span title={call.time} className="font-mono text-[12px] text-muted">
            {formatClock(call.time, lang)}
          </span>
          <span className={tag}>{name.startsWith("propose_") ? t.toolPlan : t.toolRead}</span>
          <span className="min-w-0 flex-1 basis-[170px] font-mono text-[12px] [overflow-wrap:anywhere]">{name}</span>
          {call.error && <span className="text-[12px] text-muted">{t.callFailed}</span>}
        </summary>
        <div className="flex flex-col gap-[10px] pb-[12px]">
          {input && <EvidenceBlock text={input} label={t.callInput} />}
          {call.error && <EvidenceBlock text={call.error} label={t.callError} />}
          {output ? <Collapsible text={output} label={t.callOutput} /> : !call.error && <p className="m-0 text-muted">{t.callNoOutput}</p>}
        </div>
      </details>
      {call.plan_id && (
        <p className="m-0 pb-[8px]">
          <Link to={`/plans/${encodeURIComponent(call.plan_id)}`} className="text-[13px]">
            {t.planOpen(call.plan_id)}
          </Link>
        </p>
      )}
    </li>
  );
}

/** Khối chữ dài thu gọn kèm nút "Hiện hết"; ngắn thì hiện thẳng. */
function Collapsible({ text, label }: { text: string; label: string }) {
  const t = useStrings(strings);
  const [all, setAll] = useState(false);
  const lines = text.split("\n");
  if (lines.length <= longLines) return <EvidenceBlock text={text} label={label} />;
  return (
    <div>
      <EvidenceBlock text={all ? text : lines.slice(0, collapsedLines).join("\n")} label={label} />
      <div className="mt-[6px] flex flex-wrap items-center gap-[10px] text-muted">
        {!all && <span>{t.moreLines(lines.length - collapsedLines)}</span>}
        <Button aria-expanded={all} onClick={() => setAll(!all)}>
          {all ? t.showLess : t.showAll}
        </Button>
      </div>
    </div>
  );
}
