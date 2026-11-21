// Khối (a): gói của stack có bản mới, số gói khác của hệ điều hành, và nút
// "Cập nhật N gói". Không có gói nào thì nói rõ và không có nút. Bản 2: có gói
// thì khối là một bảng tự vẽ thẻ (nút nằm ở đầu thẻ, lời dẫn và lỗi xem trước
// ở phần giữa, ghi chú ở chân); không có gói thì là một thẻ thường.

import type { UpdateStatus } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { EmptyState } from "../../components/EmptyState";
import { MonoText } from "../../components/MonoText";
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
  const error = flow.errorFor(packagesAction);

  const notes = (
    <>
      <span>{t.others(status.others, status.others_security)}</span>
      {status.lists_updated && <span>{t.listsUpdated(formatDateTime(status.lists_updated, lang))}</span>}
    </>
  );

  if (packages.length === 0) {
    return (
      <Card title={t.packagesHeading} titleId="cn-goi" footer={notes}>
        <EmptyState>{t.packagesNone}</EmptyState>
      </Card>
    );
  }

  return (
    <TableList
      title={t.packagesHeading}
      titleId="cn-goi"
      caption={t.packagesTable}
      minWidth={640}
      actions={
        <Button id={`act-${packagesAction.id}`} variant="primary" onClick={() => flow.start(packagesAction)} disabled={flow.busy}>
          {pending ? t.previewing : t.packagesButton(packages.length)}
        </Button>
      }
      above={
        <div className="flex flex-col gap-[6px] px-[16px] py-[12px]">
          <p className="m-0">{t.packagesLead(packages.length, security)}</p>
          <p className="m-0 max-w-[72ch] text-[13px] text-muted">
            {t.packagesHint} {t.previewHint}
          </p>
          {error && <div className="pt-[6px]">{error}</div>}
        </div>
      }
      footer={notes}
      rows={packages}
      rowKey={(p) => p.name}
      empty={null}
      columns={[
        { header: t.colPackage, nowrap: false, cell: (p) => <MonoText>{p.name}</MonoText> },
        { header: t.colFrom, nowrap: false, cell: (p) => <MonoText muted>{p.from}</MonoText> },
        { header: t.colTo, nowrap: false, cell: (p) => <MonoText>{p.to}</MonoText> },
        { header: t.colKind, cell: (p) => (p.security ? <Tag>{t.securityTag}</Tag> : <span className="text-muted">{t.noValue}</span>) },
      ]}
    />
  );
}
