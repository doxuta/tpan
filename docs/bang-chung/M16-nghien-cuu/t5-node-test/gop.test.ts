import assert from "node:assert/strict";
import { test } from "node:test";
import { demDat } from "./gop.ts";

test("đếm số dòng đạt", () => {
  assert.equal(demDat([{ id: "a", status: "pass" }, { id: "b", status: "fail" }]), 1);
});
