// Phần cấu hình của màn Stack: web server, nguồn của web server và MariaDB, bản
// PHP mặc định (GET /api/v1/stack), rồi các dịch vụ kèm bằng chứng. Nguồn chỉ
// chọn được lúc cài stack, nên không có nút đổi: màn nói rõ điều đó và đưa một
// lệnh ví dụ để người dùng tự gõ (CommandBlock), kèm --dry-run để chỉ xem trước.

import { useQuery } from "@tanstack/react-query";
import { errorMessage, get } from "../../api/client";
import type { StackStatus } from "../../api/types";
import { Button } from "../../components/Button";
import { CommandBlock } from "../../components/CommandBlock";
import { MonoText } from "../../components/MonoText";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/stack";
import { CheckList } from "../site/CheckItem";
import { sourceName, stackKey } from "./model";

const labelClass = "m-0 font-mono text-[11px] font-normal tracking-[0.08em] text-muted uppercase";

export function Config() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({ queryKey: stackKey, queryFn: () => get<StackStatus>("/api/v1/stack") });

  return (
    <section aria-labelledby="st-config" className="pt-[26px]">
      <h2 id="st-config" className="m-0 pb-[10px] text-[16px] font-semibold">
        {t.configHeading}
      </h2>
      {q.isPending && <p className="m-0 border-t border-hair pt-[10px] text-muted">{k.loading}</p>}
      {q.isError && (
        <div className="border-t border-hair pt-[10px]">
          <p className="mt-0 mb-[4px] font-semibold">{t.loadFailed}</p>
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </div>
      )}
      {q.isSuccess && <Loaded status={q.data} />}
    </section>
  );
}

function Loaded({ status }: { status: StackStatus }) {
  const t = useStrings(strings);
  const { config } = status;
  const names = { ubuntu: t.sourceUbuntu, sury: t.sourcePhpSury };
  // Go có thể trả null cho danh sách rỗng.
  const services = status.services ?? [];

  const value = (v: string | undefined) => (v ? <MonoText>{v}</MonoText> : <span className="text-muted">{t.noValue}</span>);

  return (
    <>
      {!status.installed && <p className="m-0 mb-[10px] max-w-[720px] text-muted">{t.notInstalled}</p>}
      <dl className="m-0 grid grid-cols-2 gap-x-[16px] gap-y-[12px] border-t border-hair pt-[12px] sm:flex sm:flex-wrap sm:gap-x-[40px]">
        <div>
          <dt className={labelClass}>{t.cfgWeb}</dt>
          <dd className="m-0 mt-[4px]">{value(config.web)}</dd>
        </div>
        <div>
          <dt className={labelClass}>{t.cfgWebSource}</dt>
          <dd className="m-0 mt-[4px]">{value(config.web_source && sourceName(config.web_source, names))}</dd>
        </div>
        <div>
          <dt className={labelClass}>{t.cfgDbSource}</dt>
          <dd className="m-0 mt-[4px]">
            {value(config.db_source && sourceName(config.db_source, names))}
            {config.db_line && <span className="text-muted"> · {t.dbLine(config.db_line)}</span>}
          </dd>
        </div>
        <div>
          <dt className={labelClass}>{t.cfgPhp}</dt>
          <dd className="m-0 mt-[4px]">{value(config.php_version)}</dd>
        </div>
      </dl>

      <p className="mt-[14px] mb-[10px] max-w-[720px]">{t.sourceFixed}</p>
      <CommandBlock commands={[t.installExample]} />

      <h3 className={`mt-[22px] mb-[6px] ${labelClass}`}>{t.servicesHeading}</h3>
      {services.length === 0 ? <p className="m-0 border-t border-hair pt-[10px] text-muted">{t.servicesNone}</p> : (
        <div className="border-t border-hair">
          <CheckList checks={services} />
        </div>
      )}
    </>
  );
}
