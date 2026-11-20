// Màn Cơ sở dữ liệu (mục 6.9 của KE-HOACH.md): bảng kẻ mảnh các cơ sở dữ liệu
// của site (GET /api/v1/databases), mỗi hàng ba việc: Đổi mật khẩu, Xuất .sql,
// Nhập từ một bản đã xuất. Cả ba đi đúng ba chặng như màn Thùng rác (màn mẫu
// để theo): bấm nút → gọi ?dry_run=1 lấy bản xem trước (PlanReview) → bấm chạy
// → máy chủ trả job_id, chuyển tới /jobs/:id. Cùng lúc chỉ mở một việc nên cả
// ba dùng chung một cặp mutation và một chỗ hiện bản xem trước. Site trong
// thùng rác chỉ cho xuất: đổi mật khẩu và nhập sẽ ghi vào wp-config.php và DB
// của một site đã ngừng chạy.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { useJobTracker } from "../../components/JobTracker";
import { errorMessage, get, post } from "../../api/client";
import { queryKeys, useOverview } from "../../api/queries";
import type { Backup, DatabaseRow, DBImportRequest, JobStarted, Preview } from "../../api/types";
import { Button } from "../../components/Button";
import { DomainText } from "../../components/DomainText";
import { MonoText } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { TableList } from "../../components/TableList";
import { useLang, useStrings, type Lang } from "../../i18n";
import { strings } from "../../i18n/co-so-du-lieu";
import { strings as khung } from "../../i18n/khung";
import { backupsKey } from "../backup/model";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "../site/ErrorNote";
import { formatWhen } from "../thung-rac/model";
import { ImportPicker } from "./ImportPicker";
import { databasesKey, databasesPath, dbOpPath } from "./model";

/** Việc đang hỏi: đủ để gọi cả hai chặng (xem trước và chạy) và để chữ nói đúng việc. */
type Req =
  | { action: "password"; row: DatabaseRow }
  | { action: "dump"; row: DatabaseRow }
  | { action: "import"; row: DatabaseRow; backup: Backup };

const pathOf = (r: Req) => dbOpPath(r.row.domain, r.action);
const bodyOf = (r: Req): DBImportRequest | undefined => (r.action === "import" ? { backup_id: r.backup.id } : undefined);

/** Byte thành KB, MB hoặc GB, một số lẻ, dấu thập phân theo ngôn ngữ. */
function formatSize(bytes: number, lang: Lang): string {
  const num = new Intl.NumberFormat(lang === "vi" ? "vi-VN" : "en-GB", { maximumFractionDigits: 1 });
  if (bytes < 1024 * 1024) return `${num.format(bytes / 1024)} KB`;
  if (bytes < 1024 ** 3) return `${num.format(bytes / 1024 ** 2)} MB`;
  return `${num.format(bytes / 1024 ** 3)} GB`;
}

/**
 * Độ rộng tối thiểu cho chữ mono dài (tên DB, domain). min-width đặt lên ô <td>
 * không được trình duyệt tôn trọng, và MonoText cho ngắt giữa chữ, nên không có
 * khối này thì cột bị ép hẹp và tên bị cắt ra nhiều dòng. Hẹp quá thì cả bảng
 * cuộn ngang trong khung riêng của TableList.
 */
function Min({ w, children }: { w: string; children: ReactNode }) {
  return <span className={`block ${w}`}>{children}</span>;
}

