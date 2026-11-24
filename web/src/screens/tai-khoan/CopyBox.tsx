// Hai cách hiện một chuỗi bí mật để người ta chép (M20): CopyBox là khối chữ mono
// kèm nút "Chép" (token, lệnh curl); CopySecret là chính chuỗi, bấm vào là chép
// (chuỗi bí mật của app xác thực: hiện chia nhóm 4 ký tự cho dễ gõ tay, nhưng
// chép đi đúng chuỗi liền, không có dấu cách).
//
// Chuỗi bí mật chỉ nằm trong props, từ state của màn đang hiện nó; chép vào bộ
// nhớ tạm là việc người dùng chủ động bấm.

import { Button } from "../../components/Button";
import { Code } from "../../components/MonoText";
import { useCopy } from "../../components/useCopy";
import { useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";

function CopyState({ state }: { state: "idle" | "copied" | "failed" }) {
  const k = useStrings(khung);
  return (
    <span aria-live="polite" className={`text-[12px] ${state === "idle" ? "sr-only" : "text-muted"}`}>
      {state === "copied" ? k.copied : state === "failed" ? k.copyFailed : ""}
    </span>
  );
}

/** Khối chữ mono (xuống dòng ở đâu cũng được) và nút chép đúng chữ đó. */
export function CopyBox({ text, buttonLabel, label }: { text: string; buttonLabel: string; label: string }) {
  const { state, copy } = useCopy();
  return (
    <div className="flex flex-col gap-[8px]">
      <Code block>{text}</Code>
      <div className="flex flex-wrap items-center gap-[10px]">
        <Button size="sm" icon={state === "copied" ? "check" : "copy"} onClick={() => copy(text)} aria-label={label}>
          {buttonLabel}
        </Button>
        <CopyState state={state} />
      </div>
    </div>
  );
}

/** Chuỗi hiện thành `shown`, bấm vào là chép `raw`. Là <button> thật. */
export function CopySecret({ raw, shown, label }: { raw: string; shown: string; label: string }) {
  const { state, copy } = useCopy();
  return (
    <div className="flex flex-wrap items-center gap-[10px]">
      <button type="button" title={label} aria-label={`${label}: ${shown}`} onClick={() => copy(raw)} className="rounded-ctl bg-pre px-[10px] py-[8px] text-left hover:underline">
        <Code>{shown}</Code>
      </button>
      <CopyState state={state} />
    </div>
  );
}
