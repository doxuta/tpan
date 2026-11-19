// Ngăn AI (mục 8.4 của KE-HOACH.md, "Ngăn AI" ở mục 6 của docs/GIAO-DIEN.md,
// hình chuẩn docs/mau-giao-dien/ngan-ai-sang.html). Từ trên xuống: đầu ngăn
// (tên, tình trạng bộ não, nút đóng); cách nối nếu bộ não chưa nối; "Ngữ cảnh
// gửi kèm" với các nhãn bỏ được; cuộc trò chuyện; ô hỏi tiếp.
//
// Màn đủ rộng (useWide): ngăn nằm cạnh nội dung, trong hàng flex của Layout.
// Màn hẹp: ngăn là lớp phủ (Radix Dialog, nên khoá focus và Esc đóng được),
// toàn màn dưới 640 px. Trạng thái cuộc trò chuyện nằm ở AiProvider nên hai
// kiểu trình bày dùng chung một thứ.

import * as RadixDialog from "@radix-ui/react-dialog";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import { errorMessage, get, post } from "../api/client";
import type { AIChip, AIChipKey, AskPreview, BrainStatus, PendingPlan } from "../api/types";
import { buttonClass } from "../components/Button";
import { ClaudeCommand } from "../components/ClaudeCommand";
import { CommandBlock } from "../components/CommandBlock";
import { EvidenceBlock } from "../components/EvidenceBlock";
import { StatusMark } from "../components/StatusMark";
import { useCopy } from "../components/useCopy";
import { tx, useLang, useStrings } from "../i18n";
import { strings } from "../i18n/ai";
import { strings as khung } from "../i18n/khung";
import { useAi, type AiMode, type TurnView, type ToolItem } from "./AiProvider";
import { AnswerText } from "./AnswerText";
import { useWide } from "./platform";
import { extractLinks } from "./views";

const monoLabel = "m-0 mb-[4px] font-mono text-[11px] tracking-[0.08em] text-muted";
const tag = "border border-ink px-[5px] font-mono text-[11px] tracking-[0.06em] text-ink";
const iconProps = { "aria-hidden": true, viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeLinecap: "round", strokeLinejoin: "round", className: "flex-none" } as const;

function CloseIcon({ size, width = 2 }: { size: number; width?: number }) {
  return (
    <svg {...iconProps} width={size} height={size} strokeWidth={width}>
      <path d="M6 6l12 12M18 6L6 18" />
    </svg>
  );
}

function PlusIcon() {
  return (
    <svg {...iconProps} width={12} height={12} strokeWidth={2.5}>
      <path d="M12 5v14M5 12h14" />
    </svg>
  );
}

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const id = window.setTimeout(() => setV(value), ms);
    return () => window.clearTimeout(id);
  }, [value, ms]);
  return v;
}

/** Tên công cụ bỏ tiền tố của MCP server (mcp__tpan__site_health → site_health). */
function toolName(full: string): string {
  return full.replace(/^mcp__tpan__/, "");
}

// ---- vỏ ngăn: cạnh nội dung hay lớp phủ ----

