// Gộp các phép kiểm cô lập của một site (mục 3.7 của docs/thay-doi/M16-thiet-ke.md).
// Phép kiểm cô lập có id "isolation:<từ>:<tới>" (chặng wordpress): site có N site
// khác thì có 2N phép, nên không in từng dòng mà gộp theo site kia.
//
// File thuần, chỉ `import type`: node --test chạy được mà không cần build
// (isolation.test.ts). Vì thế thứ tự "xấu nhất" chép lại ở đây thay vì import từ
// route.ts; hai nơi phải cùng thứ tự pass < unknown < warn < fail.

import type { CheckResult, Status } from "../../api/types";

export type IsolationPair = {
  /** Site kia (tên miền). */
  other: string;
  /** isolation:<domain>:<other>: site này chạm site kia. */
  out?: CheckResult;
  /** isolation:<other>:<domain>: site kia chạm site này. */
  in?: CheckResult;
};

export type IsolationGroup = {
  /** Số site khác (đếm theo tên miền, không trùng). */
  others: number;
  /** Số phép kiểm cô lập (mỗi cặp có hai chiều). */
  total: number;
  /** Số phép có status khác "pass". */
  bad: number;
  /** Xấu nhất theo thứ tự pass < unknown < warn < fail (cùng thứ tự với route.ts). */
  status: Status;
  /** Cặp có phép không đạt trước, rồi theo tên. */
  pairs: IsolationPair[];
  /** Id các phép cần mở sẵn: mọi phép có status "fail". */
  openIds: string[];
};

const rank: Record<Status, number> = { pass: 0, unknown: 1, warn: 2, fail: 3 };

const prefix = "isolation:";

/**
 * Tách id của một phép cô lập thành [từ, tới]. Phần sau tiền tố phải ra đúng hai
 * phần khi cắt bằng ":" (tên miền không chứa dấu hai chấm); khác hai phần, hoặc
 * không có tiền tố, thì trả null.
 */
export function isolationEnds(id: string): [from: string, to: string] | null {
  if (!id.startsWith(prefix)) return null;
  const parts = id.slice(prefix.length).split(":");
  return parts.length === 2 ? [parts[0], parts[1]] : null;
}

/** Phép kiểm cô lập có dính tới site này không (một trong hai đầu bằng đúng tên miền, so bằng, không so đầu hay đuôi). */
export function touchesSite(id: string, domain: string): boolean {
  const ends = isolationEnds(id);
  return ends !== null && ends[0] !== ends[1] && (ends[0] === domain || ends[1] === domain);
}

/** Gộp phép kiểm cô lập của site `domain`; null khi không có phép nào dính tới nó. */
export function groupIsolation(checks: readonly CheckResult[], domain: string): IsolationGroup | null {
  const byOther = new Map<string, IsolationPair>();
  for (const c of checks) {
    const ends = isolationEnds(c.id);
    if (ends === null || ends[0] === ends[1]) continue;
    const [from, to] = ends;
    // Chiều nào cũng phải bằng đúng tên miền ở một đầu: không dùng startsWith (a.com với isolation:a.com.vn:b.com).
    const out = from === domain;
    if (!out && to !== domain) continue;
    const other = out ? to : from;
    const pair = byOther.get(other) ?? { other };
    if (out) pair.out = c;
    else pair.in = c;
    byOther.set(other, pair);
  }
  if (byOther.size === 0) return null;

  const pairs = [...byOther.values()];
  const dirs = (p: IsolationPair): CheckResult[] => [p.out, p.in].filter((c): c is CheckResult => c !== undefined);
  const isBad = (p: IsolationPair): boolean => dirs(p).some((c) => c.status !== "pass");
  // Sắp theo mã ký tự, không theo locale, để thứ tự giống nhau trên mọi máy.
  pairs.sort((a, b) => Number(isBad(b)) - Number(isBad(a)) || (a.other < b.other ? -1 : a.other > b.other ? 1 : 0));

  const all = pairs.flatMap(dirs);
  let status: Status = "pass";
  for (const c of all) if (rank[c.status] > rank[status]) status = c.status;
  return {
    others: pairs.length,
    total: all.length,
    bad: all.filter((c) => c.status !== "pass").length,
    status,
    pairs,
    openIds: all.filter((c) => c.status === "fail").map((c) => c.id),
  };
}
