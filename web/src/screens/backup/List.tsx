// Bảng các bản backup (bản 2): một TableList trong thẻ, lọc được theo site. Mỗi
// hàng bản full của site đang chạy có "Khôi phục", chỉ mở bản xem trước (việc
// của màn gọi), không chạy ngay. Backup của site đã xoá hẳn vẫn hiện: hàng đó
// có nhãn "site đã xoá" thay vì nút.

import { Link } from "react-router";
import type { Backup } from "../../api/types";
import { Code, MonoText } from "../../components/MonoText";
import { Field, Select } from "../../components/Field";
import { RowActions } from "../../components/RowActions";
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
        className="bg-transparent p-0 text-left text-muted hover:text-ink hover:underline max-sm:min-h-[24px] max-sm:min-w-[24px]"
      >
        <Code>{shortSha(sha)}</Code>
      </button>
      <span aria-live="polite" className={`text-[12px] ${state === "idle" ? "sr-only" : "text-muted"}`}>
        {state === "copied" ? k.copied : state === "failed" ? k.copyFailed : ""}
      </span>
    </span>
  );
}

/** Ghi chú thay cho nút ở cột Việc (site trong thùng rác, site đã xoá, bản sql). */
const note = "inline-block max-w-[260px] text-[12.5px] text-muted";

export function BackupList({ items, filter, onFilter, stateOf, pendingId, anyPending, onRestore }: Props) {
  const t = useStrings(strings);
  const { lang } = useLang();

  const domains = [...new Set(items.map((b) => b.domain))].sort();
  const rows = filter ? items.filter((b) => b.domain === filter) : items;

  return (
    <TableList
      title={t.listHeading}
      titleId="backup-list"
      caption={t.tableName}
      minWidth={1040}
      rows={rows}
      rowKey={(b) => b.id}
      empty={filter ? t.emptyFor(filter) : t.emptyAll}
      above={
        <div className="flex flex-col gap-[12px] px-[16px] py-[12px]">
          <p className="m-0 max-w-[72ch] text-[13px] text-muted">{t.listHint}</p>
          {domains.length > 1 && (
            <div className="max-w-[360px]">
              <Field label={t.filterLabel} htmlFor="o-backup-filter">
                <Select id="o-backup-filter" value={filter} onChange={(e) => onFilter(e.target.value)}>
                  <option value="">{t.filterAll}</option>
                  {domains.map((d) => (
                    <option key={d} value={d}>
                      {d}
                    </option>
                  ))}
                </Select>
              </Field>
            </div>
          )}
        </div>
      }
      columns={[
        {
          header: t.colDomain,
          cell: (b) => (
            <span className="flex items-center gap-[8px]">
              {/* Tên miền dài nằm một dòng, cắt bằng dấu ba chấm; đủ chữ trong title. */}
              <span title={b.domain} className="inline-block max-w-[280px] truncate font-medium">
                {b.domain}
              </span>
              {stateOf(b.domain) === "gone" && <Tag>{t.siteGone}</Tag>}
            </span>
          ),
        },
        { header: t.colKind, cell: (b) => <Tag title={b.kind === "full" ? t.kindFull : t.kindSql}>{b.kind}</Tag> },
        { header: t.colReason, nowrap: false, className: "min-w-[140px]", cell: (b) => t.reasons[b.reason] ?? b.reason },
        { header: t.colCreated, cell: (b) => <span className="tabular-nums">{formatWhen(b.created, lang)}</span> },
        { header: t.colSize, align: "right", cell: (b) => formatSize(b.size, lang) },
        { header: t.colSha, cell: (b) => <Sha sha={b.sha256} /> },
        { header: t.colPath, nowrap: false, className: "min-w-[200px]", cell: (b) => <MonoText size={12} muted>{b.path}</MonoText> },
        {
          header: t.colAction,
          align: "right",
          nowrap: false,
          cell: (b) => {
            if (b.kind === "sql") {
              return (
                <span className={note}>
                  {t.sqlLead}
                  <Link to="/databases">{t.sqlLink}</Link>
                  {t.sqlTail}
                </span>
              );
            }
            const st = stateOf(b.domain);
            if (st === "trashed") {
              return (
                <span className={note}>
                  {t.trashedLead}
                  <Link to="/trash">{t.trashedLink}</Link>
                  {t.trashedTail}
                </span>
              );
            }
            if (st === "gone") return <span className={note}>{t.goneNote}</span>;
            const label = t.restoreFor(b.domain, formatWhen(b.created, lang));
            return (
              <RowActions
                moreLabel={label}
                primary={[{ label: t.restore, ariaLabel: label, disabled: anyPending, busy: pendingId === b.id, busyLabel: t.previewing, onClick: () => onRestore(b) }]}
              />
            );
          },
        },
      ]}
    />
  );
}
