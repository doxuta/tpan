// Thẻ "Bản sao lưu" của màn quản lý một DB (M33). DB rời không nằm trong bản backup
// của site nào (báo ngày 26/11/2026: backup site, xoá hẳn một DB rời, khôi phục bản
// backup của site thì không thấy DB rời quay lại), nên mỗi DB có danh sách bản .sql
// của riêng nó ngay ở đây:
//
//   Sao lưu ngay   DB của site: POST /databases/{domain}/dump   DB rời: POST /dbs/{name}/backups
//   Nhập lại       DB của site: POST /databases/{domain}/import DB rời: POST /dbs/{name}/import
//   Tải về, Xoá    GET /backups/{id}/download, DELETE /backups/{id} (như màn Backup)
//
// Danh sách là các dòng kind "sql" của GET /backups?domain=<tên miền> hoặc ?db=<tên DB>.
// Khác với sửa dữ liệu ở các thẻ trên, đây là job đổi máy nên đi đủ ba chặng: bản xem
// trước → bấm chạy → thẻ theo dõi job (usePreviewedOp).

import { useQuery } from "@tanstack/react-query";
import { errorMessage, get } from "../../api/client";
import { dbSubPath } from "../../api/m13-dbs";
import { dbInfoKey, dbDataBase, type DBKind } from "../../api/m32";
import type { Backup, BackupsOverview, DBImportRequest } from "../../api/types";
import { Button } from "../../components/Button";
import { EmptyState } from "../../components/EmptyState";
import { Notice } from "../../components/Notice";
import { usePreviewedOp } from "../../components/PreviewedOp";
import { RowActions } from "../../components/RowActions";
import { TableList } from "../../components/TableList";
import { useLang, useStrings } from "../../i18n";
import { strings as backupStrings } from "../../i18n/backup";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/quan-ly-db";
import { backupPath, backupsKey, dbBackupsKey, dbBackupsPath, formatSize, ownSqlBackups } from "../backup/model";
import { databasesKey, dbOpPath, siteBackupsKey, siteBackupsPath } from "../co-so-du-lieu/model";
import { formatWhen } from "../thung-rac/model";

type Props = {
  kind: DBKind;
  /** Tên miền (DB của site) hoặc tên DB (DB rời): cũng là giá trị trường domain của các bản sao lưu. */
  id: string;
  db: string;
  suspended: boolean;
};

export function DbBackups({ kind, id, db, suspended }: Props) {
  const t = useStrings(strings);
  const b = useStrings(backupStrings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const site = kind === "site";
  const q = useQuery({
    queryKey: site ? siteBackupsKey(id) : dbBackupsKey(id),
    queryFn: () => get<BackupsOverview>(site ? siteBackupsPath(id) : dbBackupsPath(id)),
  });
  // Sau job: danh sách bản sao lưu, số bảng và dung lượng của DB (nhập lại đổi cả hai), bảng ở màn Cơ sở dữ liệu.
  const op = usePreviewedOp([backupsKey, dbInfoKey(dbDataBase(kind, id)), databasesKey]);
  const rows = ownSqlBackups(q.data?.items ?? [], id, !site);

  const backupNow = () => void op.ask({ id: "bk-now", method: "POST", path: site ? dbOpPath(id, "dump") : dbSubPath(id, "backups"), warning: t.bkNowWarning(db), errorHeading: t.bkNowErrorHeading });
  function importIt(x: Backup) {
    const body: DBImportRequest = { backup_id: x.id };
    void op.ask({ id: `bk-import:${x.id}`, method: "POST", path: site ? dbOpPath(id, "import") : dbSubPath(id, "import"), body, warning: t.bkImportWarning(db, formatWhen(x.created, lang)), errorHeading: t.bkImportErrorHeading });
  }
  const deleteIt = (x: Backup) => void op.ask({ id: `bk-delete:${x.id}`, method: "DELETE", path: backupPath(x.id), warning: t.bkDeleteWarning(db, formatWhen(x.created, lang)), errorHeading: t.bkDeleteErrorHeading });

  return (
    <>
      <TableList<Backup>
        title={t.bkHeading}
        titleId="db-sao-luu"
        count={q.isSuccess ? t.bkCount(rows.length) : undefined}
        caption={t.bkCaption(db)}
        minWidth={560}
        rows={rows}
        rowKey={(x) => x.id}
        empty={q.isError ? <EmptyState>{t.bkLoadFailed}</EmptyState> : q.isPending ? <EmptyState>{k.loading}</EmptyState> : <EmptyState>{t.bkEmpty}</EmptyState>}
        above={
          <div className="flex flex-col gap-[12px] px-[16px] py-[12px]">
            <p className="m-0 max-w-[72ch] text-[13px] text-muted">{q.data ? (site ? t.bkHintSite(q.data.settings.keep) : t.bkHintStandalone(q.data.settings.keep)) : ""}</p>
            {suspended && <Notice kind="warn">{t.bkSuspended}</Notice>}
            {q.isError && (
              <Notice kind="fail" role="alert">
                {errorMessage(q.error, lang)}
              </Notice>
            )}
            <div>
              <Button variant="primary" disabled={op.pending} onClick={backupNow}>
                {op.busy("bk-now") ? b.previewing : t.bkNow}
              </Button>
            </div>
          </div>
        }
        columns={[
          { header: t.bkColWhen, cell: (x) => <span className="tabular-nums">{formatWhen(x.created, lang)}</span> },
          { header: t.bkColReason, nowrap: false, cell: (x) => b.reasons[x.reason] ?? x.reason },
          { header: t.bkColSize, align: "right", cell: (x) => formatSize(x.size, lang) },
          {
            header: b.colAction,
            align: "right",
            cell: (x) => {
              const when = formatWhen(x.created, lang);
              return (
                <RowActions
                  moreLabel={t.bkMoreFor(when)}
                  primary={[{ label: t.bkImport, ariaLabel: t.bkImportFor(when), disabled: op.pending || suspended || !x.local, busy: op.busy(`bk-import:${x.id}`), busyLabel: b.previewing, onClick: () => importIt(x) }]}
                  more={
                    x.local
                      ? [
                          { label: t.bkDownload, onSelect: () => window.location.assign(`/api/v1/backups/${encodeURIComponent(x.id)}/download`) },
                          { label: t.bkDelete, danger: true, disabled: op.pending, onSelect: () => deleteIt(x) },
                        ]
                      : []
                  }
                />
              );
            },
          },
        ]}
      />
      {op.panel}
    </>
  );
}
