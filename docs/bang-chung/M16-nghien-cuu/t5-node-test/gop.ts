// Bản thử (đợt 0 của M16): một hàm thuần viết bằng TypeScript, chỉ import kiểu,
// để xem `node --test` của Node đang cài chạy thẳng được file .ts không.
import type { Dong } from "./kieu.ts";

export function demDat(ds: readonly Dong[]): number {
  return ds.filter((d) => d.status === "pass").length;
}
