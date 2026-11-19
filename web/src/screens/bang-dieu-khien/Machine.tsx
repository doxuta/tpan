// Khối "Máy" (mục 6, "Thanh số liệu"): CPU, RAM, đĩa, swap, mỗi số một thanh có vạch ngưỡng 80%.

import type { Machine as MachineData } from "../../api/types";
import { MetricBar } from "../../components/MetricBar";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bang-dieu-khien";
import { fmtBytes, fmtNumber } from "./model";

export function Machine({ m }: { m: MachineData }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const pct = (used: number, total: number) => (total > 0 ? (used / total) * 100 : null);

  const cpuKnown = m.cpu_percent >= 0;
  const ramKnown = m.mem_total > 0;
  const diskKnown = m.disk.total > 0;

  return (
    <section className="pt-[28px]">
      <h2 className="m-0 mb-[14px] text-[16px] font-semibold">{t.machineTitle}</h2>
      <div className="flex flex-wrap gap-x-[32px] gap-y-[20px]">
        <MetricBar
          label={t.cpu}
          value={cpuKnown ? `${fmtNumber(m.cpu_percent, lang, 0)}%` : t.notMeasured}
          sub={cpuKnown ? t.cpuSub(fmtNumber(m.load1, lang, 2), m.cpus) : t.notMeasuredSub}
          percent={cpuKnown ? m.cpu_percent : null}
        />
        <MetricBar
          label={t.ram}
          value={ramKnown ? fmtBytes(m.mem_used, lang) : t.notMeasured}
          sub={ramKnown ? t.ofTotal(fmtBytes(m.mem_total, lang)) : t.notMeasuredSub}
          percent={ramKnown ? pct(m.mem_used, m.mem_total) : null}
        />
        <MetricBar
          label={t.disk}
          value={diskKnown ? fmtBytes(m.disk.used, lang) : t.notMeasured}
          sub={diskKnown ? t.diskSub(fmtBytes(m.disk.total, lang)) : t.notMeasuredSub}
          percent={diskKnown ? pct(m.disk.used, m.disk.total) : null}
        />
        {/* Máy không có swap: nói thẳng, không vẽ thanh rỗng. */}
        {m.swap_total > 0 ? (
          <MetricBar label={t.swap} value={fmtBytes(m.swap_used, lang)} sub={t.ofTotal(fmtBytes(m.swap_total, lang))} percent={pct(m.swap_used, m.swap_total)} />
        ) : (
          <MetricBar label={t.swap} value={t.swapNone} sub={t.swapNoneSub} percent={null} />
        )}
      </div>
      {m.errors && m.errors.length > 0 && (
        <p className="mt-[12px] mb-0 text-[13px] text-muted">
          {t.machineErrors} {m.errors.join("; ")}
        </p>
      )}
    </section>
  );
}
