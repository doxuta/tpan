// Màn Site (mục 10 của KE-HOACH.md, mục 7 của docs/GIAO-DIEN.md; giao diện bản 2,
// mục 3.7 của docs/thay-doi/M16-thiet-ke.md):
//   /sites         danh sách site: một TableList (tự vẽ thẻ), tên miền là liên kết,
//                  cột Việc (admin) là một nút Tạm khoá hoặc Mở khoá
//   /sites/:domain một site, chia sáu tab (Radix Tabs; tab đang mở nằm trong ?tab=):
//                  Tổng quan (đường đi request, request một giờ qua, sự cố của site),
//                  Phép kiểm (theo chặng; phép cô lập gộp một khối), Log, PHP, Backup,
//                  Cài đặt site (.htaccess, tạm khoá, tên và đường dẫn, thùng rác).
//                  M19: thêm hai tab Tên miền (tên phụ) và Chuyển hướng (luật theo đường dẫn),
//                  chỉ hiện khi site có web server như tab Log và PHP.
//                  Đưa site vào thùng rác đi qua bản xem trước (M6: DELETE không còn xoá
//                  hẳn, xoá hẳn ở màn Thùng rác). M14: "Request tới site này đi đường
//                  nào" (RouteExplain) và chế độ .htaccess khi web server là Apache.
//
// Nội dung của tab không mở bị gỡ khỏi trang, nên mọi thứ phải sống qua lần đổi tab
// (ga đang chọn, phép kiểm nào đang mở, khối cô lập, bản xem trước xoá) nằm ở SiteView.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router";
import { siteSuspendPath, useIdentity } from "../../api/m13-dbs";
import { useJobTracker } from "../../components/JobTracker";
import { ApiError, del, errorMessage, get } from "../../api/client";
import { queryKeys, useOverview } from "../../api/queries";
import type { CheckResult, JobStarted, Preview, SiteInfo, Stage } from "../../api/types";
import { Button, buttonClass } from "../../components/Button";
import { BulkBar, SelectAll, SelectOne, useSelection } from "../../components/Bulk";
import { Card } from "../../components/Card";
import { Disclosure } from "../../components/Disclosure";
import { DomainText } from "../../components/DomainText";
import { EmptyState } from "../../components/EmptyState";
import { Icon } from "../../components/Icon";
import { Code, MonoText } from "../../components/MonoText";
import { usePreviewedOp, type Op } from "../../components/PreviewedOp";
import { RowActions } from "../../components/RowActions";
import { ScreenHeader } from "../../components/ScreenHeader";
import { StatusLine, StatusMark } from "../../components/StatusMark";
import { SuspendTag } from "../../components/SuspendTag";
import { TableList, type Column } from "../../components/TableList";
import { Tabs, TabPanel } from "../../components/Tabs";
import { Tag } from "../../components/Tag";
import { useStatusName } from "../../components/useStatusName";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings as chung } from "../../i18n/m13-chung";
import { strings } from "../../i18n/site";
import { PlanReview } from "../duyet-plan";
import { formatWhen } from "../thung-rac/model";
import { Backups } from "./Backups";
import { CageBlock } from "./CageBlock";
import { CheckItem } from "./CheckItem";
import { ErrorNote } from "./ErrorNote";
import { formatDate, formatTime } from "./format";
import { HtaccessBlock } from "./HtaccessBlock";
import { groupIsolation, touchesSite, type IsolationGroup } from "./isolation";
import { LiveLog } from "./LiveLog";
import { Names } from "./Names";
import { PhpBlock } from "./PhpBlock";
import { Redirects } from "./Redirects";
import { RouteExplain } from "./RouteExplain";
import { Legend, RouteMap, stageGroupId } from "./RouteMap";
import { checksOfStage, routeStages, stageOrder, worstMark } from "./route";
import { OwnerBlock } from "./OwnerBlock";
import { SuspendBlock } from "./SuspendBlock";
import { Traffic } from "./Traffic";