export function AiDrawer() {
  const ai = useAi();
  const t = useStrings(strings);
  const wide = useWide();
  if (!ai.open) return null;

  if (wide) {
    return (
      <aside id="ngan-ai" aria-label={t.drawerLabel} className="min-w-0 flex-[1_1_400px] pt-[24px]" onKeyDown={(e) => e.key === "Escape" && ai.setOpen(false)}>
        <ReturnFocus />
        <Panel overlay={false} className="sticky top-[16px] max-h-[calc(100vh-32px)]" />
      </aside>
    );
  }
  return (
    <RadixDialog.Root open onOpenChange={ai.setOpen}>
      <RadixDialog.Portal>
        {/* Làm mờ trang bằng chính màu nền, không bóng đổ (mục 9). */}
        <RadixDialog.Overlay className="fixed inset-0 z-40 bg-paper/80" />
        <RadixDialog.Content id="ngan-ai" aria-describedby={undefined} className="fixed inset-y-0 right-0 z-50 w-full max-w-full text-ink sm:w-[440px]">
          <Panel overlay className="h-full" />
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}

/**
 * Ngăn nằm cạnh nội dung tắt đi cùng lúc con trỏ đang ở trong nó, nên trình
 * duyệt đưa focus về <body> và người dùng bàn phím mất dấu vị trí (mục 10, ý 2
 * của GIAO-DIEN.md). Trả focus về nút "Hỏi AI" trên thanh đầu trang. Lớp phủ
 * (Radix Dialog) tự trả focus nên không cần.
 */
function ReturnFocus() {
  useEffect(
    () => () => {
      const active = document.activeElement;
      if (!active || active === document.body) document.getElementById("nut-hoi-ai")?.focus();
    },
    [],
  );
  return null;
}

function Panel({ overlay, className }: { overlay: boolean; className: string }) {
  const t = useStrings(strings);
  const ai = useAi();
  const { lang } = useLang();

  const brain = useQuery({ queryKey: ["ai", "brain"], queryFn: () => get<BrainStatus>("/api/v1/ai/brain"), refetchInterval: 10_000, retry: false });
  const b = brain.data;
  const ready = b?.connected === true;

  // Bản xem trước dựng lại khi ngữ cảnh đổi (chuyển mục, bỏ nhãn) và khi người
  // gõ dừng tay; không gọi ở mỗi phím. AI tắt thì máy chủ từ chối, khỏi gọi.
  const typed = useDebounced(ai.draft, 500);
  const previewInput = useMemo(() => ({ ...ai.input, question: typed }), [ai.input, typed]);
  const preview = useQuery({
    queryKey: ["ai", "preview", previewInput],
    queryFn: () => post<AskPreview>("/api/v1/ai/preview", previewInput),
    enabled: b !== undefined && b.mode !== "off",
    placeholderData: keepPreviousData,
    retry: false,
  });

  const heading = <h2 className="m-0 text-[16px] font-semibold">{t.title}</h2>;

  return (
    <section className={`flex min-h-0 flex-col border border-card-border bg-surface ${className}`}>
      <div className="flex flex-none flex-wrap items-center gap-x-[12px] gap-y-[8px] border-b border-hair py-[12px] pr-[12px] pl-[18px]">
        {overlay ? <RadixDialog.Title asChild>{heading}</RadixDialog.Title> : heading}
        <span className="ml-auto flex items-center gap-[8px] font-mono text-[12px] text-muted">
          {brain.isPending ? null : (
            <>
              <StatusMark status={ready ? "pass" : "unknown"} label={b ? (ready ? t.brainOn : t.brainOff) : t.brainUnknown} />
              <span>{b ? (ready ? t.brainOnText(b.host, b.model) : t.brainOffText) : t.brainUnknownText}</span>
            </>
          )}
        </span>
        {ai.turns.length > 0 && (
          <button type="button" title={t.newSessionHint} disabled={ai.running} onClick={ai.reset} className={buttonClass("secondary")}>
            {t.newSession}
          </button>
        )}
        <button
          type="button"
          aria-label={t.close}
          title={t.close}
          onClick={() => ai.setOpen(false)}
          className="flex h-[44px] w-[44px] flex-none items-center justify-center border border-ink bg-surface text-ink hover:bg-pre sm:h-[40px] sm:w-[40px]"
        >
          <CloseIcon size={16} />
        </button>
      </div>

      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        {brain.isError && <p className="m-0 border-b border-hair px-[18px] py-[12px] text-[13px] text-muted">{t.brainStatusFailed} {errorMessage(brain.error, lang)}</p>}
        {b && !ready && <Connect brain={b} draft={ai.draft} />}
        {b && ready && b.warnings && b.warnings.length > 0 && (
          <div className="border-b border-hair px-[18px] py-[12px]">
            <p className={monoLabel}>{t.brainWarnings}</p>
            {b.warnings.map((w, i) => (
              <p key={i} className="m-0 text-[13px] text-muted">
                {tx(w, lang)}
              </p>
            ))}
          </div>
        )}
        {b?.mode !== "off" && <Context preview={preview.data} loading={preview.isFetching} error={preview.isError ? preview.error : null} />}
        <Transcript />
      </div>

      <Composer ready={ready} brainKnown={b !== undefined} previewReady={typed === ai.draft && preview.data !== undefined && !preview.isFetching && !preview.isError} />
    </section>
  );
}

// ---- cách nối bộ não ----

/** Một lệnh chạy trên máy cá nhân của người vận hành: không sudo, không nói "máy chủ" như CommandBlock. */
function LocalCommand({ command }: { command: string }) {
  const k = useStrings(khung);
  const t = useStrings(strings);
  const { state, copy } = useCopy();
  return (
    <div className="bg-pre p-[12px]">
      <p className="m-0 text-[13px] text-muted">{t.connectLocalWhere}</p>
      <div className="mt-[8px] flex flex-wrap items-center gap-x-[8px]">
        <code className="min-w-0 flex-1 py-[3px] font-mono text-[12px] leading-[1.55] whitespace-pre-wrap [overflow-wrap:anywhere] text-ink">
          <span aria-hidden="true" className="text-muted select-none">
            ${" "}
          </span>
          {command}
        </code>
        <button
          type="button"
          aria-label={`${k.copyCommand} ${command}`}
          title={k.copyCommand}
          onClick={() => copy(command)}
          className="flex h-[44px] w-[44px] flex-none items-center justify-center bg-transparent text-ink hover:bg-surface sm:h-[28px] sm:w-[28px]"
        >
          {state === "copied" ? (
            <svg {...iconProps} width={16} height={16} strokeWidth={2}>
              <path d="M5 12l5 5 9-10" />
            </svg>
          ) : (
            <svg {...iconProps} width={16} height={16} strokeWidth={2}>
              <rect x="9" y="9" width="11" height="11" />
              <path d="M15 9V4H4v11h5" />
            </svg>
          )}
        </button>
        <span aria-live="polite" className={state === "failed" ? "block basis-full text-[12px] text-muted" : "sr-only"}>
          {state === "copied" ? k.copied : state === "failed" ? k.copyFailed : ""}
        </span>
      </div>
    </div>
  );
}

function Connect({ brain, draft }: { brain: BrainStatus; draft: string }) {
  const t = useStrings(strings);
  const off = brain.mode === "off";
  return (
    <div id="ai-noi-bo-nao" className="border-b border-hair px-[18px] py-[14px]">
      <p className="m-0 text-[14px] font-medium">{off ? t.connectTitleOff : t.connectTitleBridge}</p>
      <p className="m-0 mt-[4px] text-[13px] text-muted">{off ? t.connectBodyOff : t.connectBodyBridge}</p>
      <div className="mt-[10px] flex flex-col gap-[10px]">
        {off && (
          <div>
            <p className="m-0 mb-[4px] text-[13px]">{t.connectServerStep}</p>
            <CommandBlock commands={["tpan mcp config --brain bridge"]} />
          </div>
        )}
        <div>
          {off && <p className="m-0 mb-[4px] text-[13px]">{t.connectLocalStep}</p>}
          <LocalCommand command={brain.connect_command} />
        </div>
      </div>
      <p className="m-0 mt-[12px] text-[13px] text-muted">{t.connectOrClaude}</p>
      <ClaudeCommand prompt={t.claudePrompt(draft.trim())} />
    </div>
  );
}

// ---- ngữ cảnh gửi kèm ----

function Context({ preview, loading, error }: { preview: AskPreview | undefined; loading: boolean; error: unknown }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const ai = useAi();
  const [shown, setShown] = useState<AIChipKey | null>(null);
  const [full, setFull] = useState(false);
  const chip = preview?.chips.find((c) => c.key === shown);

  return (
    <div className="border-b border-hair px-[18px] py-[14px]">
      <p className={`${monoLabel} mb-[8px]`}>{t.chipsHeading}</p>
      {preview && (
        <ul className="m-0 flex list-none flex-wrap gap-[6px] p-0">
          {preview.chips.map((c) => (
            <ChipItem key={c.key} chip={c} open={shown === c.key} onShow={() => setShown(shown === c.key ? null : c.key)} onToggle={() => ai.toggleOmit(c.key)} />
          ))}
        </ul>
      )}
      {!preview && loading && <p className="m-0 text-[13px] text-muted">{t.previewLoading}</p>}
      {!preview && error !== null && (
        <p role="alert" className="m-0 text-[13px] text-fail-text">
          {t.previewFailed} {errorMessage(error, lang)}
        </p>
      )}
      {chip && (
        <div id="ai-noi-dung-nhan" className="mt-[10px]">
          <EvidenceBlock text={chip.content} label={tx(chip.label, lang).toUpperCase()} />
        </div>
      )}
      {ai.trail.length > 1 && <p className="m-0 mt-[10px] text-[12px] leading-[1.5] text-muted">{t.trail(ai.trail.map((v) => v.label))}</p>}
      {preview && (
        <div className="mt-[10px]">
          <button type="button" aria-expanded={full} aria-controls="ai-toan-van" onClick={() => setFull(!full)} className={buttonClass("secondary")}>
            {full ? t.hideFull : t.viewFull}
          </button>
          {full && (
            <div id="ai-toan-van" className="mt-[8px]">
              <EvidenceBlock text={preview.prompt} label={t.fullLabel} />
            </div>
          )}
        </div>
      )}
    </div>
  );
}

/** Một nhãn ngữ cảnh: bấm tên để xem đúng nội dung sẽ gửi, bấm nút bên cạnh để bỏ hoặc gửi lại. */
function ChipItem({ chip, open, onShow, onToggle }: { chip: AIChip & { omitted: boolean }; open: boolean; onShow: () => void; onToggle: () => void }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const label = tx(chip.label, lang);
  return (
    <li className={`inline-flex items-center border ${chip.omitted ? "border-hair text-muted" : "border-ink text-ink"}`}>
      <button
        type="button"
        aria-label={t.chipShow(label)}
        aria-expanded={open}
        aria-controls="ai-noi-dung-nhan"
        onClick={onShow}
        className={`h-[44px] bg-transparent pr-[2px] pl-[10px] text-left font-mono text-[12px] whitespace-nowrap sm:h-[32px] ${chip.omitted ? "line-through" : ""}`}
      >
        {label}
        {chip.omitted && <span className="sr-only"> ({t.chipOmitted})</span>}
      </button>
      <button
        type="button"
        aria-label={chip.omitted ? t.chipRestore(label) : t.chipRemove(label)}
        title={chip.omitted ? t.chipRestore(label) : t.chipRemove(label)}
        onClick={onToggle}
        className="flex h-[44px] w-[44px] flex-none items-center justify-center bg-transparent p-0 text-muted hover:text-ink sm:h-[32px] sm:w-[32px]"
      >
        {chip.omitted ? <PlusIcon /> : <CloseIcon size={12} width={2.5} />}
      </button>
    </li>
  );
}

// ---- cuộc trò chuyện ----

function Transcript() {
  const t = useStrings(strings);
  const ai = useAi();
  const end = useRef<HTMLDivElement>(null);

  // Lượt đang chạy thì cuộn theo chữ mới; chỉ cuộn khung của ngăn, không cuộn cả trang.
  useEffect(() => {
    if (ai.running) end.current?.scrollIntoView({ block: "end" });
  }, [ai.turns, ai.running]);

  return (
    <div className="flex flex-col gap-[18px] px-[18px] pt-[16px] pb-[18px]">
      {ai.turns.length === 0 && <p className="m-0 text-[13px] text-muted">{t.empty}</p>}
      {ai.turns.map((turn) => (
        <TurnBlock key={turn.seq} turn={turn} />
      ))}
      <p role="status" className="sr-only">
        {ai.running ? t.working : ai.turns.length > 0 ? t.workingDone : ""}
      </p>
      <div ref={end} />
    </div>
  );
}

function TurnBlock({ turn }: { turn: TurnView }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const ai = useAi();
  const wide = useWide();
  const tools = turn.items.filter((i): i is ToolItem => i.kind === "tool");
  const plans = turn.items.flatMap((i) => (i.kind === "plan" ? [i.planId] : []));
  // Liên kết chỉ lấy khi câu trả lời đã chốt: giữa chừng "tpan:site/a.exa" chưa phải một tên miền.
  // Lượt bị từ chối thì không hiện câu trả lời dở dang (nếu có) và không dẫn đi đâu.
  const refused = turn.status === "refused";
  const links = turn.status === "running" || refused ? [] : extractLinks(turn.answer, lang);

  return (
    <div className="flex flex-col gap-[18px]">
      <div>
        <p className={monoLabel}>{t.you}</p>
        <p className="m-0 text-[14px] leading-[1.5] whitespace-pre-wrap [overflow-wrap:anywhere]">{turn.question}</p>
      </div>

      {tools.length > 0 && (
        <div>
          <p className={monoLabel}>{t.read}</p>
          <ul className="m-0 list-none border-b border-hair p-0">
            {tools.map((it) => (
              <ToolRow key={it.key} item={it} />
            ))}
          </ul>
        </div>
      )}

      {!refused && (turn.answer || turn.status === "running") && (
        <div>
          <p className={monoLabel}>{t.answer}</p>
          {turn.answer ? <AnswerText text={turn.answer} /> : <p className="m-0 text-[13px] text-muted">{t.working}</p>}
        </div>
      )}

      {turn.status === "refused" && (
        <p role="status" className="m-0 border border-ink p-[12px] text-[14px]">
          {t.refused(turn.message)}
        </p>
      )}
      {turn.status === "error" && (
        <p role="alert" className="m-0 text-[13px] text-fail-text">
          {t.failed(turn.message)}
        </p>
      )}

      {plans.map((id) => (
        <PlanBox key={id} id={id} />
      ))}

      {links.length > 0 && (
        <div>
          <p className={`${monoLabel} mb-[8px]`}>{t.goTo}</p>
          <div className="flex flex-wrap gap-[8px]">
            {links.map((l) => (
              // Màn rộng: ngăn đi theo sang mục mới. Lớp phủ che mất mục mới nên đóng lại.
              <Link key={l.path} to={l.path} onClick={() => !wide && ai.setOpen(false)} className={buttonClass("secondary")}>
                {l.label}
              </Link>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function ToolRow({ item }: { item: ToolItem }) {
  const t = useStrings(strings);
  const name = toolName(item.tool);
  const input = item.input === undefined ? "" : (JSON.stringify(item.input, null, 2) ?? "");
  return (
    <li className="border-t border-hair">
      <details>
        <summary className="flex cursor-pointer list-none flex-wrap items-baseline gap-x-[10px] gap-y-[4px] py-[7px] [&::-webkit-details-marker]:hidden">
          <span className={tag}>{name.startsWith("propose_") ? t.toolPlan : t.toolRead}</span>
          <span className="min-w-0 flex-1 basis-[170px] font-mono text-[12px] [overflow-wrap:anywhere]">{name}</span>
          {item.failed && <span className="text-[12px] text-muted">{t.toolFailed}</span>}
        </summary>
        {input && (
          <div className="pb-[8px]">
            <EvidenceBlock text={input} label={t.toolInput} />
          </div>
        )}
      </details>
    </li>
  );
}

/** Plan AI vừa đề xuất: khung viền --ink, nhãn "AI ĐỀ XUẤT", nút chính tới màn duyệt. */
function PlanBox({ id }: { id: string }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const ai = useAi();
  const wide = useWide();
  // Cùng khoá với màn chi tiết Plan (["plans", "detail", id]) nên hai nơi dùng chung một lần tải.
  const plan = useQuery({ queryKey: ["plans", "detail", id], queryFn: () => get<PendingPlan>(`/api/v1/plans/${encodeURIComponent(id)}`), retry: false });
  const title = plan.data ? tx(plan.data.title, lang) : t.planUntitled(id);
  return (
    <div className="border border-ink p-[14px]">
      <div className="flex flex-wrap items-center gap-[8px]">
        <span className={`${tag} py-[1px] pr-[6px] pl-[6px]`}>{t.planTag}</span>
        <span className="text-[14px] font-medium">{title}</span>
      </div>
      <Link to={`/plans/${encodeURIComponent(id)}`} onClick={() => !wide && ai.setOpen(false)} className={buttonClass("primary", "mt-[12px]")}>
        {t.planOpen}
      </Link>
    </div>
  );
}

// ---- ô hỏi ----

/**
 * previewReady: nội dung sẽ gửi đã dựng xong cho đúng câu đang gõ. Chưa xong thì
 * chưa cho gửi, kẻo câu từ thanh lệnh (đã điền sẵn, con trỏ nằm ngay ô hỏi) bị
 * Enter gửi đi trước khi người kịp thấy nội dung (nguyên tắc 3, mục 8.1).
 */
function Composer({ ready, brainKnown, previewReady }: { ready: boolean; brainKnown: boolean; previewReady: boolean }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const ai = useAi();
  const field = useRef<HTMLInputElement>(null);

  // Mở ngăn (hoặc openAI từ chỗ khác) thì đưa con trỏ vào ô hỏi.
  useEffect(() => {
    field.current?.focus();
  }, [ai.focusTick]);

  const label: Record<AiMode, string> = { ask: ai.turns.length > 0 ? t.askMoreLabel : t.askLabel, explain: t.explainLabel, diagnose: t.diagnoseLabel };
  const blocked = !ready || !previewReady || ai.running || ai.sending || ai.draft.trim() === "";

  return (
    <form
      className="flex-none border-t border-hair px-[18px] pt-[14px] pb-[16px]"
      onSubmit={(e) => {
        e.preventDefault();
        void ai.send();
      }}
    >
      <label htmlFor="ai-cau-hoi" className="mb-[8px] block font-mono text-[11px] tracking-[0.08em] text-muted">
        {label[ai.mode]}
      </label>
      <div className="flex flex-wrap gap-[8px]">
        <input
          id="ai-cau-hoi"
          ref={field}
          type="text"
          autoComplete="off"
          value={ai.draft}
          onChange={(e) => ai.setDraft(e.target.value)}
          className="box-border h-[44px] min-w-0 flex-[1_1_200px] border border-ink bg-surface px-[10px] text-[14px] text-ink sm:h-[40px]"
        />
        <button type="submit" disabled={blocked} className={buttonClass("primary")}>
          {ai.sending ? t.sending : t.send}
        </button>
      </div>
      {brainKnown && !ready && <p className="m-0 mt-[8px] text-[12px] text-muted">{t.needBrain}</p>}
      {ai.sendError !== null && (
        <p role="alert" className="m-0 mt-[8px] text-[13px] text-fail-text">
          {errorMessage(ai.sendError, lang)}
        </p>
      )}
      <p className="m-0 mt-[10px] text-[12px] leading-[1.5] text-muted">{t.footer}</p>
    </form>
  );
}

