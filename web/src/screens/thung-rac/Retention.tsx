// Khối "Số ngày giữ": hiện số ngày đang đặt và cho đổi (POST /trash/settings).
// Đây là đổi cấu hình của TPAN chứ không đổi máy, nên không qua bản xem trước
// (như Cấu hình của màn SSL). Số mới chỉ áp cho site xoá sau lúc đổi; câu đó
// nằm ngay dưới ô nhập để người đổi đọc được trước khi bấm Lưu.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { post } from "../../api/client";
import type { TrashOverview, TrashSettings } from "../../api/types";
import { Button } from "../../components/Button";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/thung-rac";
import { ErrorNote } from "../site/ErrorNote";
import { maxDays, parseDays, trashKey, trashSettingsPath } from "./model";

const inputClass = "box-border h-[44px] w-[120px] border border-ink bg-surface px-[10px] font-mono text-[13px] text-ink sm:h-[40px]";

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
    <section aria-labelledby="trash-retention">
      <h2 id="trash-retention" className="m-0 pt-[26px] pb-[14px] text-[16px] font-semibold">
        {t.retentionHeading}
      </h2>
      <form onSubmit={onSubmit} noValidate className="flex max-w-[560px] flex-col gap-[12px]">
        <div>
          <label htmlFor="o-trash-days" className="mb-[4px] block font-medium">
            {t.retentionLabel}
          </label>
          <p id="h-trash-days" className="mt-0 mb-[6px] text-[13px] text-muted">
            {t.retentionHint}
          </p>
          <input
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
            className={inputClass}
          />
        </div>

        {invalid && (
          <p role="alert" className="m-0">
            {t.retentionInvalid}
          </p>
        )}
        {save.isError && <ErrorNote error={save.error} heading={t.saveFailed} />}

        <div className="flex flex-wrap items-center gap-x-[16px] gap-y-[8px]">
          <Button variant="primary" type="submit" disabled={!dirty || save.isPending}>
            {save.isPending ? t.saving : t.save}
          </Button>
          {/* aria-live luôn có mặt trong cây để trình đọc màn hình đọc được chữ vừa hiện. */}
          <span aria-live="polite" className="text-muted">
            {justSaved && !dirty ? t.saved : t.retentionCurrent(days)}
          </span>
        </div>
      </form>
    </section>
  );
}
