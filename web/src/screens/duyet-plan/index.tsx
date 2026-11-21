// Bản xem trước của một Plan, trình bày như một pull request (mục 7 của
// docs/GIAO-DIEN.md, bản 2): tóm tắt, phạm vi ảnh hưởng, từng bước với lệnh sẽ
// chạy và file thay đổi dạng diff, rồi nút chạy.
// Không có route riêng: màn Tạo site và thao tác Xoá site nhúng component này
// với Preview lấy từ ?dry_run=1.

import { useId, useMemo, useState, type ReactNode } from "react";
import { errorMessage } from "../../api/client";
import type { FileChange, Preview, StepPreview } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Dialog } from "../../components/Dialog";
import { Disclosure } from "../../components/Disclosure";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { Notice } from "../../components/Notice";
import { Tag } from "../../components/Tag";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/duyet-plan";
import { useTips } from "../../tips";
import { diffFile } from "./diff";

// Chữ ký đã chốt cho đợt 2 (màn Tạo site và Site dùng song song với màn này):
export type PlanReviewProps = {
  preview: Preview;
  /** Gửi lệnh chạy thật (POST/DELETE không có dry_run). Màn gọi lo chuyển sang /jobs/:id. */
  onRun: () => void;
  /** Đang gửi lệnh chạy: nút chạy bị khoá. */
  running?: boolean;
  /** Lỗi của lần gửi lệnh chạy (ApiError hoặc lỗi khác), hiện ngay dưới nút. */
  error?: unknown;
  /** Quay lại bước nhập. Không có thì không hiện nút. */
  onCancel?: () => void;
};

const labelClass = "text-[12px] font-medium text-muted";

export function PlanReview({ preview, onRun, running = false, error, onCancel }: PlanReviewProps) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const { tips } = useTips();
  const [confirming, setConfirming] = useState(false);
  // Trang site có thể mở hai bản xem trước cùng lúc (Tạm khoá và Đưa vào thùng rác): id phải riêng từng bản.
  const headingId = useId();

  const toRun = preview.steps.filter((s) => !s.done);
  const skipped = preview.steps.length - toRun.length;
  const dangerous = toRun.filter((s) => s.info.danger);
  const fileCount = toRun.reduce((n, s) => n + (s.info.files?.length ?? 0), 0);
  const reloads = reloadServices(toRun);

  // Có bước nguy hiểm thì hỏi lại một lần ("Chạy 11 bước này?"); không thì chạy luôn.
  const press = () => (dangerous.length > 0 ? setConfirming(true) : onRun());

  return (
    // Không tự thêm khoảng trên: màn đặt khối này trong cột flex cách nhau 20 px (mục 3.5).
    <Card title={`${t.heading}: ${tx(preview.title, lang)}`} titleId={headingId}>
      <p className="m-0 text-muted">{t.summary}</p>

      <Section label={t.summaryLabel}>
        <dl className="m-0 grid grid-cols-2 gap-x-[16px] gap-y-[12px] sm:flex sm:flex-wrap sm:gap-x-[40px]">
          <Metric label={t.stepsToRun} value={t.stepsUnit(toRun.length)} />
          <Metric label={t.stepsToSkip} value={t.stepsUnit(skipped)} sub={skipped > 0 ? t.skipNote : undefined} />
          <Metric label={t.filesChanged} value={t.filesUnit(fileCount)} />
          {preview.target && (
            <div className="col-span-2 min-w-0 sm:col-span-1">
              <dt className={labelClass}>{t.targetLabel}</dt>
              {/* Đối tượng thường là một tên miền: sans, không mono (mục 4 của GIAO-DIEN.md). */}
              <dd className="m-0 mt-[8px] text-[14px] font-medium [overflow-wrap:anywhere]">{preview.target}</dd>
            </div>
          )}
        </dl>
      </Section>

      <Section label={t.scopeLabel}>
        <div className="flex flex-wrap items-center gap-x-[8px] gap-y-[6px]">
          {reloads.length > 0 ? (
            <>
              <span>{t.scopeReload}</span>
              {reloads.map((u) => (
                <Tag key={u}>{u}</Tag>
              ))}
            </>
          ) : (
            <span>{t.scopeNoReload}</span>
          )}
        </div>
        <p className="mt-[6px] mb-0">{dangerous.length > 0 ? t.scopeDanger(dangerous.length) : t.scopeNoDanger}</p>
      </Section>

      <Section label={t.stepsLabel}>
        <ol className="m-0 list-none rounded-ctl border border-hair px-[12px] py-0">
          {preview.steps.map((s, i) => (
            <StepItem key={`${i}-${s.info.id}`} n={i + 1} step={s} tips={tips} />
          ))}
        </ol>
      </Section>

      <div className="mt-[18px] border-t border-hair pt-[16px]">
        <div className="flex flex-wrap gap-[8px]">
          <Button variant="primary" data-plan-run onClick={press} disabled={running || toRun.length === 0}>
            {running ? t.running : t.run(toRun.length)}
          </Button>
          {onCancel && (
            <Button onClick={onCancel} disabled={running}>
              {t.back}
            </Button>
          )}
        </div>
        {toRun.length === 0 && <p className="mt-[10px] mb-0 text-muted">{t.nothingToRun}</p>}
        {error != null && (
          <div className="mt-[10px]">
            <Notice kind="fail" role="alert">
              {errorMessage(error, lang)}
            </Notice>
          </div>
        )}
      </div>

      <Dialog
        open={confirming}
        onOpenChange={setConfirming}
        title={t.confirmTitle(toRun.length)}
        description={t.confirmBody(dangerous.length)}
        confirmLabel={t.confirmRun(toRun.length)}
        busy={running}
        onConfirm={() => {
          setConfirming(false);
          onRun();
        }}
      >
        <ul className="m-0 list-none p-0">
          {dangerous.map((s) => (
            <li key={s.info.id} className="border-t border-hair py-[6px] first:border-t-0">
              {tx(s.info.title, lang)}
            </li>
          ))}
        </ul>
      </Dialog>
    </Card>
  );
}

