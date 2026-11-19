// Màn khung của đợt 1 (nền giao diện): chỉ có tên màn và dòng tóm tắt.
// Thợ của màn này thay hẳn nội dung, giữ tên export mà web/src/routes.tsx dùng.

import { useParams } from "react-router";
import { ScreenHeader } from "../../components/ScreenHeader";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/lich-su";

/** /jobs: các job theo thời gian. */
export function JobList() {
  const t = useStrings(strings);
  return <ScreenHeader title={t.title} summary={t.summary} />;
}

/** /jobs/:id: một job, từng bước và output trực tiếp. */
export function JobDetail() {
  const t = useStrings(strings);
  const { id = "" } = useParams();
  return <ScreenHeader title={<span className="font-mono">{id}</span>} summary={t.jobSummary} />;
}
