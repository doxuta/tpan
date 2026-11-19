// Bản đồ tuyến (mục 1, 2, 6 và 7 của docs/GIAO-DIEN.md): mỗi site một hàng cao
// 44 px, sáu ga thẳng cột, cột mã HTTP bên phải. Tuyến liền tới ga cuối còn
// đạt, đứt từ ga hỏng. Bấm một ga (hoặc mã HTTP) mở bằng chứng các phép kiểm
// của chặng đó ngay dưới bản đồ, vì màn hẹp phải cuộn ngang mới thấy hết hàng.

import { useEffect, useId, useRef, useState } from "react";
import { Link } from "react-router";
import type { Overview } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { MonoText } from "../../components/MonoText";
import { StatusMark } from "../../components/StatusMark";
import { Tag } from "../../components/Tag";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bang-dieu-khien";
import { strings as khung } from "../../i18n/khung";
import { buildRoute, stationStages, type Item, type Line, type Route, type StationStage } from "./model";

type Open = { domain: string; stage: StationStage | "e2e" };

const solid = "absolute top-[21px] border-t-2 border-line";
const dashed = "absolute top-[21px] border-t-2 border-dashed border-line-dash";

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

export function RouteMap({ o }: { o: Overview }) {
  const t = useStrings(strings);
  const kt = useStrings(khung);
  const { lang } = useLang();
  const panelId = useId();
  const [open, setOpen] = useState<Open | null>(null);

  const routes = o.sites.filter((r) => r.site.status === "active").map((r) => buildRoute(r, o.stack_checks));

  if (!o.stack.installed) {
    return (
      <div className="border-t border-hair py-[14px]">
        <h3 className="m-0 text-[16px] font-semibold">{t.stackMissingTitle}</h3>
        <p className="mt-[6px] mb-[12px]">{t.stackMissingBody}</p>
        <dl className="m-0 grid gap-y-[8px]">
          {(
            [
              [t.stackLemp, t.stackCmdNginx],
              [t.stackLamp, t.stackCmdApache],
            ] as const
          ).map(([what, cmd]) => (
            <div key={cmd}>
              <dt className="font-mono text-[11px] tracking-[0.08em] text-muted">{what}</dt>
              <dd className="m-0">
                <MonoText bold>{cmd}</MonoText>
              </dd>
            </div>
          ))}
        </dl>
      </div>
    );
  }

  if (routes.length === 0) {
    return (
      <p className="m-0 border-t border-hair py-[14px]">
        {t.noSites}{" "}
        <Link to="/sites/new" className="font-medium">
          {t.createFirst}
        </Link>
      </p>
    );
  }

  const keyOf = (domain: string, stage: string) => `${domain}|${stage}`;
  const toggle = (domain: string, stage: Open["stage"]) => setOpen((cur) => (cur && cur.domain === domain && cur.stage === stage ? null : { domain, stage }));
  const openRoute = open ? routes.find((r) => r.domain === open.domain) : undefined;

  return (
    <>
      {/* Khung cuộn ngang riêng: cả trang không bao giờ cuộn ngang. */}
      <div className="@container overflow-x-auto">
        <div className="min-w-[700px] border-b border-hair">
          <div aria-hidden="true" className="flex items-end pb-[8px] font-mono text-[11px] tracking-[0.08em] text-muted">
            <div className="w-[180px] flex-none">{t.colSite}</div>
            <div className="grid flex-1 grid-cols-6 text-center whitespace-nowrap uppercase">
              {stationStages.map((s) => (
                <div key={s}>{t.stage[s]}</div>
              ))}
            </div>
            <div className="w-[76px] flex-none text-right">{t.colHttp}</div>
          </div>
          <ul className="m-0 list-none p-0">
            {routes.map((r) => (
              <li key={r.domain} className="border-t border-hair">
                <div className="flex items-center">
                  <Link
                    to={`/sites/${encodeURIComponent(r.domain)}`}
                    title={r.domain}
                    className="w-[180px] flex-none overflow-hidden font-mono text-[13px] text-ellipsis whitespace-nowrap text-ink no-underline hover:text-ink hover:underline"
                  >
                    {r.domain}
                  </Link>
                  <div className="grid flex-1 grid-cols-6">
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
                          aria-label={kt.statusLabel(t.stationLabel(r.domain, t.stage[c.stage]), kt.status[c.status])}
                          onClick={() => toggle(r.domain, c.stage)}
                          className={`relative block h-[44px] min-w-0 hover:bg-pre ${isOpen ? "bg-pre" : ""}`}
                        >
                          <Track kind={c.line} />
                          <span className="absolute top-[22px] left-1/2 -translate-x-1/2 -translate-y-1/2">
                            <StatusMark status={c.status} />
                          </span>
                        </button>
                      );
                    })}
                  </div>
                  <HttpCell route={r} open={open} keyOf={keyOf} panelId={panelId} onToggle={() => toggle(r.domain, "e2e")} />
                </div>
                {r.broken && (
                  // sticky + bề rộng 100cqw (phần nhìn thấy của khung cuộn): ở màn hẹp dòng này nằm trọn trong tầm mắt, không phải cuộn ngang mới đọc được.
                  <p className="sticky left-0 m-0 box-border w-[100cqw] pb-[12px] text-[13px] font-medium text-fail-text sm:pl-[180px]">
                    {t.failLine(t.stage[r.broken.stage], tx(r.broken.item.check.summary, lang), r.broken.item.shared)}
                  </p>
                )}
              </li>
            ))}
          </ul>
        </div>
      </div>

      {open && openRoute && (
        <Panel
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
    </>
  );
}

