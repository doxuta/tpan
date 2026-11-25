// Khối "Chủ của site" trên trang một site (chỉ admin): chọn user khác rồi đổi
// chủ. Như mọi việc đổi máy: nút → bản xem trước → chạy. Máy chủ kiểm chủ mới
// có thật, đang hoạt động, còn chỗ trong gói; site trong lồng mà gói chủ mới
// khác trần thì chạy thành một job, không thì chỉ ghi bảng (không có job).

import { useState } from "react";
import type { Site } from "../../api/types";
import { useUsers, usersKey } from "../../api/m13-users";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Field, Select } from "../../components/Field";
import { usePreviewedOp } from "../../components/PreviewedOp";
import { useStrings } from "../../i18n";
import { strings as chung } from "../../i18n/m13-chung";
import { strings } from "../../i18n/site";

type Props = { site: Site };

export function OwnerBlock({ site }: Props) {
  const t = useStrings(strings);
  const m = useStrings(chung);
  const d = site.domain;
  const users = useUsers();
  const op = usePreviewedOp([["site", d], usersKey]);
  const others = (users.data ?? []).filter((u) => u.user.status === "active" && u.user.name !== site.owner).map((u) => u.user.name);
  const [picked, setPicked] = useState("");
  const to = others.includes(picked) ? picked : "";

  return (
    <>
      <Card title={t.ownerHeading} titleId="chu-site">
        <p className="m-0 max-w-[72ch]">{t.ownerNow(site.owner)}</p>
        <p className="mt-[6px] mb-0 max-w-[72ch] text-muted">{t.ownerHint}</p>
        <div className="flex flex-wrap items-end gap-[12px] pt-[12px]">
          <Field label={t.ownerNewLabel} htmlFor="o-site-owner">
            <Select id="o-site-owner" name="owner" value={to} onChange={(e) => setPicked(e.target.value)} disabled={Boolean(site.suspended)}>
              <option value="">{others.length === 0 ? t.ownerNoOther : t.ownerPick}</option>
              {others.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </Select>
          </Field>
          <Button
            disabled={op.pending || to === ""}
            aria-label={t.ownerChangeFor(d)}
            onClick={() =>
              void op.ask({
                id: "owner",
                method: "POST",
                path: `/api/v1/sites/${encodeURIComponent(d)}/owner`,
                body: { owner: to },
                warning: t.ownerWarning(d, site.owner, to),
                errorHeading: t.ownerErrorHeading(d),
              })
            }
          >
            {op.busy("owner") ? m.previewing : t.ownerChange}
          </Button>
        </div>
        {site.suspended && <p className="mt-[8px] mb-0 max-w-[72ch] text-muted">{t.ownerSuspended}</p>}
      </Card>
      {op.panel}
    </>
  );
}
