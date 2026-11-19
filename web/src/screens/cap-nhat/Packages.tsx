// Khối (a): gói của stack có bản mới, số gói khác của hệ điều hành, và nút
// "Cập nhật N gói". Không có gói nào thì nói rõ và không có nút.

import type { UpdateStatus } from "../../api/types";
import { Button } from "../../components/Button";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/cap-nhat";
import { formatDateTime } from "../lich-su/format";
import { packagesAction, type Flow } from "./model";

export function Packages({ status, flow }: { status: UpdateStatus; flow: Flow }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  // Go có thể trả null cho danh sách rỗng.
  const packages = status.packages ?? [];
  const security = packages.filter((p) => p.security).length;
  const pending = flow.pending === packagesAction.id;

  return (
    <section aria-labelledby="cn-goi" className="pt-[26px]">
      <h2 id="cn-goi" className="m-0 pb-[8px] text-[16px] font-semibold">
        {t.packagesHeading}
      </h2>

      {packages.length === 0 ? (
        <p className="m-0 border-t border-hair py-[10px] text-muted">{t.packagesNone}</p>
      ) : (
        <>
          <p className="m-0 pb-[10px]">{t.packagesLead(packages.length, security)}</p>
          <TableList
            caption={t.packagesTable}
            rows={packages}
            rowKey={(p) => p.name}
            columns={[
              { header: t.colPackage, cell: (p) => <span className="font-mono text-[13px] break-all">{p.name}</span> },
              { header: t.colFrom, cell: (p) => <span className="font-mono text-[13px] break-all text-muted">{p.from}</span> },
              { header: t.colTo, cell: (p) => <span className="font-mono text-[13px] break-all">{p.to}</span> },
              { header: t.colKind, cell: (p) => (p.security ? <Tag>{t.securityTag}</Tag> : <span className="text-muted">{t.noValue}</span>) },
            ]}
            empty={null}
          />
          <p className="mt-[12px] mb-[10px] max-w-[720px] text-muted">
            {t.packagesHint} {t.previewHint}
          </p>
          <Button id={`act-${packagesAction.id}`} variant="primary" onClick={() => flow.start(packagesAction)} disabled={flow.busy}>
            {pending ? t.previewing : t.packagesButton(packages.length)}
          </Button>
          {flow.errorFor(packagesAction) && <div className="pt-[12px]">{flow.errorFor(packagesAction)}</div>}
        </>
      )}

      <p className="mt-[14px] mb-0 max-w-[720px] text-muted">{t.others(status.others, status.others_security)}</p>
      {status.lists_updated && <p className="mt-[4px] mb-0 text-muted">{t.listsUpdated(formatDateTime(status.lists_updated, lang))}</p>}
    </section>
  );
}
