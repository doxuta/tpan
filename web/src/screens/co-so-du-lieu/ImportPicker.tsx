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
import { MonoText } from "../../components/MonoText";
import { useLang, useStrings } from "../../i18n";
import { strings as backupStrings } from "../../i18n/backup";
import { strings } from "../../i18n/co-so-du-lieu";
import { strings as khung } from "../../i18n/khung";
import { formatSize } from "../backup/model";
import { formatWhen } from "../thung-rac/model";
import { siteBackupsKey, siteBackupsPath } from "./model";

type Props = {
  domain: string;
  /** Đang dựng bản xem trước nhập: đổi chữ nút, khoá nút. */
  pending: boolean;
  /** Có việc khác đang dựng bản xem trước: khoá nút. */
  anyPending: boolean;
  onPreview: (backup: Backup) => void;
  onCancel: () => void;
};

const selectClass = "box-border h-[44px] w-full border border-ink bg-surface px-[10px] font-mono text-[13px] text-ink sm:h-[40px]";

export function ImportPicker({ domain, pending, anyPending, onPreview, onCancel }: Props) {
  const t = useStrings(strings);
  const b = useStrings(backupStrings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({ queryKey: siteBackupsKey(domain), queryFn: () => get<BackupsOverview>(siteBackupsPath(domain)) });
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
    <section aria-labelledby="db-import" className="pt-[26px]">
      <h2 id="db-import" className="m-0 pb-[6px] text-[16px] font-semibold">
        {t.importHeading(domain)}
      </h2>
      <p className="mt-0 mb-[12px] max-w-[720px] text-[13px] text-muted">{t.importHint}</p>
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
          <p className="mt-0 mb-[12px]">{t.importNone(domain)}</p>
          <Button onClick={onCancel}>{k.cancel}</Button>
        </div>
      )}
      {q.isSuccess && chosen && (
        <form onSubmit={onSubmit} className="flex max-w-[560px] flex-col gap-[12px]">
          <div>
            <label htmlFor="o-db-import" className="mb-[4px] block font-medium">
              {t.importLabel}
            </label>
            <select id="o-db-import" value={chosen.id} onChange={(e) => setPicked(e.target.value)} className={selectClass}>
              {exports.map((x) => (
                <option key={x.id} value={x.id}>
                  {`${formatWhen(x.created, lang)} · ${formatSize(x.size, lang)} · ${b.reasons[x.reason] ?? x.reason}`}
                </option>
              ))}
            </select>
            <p className="mt-[6px] mb-0 text-[13px] text-muted">
              {t.importFile} <MonoText size={12} muted>{chosen.path}</MonoText>
            </p>
          </div>
          <div className="flex flex-wrap gap-[8px]">
            <Button variant="primary" type="submit" disabled={anyPending}>
              {pending ? t.previewing : t.importPreview}
            </Button>
            <Button onClick={onCancel}>{k.cancel}</Button>
          </div>
        </form>
      )}
    </section>
  );
}
