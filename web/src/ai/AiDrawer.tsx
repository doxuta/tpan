// Ngăn AI (mục 8.4 của KE-HOACH.md; "Ngăn AI" ở mục 6 và 7 của docs/GIAO-DIEN.md,
// bản 2). Từ trên xuống: đầu ngăn (tên, tình trạng bộ não, nút đóng); cách nối
// nếu bộ não chưa nối; "Ngữ cảnh gửi kèm" với các nhãn bỏ được; cuộc trò
// chuyện; ô hỏi tiếp.
//
// Ngăn đóng thì KHÔNG còn gì ở góc màn hình: nút mở là "Hỏi AI" trên thanh đầu
// trang (Layout, #nut-hoi-ai).
//
// Màn đủ rộng (useWide): khung chat nổi cỡ cố định 400 × 640 px ở góc dưới bên
// phải, đè lên nội dung chứ không chiếm chỗ trong bố cục. Màn hẹp: ngăn là lớp
// phủ (Radix Dialog, nên khoá focus và Esc đóng được), toàn màn dưới 640 px.
// Trạng thái cuộc trò chuyện nằm ở AiProvider nên hai kiểu trình bày dùng chung
// một thứ.

import * as RadixDialog from "@radix-ui/react-dialog";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import { errorMessage, get, post } from "../api/client";
import type { AIChip, AIChipKey, AskPreview, BrainStatus, PendingPlan } from "../api/types";
import { Button, IconButton, buttonClass } from "../components/Button";
import { Card } from "../components/Card";
import { ClaudeCommand } from "../components/ClaudeCommand";
import { CommandBlock } from "../components/CommandBlock";
import { Disclosure } from "../components/Disclosure";
import { EmptyState } from "../components/EmptyState";
import { EvidenceBlock } from "../components/EvidenceBlock";
import { Field, Input } from "../components/Field";
import { returnFocus } from "../components/focusReturn";
import { Icon } from "../components/Icon";
import { Code } from "../components/MonoText";
import { Notice } from "../components/Notice";
import { StatusMark } from "../components/StatusMark";
import { Tag } from "../components/Tag";
import { tx, useLang, useStrings } from "../i18n";
import { strings } from "../i18n/ai";
import { useAi, type AiMode, type TurnView, type ToolItem } from "./AiProvider";
import { AnswerText } from "./AnswerText";
import { useWide } from "./platform";
import { extractLinks } from "./views";

/** Nhãn nhỏ trong ngăn: 12 px đậm vừa, viết thường (mục 4 của GIAO-DIEN.md). */
const smallLabel = "m-0 text-[12px] font-medium text-muted";

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

// ---- vỏ ngăn: khung nổi hay lớp phủ ----

