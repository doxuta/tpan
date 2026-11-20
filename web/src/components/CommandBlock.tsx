// Khối lệnh để NGƯỜI TỰ GÕ (cách sửa tay, manual_fix của phép kiểm). Khác
// EvidenceBlock: ở đó là bằng chứng tpan đã chạy, ở đây là việc người vận hành
// phải làm. Vì vậy khối luôn nói rõ chạy ở máy nào, quyền gì, thư mục nào;
// mỗi lệnh có nút chép riêng, và chữ được chép không có "$ " ở đầu.
//
//   <CommandBlock commands={check.manual_fix ?? []} />

import { useSession } from "../api/queries";
import { useStrings } from "../i18n";
import { strings } from "../i18n/khung";
import { useCopy } from "./useCopy";

/**
 * Thêm "sudo " vào đầu lệnh. Người vận hành vào máy bằng SSH với user thường,
 * nên lệnh sửa cấu hình hệ thống thiếu sudo sẽ báo "permission denied". Dữ liệu
 * manual_fix từ API giữ nguyên, không có sudo; việc thêm làm ở đây, một chỗ,
 * để chữ hiện ra và chữ được chép luôn giống nhau. Không thêm khi lệnh đã có
 * sudo, hoặc khi dòng là chú thích (bắt đầu bằng "#").
 */
export function withSudo(cmd: string): string {
  const c = cmd.trim();
  if (c === "" || c.startsWith("#") || c.startsWith("sudo ")) return c;
  return `sudo ${c}`;
}

const iconProps = {
  "aria-hidden": true,
  width: 16,
  height: 16,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 2,
  strokeLinecap: "round",
  strokeLinejoin: "round",
} as const;

function CopyIcon() {
  return (
    <svg {...iconProps}>
      <rect x="9" y="9" width="11" height="11" />
      <path d="M15 9V4H4v11h5" />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg {...iconProps}>
      <path d="M5 12l5 5 9-10" />
    </svg>
  );
}

/** Vùng báo cho trình đọc màn hình; lỗi chép thì hiện chữ cho mọi người thấy. */
function CopyNote({ state }: { state: "idle" | "copied" | "failed" }) {
  const t = useStrings(strings);
  return (
    <span aria-live="polite" className={state === "failed" ? "block basis-full text-[12px] text-muted" : "sr-only"}>
      {state === "copied" ? t.copied : state === "failed" ? t.copyFailed : ""}
    </span>
  );
}

/** Mỗi hàng một useCopy riêng nên trạng thái ✓ không lan sang hàng khác. */
function CommandRow({ command }: { command: string }) {
  const t = useStrings(strings);
  const { state, copy } = useCopy();
  return (
    <li className="flex flex-wrap items-center gap-x-[8px]">
      <code className="min-w-0 flex-1 py-[3px] font-mono text-[12px] leading-[1.55] whitespace-pre-wrap [overflow-wrap:anywhere] text-ink">
        {/* "$ " chỉ để nhìn: select-none nên bôi đen tay cũng không dính. */}
        <span aria-hidden="true" className="text-muted select-none">
          ${" "}
        </span>
        {command}
      </code>
      <button
        type="button"
        aria-label={`${t.copyCommand} ${command}`}
        title={t.copyCommand}
        onClick={() => copy(command)}
        className="flex h-[44px] w-[44px] flex-none items-center justify-center bg-transparent text-ink hover:bg-surface sm:h-[28px] sm:w-[28px]"
      >
        {state === "copied" ? <CheckIcon /> : <CopyIcon />}
      </button>
      <CopyNote state={state} />
    </li>
  );
}

/**
 * addSudo={false}: lệnh đã tự đặt sudo đúng chỗ (ví dụ `echo ... | sudo tee`),
 * thêm sudo vào đầu dòng nữa thì thừa. where: thay dòng "chạy ở đâu" mặc định
 * (máy chủ, quyền root), dùng cho lệnh chạy trên máy cá nhân.
 */
export function CommandBlock({ commands, addSudo = true, where }: { commands: string[]; addSudo?: boolean; where?: string }) {
  const t = useStrings(strings);
  const session = useSession();
  const { state, copy } = useCopy();
  const lines = commands.map((c) => (addSudo ? withSudo(c) : c.trim())).filter((c) => c !== "");
  if (lines.length === 0) return null;

  const demo = session.data?.demo === true;
  const host = session.data?.hostname || t.commandHostFallback;

  return (
    <div className="bg-pre p-[12px]">
      <div className="flex flex-wrap items-start justify-between gap-x-[12px] gap-y-[6px]">
        <p className="m-0 min-w-0 flex-1 basis-[240px] text-[13px] text-muted">{demo ? t.commandWhereDemo : (where ?? t.commandWhere(host))}</p>
        {lines.length > 1 && (
          <button
            type="button"
            onClick={() => copy(lines.join("\n"))}
            className="inline-flex h-[44px] flex-none items-center gap-[6px] border border-ink bg-surface px-[10px] text-[12px] font-medium text-ink hover:bg-paper sm:h-[28px]"
          >
            {state === "copied" ? <CheckIcon /> : <CopyIcon />}
            {state === "copied" ? t.copied : t.copyAll}
          </button>
        )}
      </div>
      <ul className="m-0 mt-[8px] flex list-none flex-col gap-[2px] p-0">
        {lines.map((line, i) => (
          <CommandRow key={i} command={line} />
        ))}
      </ul>
      {lines.length > 1 && <CopyNote state={state} />}
    </div>
  );
}
