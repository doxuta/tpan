// Một dòng của trang bảo mật (bản 2): một dòng mở ra được như CheckItem của
// màn Site. Dòng tóm tắt là dấu trạng thái, tên và câu kết quả; lệnh đã dùng
// nằm trong bằng chứng nên không in lại dưới tên dòng. Khác CheckItem ở chỗ dòng
// có thể mang một nút việc (Bật tường lửa, Cài fail2ban) hoặc một lời nói rõ
// "TPAN không sửa dòng này" (hai dòng SSH). Hai thứ đó nằm NGOÀI phần mở ra
// (dưới nó, vì Disclosure vẽ nút và phần mở ra liền nhau) và chỉ hiện khi dòng
// chưa đạt.
//
// Bên gọi quyết dòng nào mở sẵn: truyền open + onToggle (do cha giữ trạng thái)
// hoặc defaultOpen (dòng tự giữ). Không truyền gì: chỉ dòng lỗi mở sẵn.

import type { ReactNode } from "react";
import type { CheckResult } from "../../api/types";
import { Button } from "../../components/Button";
import { CommandBlock } from "../../components/CommandBlock";
import { Disclosure } from "../../components/Disclosure";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { StatusMark } from "../../components/StatusMark";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bao-mat";
import { strings as khung } from "../../i18n/khung";

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
  /** Cha giữ trạng thái mở: truyền cùng onToggle. Không truyền thì dòng tự giữ (xem defaultOpen). */
  open?: boolean;
  onToggle?: () => void;
  /** Dòng tự giữ trạng thái: mở sẵn hay không. Mặc định: chỉ dòng lỗi mở sẵn. */
  defaultOpen?: boolean;
  action?: RowAction;
  /** Dòng chỉ cảnh báo, không có nút sửa (hai dòng SSH). */
  noFix?: boolean;
};

const labelClass = "mb-[6px] text-[12px] font-medium text-muted";

export function CheckRow({ check, open, onToggle, defaultOpen, action, noFix = false }: Props) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const cause = tx(check.cause, lang);
  const fix = check.manual_fix ?? [];
  const notOk = check.status !== "pass";

  const summary = (
    <span className="flex items-start gap-[12px]">
      <span className="flex h-[21px] w-[20px] flex-none items-center justify-center">
        <StatusMark status={check.status} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block font-medium [overflow-wrap:anywhere]">
          <span className="sr-only">{k.status[check.status]}: </span>
          {tx(check.title, lang)}
        </span>
        <span className="block text-[13px] text-muted [overflow-wrap:anywhere]">{tx(check.summary, lang)}</span>
      </span>
    </span>
  );

  return (
    <li className="border-t border-hair first:border-t-0">
      <Disclosure flush open={open} defaultOpen={defaultOpen ?? check.status === "fail"} onOpenChange={() => onToggle?.()} summary={summary}>
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
        </div>
      </Disclosure>

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
    </li>
  );
}
