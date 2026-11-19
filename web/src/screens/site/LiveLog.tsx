// Khối "Log trực tiếp" của trang site (mục 10 của KE-HOACH.md): nghe
// /api/v1/sites/{domain}/log?file=access|error|php qua EventSource (client.ts,
// subscribeSiteLog).
//
// Dòng log là dữ liệu không tin cậy (khách lạ gửi URL, User-Agent): mỗi dòng
// chỉ vào DOM như chữ thường ({line} trong JSX, React tự thoát), không bao giờ
// dangerouslySetInnerHTML.
//
// Chọn file khác hoặc rời trang thì effect dọn dẹp đóng luồng cũ. Giữ tối đa
// 500 dòng cuối. Tạm dừng chỉ đóng băng phần đang hiện; luồng vẫn mở và dòng
// mới vẫn được giữ, nên "Tiếp tục" không làm mất dòng nào.

import { useCallback, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from "react";
import { useAi } from "../../ai/AiProvider";
import { subscribeSiteLog } from "../../api/client";
import type { LogFile } from "../../api/types";
import { Button } from "../../components/Button";
import { SegmentButton } from "../../components/SegmentButton";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/site";

const maxLines = 500;
// Một dòng access log thường dưới 1 KB; cắt ở 2000 ký tự để một dòng quái dị không phình DOM.
const maxLineChars = 2000;
const files: LogFile[] = ["access", "error", "php"];

type Conn = "connecting" | "live" | "reconnecting" | "ended" | "failed";
/** total đếm mọi dòng đã nhận (không bị cắt ở 500) để biết bao nhiêu dòng tới lúc đang tạm dừng. */
type Buf = { lines: string[]; total: number };

export function LiveLog({ domain }: { domain: string }) {
  const t = useStrings(strings);
  const [file, setFile] = useState<LogFile>("access");
  const [attempt, setAttempt] = useState(0); // tăng để nối lại sau "end" hay lỗi
  const [conn, setConn] = useState<Conn>("connecting");
  const [endReason, setEndReason] = useState("");
  const [buf, setBuf] = useState<Buf>({ lines: [], total: 0 });
  const [frozen, setFrozen] = useState<Buf | null>(null);
  // Dòng đang chọn để "Giải thích" (chỉ số trong shown.lines). Chọn dòng thì log tạm dừng: dòng mới
  // đẩy dòng cũ ra khỏi 500 dòng giữ lại, chỉ số sẽ trôi sang dòng khác.
  const [selected, setSelected] = useState<number | null>(null);
  const { openAI } = useAi();

  const box = useRef<HTMLDivElement>(null);
  const stick = useRef(true); // người dùng đang ở cuối thì tự cuộn theo dòng mới
  const pending = useRef<string[]>([]);

  const flush = useCallback(() => {
    const add = pending.current;
    pending.current = [];
    if (add.length === 0) return;
    setBuf((b) => ({ lines: [...b.lines, ...add].slice(-maxLines), total: b.total + add.length }));
  }, []);

  useEffect(() => {
    setConn("connecting");
    setEndReason("");
    setFrozen(null);
    setSelected(null);
    pending.current = [];
    setBuf({ lines: [], total: 0 });
    stick.current = true;
    // Gom dòng tới dồn dập (đợt 100 dòng đầu) vào một lần vẽ, thay vì 100 lần vẽ lại cả 500 dòng.
    const timer = window.setInterval(flush, 150);
    const close = subscribeSiteLog(domain, file, {
      onOpen: () => {
        // Mỗi lần mở (kể cả tự nối lại sau 10 phút) server gửi lại 100 dòng cuối: bỏ dòng cũ để không bị lặp.
        pending.current = [];
        setBuf({ lines: [], total: 0 });
        setConn("live");
      },
      onLine: (line) => pending.current.push(line.slice(0, maxLineChars)),
      onEnd: (reason) => {
        flush();
        setEndReason(reason);
        setConn("ended");
      },
      onError: (closed) => setConn(closed ? "failed" : "reconnecting"),
    });
    return () => {
      close();
      window.clearInterval(timer);
    };
  }, [domain, file, attempt, flush]);

  const shown = frozen ?? buf;
  useLayoutEffect(() => {
    const el = box.current;
    if (el && stick.current && !frozen) el.scrollTop = el.scrollHeight;
  }, [shown, frozen]);

  const select = (i: number | null) => {
    if (i !== null && !frozen) setFrozen(buf);
    setSelected(i);
  };
  const selectedLine = selected === null ? undefined : shown.lines[selected];

  // Mũi tên lên xuống đổi dòng chọn khi con trỏ đang ở khung log (người dùng bàn phím không có chuột để bấm dòng).
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if ((e.key !== "ArrowDown" && e.key !== "ArrowUp") || shown.lines.length === 0) return;
    e.preventDefault();
    const last = shown.lines.length - 1;
    const down = e.key === "ArrowDown";
    const next = selected === null ? (down ? 0 : last) : Math.min(last, Math.max(0, selected + (down ? 1 : -1)));
    select(next);
    box.current?.children[next]?.scrollIntoView({ block: "nearest" });
  };

  const onScroll = () => {
    const el = box.current;
    if (el) stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 8;
  };

  const over = conn === "ended" || conn === "failed";
  // Luồng đã dừng thì nói điều đó trước, kể cả đang tạm dừng: không còn gì để "tiếp tục".
  const status =
    conn === "ended"
      ? t.logEnded(endReason)
      : conn === "failed"
        ? t.logFailed
        : frozen !== null
          ? t.logPaused(Math.max(0, buf.total - frozen.total))
          : conn === "connecting"
            ? t.logConnecting
            : conn === "reconnecting"
              ? t.logReconnecting
              : t.logLive;

  return (
    <section aria-labelledby="log-truc-tiep">
      <div className="flex flex-wrap items-end justify-between gap-x-[24px] gap-y-[6px] pt-[26px] pb-[10px]">
        <h2 id="log-truc-tiep" className="m-0 text-[16px] font-semibold">
          {t.logHeading}
        </h2>
        <span className="text-[13px] text-muted">{t.logHint}</span>
      </div>
      <div className="flex flex-wrap items-center gap-x-[14px] gap-y-[10px] pb-[12px]">
        <SegmentButton label={t.logFileLabel} value={file} onChange={setFile} options={files.map((f) => ({ value: f, label: t.logFiles[f] }))} />
        {over ? (
          <Button onClick={() => setAttempt((n) => n + 1)}>{t.logReconnect}</Button>
        ) : (
          <Button
            aria-pressed={frozen !== null}
            onClick={() => {
              setSelected(null);
              setFrozen(frozen ? null : buf);
            }}
          >
            {frozen ? t.logResume : t.logPause}
          </Button>
        )}
        {/* Dòng log là chữ không tin cậy: chỉ đi vào câu hỏi như chữ thường, người xem trước rồi tự gửi. Nút vẫn hiện khi AI tắt; ngăn AI tự chỉ cách nối. */}
        <Button
          disabled={selectedLine === undefined}
          title={t.explainHint}
          onClick={() =>
            selectedLine !== undefined &&
            openAI({
              mode: "explain",
              question: t.explainLogQuestion,
              pointer: { kind: "log_line", ref: `${domain}/${file}`, text: selectedLine, label: t.explainLogLabel(t.logFiles[file]) },
            })
          }
        >
          {t.explainLogButton}
        </Button>
        <span aria-live="polite" className="min-w-0 flex-[1_1_240px] text-[13px] text-muted [overflow-wrap:anywhere]">
          {status}
        </span>
      </div>
      <p aria-live="polite" className="m-0 pb-[8px] text-[12px] text-muted">
        {selected === null ? t.explainLogNone : t.explainLogSelected}
      </p>
      {/* tabIndex 0 để người dùng bàn phím cuộn được khung; aria-live tắt vì dòng log tới liên tục sẽ làm trình đọc màn hình nói không ngừng. */}
      <div
        ref={box}
        role="log"
        tabIndex={0}
        aria-live="off"
        aria-label={`${t.logHeading}: ${t.logFiles[file]}`}
        onScroll={onScroll}
        onKeyDown={onKeyDown}
        className="max-h-[360px] min-h-[120px] overflow-y-auto bg-pre p-[12px] font-mono text-[12px] leading-[1.55] text-ink"
      >
        {shown.lines.length === 0 ? (
          <span className="text-muted">{conn === "live" ? t.logEmpty : ""}</span>
        ) : (
          shown.lines.map((line, i) => (
            <div key={i} onClick={() => select(selected === i ? null : i)} className={`cursor-pointer whitespace-pre-wrap [overflow-wrap:anywhere] ${selected === i ? "bg-ink text-on-ink" : ""}`}>
              {line || " "}
            </div>
          ))
        )}
      </div>
    </section>
  );
}
