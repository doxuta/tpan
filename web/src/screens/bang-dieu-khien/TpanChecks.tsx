// Thẻ "TPAN tự kiểm" (mục 6.7: SQLite, chỗ trống của tpan, chứng chỉ panel,
// bộ hẹn giờ, job treo, quyền file config). Đặt cạnh thẻ "Máy" vì cùng nói về
// chính cái máy đang chạy tpan. Hiện cả phép kiểm đạt, để thấy tpan đã tự kiểm
// những gì; mỗi phép kiểm là một dòng mở ra được, đóng sẵn trừ dòng lỗi (mục 9:
// không khẳng định trạng thái mà không mở ra được bằng chứng).

import type { CheckResult } from "../../api/types";
import { Card } from "../../components/Card";
import { EmptyState } from "../../components/EmptyState";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/bang-dieu-khien";
import { CheckDisclosure } from "./CheckDisclosure";

export function TpanChecks({ checks }: { checks: CheckResult[] }) {
  const t = useStrings(strings);
  return (
    <Card title={t.tpanTitle} titleId="bdk-tu-kiem">
      {checks.length === 0 ? (
        <EmptyState>{t.tpanEmpty}</EmptyState>
      ) : (
        <div>
          {checks.map((c) => (
            <CheckDisclosure key={c.id} check={c} />
          ))}
        </div>
      )}
    </Card>
  );
}
