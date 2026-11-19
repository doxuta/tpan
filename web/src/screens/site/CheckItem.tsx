// Một phép kiểm: dấu trạng thái, tên, câu kết quả; bấm để mở bằng chứng (lệnh
// và output thật), nguyên nhân và cách sửa tay. Màn Tạo site dùng lại để hiện
// các điều kiện chưa đạt của một lần từ chối.

import { useState } from "react";
import { useAi } from "../../ai/AiProvider";
import type { CheckResult } from "../../api/types";
import { Button } from "../../components/Button";
import { CommandBlock } from "../../components/CommandBlock";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { StatusMark } from "../../components/StatusMark";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/site";

/** Id hợp lệ cho aria-controls: id phép kiểm có dấu ":" và "." nên đổi hết ký tự lạ. */
export function checkPanelId(id: string): string {
  return "phep-kiem-" + id.replace(/[^a-zA-Z0-9_-]/g, "-");
}

type Props = {
  check: CheckResult;
  open: boolean;
  onToggle: () => void;
};

export function CheckItem({ check, open, onToggle }: Props) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const { openAI } = useAi();
  const panelId = checkPanelId(check.id);
  const cause = tx(check.cause, lang);
  const fix = check.manual_fix ?? [];

  return (
    <li className="border-t border-hair first:border-t-0">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={onToggle}
        className="flex min-h-[44px] w-full items-start gap-[12px] bg-transparent py-[10px] text-left text-ink"
      >
        <span className="flex h-[21px] w-[20px] flex-none items-center justify-center">
          <StatusMark status={check.status} />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block font-medium">
            <span className="sr-only">{k.status[check.status]}: </span>
            {tx(check.title, lang)}
          </span>
          <span className="block text-muted">{tx(check.summary, lang)}</span>
        </span>
        <svg aria-hidden="true" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={`mt-[3px] flex-none text-muted ${open ? "rotate-180" : ""}`}>
          <path d="M6 9l6 6 6-6" />
        </svg>
      </button>
      {open && (
        <div id={panelId} className="flex flex-col gap-[14px] pb-[16px] pl-[32px]">
          {check.evidence && check.evidence.length > 0 ? <EvidenceBlock evidence={check.evidence} /> : <p className="m-0 text-muted">{t.noEvidence}</p>}
          {cause && (
            <div>
              <div className="mb-[6px] font-mono text-[11px] tracking-[0.08em] text-muted">{t.cause}</div>
              <p className="m-0">{cause}</p>
            </div>
          )}
          {fix.length > 0 && (
            <div>
              <div className="mb-[6px] font-mono text-[11px] tracking-[0.08em] text-muted">{t.manualFix}</div>
              <CommandBlock commands={fix} />
            </div>
          )}
          {/* Phép kiểm không đạt: ngăn AI mở ra với câu soạn sẵn, người xem trước rồi tự gửi. Nút vẫn hiện khi AI tắt; ngăn AI tự chỉ cách nối. */}
          {(check.status === "fail" || check.status === "warn") && (
            <div>
              <Button
                title={t.explainHint}
                onClick={() =>
                  openAI({
                    mode: "explain",
                    question: t.explainCheckQuestion(tx(check.title, lang)),
                    pointer: { kind: "check", ref: check.id, label: tx(check.title, lang) },
                  })
                }
              >
                {t.explain}
              </Button>
            </div>
          )}
        </div>
      )}
    </li>
  );
}

/** Danh sách phép kiểm tự giữ trạng thái mở/đóng của từng dòng; phép kiểm lỗi mở sẵn. */
export function CheckList({ checks }: { checks: CheckResult[] }) {
  const [open, setOpen] = useState<Set<string>>(() => new Set(checks.filter((c) => c.status === "fail").map((c) => c.id)));
  const toggle = (id: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  return (
    <ul className="m-0 list-none p-0">
      {checks.map((c) => (
        <CheckItem key={c.id} check={c} open={open.has(c.id)} onToggle={() => toggle(c.id)} />
      ))}
    </ul>
  );
}