export default function CoSoDuLieu() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const qc = useQueryClient();
  const { track } = useJobTracker();
  const q = useQuery({ queryKey: databasesKey, queryFn: () => get<DatabaseRow[]>(databasesPath) });
  // Cùng khoá overview với khung trang: không gọi thêm API. Cho biết site nào đang ở thùng rác.
  const overview = useOverview();
  const trashed = useMemo(() => new Set((overview.data?.sites ?? []).filter((r) => r.site.status === "trashed").map((r) => r.site.domain)), [overview.data]);

  /** Site đang chọn nguồn nhập (chặng trước khi có bản xem trước). */
  const [picking, setPicking] = useState<DatabaseRow | null>(null);
  const [open, setOpen] = useState<{ req: Req; preview: Preview } | null>(null);
  /** Việc vừa bấm: để chữ nút và lỗi nói đúng việc. */
  const [asked, setAsked] = useState<Req | null>(null);
  const resultRef = useRef<HTMLDivElement>(null);

  const dry = useMutation({
    mutationFn: (req: Req) => post<Preview>(`${pathOf(req)}?dry_run=1`, bodyOf(req)),
    onSuccess: (preview, req) => setOpen({ req, preview }),
  });
  const run = useMutation({
    mutationFn: (req: Req) => post<JobStarted>(pathOf(req), bodyOf(req)),
    onSuccess: (job) => {
      // Dung lượng, danh sách bản xuất và số đếm ở menu đều đổi sau job.
      void qc.invalidateQueries({ queryKey: databasesKey });
      void qc.invalidateQueries({ queryKey: backupsKey });
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      // Ở lại trang đang dùng; thẻ theo dõi ở góc dưới báo tiến độ và tải lại dữ liệu khi job xong.
      track(job.job_id);
      setOpen(null);
    },
  });

  // Có bản xem trước, lỗi hoặc ô chọn nguồn nhập mới thì đưa người dùng tới đó, vì chúng nằm dưới bảng.
  const shown = open ?? (dry.isError ? asked : null) ?? picking;
  useEffect(() => {
    if (shown) {
      resultRef.current?.scrollIntoView({ block: "start" });
      resultRef.current?.focus();
    }
  }, [shown]);

  function ask(req: Req) {
    setOpen(null);
    run.reset();
    setAsked(req);
    dry.mutate(req);
  }

  function close() {
    setOpen(null);
    setAsked(null);
    setPicking(null);
    dry.reset();
    run.reset();
  }

  function pickImport(row: DatabaseRow) {
    close();
    setPicking(row);
  }

  const pending = dry.isPending ? asked : null;
  const busy = (action: Req["action"], domain: string) => pending?.action === action && pending.row.domain === domain;

  const errorHeading = (r: Req): string =>
    r.action === "password" ? t.passwordErrorHeading(r.row.domain) : r.action === "dump" ? t.dumpErrorHeading(r.row.domain) : t.importErrorHeading(r.row.domain);

  /** Lời nói rõ điều gì sẽ xảy ra, đặt trên bản xem trước (chữ đậm, không dấu đỏ: đỏ chỉ dành cho tình trạng máy). */
  const warningOf = (r: Req): string =>
    r.action === "password" ? t.passwordWarning(r.row.db, r.row.domain) : r.action === "dump" ? t.dumpWarning(r.row.db) : t.importWarning(r.row.domain, formatWhen(r.backup.created, lang));

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />
      <div className="pt-[20px]">
        {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
        {q.isError && (
          <div>
            <p className="m-0">{errorMessage(q.error, lang)}</p>
            <Button className="mt-[12px]" onClick={() => void q.refetch()}>
              {k.retry}
            </Button>
          </div>
        )}
        {q.isSuccess && (
          <>
            <TableList
              caption={t.tableName}
              rows={q.data}
              rowKey={(r) => r.db}
              empty={
                <>
                  {t.emptyText} <Link to="/sites/new">{t.emptyLink}</Link>
                </>
              }
              columns={[
                {
                  // DB và user chung một cột (thường cùng tên): bảng bớt một cột rộng, vừa khung khi cửa sổ hẹp.
                  header: t.colDbUser,
                  cell: (r) => (
                    <Min w="min-w-[216px]">
                      <MonoText>{r.db}</MonoText>
                      {r.user === r.db ? (
                        <span className="block text-[12px] text-muted">{t.sameUser}</span>
                      ) : (
                        <span className="block">
                          <MonoText>{r.user}</MonoText>
                        </span>
                      )}
                    </Min>
                  ),
                },
                {
                  header: t.colSite,
                  cell: (r) => (
                    <Min w="min-w-[130px]">
                      <Link to={`/sites/${encodeURIComponent(r.domain)}`} className="font-mono text-[13px]">
                        <DomainText domain={r.domain} />
                      </Link>
                    </Min>
                  ),
                },
                {
                  header: t.colSize,
                  className: "text-right whitespace-nowrap tabular-nums",
                  cell: (r) => (r.size_bytes < 0 ? <span className="text-muted">{t.unreadable}</span> : formatSize(r.size_bytes, lang)),
                },
                {
                  header: t.colTables,
                  className: "text-right whitespace-nowrap tabular-nums",
                  cell: (r) => (r.tables < 0 ? <span className="text-muted">{t.unreadable}</span> : r.tables),
                },
                {
                  header: t.colAction,
                  className: "pr-0",
                  cell: (r) => {
                    const locked = dry.isPending;
                    const inTrash = trashed.has(r.domain);
                    return (
                      <span className="flex min-w-[150px] flex-wrap items-center gap-[8px]">
                        {!inTrash && (
                          <Button aria-label={t.passwordFor(r.domain)} disabled={locked} onClick={() => ask({ action: "password", row: r })}>
                            {busy("password", r.domain) ? t.previewing : t.password}
                          </Button>
                        )}
                        <Button aria-label={t.dumpFor(r.domain)} disabled={locked} onClick={() => ask({ action: "dump", row: r })}>
                          {busy("dump", r.domain) ? t.previewing : t.dump}
                        </Button>
                        {!inTrash && (
                          <Button aria-label={t.importFor(r.domain)} disabled={locked} onClick={() => pickImport(r)}>
                            {t.importOpen}
                          </Button>
                        )}
                        {inTrash && (
                          <span className="text-[13px] text-muted">
                            {t.trashedLead}
                            <Link to="/trash">{t.trashedLink}</Link>
                            {t.trashedTail}
                          </span>
                        )}
                      </span>
                    );
                  },
                },
              ]}
            />
            <p className="mt-[16px] mb-0 max-w-[720px] text-[13px] text-muted">{t.hint}</p>

            {picking && !open && !dry.isError && (
              <div ref={resultRef} tabIndex={-1} className="outline-offset-0">
                <ImportPicker
                  key={picking.domain}
                  domain={picking.domain}
                  pending={pending?.action === "import"}
                  anyPending={dry.isPending}
                  onPreview={(backup) => ask({ action: "import", row: picking, backup })}
                  onCancel={close}
                />
              </div>
            )}

            {dry.isError && asked && (
              <div ref={resultRef} tabIndex={-1} className="pt-[26px] outline-offset-0">
                <ErrorNote error={dry.error} heading={errorHeading(asked)} checksHeading={t.checksHeading} />
                <div className="pt-[12px]">
                  <Button onClick={close}>{k.cancel}</Button>
                </div>
              </div>
            )}

            {open && (
              <div ref={resultRef} tabIndex={-1} className="outline-offset-0">
                <p role="alert" className="m-0 max-w-[720px] pt-[26px] font-semibold">
                  {warningOf(open.req)}
                </p>
                <PlanReview preview={open.preview} onRun={() => run.mutate(open.req)} running={run.isPending} error={run.error} onCancel={close} />
              </div>
            )}
          </>
        )}
      </div>
    </>
  );
}
