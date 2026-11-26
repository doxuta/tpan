// Màn quản lý một DB (M32, v0.2.14): /databases/site/:id (DB của một site, id là
// tên miền) và /databases/db/:id (DB rời, id là tên DB). Bốn khối:
//
//   Nối vào DB này   tên DB, user, host, cổng để điền vào ứng dụng
//   Bảng             danh sách bảng, mở xem dữ liệu, công cụ bảng (kiểm tra, sửa, tối ưu)
//   Dữ liệu của bảng lưới dòng có phân trang, thêm, sửa, xoá từng dòng (bảng có khoá chính)
//   Chạy SQL         một câu mỗi lần, kết quả hiện ngay bên dưới
//   Bản sao lưu      (M33) sao lưu ngay, nhập lại, tải về, xoá các bản .sql của DB này
//
// Mọi câu chạy bằng một tài khoản MariaDB tạm chỉ có quyền trên đúng DB này
// (internal/core/dbdata.go), nên màn này không cần và không có "bản xem trước":
// đây là sửa dữ liệu của chính người dùng, như phpMyAdmin, không phải đổi cấu hình máy.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useParams } from "react-router";
import { errorMessage, get, post } from "../../api/client";
import { dbDataBase, dbInfoKey, type DBInfo, type DBKind, type DBSQLResult, type DBTable, type DBToolOp } from "../../api/m32";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { EmptyState } from "../../components/EmptyState";
import { MonoText } from "../../components/MonoText";
import { Notice } from "../../components/Notice";
import { RowActions } from "../../components/RowActions";
import { ScreenHeader } from "../../components/ScreenHeader";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/quan-ly-db";
import { formatSize } from "../backup/model";
import { DbBackups } from "./Backups";
import { ResultTable } from "./ResultTable";
import { Rows } from "./Rows";
import { SqlBox } from "./SqlBox";

function DbManage({ kind }: { kind: DBKind }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const { id = "" } = useParams();
  const base = dbDataBase(kind, id);
  const qc = useQueryClient();
  const q = useQuery({ queryKey: dbInfoKey(base), queryFn: () => get<DBInfo>(`${base}/data`) });
  const [table, setTable] = useState<string | null>(null);
  const [tool, setTool] = useState<{ op: DBToolOp; table: string; result: DBSQLResult } | null>(null);

  const run = useMutation({
    mutationFn: (v: { op: DBToolOp; table: string }) => post<DBSQLResult>(`${base}/data/${encodeURIComponent(v.table)}/maintain`, { op: v.op }),
    onSuccess: (result, v) => {
      setTool({ ...v, result });
      void qc.invalidateQueries({ queryKey: dbInfoKey(base) });
    },
  });

  const info = q.data;
  const toolNames: Record<DBToolOp, string> = { check: t.toolCheck, repair: t.toolRepair, optimize: t.toolOptimize, analyze: t.toolAnalyze };

  return (
    <>
      <ScreenHeader crumbs={[{ label: t.crumb, to: "/databases" }, { label: info?.db ?? id }]} title={info?.db ?? id} summary={kind === "site" ? t.summarySite(id) : t.summaryStandalone} />

      {q.isPending && (
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {q.isError && (
        <Card>
          <p className="mt-0 mb-[4px] font-semibold">{t.loadFailed}</p>
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </Card>
      )}

      {info && (
        <>
          {info.suspended && (
            <Card>
              <Notice kind="warn">{t.suspended}</Notice>
            </Card>
          )}

          <Card title={t.connHeading} titleId="db-noi">
            <dl className="m-0 grid grid-cols-[max-content_1fr] gap-x-[16px] gap-y-[8px]">
              <dt className="text-muted">{t.connDb}</dt>
              <dd className="m-0">
                <MonoText>{info.db}</MonoText>
              </dd>
              <dt className="text-muted">{t.connUser}</dt>
              <dd className="m-0">
                <MonoText>{info.user}</MonoText>
              </dd>
              <dt className="text-muted">{t.connHost}</dt>
              <dd className="m-0">
                <MonoText>{info.conn.host}</MonoText>
              </dd>
              <dt className="text-muted">{t.connPort}</dt>
              <dd className="m-0">
                <MonoText>{String(info.conn.port)}</MonoText>
              </dd>
            </dl>
            <p className="mt-[12px] mb-0 max-w-[72ch] text-[13px] text-muted">
              {t.connHint} {t.connLocalOnly}
            </p>
          </Card>

          <TableList<DBTable>
            title={t.tablesHeading}
            titleId="db-bang"
            count={t.tablesCount(info.tables.length)}
            caption={t.tablesCaption(info.db)}
            minWidth={640}
            rows={info.tables}
            rowKey={(r) => r.name}
            empty={<EmptyState>{t.tablesEmpty}</EmptyState>}
            columns={[
              {
                header: t.colTable,
                nowrap: false,
                cell: (r) => (
                  <span className="flex flex-wrap items-center gap-x-[8px] gap-y-[2px]">
                    <button type="button" onClick={() => setTable(r.name)} aria-label={t.openFor(r.name)} className="bg-transparent p-0 text-left font-medium text-accent hover:underline">
                      {r.name}
                    </button>
                    {r.view && <Tag>{t.viewTag}</Tag>}
                  </span>
                ),
              },
              { header: t.colRows, align: "right", cell: (r) => <span title={t.colRowsHint}>{r.view ? "" : r.rows.toLocaleString(lang)}</span> },
              { header: t.colSize, align: "right", cell: (r) => (r.view ? "" : formatSize(r.bytes, lang)) },
              { header: t.colEngine, cell: (r) => <span className="text-muted">{r.engine}</span> },
              {
                header: t.colAction,
                align: "right",
                cell: (r) => (
                  <RowActions
                    moreLabel={t.moreFor(r.name)}
                    primary={[{ label: t.open, ariaLabel: t.openFor(r.name), onClick: () => setTable(r.name) }]}
                    more={r.view ? [] : (["check", "repair", "optimize", "analyze"] as const).map((op) => ({ label: toolNames[op], disabled: run.isPending, onSelect: () => run.mutate({ op, table: r.name }) }))}
                  />
                ),
              },
            ]}
          />

          {run.isPending && (
            <Card>
              <p className="m-0 text-muted">{t.toolRunning}</p>
            </Card>
          )}
          {run.isError && (
            <Card>
              <Notice kind="fail" role="alert">
                {errorMessage(run.error, lang)}
              </Notice>
            </Card>
          )}
          {tool && !run.isPending && <ResultTable title={t.toolResult(toolNames[tool.op], tool.table)} titleId="db-cong-cu" result={tool.result} onClose={() => setTool(null)} />}

          {table !== null && <Rows key={table} base={base} table={table} onClose={() => setTable(null)} />}

          <SqlBox base={base} maxRows={info.max_rows} maxSeconds={info.max_seconds} onDone={() => void qc.invalidateQueries({ queryKey: dbInfoKey(base) })} />

          {/* M33: bản sao lưu của chính DB này (DB rời không nằm trong bản backup của site nào). */}
          <DbBackups kind={kind} id={id} db={info.db} suspended={info.suspended} />
        </>
      )}
    </>
  );
}

export function DbManageSite() {
  return <DbManage kind="site" />;
}

export function DbManageStandalone() {
  return <DbManage kind="standalone" />;
}
