// Thanh số liệu (mục 6): nhãn 12/500, số 24 px, một dòng phụ, rồi thanh cao
// 6 px: nền --track, phần đầy --ink, vạch ngưỡng 80% màu --muted.
//
//   <MetricBar label="CPU" value="12%" sub="tải 0,42 · 4 nhân" percent={12} />
//
// percent null: không vẽ thanh (ví dụ máy không có swap, hoặc chưa đo được).

type Props = {
  label: string;
  value: string;
  sub?: string;
  percent: number | null;
  /** Vạch ngưỡng, phần trăm; mặc định 80. */
  threshold?: number;
};

export function MetricBar({ label, value, sub, percent, threshold = 80 }: Props) {
  const clamped = percent === null ? null : Math.min(100, Math.max(0, percent));
  return (
    <div className="min-w-0 flex-[1_1_150px]">
      <div className="text-[12px] font-medium text-muted">{label}</div>
      <div className="mt-[4px] text-[24px] leading-[1.2] font-medium tabular-nums">{value}</div>
      {sub && <div className="text-[13px] text-muted">{sub}</div>}
      {clamped !== null && (
        // Con số đã nằm ở dòng trên, thanh chỉ là hình nên ẩn khỏi trình đọc màn hình.
        <div aria-hidden="true" className="relative mt-[10px] h-[6px] rounded-full bg-track">
          <div className="h-full rounded-full bg-ink" style={{ width: `${clamped}%` }} />
          <div className="absolute top-[-3px] h-[12px] w-[2px] bg-muted" style={{ left: `${threshold}%` }} />
        </div>
      )}
    </div>
  );
}
