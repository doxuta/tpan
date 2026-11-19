// Dựng bản xem của một job từ danh sách sự kiện (engine.Event qua API).
//
// Dựng theo THỨ TỰ sự kiện, không gom theo step_id: hoàn tác dùng lại id của
// bước đã chạy, và lệnh hoàn tác cũng mang id đó, nên gom theo id sẽ trộn lần
// chạy với lần hoàn tác của cùng một bước. Mỗi step.start và mỗi undo.start mở
// một khối mới; command/output gắn vào khối đang mở.

import type { CheckResult, JobEvent } from "../../api/types";

export type StepState = "running" | "skipped" | "done" | "failed";
export type UndoState = "running" | "done" | "failed";

export type Block<S extends string> = {
  id: string;
  state: S;
  /** Dòng log theo thứ tự: lệnh bắt đầu bằng "$ " (EvidenceBlock in đậm), rồi output. */
  lines: string[];
  started: string;
  ended?: string;
  error?: string;
};

export type JobView = {
  steps: Block<StepState>[];
  undo: Block<UndoState>[];
  checks: CheckResult[];
  /** Message của job.done (JobStatus) khi đã có. */
  doneStatus?: string;
  doneTime?: string;
  /** Vị trí (từ 0) của bước hỏng trong steps, -1 nếu không có bước nào hỏng. */
  failedIndex: number;
  lastSeq: number;
};

export function buildView(events: JobEvent[]): JobView {
  const v: JobView = { steps: [], undo: [], checks: [], failedIndex: -1, lastSeq: 0 };
  // Khối bước và khối hoàn tác đang mở; command/output gắn vào khối mở gần nhất.
  let step: Block<StepState> | null = null;
  let undo: Block<UndoState> | null = null;
  let cur: Block<StepState> | Block<UndoState> | null = null;

  for (const e of events) {
    v.lastSeq = Math.max(v.lastSeq, e.seq);
    switch (e.kind) {
      case "step.start":
        step = { id: e.step_id ?? "", state: "running", lines: [], started: e.time };
        v.steps.push(step);
        cur = step;
        break;
      case "step.skip":
      case "step.done":
      case "step.fail":
        if (!step) break;
        step.state = e.kind === "step.skip" ? "skipped" : e.kind === "step.done" ? "done" : "failed";
        step.ended = e.time;
        if (e.kind === "step.fail") {
          step.error = e.message;
          v.failedIndex = v.steps.length - 1;
        }
        break;
      case "undo.start":
        undo = { id: e.step_id ?? "", state: "running", lines: [], started: e.time };
        v.undo.push(undo);
        cur = undo;
        break;
      case "undo.done":
      case "undo.fail":
        if (!undo) break;
        undo.state = e.kind === "undo.done" ? "done" : "failed";
        undo.ended = e.time;
        if (e.kind === "undo.fail") undo.error = e.message;
        break;
      case "command":
        cur?.lines.push(`$ ${e.message ?? ""}`);
        break;
      case "output":
        cur?.lines.push(e.message ?? "");
        break;
      case "check":
        if (e.check) v.checks.push(e.check);
        break;
      case "job.done":
        v.doneStatus = e.message;
        v.doneTime = e.time;
        break;
      case "job.start":
        break;
    }
  }
  return v;
}

/** Số mili giây giữa hai mốc RFC 3339; không đọc được thì null. */
export function elapsedMs(from: string, to: string): number | null {
  const a = Date.parse(from);
  const b = Date.parse(to);
  return Number.isNaN(a) || Number.isNaN(b) ? null : Math.max(0, b - a);
}
