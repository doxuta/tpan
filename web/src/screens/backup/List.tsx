// Bảng các bản backup: bảng kẻ mảnh như "Lịch sử máy" (mục 7 của
// GIAO-DIEN.md), lọc được theo site. Mỗi hàng bản full của site đang chạy có
// "Khôi phục", chỉ mở bản xem trước (việc của màn gọi), không chạy ngay.
// Backup của site đã xoá hẳn vẫn hiện: hàng đó có dấu "site đã xoá" thay vì nút.

import { Link } from "react-router";
import type { Backup } from "../../api/types";
import { Button } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useCopy } from "../../components/useCopy";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/backup";
import { formatWhen } from "../thung-rac/model";
import { formatSize, shortSha, type SiteState } from "./model";

type Props = {
  items: Backup[];
  /** Domain đang lọc; rỗng là mọi site. */
  filter: string;
  onFilter: (domain: string) => void;
  stateOf: (domain: string) => SiteState;
  /** Id bản đang dựng bản xem trước: khoá mọi nút cho tới khi xong, và đổi chữ nút vừa bấm. */
  pendingId: string | null;
  anyPending: boolean;
  onRestore: (b: Backup) => void;
};

const selectClass = "box-border h-[44px] w-full max-w-[360px] border border-ink bg-surface px-[10px] font-mono text-[13px] text-ink sm:h-[40px]";

/** sha256 rút gọn bằng chữ mono: bấm là chép cả chuỗi (MonoText chỉ chép đúng chữ đang hiện nên không dùng được ở đây). */
function Sha({ sha }: { sha: string }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { state, copy } = useCopy();
  return (
    <span className="inline-flex flex-wrap items-baseline gap-x-[8px]">
      <button
        type="button"
        title={t.shaTitle}
        aria-label={t.shaCopy(sha)}
        onClick={() => copy(sha)}
        className="font-mono text-[12px] text-muted hover:text-ink hover:underline"
      >
        {shortSha(sha)}
      </button>
      <span aria-live="polite" className={`text-[12px] ${state === "idle" ? "sr-only" : "text-muted"}`}>
        {state === "copied" ? k.copied : state === "failed" ? k.copyFailed : ""}
      </span>
    </span>
  );
}

export function BackupList({ items, filter, onFilter, stateOf, pendingId, anyPending, onRestore }: Props) {
  const t = useStrings(strings);
  const { lang } = useLang();

  const domains = [...new Set(items.map((b) => b.domain))].sort();
  const rows = filter ? items.filter((b) => b.domain === filter) : items;

  return (
    <>
      <p className="mt-0 mb-[12px] max-w-[720px] text-[13px] text-muted">{t.listHint}</p>
      {domains.length > 1 && (
        <div className="mb-[14px]">
          <label htmlFor="o-backup-filter" className="mb-[4px] block font-medium">
            {t.filterLabel}
          </label>
          <select id="o-backup-filter" value={filter} onChange={(e) => onFilter(e.target.value)} className={selectClass}>
            <option value="">{t.filterAll}</option>
            {domains.map((d) => (
              <option key={d} value={d}>
                {d}
              </option>
            ))}
          </select>
        </div>
      )}
      <TableList
        caption={t.tableName}
        rows={rows}
        rowKey={(b) => b.id}
        empty={filter ? t.emptyFor(filter) : t.emptyAll}
        columns={[
          {
            header: t.colDomain,
            cell: (b) => (
              <span className="flex flex-wrap items-baseline gap-x-[8px] gap-y-[4px]">
                <MonoText>{b.domain}</MonoText>
                {stateOf(b.domain) === "gone" && <Tag>{t.siteGone}</Tag>}
              </span>
            ),
          },
          { header: t.colKind, cell: (b) => <Tag title={b.kind === "full" ? t.kindFull : t.kindSql}>{b.kind}</Tag> },
          { header: t.colReason, cell: (b) => t.reasons[b.reason] ?? b.reason },
          { header: t.colCreated, cell: (b) => <span className="font-mono text-[12px] whitespace-nowrap text-muted">{formatWhen(b.created, lang)}</span> },
          { header: t.colSize, cell: (b) => <span className="font-mono text-[12px] whitespace-nowrap">{formatSize(b.size, lang)}</span> },
          { header: t.colSha, cell: (b) => <Sha sha={b.sha256} /> },
          { header: t.colPath, cell: (b) => <MonoText size={12} muted>{b.path}</MonoText> },
          {
            header: t.colAction,
            className: "pr-0",
            cell: (b) => {
              if (b.kind === "sql") {
                return (
                  <span className="text-[13px] text-muted">
                    {t.sqlLead}
                    <Link to="/databases">{t.sqlLink}</Link>
                    {t.sqlTail}
                  </span>
                );
              }
              const st = stateOf(b.domain);
              if (st === "trashed") {
                return (
                  <span className="text-[13px] text-muted">
                    {t.trashedLead}
                    <Link to="/trash">{t.trashedLink}</Link>
                    {t.trashedTail}
                  </span>
                );
              }
              if (st === "gone") return <span className="text-[13px] text-muted">{t.goneNote}</span>;
              return (
                <Button aria-label={t.restoreFor(b.domain, formatWhen(b.created, lang))} disabled={anyPending} onClick={() => onRestore(b)}>
                  {pendingId === b.id ? t.previewing : t.restore}
                </Button>
              );
            },
          },
        ]}
      />
    </>
  );
}
