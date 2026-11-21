// Thẻ lịch backup (mục 6.5 và 6.10): một TableList một hàng, cùng kiểu với bảng
// của từng site. Màn Cron chỉ cho xem; đổi giờ và số bản giữ ở màn Backup (đi
// đủ ba chặng xem trước, chạy, bằng chứng), nên ở đây chỉ có liên kết sang đó.

import { Link } from "react-router";
import type { BackupSettings } from "../../api/types";
import { buttonClass } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/cron";

type Props = { settings: BackupSettings; file?: string };

export function BackupSchedule({ settings, file }: Props) {
  const t = useStrings(strings);
  // at rỗng là lịch tắt (BackupSettings.at).
  const on = settings.at !== "";

  return (
    <TableList
      title={t.backupHeading}
      titleId="cron-backup"
      caption={t.backupTableName}
      minWidth={820}
      rows={[settings]}
      rowKey={() => "backup"}
      empty={null}
      above={<p className="m-0 max-w-[72ch] px-[16px] py-[12px] text-[13px] text-muted">{t.backupHint}</p>}
      columns={[
        { header: t.colJob, nowrap: false, className: "min-w-[200px]", cell: () => t.backupJob },
        { header: t.colEvery, cell: () => (on ? t.backupDaily(settings.at) : <Tag>{t.backupOff}</Tag>) },
        { header: t.colKeep, cell: () => t.backupKeep(settings.keep) },
        {
          header: t.colFile,
          cell: () => (file ? <MonoText size={12}>{file}</MonoText> : <span className="text-[12.5px] text-muted">{on ? "—" : t.backupFileNone}</span>),
        },
        {
          header: t.colChange,
          align: "right",
          cell: () => (
            <Link to="/backups" className={buttonClass("text", "", "sm")}>
              {t.backupChange}
            </Link>
          ),
        },
      ]}
    />
  );
}
