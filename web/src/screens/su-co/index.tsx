// Màn Sự cố (mục 6.7 và 10 của KE-HOACH.md): danh sách sự cố đang mở và đã
// đóng, trang một sự cố có bằng chứng và ba cách sửa. Đợt 0 chỉ có chỗ đứng;
// thợ giao diện viết màn thật.

import { ScreenHeader } from "../../components/ScreenHeader";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/su-co";

export function IncidentList() {
  const t = useStrings(strings);
  return <ScreenHeader title={t.title} />;
}

export function IncidentDetail() {
  const t = useStrings(strings);
  return <ScreenHeader title={t.title} />;
}
