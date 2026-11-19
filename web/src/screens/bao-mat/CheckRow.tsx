// Một dòng của trang bảo mật: dấu trạng thái, tên, câu kết quả và lệnh đã dùng
// để kiểm; bấm để mở bằng chứng, nguyên nhân và cách sửa tay. Khác CheckItem
// của màn Site ở chỗ dòng có thể mang một nút việc (Bật tường lửa, Cài
// fail2ban) hoặc một lời nói rõ "TPAN không sửa dòng này" (hai dòng SSH).
// Nút việc nằm NGOÀI nút mở dòng, vì <button> không được nằm trong <button>.

import type { ReactNode } from "react";
import type { CheckResult } from "../../api/types";
import { Button } from "../../components/Button";
import { CommandBlock } from "../../components/CommandBlock";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { StatusMark } from "../../components/StatusMark";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bao-mat";
import { strings as khung } from "../../i18n/khung";
import { checkPanelId } from "../site/CheckItem";

export type RowAction = {
  /** Dùng làm id nút ("act-ufw") để trả focus về nút này khi huỷ bản xem trước. */
  kind: string;
  label: string;
  hint: string;
  pending: boolean;
  disabled: boolean;
  onClick: () => void;
  /** Lỗi của lần xem trước (ErrorNote), hiện ngay dưới nút. */
  error?: ReactNode;
};

type Props = {
  check: CheckResult;
  open: boolean;
  onToggle: () => void;
  action?: RowAction;
  /** Dòng chỉ cảnh báo, không có nút sửa (hai dòng SSH). */
  noFix?: boolean;
};

const labelClass = "mb-[6px] font-mono text-[11px] tracking-[0.08em] text-muted";

export function CheckRow({ check, open, onToggle, action, noFix = false }: Props) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const panelId = checkPanelId(check.id);
  const cause = tx(check.cause, lang);
  const fix = check.manual_fix ?? [];
  const commands = (check.evidence ?? []).map((e) => e.command);
  const notOk = check.status !== "pass";

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
          {commands.length > 0 && (
            <span className="mt-[2px] block font-mono text-[12px] break-all text-muted">
              <span className="sr-only">{t.commandsUsed}: </span>
              $ {commands[0]}
              {commands.length > 1 && <span> ({t.commandsMore(commands.length - 1)})</span>}
            </span>
          )}
        </span>
        <svg aria-hidden="true" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={`mt-[3px] flex-none text-muted ${open ? "rotate-180" : ""}`}>
          <path d="M6 9l6 6 6-6" />
        </svg>
      </button>

      {/* Việc làm được ngay từ dòng: chỉ hiện khi dòng chưa đạt. */}
      {action && notOk && (
        <div className="pb-[14px] pl-[32px]">
          <p className="mt-0 mb-[10px] text-muted">{action.hint}</p>
          <Button id={`act-${action.kind}`} onClick={action.onClick} disabled={action.disabled}>
            {action.pending ? t.previewing : action.label}
          </Button>
          {action.error && <div className="pt-[12px]">{action.error}</div>}
        </div>
      )}
      {noFix && notOk && <p className="mt-0 mb-[14px] pl-[32px] text-muted">{t.sshNoFix}</p>}

      {open && (
        <div id={panelId} className="flex flex-col gap-[14px] pb-[16px] pl-[32px]">
          {check.evidence && check.evidence.length > 0 ? <EvidenceBlock evidence={check.evidence} /> : <p className="m-0 text-muted">{t.noEvidence}</p>}
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
        </div>
      )}
    </li>
  );
}
