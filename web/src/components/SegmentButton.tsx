// Nút đoạn (Tiếng Việt/English, Sáng/Tối, 1 giờ/24 giờ): nhóm bo 8 px viền
// --line, các ô cao 40 px (44 px dưới 640 px) dính liền nhau; ô đang chọn nền
// --accent-tint chữ --accent và aria-pressed="true" (mục 6).
//
//   <SegmentButton label="Ngôn ngữ" value={lang} onChange={setLang}
//     options={[{ value: "vi", label: "Tiếng Việt" }, { value: "en", label: "English" }]} />

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
    <div role="group" aria-label={label} className="inline-flex overflow-hidden rounded-ctl border border-line bg-surface">
      {options.map((o, i) => {
        const on = o.value === value;
        return (
          <button
            key={o.value}
            type="button"
            aria-pressed={on}
            onClick={() => onChange(o.value)}
            // Khung nhóm cắt tràn (overflow-hidden, để ô đầu và ô cuối theo góc bo), nên viền focus vẽ vào trong ô.
            className={
              "inline-flex h-[42px] min-w-[44px] items-center justify-center px-[12px] text-[14px] whitespace-nowrap transition-colors focus-visible:-outline-offset-2 sm:h-[38px] " +
              (i > 0 ? "border-l border-line " : "") +
              (on ? "bg-accent-tint font-semibold text-accent" : "bg-surface text-ink hover:bg-soft")
            }
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
