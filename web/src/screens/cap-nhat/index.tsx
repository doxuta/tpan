// Màn Cập nhật (mục 6.11 và mục 10 của KE-HOACH.md). Màn giữ chỗ của đợt 0
// M12; thợ giao diện viết tiếp theo GET /api/v1/updates.

import { ScreenHeader } from "../../components/ScreenHeader";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/cap-nhat";

export default function UpdatesScreen() {
  const t = useStrings(strings);
  return <ScreenHeader title={t.title} summary={t.summary} />;
}
