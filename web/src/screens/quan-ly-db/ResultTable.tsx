// Bảng kết quả của một câu SQL hay một công cụ bảng (M32): cột là tên cột MariaDB
// trả, ô NULL hiện chữ NULL mờ. Kết quả là dữ liệu của khách nên rộng tới đâu thì
// bảng tự cuộn ngang trong thẻ của nó (đây là lưới dữ liệu, không phải bảng của panel).

import type { DBCell, DBSQLResult } from "../../api/m32";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Notice } from "../../components/Notice";
import { TableList } from "../../components/TableList";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/quan-ly-db";

/** Một ô dữ liệu: NULL mờ, chữ dài cắt bằng dấu ba chấm (đủ chữ trong title). */
export function Cell({ value, clipped }: { value: DBCell; clipped?: boolean }) {
  const t = useStrings(strings);
  if (value === null) return <span className="text-muted italic">{t.nullValue}</span>;
  return (
    <span title={clipped ? t.clippedTitle : value.length > 60 ? value : undefined} className="inline-block max-w-[320px] truncate align-bottom">
      {value}
      {clipped ? t.clipped : ""}
    </span>
  );
}

type Props = {
  title: string;
  titleId: string;
  result: DBSQLResult;
  onClose?: () => void;
};

export function ResultTable({ title, titleId, result, onClose }: Props) {
  const t = useStrings(strings);
  const close = onClose ? <Button onClick={onClose}>{t.rowsClose}</Button> : undefined;
  if (result.error) {
    return (
      <Card title={title} titleId={titleId} actions={close}>
        <Notice kind="fail" role="alert">
          <span className="font-semibold">{t.sqlError}</span>
          <span className="mt-[4px] block [overflow-wrap:anywhere]">{result.error}</span>
        </Notice>
      </Card>
    );
  }
  if (result.columns.length === 0) {
    return (
      <Card title={title} titleId={titleId} actions={close}>
        <Notice kind="pass" role="status">
          {result.affected >= 0 ? t.sqlAffected(result.affected) : t.sqlNoRows}
        </Notice>
      </Card>
    );
  }
  return (
    <TableList<DBCell[]>
      title={title}
      titleId={titleId}
      count={t.sqlRows(result.rows.length, result.ms)}
      caption={t.sqlResultCaption}
      actions={close}
      rows={result.rows}
      rowKey={(r) => String(result.rows.indexOf(r))}
      empty={t.sqlNoRows}
      footer={result.truncated ? <span>{t.sqlTruncated(result.rows.length)}</span> : undefined}
      columns={result.columns.map((name, i) => ({ header: name, cell: (r: DBCell[]) => <Cell value={r[i] ?? null} /> }))}
    />
  );
}
