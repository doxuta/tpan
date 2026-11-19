// Mảnh dùng chung của danh sách và trang một sự cố.

import { Link } from "react-router";
import type { Incident, Status } from "../../api/types";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/su-co";

/**
 * Dấu và chữ trạng thái của một sự cố. Đang mở là lỗi, đóng vì đạt lại là đạt.
 * Đóng vì đối tượng không còn được kiểm ("gone") thì không có phép kiểm nào
 * vừa đạt, nên dùng "chưa kiểm được": không khẳng định điều chưa có bằng chứng.
 */
export function incidentState(inc: Incident, t: (typeof strings)["vi"]): { status: Status; text: string } {
  if (!inc.closed) return { status: "fail", text: t.stateOpen };
  if (inc.close_reason === "gone") return { status: "unknown", text: t.stateGone };
  return { status: "pass", text: t.stateClosed };
}

/** Đối tượng của sự cố: "" là máy, "tpan" là chính tpan, còn lại là domain (liên kết tới trang site). */
export function Target({ target }: { target: string }) {
  const t = useStrings(strings);
  if (target === "") return <>{t.targetMachine}</>;
  if (target === "tpan") return <>{t.targetTpan}</>;
  return (
    <Link to={`/sites/${encodeURIComponent(target)}`} className="font-mono text-[13px] [overflow-wrap:anywhere]">
      {target}
    </Link>
  );
}

/** Domain để gửi cho POST /api/v1/health/run?domain=; rỗng khi đối tượng không phải một site. */
export function domainOf(target: string): string {
  return target === "" || target === "tpan" ? "" : target;
}
