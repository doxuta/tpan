// Đường đi request của MỘT site, vẽ ngang một tuyến (mục 1 và 7 của
// docs/GIAO-DIEN.md, bản 2). Ga to hơn bản đồ ở Bảng điều khiển và có tên bên dưới.
// Bấm một ga thì trang chuyển sang tab Phép kiểm (index.tsx), nên nút ga không còn
// aria-controls: nhóm phép kiểm của nó không có trong trang lúc đang ở tab Tổng quan.
//
// Nét tuyến: liền tới ga cuối còn đạt, đứt từ ga hỏng trở đi. Đoạn nối hai ga
// là một nét; nó đứt khi một trong hai ga chưa kiểm được, hoặc khi ga bên trái
// đã hỏng hay nằm sau ga hỏng.

import type { CheckResult, Stage, Status } from "../../api/types";
import { StatusMark } from "../../components/StatusMark";
import { useStatusName } from "../../components/useStatusName";
import { useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/site";
import { checksOfStage, routeStages, worstMark } from "./route";

/** Id của nhóm phép kiểm ứng với một ga (tab Phép kiểm), để chuyển focus tới khi bấm ga. */
export function stageGroupId(stage: Stage): string {
  return `chang-${stage}`;
}

/** Chú giải bốn hình, nằm ngay trên tuyến (mục 2). */
export function Legend() {
  const k = useStrings(khung);
  const items: Status[] = ["pass", "warn", "fail", "unknown"];
  return (
    <ul className="m-0 flex list-none flex-wrap gap-x-[18px] gap-y-[8px] p-0">
      {items.map((s) => (
        <li key={s} className="flex items-center gap-[8px] text-[13px] text-muted">
          <span className="flex h-[20px] w-[20px] items-center justify-center">
            <StatusMark status={s} />
          </span>
          <span>{k.status[s]}</span>
        </li>
      ))}
    </ul>
  );
}

type Props = {
  domain: string;
  checks: CheckResult[];
  /** Ga vừa được bấm (gạch chân tên ga). */
  selected: Stage | null;
  onSelect: (stage: Stage) => void;
  /** M13: site đang tạm khoá. Chặng không có phép kiểm nào thì xám "tạm khoá" thay vì "chưa kiểm được". */
  suspended?: boolean;
  /** M29: site PHP trống. Ga "WordPress" khi đó mang tên "Mã nguồn" (site không có WordPress). */
  plain?: boolean;
};

export function RouteMap({ domain, checks, selected, onSelect, suspended = false, plain = false }: Props) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const statusName = useStatusName();
  const states = routeStages.map((stage) => ({ stage, ...worstMark(checksOfStage(checks, stage), suspended) }));
  const firstFail = states.findIndex((s) => s.status === "fail");

  // Đoạn j nằm giữa ga j-1 và ga j (j = 0 là đoạn trước ga đầu, j = 6 là đoạn sau ga cuối).
  const dashed = (j: number): boolean => {
    const left = states[j - 1];
    const right = states[j];
    return left?.status === "unknown" || right?.status === "unknown" || (firstFail >= 0 && j - 1 >= firstFail);
  };
  const seg = (j: number) => (dashed(j) ? "border-dashed border-route-dash" : "border-solid border-route");

  return (
    // -m/p 4 px: chừa chỗ cho viền focus của nút ga (nút sát mép khung cuộn), bố cục không đổi.
    <div className="relative -m-[4px] overflow-x-auto p-[4px]">
      <ol aria-label={t.routeLabel(domain)} className="m-0 grid min-w-[600px] list-none grid-cols-6 border-b border-hair p-0">
        {states.map((s, i) => {
          const name = plain && s.stage === "wordpress" ? t.stageCode : t.stages[s.stage];
          const on = selected === s.stage;
          return (
            <li key={s.stage} className="min-w-0">
              <button
                type="button"
                aria-current={on ? "true" : undefined}
                aria-label={k.statusLabel(name, statusName(s.status, s.suspended))}
                onClick={() => onSelect(s.stage)}
                className="group block w-full bg-transparent p-0 text-center"
              >
                <span className="relative block h-[48px]">
                  <span aria-hidden="true" className={`absolute top-[23px] right-1/2 left-0 border-t-2 ${seg(i)}`} />
                  <span aria-hidden="true" className={`absolute top-[23px] right-0 left-1/2 border-t-2 ${seg(i + 1)}`} />
                  <span className="absolute top-[24px] left-1/2 flex -translate-x-1/2 -translate-y-1/2 items-center justify-center">
                    <StatusMark status={s.status} size="lg" />
                  </span>
                </span>
                <span
                  aria-hidden="true"
                  className={`inline-block border-b-2 px-[2px] pb-[2px] text-[12px] font-medium ${on ? "border-accent text-ink" : "border-transparent text-muted group-hover:text-ink"}`}
                >
                  {name}
                </span>
                <span className="block h-[10px]" />
              </button>
            </li>
          );
        })}
      </ol>
    </div>
  );
}
