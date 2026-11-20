// Trạng thái của ngăn AI. Nằm trong Layout (không trong màn nào) nên cuộc trò
// chuyện, câu đang gõ và lượt đang chạy sống qua mọi lần chuyển mục, và qua
// cả lúc đóng ngăn: luồng SSE của một lượt vẫn chạy khi ngăn đóng.
//
// Các màn khác chỉ cần một hàm: openAI. Nút "Giải thích" hay "Chẩn đoán" gọi
//
//   const { openAI } = useAi();
//   openAI({ pointer: { kind: "check", ref: check.id }, mode: "explain", question: "Vì sao phép kiểm này lỗi?" });
//
// Ngăn mở ra với câu hỏi và nhãn "đang chỉ vào" đã điền. Hàm KHÔNG gửi: người
// xem nội dung sẽ gửi (nguyên tắc 3 của mục 8.1) rồi tự bấm Gửi.
//
// Mỗi lần chuyển mục, provider báo máy chủ (POST /api/v1/ai/views) để công cụ
// current_view biết người dùng đang ở đâu, và giữ đường vừa đi (tối đa 10 mục)
// để gửi kèm câu hỏi kế tiếp.

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useLocation } from "react-router";
import { post, subscribeAITurn } from "../api/client";
import { useWho } from "../api/m13-users";
import type { AIChipKey, AIEvent, AIPointer, AITurnStatus, AIView, AskInput, AskStarted } from "../api/types";
import { useLang } from "../i18n";
import { viewLabel } from "./views";

/** Lối vào ngăn: chỉ đổi nhãn ô hỏi, không đổi cách gửi. */
export type AiMode = "ask" | "explain" | "diagnose";

/** Một lần AI gọi công cụ. input là đối số đã che. */
export type ToolItem = { kind: "tool"; key: string; tool: string; input: unknown; failed: boolean };
/** Một Plan AI vừa đề xuất (kết quả của công cụ lập Plan). */
export type PlanItem = { kind: "plan"; key: string; planId: string };
export type TurnItem = ToolItem | PlanItem;

/** Một lượt hỏi trong cuộc trò chuyện đang hiện. */
export type TurnView = {
  seq: number;
  /** Câu người dùng đã gõ (không phải bản đã che). */
  question: string;
  status: AITurnStatus;
  /** Toàn bộ câu trả lời tới lúc này; mỗi sự kiện thay chữ cũ. */
  answer: string;
  items: TurnItem[];
  /** Lời từ chối hoặc lỗi máy chủ nói; rỗng thì giao diện dùng câu chung. */
  message: string;
};

/** Tối đa 10 mục trong đường vừa đi (mục 8.4). */
const trailMax = 10;

type AiState = {
  open: boolean;
  setOpen: (open: boolean) => void;
  toggle: () => void;
  openAI: (o?: { question?: string; pointer?: AIPointer; mode?: AiMode }) => void;
  /** Tăng mỗi lần openAI, để ngăn đang mở cũng kéo con trỏ về ô hỏi. */
  focusTick: number;
  mode: AiMode;

  sessionId: string;
  turns: TurnView[];
  /** Có lượt đang chạy: chưa gửi thêm được (máy chủ cũng từ chối ai_turn_running). */
  running: boolean;
  sending: boolean;
  sendError: unknown;
  send: () => Promise<void>;
  /** Bỏ cuộc trò chuyện hiện tại, câu kế tiếp mở phiên mới. */
  reset: () => void;

  view: AIView;
  trail: AIView[];
  pointer: AIPointer | undefined;
  draft: string;
  setDraft: (q: string) => void;
  omit: AIChipKey[];
  toggleOmit: (key: AIChipKey) => void;
  /** Thân của POST /api/v1/ai/preview và /ask với câu đang gõ. */
  input: AskInput;
};

const Ctx = createContext<AiState | null>(null);

export function useAi(): AiState {
  const v = useContext(Ctx);
  if (!v) throw new Error("useAi chỉ dùng được trong AiProvider (Layout bọc sẵn)");
  return v;
}

/** Cộng một sự kiện của máy chủ vào lượt. Hàm thuần: cùng lượt cũ và sự kiện thì ra cùng lượt mới. */
function applyEvent(t: TurnView, ev: AIEvent): TurnView {
  switch (ev.kind) {
    case "text":
      return { ...t, answer: ev.text ?? t.answer };
    case "done":
      return { ...t, status: "done", answer: ev.text ?? t.answer };
    case "error":
      return { ...t, status: "error", message: ev.text ?? "" };
    case "refused":
      return { ...t, status: "refused", answer: "", message: ev.text ?? "" };
    case "tool_call":
      return { ...t, items: [...t.items, { kind: "tool", key: `${ev.time}|${ev.tool ?? ""}`, tool: ev.tool ?? "", input: ev.input, failed: false }] };
    case "tool_result": {
      let items = t.items;
      if (ev.is_error) {
        // Đánh dấu lần gọi gần nhất của đúng công cụ đó là lỗi.
        let i = items.length - 1;
        while (i >= 0 && !(items[i]!.kind === "tool" && (items[i] as ToolItem).tool === ev.tool)) i--;
        if (i >= 0) items = items.map((it, j) => (j === i && it.kind === "tool" ? { ...it, failed: true } : it));
      }
      if (ev.plan_id && !items.some((it) => it.kind === "plan" && it.planId === ev.plan_id)) {
        items = [...items, { kind: "plan", key: `plan|${ev.plan_id}`, planId: ev.plan_id }];
      }
      return items === t.items ? t : { ...t, items };
    }
    default:
      return t; // init: chỉ báo phiên bản và model, không có gì để hiện
  }
}

