// Thẻ "Lịch và số bản giữ": số bản giữ mỗi site (1..30) và giờ backup hằng
// ngày (để trống là tắt). Khác Số ngày giữ của Thùng rác: lưu ở đây ghi một
// file cron trên máy (/etc/cron.d), nên đi đủ ba chặng như mọi thao tác đổi
// máy. Nút chỉ mở bản xem trước; việc gọi API là của màn.

import { useState, type FormEvent } from "react";
import type { BackupSettings } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Field, Input } from "../../components/Field";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/backup";
import { maxKeep, parseKeep } from "./model";

type Props = {
  settings: BackupSettings;
  pending: boolean;
  anyPending: boolean;
  onPreview: (body: BackupSettings) => void;
};

/** Màn dựng lại khối này (key) mỗi khi máy chủ đổi cài đặt, nên ô nhập luôn bắt đầu từ giá trị đang có. */
export function BackupSettingsForm({ settings, pending, anyPending, onPreview }: Props) {
  const t = useStrings(strings);
  const [keepRaw, setKeepRaw] = useState(String(settings.keep));
  const [at, setAt] = useState(settings.at);

  const keep = parseKeep(keepRaw);
  const invalid = keepRaw.trim() !== "" && keep === null;
  const dirty = keep !== null && (keep !== settings.keep || at !== settings.at);

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (keep === null || !dirty || anyPending) return;
    onPreview({ keep, at });
  }

  return (
    <Card title={t.settingsHeading} titleId="backup-settings">
      <form onSubmit={onSubmit} noValidate className="flex max-w-[560px] flex-col gap-[20px]">
        <Field label={t.keepLabel} htmlFor="o-backup-keep" hint={t.keepHint} hintId="h-backup-keep" error={invalid ? t.keepInvalid : undefined}>
          {/* Input luôn rộng hết dòng: ô số chỉ cần 120 px nên bọc trong một khung hẹp. */}
          <div className="w-[120px]">
            <Input
              id="o-backup-keep"
              name="keep"
              type="number"
              inputMode="numeric"
              min={1}
              max={maxKeep}
              step={1}
              value={keepRaw}
              onChange={(e) => setKeepRaw(e.target.value)}
              aria-describedby="h-backup-keep"
              aria-invalid={invalid}
            />
          </div>
        </Field>

        <Field label={t.atLabel} htmlFor="o-backup-at" hint={t.atHint} hintId="h-backup-at">
          <div className="flex flex-wrap items-center gap-[8px]">
            {/* type="time" cho "HH:MM" theo giờ 24; ô trống cho "" nên khớp với at rỗng của API. */}
            <div className="w-[160px]">
              <Input id="o-backup-at" name="at" type="time" value={at} onChange={(e) => setAt(e.target.value)} aria-describedby="h-backup-at" />
            </div>
            {/* Chrome không có nút xoá trong ô giờ; nút này cho cả người dùng bàn phím lẫn chuột tắt lịch. */}
            {at !== "" && <Button onClick={() => setAt("")}>{t.atClear}</Button>}
          </div>
        </Field>

        <div className="flex flex-wrap items-center gap-x-[16px] gap-y-[8px]">
          <Button variant="primary" type="submit" disabled={!dirty || anyPending}>
            {pending ? t.previewing : t.settingsPreview}
          </Button>
          <span className="text-muted">{t.current(settings.keep, settings.at)}</span>
        </div>
      </form>
    </Card>
  );
}
