// Chọn bản đã xuất để nhập vào cơ sở dữ liệu của một site (mục 6.9 của
// KE-HOACH.md): danh sách lấy từ GET /api/v1/backups?domain=D, chỉ giữ bản
// kind "sql". Nhập chỉ nhận file TPAN đã ghi (kiểm sha256 ở máy chủ), nên
// không có ô tải file lên. Bấm "Xem trước nhập" chỉ dựng bản xem trước (việc
// của màn gọi), chưa đổi gì.

import { useQuery } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { errorMessage, get } from "../../api/client";
import type { Backup, BackupsOverview } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Field, Select } from "../../components/Field";
import { MonoText } from "../../components/MonoText";
import { useLang, useStrings } from "../../i18n";
import { strings as backupStrings } from "../../i18n/backup";
import { strings } from "../../i18n/co-so-du-lieu";
import { strings as khung } from "../../i18n/khung";
import { dbBackupsKey, dbBackupsPath, formatSize } from "../backup/model";
import { formatWhen } from "../thung-rac/model";
import { siteBackupsKey, siteBackupsPath } from "./model";

type Props = {
  /** DB của site: tên miền. DB rời (M33): tên DB, cũng là giá trị trường domain của các bản sao lưu của nó. */
  domain: string;
  /** DB rời: đọc GET /backups?db=<tên> và đổi lời cho đúng (bản sao lưu, không phải bản xuất của site). */
  standalone?: boolean;
  /** Đang dựng bản xem trước nhập: đổi chữ nút, khoá nút. */
  pending: boolean;
  /** Có việc khác đang dựng bản xem trước: khoá nút. */
  anyPending: boolean;
  onPreview: (backup: Backup) => void;
  onCancel: () => void;
};

export function ImportPicker({ domain, standalone = false, pending, anyPending, onPreview, onCancel }: Props) {
  const t = useStrings(strings);
  const b = useStrings(backupStrings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({
    queryKey: standalone ? dbBackupsKey(domain) : siteBackupsKey(domain),
    queryFn: () => get<BackupsOverview>(standalone ? dbBackupsPath(domain) : siteBackupsPath(domain)),
  });
  const [picked, setPicked] = useState("");

  // Lọc lại theo domain phòng máy chủ trả thừa: nhập bản của site khác vào DB này là sai.
  const exports = (q.data?.items ?? []).filter((x) => x.kind === "sql" && x.domain === domain);
  // Bản đã chọn có thể biến mất sau khi danh sách tải lại: quay về bản mới nhất.
  const chosen = exports.find((x) => x.id === picked) ?? exports[0];

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (chosen && !anyPending) onPreview(chosen);
  }

  return (
    <Card title={standalone ? t.importDbHeading(domain) : t.importHeading(domain)} titleId="db-import">
      <p className="mt-0 mb-[12px] max-w-[72ch] text-[13px] text-muted">{standalone ? t.importDbHint : t.importHint}</p>
      {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div>
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <div className="flex flex-wrap gap-[8px]">
            <Button onClick={() => void q.refetch()}>{k.retry}</Button>
            <Button onClick={onCancel}>{k.cancel}</Button>
          </div>
        </div>
      )}
      {q.isSuccess && !chosen && (
        <div>
          <p className="mt-0 mb-[12px]">{standalone ? t.importDbNone(domain) : t.importNone(domain)}</p>
          <Button onClick={onCancel}>{k.cancel}</Button>
        </div>
      )}
      {q.isSuccess && chosen && (
        <form onSubmit={onSubmit} className="flex max-w-[560px] flex-col gap-[12px]">
          <Field
            label={standalone ? t.importDbLabel : t.importLabel}
            htmlFor="o-db-import"
            hint={
              <>
                {t.importFile} <MonoText size={12} muted>{chosen.path}</MonoText>
              </>
            }
          >
            <Select id="o-db-import" value={chosen.id} onChange={(e) => setPicked(e.target.value)}>
              {exports.map((x) => (
                <option key={x.id} value={x.id}>
                  {`${formatWhen(x.created, lang)} · ${formatSize(x.size, lang)} · ${b.reasons[x.reason] ?? x.reason}`}
                </option>
              ))}
            </Select>
          </Field>
          <div className="flex flex-wrap gap-[8px]">
            <Button variant="primary" type="submit" disabled={anyPending}>
              {pending ? t.previewing : t.importPreview}
            </Button>
            <Button onClick={onCancel}>{k.cancel}</Button>
          </div>
        </form>
      )}
    </Card>
  );
}
