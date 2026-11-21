// Ô mức dùng (mục 6 của docs/GIAO-DIEN.md, bản 2): "5 / ∞", số đã dùng đậm,
// phần "/ trần" màu --muted. Có trần và có phần trăm thì thêm một thanh cao
// 4 px (rãnh --track, phần đầy --ink); không trần thì không vẽ thanh.
//
//   <UsageMeter used="3" limit="10" percent={30} />
//   <UsageMeter used="740,6 MB" limit={null} />           → 740,6 MB / ∞
//   <UsageMeter used={null} limit="1 GB" />                → chưa đo / 1 GB

import { useStrings } from "../i18n";
import { strings } from "../i18n/khung";

type Props = {
  used: string | null;
  limit: string | null;
  percent?: number | null;
  /** Gợi ý khi rê chuột (ví dụ lúc đo). */
  hint?: string;
  /** Thanh rộng tối đa (px) khi có. */
  barWidth?: number;
};

export function UsageMeter({ used, limit, percent = null, hint, barWidth = 64 }: Props) {
  const t = useStrings(strings);
  const p = percent === null || limit === null ? null : Math.min(100, Math.max(0, percent));
  return (
    <span title={hint} className="inline-flex flex-col gap-[4px] whitespace-nowrap tabular-nums">
      <span>
        {used === null ? <span className="text-muted">{t.notMeasured}</span> : <span className="font-medium">{used}</span>}
        <span className="text-muted"> / {limit ?? "∞"}</span>
        {limit === null && <span className="sr-only"> ({t.unlimited})</span>}
      </span>
      {p !== null && (
        // Con số đã nằm ở dòng trên, thanh chỉ là hình nên ẩn khỏi trình đọc màn hình.
        <span aria-hidden="true" className="block h-[4px] overflow-hidden rounded-full bg-track" style={{ width: barWidth }}>
          <span className="block h-full bg-ink" style={{ width: `${p}%` }} />
        </span>
      )}
    </span>
  );
}
