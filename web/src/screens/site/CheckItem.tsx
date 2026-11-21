// Một phép kiểm (bản 2): một dòng mở ra được. Dòng tóm tắt là dấu trạng thái,
// tên và câu kết quả; mở ra thì thấy bằng chứng (lệnh và output thật), nguyên
// nhân và cách sửa tay. Màn Tạo site dùng lại để hiện các điều kiện chưa đạt
// của một lần từ chối.

import { useState } from "react";
import { Link } from "react-router";
import { useAi } from "../../ai/AiProvider";
import { useWho } from "../../api/m13-users";
import type { CheckResult } from "../../api/types";
import { Button } from "../../components/Button";
import { CommandBlock } from "../../components/CommandBlock";
import { Disclosure } from "../../components/Disclosure";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { StatusMark } from "../../components/StatusMark";
import { useStatusName } from "../../components/useStatusName";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/site";

type Props = {
  check: CheckResult;
  open: boolean;
  onToggle: () => void;
};

const labelClass = "mb-[6px] text-[12px] font-medium text-muted";

export function CheckItem({ check, open, onToggle }: Props) {
  const t = useStrings(strings);
  const statusName = useStatusName();
  const { lang } = useLang();
  const { openAI } = useAi();
  const { isAdmin } = useWho(); // user thường không có AI (mục 6.12): không có nút "Giải thích"
  const cause = tx(check.cause, lang);
  const fix = check.manual_fix ?? [];

  const summary = (
    <span className="flex items-start gap-[12px]">
      <span className="flex h-[21px] w-[20px] flex-none items-center justify-center">
        <StatusMark status={check.status} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block font-medium [overflow-wrap:anywhere]">
          <span className="sr-only">{statusName(check.status, check.suspended)}: </span>
          {tx(check.title, lang)}
        </span>
        <span className="block text-[13px] text-muted [overflow-wrap:anywhere]">{tx(check.summary, lang)}</span>
      </span>
    </span>
  );

  return (
    <li className="border-t border-hair first:border-t-0">
      <Disclosure flush open={open} onOpenChange={onToggle} summary={summary}>
        <div className="flex flex-col gap-[14px] pl-[32px]">
          {check.evidence && check.evidence.length > 0 ? <EvidenceBlock evidence={check.evidence} clamp={20} /> : <p className="m-0 text-muted">{t.noEvidence}</p>}
          {cause && (
            <div>
              <div className={labelClass}>{t.cause}</div>
              <p className="m-0">{cause}</p>
            </div>
          )}
          {fix.length > 0 && (
            <div>
              <div className={labelClass}>{t.manualFix}</div>
              <CommandBlock commands={fix} />
            </div>
          )}
          {/* TLS chưa đạt: nút xin chứng chỉ nằm ở màn SSL (có xem trước Plan), dẫn thẳng tới đó. */}
          {check.id.startsWith("tls.cert:") && check.status !== "pass" && (
            <Link to="/ssl" className="self-start">
              {t.sslLink}
            </Link>
          )}
          {/* Phép kiểm không đạt: ngăn AI mở ra với câu soạn sẵn, người xem trước rồi tự gửi. Nút vẫn hiện khi AI tắt; ngăn AI tự chỉ cách nối. */}
          {isAdmin && (check.status === "fail" || check.status === "warn") && (
            <div>
              <Button
                size="sm"
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
      </Disclosure>
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
