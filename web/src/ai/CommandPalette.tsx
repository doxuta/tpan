// Thanh lệnh ⌘K (mục 10, màn 11 của KE-HOACH.md). Mở từ mọi màn, dùng hoàn toàn
// bằng bàn phím: gõ để lọc, ↑ ↓ để đổi dòng, Enter để chạy, Esc để đóng.
//
// Hai việc:
//   - gõ tên lệnh: danh sách lọc theo chữ (bỏ dấu, nên "tao site" tìm ra "Tạo site");
//     lệnh là mở từng màn có trong routes.tsx, và mở hoặc đóng ngăn AI;
//   - gõ một câu không khớp lệnh nào rồi Enter: mở ngăn AI với câu đó làm câu hỏi.
//     Ngăn chỉ điền câu và hiện nội dung sẽ gửi; người bấm Gửi (nguyên tắc 3, mục 8.1).
//
// Dòng "Hỏi AI: <câu>" luôn nằm cuối danh sách khi đã gõ chữ, nên câu hỏi nào
// cũng có đường tới ngăn AI, kể cả câu tình cờ chứa tên một lệnh.
//
// Mẫu bàn phím theo kiểu combobox: focus ở ô nhập, dòng đang chọn được báo qua
// aria-activedescendant.

import * as RadixDialog from "@radix-ui/react-dialog";
import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { useNavigate } from "react-router";
import { useLang, useStrings } from "../i18n";
import { strings } from "../i18n/ai";
import { screens } from "../routes";
import { useAi } from "./AiProvider";
import { useWide } from "./platform";
import { viewLabel } from "./views";

type Command = { id: string; label: string; hint: string; run: () => void };

/** Bỏ dấu và hạ chữ thường, để gõ không dấu vẫn khớp. "đ" không tách được bằng NFD nên đổi riêng. */
function fold(s: string): string {
  return s
    .normalize("NFD")
    .replace(/\p{M}/gu, "")
    .replaceAll("đ", "d")
    .replaceAll("Đ", "d")
    .toLowerCase();
}

export function CommandPalette({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const ai = useAi();
  const navigate = useNavigate();
  const wide = useWide();
  const listId = useId();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  // Chạy xong lệnh "hỏi AI" thì con trỏ phải ở ô hỏi của ngăn, không về nút đã mở thanh lệnh.
  const toAi = useRef(false);

  useEffect(() => {
    if (open) {
      setQuery("");
      setActive(0);
    }
  }, [open]);

  const commands = useMemo<Command[]>(() => {
    // Mọi màn không có tham số trong đường dẫn: mở được mà không cần biết domain hay id.
    const routes = screens
      .filter((s) => s.load !== undefined && !s.bare && !s.path.includes(":"))
      .map<Command>((s) => {
        const name = viewLabel(s.path, lang);
        return {
          id: s.path,
          label: s.path === "/sites/new" ? t.paletteCreateSite : t.paletteOpen(name),
          hint: s.path,
          run: () => {
            // Ngăn phủ kín màn hẹp: đóng lại, nếu không người dùng không thấy mục vừa mở.
            if (!wide) ai.setOpen(false);
            navigate(s.path);
          },
        };
      });
    const toggleAi: Command = {
      id: "ai",
      label: ai.open ? t.paletteCloseAi : t.paletteOpenAi,
      hint: "",
      run: () => {
        if (ai.open) {
          ai.setOpen(false);
        } else {
          toAi.current = true;
          ai.openAI();
        }
      },
    };
    return [toggleAi, ...routes];
  }, [lang, t, wide, ai, navigate]);

  const text = query.trim();
  const options = useMemo<Command[]>(() => {
    const words = fold(text).split(/\s+/).filter(Boolean);
    const hit = commands.filter((c) => {
      const hay = fold(`${c.label} ${c.hint}`);
      return words.every((w) => hay.includes(w));
    });
    if (!text) return hit;
    const ask: Command = {
      id: "hoi-ai",
      label: t.paletteAsk(text),
      hint: t.paletteAskHint,
      run: () => {
        toAi.current = true;
        ai.openAI({ question: text });
      },
    };
    return [...hit, ask];
  }, [commands, text, t, ai]);

  // Gõ chữ khác thì danh sách đổi: về dòng đầu. Không có lệnh nào khớp thì dòng đầu chính là "Hỏi AI".
  useEffect(() => setActive(0), [text]);

  const optionId = (i: number) => `${listId}-${i}`;

  // Dòng đang chọn phải nằm trong tầm nhìn khi danh sách dài hơn khung.
  useEffect(() => {
    document.getElementById(optionId(active))?.scrollIntoView({ block: "nearest" });
  }, [active, options]);

  const run = (c: Command | undefined) => {
    if (!c) return;
    onOpenChange(false);
    c.run();
  };

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((a) => (a + 1) % options.length);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((a) => (a - 1 + options.length) % options.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      run(options[active]);
    }
  };

  return (
    <RadixDialog.Root open={open} onOpenChange={onOpenChange}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay className="fixed inset-0 z-[60] bg-paper/80" />
        <RadixDialog.Content
          onCloseAutoFocus={(e) => {
            if (toAi.current) {
              e.preventDefault();
              toAi.current = false;
            }
          }}
          className="fixed top-[12vh] left-1/2 z-[70] w-[min(560px,calc(100vw-32px))] max-h-[calc(100vh-24vh)] -translate-x-1/2 overflow-y-auto border border-ink bg-surface text-ink"
        >
          <RadixDialog.Title className="sr-only">{t.paletteTitle}</RadixDialog.Title>
          <RadixDialog.Description className="sr-only">{t.paletteHint}</RadixDialog.Description>
          <input
            type="text"
            role="combobox"
            aria-label={t.paletteInput}
            aria-expanded="true"
            aria-controls={listId}
            aria-activedescendant={options.length > 0 ? optionId(active) : undefined}
            aria-autocomplete="list"
            autoComplete="off"
            spellCheck={false}
            placeholder={t.palettePlaceholder}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={onKeyDown}
            className="box-border block h-[48px] w-full border-0 border-b border-hair bg-surface px-[14px] text-[14px] text-ink placeholder:text-muted"
          />
          <ul id={listId} role="listbox" aria-label={t.paletteList} className="m-0 list-none p-0">
            {options.map((c, i) => (
              <li
                key={c.id}
                id={optionId(i)}
                role="option"
                aria-selected={i === active}
                onMouseMove={() => setActive(i)}
                onClick={() => run(c)}
                className={`flex min-h-[44px] cursor-pointer flex-wrap items-baseline justify-between gap-x-[12px] border-l-[3px] px-[11px] py-[8px] text-[14px] sm:min-h-[40px] ${
                  i === active ? "border-ink bg-pre font-semibold" : "border-transparent"
                }`}
              >
                <span className="min-w-0 [overflow-wrap:anywhere]">{c.label}</span>
                {c.hint && <span className="font-mono text-[12px] font-normal text-muted">{c.hint}</span>}
              </li>
            ))}
          </ul>
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}
