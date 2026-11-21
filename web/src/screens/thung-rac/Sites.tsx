// Bảng site trong thùng rác (bản 2): một TableList trong thẻ, mỗi hàng một site
// với hai việc: Khôi phục (nút chữ) và Xoá hẳn (trong menu ba chấm, vì không
// hoàn tác được). Cả hai chỉ mở bản xem trước (việc của màn gọi), không chạy
// ngay. Dải "Đã chọn N" của việc hàng loạt nằm ở prop `above`, màn truyền vào.

import type { ReactNode } from "react";
import { Link } from "react-router";
import { useIdentity } from "../../api/m13-dbs";
import type { Site } from "../../api/types";
import { SelectAll, SelectOne, type Selection } from "../../components/Bulk";
import { RowActions } from "../../components/RowActions";
import { TableList } from "../../components/TableList";
import { useLang, useStrings } from "../../i18n";
import { strings as chung } from "../../i18n/m13-chung";
import { strings } from "../../i18n/thung-rac";
import { dueOf, formatWhen } from "./model";

export type TrashAction = "restore" | "purge";

type Props = {
  sites: Site[];
  /** Đang dựng bản xem trước: khoá mọi nút cho tới khi xong. */
  locked: boolean;
  /** Nút có khoá id (`restore:<domain>`, `purge:<domain>`) đang dựng bản xem trước: đổi chữ nút vừa bấm. */
  busy: (id: string) => boolean;
  onAct: (action: TrashAction, domain: string) => void;
  /** Ô chọn cho thao tác hàng loạt (khôi phục, xoá hẳn nhiều site). */
  sel: Selection;
  /** Dải "Đã chọn N" (BulkBar) đặt giữa đầu thẻ và bảng. */
  above?: ReactNode;
  /** Dòng chân bảng (câu nói về số ngày giữ khi người xem không phải admin). */
  footer?: ReactNode;
};

/** Ô hạn xoá hẳn: "còn N ngày", dưới là ngày giờ. Bảng site và bảng DB rời dùng chung. */
export function PurgeCell({ purgeAt }: { purgeAt?: string }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const d = dueOf(purgeAt, new Date());
  const left = d.kind === "days" ? t.daysLeft(d.n) : d.kind === "today" ? t.today : d.kind === "overdue" ? t.overdue : t.noDue;
  return (
    <span className="flex flex-col">
      <span>{left}</span>
      <span className="text-[12.5px] text-muted tabular-nums">{formatWhen(purgeAt, lang)}</span>
    </span>
  );
}

export function TrashTable({ sites, locked, busy, onAct, sel, above, footer }: Props) {
  const t = useStrings(strings);
  const m = useStrings(chung);
  const { isAdmin } = useIdentity();
  const { lang } = useLang();
  return (
    <TableList
      title={t.listHeading}
      titleId="trash-list"
      caption={t.tableName}
      minWidth={780}
      rows={sites}
      rowKey={(s) => s.domain}
      above={above}
      footer={footer}
      empty={
        <>
          {t.emptyLead}
          <Link to="/sites">{t.emptyLink}</Link>
          {t.emptyTail}
        </>
      }
      columns={[
        {
          header: "chon",
          headerCell: <SelectAll sel={sel} label={t.selectAll} />,
          className: "w-[34px]",
          cell: (s) => <SelectOne sel={sel} id={s.domain} label={t.selectOne(s.domain)} />,
        },
        {
          header: t.colDomain,
          cell: (s) => (
            // Tên miền dài nằm một dòng, cắt bằng dấu ba chấm; đủ chữ trong title.
            <span title={s.domain} className="inline-block max-w-[320px] truncate align-bottom font-medium">
              {s.domain}
            </span>
          ),
        },
        ...(isAdmin ? [{ header: m.colOwner, cell: (s: Site) => s.owner }] : []),
        { header: t.colTrashed, cell: (s) => <span className="tabular-nums">{formatWhen(s.trashed_at, lang)}</span> },
        { header: t.colPurge, cell: (s) => <PurgeCell purgeAt={s.purge_at} /> },
        {
          header: t.colAction,
          align: "right",
          cell: (s) => (
            <RowActions
              moreLabel={t.moreFor(s.domain, t.purge.toLowerCase())}
              primary={[{ label: t.restore, ariaLabel: t.restoreFor(s.domain), disabled: locked, busy: busy(`restore:${s.domain}`), busyLabel: t.previewing, onClick: () => onAct("restore", s.domain) }]}
              more={[{ label: t.purge, danger: true, disabled: locked, onSelect: () => onAct("purge", s.domain) }]}
            />
          ),
        },
      ]}
    />
  );
}
