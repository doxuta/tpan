// Bảng kết quả từng site của job hàng loạt (mục 7 của KE-HOACH.md: "xong, lỗi ở
// bước nào, vì sao"). Mỗi hàng là một sự kiện unit.done, nên bảng đọc lại được
// về sau như phần còn lại của trang job. Bản 2: TableList tự vẽ thẻ, dòng tóm
// tắt "N site xong, M site lỗi" nằm ở chân bảng. Dấu trạng thái là component
// chung; chữ kết quả nằm ngay cạnh dấu nên dấu ẩn khỏi trình đọc.

import type { Status, UnitResult } from "../../api/types";
import { DomainText } from "../../components/DomainText";
import { Code } from "../../components/MonoText";
import { StatusLine } from "../../components/StatusMark";
import { TableList } from "../../components/TableList";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/lich-su";

const markOf: Record<UnitResult["status"], Status> = {
  succeeded: "pass",
  rolled_back: "fail",
  undo_failed: "fail",
  not_run: "unknown",
};

export function UnitTable({ units }: { units: UnitResult[] }) {
  const t = useStrings(strings);
  const count = (s: UnitResult["status"]) => units.filter((u) => u.status === s).length;
  const failed = count("rolled_back") + count("undo_failed");

  return (
    <TableList
      title={t.unitsHeading}
      titleId="units-heading"
      caption={t.unitsHeading}
      minWidth={760}
      rows={units}
      rowKey={(u) => u.target}
      empty={t.unitsEmpty}
      footer={<span>{t.unitsSummary(count("succeeded"), failed, count("not_run"))}</span>}
      columns={[
        // Tên miền là chữ thường (sans), được ngắt dòng sau dấu chấm.
        { header: t.unitColSite, nowrap: false, className: "min-w-[200px]", cell: (u) => <DomainText domain={u.target} /> },
        { header: t.unitColResult, cell: (u) => <StatusLine status={markOf[u.status]}>{t.unitStatus[u.status]}</StatusLine> },
        {
          header: t.unitColStep,
          nowrap: false,
          cell: (u) =>
            u.phase || u.step_id ? (
              <span className="flex flex-col gap-[2px]">
                {u.phase && <span>{t.unitPhase(u.phase)}</span>}
                {u.step_id && <Code className="text-muted">{u.step_id}</Code>}
              </span>
            ) : null,
        },
        // Lỗi của lệnh có thể nhiều dòng: giữ xuống dòng, không cắt.
        { header: t.unitColWhy, nowrap: false, cell: (u) => (u.error ? <span className="block min-w-[200px] [overflow-wrap:anywhere] whitespace-pre-wrap">{u.error}</span> : null) },
      ]}
    />
  );
}
