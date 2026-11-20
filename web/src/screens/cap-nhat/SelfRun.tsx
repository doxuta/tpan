// Hai trạng thái của một lượt cập nhật TPAN trong khối "Phiên bản và cập nhật"
// (mục 6.14 của KE-HOACH.md, quyết định 5 và 9):
//   SelfProgress  đang chạy: dấu trung tính, không nút nào; panel đang restart
//                 thì thêm một dòng nhỏ, không báo lỗi
//   SelfResult    kết quả của lượt gần nhất. fresh: trình duyệt này vừa theo
//                 dõi lượt đó, nên mời tải lại trang (mã giao diện đang mở còn
//                 là của bản cũ). Không fresh thì đây là dòng "lượt gần nhất".

import { Link } from "react-router";
import { useIsolation } from "../../api/m14";
import { selfUnit, type SelfAction, type SelfRun } from "../../api/m15-types";
import type { Status } from "../../api/types";
import { Button } from "../../components/Button";
import { CommandBlock } from "../../components/CommandBlock";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { StatusMark } from "../../components/StatusMark";
import { useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/phien-ban";
import { formatDateTime } from "../lich-su/format";
import type { SelfPhase } from "./selfPhase";

export const labelClass = "m-0 font-mono text-[11px] font-normal tracking-[0.08em] text-muted";

// Ô giữ dấu trạng thái thẳng hàng với dòng chữ đầu (như ErrorNote): bốn dấu có bốn cỡ khác nhau.
export const markBox ="flex h-[21px] w-[20px] flex-none items-center justify-center";

// Lệnh gỡ bằng tay khi lượt chạy không báo lại: log của đơn vị tạm, tình trạng dịch vụ, quay về bản trước.
const stuckCommands = [`sudo journalctl -u ${selfUnit} --no-pager -n 50`, "sudo systemctl status tpan", "sudo tpan self-update --rollback"];

export function SelfProgress({ action, restarting }: { action: SelfAction; restarting: boolean }) {
  const t = useStrings(strings);
  return (
    <div role="status" className="mt-[16px] flex items-start gap-[10px]">
      {/* Vòng rỗng "chưa kiểm được": chưa biết kết quả, như thẻ theo dõi job lúc đang chạy. */}
      <span className={markBox}>
        <StatusMark status="unknown" />
      </span>
      <div className="min-w-0 flex-1">
        <p className="m-0 font-medium">{action === "rollback" ? t.rollingBack : t.updating}</p>
        <p className="m-0 mt-[2px] max-w-[720px] text-muted">{t.updatingHint}</p>
        {restarting && <p className="m-0 mt-[6px] text-[13px] text-muted">{t.restarting}</p>}
      </div>
    </div>
  );
}

export type SettledPhase = Exclude<SelfPhase, { kind: "idle" | "updating" }>;

const marks: Record<SettledPhase["kind"], Status> = { done: "pass", same: "pass", failed: "fail", stuck: "warn" };

export function SelfResult({ phase, run, fresh }: { phase: SettledPhase; run: SelfRun; fresh: boolean }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const rollback = run.action === "rollback";
  // Bản mới có thể mang phép kiểm cô lập mới (M14). Hỏi cả khi không fresh: sau
  // khi tải lại trang dòng này vẫn phải còn, tới lúc bản vá được áp.
  const iso = useIsolation(phase.kind === "done");

  const headline =
    phase.kind === "done"
      ? (rollback ? t.doneRollback : t.doneUpdate)(phase.to)
      : phase.kind === "same"
        ? t.same
        : phase.kind === "failed"
          ? rollback
            ? t.failedRollback
            : t.failedUpdate
          : rollback
            ? t.stuckRollback
            : t.stuckUpdate;
  const to = run.to ?? (phase.kind === "done" ? phase.to : "");
  const by = t.runBy(formatDateTime(run.finished ?? run.started, lang), run.actor);
  const mark = marks[phase.kind];

  return (
    <div role={!fresh ? undefined : phase.kind === "failed" ? "alert" : "status"} className="mt-[16px]">
      {!fresh && <div className={`mb-[6px] ${labelClass}`}>{t.lastLabel}</div>}
      <div className="flex items-start gap-[10px]">
        <span className={markBox}>
          <StatusMark status={mark} label={k.status[mark]} />
        </span>
        <div className="min-w-0 flex-1">
          <p className="m-0 font-medium">{headline}</p>
          <p className="m-0 mt-[2px] text-muted [overflow-wrap:anywhere]">{phase.kind === "same" ? by : `${(rollback ? t.runRollback : t.runUpdate)(run.from, to)} · ${by}`}</p>

          {phase.kind === "failed" && run.error && (
            <div className="mt-[10px] max-w-[720px]">
              <EvidenceBlock text={run.error} label={t.errorLabel} />
            </div>
          )}
          {run.job_id && (
            <p className="m-0 mt-[6px]">
              <Link to={`/jobs/${encodeURIComponent(run.job_id)}`}>{t.jobLink}</Link>
            </p>
          )}

          {fresh && phase.kind === "done" && (
            <>
              <p className="m-0 mt-[10px] max-w-[720px]">{t.reloadWhy}</p>
              <Button className="mt-[8px]" onClick={() => window.location.reload()}>
                {t.reload}
              </Button>
            </>
          )}
          {phase.kind === "done" && iso.data?.needs_harden && (
            <p className="m-0 mt-[10px] max-w-[720px]">
              {t.hardenNeeded} <Link to="/security#bm-co-lap">{t.hardenLink}</Link>
            </p>
          )}

          {phase.kind === "stuck" && (
            <>
              <p className="m-0 mt-[6px] max-w-[720px]">{t.stuckBody}</p>
              <div className={`mt-[10px] mb-[6px] ${labelClass}`}>{t.manual}</div>
              <CommandBlock commands={stuckCommands} />
            </>
          )}
        </div>
      </div>
    </div>
  );
}
