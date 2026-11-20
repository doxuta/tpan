// Màn Site (mục 10 của KE-HOACH.md, mục 7 của docs/GIAO-DIEN.md):
//   /sites         danh sách site, bảng kẻ mảnh như "Lịch sử máy"
//   /sites/:domain một site: tuyến đường đi request, phép kiểm kèm bằng chứng,
//                  tên và đường dẫn, kiểm lại, đưa site vào thùng rác (qua bản
//                  xem trước; M6: DELETE không còn xoá hẳn, xoá hẳn ở màn Thùng rác).
//                  M14: "Request tới site này đi đường nào" (RouteExplain) và
//                  chế độ .htaccess khi web server là Apache (HtaccessBlock)

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { siteSuspendPath, useIdentity } from "../../api/m13-dbs";
import { ApiError, del, errorMessage, get } from "../../api/client";
import { queryKeys, useOverview } from "../../api/queries";
import type { JobStarted, Preview, SiteInfo, Stage } from "../../api/types";
import { Button, buttonClass } from "../../components/Button";
import { DomainText } from "../../components/DomainText";
import { MonoText } from "../../components/MonoText";
import { usePreviewedOp, type Op } from "../../components/PreviewedOp";
import { ScreenHeader } from "../../components/ScreenHeader";
import { StatusMark } from "../../components/StatusMark";
import { SuspendTag } from "../../components/SuspendTag";
import { TableList, type Column } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useStatusName } from "../../components/useStatusName";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings as chung } from "../../i18n/m13-chung";
import { strings } from "../../i18n/site";
import { PlanReview } from "../duyet-plan";
import { Backups } from "./Backups";
import { CheckItem } from "./CheckItem";
import { ErrorNote } from "./ErrorNote";
import { LiveLog } from "./LiveLog";
import { formatDate, formatTime } from "./format";
import { HtaccessBlock } from "./HtaccessBlock";
import { PhpBlock } from "./PhpBlock";
import { RouteExplain } from "./RouteExplain";
import { Legend, RouteMap, stageGroupId } from "./RouteMap";
import { checksOfStage, routeStages, stageOrder, worstMark } from "./route";
import { SuspendBlock } from "./SuspendBlock";
import { Traffic } from "./Traffic";

const sitePath = (domain: string) => `/api/v1/sites/${encodeURIComponent(domain)}`;
const siteKey = (domain: string) => ["site", domain] as const;

const linkClass = "font-mono text-[13px] text-ink no-underline hover:text-ink hover:underline";

