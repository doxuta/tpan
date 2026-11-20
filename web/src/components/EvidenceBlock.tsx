// Khối bằng chứng (mục 6): nền --pre, chữ mono 12 px, dòng 1,55, tự xuống
// dòng; dòng lệnh (bắt đầu bằng "$ ") đậm 600. Bấm vào khối là chép toàn bộ.
// Dấu "$ " chỉ để nhìn, giống CommandBlock: không bôi đen được và không có
// trong chữ được chép, nên dán vào terminal không dính "$".
//
//   <EvidenceBlock evidence={check.evidence} />
//   <EvidenceBlock text={outputSoFar} label={null} />
//
// Có nhãn "BẰNG CHỨNG" ở trên; label={null} để bỏ, hoặc truyền nhãn khác.

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

export function EvidenceBlock({ evidence, text, label }: Props) {
  const t = useStrings(strings);
  const { state, copy } = useCopy();
  const body = evidence ? evidenceText(evidence) : (text ?? "");
  const heading = label === undefined ? t.evidence : label;

  return (
    <div>
      {heading && <div className="mb-[6px] font-mono text-[11px] tracking-[0.08em] text-muted">{heading}</div>}
      {/* <button> chứa <span> khối chứ không chứa <pre>, vì nút chỉ được chứa phần tử dạng dòng. */}
      <button type="button" title={t.copy} onClick={() => copy(copyText(body))} className="block w-full select-text bg-pre p-[12px] text-left font-mono text-[12px] leading-[1.55] text-ink">
        {body.split("\n").map((line, i) =>
          line.startsWith(PROMPT) ? (
            <span key={i} className="block whitespace-pre-wrap [overflow-wrap:anywhere] font-semibold">
              {/* "$ " chỉ để nhìn: select-none nên bôi đen tay cũng không dính. */}
              <span aria-hidden="true" className="select-none">
                {PROMPT}
              </span>
              {line.slice(PROMPT.length)}
            </span>
          ) : (
            <span key={i} className="block whitespace-pre-wrap [overflow-wrap:anywhere]">
              {line || " "}
            </span>
          ),
        )}
      </button>
      <span aria-live="polite" className={`text-[12px] ${state === "idle" ? "sr-only" : "text-muted"}`}>
        {state === "copied" ? t.copied : state === "failed" ? t.copyFailed : ""}
      </span>
    </div>
  );
}
