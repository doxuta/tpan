// Khối "TPAN tự kiểm" (mục 6.7: SQLite, chỗ trống của tpan, chứng chỉ panel,
// bộ hẹn giờ, job treo, quyền file config). Đặt cạnh khối "Máy" vì cùng nói về
// chính cái máy đang chạy tpan. Hiện cả phép kiểm đạt, để thấy tpan đã tự kiểm
// những gì; mỗi hàng là một nút, bấm mở bằng chứng ngay dưới (mục 9: không
// khẳng định trạng thái mà không mở ra được bằng chứng).

import { useState } from "react";
import type { CheckResult } from "../../api/types";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { StatusMark } from "../../components/StatusMark";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bang-dieu-khien";
import { strings as khung } from "../../i18n/khung";

export function TpanChecks({ checks }: { checks: CheckResult[] }) {
  const t = useStrings(strings);
  const kt = useStrings(khung);
  const { lang } = useLang();
  const [open, setOpen] = useState<string | null>(null);

  return (
    <section className="pt-[28px]">
      <h2 className="m-0 mb-[6px] text-[16px] font-semibold">{t.tpanTitle}</h2>
      {checks.length === 0 ? (
        <p className="m-0 border-t border-hair py-[10px] text-muted">{t.tpanEmpty}</p>
      ) : (
        <ul className="m-0 list-none p-0">
          {checks.map((c) => {
            const isOpen = open === c.id;
            const title = tx(c.title, lang);
            return (
              <li key={c.id} className="border-t border-hair">
                <button
                  type="button"
                  aria-expanded={isOpen}
                  onClick={() => setOpen(isOpen ? null : c.id)}
                  className={`flex w-full flex-wrap items-center gap-x-[12px] gap-y-[2px] py-[10px] text-left hover:bg-pre ${isOpen ? "bg-pre" : ""}`}
                >
                  <span className="flex h-[20px] w-[20px] flex-none items-center justify-center">
                    <StatusMark status={c.status} label={kt.statusLabel(title, kt.status[c.status])} />
                  </span>
                  <span className="font-medium">{title}</span>
                  <span className="min-w-0 flex-[1_1_200px] text-[13px] text-muted [overflow-wrap:anywhere]">{tx(c.summary, lang)}</span>
                </button>
                {isOpen && (
                  <div className="pb-[12px]">
                    {c.status !== "pass" && c.cause && <p className="mt-0 mb-[10px]">{tx(c.cause, lang)}</p>}
                    {c.evidence && c.evidence.length > 0 ? <EvidenceBlock evidence={c.evidence} /> : <p className="m-0 text-[13px] text-muted">{t.noEvidence}</p>}
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
