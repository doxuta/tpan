// Màn Backup (mục 6.5, 10 của KE-HOACH.md). Giữ chỗ của đợt 0: thợ giao diện
// thay bằng bản thật (danh sách backup, tạo, khôi phục, lịch và số bản giữ).

import { ScreenHeader } from "../../components/ScreenHeader";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/backup";

export default function Backups() {
  const t = useStrings(strings);
  return <ScreenHeader title={t.title} summary={t.summary} />;
}
