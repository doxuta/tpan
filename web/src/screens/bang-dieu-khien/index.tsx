// Màn khung của đợt 1 (nền giao diện): chỉ có tên màn và dòng tóm tắt.
// Thợ của màn này thay hẳn nội dung, giữ tên export mà web/src/routes.tsx dùng.

import { ScreenHeader } from "../../components/ScreenHeader";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/bang-dieu-khien";

export default function BangDieuKhien() {
  const t = useStrings(strings);
  return <ScreenHeader title={t.title} summary={t.summary} />;
}
