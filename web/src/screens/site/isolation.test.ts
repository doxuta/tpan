// Test gộp phép kiểm cô lập (bất biến B10, mục 4 của docs/thay-doi/M16-thiet-ke.md).
// Chạy bằng `make test-web` (node --test, bỏ kiểu TypeScript khi chạy).

import assert from "node:assert/strict";
import { test } from "node:test";
import type { CheckResult, Status } from "../../api/types";
import { groupIsolation, isolationEnds, touchesSite } from "./isolation.ts";

const check = (id: string, status: Status = "pass"): CheckResult => ({
  id,
  stage: "wordpress",
  title: { vi: id, en: id },
  status,
  summary: { vi: "", en: "" },
  evidence: null,
});
const iso = (from: string, to: string, status: Status = "pass") => check(`isolation:${from}:${to}`, status);

/** Hai chiều đều đạt giữa domain và mỗi site khác. */
const allPass = (domain: string, others: string[]): CheckResult[] => others.flatMap((o) => [iso(domain, o), iso(o, domain)]);

test("không có phép nào thì null", () => {
  assert.equal(groupIsolation([], "a.com"), null);
  assert.equal(groupIsolation([check("dns.a:a.com"), check("tls.cert:a.com")], "a.com"), null);
});

test("chỉ có phép của các site khác thì cũng null", () => {
  assert.equal(groupIsolation([iso("b.com", "c.com"), iso("c.com", "b.com")], "a.com"), null);
});

test("N site khác, 2N phép đều đạt: bad = 0, openIds rỗng", () => {
  const g = groupIsolation(allPass("a.com", ["b.com", "c.com", "d.com"]), "a.com");
  assert.ok(g);
  assert.equal(g.others, 3);
  assert.equal(g.total, 6);
  assert.equal(g.bad, 0);
  assert.equal(g.status, "pass");
  assert.deepEqual(g.openIds, []);
  assert.deepEqual(
    g.pairs.map((p) => p.other),
    ["b.com", "c.com", "d.com"],
  );
  assert.ok(g.pairs.every((p) => p.out && p.in));
});

test("một phép lỗi: openIds chỉ có id đó và cặp ấy đứng đầu", () => {
  // z.com đứng cuối theo bảng chữ cái; nó lên đầu chỉ vì có phép lỗi.
  const checks = [...allPass("a.com", ["b.com", "c.com"]), iso("a.com", "z.com"), iso("z.com", "a.com", "fail")];
  const g = groupIsolation(checks, "a.com");
  assert.ok(g);
  assert.deepEqual(g.openIds, ["isolation:z.com:a.com"]);
  assert.equal(g.pairs[0].other, "z.com");
  assert.equal(g.pairs[0].in?.id, "isolation:z.com:a.com");
  assert.equal(g.pairs[0].out?.id, "isolation:a.com:z.com");
  assert.deepEqual(
    g.pairs.slice(1).map((p) => p.other),
    ["b.com", "c.com"],
  );
  assert.equal(g.others, 3);
  assert.equal(g.total, 6);
  assert.equal(g.bad, 1);
  assert.equal(g.status, "fail");
});

test("phép không đạt mà chưa lỗi (warn, unknown): tính bad, đẩy cặp lên đầu, không mở sẵn", () => {
  const checks = [...allPass("a.com", ["b.com"]), iso("a.com", "y.com", "unknown"), iso("y.com", "a.com"), iso("a.com", "z.com", "warn"), iso("z.com", "a.com")];
  const g = groupIsolation(checks, "a.com");
  assert.ok(g);
  assert.equal(g.bad, 2);
  assert.equal(g.status, "warn");
  assert.deepEqual(g.openIds, []);
  assert.deepEqual(
    g.pairs.map((p) => p.other),
    ["y.com", "z.com", "b.com"],
  );
});

test("trạng thái xấu nhất theo pass < unknown < warn < fail", () => {
  const worst = (...s: Status[]) =>
    groupIsolation(
      s.map((st, i) => iso("a.com", `s${i}.com`, st)),
      "a.com",
    )?.status;
  assert.equal(worst("pass", "pass"), "pass");
  assert.equal(worst("pass", "unknown"), "unknown");
  assert.equal(worst("unknown", "warn", "pass"), "warn");
  assert.equal(worst("warn", "fail", "unknown"), "fail");
});

test("mọi phép fail đều vào openIds, theo thứ tự cặp rồi chiều ra, chiều vào", () => {
  const g = groupIsolation([iso("a.com", "c.com", "fail"), iso("c.com", "a.com", "fail"), iso("a.com", "b.com", "fail"), iso("b.com", "a.com")], "a.com");
  assert.ok(g);
  assert.deepEqual(g.openIds, ["isolation:a.com:b.com", "isolation:a.com:c.com", "isolation:c.com:a.com"]);
});

