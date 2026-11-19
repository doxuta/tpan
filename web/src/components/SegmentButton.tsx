// Nút đoạn (VI/EN, Sáng/Tối): các ô cao 40 px dính liền nhau, chữ mono 12 px,
// ô đang chọn nền --ink chữ --on-ink và aria-pressed="true" (mục 6).
//
//   <SegmentButton label="Ngôn ngữ" value={lang} onChange={setLang}
//     options={[{ value: "vi", label: "VI" }, { value: "en", label: "EN" }]} />

type Option<T extends string> = { value: T; label: string };

type Props<T extends string> = {
  /** Tên nhóm cho trình đọc màn hình. */
  label: string;
  options: Option<T>[];
  value: T;
  onChange: (v: T) => void;
};

export function SegmentButton<T extends string>({ label, options, value, onChange }: Props<T>) {
  return (
    <div role="group" aria-label={label} className="flex">
      {options.map((o, i) => {
        const on = o.value === value;
        return (
          <button
            key={o.value}
            type="button"
            aria-pressed={on}
            onClick={() => onChange(o.value)}
            className={
              "inline-flex h-[44px] min-w-[44px] items-center justify-center border border-ink px-[10px] font-mono text-[12px] transition-colors sm:h-[40px] " +
              (i > 0 ? "-ml-px " : "") +
              (on ? "bg-ink text-on-ink" : "bg-surface text-ink hover:bg-pre")
            }
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
