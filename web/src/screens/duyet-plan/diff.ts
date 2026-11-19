// Diff theo dòng cho file thay đổi trong bản xem trước của Plan.
//
// API gửi nội dung file dạng base64 (FileChange.old / .new, []byte bên Go). Hai
// điều dễ sai:
//   - Go bỏ trường rỗng (omitempty): file mới rỗng thì không có "new", file cũ
//     rỗng thì không có "old". Thiếu trường nghĩa là nội dung rỗng.
//   - old_exists=false là file chưa có: mọi dòng của "new" là dòng thêm.

import type { FileChange } from "../../api/types";

export type DiffLine =
  | { kind: "add" | "del" | "ctx"; text: string }
  /** Một đoạn dòng không đổi nằm xa chỗ thay đổi, được gập lại. */
  | { kind: "gap"; count: number };

export type FileDiff =
  | { binary: true }
  | { binary: false; lines: DiffLine[]; added: number; removed: number; changed: boolean };

const contextLines = 3;
/** Quá ngưỡng này (số dòng cũ × số dòng mới sau khi cắt đầu đuôi giống nhau) thì khỏi tìm đoạn chung. */
const maxCells = 1_000_000;

/** base64 → chữ UTF-8. null nếu không phải chữ (nhị phân hoặc UTF-8 hỏng). */
export function decodeText(b64: string | undefined): string | null {
  if (!b64) return "";
  let bytes: Uint8Array;
  try {
    bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
  } catch {
    return null;
  }
  if (bytes.includes(0)) return null;
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    return null;
  }
}

function toLines(s: string): string[] {
  if (s === "") return [];
  const lines = s.split("\n");
  if (lines.at(-1) === "") lines.pop(); // dấu xuống dòng cuối file không phải một dòng thêm
  return lines;
}

export function diffFile(f: FileChange): FileDiff {
  const oldText = f.old_exists ? decodeText(f.old) : "";
  const newText = f.delete ? "" : decodeText(f.new);
  if (oldText === null || newText === null) return { binary: true };
  const a = toLines(oldText);
  const b = toLines(newText);
  const ops = diffOps(a, b);
  const added = ops.filter((o) => o.kind === "add").length;
  const removed = ops.filter((o) => o.kind === "del").length;
  return { binary: false, lines: collapse(ops), added, removed, changed: added + removed > 0 };
}

type Op = { kind: "add" | "del" | "ctx"; text: string };

function diffOps(a: string[], b: string[]): Op[] {
  // Cắt phần đầu và phần đuôi giống nhau: thường chỉ còn vài dòng ở giữa.
  let head = 0;
  while (head < a.length && head < b.length && a[head] === b[head]) head++;
  let tail = 0;
  while (tail < a.length - head && tail < b.length - head && a[a.length - 1 - tail] === b[b.length - 1 - tail]) tail++;
  const x = a.slice(head, a.length - tail);
  const y = b.slice(head, b.length - tail);

  const ops: Op[] = a.slice(0, head).map((text) => ({ kind: "ctx", text }));
  ops.push(...middle(x, y));
  ops.push(...a.slice(a.length - tail).map((text): Op => ({ kind: "ctx", text })));
  return ops;
}

/** Đoạn khác nhau ở giữa, bằng dãy con chung dài nhất theo dòng. */
function middle(x: string[], y: string[]): Op[] {
  if (x.length === 0) return y.map((text) => ({ kind: "add", text }));
  if (y.length === 0) return x.map((text) => ({ kind: "del", text }));
  // ponytail: file quá lớn thì coi như thay hết (bỏ hết dòng cũ, thêm hết dòng mới). Vẫn
  // đúng, chỉ dài hơn diff tối ưu; file cấu hình thật không bao giờ chạm ngưỡng này.
  if (x.length * y.length > maxCells) return [...x.map((text): Op => ({ kind: "del", text })), ...y.map((text): Op => ({ kind: "add", text }))];

  const w = y.length + 1;
  const lcs = new Uint32Array((x.length + 1) * w);
  for (let i = x.length - 1; i >= 0; i--) {
    for (let j = y.length - 1; j >= 0; j--) {
      lcs[i * w + j] = x[i] === y[j] ? lcs[(i + 1) * w + j + 1]! + 1 : Math.max(lcs[(i + 1) * w + j]!, lcs[i * w + j + 1]!);
    }
  }
  const out: Op[] = [];
  let i = 0;
  let j = 0;
  while (i < x.length && j < y.length) {
    if (x[i] === y[j]) {
      out.push({ kind: "ctx", text: x[i]! });
      i++;
      j++;
    } else if (lcs[(i + 1) * w + j]! >= lcs[i * w + j + 1]!) {
      out.push({ kind: "del", text: x[i++]! });
    } else {
      out.push({ kind: "add", text: y[j++]! });
    }
  }
  while (i < x.length) out.push({ kind: "del", text: x[i++]! });
  while (j < y.length) out.push({ kind: "add", text: y[j++]! });
  return out;
}

/** Giữ contextLines dòng không đổi quanh mỗi chỗ thay đổi, gập phần còn lại. */
function collapse(ops: Op[]): DiffLine[] {
  const near = new Array<boolean>(ops.length).fill(false);
  ops.forEach((o, i) => {
    if (o.kind === "ctx") return;
    for (let k = Math.max(0, i - contextLines); k <= Math.min(ops.length - 1, i + contextLines); k++) near[k] = true;
  });
  // File không đổi gì thì không có dòng nào "gần": hiện hết thay vì gập thành một dòng.
  if (!ops.some((o) => o.kind !== "ctx")) return ops;
  const out: DiffLine[] = [];
  let gap = 0;
  ops.forEach((o, i) => {
    if (near[i]) {
      if (gap > 0) out.push({ kind: "gap", count: gap });
      gap = 0;
      out.push(o);
    } else {
      gap++;
    }
  });
  if (gap > 0) out.push({ kind: "gap", count: gap });
  return out;
}
