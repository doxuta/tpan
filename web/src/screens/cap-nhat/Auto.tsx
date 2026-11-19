// Khối (b): bản vá bảo mật tự cài hằng ngày. Dòng trạng thái là CheckResult của
// phép kiểm updates.auto, vẽ bằng đúng CheckRow của màn Bảo mật (dấu trạng thái,
// bấm mở bằng chứng, nguyên nhân, sửa tay). Chưa đạt thì dòng có nút "Bật bản
// vá tự động".

import { useState } from "react";
import type { UpdateStatus } from "../../api/types";
import { MonoText } from "../../components/MonoText";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/cap-nhat";
import { CheckRow } from "../bao-mat/CheckRow";
import { autoAction, type Flow } from "./model";

export function Auto({ status, flow }: { status: UpdateStatus; flow: Flow }) {
  const t = useStrings(strings);
  const check = status.auto;
  // Dòng chưa đạt mở sẵn để thấy ngay bằng chứng, như màn Bảo mật.
  const [open, setOpen] = useState(check.status === "fail" || check.status === "warn");
  const origins = status.auto_origins ?? [];

  return (
    <section aria-labelledby="cn-auto" className="pt-[26px]">
      <h2 id="cn-auto" className="m-0 pb-[8px] text-[16px] font-semibold">
        {t.autoHeading}
      </h2>
      <ul aria-label={t.autoLabel} className="m-0 list-none border-t border-hair p-0">
        <CheckRow
          check={check}
          open={open}
          onToggle={() => setOpen((o) => !o)}
          action={{
            kind: autoAction.id,
            label: t.autoButton,
            hint: t.autoHint,
            pending: flow.pending === autoAction.id,
            disabled: flow.busy,
            onClick: () => flow.start(autoAction),
            error: flow.errorFor(autoAction),
          }}
        />
      </ul>

      <h3 className="m-0 mt-[14px] mb-[6px] font-mono text-[11px] font-normal tracking-[0.08em] text-muted uppercase">{t.autoOrigins}</h3>
      {origins.length === 0 ? (
        <p className="m-0 text-muted">{t.autoOriginsNone}</p>
      ) : (
        <ul className="m-0 flex list-none flex-wrap gap-x-[16px] gap-y-[6px] p-0">
          {origins.map((o) => (
            <li key={o}>
              <MonoText>{o}</MonoText>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
