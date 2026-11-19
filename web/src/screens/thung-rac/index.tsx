// Màn Thùng rác (mục 6.4, 10 của KE-HOACH.md). Hợp đồng M6 đợt 0: màn giữ
// chỗ để route và menu có chỗ trỏ tới; thợ giao diện dựng màn thật.

import { ScreenHeader } from "../../components/ScreenHeader";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/thung-rac";

export default function Trash() {
  const t = useStrings(strings);
  return <ScreenHeader title={t.title} summary={t.summary} />;
}
