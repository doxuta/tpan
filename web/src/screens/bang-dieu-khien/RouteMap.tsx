// Thẻ "Đường đi request của từng site" (mục 1, 2, 6 và 7 của docs/GIAO-DIEN.md;
// bản 2: mục 3.7 của docs/thay-doi/M16-thiet-ke.md). Mỗi site một hàng cao
// 52 px: cột tên site (liên kết một dòng, cắt bằng dấu ba chấm), sáu ga thẳng
// cột, cột mã HTTP bên phải. Tuyến liền tới ga cuối còn đạt, đứt từ ga hỏng.
// Bấm một ga (hoặc mã HTTP) mở bằng chứng các phép kiểm của chặng đó ngay
// trong thẻ, dưới bản đồ, vì màn hẹp phải cuộn ngang mới thấy hết hàng.
// Chú giải bốn trạng thái nằm ở đầu thẻ.

import { useEffect, useId, useRef, useState } from "react";
import { Link } from "react-router";
import type { Overview, Traffic as TrafficData } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Code, MonoText } from "../../components/MonoText";
import { StatusLine, StatusMark } from "../../components/StatusMark";
import { useStatusName } from "../../components/useStatusName";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bang-dieu-khien";
import { strings as khung } from "../../i18n/khung";
import { CheckDisclosure } from "./CheckDisclosure";
import { buildRoute, fmtNumber, stationStages, type Item, type Line, type Route, type StationStage } from "./model";

type Open = { domain: string; stage: StationStage | "e2e" };

const legend = ["pass", "warn", "fail", "unknown"] as const;

// Hàng cao 52 px: nét tuyến dày 2 px nằm giữa (25 đến 27), dấu ga canh giữa ở 26.
const solid = "absolute top-[25px] border-t-2 border-route";
const dashed = "absolute top-[25px] border-t-2 border-dashed border-route-dash";

// Cột tên site: co được từ 320 px xuống 220 px (đặc tả mục 3.7). Hàng đầu bảng và
// mọi hàng dùng cùng class này để ga thẳng cột.
const nameCol = "min-w-[220px] flex-[0_1_320px] pr-[12px]";

/** Nét tuyến chạy ngang qua một ô ga. Ở ga hỏng, nửa trái liền và nửa phải đứt (như trang mẫu). */
function Track({ kind }: { kind: Line }) {
  if (kind === "solid") return <span aria-hidden="true" className={`${solid} inset-x-0`} />;
  if (kind === "dashed") return <span aria-hidden="true" className={`${dashed} inset-x-0`} />;
  return (
    <>
      <span aria-hidden="true" className={`${solid} right-1/2 left-0`} />
      <span aria-hidden="true" className={`${dashed} right-0 left-1/2`} />
    </>
  );
}

/** Chú giải bốn trạng thái, nằm ở phần actions của thẻ. */
function Legend() {
  const kt = useStrings(khung);
  const t = useStrings(strings);
  return (
    <ul aria-label={t.legend} className="m-0 flex list-none flex-wrap gap-x-[16px] gap-y-[4px] p-0 text-[13px] text-muted">
      {legend.map((s) => (
        <li key={s}>
          <StatusLine status={s}>{kt.status[s]}</StatusLine>
        </li>
      ))}
    </ul>
  );
}

