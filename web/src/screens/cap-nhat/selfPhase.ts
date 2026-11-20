// Khối "Phiên bản và cập nhật" đang ở chặng nào (mục 6.14 của KE-HOACH.md,
// quyết định 5 và 9). Hàm thuần: không gọi API, không đọc đồng hồ, nên đọc là
// thấy hết luật và thử được bằng tay với vài bộ số.
//
//   selfPhase(state, sessionVersion, Date.now())
//
// state là câu trả lời gần nhất còn giữ của GET /self (lúc panel restart nó
// là bản cũ, vẫn dùng được). sessionVersion là version của GET /session hỏi
// SAU khi lượt này bắt đầu; chưa hỏi được thì undefined.

import type { SelfAction, SelfState } from "../../api/m15-types";

/** Quá chừng này kể từ lúc bấm mà chưa có kết quả thì đưa lệnh gỡ bằng tay. */
export const stuckAfterMs = 3 * 60 * 1000;

export type SelfPhase =
  /** Chưa có lượt nào chạy từ panel. */
  | { kind: "idle" }
  /** Đang chạy, chưa có kết quả; panel có thể đang restart. */
  | { kind: "updating"; action: SelfAction }
  /** File chạy đã là bản khác: to là bản đang chạy bây giờ. */
  | { kind: "done"; action: SelfAction; to: string }
  /** Bản tải về trùng bản đang chạy, không thay gì. */
  | { kind: "same" }
  /** Job hỏng và đã hoàn tác: file chạy cũ còn nguyên, lý do ở last.error. */
  | { kind: "failed"; action: SelfAction }
  /** Bị ngắt giữa chừng, hoặc quá 3 phút không có kết quả: gỡ bằng tay. */
  | { kind: "stuck"; action: SelfAction };

export function selfPhase(state: SelfState, sessionVersion: string | undefined, now: number): SelfPhase {
  const last = state.last;
  // busy mà chưa có last: đơn vị tạm của trình duyệt khác vừa khởi, máy chủ chưa kịp trả lượt chạy.
  if (!last) return state.busy ? { kind: "updating", action: "update" } : { kind: "idle" };

  const { action } = last;
  switch (last.status) {
    case "ok":
      return { kind: "done", action, to: last.to || sessionVersion || state.version };
    case "same":
      return { kind: "same" };
    case "failed":
      return { kind: "failed", action };
    case "interrupted":
      return { kind: "stuck", action };
  }

  // Còn "running". Panel đã lên lại với bản khác bản lúc bấm thì coi như xong,
  // dù đơn vị tạm chưa ghi kết quả (nó ghi sau bước restart). Nếu bước cuối
  // hỏng và job hoàn tác, nhịp hỏi sau trả "failed" và màn đổi theo.
  const running = [sessionVersion, state.version].find((v) => v && v !== last.from);
  if (running) return { kind: "done", action, to: running };
  if (now - Date.parse(last.started) > stuckAfterMs) return { kind: "stuck", action };
  return { kind: "updating", action };
}
