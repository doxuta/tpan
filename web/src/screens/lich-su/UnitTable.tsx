// Bảng kết quả từng site của job hàng loạt (mục 7 của KE-HOACH.md: "xong, lỗi ở
// bước nào, vì sao"). Mỗi hàng là một sự kiện unit.done, nên bảng đọc lại được
// về sau như phần còn lại của trang job. Bảng kẻ mảnh, dấu trạng thái là
// component chung; chữ kết quả nằm ngay cạnh dấu nên dấu ẩn khỏi trình đọc.

import type { Status, UnitResult } from "../../api/types";
import { MonoText } from "../../components/MonoText";
import { StatusMark } from "../../components/StatusMark";
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
    <section aria-labelledby="units-heading" className="pt-[26px]">
      <h2 id="units-heading" className="m-0 text-[16px] font-semibold">
        {t.unitsHeading}
      </h2>
      <p className="mt-[6px] mb-[14px] text-muted">{t.unitsSummary(count("succeeded"), failed, count("not_run"))}</p>
      <TableList
        caption={t.unitsHeading}
        rows={units}
        rowKey={(u) => u.target}
        empty={t.unitsEmpty}
        columns={[
          { header: t.unitColSite, className: "min-w-[200px]", cell: (u) => <MonoText>{u.target}</MonoText> },
          {
            header: t.unitColResult,
            className: "whitespace-nowrap",
            cell: (u) => (
              <span className="inline-flex items-center gap-[8px]">
                <span className="flex h-[20px] w-[20px] items-center justify-center">
                  <StatusMark status={markOf[u.status]} />
                </span>
                {t.unitStatus[u.status]}
              </span>
            ),
          },
          {
            header: t.unitColStep,
            cell: (u) =>
              u.phase || u.step_id ? (
                <span className="flex flex-col gap-[2px]">
                  {u.phase && <span>{t.unitPhase(u.phase)}</span>}
                  {u.step_id && <span className="font-mono text-[12px] text-muted [overflow-wrap:anywhere]">{u.step_id}</span>}
                </span>
              ) : null,
          },
          // Lỗi của lệnh có thể nhiều dòng: giữ xuống dòng, không cắt.
          { header: t.unitColWhy, cell: (u) => (u.error ? <span className="[overflow-wrap:anywhere] whitespace-pre-wrap">{u.error}</span> : null) },
        ]}
      />
    </section>
  );
}
