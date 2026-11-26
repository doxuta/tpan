// M36 (v0.2.17): khối "Đổi mật khẩu" của DB rời và DB của site PHP trống, hiện
// dưới bảng như khối Sửa. Hai lựa chọn: TPAN tạo ngẫu nhiên (như trước, hiện
// một lần ở trang job) hoặc tự đặt. "Xem trước" gọi ?dry_run=1 rồi PlanReview
// như mọi việc khác của màn. Mật khẩu tự đặt đi trong thân request, không bao
// giờ trong đường dẫn; máy chủ không trả lại nó.
//
// DB của site WordPress không mở khối này: TPAN ghi mật khẩu vào wp-config.php,
// máy chủ từ chối mật khẩu tự đặt (db_password_wordpress).

import { useState } from "react";
import type { DatabaseRow } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { MonoText } from "../../components/MonoText";
import type { usePreviewedOp } from "../../components/PreviewedOp";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/co-so-du-lieu";
import { strings as chung } from "../../i18n/m13-chung";
import { rowOp } from "./model";
import { PasswordField, passwordReady } from "./PasswordField";

type Props = {
  row: DatabaseRow;
  op: ReturnType<typeof usePreviewedOp>;
  onClose: () => void;
};

export function PasswordPanel({ row, op, onClose }: Props) {
  const t = useStrings(strings);
  const m = useStrings(chung);
  const [mode, setMode] = useState<"random" | "chosen">("random");
  const [pw, setPw] = useState("");
  const [tried, setTried] = useState(false);
  const name = `doi-mk-${row.db}`;

  function preview() {
    if (mode === "chosen") {
      setTried(true);
      if (!passwordReady(pw, row.db)) {
        document.getElementById("doi-mk-o")?.focus();
        return;
      }
      void op.ask(rowOp(t, "password", row, { password: pw }));
      return;
    }
    void op.ask(rowOp(t, "password", row));
  }

  return (
    <Card title={t.pwHeading(row.db)} titleId="doi-mat-khau" actions={<Button onClick={onClose}>{t.editClose}</Button>}>
      <div className="flex flex-col gap-[16px]">
        <MonoText>{row.db}</MonoText>
        <p className="m-0 max-w-[72ch] text-[13px] text-muted">{t.pwIntro}</p>
        <fieldset className="m-0 min-w-0 border-0 p-0">
          <legend className="mb-[6px] p-0 text-[14px] font-medium">{t.pwChoose}</legend>
          {(["random", "chosen"] as const).map((v) => (
            <label key={v} className="flex max-w-[72ch] items-start gap-[10px] border-t border-hair py-[10px]">
              <input type="radio" name={name} value={v} checked={mode === v} onChange={() => setMode(v)} className="mt-[1px] size-[20px] flex-none accent-accent" />
              <span className="min-w-0">
                {v === "random" ? t.pwRandom : t.pwChosen}
                <span className="block text-muted">{v === "random" ? t.pwRandomHint : t.pwChosenHint}</span>
              </span>
            </label>
          ))}
        </fieldset>
        {mode === "chosen" && <PasswordField id="doi-mk-o" label={t.pwLabel} value={pw} onChange={setPw} dbName={row.db} showError={tried} />}
        <div>
          <Button variant="primary" disabled={op.pending} onClick={preview}>
            {op.busy(`password:${row.db}`) ? m.previewing : t.pwPreview}
          </Button>
        </div>
      </div>
    </Card>
  );
}
