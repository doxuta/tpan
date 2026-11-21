// Test chân bảng của màn Cơ sở dữ liệu (mục 3.7 và 3.8 của
// docs/thay-doi/M16-thiet-ke.md). Chạy bằng `make test-web` (node --test, bỏ
// kiểu TypeScript khi chạy). Chuỗi viết thẳng ở đây vì file chuỗi thật nạp
// i18n/index.tsx, Node không đọc được .tsx.

import assert from "node:assert/strict";
import { test } from "node:test";
import { footerCounts, footerText } from "./footer.ts";

const t = {
  footerNone: "Chưa có cơ sở dữ liệu nào đang chạy",
  footerRunning: (n: number) => `${n} cơ sở dữ liệu`,
  footerAllSite: "đều là DB của site",
  footerAllStandalone: "đều là DB rời",
  footerSplit: (site: number, standalone: number) => `${site} của site, ${standalone} DB rời`,
  footerTrashed: (n: number) => `${n} trong thùng rác`,
};

type Row = { kind: "site" | "standalone"; status: "active" | "trashed" };
const rows = (site: number, standalone: number, trashedSite: number, trashedStandalone: number): Row[] => [
  ...Array.from({ length: site }, (): Row => ({ kind: "site", status: "active" })),
  ...Array.from({ length: standalone }, (): Row => ({ kind: "standalone", status: "active" })),
  ...Array.from({ length: trashedSite }, (): Row => ({ kind: "site", status: "trashed" })),
  ...Array.from({ length: trashedStandalone }, (): Row => ({ kind: "standalone", status: "trashed" })),
];

test("N chỉ đếm hàng đang chạy, cùng định nghĩa với db_count của menu", () => {
  assert.deepEqual(footerCounts(rows(7, 1, 1, 1)), { active: 8, site: 7, standalone: 1, trashed: 2 });
  assert.deepEqual(footerCounts([]), { active: 0, site: 0, standalone: 0, trashed: 0 });
});

test("câu chân bảng theo từng cách chia", () => {
  const cases: [Row[], string][] = [
    [rows(0, 0, 0, 0), "Chưa có cơ sở dữ liệu nào đang chạy"],
    [rows(0, 0, 1, 0), "Chưa có cơ sở dữ liệu nào đang chạy, 1 trong thùng rác"],
    [rows(5, 0, 0, 0), "5 cơ sở dữ liệu, đều là DB của site"],
    [rows(0, 2, 0, 0), "2 cơ sở dữ liệu, đều là DB rời"],
    [rows(7, 1, 1, 1), "8 cơ sở dữ liệu, 7 của site, 1 DB rời, 2 trong thùng rác"],
    [rows(3, 0, 0, 2), "3 cơ sở dữ liệu, đều là DB của site, 2 trong thùng rác"],
  ];
  for (const [r, want] of cases) assert.equal(footerText(t, footerCounts(r)), want);
});
