// Phần cấu hình của màn Stack: web server, nguồn của web server và MariaDB, bản
// PHP mặc định (GET /api/v1/stack), rồi các dịch vụ kèm bằng chứng. Nguồn chỉ
// chọn được lúc cài stack, nên không có nút đổi: màn nói rõ điều đó và đưa một
// lệnh ví dụ để người dùng tự gõ (CommandBlock), kèm --dry-run để chỉ xem trước.
// Bản 2: hai thẻ, "Cấu hình" và "Dịch vụ".

import { useQuery } from "@tanstack/react-query";
import { errorMessage, get } from "../../api/client";
import type { StackStatus } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { CommandBlock } from "../../components/CommandBlock";
import { MonoText } from "../../components/MonoText";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/stack";
import { CheckList } from "../site/CheckItem";
import { sourceName, stackKey } from "./model";

const labelClass = "m-0 text-[12px] font-medium text-muted";

export function Config() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({ queryKey: stackKey, queryFn: () => get<StackStatus>("/api/v1/stack") });

  if (q.isSuccess) return <Loaded status={q.data} />;
  return (
    <Card title={t.configHeading} titleId="st-config">
      {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div>
          <p className="mt-0 mb-[4px] font-semibold">{t.loadFailed}</p>
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </div>
      )}
    </Card>
  );
}

function Loaded({ status }: { status: StackStatus }) {
  const t = useStrings(strings);
  const { config } = status;
  // Go có thể trả null cho danh sách rỗng.
  const services = status.services ?? [];

  const value = (v: string | undefined) => (v ? <MonoText>{v}</MonoText> : <span className="text-muted">{t.noValue}</span>);
  // "ubuntu" và "sury" có tên thường ngày (chữ thường); tên kho còn lại (nginx.org, mariadb.org) là giá trị cấu hình nên mono.
  const source = (s: string | undefined) => {
    if (!s) return value(s);
    return s === "ubuntu" || s === "sury" ? <span>{sourceName(s, { ubuntu: t.sourceUbuntu, sury: t.sourcePhpSury })}</span> : value(s);
  };

  return (
    <>
      <Card title={t.configHeading} titleId="st-config">
        <div className="flex flex-col gap-[16px]">
          {!status.installed && <p className="m-0 max-w-[72ch] text-muted">{t.notInstalled}</p>}
          <dl className="m-0 grid grid-cols-2 gap-x-[16px] gap-y-[12px] sm:flex sm:flex-wrap sm:gap-x-[40px]">
            <div>
              <dt className={labelClass}>{t.cfgWeb}</dt>
              <dd className="m-0 mt-[4px]">{value(config.web)}</dd>
            </div>
            <div>
              <dt className={labelClass}>{t.cfgWebSource}</dt>
              <dd className="m-0 mt-[4px]">{source(config.web_source)}</dd>
            </div>
            <div>
              <dt className={labelClass}>{t.cfgDbSource}</dt>
              <dd className="m-0 mt-[4px]">
                {source(config.db_source)}
                {config.db_line && <span className="text-muted"> · {t.dbLine(config.db_line)}</span>}
              </dd>
            </div>
            <div>
              <dt className={labelClass}>{t.cfgPhp}</dt>
              <dd className="m-0 mt-[4px]">{value(config.php_version)}</dd>
            </div>
          </dl>
          <div className="flex flex-col gap-[10px]">
            <p className="m-0 max-w-[72ch]">{t.sourceFixed}</p>
            <CommandBlock commands={[t.installExample]} />
          </div>
        </div>
      </Card>

      <Card title={t.servicesHeading} titleId="st-services">
        {services.length === 0 ? <p className="m-0 text-muted">{t.servicesNone}</p> : <CheckList checks={services} />}
      </Card>
    </>
  );
}
