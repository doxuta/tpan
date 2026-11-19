// Màn Bảo mật (mục 6.8 của KE-HOACH.md). Đợt 0 chỉ giữ chỗ để route biên dịch
// được; thợ của màn thay toàn bộ file này.

import { ScreenHeader } from "../../components/ScreenHeader";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/bao-mat";

export default function SecurityScreen() {
  const t = useStrings(strings);
  return <ScreenHeader title={t.title} summary={t.soon} />;
}
