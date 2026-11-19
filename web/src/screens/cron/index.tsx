// Màn Cron (mục 6.10, 10 của KE-HOACH.md). Giữ chỗ của đợt 0: thợ giao diện
// thay bằng bản thật (lịch của từng site, lần chạy gần nhất, lịch backup).

import { ScreenHeader } from "../../components/ScreenHeader";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/cron";

export default function Cron() {
  const t = useStrings(strings);
  return <ScreenHeader title={t.title} summary={t.summary} />;
}
