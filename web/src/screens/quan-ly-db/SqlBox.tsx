// Khối "Chạy SQL" của màn quản lý DB (M32): một câu mỗi lần, kết quả hiện ngay
// bên dưới. Câu chạy bằng tài khoản tạm chỉ có quyền trên đúng DB này; lỗi của
// câu SQL là kết quả (result.error), không phải lỗi của panel.

import { useMutation } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { errorMessage, post } from "../../api/client";
import type { DBSQLResult } from "../../api/m32";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Field, Textarea } from "../../components/Field";
import { Notice } from "../../components/Notice";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/quan-ly-db";
import { ResultTable } from "./ResultTable";

type Props = {
  base: string;
  maxRows: number;
  maxSeconds: number;
  /** Gọi sau mỗi câu chạy xong không lỗi: câu có thể đã tạo hay xoá bảng. */
  onDone: () => void;
};

export function SqlBox({ base, maxRows, maxSeconds, onDone }: Props) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const [sql, setSql] = useState("");
  const run = useMutation({
    mutationFn: (text: string) => post<DBSQLResult>(`${base}/sql`, { sql: text }),
    onSuccess: (res) => {
      if (!res.error) onDone();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (sql.trim() !== "") run.mutate(sql);
  };
  return (
    <>
      <Card title={t.sqlHeading} titleId="db-sql">
        <form onSubmit={submit}>
          <Field label={t.sqlLabel} htmlFor="db-sql-cau" hint={t.sqlHint(maxRows, maxSeconds)} hintId="db-sql-goi-y">
            <Textarea id="db-sql-cau" mono rows={5} value={sql} onChange={(e) => setSql(e.target.value)} aria-describedby="db-sql-goi-y" spellCheck={false} autoCapitalize="none" autoComplete="off" placeholder="SELECT * FROM wp_options LIMIT 10" />
          </Field>
          <p className="mt-[8px] mb-0 max-w-[72ch] text-[13px] text-muted">{t.sqlWarn}</p>
          <div className="pt-[12px]">
            <Button type="submit" variant="primary" disabled={run.isPending || sql.trim() === ""}>
              {run.isPending ? t.sqlRunning : t.sqlRun}
            </Button>
          </div>
        </form>
        {run.isError && (
          <div className="pt-[12px]">
            <Notice kind="fail" role="alert">
              {errorMessage(run.error, lang)}
            </Notice>
          </div>
        )}
      </Card>
      {run.data && !run.isPending && <ResultTable title={t.sqlResultCaption} titleId="db-sql-ket-qua" result={run.data} />}
    </>
  );
}