export function RouteMap({ o }: { o: Overview }) {
  const t = useStrings(strings);
  const kt = useStrings(khung);
  const statusName = useStatusName();
  const { lang } = useLang();
  const panelId = useId();
  const [open, setOpen] = useState<Open | null>(null);

  const routes = o.sites.filter((r) => r.site.status === "active").map((r) => buildRoute(r, o.stack_checks));

  if (!o.stack.installed) {
    return (
      <Card title={t.mapTitle} titleId="bdk-ban-do">
        <h3 className="m-0 text-[14px] font-semibold">{t.stackMissingTitle}</h3>
        <p className="mt-[6px] mb-[12px]">{t.stackMissingBody}</p>
        <dl className="m-0 grid gap-y-[10px]">
          {(
            [
              [t.stackLemp, t.stackCmdNginx],
              [t.stackLamp, t.stackCmdApache],
            ] as const
          ).map(([what, cmd]) => (
            <div key={cmd}>
              <dt className="text-[12px] font-medium text-muted">{what}</dt>
              <dd className="m-0">
                <MonoText bold>{cmd}</MonoText>
              </dd>
            </div>
          ))}
        </dl>
      </Card>
    );
  }

  if (routes.length === 0) {
    return (
      <Card title={t.mapTitle} titleId="bdk-ban-do">
        <p className="m-0 text-muted">
          {t.noSites}{" "}
          <Link to="/sites/new" className="font-medium">
            {t.createFirst}
          </Link>
        </p>
      </Card>
    );
  }

  const keyOf = (domain: string, stage: string) => `${domain}|${stage}`;
  const toggle = (domain: string, stage: Open["stage"]) => setOpen((cur) => (cur && cur.domain === domain && cur.stage === stage ? null : { domain, stage }));
  const openRoute = open ? routes.find((r) => r.domain === open.domain) : undefined;

  return (
    <Card title={t.mapTitle} titleId="bdk-ban-do" actions={<Legend />}>
      {/* Khung cuộn ngang riêng: cả trang không bao giờ cuộn ngang. */}
      {/* relative: span sr-only của dòng request không thoát khỏi khung cuộn; -m/p 4 px: chừa chỗ cho viền focus ở mép khung. */}
      {/* min-w 720 px: mỗi ga còn đủ chỗ cho nhãn "WordPress" và "Web server"; hẹp hơn thì khung này cuộn, không phải cả trang. */}
      <div className="@container relative -m-[4px] overflow-x-auto p-[4px]">
        <div className="min-w-[720px] border-b border-hair">
          <div aria-hidden="true" className="flex items-end pb-[8px] text-[12px] font-medium text-muted">
            <div className={nameCol}>{t.colSite}</div>
            <div className="grid min-w-0 flex-1 grid-cols-6 text-center leading-[1.3]">
              {stationStages.map((s) => (
                <div key={s}>{t.stage[s]}</div>
              ))}
            </div>
            <div className="w-[76px] flex-none text-right">{t.colHttp}</div>
          </div>
          <ul className="m-0 list-none p-0">
            {routes.map((r) => (
              <li key={r.domain} className="border-t border-hair">
                <div className="flex min-h-[52px] items-center">
                  <div className={nameCol}>
                    {/* Một dòng, cắt bằng dấu ba chấm; đủ tên trong title. Viền focus của chính liên kết không bị overflow của nó cắt. */}
                    <Link
                      to={`/sites/${encodeURIComponent(r.domain)}`}
                      title={r.domain}
                      data-site-name={r.domain}
                      className="block w-fit max-w-full truncate text-[14px] leading-[1.45] font-medium text-ink no-underline hover:text-ink hover:underline"
                    >
                      {r.domain}
                    </Link>
                    {r.traffic && <Traffic traffic={r.traffic} />}
                  </div>
                  <div className="grid min-w-0 flex-1 grid-cols-6">
                    {r.cells.map((c) => {
                      const k = keyOf(r.domain, c.stage);
                      const isOpen = open !== null && keyOf(open.domain, open.stage) === k;
                      return (
                        <button
                          key={c.stage}
                          type="button"
                          data-key={k}
                          aria-expanded={isOpen}
                          aria-controls={isOpen ? panelId : undefined}
                          aria-label={kt.statusLabel(t.stationLabel(r.domain, t.stage[c.stage]), statusName(c.status, c.suspended))}
                          onClick={() => toggle(r.domain, c.stage)}
                          className={`relative block h-[52px] min-w-0 hover:bg-soft ${isOpen ? "bg-accent-tint" : ""}`}
                        >
                          <Track kind={c.line} />
                          <span className="absolute top-[26px] left-1/2 -translate-x-1/2 -translate-y-1/2">
                            <StatusMark status={c.status} />
                          </span>
                        </button>
                      );
                    })}
                  </div>
                  <HttpCell route={r} open={open} keyOf={keyOf} panelId={panelId} onToggle={() => toggle(r.domain, "e2e")} />
                </div>
                {r.broken ? (
                  // sticky + bề rộng 100cqw (phần nhìn thấy của khung cuộn): ở màn hẹp dòng này nằm trọn trong tầm mắt, không phải cuộn ngang mới đọc được.
                  <p className="sticky left-0 m-0 box-border w-[100cqw] pb-[12px] text-[13px] font-medium text-fail-text">
                    {t.failLine(t.stage[r.broken.stage], tx(r.broken.item.check.summary, lang), r.broken.item.shared)}
                  </p>
                ) : (
                  r.suspended && (
                    // Site tạm khoá: dòng xám (không đỏ) nói ga xám là do khoá, như trang site.
                    <p className="sticky left-0 m-0 box-border w-[100cqw] pb-[12px] text-[13px] text-muted">{t.suspendedLine}</p>
                  )
                )}
              </li>
            ))}
          </ul>
        </div>
      </div>

      {open && openRoute && (
        <Panel
          key={keyOf(open.domain, open.stage)}
          id={panelId}
          domain={openRoute.domain}
          stage={open.stage}
          items={open.stage === "e2e" ? openRoute.http.items : (openRoute.cells.find((c) => c.stage === open.stage)?.items ?? [])}
          onClose={() => {
            const k = keyOf(open.domain, open.stage);
            setOpen(null);
            // Trả focus về ga đã mở để người dùng bàn phím không lạc chỗ.
            requestAnimationFrame(() => document.querySelector<HTMLElement>(`[data-key="${CSS.escape(k)}"]`)?.focus());
          }}
        />
      )}
    </Card>
  );
}