export function AiDrawer() {
  const ai = useAi();
  const t = useStrings(strings);
  const wide = useWide();
  if (!ai.open) return null;

  if (wide) {
    // Khung chat nổi: cỡ cố định ở góc dưới bên phải, không chiếm cột của trang và
    // không phình theo nội dung; chữ dài thì cuộn bên trong khung.
    return (
      <div
        id="ngan-ai"
        role="dialog"
        aria-label={t.drawerLabel}
        className="fixed right-[16px] bottom-[16px] z-40 flex h-[min(640px,calc(100dvh-32px))] w-[400px] max-w-[calc(100vw-32px)]"
        onKeyDown={(e) => e.key === "Escape" && ai.setOpen(false)}
      >
        <ReturnFocus />
        <Panel overlay={false} className="h-full w-full rounded-card border border-hair shadow-pop" />
      </div>
    );
  }
  return (
    <RadixDialog.Root open onOpenChange={ai.setOpen}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay className="fixed inset-0 z-40 bg-scrim" />
        <RadixDialog.Content id="ngan-ai" aria-describedby={undefined} onCloseAutoFocus={returnFocus} className="fixed inset-y-0 right-0 z-50 w-full max-w-full text-ink sm:w-[440px]">
          <Panel overlay className="h-full border-l border-hair shadow-pop" />
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}

/**
 * Ngăn nổi tắt đi cùng lúc con trỏ đang ở trong nó, nên trình duyệt đưa focus về
 * <body> và người dùng bàn phím mất dấu vị trí (mục 10, ý 2 của GIAO-DIEN.md).
 * Trả focus về nút "Hỏi AI" trên thanh đầu trang. Lớp phủ (Radix Dialog) tự trả
 * focus nên không cần.
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
    <section className={`flex min-h-0 flex-col overflow-hidden bg-surface ${className}`}>
      <div className="flex flex-none flex-wrap items-center gap-x-[12px] gap-y-[6px] border-b border-hair py-[10px] pr-[8px] pl-[16px]">
        {overlay ? <RadixDialog.Title asChild>{heading}</RadixDialog.Title> : heading}
        <span className="flex-1" />
        {ai.turns.length > 0 && (
          <Button size="sm" title={t.newSessionHint} disabled={ai.running} onClick={ai.reset}>
            {t.newSession}
          </Button>
        )}
        <IconButton icon="close" label={t.close} onClick={() => ai.setOpen(false)} />
        {/* Tình trạng bộ não: một dòng riêng dưới tiêu đề, cắt "…" khi tên máy dài; tooltip có câu đầy đủ. */}
        {!brain.isPending && (
          <span className="flex min-w-0 basis-full items-center gap-[8px] pr-[8px] text-[13px] text-muted">
            <StatusMark status={ready ? "pass" : "unknown"} label={b ? (ready ? t.brainOn : t.brainOff) : t.brainUnknown} />
            {(() => {
              const s = b ? (ready ? t.brainOnText(b.host, b.model) : t.brainOffText) : t.brainUnknownText;
              return (
                <span title={s} className="min-w-0 truncate">
                  {s}
                </span>
              );
            })()}
          </span>
        )}
      </div>

      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        {brain.isError && (
          <p className="m-0 border-b border-hair px-[16px] py-[12px] text-[13px] text-muted">
            {t.brainStatusFailed} {errorMessage(brain.error, lang)}
          </p>
        )}
        {b && !ready && <Connect brain={b} draft={ai.draft} />}
        {b && ready && b.warnings && b.warnings.length > 0 && (
          <div className="border-b border-hair px-[16px] py-[12px]">
            <p className={`${smallLabel} mb-[4px]`}>{t.brainWarnings}</p>
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

function Connect({ brain, draft }: { brain: BrainStatus; draft: string }) {
  const t = useStrings(strings);
  const off = brain.mode === "off";
  return (
    <div id="ai-noi-bo-nao" className="border-b border-hair px-[16px] py-[14px]">
      <p className="m-0 text-[14px] font-medium">{off ? t.connectTitleOff : t.connectTitleBridge}</p>
      <p className="m-0 mt-[4px] text-[13px] text-muted">{off ? t.connectBodyOff : t.connectBodyBridge}</p>
      <div className="mt-[10px] flex flex-col gap-[10px]">
        {off && (
          <div>
            <p className="m-0 mb-[4px] text-[13px]">{t.connectServerStep}</p>
            <CommandBlock commands={["tpan mcp config --brain bridge", "systemctl restart tpan"]} />
          </div>
        )}
        <div>
          {off && <p className="m-0 mb-[4px] text-[13px]">{t.connectLocalStep}</p>}
          {/* Lệnh chạy trên máy cá nhân của người vận hành: không sudo, và dòng "chạy ở đâu" nói máy cá nhân chứ không phải máy chủ. Tên máy (hostname) thường không phân giải được từ máy cá nhân: dùng địa chỉ đang mở panel. */}
          <CommandBlock commands={[brain.connect_command.replace(/@[^@\s]*$/, "@" + window.location.hostname)]} addSudo={false} where={t.connectLocalWhere} />
        </div>
        <p className="m-0 text-[13px]">
          {t.connectFullLead}
          <Link to="/settings">{t.connectFullLink}</Link>
          {t.connectFullTail}
        </p>
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
  // Mở sẵn khi chưa hỏi gì (người hỏi lần đầu thấy AI sẽ nhận gì); hỏi xong câu
  // đầu thì gập lại thành một dòng để chỗ cho cuộc trò chuyện. Bấm dòng đó để mở lại.
  const hasTurns = ai.turns.length > 0;
  const [open, setOpen] = useState(!hasTurns);
  useEffect(() => {
    if (hasTurns) setOpen(false);
  }, [hasTurns]);
  const sent = preview ? preview.chips.filter((c) => !c.omitted).length : null;

  return (
    <div className="border-b border-hair px-[16px]">
      <Disclosure
        flush
        open={open}
        onOpenChange={setOpen}
        summary={
          <span className="text-[12px] font-medium text-muted">
            {t.chipsHeading}
            {sent !== null && <span className="font-normal"> · {t.chipsCount(sent)}</span>}
          </span>
        }
      >
        <div className="flex flex-col gap-[10px]">
          {preview && (
            <ul className="m-0 flex list-none flex-wrap gap-[6px] p-0">
              {preview.chips.map((c) => (
                <ChipItem key={c.key} chip={c} open={shown === c.key} onShow={() => setShown(shown === c.key ? null : c.key)} onToggle={() => ai.toggleOmit(c.key)} />
              ))}
            </ul>
          )}
          {!preview && loading && <p className="m-0 text-[13px] text-muted">{t.previewLoading}</p>}
          {!preview && error !== null && (
            <div role="alert">
              <Notice kind="fail">
                {t.previewFailed} {errorMessage(error, lang)}
              </Notice>
            </div>
          )}
          {chip && (
            <div id="ai-noi-dung-nhan">
              <EvidenceBlock text={chip.content} label={tx(chip.label, lang)} />
            </div>
          )}
          {ai.trail.length > 1 && <p className="m-0 text-[12px] leading-[1.5] text-muted">{t.trail(ai.trail.map((v) => v.label))}</p>}
          {preview && (
            <div>
              <Button size="sm" aria-expanded={full} aria-controls="ai-toan-van" onClick={() => setFull(!full)}>
                {full ? t.hideFull : t.viewFull}
              </Button>
              {full && (
                <div id="ai-toan-van" className="mt-[8px]">
                  <EvidenceBlock text={preview.prompt} label={t.fullLabel} />
                </div>
              )}
            </div>
          )}
        </div>
      </Disclosure>
    </div>
  );
}

/**
 * Một nhãn ngữ cảnh: bấm tên để xem đúng nội dung sẽ gửi, bấm nút × bên cạnh để bỏ hoặc gửi lại.
 * Chưa có thành phần dùng chung cho nó (Tag không bấm được): dựng bằng đúng kiểu dáng của Tag.
 */
function ChipItem({ chip, open, onShow, onToggle }: { chip: AIChip & { omitted: boolean }; open: boolean; onShow: () => void; onToggle: () => void }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const label = tx(chip.label, lang);
  return (
    <li className={`inline-flex items-center rounded-full border ${chip.omitted ? "border-hair bg-surface text-muted" : "border-transparent bg-paper text-ink-soft"}`}>
      <button
        type="button"
        aria-label={t.chipShow(label)}
        aria-expanded={open}
        aria-controls="ai-noi-dung-nhan"
        onClick={onShow}
        className={`h-[44px] rounded-l-full bg-transparent pr-[2px] pl-[12px] text-left text-[13px] whitespace-nowrap sm:h-[32px] ${chip.omitted ? "line-through" : ""}`}
      >
        {label}
        {chip.omitted && <span className="sr-only"> ({t.chipOmitted})</span>}
      </button>
      <button
        type="button"
        aria-label={chip.omitted ? t.chipRestore(label) : t.chipRemove(label)}
        title={chip.omitted ? t.chipRestore(label) : t.chipRemove(label)}
        onClick={onToggle}
        className="flex size-[44px] flex-none items-center justify-center rounded-r-full bg-transparent p-0 text-muted hover:text-ink sm:size-[32px]"
      >
        <Icon name={chip.omitted ? "plus" : "close"} size={14} />
      </button>
    </li>
  );
}

// ---- cuộc trò chuyện ----

function Transcript() {
  const t = useStrings(strings);
  const ai = useAi();
  const end = useRef<HTMLDivElement>(null);

  // Lượt đang chạy thì cuộn theo chữ mới; lúc lượt vừa xong cuộn thêm một lần,
  // vì nút "Đi tới" và Plan đề xuất chỉ hiện sau khi câu trả lời đã chốt.
  const wasRunning = useRef(false);
  useEffect(() => {
    if (ai.running || wasRunning.current) end.current?.scrollIntoView({ block: "end" });
    wasRunning.current = ai.running;
  }, [ai.turns, ai.running]);

  return (
    <div className="flex flex-col gap-[18px] px-[16px] pt-[16px] pb-[18px]">
      {ai.turns.length === 0 && <EmptyState>{t.empty}</EmptyState>}
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
    <div className="flex flex-col gap-[10px]">
      {/* Câu của bạn: khung nền --pre lệch phải, như tin nhắn đã gửi; nhãn ẩn chỉ cho trình đọc màn hình. */}
      <div className="flex justify-end">
        <div className="max-w-[85%] rounded-ctl bg-pre px-[12px] py-[8px]">
          <span className="sr-only">{t.you}: </span>
          <p className="m-0 text-[14px] leading-[1.5] whitespace-pre-wrap [overflow-wrap:anywhere]">{turn.question}</p>
        </div>
      </div>

      {/* Phần của AI: lệch trái, nhãn "AI" một lần, rồi việc đã đọc (gập) và câu trả lời. */}
      <div className="flex flex-col gap-[8px]">
        <p className={smallLabel}>{t.answer}</p>

        {tools.length > 0 && (
          <Disclosure
            flush
            summary={
              <span className="text-[13px] text-muted">
                {t.readCount(tools.length)}
                {tools.some((it) => it.failed) && <span> · {t.toolFailed}</span>}
              </span>
            }
          >
            <ul className="m-0 list-none p-0">
              {tools.map((it) => (
                <ToolRow key={it.key} item={it} />
              ))}
            </ul>
          </Disclosure>
        )}

        {!refused && (turn.answer || turn.status === "running") && (
          <div>{turn.answer ? <AnswerText text={turn.answer} /> : <p className="m-0 text-[13px] text-muted">{t.working}</p>}</div>
        )}
      </div>

      {turn.status === "refused" && (
        <Notice kind="warn" role="status">
          {t.refused(turn.message)}
        </Notice>
      )}
      {turn.status === "error" && (
        <Notice kind="fail" role="alert">
          {t.failed(turn.message)}
        </Notice>
      )}

      {plans.map((id) => (
        <PlanBox key={id} id={id} />
      ))}

      {links.length > 0 && (
        <div>
          <p className={`${smallLabel} mb-[8px]`}>{t.goTo}</p>
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

/** Một lần AI gọi công cụ: nhãn "Đọc", tên công cụ, và "dữ liệu đã gửi" mở ra đúng bản đã che. */
function ToolRow({ item }: { item: ToolItem }) {
  const t = useStrings(strings);
  const name = toolName(item.tool);
  const input = item.input === undefined ? "" : (JSON.stringify(item.input, null, 2) ?? "");
  const head = (
    <span className="flex min-w-0 flex-wrap items-center gap-x-[8px] gap-y-[4px]">
      <Tag>{name.startsWith("propose_") ? t.toolPlan : t.toolRead}</Tag>
      <Code>{name}</Code>
      {item.failed && <span className="text-[12px] text-muted">{t.toolFailed}</span>}
      {input && <span className="text-[13px] text-accent">{t.toolSent}</span>}
    </span>
  );
  return (
    <li className="border-t border-hair first:border-t-0">
      {input ? (
        <Disclosure flush summary={head}>
          <EvidenceBlock text={input} label={t.toolInput} />
        </Disclosure>
      ) : (
        <div className="py-[10px]">{head}</div>
      )}
    </li>
  );
}

/** Plan AI vừa đề xuất: một thẻ có nhãn "AI đề xuất" và nút chính tới màn duyệt. */
function PlanBox({ id }: { id: string }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const ai = useAi();
  const wide = useWide();
  // Cùng khoá với màn chi tiết Plan (["plans", "detail", id]) nên hai nơi dùng chung một lần tải.
  const plan = useQuery({ queryKey: ["plans", "detail", id], queryFn: () => get<PendingPlan>(`/api/v1/plans/${encodeURIComponent(id)}`), retry: false });
  const title = plan.data ? tx(plan.data.title, lang) : t.planUntitled(id);
  return (
    <Card>
      <div className="flex flex-wrap items-center gap-[8px]">
        <Tag>{t.planTag}</Tag>
        <span className="font-medium">{title}</span>
      </div>
      <Link to={`/plans/${encodeURIComponent(id)}`} onClick={() => !wide && ai.setOpen(false)} className={buttonClass("primary", "mt-[12px]")}>
        {t.planOpen}
      </Link>
    </Card>
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
      className="flex-none border-t border-hair px-[16px] pt-[14px] pb-[16px]"
      onSubmit={(e) => {
        e.preventDefault();
        void ai.send();
      }}
    >
      <Field label={label[ai.mode]} htmlFor="ai-cau-hoi">
        <div className="flex flex-wrap gap-[8px]">
          <Input id="ai-cau-hoi" ref={field} type="text" autoComplete="off" value={ai.draft} onChange={(e) => ai.setDraft(e.target.value)} className="min-w-0 flex-[1_1_200px]" />
          <Button type="submit" variant="primary" disabled={blocked}>
            {ai.sending ? t.sending : t.send}
          </Button>
        </div>
      </Field>
      {brainKnown && !ready && <p className="m-0 mt-[8px] text-[12px] text-muted">{t.needBrain}</p>}
      {ai.sendError !== null && (
        <div role="alert" className="mt-[8px]">
          <Notice kind="fail">{errorMessage(ai.sendError, lang)}</Notice>
        </div>
      )}
      <p className="m-0 mt-[10px] text-[12px] leading-[1.5] text-muted">{t.footer}</p>
    </form>
  );
}
