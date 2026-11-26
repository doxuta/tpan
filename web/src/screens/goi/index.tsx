// Màn Gói (M13, mục 6.12 của KE-HOACH.md), chỉ admin: bảng gói với giới hạn
// (0 hiện là "không giới hạn") và số user đang dùng; thêm, sửa, xoá. Gói
// "default" có sẵn, không xoá được. Giới hạn có hiệu lực ngay cho mọi user dùng gói.
// Bản 2: ScreenHeader (nút chính "Thêm gói") → một TableList; Sửa là nút chữ,
// Xoá nằm trong menu ba chấm.
//
// M17 (mục 6.15): gói có thêm cờ lồng và sáu mức tối đa cho mỗi site trong lồng
// (CPU, RAM, tiến trình, đọc, ghi, worker PHP). Sửa gói có site trong lồng dùng
// gói mà một mức đổi thì máy chủ phải áp mức mới cho các site đó bằng job
// cage.sync: đi qua bản xem trước (usePlanFlow) như mọi việc đổi máy. Các
// trường hợp còn lại giữ đường lưu thẳng như trước M17. Máy chủ là bên quyết
// định: lưu thẳng mà nhận 202 (dữ liệu đổi giữa chừng) thì coi như job.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { del, errorMessage, post } from "../../api/client";
import { useCages } from "../../api/m17";
import { limitRange, tasksSlack, type PackageEdit } from "../../api/m17-types";
import { packagePath, packagesKey, packagesPath, refreshAccounts, usePackages, usePlanFlow, type PlanRequest } from "../../api/m13-users";
import type { JobStarted, PackageRow } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Dialog } from "../../components/Dialog";
import { Checkbox, Field, Input, TextField } from "../../components/Field";
import { useJobTracker } from "../../components/JobTracker";
import { Notice } from "../../components/Notice";
import { RowActions } from "../../components/RowActions";
import { ScreenHeader } from "../../components/ScreenHeader";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/goi";
import { strings as khung } from "../../i18n/khung";
import { strings as chung } from "../../i18n/m13-chung";
import { PlanReview } from "../duyet-plan";
import { diskLimitText } from "../nguoi-dung/model";
import { ErrorNote } from "../site/ErrorNote";
import { limitKeys } from "../tai-nguyen/model";
import { cagedSitesOf, checkLimits, hasLimit, limitSummary, limitsLocked, limitsOf, needsPlan, parseLimit, parseLimits, type LimitForm } from "./model";

type Dlg = { kind: "add" } | { kind: "edit" | "delete"; row: PackageRow };
type Flow = ReturnType<typeof usePlanFlow>;
/** Điều bản xem trước đang nói tới: để câu hỏi trên nó nêu đúng gói và đúng site. */
type PlanInfo = { name: string; sites: string[] };

const rules = { range: limitRange, slack: tasksSlack };

