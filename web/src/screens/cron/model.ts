// Phần không dính giao diện của màn Cron: khoá truy vấn, đường dẫn API (khớp
// internal/server/types_m9.go), đọc dòng lịch cron ra nhịp chạy, đổi thời gian
// chạy ra chữ.
//
// Khoá truy vấn khai báo ở đây chứ không thêm vào api/queries.ts (file đó của
// phiên chính).

import type { CronStatus, Status } from "../../api/types";

export const cronKey = ["cron"] as const;
export const cronPath = "/api/v1/cron";

/** Nhịp chạy đọc được từ năm trường của cron. Dạng nào không nhận ra thì "other" và màn hiện nguyên dòng gốc. */
export type Every =
  | { kind: "minutes"; n: number }
  | { kind: "hourly"; minute: string }
  | { kind: "daily"; hhmm: string }
  | { kind: "other" };

const pad = (s: string) => s.padStart(2, "0");

// Chỉ nhận ba dạng mà TPAN ghi ra: "*/N * * * *" (site, 5 phút), "M * * * *" và
// "M H * * *" (lịch backup hằng ngày). Không viết bộ đọc cron đủ cú pháp: máy
// chủ chỉ trả lịch do TPAN định nghĩa (mục 6.10), nên dạng lạ hiện nguyên dòng
// gốc còn đúng hơn đoán sai.
export function parseSchedule(schedule: string): Every {
  const s = schedule.trim();
  let m = /^\*\/(\d{1,2}) \* \* \* \*$/.exec(s);
  if (m && Number(m[1]) >= 1) return { kind: "minutes", n: Number(m[1]) };
  m = /^(\d{1,2}) \* \* \* \*$/.exec(s);
  if (m && Number(m[1]) <= 59) return { kind: "hourly", minute: pad(m[1]) };
  m = /^(\d{1,2}) (\d{1,2}) \* \* \*$/.exec(s);
  if (m && Number(m[1]) <= 59 && Number(m[2]) <= 23) return { kind: "daily", hhmm: `${pad(m[2])}:${pad(m[1])}` };
  return { kind: "other" };
}

/** Dấu trạng thái của khung có bốn giá trị; "bỏ qua" không phải lỗi mà cũng không phải đạt, nên là "chưa kiểm được". */
export const markOf: Record<CronStatus, Status> = { ok: "pass", fail: "fail", skipped: "unknown" };

/** Thời gian chạy: dưới 1 giây tính bằng ms, dưới 1 phút bằng giây (một chữ số lẻ), còn lại phút và giây. Số âm hay NaN coi là 0. */
export function splitDuration(ns: number): { kind: "ms"; n: number } | { kind: "sec"; n: number } | { kind: "min"; m: number; s: number } {
  const ms = Number.isFinite(ns) && ns > 0 ? ns / 1e6 : 0;
  if (ms < 1000) return { kind: "ms", n: Math.round(ms) };
  if (ms < 60_000) return { kind: "sec", n: Math.round(ms / 100) / 10 };
  const total = Math.round(ms / 1000);
  return { kind: "min", m: Math.floor(total / 60), s: total % 60 };
}
