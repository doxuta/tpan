// Một phép kiểm của Bảng điều khiển là một dòng mở ra được (bản 2, mục 3.7):
// dòng tóm tắt là dấu trạng thái, tên, câu kết quả; mở ra thì thấy nguyên nhân
// (khi chưa đạt) và bằng chứng. Đóng sẵn, trừ dòng đang lỗi. Dùng cho thẻ
// "TPAN tự kiểm" và cho khối bằng chứng của một ga trên bản đồ tuyến.
//
// Không dùng CheckItem của màn Site: dòng ở đây không có nút "Giải thích" hay
// lệnh sửa tay (đó là việc của trang site và trang sự cố), và có nhãn "dùng chung
// cả máy" cho phép kiểm của tầng dùng chung.

import type { CheckResult } from "../../api/types";
import { Disclosure } from "../../components/Disclosure";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { StatusMark } from "../../components/StatusMark";
import { Tag } from "../../components/Tag";
import { useStatusName } from "../../components/useStatusName";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bang-dieu-khien";

export function CheckDisclosure({ check, shared = false }: { check: CheckResult; shared?: boolean }) {
  const t = useStrings(strings);
  const statusName = useStatusName();
  const { lang } = useLang();
  const cause = tx(check.cause, lang);

  const summary = (
    <span className="flex items-start gap-[12px]">
      <span className="flex h-[21px] w-[20px] flex-none items-center justify-center">
        <StatusMark status={check.status} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block font-medium [overflow-wrap:anywhere]">
          <span className="sr-only">{statusName(check.status, check.suspended)}: </span>
          {tx(check.title, lang)}
          {shared && (
            <>
              {" "}
              <Tag>{t.sharedTag}</Tag>
            </>
          )}
        </span>
        <span className="block text-[13px] text-muted [overflow-wrap:anywhere]">{tx(check.summary, lang)}</span>
      </span>
    </span>
  );

  return (
    <Disclosure defaultOpen={check.status === "fail"} summary={summary}>
      <div className="flex flex-col gap-[12px] pl-[32px]">
        {check.status !== "pass" && cause && <p className="m-0">{cause}</p>}
        {check.evidence && check.evidence.length > 0 ? <EvidenceBlock evidence={check.evidence} clamp={20} /> : <p className="m-0 text-muted">{t.noEvidence}</p>}
      </div>
    </Disclosure>
  );
}
