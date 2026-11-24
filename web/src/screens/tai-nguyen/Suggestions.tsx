// Thẻ "Gợi ý mức tối đa" của màn Tài nguyên (chỉ admin). Mỗi gói một khối mở
// ra: dòng tóm tắt nói mức đang đặt và mức gợi ý của các trường có đổi, mở ra
// thì thấy từng site của gói: số mẫu, khoảng thời gian, cờ "Ít số liệu" và các
// câu giải thích đã tính ra sao (reasons, song ngữ từ API).

import { Link } from "react-router";
import { errorMessage } from "../../api/client";
import type { useSuggestions } from "../../api/m17";
import type { PackageSuggestion, Suggestion } from "../../api/m17-types";
import { Card } from "../../components/Card";
import { Disclosure } from "../../components/Disclosure";
import { EmptyState } from "../../components/EmptyState";
import { Notice } from "../../components/Notice";
import { Tag } from "../../components/Tag";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/tai-nguyen";
import { formatWhen } from "../thung-rac/model";
import { changedLimits } from "./model";

const namesShown = 3;

export function Suggestions({ query }: { query: ReturnType<typeof useSuggestions> }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const packages = query.data?.packages ?? [];
  const sites = query.data?.sites ?? [];

  const label = { ram_mb: t.sugRam, cpu_percent: t.sugCpu, tasks: t.sugTasks } as const;
  const value = { ram_mb: t.sugRamValue, cpu_percent: t.sugCpuValue, tasks: t.sugTasksValue } as const;

  const summary = (p: PackageSuggestion) => {
    const changed = changedLimits(p.current, p.suggested);
    return (
      <span className="flex flex-col gap-[2px]">
        <span>
          <span className="font-medium">{p.package}</span>
          <span className="text-muted"> · {t.sugSites(p.sites.slice(0, namesShown), Math.max(0, p.sites.length - namesShown))}</span>
        </span>
        <span className="text-[13px] text-muted">
          {changed.length === 0 ? t.sugFine : changed.map((key) => t.sugCurrent(label[key], value[key](p.current[key]), value[key](p.suggested[key]))).join(" · ")}
        </span>
      </span>
    );
  };

  const detail = (s: Suggestion) => (
    <li key={s.domain} className="flex flex-col gap-[4px] border-t border-hair py-[10px] first:border-t-0">
      <span className="flex flex-wrap items-center gap-x-[8px] gap-y-[4px]">
        <span className="font-medium [overflow-wrap:anywhere]">{s.domain}</span>
        {s.low_data && <Tag title={t.sugLowDataHint}>{t.sugLowData}</Tag>}
        <span className="text-[13px] text-muted">
          {t.sugSamples(s.samples)}, {t.sugRange(formatWhen(s.from, lang), formatWhen(s.to, lang))}
        </span>
      </span>
      {s.reasons.length > 0 && (
        <ul className="m-0 flex list-none flex-col gap-[2px] p-0 text-[13px] text-muted">
          {s.reasons.map((r, i) => (
            <li key={i}>{tx(r, lang)}</li>
          ))}
        </ul>
      )}
    </li>
  );

  return (
    <Card title={t.sugTitle} titleId="usage-suggestions" actions={<Link to="/packages">{t.editPackages}</Link>}>
      {query.isPending && <p className="m-0 text-muted">{k.loading}</p>}
      {query.isError && (
        <Notice kind="fail">
          {t.sugError} {errorMessage(query.error, lang)}
        </Notice>
      )}
      {query.isSuccess && (
        <>
          <p className="mt-0 mb-[8px] max-w-[72ch] text-muted">{t.sugNote}</p>
          {packages.length === 0 ? (
            <EmptyState>{t.sugNone}</EmptyState>
          ) : (
            <div>
              {packages.map((p) => (
                <Disclosure key={p.package} summary={summary(p)}>
                  <ul aria-label={t.sugDetail(p.package)} className="m-0 list-none p-0">
                    {sites.filter((s) => s.package === p.package).map(detail)}
                  </ul>
                </Disclosure>
              ))}
            </div>
          )}
        </>
      )}
    </Card>
  );
}
