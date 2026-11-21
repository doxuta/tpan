// Khối bằng chứng (mục 6): nền --pre, bo 8 px, chữ mono 12,5 px, dòng 1,55, tự
// xuống dòng; dòng lệnh (bắt đầu bằng "$ ") đậm 600. Bấm vào khối là chép TOÀN
// BỘ, kể cả phần đang thu gọn. Dấu "$ " chỉ để nhìn, giống CommandBlock: không
// bôi đen được và không có trong chữ được chép, nên dán vào terminal không dính "$".
//
//   <EvidenceBlock evidence={check.evidence} />
//   <EvidenceBlock evidence={check.evidence} clamp={20} />    (chỉ vẽ 20 dòng đầu, có nút "Xem hết")
//   <EvidenceBlock text={outputSoFar} label={null} />
//
// Có nhãn "Bằng chứng" ở trên; label={null} để bỏ, hoặc truyền nhãn khác.

import { useState } from "react";
import type { Evidence } from "../api/types";
import { useStrings } from "../i18n";
import { strings } from "../i18n/khung";
import { useCopy } from "./useCopy";

/** Dấu nhắc đầu dòng lệnh. Chỉ để nhìn: không bôi đen được, không được chép. */
const PROMPT = "$ ";

type Props = {
  /** Lệnh và output thật của phép kiểm. Có evidence thì không cần text. */
  evidence?: Evidence[] | null;
  /** Chữ thô (ví dụ output job đang chạy). Dòng bắt đầu bằng "$ " được in đậm. */
  text?: string;
  label?: string | null;
  /** Dài hơn chừng này dòng thì chỉ vẽ chừng đó dòng đầu và nút "Xem hết N dòng". */
  clamp?: number;
};

/** Ghép bằng chứng thành chữ: mỗi lệnh một dòng "$ ...", rồi output của nó. */
export function evidenceText(evidence: Evidence[]): string {
  return evidence
    .map((e) => {
      const out = e.output.replace(/\n+$/, "");
      return out ? `$ ${e.command}\n${out}` : `$ ${e.command}`;
    })
    .join("\n");
}

/** Chữ được chép: đúng chữ hiện ra, chỉ bỏ dấu "$ " ở đầu các dòng lệnh. */
export function copyText(body: string): string {
  return body
    .split("\n")
    .map((line) => (line.startsWith(PROMPT) ? line.slice(PROMPT.length) : line))
    .join("\n");
}

export function EvidenceBlock({ evidence, text, label, clamp }: Props) {
  const t = useStrings(strings);
  const { state, copy } = useCopy();
  const [all, setAll] = useState(false);
  const body = evidence ? evidenceText(evidence) : (text ?? "");
  const heading = label === undefined ? t.evidence : label;
  const lines = body.split("\n");
  const cut = clamp !== undefined && !all && lines.length > clamp;
  const shown = cut ? lines.slice(0, clamp) : lines;

  return (
    <div>
      {heading && <div className="mb-[6px] text-[12px] font-medium text-muted">{heading}</div>}
      {/* <button> chứa <span> khối chứ không chứa <pre>, vì nút chỉ được chứa phần tử dạng dòng. */}
      <button type="button" title={t.copy} onClick={() => copy(copyText(body))} className="block w-full rounded-ctl bg-pre p-[12px] text-left font-mono text-[12.5px] leading-[1.55] text-ink select-text">
        {shown.map((line, i) =>
          line.startsWith(PROMPT) ? (
            <span key={i} className="block font-semibold whitespace-pre-wrap [overflow-wrap:anywhere]">
              {/* "$ " chỉ để nhìn: select-none nên bôi đen tay cũng không dính. */}
              <span aria-hidden="true" className="select-none">
                {PROMPT}
              </span>
              {line.slice(PROMPT.length)}
            </span>
          ) : (
            <span key={i} className="block whitespace-pre-wrap [overflow-wrap:anywhere]">
              {line || " "}
            </span>
          ),
        )}
      </button>
      {clamp !== undefined && lines.length > clamp && (
        <button type="button" aria-expanded={all} onClick={() => setAll(!all)} className="mt-[6px] min-h-[24px] bg-transparent p-0 text-[13px] font-medium text-accent hover:underline">
          {all ? t.collapse : t.showAll(lines.length)}
        </button>
      )}
      <span aria-live="polite" className={`text-[12px] ${state === "idle" ? "sr-only" : "ml-[8px] text-muted"}`}>
        {state === "copied" ? t.copied : state === "failed" ? t.copyFailed : ""}
      </span>
    </div>
  );
}
