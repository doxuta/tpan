// Số request và lỗi 5xx trong một giờ qua của site (mục 6.7), đếm từ
// access.log: GET /api/v1/sites/{domain}/traffic. Hai số lớn theo kiểu "Thanh
// số liệu" nhưng không có thanh, vì không có ngưỡng nào để vẽ vạch. Số 5xx
// không tô đỏ: màu đỏ chỉ dành cho dấu trạng thái (mục 2 của GIAO-DIEN.md).
// Server chỉ đọc được phần cuối của log lớn (partial): khi đó ghi "ít nhất".

import { useQuery } from "@tanstack/react-query";
import { get } from "../../api/client";
import type { Traffic as TrafficData } from "../../api/types";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/site";
import { formatTime } from "./format";

function Figure({ label, value, partial }: { label: string; value: string; partial: boolean }) {
  const t = useStrings(strings);
  return (
    <div className="min-w-0 flex-[0_1_180px]">
      <div className="font-mono text-[11px] tracking-[0.08em] text-muted">{label}</div>
      <div className="mt-[4px] text-[24px] leading-[1.2] font-medium tabular-nums">
        {partial && <span className="mr-[6px] text-[13px] font-normal text-muted">{t.trafficPartial}</span>}
        {value}
      </div>
    </div>
  );
}

export function Traffic({ domain }: { domain: string }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const q = useQuery({
    queryKey: ["traffic", domain],
    queryFn: () => get<TrafficData>(`/api/v1/sites/${encodeURIComponent(domain)}/traffic`),
    // Cùng nhịp với overview; không cần đọc lại cả khi tab nằm nền.
    refetchInterval: 30_000,
  });
  const n = (v: number) => new Intl.NumberFormat(lang === "vi" ? "vi-VN" : "en-GB").format(v);
  const d = q.data;
  const since = d ? Date.parse(d.since) : NaN;

  return (
    <section aria-labelledby="traffic">
      <h2 id="traffic" className="m-0 pt-[26px] pb-[10px] text-[16px] font-semibold">
        {t.trafficHeading}
      </h2>
      {d ? (
        <>
          <div className="flex flex-wrap gap-x-[32px] gap-y-[16px]">
            <Figure label={t.trafficRequests} value={n(d.requests)} partial={d.partial === true} />
            <Figure label={t.trafficErrors} value={n(d.errors_5xx)} partial={d.partial === true} />
          </div>
          <p className="mt-[8px] mb-0 text-[13px] text-muted">
            {Number.isNaN(since) ? "" : `${t.trafficSince(formatTime(since, lang))} `}
            {d.partial && t.trafficPartialNote}
          </p>
        </>
      ) : (
        // Đang tải thì chưa nói gì; lỗi thì nói thẳng là chưa có số, không điền 0.
        q.isError && <p className="m-0 text-muted">{t.trafficNone}</p>
      )}
    </section>
  );
}
