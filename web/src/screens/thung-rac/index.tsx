// Màn Thùng rác (mục 6.4, 6.12, 10 của KE-HOACH.md): các site đã xoá và (M13)
// các DB rời đã xoá, mỗi cái có Khôi phục và Xoá hẳn, cùng khối đổi số ngày giữ
// (chỉ admin). Hai việc kia đi đúng ba chặng như mọi thao tác: bấm nút → gọi
// ?dry_run=1 lấy bản xem trước (PlanReview) → bấm chạy → máy chủ trả job_id,
// ở lại trang và thẻ theo dõi job báo tiến độ. Ba chặng nằm trong hook usePreviewedOp. Xoá hẳn có thêm
// một dòng cảnh báo mất dữ liệu trên bản xem trước; hộp hỏi lại lúc bấm chạy là
// của PlanReview, hiện vì các bước xoá là Danger.

import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { del, errorMessage, get, post } from "../../api/client";
import { dbSubPath, trashDbPath, useIdentity } from "../../api/m13-dbs";
import type { JobStarted, TrashOverview } from "../../api/types";
import { BulkBar, useSelection, type Selection } from "../../components/Bulk";
import { Button } from "../../components/Button";
import { usePreviewedOp, type Op } from "../../components/PreviewedOp";
import { ScreenHeader } from "../../components/ScreenHeader";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/thung-rac";
import { databasesKey } from "../co-so-du-lieu/model";
import { purgePath, restorePath, trashKey, trashPath } from "./model";
import { Retention } from "./Retention";
import { TrashDbTable } from "./Dbs";
import { TrashTable, type TrashAction } from "./Sites";

export default function Trash() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const { isAdmin } = useIdentity();
  const q = useQuery({ queryKey: trashKey, queryFn: () => get<TrashOverview>(trashPath) });
  // Danh sách thùng rác, danh sách DB và số đếm ở menu (overview) đều đổi sau job.
  const op = usePreviewedOp([trashKey, databasesKey]);

  const siteOp = (action: TrashAction, domain: string): Op =>
    action === "restore"
      ? { id: `restore:${domain}`, method: "POST", path: restorePath(domain), errorHeading: t.restoreErrorHeading(domain) }
      : { id: `purge:${domain}`, method: "DELETE", path: purgePath(domain), warning: t.purgeWarning(domain), errorHeading: t.purgeErrorHeading(domain) };

  const dbOp = (action: TrashAction, name: string): Op =>
    action === "restore"
      ? { id: `dbrestore:${name}`, method: "POST", path: dbSubPath(name, "restore"), warning: t.restoreDbWarning(name), errorHeading: t.restoreDbErrorHeading(name) }
      : { id: `dbpurge:${name}`, method: "DELETE", path: trashDbPath(name), warning: t.purgeDbWarning(name), errorHeading: t.purgeDbErrorHeading(name) };

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />

      {q.isPending && <p className="mt-[26px] mb-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div className="pt-[26px]">
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </div>
      )}
      {q.isSuccess && (
        <>
          <section aria-labelledby="trash-list">
            <h2 id="trash-list" className="m-0 pt-[26px] pb-[14px] text-[16px] font-semibold">
              {t.listHeading}
            </h2>
            {/* Go trả null cho danh sách rỗng, nên sites có thể vắng. */}
            <TrashBulk domains={(q.data.sites ?? []).map((x) => x.domain)}>
              {(sel) => <TrashTable sites={q.data.sites ?? []} locked={op.pending} busy={op.busy} onAct={(a, d) => void op.ask(siteOp(a, d))} sel={sel} />}
            </TrashBulk>
          </section>

          <section aria-labelledby="trash-dbs">
            <h2 id="trash-dbs" className="m-0 pt-[26px] pb-[14px] text-[16px] font-semibold">
              {t.dbHeading}
            </h2>
            {/* Go trả null cho danh sách rỗng, nên dbs có thể vắng. */}
            <TrashDbTable dbs={q.data.dbs ?? []} locked={op.pending} busy={op.busy} onAct={(a, n) => void op.ask(dbOp(a, n))} />
          </section>

          {isAdmin ? <Retention days={q.data.days} /> : <p className="mt-[26px] mb-0 max-w-[720px] text-[13px] text-muted">{t.retentionUser(q.data.days)}</p>}

          {op.panel}
        </>
      )}
    </>
  );
}

/** Khôi phục hay xoá hẳn nhiều site trong thùng rác một lần (mỗi site một job, lần lượt). */
function TrashBulk({ domains, children }: { domains: string[]; children: (sel: Selection) => ReactNode }) {
  const t = useStrings(strings);
  const sel = useSelection(domains);
  return (
    <>
      <BulkBar
        sel={sel}
        unit={t.unitSite}
        actions={[
          {
            key: "restore",
            label: t.bulkRestore,
            confirmTitle: t.bulkRestoreTitle,
            confirmBody: <p className="m-0">{t.bulkRestoreBody}</p>,
            confirmLabel: t.bulkRestore,
            run: (d) => post<JobStarted>(restorePath(d)),
          },
          {
            key: "purge",
            label: t.bulkPurge,
            confirmTitle: t.bulkPurgeTitle,
            // Chữ đậm, không đỏ: đỏ chỉ dành cho tình trạng máy (mục 2 của GIAO-DIEN.md).
            confirmBody: <p className="m-0 font-semibold">{t.bulkPurgeBody}</p>,
            confirmLabel: t.bulkPurge,
            run: (d) => del<JobStarted>(purgePath(d)),
          },
        ]}
      />
      {children(sel)}
    </>
  );
}
