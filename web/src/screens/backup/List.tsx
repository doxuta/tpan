// Bảng các bản backup (bản 2): một TableList trong thẻ, lọc được theo site. Mỗi
// hàng bản full của site đang chạy có "Khôi phục", chỉ mở bản xem trước (việc
// của màn gọi), không chạy ngay. Backup của site đã xoá hẳn vẫn hiện: hàng đó
// có nhãn "site đã xoá" thay vì nút.
//
// M18: cột "Ở đâu" (nơi bản nằm, chỉ hiện khi máy có dùng kho, B11) và hai việc
// kho cho admin ở cột Việc: "Đẩy lên kho", "Tải về máy". Khi cột "Ở đâu" hiện
// thì hai cột "sha256" và "Đường dẫn" ẩn (lý do ở chỗ minWidth bên dưới). Luật hiện việc nào ở
// hàng nào nằm trong model.ts (storageWork) để test được.

import type { ReactNode } from "react";
import { Link } from "react-router";
import type { Backup } from "../../api/types";
import { Code, MonoText } from "../../components/MonoText";
import { Field, Select } from "../../components/Field";
import { RowActions, type RowAction } from "../../components/RowActions";
import { StatusLine } from "../../components/StatusMark";
import { TableList, type Column } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useCopy } from "../../components/useCopy";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/backup";
import { formatWhen } from "../thung-rac/model";
import { formatSize, shortSha, showWhere, storageWork, type PendingRow, type SiteState } from "./model";