function HttpCell({ route, open, keyOf, panelId, onToggle }: { route: Route; open: Open | null; keyOf: (d: string, s: string) => string; panelId: string; onToggle: () => void }) {
  const t = useStrings(strings);
  const { code } = route.http;
  // Mã khác 200 có nền --fail (mục 6); phép kiểm cuối hỏng dù mã là 200 cũng vậy, kẻo màu đỏ bị giấu.
  const bad = (code !== null && code !== "200") || route.http.status === "fail";
  const chip = bad ? "bg-fail text-on-fail" : "text-muted";

  if (route.http.items.length === 0) {
    return <div className="flex w-[76px] flex-none justify-end px-[8px] font-mono text-[13px] text-muted">{code ?? "—"}</div>;
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
      className={`flex h-[44px] w-[76px] flex-none items-center justify-end hover:bg-pre ${isOpen ? "bg-pre" : ""}`}
    >
      <span className={`px-[8px] py-[2px] font-mono text-[13px] font-semibold ${chip}`}>{code ?? "—"}</span>
    </button>
  );
}

/** Bằng chứng của các phép kiểm trong một chặng của một site. */
function Panel({ id, domain, stage, items, onClose }: { id: string; domain: string; stage: Open["stage"]; items: Item[]; onClose: () => void }) {
  const t = useStrings(strings);
  const kt = useStrings(khung);
  const { lang } = useLang();
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
      className="mt-[16px] outline-offset-2"
    >
      <Card>
        <div className="flex flex-wrap items-center justify-between gap-[8px]">
          <h3 className="m-0 text-[16px] font-semibold">{t.panelTitle(domain, t.stage[stage])}</h3>
          <Button onClick={onClose}>{t.panelClose}</Button>
        </div>
        {items.length === 0 && <p className="mt-[10px] mb-0 text-muted">{t.noChecks}</p>}
        {items.map(({ check, shared }) => (
          <div key={`${shared ? "s" : "x"}:${check.id}`} className="mt-[14px] border-t border-hair pt-[12px]">
            <div className="flex items-center gap-[10px]">
              <StatusMark status={check.status} label={kt.statusLabel(tx(check.title, lang), kt.status[check.status])} />
              <span className="font-medium">{tx(check.title, lang)}</span>
              {shared && <Tag>{t.sharedTag}</Tag>}
            </div>
            <p className="mt-[6px] mb-0 text-[13px] text-muted">{tx(check.summary, lang)}</p>
            {check.status !== "pass" && check.cause && <p className="mt-[6px] mb-0">{tx(check.cause, lang)}</p>}
            {check.evidence && check.evidence.length > 0 && (
              <div className="mt-[10px]">
                <EvidenceBlock evidence={check.evidence} />
              </div>
            )}
          </div>
        ))}
      </Card>
    </div>
  );
}