export default function Packages() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = usePackages();
  const cages = useCages();
  const flow = usePlanFlow();
  const [dlg, setDlg] = useState<Dlg | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [plan, setPlan] = useState<PlanInfo | null>(null);
  const planRef = useRef<HTMLDivElement>(null);

  // Bản xem trước đã dựng xong thì hộp sửa gói nhường chỗ cho nó (nó nằm dưới bảng); lỗi dựng thì hộp ở lại để sửa tiếp.
  useEffect(() => {
    if (flow.open) {
      // Danh sách lồng trong bộ nhớ có thể cũ (site vừa ra khỏi lồng ở chỗ khác):
      // server không còn gì phải chạy thì lưu luôn, không để bản xem trước 0 bước
      // khoá nút Chạy (usePlanFlow nhận 200 PackageRow như một lần lưu thường).
      if (flow.open.preview.steps.every((s) => s.done)) {
        flow.start();
        setDlg(null);
        if (plan) setNotice(t.editedDone(plan.name));
        return;
      }
      setDlg(null);
      planRef.current?.scrollIntoView({ block: "start" });
      planRef.current?.focus();
    }
  }, [flow.open]);

  const limit = (n: number) => (n > 0 ? String(n) : <span className="text-muted">{t.unlimited}</span>);

  return (
    <>
      <ScreenHeader
        crumbs={[{ label: k.groups.taiKhoan }, { label: t.title }]}
        title={t.title}
        summary={t.summary}
        actions={
          <Button variant="primary" icon="plus" onClick={() => setDlg({ kind: "add" })}>
            {t.add}
          </Button>
        }
      />
      {notice && (
        <Card>
          <Notice kind="pass" role="status">
            {notice}
          </Notice>
        </Card>
      )}

      {q.isPending && (
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {q.isError && (
        <Card>
          <p className="mt-0 mb-[12px]">
            {t.loadFailed} {errorMessage(q.error, lang)}
          </p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {q.isSuccess && (
        <TableList
          label={t.tableName}
          caption={t.tableName}
          minWidth={860}
          rows={q.data}
          rowKey={(r) => r.package.name}
          empty={t.empty}
          columns={[
            {
              header: t.colName,
              nowrap: false,
              cell: (r) => (
                <span className="inline-flex flex-wrap items-baseline gap-x-[8px] gap-y-[4px]">
                  <span className="font-semibold [overflow-wrap:anywhere]">{r.package.name}</span>
                  {r.package.name === "default" && <Tag>{t.builtIn}</Tag>}
                </span>
              ),
            },
            { header: t.colSites, align: "right", cell: (r) => limit(r.package.max_sites) },
            { header: t.colDBs, align: "right", cell: (r) => limit(r.package.max_dbs) },
            { header: t.colDisk, align: "right", cell: (r) => diskLimitText(r.package.disk_mb, lang) ?? <span className="text-muted">{t.unlimited}</span> },
            { header: t.colBackups, align: "right", cell: (r) => limit(r.package.max_backups) },
            { header: t.colUsers, align: "right", cell: (r) => r.users },
            { header: t.colCage, cell: (r) => (r.package.cage ? t.cageYes : <span className="text-muted">{t.cageNo}</span>) },
            {
              header: t.colLimits,
              // v0.2.14: chữ được xuống dòng thay vì một dòng dài cắt ba chấm, để bảng vừa khung ở 1280 px.
              nowrap: false,
              className: "min-w-[200px]",
              cell: (r) => {
                if (!r.package.cage) return <span className="text-muted">{t.noLimits}</span>;
                return <span className="block max-w-[280px] text-[13px]">{limitSummary(r.package, t.sum)}</span>;
              },
            },
            {
              header: t.colAction,
              align: "right",
              cell: (r) => {
                const name = r.package.name;
                const more =
                  name === "default"
                    ? []
                    : [{ label: t.remove, danger: true, onSelect: () => setDlg({ kind: "delete", row: r }) }];
                return (
                  <RowActions
                    primary={[{ label: t.edit, ariaLabel: t.forPackage(t.edit, name), onClick: () => setDlg({ kind: "edit", row: r }) }]}
                    more={more}
                    moreLabel={t.moreFor(name, more.map((m) => m.label.toLowerCase()).join(", "))}
                  />
                );
              },
            },
          ]}
        />
      )}

      {(dlg?.kind === "add" || dlg?.kind === "edit") && (
        <PackageDialog
          row={dlg.kind === "edit" ? dlg.row : undefined}
          cagedSites={dlg.kind === "edit" ? cagedSitesOf(cages.data, dlg.row.package.name) : []}
          flow={flow}
          onPlan={(info, req) => {
            setPlan(info);
            flow.ask(req);
          }}
          onClose={() => {
            flow.close();
            setDlg(null);
          }}
          onDone={setNotice}
        />
      )}
      {dlg?.kind === "delete" && <DeleteDialog name={dlg.row.package.name} onClose={() => setDlg(null)} onDone={(n) => setNotice(t.deletedDone(n))} />}

      {flow.open && plan && (
        <div ref={planRef} tabIndex={-1} className="flex flex-col gap-[20px] outline-offset-0">
          <p className="m-0 max-w-[72ch] font-medium">{t.planAsk(plan.name, plan.sites.length, sitesText(plan.sites, t.moreSites))}</p>
          <PlanReview preview={flow.open.preview} onRun={flow.start} running={flow.run.isPending} error={flow.run.error} onCancel={flow.close} />
        </div>
      )}
    </>
  );
}

/** Tối đa ba tên miền, còn lại gộp thành "và N site khác". */
function sitesText(sites: string[], more: (n: number) => string): string {
  const shown = sites.slice(0, 3).join(", ");
  return sites.length > 3 ? shown + more(sites.length - 3) : shown;
}



type DialogProps = {
  row?: PackageRow;
  /** Site đang trong lồng và dùng gói này (chỉ khi sửa). */
  cagedSites: string[];
  flow: Flow;
  onPlan: (info: PlanInfo, req: PlanRequest) => void;
  onClose: () => void;
  onDone: (message: string) => void;
};

function PackageDialog({ row, cagedSites, flow, onPlan, onClose, onDone }: DialogProps) {
  const t = useStrings(strings);
  const m = useStrings(chung);
  const qc = useQueryClient();
  const { track } = useJobTracker();
  const edit = row !== undefined;
  const p = row?.package;
  const [name, setName] = useState(p?.name ?? "");
  const [sites, setSites] = useState(String(p?.max_sites ?? 0));
  const [dbs, setDbs] = useState(String(p?.max_dbs ?? 0));
  const [disk, setDisk] = useState(String(p?.disk_mb ?? 0));
  const [backups, setBackups] = useState(String(p?.max_backups ?? 0));
  const [cage, setCage] = useState(p?.cage ?? false);
  const [lim, setLim] = useState<LimitForm>(() => {
    const l = limitsOf(p);
    return { cpu_percent: String(l.cpu_percent), ram_mb: String(l.ram_mb), tasks: String(l.tasks), io_read_mb: String(l.io_read_mb), io_write_mb: String(l.io_write_mb), php_workers: String(l.php_workers) };
  });
  const [local, setLocal] = useState<string | null>(null);
  // Chỉ báo lỗi ô sau lần bấm đầu tiên: gõ dở một số không bị chê.
  const [tried, setTried] = useState(false);

  const save = useMutation({
    // Máy chủ trả 200 PackageRow, hoặc 202 JobStarted khi có site trong lồng cần áp mức mới.
    // Sửa: tên nằm trên đường dẫn, thân không mang "name" (PackageEdit). Tạo: thân là cả gói, có tên.
    mutationFn: ({ name, ...rest }: PackageEdit & { name: string }) =>
      post<PackageRow | JobStarted>(edit ? packagePath(name) : packagesPath, edit ? rest : { name, ...rest }),
    onSuccess: (r, body) => {
      refreshAccounts(qc);
      void qc.invalidateQueries({ queryKey: packagesKey });
      if ("job_id" in r) {
        track(r.job_id);
        onDone(t.editedJob(body.name));
      } else {
        const turnedOn = body.cage === true && p?.cage !== true;
        onDone(`${edit ? t.editedDone(body.name) : t.addedDone(body.name)}${turnedOn && edit ? ` ${t.cageOnDone}` : ""}`);
      }
      onClose();
    },
  });

  const errs = checkLimits(lim, cage, rules);
  const locked = limitsLocked(cage, lim);
  const dryBusy = flow.dry.isPending;

  const submit = () => {
    if (save.isPending || dryBusy) return;
    save.reset();
    setTried(true);
    const n = { max_sites: parseLimit(sites), max_dbs: parseLimit(dbs), disk_mb: parseLimit(disk), max_backups: parseLimit(backups) };
    if (!edit && name.trim() === "") return setLocal(t.nameMissing);
    if (Object.values(n).some((v) => v === null)) return setLocal(t.notNumber);
    setLocal(null);
    if (Object.keys(errs).length > 0) {
      // Đưa focus tới ô sai đầu tiên; lỗi cờ lồng thì tới ô chọn lồng.
      const first = limitKeys.find((key) => errs[key]);
      document.getElementById(first ? `g-${first}` : "g-long")?.focus();
      return;
    }
    const l = parseLimits(lim)!; // errs rỗng nghĩa là sáu ô đều là số
    const body: PackageEdit = { ...(n as Pick<PackageEdit, "max_sites" | "max_dbs" | "disk_mb" | "max_backups">), cage, ...l };
    const pname = edit ? p!.name : name.trim();
    if (edit && needsPlan(p, l, cagedSites.length)) {
      onPlan({ name: pname, sites: cagedSites }, { method: "POST", path: packagePath(pname), body });
      return;
    }
    save.mutate({ name: pname, ...body });
  };
  const onForm = (e: FormEvent) => {
    e.preventDefault();
    submit();
  };

  const num = (id: string, label: string, hint: string, v: string, set: (s: string) => void) => (
    <TextField id={id} type="number" min={0} step={1} inputMode="numeric" label={label} hint={hint} value={v} onChange={(e) => set(e.target.value)} />
  );

  const limitError = (key: (typeof limitKeys)[number]): string | undefined => {
    const e = errs[key];
    if (!tried || !e) return undefined;
    if (e.code === "notNumber") return t.errNotNumber;
    if (e.code === "range") return t.errRange(e.min, e.max);
    return t.errTasksLow(e.need, e.workers, e.slack);
  };
  const limitField = (key: (typeof limitKeys)[number]) => {
    const [min, max] = limitRange[key];
    const id = `g-${key}`;
    const hint = `${key === "php_workers" ? t.fRangeWorkers(min, max) : t.fRange(min, max)}${key === "tasks" ? ` ${t.fTasksExtra(tasksSlack)}` : ""}`;
    const error = limitError(key);
    return (
      <Field key={key} label={t.fLimit[key]} htmlFor={id} hint={hint} hintId={`${id}-goi-y`} error={error}>
        <Input
          id={id}
          type="number"
          min={0}
          step={1}
          inputMode="numeric"
          value={lim[key]}
          disabled={locked}
          title={locked ? t.fLocked : undefined}
          aria-invalid={error ? true : undefined}
          aria-describedby={`${id}-goi-y${locked ? " g-khoa" : ""}`}
          onChange={(e) => setLim({ ...lim, [key]: e.target.value })}
        />
      </Field>
    );
  };

  // Lời nhắc về gói đang bật lồng mà sắp tắt: site đang trong lồng không tự ra.
  const turningOff = edit && p?.cage === true && !cage && cagedSites.length > 0;
  const planned = edit && cagedSites.length > 0 && needsPlan(p, parseLimits(lim) ?? limitsOf(p), cagedSites.length);
  const needCage = !cage && hasLimit(lim);

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={edit ? t.editTitle(p!.name) : t.addTitle}
      confirmLabel={save.isPending ? t.saving : dryBusy ? m.previewing : planned ? t.editPreview : edit ? t.editConfirm : t.addConfirm}
      onConfirm={submit}
      busy={save.isPending || dryBusy}
    >
      <form onSubmit={onForm} className="flex flex-col gap-[14px]">
        {!edit && <TextField id="g-ten" label={t.fName} hint={t.fNameHint} autoComplete="off" autoCapitalize="none" spellCheck={false} autoFocus required value={name} onChange={(e) => setName(e.target.value)} />}
        {num("g-site", t.fSites, `${t.fSitesHint} ${t.fZeroHint}`, sites, setSites)}
        {num("g-db", t.fDBs, `${t.fDBsHint} ${t.fZeroHint}`, dbs, setDbs)}
        {num("g-dia", t.fDisk, `${t.fDiskHint} ${t.fZeroHint}`, disk, setDisk)}
        {num("g-backup", t.fBackups, t.fZeroHint, backups, setBackups)}

        <div className="border-t border-hair pt-[14px]">
          <label className="flex items-start gap-[10px] font-medium">
            <span className="pt-[2px]">
              <Checkbox id="g-long" label={t.fCage} checked={cage} onChange={(e) => setCage(e.target.checked)} aria-describedby="g-long-goi-y" />
            </span>
            {t.fCage}
          </label>
          <p id="g-long-goi-y" className="mt-[6px] mb-0 text-[13px] text-muted">
            {t.fCageHint}
          </p>
          {needCage && (
            <div className="mt-[10px]">
              <Notice kind={tried && errs.needCage ? "fail" : "warn"} role={tried && errs.needCage ? "alert" : undefined}>
                {t.needCageNote}
              </Notice>
            </div>
          )}
          {turningOff && (
            <div className="mt-[10px]">
              <Notice kind="warn">{t.cageOffSites(cagedSites.length)}</Notice>
            </div>
          )}
        </div>

        <fieldset className="m-0 min-w-0 border-0 p-0">
          <legend className="mb-[8px] p-0 text-[14px] font-medium">{t.fLimitsLegend}</legend>
          {locked && (
            <p id="g-khoa" className="mt-0 mb-[10px] text-[13px] text-muted">
              {t.fLocked}
            </p>
          )}
          <div className="grid grid-cols-1 gap-[14px] sm:grid-cols-2">{limitKeys.map(limitField)}</div>
        </fieldset>

        {planned && <p className="m-0 text-[13px] text-muted">{t.planNote(cagedSites.length)}</p>}
        {local && (
          <Notice kind="fail" role="alert">
            {local}
          </Notice>
        )}
        {save.isError && <ErrorNote error={save.error} />}
        {flow.dry.isError && <ErrorNote error={flow.dry.error} heading={t.planErrorHeading(p?.name ?? name)} />}
        {/* Nút gửi ẩn để bấm Enter trong ô nhập là gửi (nút thật của hộp nằm ngoài form). */}
        <button type="submit" tabIndex={-1} aria-hidden="true" className="sr-only" />
      </form>
    </Dialog>
  );
}

function DeleteDialog({ name, onClose, onDone }: { name: string; onClose: () => void; onDone: (name: string) => void }) {
  const t = useStrings(strings);
  const qc = useQueryClient();
  const remove = useMutation({
    mutationFn: () => del<void>(packagePath(name)),
    onSuccess: () => {
      refreshAccounts(qc);
      onDone(name);
      onClose();
    },
  });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={t.delTitle(name)} description={t.delDesc} confirmLabel={remove.isPending ? t.working : t.delConfirm} onConfirm={() => remove.mutate()} busy={remove.isPending} danger>
      {remove.isError && <ErrorNote error={remove.error} />}
    </Dialog>
  );
}