/** /sites: danh sách site. */
export function SiteList() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const m = useStrings(chung);
  const statusName = useStatusName();
  const { lang } = useLang();
  const overview = useOverview();
  const { isAdmin } = useIdentity();
  // Tạm khoá và mở khoá từ bảng: xem trước rồi chạy, bản xem trước nằm dưới bảng.
  const op = usePreviewedOp([]);

  // Trạng thái chung của site lấy từ phép kiểm trong overview (cùng truy vấn với menu, không gọi thêm).
  const rows = (overview.data?.sites ?? [])
    .filter((r) => r.site.status === "active")
    .sort((a, b) => a.site.domain.localeCompare(b.site.domain));
  type Row = (typeof rows)[number];

  const suspendOp = (d: string, on: boolean): Op => ({
    id: `${on ? "suspend" : "unsuspend"}:${d}`,
    method: "POST",
    path: siteSuspendPath(d, on),
    warning: on ? t.suspendWarning(d) : t.unsuspendWarning(d),
    errorHeading: on ? t.suspendErrorHeading(d) : t.unsuspendErrorHeading(d),
  });

  // Cột Chủ và cột Việc (Tạm khoá, Mở khoá) chỉ admin thấy: máy chủ cũng từ chối user thường, ẩn ở đây để khỏi có nút bấm không được.
  const columns: Column<Row>[] = [
    {
      header: t.colDomain,
      className: "min-w-[160px]",
      cell: (r) => (
        <span className="flex flex-wrap items-baseline gap-x-[10px] gap-y-[4px]">
          <Link to={`/sites/${encodeURIComponent(r.site.domain)}`} className={linkClass}>
            <DomainText domain={r.site.domain} />
          </Link>
          {r.site.suspended && <SuspendTag how={r.site.suspended} />}
          {r.site.db_suspended && <SuspendTag how={r.site.db_suspended} label={m.dbLocked} />}
        </span>
      ),
    },
    ...(isAdmin ? [{ header: m.colOwner, className: "font-mono text-[13px]", cell: (r: Row) => r.site.owner } satisfies Column<Row>] : []),
    {
      header: t.colStatus,
      className: "w-[170px]",
      cell: (r) => {
        const mark = worstMark(r.checks, Boolean(r.site.suspended));
        const name = statusName(mark.status, mark.suspended);
        return (
          <Link
            to={`/sites/${encodeURIComponent(r.site.domain)}`}
            aria-label={t.statusLink(r.site.domain, name)}
            className="inline-flex items-center gap-[8px] text-ink no-underline hover:text-ink hover:underline"
          >
            <span className="flex h-[20px] w-[20px] items-center justify-center">
              <StatusMark status={mark.status} />
            </span>
            {name}
          </Link>
        );
      },
    },
    { header: t.colPhp, className: "w-[80px] font-mono text-[13px]", cell: (r) => r.site.php_version },
    { header: t.colCreated, className: "w-[130px] whitespace-nowrap tabular-nums", cell: (r) => formatDate(r.site.created, lang) },
    ...(isAdmin
      ? [
          {
            header: t.colAction,
            className: "pr-0",
            cell: (r: Row) => {
              const d = r.site.domain;
              const on = !r.site.suspended;
              return (
                <Button aria-label={on ? t.suspendFor(d) : t.unsuspendFor(d)} disabled={op.pending} onClick={() => void op.ask(suspendOp(d, on))}>
                  {op.busy(`${on ? "suspend" : "unsuspend"}:${d}`) ? m.previewing : on ? t.suspend : t.unsuspend}
                </Button>
              );
            },
          } satisfies Column<Row>,
        ]
      : []),
  ];

  return (
    <>
      <ScreenHeader title={t.listTitle} summary={t.listSummary} />
      <div className="flex flex-wrap items-end justify-between gap-x-[24px] gap-y-[10px] pt-[26px] pb-[14px]">
        <h2 className="m-0 text-[16px] font-semibold">{overview.data ? t.count(rows.length) : t.listCaption}</h2>
        <Link to="/sites/new" className={buttonClass("primary")}>
          {t.create}
        </Link>
      </div>
      {overview.isPending && <p className="m-0 text-muted">{k.loading}</p>}
      {overview.isError && (
        <div>
          <p className="mt-0 mb-[12px]">{errorMessage(overview.error, lang)}</p>
          <Button onClick={() => void overview.refetch()}>{k.retry}</Button>
        </div>
      )}
      {overview.data && (
        <TableList
          caption={t.listCaption}
          rows={rows}
          rowKey={(r) => r.site.domain}
          empty={
            <>
              {t.emptyLead}
              <Link to="/sites/new">{t.emptyLink}</Link>
            </>
          }
          columns={columns}
        />
      )}
      {op.panel}
    </>
  );
}

/** /sites/:domain: một site. */
export function SiteDetail() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const { domain = "" } = useParams();
  const q = useQuery({ queryKey: siteKey(domain), queryFn: () => get<SiteInfo>(sitePath(domain)) });
  const title = <span className="font-mono [overflow-wrap:anywhere]">{domain}</span>;

  if (q.isPending) {
    return (
      <>
        <ScreenHeader title={title} />
        <p className="mt-[16px] mb-0 text-muted">{k.loading}</p>
      </>
    );
  }
  if (q.isError) {
    const missing = q.error instanceof ApiError && q.error.code === "site_not_found";
    return (
      <>
        <ScreenHeader title={title} />
        <div className="pt-[16px]">
          <p className="mt-0 mb-[12px]">{missing ? t.siteMissing(domain) : errorMessage(q.error, lang)}</p>
          {missing ? (
            <Link to="/sites" className={buttonClass("secondary")}>
              {t.backToList}
            </Link>
          ) : (
            <Button onClick={() => void q.refetch()}>{k.retry}</Button>
          )}
        </div>
      </>
    );
  }
  return <SiteView key={domain} domain={domain} info={q.data} updatedAt={q.dataUpdatedAt} />;
}

