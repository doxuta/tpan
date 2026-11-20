// Bảng site trong thùng rác: bảng kẻ mảnh như "Lịch sử máy" (mục 7 của
// GIAO-DIEN.md), mỗi hàng một site với hai việc: Khôi phục và Xoá hẳn. Cả hai
// chỉ mở bản xem trước (việc của màn gọi), không chạy ngay.

import { Link } from "react-router";
import { useIdentity } from "../../api/m13-dbs";
import type { Site } from "../../api/types";
import { Button } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
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
};

/** Ô hạn xoá hẳn: ngày giờ, rồi "còn N ngày". Bảng site và bảng DB rời dùng chung. */
export function PurgeCell({ purgeAt }: { purgeAt?: string }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const d = dueOf(purgeAt, new Date());
  const left = d.kind === "days" ? t.daysLeft(d.n) : d.kind === "today" ? t.today : d.kind === "overdue" ? t.overdue : t.noDue;
  return (
    <span className="whitespace-nowrap">
      <span className="font-mono text-[12px] text-muted">{formatWhen(purgeAt, lang)}</span>
      <span className="ml-[8px]">{left}</span>
    </span>
  );
}

export function TrashTable({ sites, locked, busy, onAct }: Props) {
  const t = useStrings(strings);
  const m = useStrings(chung);
  const { lang } = useLang();
  const { isAdmin } = useIdentity();
  return (
    <TableList
      caption={t.tableName}
      rows={sites}
      rowKey={(s) => s.domain}
      empty={
        <>
          {t.emptyLead}
          <Link to="/sites">{t.emptyLink}</Link>
          {t.emptyTail}
        </>
      }
      columns={[
        { header: t.colDomain, cell: (s) => <MonoText>{s.domain}</MonoText> },
        ...(isAdmin ? [{ header: m.colOwner, className: "font-mono text-[13px]", cell: (s: Site) => s.owner }] : []),
        { header: t.colTrashed, cell: (s) => <span className="font-mono text-[12px] whitespace-nowrap text-muted">{formatWhen(s.trashed_at, lang)}</span> },
        {
          header: t.colPurge,
          cell: (s) => <PurgeCell purgeAt={s.purge_at} />,
        },
        {
          header: t.colAction,
          className: "pr-0",
          cell: (s) => (
            <span className="flex flex-wrap gap-[8px]">
              <Button aria-label={t.restoreFor(s.domain)} disabled={locked} onClick={() => onAct("restore", s.domain)}>
                {busy(`restore:${s.domain}`) ? t.previewing : t.restore}
              </Button>
              <Button aria-label={t.purgeFor(s.domain)} disabled={locked} onClick={() => onAct("purge", s.domain)}>
                {busy(`purge:${s.domain}`) ? t.previewing : t.purge}
              </Button>
            </span>
          ),
        },
      ]}
    />
  );
}
