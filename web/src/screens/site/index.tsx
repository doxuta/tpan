// Màn khung của đợt 1 (nền giao diện): chỉ có tên màn và dòng tóm tắt.
// Thợ của màn này thay hẳn nội dung, giữ tên export mà web/src/routes.tsx dùng.

import { useParams } from "react-router";
import { ScreenHeader } from "../../components/ScreenHeader";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/site";

/** /sites: danh sách site. */
export function SiteList() {
  const t = useStrings(strings);
  return <ScreenHeader title={t.listTitle} summary={t.listSummary} />;
}

/** /sites/:domain: một site. */
export function SiteDetail() {
  const t = useStrings(strings);
  const { domain = "" } = useParams();
  return <ScreenHeader title={<span className="font-mono">{domain}</span>} summary={t.detailSummary} />;
}
