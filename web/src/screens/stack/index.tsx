// Màn Stack (mục 6.11 và mục 10 của KE-HOACH.md). Màn giữ chỗ của đợt 0 M12;
// thợ giao diện viết tiếp: các bản PHP (GET /api/v1/php), cài thêm, gỡ bản
// không còn site dùng, nguồn của web server và MariaDB (GET /api/v1/stack).

import { ScreenHeader } from "../../components/ScreenHeader";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/stack";

export default function StackScreen() {
  const t = useStrings(strings);
  return <ScreenHeader title={t.title} summary={t.summary} />;
}