type Props = {
  items: Backup[];
  /** Domain đang lọc; rỗng là mọi site. */
  filter: string;
  onFilter: (domain: string) => void;
  stateOf: (domain: string) => SiteState;
  /** Hàng đang dựng bản xem trước: khoá mọi nút cho tới khi xong, và đổi chữ nút vừa bấm. */
  pending: PendingRow;
  anyPending: boolean;
  onRestore: (b: Backup) => void;
  /** Xoá file của bản backup khỏi máy (chủ bản backup hoặc admin). */
  onDelete: (b: Backup) => void;
  /** Việc kho (đẩy lên kho, tải về máy): chỉ admin. */
  isAdmin: boolean;
  /** Đã đặt đích ngoài máy chưa (admin đọc từ GET /backups/target); undefined với user thường hoặc khi chưa đọc được. */
  targetConfigured: boolean | undefined;
  onPush: (b: Backup) => void;
  onPull: (b: Backup) => void;
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

/** Câu lỗi của lần đẩy gần nhất, một dòng dưới nhãn (chỉ admin nhận remote_error). */
function PushError({ error }: { error?: string }) {
  if (!error) return null;
  return (
    <span title={error} className="inline-block max-w-[180px] truncate text-[12.5px] text-muted">
      {error}
    </span>
  );
}

/**
 * Nơi bản nằm, theo `location` máy chủ đã tính, với một ngoại lệ: dòng "kho khác"
 * mà file vẫn còn trên máy (`local`) thì ghi "máy". Máy chủ tính "kho khác" chỉ
 * từ cột kho (sau khi gỡ hay đổi đích), nhưng người đọc bảng lúc cần khôi phục
 * hỏi "bản này có trên máy không": bản đó khôi phục được ngay (G2 của lượt soát).
 * Đẩy lỗi là trạng thái cảnh báo nên dùng dấu cảnh báo kèm chữ; admin có thêm câu
 * lỗi của lần đẩy gần nhất (user thường không nhận trường này nên không có dòng đó),
 * ở cả dòng "kho khác": lần đẩy lên đích mới lỗi thì dòng vẫn giữ đích cũ.
 * `configured`: đã đặt đích chưa (undefined với user thường, họ không đọc được đích);
 * chỉ khi chắc là đã đặt mới nói "khác với kho đang đặt".
 */
export function Where({ b, configured }: { b: Backup; configured: boolean | undefined }) {
  const t = useStrings(strings);
  const loc = b.location;
  if (loc === undefined) return null;
  if (loc === "push_failed") {
    return (
      <span className="flex flex-col items-start gap-[2px]">
        <StatusLine status="warn">{t.locations.push_failed}</StatusLine>
        <PushError error={b.remote_error} />
      </span>
    );
  }
  if (loc === "other_target") {
    const which = configured === true ? "set" : configured === false ? "unset" : "unknown";
    return (
      <span className="flex flex-col items-start gap-[2px]">
        {b.local ? <Tag title={t.otherLocalTitles[which]}>{t.locations.machine}</Tag> : <Tag title={t.otherTitles[which]}>{t.locations.other_target}</Tag>}
        <PushError error={b.remote_error} />
      </span>
    );
  }
  return <Tag title={t.locTitles[loc]}>{t.locations[loc]}</Tag>;
}

/** Ghi chú thay cho nút ở cột Việc (site trong thùng rác, site đã xoá, bản sql). */
const note = "inline-block max-w-[200px] text-[12.5px] text-muted";

export function BackupList({ items, filter, onFilter, stateOf, pending, anyPending, onRestore, onDelete, isAdmin, targetConfigured, onPush, onPull }: Props) {
  const t = useStrings(strings);
  const { lang } = useLang();

  const domains = [...new Set(items.map((b) => b.domain))].sort();
  const rows = filter ? items.filter((b) => b.domain === filter) : items;
  // Tính trên cả danh sách chứ không trên các hàng đang lọc, để cột không nhảy ra vào khi đổi bộ lọc.
  const where = showWhere(items, targetConfigured);

  /** Cột Việc của một hàng: "Khôi phục" (nếu được) rồi một việc kho (nếu được); tối đa hai nút, đúng luật của RowActions. */
  function actions(b: Backup): ReactNode {
    const when = formatWhen(b.created, lang);
    let lead: ReactNode = null;
    const primary: RowAction[] = [];
    if (b.kind === "sql") {
      lead = (
        <span className={note}>
          {t.sqlLead}
          <Link to="/databases">{t.sqlLink}</Link>
          {t.sqlTail}
        </span>
      );
    } else {
      const st = stateOf(b.domain);
      if (st === "trashed") {
        lead = (
          <span className={note}>
            {t.trashedLead}
            <Link to="/trash">{t.trashedLink}</Link>
            {t.trashedTail}
          </span>
        );
      } else if (st === "gone") {
        // Site đã xoá hẳn: máy chủ tạo lại site rồi khôi phục (POST /backups/{id}/restore).
        lead = <span className={note}>{t.goneNote}</span>;
        primary.push({ label: t.restoreRecreate, ariaLabel: t.restoreRecreateFor(b.domain, when), disabled: anyPending, busy: pending?.action === "restore" && pending.id === b.id, busyLabel: t.previewing, onClick: () => onRestore(b) });
      } else {
        const label = t.restoreFor(b.domain, when);
        // Bản chỉ còn trên kho vẫn có nút này: màn hỏi tải về trước (admin) hoặc nhắn nhờ admin (user thường), xem index.tsx.
        primary.push({ label: t.restore, ariaLabel: label, disabled: anyPending, busy: pending?.action === "restore" && pending.id === b.id, busyLabel: t.previewing, onClick: () => onRestore(b) });
      }
    }
    // Việc kho không phụ thuộc site còn hay không: file backup của site đã xoá vẫn đẩy hay tải được.
    const work = storageWork(b, isAdmin, targetConfigured);
    if (work) {
      const push = work === "push";
      const label = push ? t.pushFor(b.domain, when) : t.pullFor(b.domain, when);
      primary.push({ label: push ? t.push : t.pull, ariaLabel: label, disabled: anyPending, busy: pending?.action === work && pending.id === b.id, busyLabel: t.previewing, onClick: () => (push ? onPush(b) : onPull(b)) });
    }
    // Xoá: chỉ bản còn file trên máy (bản chỉ còn trên kho do kho tự dọn).
    // Tải về máy tính: trình duyệt tự lưu file (máy chủ trả Content-Disposition: attachment).
    const more = b.local
      ? [
          { label: t.download, onSelect: () => window.location.assign(`/api/v1/backups/${encodeURIComponent(b.id)}/download`) },
          { label: t.deleteLocal, danger: true, disabled: anyPending, onSelect: () => onDelete(b) },
        ]
      : [];
    if (primary.length === 0 && more.length === 0) return lead;
    const buttons = <RowActions moreLabel={t.moreFor(b.domain, when)} primary={primary} more={more} />;
    if (!lead) return buttons;
    return (
      <span className="flex flex-col items-end gap-[4px]">
        {lead}
        {buttons}
      </span>
    );
  }

  const whereColumn: Column<Backup> = { header: t.colWhere, cell: (b) => <Where b={b} configured={targetConfigured} /> };
  const shaColumn: Column<Backup> = { header: t.colSha, cell: (b) => <Sha sha={b.sha256} /> };
  const pathColumn: Column<Backup> = { header: t.colPath, nowrap: false, className: "min-w-[200px]", cell: (b) => <MonoText size={12} muted>{b.path}</MonoText> };

  return (
    <TableList
      title={t.listHeading}
      titleId="backup-list"
      caption={t.tableName}
      // Cột "Ở đâu" thay chỗ hai cột "sha256" và "Đường dẫn" chứ không thêm vào:
      // thêm vào thì tổng bề rộng tự nhiên của các cột (đa số không xuống dòng)
      // vượt khung nội dung ở 1440 × 900 (khoảng 1150 px), cột Việc bị đẩy ra
      // ngoài và phải cuộn ngang mới thấy nút "Khôi phục" (phép kiểm
      // restoreButtonsInView của make anh-m18 đo được). Hai cột đó là chi tiết để
      // đối chiếu, còn xem được bằng `tpan backup list --json`; với bản chỉ còn
      // trên kho thì đường dẫn trên máy cũng không còn nghĩa.
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
        ...(where ? [whereColumn] : []),
        ...(where ? [] : [shaColumn, pathColumn]),
        {
          header: t.colAction,
          align: "right",
          nowrap: false,
          cell: actions,
        },
      ]}
    />
  );
}
