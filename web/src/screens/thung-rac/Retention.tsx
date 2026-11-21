// Thẻ "Số ngày giữ": hiện số ngày đang đặt và cho đổi (POST /trash/settings).
// Đây là đổi cấu hình của TPAN chứ không đổi máy, nên không qua bản xem trước
// (như Cấu hình của màn SSL). Số mới chỉ áp cho site xoá sau lúc đổi; câu đó
// nằm ngay dưới ô nhập để người đổi đọc được trước khi bấm Lưu.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { post } from "../../api/client";
import type { TrashOverview, TrashSettings } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Field, Input } from "../../components/Field";
import { Notice } from "../../components/Notice";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/thung-rac";
import { ErrorNote } from "../site/ErrorNote";
import { maxDays, parseDays, trashKey, trashSettingsPath } from "./model";

export function Retention({ days }: { days: number }) {
  const t = useStrings(strings);
  const qc = useQueryClient();
  const [raw, setRaw] = useState(String(days));
  const [justSaved, setJustSaved] = useState(false);

  const save = useMutation({
    mutationFn: (body: TrashSettings) => post<TrashOverview>(trashSettingsPath, body),
    onSuccess: (saved) => {
      // Máy chủ trả cả TrashOverview: ghi thẳng vào bộ nhớ đệm để danh sách và số ngày khớp ngay.
      qc.setQueryData<TrashOverview>(trashKey, saved);
      setRaw(String(saved.days));
      setJustSaved(true);
    },
  });

  const parsed = parseDays(raw);
  const invalid = raw.trim() !== "" && parsed === null;
  const dirty = parsed !== null && parsed !== days;

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (parsed === null || !dirty) return;
    save.mutate({ days: parsed });
  }

  return (
    <Card title={t.retentionHeading} titleId="trash-retention">
      <form onSubmit={onSubmit} noValidate className="flex max-w-[560px] flex-col gap-[16px]">
        <Field label={t.retentionLabel} htmlFor="o-trash-days" hint={t.retentionHint} hintId="h-trash-days" error={invalid ? t.retentionInvalid : undefined}>
          {/* Input luôn rộng hết dòng: ô số chỉ cần 120 px nên bọc trong một khung hẹp. */}
          <div className="w-[120px]">
            <Input
              id="o-trash-days"
              name="days"
              type="number"
              inputMode="numeric"
              min={0}
              max={maxDays}
              step={1}
              value={raw}
              onChange={(e) => {
                setRaw(e.target.value);
                setJustSaved(false);
                save.reset();
              }}
              aria-describedby="h-trash-days"
              aria-invalid={invalid}
            />
          </div>
        </Field>

        {save.isError && <ErrorNote error={save.error} heading={t.saveFailed} />}

        <div className="flex flex-wrap items-center gap-x-[16px] gap-y-[8px]">
          <Button variant="primary" type="submit" disabled={!dirty || save.isPending}>
            {save.isPending ? t.saving : t.save}
          </Button>
          {/* aria-live luôn có mặt trong cây để trình đọc màn hình đọc được chữ vừa hiện. */}
          <div aria-live="polite">{justSaved && !dirty ? <Notice kind="pass">{t.saved}</Notice> : <span className="text-muted">{t.retentionCurrent(days)}</span>}</div>
        </div>
      </form>
    </Card>
  );
}
