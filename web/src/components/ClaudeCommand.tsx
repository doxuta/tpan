// Nút "Chép lệnh cho Claude Code" (mục 8.3 của KE-HOACH.md): chép vào bộ nhớ
// tạm lệnh `claude '<câu>'`, dán vào terminal máy cá nhân là Claude Code làm
// việc qua MCP. Lệnh hiện ngay bên dưới để đọc trước khi chép, theo nguyên tắc
// 3 của mục 8.1: người dùng thấy được thứ sắp chạy, không chép một lệnh mù.
//
//   <ClaudeCommand prompt="Dùng công cụ tpan chẩn đoán ... rồi đề xuất Plan sửa." />
//
// prompt đã theo ngôn ngữ giao diện: nơi gọi tự lấy từ i18n/agent.ts.

import { Button } from "./Button";
import { Disclosure } from "./Disclosure";
import { EvidenceBlock } from "./EvidenceBlock";
import { useCopy } from "./useCopy";
import { useStrings } from "../i18n";
import { strings } from "../i18n/agent";
import { strings as khung } from "../i18n/khung";

/**
 * Bọc chuỗi trong nháy đơn cho shell: trong nháy đơn mọi ký tự đều là chữ
 * thường (không $, không `, không \), trừ chính nháy đơn. Muốn có nháy đơn thì
 * đóng chuỗi, viết '\'' rồi mở lại. Nhờ vậy câu có tên phép kiểm hay domain
 * chứa ký tự lạ cũng không thành lệnh khác khi dán vào terminal.
 */
export function shellQuote(s: string): string {
  return `'${s.replaceAll("'", `'\\''`)}'`;
}

export function claudeCommand(prompt: string): string {
  return `claude ${shellQuote(prompt)}`;
}

export function ClaudeCommand({ prompt }: { prompt: string }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { state, copy } = useCopy();
  const cmd = claudeCommand(prompt);
  // Đóng sẵn: lệnh cho Claude Code là đường phụ, không chiếm chỗ của thẻ chứa nó.
  return (
    <div className="mt-[12px]">
      <Disclosure flush summary={<span className="text-[14px] font-medium">{t.claudeCopy}</span>}>
        <div className="flex flex-wrap items-center gap-x-[12px] gap-y-[6px]">
          <Button icon="copy" onClick={() => copy(cmd)}>
            {t.claudeCopy}
          </Button>
          <span aria-live="polite" className={`text-[12px] ${state === "idle" ? "sr-only" : "text-muted"}`}>
            {state === "copied" ? k.copied : state === "failed" ? k.copyFailed : ""}
          </span>
        </div>
        <div className="mt-[8px]">
          <EvidenceBlock text={cmd} label={t.claudeLabel} />
        </div>
        <p className="mt-[6px] mb-0 text-[12px] text-muted">{t.claudeHint}</p>
      </Disclosure>
    </div>
  );
}