test("phép không thuộc site này bị bỏ qua", () => {
  const checks = [...allPass("a.com", ["b.com"]), iso("b.com", "c.com", "fail"), iso("c.com", "b.com", "fail"), check("dns.a:a.com", "fail")];
  const g = groupIsolation(checks, "a.com");
  assert.ok(g);
  assert.equal(g.others, 1);
  assert.equal(g.total, 2);
  assert.equal(g.bad, 0);
  assert.deepEqual(g.openIds, []);
});

test("tên miền là đuôi trùng của tên khác (a.com, aa.com) không bị nhận nhầm", () => {
  // Với domain a.com: aa.com chỉ là một site khác, và phép giữa aa.com với b.com không thuộc a.com.
  const g = groupIsolation([iso("aa.com", "b.com", "fail"), iso("b.com", "aa.com", "fail"), iso("a.com", "aa.com"), iso("aa.com", "a.com")], "a.com");
  assert.ok(g);
  assert.equal(g.others, 1);
  assert.equal(g.pairs[0].other, "aa.com");
  assert.equal(g.total, 2);
  assert.deepEqual(g.openIds, []);
  // Ngược lại, domain aa.com không nhận phép của a.com.
  const h = groupIsolation([iso("a.com", "b.com", "fail"), iso("b.com", "a.com", "fail")], "aa.com");
  assert.equal(h, null);
});

test("tên miền là đầu trùng của tên khác (a.com, a.com.vn) không bị nhận nhầm", () => {
  assert.equal(groupIsolation([iso("a.com.vn", "b.com", "fail"), iso("b.com", "a.com.vn", "fail")], "a.com"), null);
  const g = groupIsolation([iso("a.com.vn", "b.com", "fail"), iso("a.com", "b.com"), iso("b.com", "a.com")], "a.com");
  assert.ok(g);
  assert.equal(g.others, 1);
  assert.equal(g.total, 2);
  assert.equal(g.bad, 0);
  // Và domain a.com.vn chỉ thấy phép của chính nó.
  const v = groupIsolation([iso("a.com.vn", "b.com", "fail"), iso("a.com", "b.com"), iso("b.com", "a.com")], "a.com.vn");
  assert.ok(v);
  assert.equal(v.total, 1);
  assert.deepEqual(v.openIds, ["isolation:a.com.vn:b.com"]);
});

test("id có số phần khác hai sau khi tách bằng dấu hai chấm bị bỏ qua", () => {
  const odd = [check("isolation:a.com", "fail"), check("isolation:a.com:b.com:c.com", "fail"), check("isolation:", "fail"), check("isolation:a.com:b.com:", "fail"), check("isolation::", "pass")];
  assert.equal(groupIsolation(odd, "a.com"), null);
  const g = groupIsolation([...odd, ...allPass("a.com", ["b.com"])], "a.com");
  assert.ok(g);
  assert.equal(g.total, 2);
  assert.equal(g.bad, 0);
  assert.deepEqual(g.openIds, []);
});

test("phép giữa site này với chính nó bị bỏ qua", () => {
  assert.equal(groupIsolation([iso("a.com", "a.com", "fail")], "a.com"), null);
});

test("cặp chỉ có một chiều vẫn được gộp", () => {
  const g = groupIsolation([iso("b.com", "a.com", "fail")], "a.com");
  assert.ok(g);
  assert.equal(g.total, 1);
  assert.equal(g.pairs[0].out, undefined);
  assert.equal(g.pairs[0].in?.id, "isolation:b.com:a.com");
});

test("isolationEnds và touchesSite dùng cùng cách tách (cho thẻ Sự cố của site)", () => {
  assert.deepEqual(isolationEnds("isolation:a.com:b.com"), ["a.com", "b.com"]);
  assert.equal(isolationEnds("isolation:a.com"), null);
  assert.equal(isolationEnds("isolation:a.com:b.com:c.com"), null);
  assert.equal(isolationEnds("tls.cert:a.com"), null);
  assert.equal(touchesSite("isolation:a.com:b.com", "a.com"), true);
  assert.equal(touchesSite("isolation:b.com:a.com", "a.com"), true);
  assert.equal(touchesSite("isolation:aa.com:b.com", "a.com"), false);
  assert.equal(touchesSite("isolation:a.com.vn:b.com", "a.com"), false);
  assert.equal(touchesSite("isolation:b.com:aa.com", "a.com"), false);
  assert.equal(touchesSite("isolation:a.com:b.com:c", "a.com"), false);
});
