// "Request tới site này đi đường nào" (mục 6.13 của KE-HOACH.md): bấm nút thì
// máy chủ lần đường đi từ cấu hình ĐANG CHẠY (GET /sites/{domain}/route) và
// trả các chặng theo thứ tự: tên miền → vhost → root → socket → pool → user →
// open_basedir → gọi thật. Mỗi chặng có dấu trạng thái, tên, giá trị (chữ mono
// bấm là chép), một câu tóm tắt, và mở ra được lệnh cùng output.
//
// Khác bản đồ tuyến phía trên: bản đồ vẽ kết quả health đã lưu, còn khối này
// chạy lệnh thật ngay lúc bấm, nên chỉ gọi khi người dùng yêu cầu.

import { useState } from "react";
import { useRouteTrace } from "../../api/m14";
import type { RouteHop } from "../../api/m14-types";
import { Button } from "../../components/Button";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { Icon } from "../../components/Icon";
import { MonoText } from "../../components/MonoText";
import { StatusMark } from "../../components/StatusMark";
import { Tag } from "../../components/Tag";
import { useStatusName } from "../../components/useStatusName";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/site";
import { ErrorNote } from "./ErrorNote";
import { formatTime } from "./format";

const hopPanelId = (key: string) => `chang-that-${key}`;

export function RouteExplain({ domain }: { domain: string }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const trace = useRouteTrace(domain);
  /** Giờ của lần lần gần nhất: kết quả là ảnh chụp lúc đó, không tự cập nhật. */
  const [at, setAt] = useState(0);
  const [open, setOpen] = useState<Set<string>>(new Set());

  const run = () =>
    trace.mutate(undefined, {
      onSuccess: (d) => {
        setAt(Date.now());
        // Chặng lệch mở sẵn: người bấm nút này đang tìm chỗ request đi sai.
        setOpen(new Set((d.hops ?? []).filter((h) => h.status === "fail").map((h) => h.key)));
      },
    });
  const toggle = (key: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (!next.delete(key)) next.add(key);
      return next;
    });

  const done = trace.isSuccess || trace.isError;
  const hops = trace.data?.hops ?? [];

  return (
    // Nằm trong thẻ "Đường đi request": kẻ một đường phân với bản đồ tuyến phía trên.
    <div className="mt-[16px] border-t border-hair pt-[16px]">
      <div className="flex flex-wrap items-center gap-x-[14px] gap-y-[8px]">
        <Button onClick={run} disabled={trace.isPending}>
          {trace.isPending ? t.traceRunning : done ? t.traceAgain : t.traceButton}
        </Button>
        {done && <Button onClick={() => trace.reset()}>{t.traceClose}</Button>}
        <span aria-live="polite" className="text-[13px] text-muted">
          {trace.isSuccess ? t.traceAt(formatTime(at, lang)) : ""}
        </span>
      </div>
      {!done && <p className="mt-[8px] mb-0 max-w-[720px] text-[13px] text-muted">{t.traceHint}</p>}

      {trace.isError && (
        <div className="pt-[12px]">
          <ErrorNote error={trace.error} heading={t.traceFailed} />
        </div>
      )}

      {trace.isSuccess && (
        <div className="pt-[14px]">
          <div className="flex flex-wrap items-center gap-x-[10px] gap-y-[6px]">
            <h3 className="m-0 text-[12px] font-medium text-muted">{t.traceLabel}</h3>
            {trace.data.web && <Tag>{trace.data.web}</Tag>}
          </div>
          {hops.length === 0 ? (
            <p className="mt-[8px] mb-0">{t.traceEmpty}</p>
          ) : (
            <ol aria-label={t.traceListLabel(domain)} className="m-0 mt-[8px] list-none border-t border-hair p-0">
              {hops.map((h) => (
                <Hop key={h.key} hop={h} open={open.has(h.key)} onToggle={() => toggle(h.key)} />
              ))}
            </ol>
          )}
        </div>
      )}
    </div>
  );
}

function Hop({ hop, open, onToggle }: { hop: RouteHop; open: boolean; onToggle: () => void }) {
  const t = useStrings(strings);
  const statusName = useStatusName();
  const { lang } = useLang();
  const panelId = hopPanelId(hop.key);

  return (
    <li className="border-t border-hair first:border-t-0">
      {/* Nút mở chặng chỉ ôm dấu và tên: giá trị bấm-là-chép là một <button> khác nên phải nằm ngoài nút này. */}
      <button type="button" aria-expanded={open} aria-controls={panelId} onClick={onToggle} className="flex min-h-[44px] w-full items-start gap-[12px] bg-transparent pt-[10px] pb-[2px] text-left text-ink sm:min-h-[34px]">
        <span className="flex h-[21px] w-[20px] flex-none items-center justify-center">
          <StatusMark status={hop.status} />
        </span>
        <span className="min-w-0 flex-1 font-medium">
          <span className="sr-only">{statusName(hop.status)}: </span>
          {tx(hop.title, lang)}
        </span>
        <Icon name="chevron-down" size={16} className={`mt-[3px] flex-none text-muted transition-transform ${open ? "rotate-180" : ""}`} />
      </button>
      <div className="pb-[10px] pl-[32px]">
        <div>{hop.value ? <MonoText>{hop.value}</MonoText> : <span className="text-[13px] text-muted">{t.traceNoValue}</span>}</div>
        <p className="m-0 text-muted">{tx(hop.summary, lang)}</p>
      </div>
      {open && (
        <div id={panelId} className="pb-[16px] pl-[32px]">
          {hop.evidence && hop.evidence.length > 0 ? <EvidenceBlock evidence={hop.evidence} clamp={20} /> : <p className="m-0 text-muted">{t.noEvidence}</p>}
        </div>
      )}
    </li>
  );
}