/**
 * Số request và lỗi 5xx một giờ qua, dòng phụ dưới tên site (12 px, màu --muted;
 * màu đỏ chỉ dành cho dấu trạng thái). Chữ hiện ra rút gọn cho vừa cột; trình
 * đọc màn hình và tooltip nhận câu đầy đủ, trong đó partial (chỉ đọc được phần
 * cuối log) ghi là "ít nhất".
 */
function Traffic({ traffic }: { traffic: TrafficData }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const req = fmtNumber(traffic.requests, lang, 0);
  const err = fmtNumber(traffic.errors_5xx, lang, 0);
  const partial = traffic.partial === true;
  return (
    <div title={t.trafficFull(req, err, partial)} className="truncate text-[12px] leading-[1.4] text-muted tabular-nums">
      <span aria-hidden="true">{t.trafficShort(req, err, partial)}</span>
      <span className="sr-only">{t.trafficFull(req, err, partial)}</span>
    </div>
  );
}

function HttpCell({ route, open, keyOf, panelId, onToggle }: { route: Route; open: Open | null; keyOf: (d: string, s: string) => string; panelId: string; onToggle: () => void }) {
  const t = useStrings(strings);
  const { code } = route.http;
  // Mã khác 200 có nền --fail (mục 6); phép kiểm cuối hỏng dù mã là 200 cũng vậy, kẻo màu đỏ bị giấu.
  // Site tạm khoá trả 403 là đúng ý (M13): chỉ phép kiểm hỏng mới tô đỏ.
  const bad = (code !== null && code !== "200" && !route.suspended) || route.http.status === "fail";
  const chip = bad ? "bg-fail text-on-fail" : "text-muted";

  if (route.http.items.length === 0) {
    return (
      <div className="flex w-[76px] flex-none justify-end px-[8px]">
        <Code className="text-muted">{code ?? "—"}</Code>
      </div>
    );
  }
  const k = keyOf(route.domain, "e2e");
  const isOpen = open !== null && keyOf(open.domain, open.stage) === k;
  return (
    <button
      type="button"
      data-key={k}
      aria-expanded={isOpen}
      aria-controls={isOpen ? panelId : undefined}
      aria-label={t.httpLabel(route.domain, code)}
      onClick={onToggle}
      className={`flex h-[52px] w-[76px] flex-none items-center justify-end hover:bg-soft ${isOpen ? "bg-accent-tint" : ""}`}
    >
      <span className="px-[8px]">
        <Code className={`px-[6px] py-[2px] font-semibold ${chip}`}>{code ?? "—"}</Code>
      </span>
    </button>
  );
}

/** Bằng chứng của các phép kiểm trong một chặng của một site: mỗi phép kiểm một dòng mở ra được, chỉ dòng lỗi mở sẵn. */
function Panel({ id, domain, stage, items, onClose }: { id: string; domain: string; stage: Open["stage"]; items: Item[]; onClose: () => void }) {
  const t = useStrings(strings);
  const root = useRef<HTMLDivElement>(null);

  // Mở bằng chứng thì chuyển focus sang đó (người dùng bàn phím khỏi phải đi qua cả bản đồ để tới) và kéo vào tầm nhìn.
  useEffect(() => {
    root.current?.focus();
    root.current?.scrollIntoView({ block: "nearest" });
  }, [domain, stage]);

  return (
    <div
      id={id}
      ref={root}
      tabIndex={-1}
      role="region"
      aria-label={t.panelTitle(domain, t.stage[stage])}
      onKeyDown={(e) => e.key === "Escape" && onClose()}
      className="mt-[16px] border-t border-hair pt-[16px] outline-offset-2"
    >
      <div className="flex flex-wrap items-center justify-between gap-[8px]">
        <h3 className="m-0 min-w-0 text-[14px] font-semibold [overflow-wrap:anywhere]">{t.panelTitle(domain, t.stage[stage])}</h3>
        <Button size="sm" onClick={onClose}>
          {t.panelClose}
        </Button>
      </div>
      {items.length === 0 ? (
        <p className="mt-[10px] mb-0 text-muted">{t.noChecks}</p>
      ) : (
        <div className="mt-[8px]">
          {items.map(({ check, shared }) => (
            <CheckDisclosure key={`${shared ? "s" : "x"}:${check.id}`} check={check} shared={shared} />
          ))}
        </div>
      )}
    </div>
  );
}
