// Khối bằng chứng (mục 6): nền --pre, chữ mono 12 px, dòng 1,55, tự xuống
// dòng; dòng lệnh (bắt đầu bằng "$") đậm 600. Bấm vào khối là chép toàn bộ.
//
//   <EvidenceBlock evidence={check.evidence} />
//   <EvidenceBlock text={outputSoFar} label={null} />
//
// Có nhãn "BẰNG CHỨNG" ở trên; label={null} để bỏ, hoặc truyền nhãn khác.

import type { Evidence } from "../api/types";
import { useStrings } from "../i18n";
import { strings } from "../i18n/khung";
import { useCopy } from "./useCopy";

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

export function EvidenceBlock({ evidence, text, label }: Props) {
  const t = useStrings(strings);
  const { state, copy } = useCopy();
  const body = evidence ? evidenceText(evidence) : (text ?? "");
  const heading = label === undefined ? t.evidence : label;

  return (
    <div>
      {heading && <div className="mb-[6px] font-mono text-[11px] tracking-[0.08em] text-muted">{heading}</div>}
      {/* <button> chứa <span> khối chứ không chứa <pre>, vì nút chỉ được chứa phần tử dạng dòng. */}
      <button type="button" title={t.copy} onClick={() => copy(body)} className="block w-full select-text bg-pre p-[12px] text-left font-mono text-[12px] leading-[1.55] text-ink">
        {body.split("\n").map((line, i) => (
          <span key={i} className={`block whitespace-pre-wrap [overflow-wrap:anywhere] ${line.startsWith("$") ? "font-semibold" : ""}`}>
            {line || " "}
          </span>
        ))}
      </button>
      <span aria-live="polite" className={`text-[12px] ${state === "idle" ? "sr-only" : "text-muted"}`}>
        {state === "copied" ? t.copied : state === "failed" ? t.copyFailed : ""}
      </span>
    </div>
  );
}