const sitePath = (domain: string) => `/api/v1/sites/${encodeURIComponent(domain)}`;
const siteKey = (domain: string) => ["site", domain] as const;

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
  const sel = useSelection(rows.map((r) => r.site.domain));

  const columns: Column<Row>[] = [
    {
      header: "chon",
      headerCell: <SelectAll sel={sel} label={t.selectAll} />,
      className: "w-[34px]",
      cell: (r) => <SelectOne sel={sel} id={r.site.domain} label={t.selectOne(r.site.domain)} />,
    },
    {
      header: t.colDomain,
      className: "min-w-[200px]",
      // Tên miền dài xuống dòng ngay sau dấu chấm (DomainText), nên bảng không bị cắt chữ.
      nowrap: false,
      cell: (r) => (
        <span className="flex flex-wrap items-center gap-x-[10px] gap-y-[4px]">
          <Link to={`/sites/${encodeURIComponent(r.site.domain)}`} className="font-medium no-underline hover:underline">
            <DomainText domain={r.site.domain} />
          </Link>
          {r.site.suspended && <SuspendTag how={r.site.suspended} />}
          {r.site.db_suspended && <SuspendTag how={r.site.db_suspended} label={m.dbLocked} />}
        </span>
      ),
    },
    ...(isAdmin ? [{ header: m.colOwner, cell: (r: Row) => r.site.owner } satisfies Column<Row>] : []),
    {
      header: t.colStatus,
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
    { header: t.colPhp, cell: (r) => <Code>{r.site.php_version}</Code> },
    { header: t.colCreated, className: "tabular-nums", cell: (r) => formatDate(r.site.created, lang) },
    ...(isAdmin
      ? [
          {
            header: t.colAction,
            align: "right",
            cell: (r: Row) => {
              const d = r.site.domain;
              const on = !r.site.suspended;
              const label = on ? t.suspendFor(d) : t.unsuspendFor(d);
              return (
                <RowActions
                  moreLabel={label}
                  primary={[
                    {
                      label: on ? t.suspend : t.unsuspend,
                      ariaLabel: label,
                      disabled: op.pending,
                      busy: op.busy(`${on ? "suspend" : "unsuspend"}:${d}`),
                      busyLabel: m.previewing,
                      onClick: () => void op.ask(suspendOp(d, on)),
                    },
                  ]}
                />
              );
            },
          } satisfies Column<Row>,
        ]
      : []),
  ];

  return (
    <>
      <ScreenHeader
        title={t.listTitle}
        summary={t.listSummary}
        actions={
          <Link to="/sites/new" className={buttonClass("primary", "pl-[12px]")}>
            <Icon name="plus" size={16} />
            {t.create}
          </Link>
        }
      />
      {overview.isPending && (
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {overview.isError && (
        <Card>
          <p className="mt-0 mb-[12px]">{errorMessage(overview.error, lang)}</p>
          <Button onClick={() => void overview.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {overview.data && (
        <TableList
          label={t.listCaption}
          caption={t.listCaption}
          minWidth={820}
          rows={rows}
          rowKey={(r) => r.site.domain}
          empty={
            <>
              {t.emptyLead}
              <Link to="/sites/new">{t.emptyLink}</Link>
            </>
          }
          columns={columns}
          above={
            <BulkBar
              sel={sel}
              unit={t.unitSite}
              actions={[
                {
                  key: "trash",
                  label: t.bulkTrash,
                  confirmTitle: t.bulkTrashTitle,
                  confirmBody: <p className="m-0">{t.bulkTrashBody}</p>,
                  confirmLabel: t.bulkTrash,
                  run: (d) => del<JobStarted>(sitePath(d)),
                },
              ]}
            />
          }
          footer={rows.length > 0 ? <span>{t.count(rows.length)}</span> : undefined}
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
  const crumbs = [{ label: t.listTitle, to: "/sites" }, { label: domain }];

  if (q.isPending) {
    return (
      <>
        <ScreenHeader crumbs={crumbs} title={domain} />
        <Card>
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      </>
    );
  }
  if (q.isError) {
    const missing = q.error instanceof ApiError && q.error.code === "site_not_found";
    return (
      <>
        <ScreenHeader crumbs={crumbs} title={domain} />
        <Card>
          <p className="mt-0 mb-[12px]">{missing ? t.siteMissing(domain) : errorMessage(q.error, lang)}</p>
          {missing ? (
            <Link to="/sites" className={buttonClass("secondary")}>
              {t.backToList}
            </Link>
          ) : (
            <Button onClick={() => void q.refetch()}>{k.retry}</Button>
          )}
        </Card>
      </>
    );
  }
  return <SiteView key={domain} domain={domain} info={q.data} updatedAt={q.dataUpdatedAt} />;
}

type TabId = "tong-quan" | "phep-kiem" | "log" | "php" | "ten-mien" | "chuyen-huong" | "backup" | "cai-dat";

/** Nhãn "tạm khoá" đặt thẳng trên nền trang (meta của ScreenHeader): cùng ruột với SuspendTag nhưng nền Tag là --surface. */
function SiteView({ domain, info, updatedAt }: { domain: string; info: SiteInfo; updatedAt: number }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const m = useStrings(chung);
  const statusName = useStatusName();
  const { lang } = useLang();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { isAdmin } = useIdentity();
  const { track } = useJobTracker();
  const checks = info.checks;
  // M13: site hay user DB đang khoá. Việc cần pool PHP của site (đổi PHP, đưa vào thùng rác) bị máy chủ từ chối; nút vô hiệu kèm lý do, khỏi để người dùng bấm rồi mới nghe từ chối.
  const siteSuspended = Boolean(info.site.suspended);
  const dbSuspended = Boolean(info.site.db_suspended);
  const phpBlocked = siteSuspended ? t.blockedSite : dbSuspended ? t.blockedDb : undefined;
  const trashBlocked = siteSuspended ? t.blockedTrashSite : dbSuspended ? t.blockedTrashDb : undefined;

  const noStack = info.web === "" && checks.length === 0;
  const hasLogs = info.web !== "" && info.site.status === "active";

  // ---- tab: nằm trong ?tab=, giá trị lạ hay tab đang ẩn thì coi như Tổng quan ----
  const [params, setParams] = useSearchParams();
  const visible: TabId[] = hasLogs ? ["tong-quan", "phep-kiem", "log", "php", "ten-mien", "chuyen-huong", "backup", "cai-dat"] : ["tong-quan", "phep-kiem", "backup", "cai-dat"];
  const raw = params.get("tab");
  const tab: TabId = visible.find((v) => v === raw) ?? "tong-quan";
  const setTab = (v: TabId) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (v === "tong-quan") next.delete("tab");
        else next.set("tab", v);
        return next;
      },
      { replace: true },
    );

  // ---- ga và phép kiểm ----
  // Phép cô lập gộp thành một khối (isolation.ts); các phép đã gộp không in lại ở danh sách thường của chặng.
  const iso = groupIsolation(checksOfStage(checks, "wordpress"), domain);
  const isoIds = new Set<string>();
  for (const p of iso?.pairs ?? []) {
    if (p.out) isoIds.add(p.out.id);
    if (p.in) isoIds.add(p.in.id);
  }
  const plainOf = (stage: Stage) => checksOfStage(checks, stage).filter((c) => !isoIds.has(c.id));

  const [selected, setSelected] = useState<Stage | null>(null);
  // Phép kiểm lỗi mở sẵn: người vào trang một site hỏng cần thấy bằng chứng ngay. Gồm cả phép cô lập lỗi.
  const [open, setOpen] = useState<Set<string>>(() => new Set(checks.filter((c) => c.status === "fail").map((c) => c.id)));
  // Khối cô lập tự mở khi có phép lỗi (openIds khác rỗng).
  const [isoOpen, setIsoOpen] = useState(() => iso !== null && iso.openIds.length > 0);
  // Ga cần đưa focus tới: nhóm phép kiểm chỉ có trong trang sau khi sang tab Phép kiểm, nên focus nằm trong effect.
  const [focusStage, setFocusStage] = useState<Stage | null>(null);
  useEffect(() => {
    if (focusStage === null || tab !== "phep-kiem") return;
    (document.getElementById(stageGroupId(focusStage)) ?? document.getElementById("phep-kiem"))?.focus();
    setFocusStage(null);
  }, [focusStage, tab]);

  const select = (stage: Stage) => {
    // Bấm ga là mở các phép kiểm của chặng. Bản 1 bấm lần hai thì đóng lại; giờ bấm ga là sang tab khác nên lần hai không còn nghĩa đó.
    setSelected(stage);
    setOpen((prev) => {
      const next = new Set(prev);
      for (const c of plainOf(stage)) next.add(c.id);
      return next;
    });
    // Chặng WordPress: mở cả khối cô lập (chỉ phép lỗi trong đó tự mở), không mở hàng chục phép cô lập một lượt.
    if (stage === "wordpress" && iso !== null) setIsoOpen(true);
    // Bấm ga là sang tab Phép kiểm, rồi đưa người dùng (và trình đọc màn hình) tới nhóm của ga vừa bấm.
    setTab("phep-kiem");
    setFocusStage(stage);
  };
  const toggle = (id: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  const groups = stageOrder.filter((s) => (routeStages as readonly Stage[]).includes(s) || s === "e2e" || checksOfStage(checks, s).length > 0);
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
      // Ở lại trang đang dùng; thẻ theo dõi ở góc dưới báo tiến độ và tải lại dữ liệu khi job xong.
      track(job.job_id);
      void navigate("/sites");
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
      <ScreenHeader
        crumbs={[{ label: t.listTitle, to: "/sites" }, { label: domain }]}
        title={domain}
        summary={t.detailSummary}
        meta={
          <>
            {info.web && <Tag tone="surface">{info.web}</Tag>}
            <Tag tone="surface">{k.php(info.site.php_version)}</Tag>
            {isAdmin && <Tag tone="surface">{m.ownerLabel(info.site.owner)}</Tag>}
            {info.site.suspended && <SuspendTag how={info.site.suspended} tone="surface" />}
            {info.site.db_suspended && <SuspendTag how={info.site.db_suspended} label={m.dbLocked} tone="surface" />}
            {info.site.status === "trashed" && <Tag tone="surface">{t.trashed}</Tag>}
          </>
        }
      />

      <Tabs label={t.tabsLabel} value={tab} onValueChange={setTab} tabs={visible.map((v) => ({ value: v, label: t.tabs[v] }))}>
        <TabPanel value="tong-quan">
          <Card title={t.routeHeading} titleId="tuyen" actions={<Legend />}>
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
          </Card>

          {/* Số request đọc file log của site: chưa cài stack hoặc site trong thùng rác thì không có gì để đọc. */}
          {hasLogs && <Traffic domain={domain} />}

          <SiteIncidents domain={domain} />

          {/* M17: thẻ Lồng. Cần stack (lồng là phần của pool PHP) và site đang chạy; hasLogs đã gồm cả hai điều kiện đó. */}
          {hasLogs && <CageBlock site={info.site} admin={isAdmin} />}
        </TabPanel>

        <TabPanel value="phep-kiem">
          {/* id của thẻ là chỗ dự phòng để chuyển focus tới khi chặng của ga không có nhóm riêng. */}
          <Card id="phep-kiem" title={t.checksHeading} titleId="phep-kiem-tieu-de">
            {noStack ? (
              <p className="m-0 text-muted">{t.noStack}</p>
            ) : (
              groups.map((stage) => {
                const list = checksOfStage(checks, stage);
                const plain = plainOf(stage);
                const stageIso = stage === "wordpress" ? iso : null;
                // Dấu của chặng tính trên mọi phép kiểm của chặng, kể cả các phép đã gộp.
                const mark = worstMark(list, siteSuspended);
                return (
                  <div key={stage} className="border-t border-hair pt-[12px] first:border-t-0 first:pt-0">
                    <h3 id={stageGroupId(stage)} tabIndex={-1} className="m-0 flex items-center gap-[8px] text-[14px] font-semibold">
                      <span className="flex h-[20px] w-[20px] items-center justify-center">
                        <StatusMark status={mark.status} />
                      </span>
                      {t.stages[stage]}
                      <span className="sr-only">: {statusName(mark.status, mark.suspended)}</span>
                    </h3>
                    {list.length === 0 ? (
                      <p className="mt-[6px] mb-[12px] pl-[28px] text-muted">{siteSuspended ? t.noChecksSuspended : t.noChecksInStage}</p>
                    ) : (
                      <>
                        {plain.length > 0 && (
                          <ul className="m-0 mt-[2px] list-none p-0">
                            {plain.map((c) => (
                              <CheckItem key={c.id} check={c} open={open.has(c.id)} onToggle={() => toggle(c.id)} />
                            ))}
                          </ul>
                        )}
                        {stageIso && <IsolationBlock group={stageIso} open={open} onToggle={toggle} isoOpen={isoOpen} onIsoOpenChange={setIsoOpen} />}
                      </>
                    )}
                  </div>
                );
              })
            )}
          </Card>
        </TabPanel>

        {hasLogs && (
          <TabPanel value="log">
            <LiveLog domain={domain} />
          </TabPanel>
        )}

        {hasLogs && (
          <TabPanel value="php">
            <PhpBlock domain={domain} version={info.site.php_version} socket={info.socket} blocked={phpBlocked} />
          </TabPanel>
        )}

        {/* M19: tên phụ và luật chuyển hướng cần vhost của site, nên cùng điều kiện hiện với tab Log và PHP. */}
        {hasLogs && (
          <TabPanel value="ten-mien">
            <Names domain={domain} suspended={siteSuspended} />
          </TabPanel>
        )}

        {hasLogs && (
          <TabPanel value="chuyen-huong">
            <Redirects domain={domain} suspended={siteSuspended} />
          </TabPanel>
        )}

        <TabPanel value="backup">
          <Backups domain={domain} active={info.site.status === "active"} restoreBlocked={siteSuspended ? t.blockedSite : undefined} />
        </TabPanel>

        <TabPanel value="cai-dat">
          {/* M14: chế độ .htaccess chỉ có nghĩa trên Apache. Server cũ không gửi trường htaccess thì cũng chưa có API đổi chế độ: không hiện khối. */}
          {info.web === "apache" && info.site.status === "active" && info.site.htaccess && (
            <HtaccessBlock domain={domain} mode={info.site.htaccess} blocked={siteSuspended ? t.htBlockedSite : undefined} />
          )}

          {info.site.status === "active" && <SuspendBlock site={info.site} admin={isAdmin} />}
          {isAdmin && info.site.status === "active" && <OwnerBlock site={info.site} />}

          <Card title={t.layoutHeading} titleId="ten-duong-dan" actions={<span className="text-[13px] text-muted">{t.layoutHint}</span>}>
            <dl className="m-0">
              {layoutRows.map(([label, value]) => (
                <div key={label} className="flex flex-wrap gap-x-[16px] gap-y-[2px] border-t border-hair py-[8px] first:border-t-0 first:pt-0 last:pb-0">
                  <dt className="flex-[0_0_220px] text-muted">{label}</dt>
                  <dd className="m-0 min-w-0 flex-[1_1_240px]">{value ? <MonoText>{value}</MonoText> : <span className="text-muted">{t.vhostNoStack}</span>}</dd>
                </div>
              ))}
            </dl>
          </Card>

          {info.site.status === "trashed" ? (
            // Site đã trong thùng rác: không còn gì để "đưa vào" nữa, việc tiếp theo ở màn Thùng rác.
            <Card title={t.trashedHeading} titleId="xoa-site">
              <p className="m-0 text-muted">{t.trashedHint}</p>
              <div className="pt-[12px]">
                <Link to="/trash" className={buttonClass("primary")}>
                  {t.trashedLink}
                </Link>
              </div>
            </Card>
          ) : (
            <>
              <Card title={t.deleteHeading} titleId="xoa-site">
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
                  <p id="xoa-bi-chan" className="mt-[10px] mb-0 max-w-[72ch] text-[13px] text-muted">
                    {trashBlocked}
                  </p>
                )}
                {dry.isError && (
                  <div className="pt-[12px]">
                    <ErrorNote error={dry.error} heading={t.deleteFailed} />
                  </div>
                )}
              </Card>
              {preview && (
                <div ref={previewRef} tabIndex={-1} className="flex flex-col gap-[20px] outline-offset-0">
                  {/* Câu hỏi lại của việc này: đưa vào thùng rác không mất dữ liệu, nói rõ để người dùng bấm chạy mà yên tâm. */}
                  <p className="m-0 max-w-[72ch] font-medium">{t.deleteAsk(domain)}</p>
                  <PlanReview
                    preview={preview}
                    onRun={() => run.mutate()}
                    running={run.isPending}
                    error={run.error}
                    onCancel={() => {
                      setPreview(null);
                      run.reset();
                    }}
                  />
                </div>
              )}
            </>
          )}
        </TabPanel>
      </Tabs>
    </>
  );
}

/**
 * Khối gộp phép kiểm cô lập của chặng WordPress: một dòng tóm tắt, mở ra mỗi site
 * khác một nhóm với hai chiều. Trạng thái mở của khối và của từng phép nằm ở SiteView.
 */
function IsolationBlock({
  group,
  open,
  onToggle,
  isoOpen,
  onIsoOpenChange,
}: {
  group: IsolationGroup;
  open: Set<string>;
  onToggle: (id: string) => void;
  isoOpen: boolean;
  onIsoOpenChange: (open: boolean) => void;
}) {
  const t = useStrings(strings);
  const summary = group.bad === 0 ? t.isoSummaryOk(group.others, group.total) : t.isoSummaryBad(group.others, group.bad, group.total);
  const direction = (label: string, check: CheckResult | undefined) =>
    check && (
      <div>
        <p className="m-0 text-[12px] font-medium text-muted">{label}</p>
        <ul className="m-0 list-none p-0">
          <CheckItem check={check} open={open.has(check.id)} onToggle={() => onToggle(check.id)} />
        </ul>
      </div>
    );
  return (
    <Disclosure open={isoOpen} onOpenChange={onIsoOpenChange} summary={<StatusLine status={group.status}>{summary}</StatusLine>}>
      <div className="flex flex-col gap-[12px] pl-[28px]">
        {group.pairs.map((p) => (
          <div key={p.other} className="flex flex-col gap-[6px] border-t border-hair pt-[12px] first:border-t-0 first:pt-0">
            <h4 className="m-0 text-[13px] font-medium [overflow-wrap:anywhere]">{p.other}</h4>
            {direction(t.isoOut(p.other), p.out)}
            {direction(t.isoIn(p.other), p.in)}
          </div>
        ))}
      </div>
    </Disclosure>
  );
}

/**
 * Thẻ "Sự cố của site": sự cố đang mở của site này, lọc từ tổng quan (cùng truy vấn
 * với khung trang, không gọi thêm). Lấy sự cố có target bằng tên miền, hoặc có
 * check_id là phép cô lập dính tới site (mỗi id cô lập chỉ được ghi một lần, dưới
 * tên site gặp trước, nên lọc riêng theo target sẽ sót).
 */
function SiteIncidents({ domain }: { domain: string }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const statusName = useStatusName();
  const { lang } = useLang();
  const overview = useOverview();
  const list = (overview.data?.incidents ?? []).filter((i) => i.target === domain || touchesSite(i.check_id, domain));
  return (
    <Card title={t.incidentsHeading} titleId="su-co-site" count={list.length > 0 ? list.length : undefined}>
      {overview.isPending ? (
        <p className="m-0 text-muted">{k.loading}</p>
      ) : overview.isError ? (
        <p className="m-0">{errorMessage(overview.error, lang)}</p>
      ) : list.length === 0 ? (
        <EmptyState>{t.incidentsNone}</EmptyState>
      ) : (
        <ul className="m-0 list-none p-0">
          {list.map((i) => {
            const title = tx(i.title, lang);
            return (
              <li key={i.id} className="flex flex-wrap items-center gap-x-[12px] gap-y-[4px] border-t border-hair py-[4px] first:border-t-0 first:pt-0 last:pb-0">
                <span className="flex h-[20px] w-[20px] flex-none items-center justify-center">
                  <StatusMark status="fail" label={statusName("fail")} />
                </span>
                <span className="min-w-0 flex-[1_1_240px] font-medium [overflow-wrap:anywhere]">{title}</span>
                <span className="text-[13px] text-muted">{t.incidentOpened(formatWhen(i.opened, lang))}</span>
                <Link to={`/incidents/${i.id}`} aria-label={t.incidentLinkFor(title)} className={buttonClass("text", "", "sm")}>
                  {t.incidentLink}
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
}