export function AiProvider({ children }: { children: ReactNode }) {
  const { lang } = useLang();
  const location = useLocation();
  // Mọi route /ai/... trả 403 cho user thường (mục 6.12): không gọi chúng.
  const { isAdmin } = useWho();

  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<AiMode>("ask");
  const [focusTick, setFocusTick] = useState(0);
  const [sessionId, setSessionId] = useState("");
  const [turns, setTurns] = useState<TurnView[]>([]);
  const [sending, setSending] = useState(false);
  const [sendError, setSendError] = useState<unknown>(null);
  const [draft, setDraft] = useState("");
  const [pointer, setPointer] = useState<AIPointer | undefined>(undefined);
  const [omit, setOmit] = useState<AIChipKey[]>([]);
  const [paths, setPaths] = useState<string[]>(() => [location.pathname]);

  const stop = useRef<(() => void) | null>(null);
  const lastPath = useRef<string | null>(null);

  // Mỗi lần chuyển mục: báo máy chủ và nhớ vào đường vừa đi. Lỗi thì bỏ im
  // lặng: ngữ cảnh làm việc chỉ để AI hiểu rõ hơn, không đáng làm người dùng vướng.
  useEffect(() => {
    const path = location.pathname;
    if (lastPath.current === path) return; // đổi ngôn ngữ cũng chạy lại effect này
    lastPath.current = path;
    setPaths((p) => (p.at(-1) === path ? p : [...p, path].slice(-trailMax)));
    if (!isAdmin) return;
    post("/api/v1/ai/views", { path, label: viewLabel(path, lang) } satisfies AIView).catch(() => {});
  }, [location.pathname, lang, isAdmin]);

  // Rời Layout (đăng xuất) thì thôi nghe lượt đang chạy.
  useEffect(() => () => stop.current?.(), []);

  // Nhãn tính lại theo ngôn ngữ đang dùng, nên đổi VI/EN thì đường vừa đi đổi theo.
  const trail = useMemo<AIView[]>(() => paths.map((path) => ({ path, label: viewLabel(path, lang) })), [paths, lang]);
  const view = trail[trail.length - 1]!;

  const input = useMemo<AskInput>(
    () => ({ session_id: sessionId || undefined, question: draft, lang, view, pointer, trail, omit }),
    [sessionId, draft, lang, view, pointer, trail, omit],
  );

  const openAI = useCallback<AiState["openAI"]>((o = {}) => {
    if (o.question !== undefined) setDraft(o.question);
    if (o.pointer) setPointer(o.pointer);
    setMode(o.mode ?? "ask");
    setOpen(true);
    setFocusTick((n) => n + 1);
  }, []);

  const toggle = useCallback(() => setOpen((o) => !o), []);

  const toggleOmit = useCallback((key: AIChipKey) => {
    setOmit((o) => (o.includes(key) ? o.filter((k) => k !== key) : [...o, key]));
  }, []);

  const reset = useCallback(() => {
    stop.current?.();
    stop.current = null;
    setSessionId("");
    setTurns([]);
    setSendError(null);
  }, []);

  const running = turns.some((t) => t.status === "running");

  const send = useCallback(async () => {
    const question = draft.trim();
    if (!question || running || sending) return;
    setSending(true);
    setSendError(null);
    try {
      const started = await post<AskStarted>("/api/v1/ai/ask", { ...input, question } satisfies AskInput);
      const seq = started.seq;
      setSessionId(started.session_id);
      setTurns((ts) => [...ts, { seq, question, status: "running", answer: "", items: [], message: "" }]);
      // Câu kế tiếp không mang lại nhãn "đang chỉ vào" và các khối đã bỏ: chúng thuộc về câu vừa gửi.
      setDraft("");
      setPointer(undefined);
      setOmit([]);
      setMode("ask");

      // Lúc nối lại, EventSource có thể phát lại sự kiện đã nhận; text và done
      // mang toàn bộ câu trả lời nên phát lại vô hại, các sự kiện còn lại bỏ trùng theo (loại, giờ, công cụ).
      const seen = new Set<string>();
      stop.current?.();
      stop.current = subscribeAITurn(started.session_id, seq, {
        onEvent: (ev) => {
          if (ev.kind !== "text" && ev.kind !== "done") {
            const key = `${ev.kind}|${ev.time}|${ev.tool ?? ""}`;
            if (seen.has(key)) return;
            seen.add(key);
          }
          setTurns((ts) => ts.map((t) => (t.seq === seq ? applyEvent(t, ev) : t)));
        },
        onClose: (reason) => {
          // Luồng chết mà chưa có sự kiện cuối: đừng để lượt quay mãi.
          if (reason === "error") setTurns((ts) => ts.map((t) => (t.seq === seq && t.status === "running" ? { ...t, status: "error" } : t)));
        },
      });
    } catch (err) {
      setSendError(err);
    } finally {
      setSending(false);
    }
  }, [draft, input, running, sending]);

  const value = useMemo<AiState>(
    () => ({
      open,
      setOpen,
      toggle,
      openAI,
      focusTick,
      mode,
      sessionId,
      turns,
      running,
      sending,
      sendError,
      send,
      reset,
      view,
      trail,
      pointer,
      draft,
      setDraft,
      omit,
      toggleOmit,
      input,
    }),
    [open, toggle, openAI, focusTick, mode, sessionId, turns, running, sending, sendError, send, reset, view, trail, pointer, draft, omit, toggleOmit, input],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}