function SiteView({ domain, info, updatedAt }: { domain: string; info: SiteInfo; updatedAt: number }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const m = useStrings(chung);
  const statusName = useStatusName();
  const { lang } = useLang();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { isAdmin } = useIdentity();
  const checks = info.checks;
  // M13: site hay user DB đang khoá. Việc cần pool PHP của site (đổi PHP, đưa vào thùng rác) bị máy chủ từ chối; nút vô hiệu kèm lý do, khỏi để người dùng bấm rồi mới nghe từ chối.
  const siteSuspended = Boolean(info.site.suspended);
  const dbSuspended = Boolean(info.site.db_suspended);
  const phpBlocked = siteSuspended ? t.blockedSite : dbSuspended ? t.blockedDb : undefined;
  const trashBlocked = siteSuspended ? t.blockedTrashSite : dbSuspended ? t.blockedTrashDb : undefined;

  // ---- ga và phép kiểm ----
  const [selected, setSelected] = useState<Stage | null>(null);
  // Phép kiểm lỗi mở sẵn: người vào trang một site hỏng cần thấy bằng chứng ngay.
  const [open, setOpen] = useState<Set<string>>(() => new Set(checks.filter((c) => c.status === "fail").map((c) => c.id)));

  const select = (stage: Stage) => {
    const ids = checksOfStage(checks, stage).map((c) => c.id);
    const close = selected === stage && ids.length > 0 && ids.every((id) => open.has(id));
    setSelected(stage);
    setOpen((prev) => {
      const next = new Set(prev);
      for (const id of ids) {
        if (close) next.delete(id);
        else next.add(id);
      }
      return next;
    });
    // Đưa người dùng (và trình đọc màn hình) tới nhóm phép kiểm của ga vừa bấm.
    (document.getElementById(stageGroupId(stage)) ?? document.getElementById("phep-kiem"))?.focus();
  };
  const toggle = (id: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  const groups = stageOrder.filter((s) => (routeStages as readonly Stage[]).includes(s) || s === "e2e" || checksOfStage(checks, s).length > 0);
  const noStack = info.web === "" && checks.length === 0;
  const hasLogs = info.web !== "" && info.site.status === "active";
  // Dòng "Hỏng ở ..." lấy ga hỏng đầu tiên; nếu sáu ga đều ổn mà phép gọi HTTP thật hỏng thì nói về nó.
  const firstFail = [...routeStages, "e2e" as const].map((s) => ({ s, c: checksOfStage(checks, s).find((c) => c.status === "fail") })).find((x) => x.c);

  // ---- kiểm lại ----
  const recheck = useMutation({
    mutationFn: () => get<SiteInfo>(`${sitePath(domain)}?refresh=1`),
    onSuccess: (data) => {
      qc.setQueryData(siteKey(domain), data);
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
    },
  });

  // ---- xoá site: xem trước (dry_run), duyệt, rồi chạy thật ----
  const [preview, setPreview] = useState<Preview | null>(null);
  const previewRef = useRef<HTMLDivElement>(null);
  const dry = useMutation({
    mutationFn: () => del<Preview>(`${sitePath(domain)}?dry_run=1`),
    onSuccess: setPreview,
  });
  const run = useMutation({
    mutationFn: () => del<JobStarted>(sitePath(domain)),
    onSuccess: (job) => {
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      void navigate(`/jobs/${encodeURIComponent(job.job_id)}`);
    },
  });
  useEffect(() => {
    if (preview) {
      previewRef.current?.scrollIntoView({ block: "start" });
      previewRef.current?.focus();
    }
  }, [preview]);

  const vhost = info.web === "nginx" ? info.layout.nginx_conf : info.web === "apache" ? info.layout.apache_conf : "";
  const l = info.layout;
  const layoutRows: Array<[string, string]> = [
    [t.layout.user, l.user],
    [t.layout.db, l.db],
    [t.layout.dir, l.dir],
    [t.layout.public, l.public],
    [t.layout.pool, l.pool],
    // Layout không còn trường socket từ M12 (socket mang số bản PHP); lấy từ SiteInfo.socket.
    [t.layout.socket, info.socket ?? ""],
    [t.layout.vhost, vhost],
    [t.layout.wpConfig, l.wp_config],
    [t.layout.logDir, l.log_dir],
    [t.layout.phpErrorLog, l.php_error_log],
  ];

  return (
    <>
      <ScreenHeader title={<span className="font-mono [overflow-wrap:anywhere]">{domain}</span>} summary={t.detailSummary} />
      <div className="flex flex-wrap gap-[6px] pt-[14px]">
        {info.web && <Tag>{info.web}</Tag>}
        <Tag>{k.php(info.site.php_version)}</Tag>
        {info.site.status === "trashed" && <Tag>{t.trashed}</Tag>}
        {info.site.suspended && <SuspendTag how={info.site.suspended} />}
        {info.site.db_suspended && <SuspendTag how={info.site.db_suspended} label={m.dbLocked} />}
        {isAdmin && <Tag>{m.ownerLabel(info.site.owner)}</Tag>}
      </div>

      <section aria-labelledby="tuyen">
        <div className="flex flex-wrap items-end justify-between gap-x-[24px] gap-y-[10px] pt-[26px] pb-[14px]">
          <h2 id="tuyen" className="m-0 text-[16px] font-semibold">
            {t.routeHeading}
          </h2>
          <Legend />
        </div>
        <RouteMap domain={domain} checks={checks} selected={selected} onSelect={select} suspended={siteSuspended} />
        {firstFail?.c && (
          <p className="mt-[10px] mb-0 text-[13px] font-medium text-fail-text">{t.failedAt(t.stages[firstFail.s], tx(firstFail.c.summary, lang))}</p>
        )}
        {/* Site tạm khoá: một dòng xám (không đỏ) nói các chặng xám là do khoá, không phải do hỏng. */}
        {siteSuspended && !firstFail?.c && <p className="mt-[10px] mb-0 text-[13px] text-muted">{t.suspendedLine}</p>}
        <div className="flex flex-wrap items-center gap-x-[14px] gap-y-[8px] pt-[12px]">
          <Button onClick={() => recheck.mutate()} disabled={recheck.isPending}>
            {recheck.isPending ? t.rechecking : t.recheck}
          </Button>
          <span aria-live="polite" className="text-[13px] text-muted">
            {t.updatedAt(formatTime(updatedAt, lang))}
          </span>
          <span className="text-[13px] text-muted">{t.routeHint}</span>
        </div>
        {recheck.isError && (
          <div className="pt-[12px]">
            <ErrorNote error={recheck.error} heading={t.recheckFailed} />
          </div>
        )}
        {/* M14: lần đường đi từ cấu hình đang chạy. Chưa cài stack hoặc site trong thùng rác thì máy chủ từ chối, nên không hiện nút. */}
        {hasLogs && <RouteExplain domain={domain} />}
      </section>

      {/* Log và số request đọc file log của site: chưa cài stack hoặc site trong thùng rác thì không có gì để đọc. */}
      {hasLogs && <Traffic domain={domain} />}

      <section aria-labelledby="phep-kiem">
        <h2 id="phep-kiem" tabIndex={-1} className="m-0 pt-[26px] pb-[6px] text-[16px] font-semibold">
          {t.checksHeading}
        </h2>
        {noStack ? (
          <p className="m-0 border-t border-hair pt-[10px] text-muted">{t.noStack}</p>
        ) : (
          groups.map((stage) => {
            const list = checksOfStage(checks, stage);
            const mark = worstMark(list, siteSuspended);
            return (
              <div key={stage} className="border-t border-hair pt-[12px]">
                <h3 id={stageGroupId(stage)} tabIndex={-1} className="m-0 flex items-center gap-[8px] font-mono text-[11px] font-normal tracking-[0.08em] text-muted uppercase">
                  <span className="flex h-[20px] w-[20px] items-center justify-center">
                    <StatusMark status={mark.status} />
                  </span>
                  {t.stages[stage]}
                  <span className="sr-only">: {statusName(mark.status, mark.suspended)}</span>
                </h3>
                {list.length === 0 ? (
                  <p className="mt-[6px] mb-[12px] pl-[28px] text-muted">{siteSuspended ? t.noChecksSuspended : t.noChecksInStage}</p>
                ) : (
                  <ul className="m-0 mt-[2px] list-none p-0">
                    {list.map((c) => (
                      <CheckItem key={c.id} check={c} open={open.has(c.id)} onToggle={() => toggle(c.id)} />
                    ))}
                  </ul>
                )}
              </div>
            );
          })
        )}
      </section>

      {hasLogs && <LiveLog domain={domain} />}

      {hasLogs && <PhpBlock domain={domain} version={info.site.php_version} socket={info.socket} blocked={phpBlocked} />}

      {/* M14: chế độ .htaccess chỉ có nghĩa trên Apache. Server cũ không gửi trường htaccess thì cũng chưa có API đổi chế độ: không hiện khối. */}
      {info.web === "apache" && info.site.status === "active" && info.site.htaccess && (
        <HtaccessBlock domain={domain} mode={info.site.htaccess} blocked={siteSuspended ? t.htBlockedSite : undefined} />
      )}

      <Backups domain={domain} active={info.site.status === "active"} restoreBlocked={siteSuspended ? t.blockedSite : undefined} />

      {info.site.status === "active" && <SuspendBlock site={info.site} admin={isAdmin} />}

      <section aria-labelledby="ten-duong-dan">
        <div className="flex flex-wrap items-end justify-between gap-x-[24px] gap-y-[6px] pt-[26px] pb-[10px]">
          <h2 id="ten-duong-dan" className="m-0 text-[16px] font-semibold">
            {t.layoutHeading}
          </h2>
          <span className="text-[13px] text-muted">{t.layoutHint}</span>
        </div>
        <dl className="m-0">
          {layoutRows.map(([label, value]) => (
            <div key={label} className="flex flex-wrap gap-x-[16px] border-t border-hair py-[8px]">
              <dt className="flex-[0_0_220px] text-muted">{label}</dt>
              <dd className="m-0 min-w-0 flex-[1_1_240px]">{value ? <MonoText>{value}</MonoText> : <span className="text-muted">{t.vhostNoStack}</span>}</dd>
            </div>
          ))}
        </dl>
      </section>

      {info.site.status === "trashed" ? (
        // Site đã trong thùng rác: không còn gì để "đưa vào" nữa, việc tiếp theo ở màn Thùng rác.
        <section aria-labelledby="xoa-site">
          <h2 id="xoa-site" className="m-0 pt-[26px] pb-[6px] text-[16px] font-semibold">
            {t.trashedHeading}
          </h2>
          <p className="m-0 text-muted">{t.trashedHint}</p>
          <div className="pt-[12px]">
            <Link to="/trash" className={buttonClass("primary")}>
              {t.trashedLink}
            </Link>
          </div>
        </section>
      ) : (
        <section aria-labelledby="xoa-site">
          <h2 id="xoa-site" className="m-0 pt-[26px] pb-[6px] text-[16px] font-semibold">
            {t.deleteHeading}
          </h2>
          <p className="m-0 text-muted">
            {t.deleteHintLead}
            <Link to="/trash">{t.trashLink}</Link>
            {t.deleteHintTail}
          </p>
          <div className="pt-[12px]">
            <Button onClick={() => dry.mutate()} disabled={dry.isPending || run.isPending || trashBlocked !== undefined} aria-describedby={trashBlocked ? "xoa-bi-chan" : undefined}>
              {dry.isPending ? t.deletePreviewing : t.delete}
            </Button>
          </div>
          {trashBlocked && (
            <p id="xoa-bi-chan" className="mt-[10px] mb-0 max-w-[720px] text-[13px] text-muted">
              {trashBlocked}
            </p>
          )}
          {dry.isError && (
            <div className="pt-[12px]">
              <ErrorNote error={dry.error} heading={t.deleteFailed} />
            </div>
          )}
          {preview && (
            <div ref={previewRef} tabIndex={-1} className="outline-offset-0">
              {/* Câu hỏi lại của việc này: đưa vào thùng rác không mất dữ liệu, nói rõ để người dùng bấm chạy mà yên tâm. */}
              <p className="m-0 pt-[26px] font-medium">{t.deleteAsk(domain)}</p>
              <PlanReview preview={preview} onRun={() => run.mutate()} running={run.isPending} error={run.error} onCancel={() => {
                  setPreview(null);
                  run.reset();
                }} />
            </div>
          )}
        </section>
      )}
    </>
  );
}
