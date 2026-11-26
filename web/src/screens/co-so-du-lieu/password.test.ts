// Test luật mật khẩu DB tự đặt (M36). Chạy bằng `make test-web`. Cùng bộ ca với
// TestCheckChosenDBPassword của máy chủ, để hai bên không lệch nhau.

import assert from "node:assert/strict";
import { test } from "node:test";
import { dbPasswordProblem } from "./password.ts";

test("luật mật khẩu DB tự đặt khớp máy chủ", () => {
  const cases: [string, string | null][] = [
    ["Ab3-kho-bi-mat", null],
    ["x9.y8:z7@w6^v5~", null],
    ["a1".repeat(32), null],
    ["Ab3-ngan", "short"],
    ["a1".repeat(32) + "x", "long"],
    ["co khoang trang 1", "chars"],
    ["co'nhay-don-1234", "chars"],
    ["co\\gach-nguoc-123", "chars"],
    ["co$do-la-12345", "chars"],
    ["co&va;cham-phay1", "chars"],
    ["Mật-khẩu-có-dấu-123", "chars"],
    ["chicochucai-khongso", "classes"],
    ["123456789012345", "classes"],
    ["mat-khau-alice_shop-1", "name"],
  ];
  for (const [pw, want] of cases) assert.equal(dbPasswordProblem(pw, "alice_shop"), want, pw);
});