function Section({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="mt-[16px] border-t border-hair pt-[14px]">
      <h3 className={`m-0 ${labelClass}`}>{label}</h3>
      <div className="mt-[8px]">{children}</div>
    </div>
  );
}

function Metric({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div>
      <dt className={labelClass}>{label}</dt>
      <dd className="m-0 mt-[4px] text-[24px] leading-[1.2] font-medium tabular-nums">{value}</dd>
      {sub && <dd className="m-0 text-[12px] text-muted">{sub}</dd>}
    </div>
  );
}

/**
 * Một bước. Dòng tóm tắt (số bước, tên, nhãn nguy hiểm, lời cảnh báo) luôn thấy;
 * lệnh sẽ chạy và file thay đổi nằm trong khối mở ra, **mở sẵn** như bản 1: bản
 * xem trước đọc như một pull request (mục 10 của KE-HOACH), người duyệt phải thấy
 * lệnh và diff mà không bấm từng bước. Gập lại được khi đã đọc xong.
 * Bước đã đúng sẽ bị bỏ qua: không có lệnh hay file nào chạy, nên chỉ là một dòng.
 */
function StepItem({ n, step, tips }: { n: number; step: StepPreview; tips: boolean }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const { info, done } = step;
  const files = info.files ?? [];
  const commands = info.commands ?? [];
  const why = tx(info.why, lang);
  const noUndo = tx(info.no_undo, lang);
  const muted = done ? "text-muted" : "";

  const head = (
    <span className="block min-w-0">
      <span className="flex flex-wrap items-baseline gap-x-[12px] gap-y-[4px]">
        <span className="text-[13px] font-normal text-muted">{t.stepNumber(n)}</span>
        {/* overflow-wrap: tên bước hay chứa đường dẫn dài không có chỗ ngắt, ở 390 px chữ tràn khỏi hàng. */}
        <span className={`min-w-0 flex-[1_1_240px] text-[14px] font-semibold [overflow-wrap:anywhere] ${muted}`}>{tx(info.title, lang)}</span>
        {info.danger && !done && <Tag>{t.danger}</Tag>}
        {done && <span className="text-[13px] font-normal text-muted">{t.willSkip}</span>}
      </span>
      {tips && why && <span className="mt-[4px] block text-[13px] font-normal text-muted">{why}</span>}
      {!done && noUndo && <span className="mt-[4px] block text-[13px] font-normal text-muted">{t.noUndo(noUndo)}</span>}
      {!done && step.check_error && <span className="mt-[4px] block text-[13px] font-normal text-muted">{t.checkError(step.check_error)}</span>}
    </span>
  );

  if (done || (commands.length === 0 && files.length === 0)) {
    return <li className="border-t border-hair py-[10px] first:border-t-0">{head}</li>;
  }
  return (
    <li className="border-t border-hair first:border-t-0">
      <Disclosure flush defaultOpen heading={4} summary={head}>
        <div className="flex flex-col gap-[12px]">
          {commands.length > 0 && <EvidenceBlock text={commands.map((c) => `$ ${c}`).join("\n")} label={t.commandsLabel} />}
          {files.length > 0 && (
            <div>
              <div className={`mb-[6px] ${labelClass}`}>{t.filesLabel}</div>
              {files.map((f) => (
                <FileDiffView key={f.path} file={f} />
              ))}
            </div>
          )}
        </div>
      </Disclosure>
    </li>
  );
}

