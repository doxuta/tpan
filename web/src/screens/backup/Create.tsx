// Thẻ "Backup ngay": chọn một site đang chạy rồi bấm. Bấm chỉ mở bản xem
// trước (việc của màn gọi); máy chỉ đổi sau khi người dùng bấm chạy ở đó.
// Chỉ liệt kê site đang chạy. Core vẫn cho backup site trong thùng rác
// (PrepareCreate nhận cả hai), nhưng việc đó không có nút ở đây: site đó nên
// khôi phục khỏi thùng rác trước.

import { useState, type FormEvent } from "react";
import { Link } from "react-router";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Field, Select } from "../../components/Field";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/backup";

type Props = {
  /** Domain các site đang chạy, đã sắp xếp. */
  domains: string[];
  /** Domain đang dựng bản xem trước backup (để đổi chữ nút). */
  pendingDomain: string | null;
  anyPending: boolean;
  onCreate: (domain: string) => void;
};

export function CreateNow({ domains, pendingDomain, anyPending, onCreate }: Props) {
  const t = useStrings(strings);
  const [picked, setPicked] = useState("");
  // Site đã chọn có thể biến mất (xoá, thùng rác) sau khi danh sách tải lại: quay về site đầu.
  const domain = domains.includes(picked) ? picked : (domains[0] ?? "");

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (domain && !anyPending) onCreate(domain);
  }

  return (
    <Card title={t.createHeading} titleId="backup-create">
      {domains.length === 0 ? (
        <p className="m-0 text-muted">
          {t.createNoSitesLead}
          <Link to="/sites/new">{t.createNoSitesLink}</Link>
          {t.createNoSitesTail}
        </p>
      ) : (
        <form onSubmit={onSubmit} className="flex max-w-[560px] flex-col gap-[16px]">
          <Field label={t.createLabel} htmlFor="o-backup-site" hint={t.createHint} hintId="h-backup-site">
            <Select id="o-backup-site" value={domain} onChange={(e) => setPicked(e.target.value)} aria-describedby="h-backup-site">
              {domains.map((d) => (
                <option key={d} value={d}>
                  {d}
                </option>
              ))}
            </Select>
          </Field>
          <div>
            <Button variant="primary" type="submit" disabled={anyPending}>
              {pendingDomain === domain ? t.previewing : t.createButton}
            </Button>
          </div>
        </form>
      )}
    </Card>
  );
}
