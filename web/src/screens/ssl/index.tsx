// Màn SSL (mục 6.3 của KE-HOACH.md). Đợt 0 chỉ giữ chỗ để route biên dịch được;
// thợ của màn thay toàn bộ file này.

import { ScreenHeader } from "../../components/ScreenHeader";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/ssl";

export default function SSLScreen() {
  const t = useStrings(strings);
  return <ScreenHeader title={t.title} summary={t.soon} />;
}
