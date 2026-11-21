// Thao tác hàng loạt trên một bảng: ô chọn từng hàng, ô chọn tất cả, thanh
// "Đã chọn N" với các nút việc. Bấm việc thì hộp hỏi lại liệt kê đúng các mục
// đã chọn; đồng ý thì chạy LẦN LƯỢT từng mục (mỗi mục một job như bấm tay, chờ
// job trước xong mới gửi job sau, để không giành khoá dpkg hay reload chồng
// nhau). Tiến độ từng job hiện ở thẻ theo dõi góc dưới (JobTracker).
//
//   const sel = useSelection(rows.map((r) => r.domain));
//   <SelectAll sel={sel} label="Chọn mọi site" />   // ô ở đầu cột
//   <SelectOne sel={sel} id={r.domain} label={`Chọn ${r.domain}`} />
//   <BulkBar sel={sel} actions={[{ label: (n) => `Đưa ${n} site vào thùng rác`, ... }]} />

import { useCallback, useMemo, useState, type ReactNode } from "react";
import type { JobStarted } from "../api/types";
import { errorMessage } from "../api/client";
import { useLang, useStrings } from "../i18n";
import { strings } from "../i18n/khung";
import { Button } from "./Button";
import { Dialog } from "./Dialog";
import { Checkbox } from "./Field";
import { useJobTracker } from "./JobTracker";

export type Selection = {
  ids: string[];
  has: (id: string) => boolean;
  toggle: (id: string) => void;
  all: boolean;
  some: boolean;
  toggleAll: () => void;
  clear: () => void;
};

/** Chọn theo id; id không còn trong bảng (vừa xoá) thì tự rơi khỏi lựa chọn. */
export function useSelection(available: string[]): Selection {
  const [picked, setPicked] = useState<Set<string>>(() => new Set());
  const key = available.join("\n");
  const ids = useMemo(() => available.filter((id) => picked.has(id)), [key, picked]); // eslint-disable-line react-hooks/exhaustive-deps
  const toggle = useCallback(
    (id: string) =>
      setPicked((cur) => {
        const next = new Set(cur);
        if (!next.delete(id)) next.add(id);
        return next;
      }),
    [],
  );
  const all = available.length > 0 && ids.length === available.length;
  return {
    ids,
    has: (id) => picked.has(id),
    toggle,
    all,
    some: ids.length > 0 && !all,
    toggleAll: () => setPicked(all ? new Set() : new Set(available)),
    clear: () => setPicked(new Set()),
  };
}

export function SelectAll({ sel, label }: { sel: Selection; label: string }) {
  return (
    <input
      type="checkbox"
      aria-label={label}
      title={label}
      // Cùng kiểu với Checkbox (Field.tsx); không dùng Checkbox được vì ô này cần ref để đặt trạng thái "một phần".
      className="size-[16px] flex-none cursor-pointer accent-[var(--accent)] max-sm:size-[20px]"
      checked={sel.all}
      ref={(el) => {
        if (el) el.indeterminate = sel.some;
      }}
      onChange={sel.toggleAll}
    />
  );
}

export function SelectOne({ sel, id, label }: { sel: Selection; id: string; label: string }) {
  return <Checkbox label={label} checked={sel.has(id)} onChange={() => sel.toggle(id)} />;
}

export type BulkAction = {
  key: string;
  /** Chữ trên nút, theo số mục đã chọn: "Đưa 3 site vào thùng rác". */
  label: (n: number) => string;
  confirmTitle: (n: number) => string;
  /** Câu nói rõ hậu quả, hiện trên danh sách mục trong hộp hỏi lại. */
  confirmBody: ReactNode;
  confirmLabel: (n: number) => string;
  /** Gửi lệnh thật cho một mục (không dry_run); trả job để theo dõi. */
  run: (id: string) => Promise<JobStarted>;
  primary?: boolean;
};

export function BulkBar({ sel, actions, unit }: { sel: Selection; actions: BulkAction[]; unit: string }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const { track, wait } = useJobTracker();
  const [asking, setAsking] = useState<BulkAction | null>(null);
  const [progress, setProgress] = useState<{ done: number; total: number; failed: string[] } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const running = progress !== null && progress.done < progress.total;

  if (sel.ids.length === 0 && !progress && !error) return null;

  async function start(a: BulkAction) {
    const list = [...sel.ids];
    setAsking(null);
    setError(null);
    setProgress({ done: 0, total: list.length, failed: [] });
    const failed: string[] = [];
    for (let i = 0; i < list.length; i++) {
      const id = list[i];
      try {
        const job = await a.run(id);
        track(job.job_id);
        const status = await wait(job.job_id);
        if (status !== "succeeded") failed.push(id);
      } catch (err) {
        // Mục này bị từ chối (ví dụ điều kiện đầu chưa đạt): ghi lại rồi làm tiếp mục sau.
        failed.push(id);
        setError(`${id}: ${errorMessage(err, lang)}`);
      }
      setProgress({ done: i + 1, total: list.length, failed: [...failed] });
    }
    sel.clear();
  }

  return (
    // mx và mb: dải nằm trong thẻ bảng (prop `above` của TableList), nên cách mép thẻ 16 px và cách hàng đầu 12 px.
    <div className="mx-[16px] mb-[12px] flex flex-wrap items-center gap-[10px] rounded-ctl bg-accent-tint px-[12px] py-[8px]">
      {running && progress ? (
        <span role="status" className="text-[14px] font-medium">
          {t.bulkRunning(progress.done, progress.total)}
        </span>
      ) : progress ? (
        <span role="status" className="text-[14px] font-medium">
          {t.bulkDone(progress.total - progress.failed.length, progress.total)}
          {progress.failed.length > 0 && <span className="font-normal text-fail-text"> · {t.bulkFailed(progress.failed.join(", "))}</span>}
        </span>
      ) : (
        <span className="text-[14px] font-medium">{t.bulkSelected(sel.ids.length, unit)}</span>
      )}
      <span className="flex-1" />
      {!running &&
        sel.ids.length > 0 &&
        actions.map((a) => (
          <Button key={a.key} size="sm" variant={a.primary ? "primary" : "secondary"} onClick={() => setAsking(a)}>
            {a.label(sel.ids.length)}
          </Button>
        ))}
      {!running && sel.ids.length > 0 && (
        <Button size="sm" onClick={sel.clear}>
          {t.bulkClear}
        </Button>
      )}
      {!running && progress && sel.ids.length === 0 && (
        <Button
          size="sm"
          onClick={() => {
            setProgress(null);
            setError(null);
          }}
        >
          {t.close}
        </Button>
      )}
      {error && <p className="m-0 basis-full text-[13px] text-fail-text">{error}</p>}

      {asking && (
        <Dialog
          open
          onOpenChange={(o) => !o && setAsking(null)}
          title={asking.confirmTitle(sel.ids.length)}
          confirmLabel={asking.confirmLabel(sel.ids.length)}
          onConfirm={() => void start(asking)}
        >
          <div className="text-[14px]">{asking.confirmBody}</div>
          <ul className="mt-[10px] mb-0 max-h-[240px] overflow-y-auto pl-[18px] text-[14px]">
            {sel.ids.map((id) => (
              <li key={id} className="[overflow-wrap:anywhere]">
                {id}
              </li>
            ))}
          </ul>
        </Dialog>
      )}
    </div>
  );
}
