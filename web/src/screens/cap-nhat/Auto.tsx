// Khối (b): bản vá bảo mật tự cài hằng ngày. Dòng trạng thái là CheckResult của
// phép kiểm updates.auto, vẽ bằng đúng CheckRow của màn Bảo mật (dấu trạng thái,
// bấm mở bằng chứng, nguyên nhân, sửa tay). Chưa đạt thì dòng có nút "Bật bản
// vá tự động". Dòng đóng sẵn, trừ khi phép kiểm lỗi (mặc định của CheckRow).

import type { UpdateStatus } from "../../api/types";
import { Card } from "../../components/Card";
import { MonoText } from "../../components/MonoText";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/cap-nhat";
import { CheckRow } from "../bao-mat/CheckRow";
import { autoAction, type Flow } from "./model";

export function Auto({ status, flow }: { status: UpdateStatus; flow: Flow }) {
  const t = useStrings(strings);
  const check = status.auto;
  const origins = status.auto_origins ?? [];

  return (
    <Card title={t.autoHeading} titleId="cn-auto">
      <ul aria-label={t.autoLabel} className="m-0 list-none p-0">
        <CheckRow
          check={check}
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

      <h3 className="m-0 mt-[16px] mb-[8px] border-t border-hair pt-[16px] text-[14px] font-semibold">{t.autoOrigins}</h3>
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
    </Card>
  );
}
