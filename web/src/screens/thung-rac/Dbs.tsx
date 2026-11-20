// Bảng DB rời trong thùng rác (M13, mục 6.12 của KE-HOACH.md): cùng hình thức
// bảng site, mỗi hàng hai việc: Khôi phục và Xoá hẳn. Cả hai chỉ mở bản xem trước
// (việc của màn gọi), không chạy ngay. Admin thấy thêm cột Chủ.

import { useIdentity } from "../../api/m13-dbs";
import type { StandaloneDB } from "../../api/types";
import { Button } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
import { TableList } from "../../components/TableList";
import { useLang, useStrings } from "../../i18n";
import { strings as chung } from "../../i18n/m13-chung";
import { strings } from "../../i18n/thung-rac";
import { formatWhen } from "./model";
import { PurgeCell, type TrashAction } from "./Sites";

type Props = {
  dbs: StandaloneDB[];
  /** Đang dựng bản xem trước: khoá mọi nút cho tới khi xong. */
  locked: boolean;
  /** Nút có khoá id (`dbrestore:<tên>`, `dbpurge:<tên>`) đang dựng bản xem trước. */
  busy: (id: string) => boolean;
  onAct: (action: TrashAction, name: string) => void;
};

export function TrashDbTable({ dbs, locked, busy, onAct }: Props) {
  const t = useStrings(strings);
  const m = useStrings(chung);
  const { lang } = useLang();
  const { isAdmin } = useIdentity();

  return (
    <TableList
      caption={t.dbTableName}
      rows={dbs}
      rowKey={(d) => d.name}
      empty={t.dbEmpty}
      columns={[
        { header: t.colDbName, cell: (d) => <MonoText>{d.name}</MonoText> },
        ...(isAdmin ? [{ header: m.colOwner, className: "font-mono text-[13px]", cell: (d: StandaloneDB) => d.owner }] : []),
        { header: t.colTrashed, cell: (d) => <span className="font-mono text-[12px] whitespace-nowrap text-muted">{formatWhen(d.trashed_at, lang)}</span> },
        { header: t.colPurge, cell: (d) => <PurgeCell purgeAt={d.purge_at} /> },
        {
          header: t.colAction,
          className: "pr-0",
          cell: (d) => (
            <span className="flex flex-wrap gap-[8px]">
              <Button aria-label={t.restoreDbFor(d.name)} disabled={locked} onClick={() => onAct("restore", d.name)}>
                {busy(`dbrestore:${d.name}`) ? t.previewing : t.restore}
              </Button>
              <Button aria-label={t.purgeDbFor(d.name)} disabled={locked} onClick={() => onAct("purge", d.name)}>
                {busy(`dbpurge:${d.name}`) ? t.previewing : t.purge}
              </Button>
            </span>
          ),
        },
      ]}
    />
  );
}
