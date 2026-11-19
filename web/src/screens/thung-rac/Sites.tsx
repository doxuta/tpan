// Bảng site trong thùng rác: bảng kẻ mảnh như "Lịch sử máy" (mục 7 của
// GIAO-DIEN.md), mỗi hàng một site với hai việc: Khôi phục và Xoá hẳn. Cả hai
// chỉ mở bản xem trước (việc của màn gọi), không chạy ngay.

import { Link } from "react-router";
import type { Site } from "../../api/types";
import { Button } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
import { TableList } from "../../components/TableList";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/thung-rac";
import { dueOf, formatWhen } from "./model";

export type TrashAction = "restore" | "purge";

type Props = {
  sites: Site[];
  /** Hàng đang dựng bản xem trước: khoá mọi nút cho tới khi xong, và đổi chữ nút vừa bấm. */
  pending: { domain: string; action: TrashAction } | null;
  onAct: (action: TrashAction, domain: string) => void;
};

export function TrashTable({ sites, pending, onAct }: Props) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const now = new Date();

  const due = (s: Site): string => {
    const d = dueOf(s.purge_at, now);
    switch (d.kind) {
      case "days":
        return t.daysLeft(d.n);
      case "today":
        return t.today;
      case "overdue":
        return t.overdue;
      default:
        return t.noDue;
    }
  };

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
        { header: t.colTrashed, cell: (s) => <span className="font-mono text-[12px] whitespace-nowrap text-muted">{formatWhen(s.trashed_at, lang)}</span> },
        {
          header: t.colPurge,
          cell: (s) => (
            <span className="whitespace-nowrap">
              <span className="font-mono text-[12px] text-muted">{formatWhen(s.purge_at, lang)}</span>
              <span className="ml-[8px]">{due(s)}</span>
            </span>
          ),
        },
        {
          header: t.colAction,
          className: "pr-0",
          cell: (s) => {
            const busy = (a: TrashAction) => pending?.domain === s.domain && pending.action === a;
            return (
              <span className="flex flex-wrap gap-[8px]">
                <Button aria-label={t.restoreFor(s.domain)} disabled={pending !== null} onClick={() => onAct("restore", s.domain)}>
                  {busy("restore") ? t.previewing : t.restore}
                </Button>
                <Button aria-label={t.purgeFor(s.domain)} disabled={pending !== null} onClick={() => onAct("purge", s.domain)}>
                  {busy("purge") ? t.previewing : t.purge}
                </Button>
              </span>
            );
          },
        },
      ]}
    />
  );
}