/**
 * Một file thay đổi dạng diff. Chỉ đen trắng: dòng thêm có dấu "+" và đậm, dòng
 * bớt có dấu "−" và màu --muted, nên đọc được cả khi in đen trắng. File dài thì
 * gập lại, bấm tiêu đề để mở (<details> có sẵn bàn phím).
 */
function FileDiffView({ file }: { file: FileChange }) {
  const t = useStrings(strings);
  const d = useMemo(() => diffFile(file), [file]);
  const kind = file.delete ? t.fileDelete : !file.old_exists ? t.fileNew : t.fileEdit;
  const size = d.binary ? 0 : d.added + d.removed;
  // File mới toàn dòng thêm: đậm cả file thì khó đọc, dấu "+" đã đủ. Đậm chỉ để dòng thêm nổi lên giữa dòng giữ nguyên.
  const boldAdds = !d.binary && d.lines.some((l) => l.kind !== "add");
  let body: ReactNode;
  if (d.binary) body = <Note>{t.fileBinary}</Note>;
  else if (!d.changed) body = <Note>{file.old_exists ? t.fileUnchanged : t.fileEmptyNew}</Note>;
  else
    body = (
      <div role="group" aria-label={t.diffOf(file.path)} className="bg-pre py-[8px] font-mono text-[12.5px] leading-[1.55]">
        {d.lines.map((l, i) =>
          l.kind === "gap" ? (
            <div key={i} className="px-[12px] text-muted">
              {t.gap(l.count)}
            </div>
          ) : (
            <div key={i} className={`flex gap-[8px] px-[12px] ${l.kind === "add" && boldAdds ? "font-semibold" : l.kind === "del" ? "text-muted" : ""}`}>
              <span className="w-[1ch] flex-none">{l.kind === "add" ? "+" : l.kind === "del" ? "−" : " "}</span>
              <span className="min-w-0 whitespace-pre-wrap [overflow-wrap:anywhere]">{l.text || " "}</span>
            </div>
          ),
        )}
      </div>
    );
  return (
    <details open={!d.binary && d.changed && size <= 80} className="mt-[8px] overflow-hidden rounded-ctl border border-hair first:mt-0">
      {/* Khung cha cắt tràn (overflow-hidden) nên viền focus vẽ vào trong dòng tiêu đề. */}
      <summary className="cursor-pointer bg-soft px-[12px] py-[8px] focus-visible:-outline-offset-2">
        <span className="mr-[12px] font-mono text-[13px] [overflow-wrap:anywhere]">{file.path}</span>
        <span className="mr-[12px] text-[12px] text-muted">{kind}</span>
        {!d.binary && d.changed && <span className="font-mono text-[12px] text-muted">{t.diffStat(d.added, d.removed)}</span>}
      </summary>
      {body}
    </details>
  );
}

function Note({ children }: { children: ReactNode }) {
  return <p className="m-0 px-[12px] py-[8px] text-muted">{children}</p>;
}

/** Dịch vụ sẽ reload, suy từ lệnh "systemctl reload <unit>" của các bước sẽ chạy. */
function reloadServices(steps: StepPreview[]): string[] {
  const units = new Set<string>();
  for (const s of steps) {
    for (const c of s.info.commands ?? []) {
      const tok = c.split(/\s+/);
      const unit = tok[tok.indexOf("reload") + 1]?.replace(/^'|'$/g, "");
      if (tok.includes("reload") && unit && !unit.startsWith("-")) units.add(unit);
    }
  }
  return [...units];
}
