// Trang Công cụ (/tools, mốc M16; mục 3.7 của docs/thay-doi/M16-thiet-ke.md,
// trang mẫu docs/mau-giao-dien/v2/cong-cu.html). Mọi việc làm được với tài
// khoản này, xếp theo nhóm; cột bên là tài khoản, mức dùng so với gói và sự cố
// cần xem. Là trang chủ của user thường.
//
// Không gọi API mới: phiên (/api/v1/session) và tổng quan (/api/v1/overview)
// đã có sẵn trong bộ đệm của khung trang. Riêng admin không có quota trong
// tổng quan, nên gói và mức dùng của admin lấy từ hàng của chính họ ở
// GET /api/v1/users (route chỉ admin; user thường không bao giờ gọi).

import type { ReactNode } from "react";
import { Link } from "react-router";
import { useUsers, useWho } from "../../api/m13-users";
import { useOverview, useSession } from "../../api/queries";
import type { Overview, Package, StackSummary, Usage } from "../../api/types";
import { Card } from "../../components/Card";
import { Icon } from "../../components/Icon";
import { Code } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { StatusMark } from "../../components/StatusMark";
import { UsageMeter } from "../../components/UsageMeter";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/cong-cu";
import { strings as khung } from "../../i18n/khung";
import { activeSites, isAdminPath } from "../../routes";
import { diskLimitText, diskUsedText, percentOf } from "../nguoi-dung/model";
import { toolGroups, visibleGroups, type Tool } from "./tools";

type T = (typeof strings)["vi"];

export default function Tools() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const who = useWho();
  const admin = who.isAdmin;
  const session = useSession().data;
  const overview = useOverview().data;
  // Chỉ admin mới gọi danh sách user (route chỉ admin); user thường đọc quota trong tổng quan.
  const users = useUsers(admin);
  const own = admin ? users.data?.find((r) => r.user.name === who.name) : undefined;
  const plan: { pkg: Package; usage: Usage } | undefined = admin ? (own ? { pkg: own.limits, usage: own.usage } : undefined) : overview?.quota && { pkg: overview.quota.package, usage: overview.quota.usage };
  const incidents = overview?.incidents.length ?? 0;

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />
      <div className="flex flex-wrap items-start gap-[24px]">
        <div className="flex min-w-0 flex-[999_1_520px] flex-col gap-[16px]">
          {visibleGroups(toolGroups, admin, isAdminPath).map((g) => (
            <Card key={g.group} title={t.groups[g.group]}>
              <div className="grid grid-cols-[repeat(auto-fit,minmax(min(230px,100%),1fr))] gap-[8px]">
                {g.tools.map((tool) => (
                  <ToolTile key={tool.key} t={t} tool={tool} admin={admin} count={countOf(tool, overview)} />
                ))}
              </div>
            </Card>
          ))}
        </div>

        <aside aria-label={t.asideLabel} className="flex min-w-0 flex-[1_1_280px] flex-col gap-[16px]">
          <Card title={t.account}>
            <Rows>
              <Row label={t.user}>
                <span className="font-medium">{who.name}</span>
              </Row>
              {who.identity && <Row label={t.role}>{k.roles[who.identity.role]}</Row>}
              {plan && (
                <Row label={t.pkg}>
                  {/* /packages là màn admin: user thường thấy tên gói là chữ thường. */}
                  {admin ? (
                    <Link to="/packages" className="font-medium no-underline hover:underline">
                      {plan.pkg.name}
                    </Link>
                  ) : (
                    plan.pkg.name
                  )}
                </Row>
              )}
              {session?.hostname && (
                <Row label={t.machine}>
                  <Code>{session.hostname}</Code>
                </Row>
              )}
              {overview && <Row label={t.stack}>{stackText(overview.stack, t)}</Row>}
              {session?.version && (
                <Row label={t.tpan}>
                  <Code>{session.version}</Code>
                </Row>
              )}
            </Rows>
          </Card>

          {plan && (
            <Card title={t.usage}>
              <Rows>
                <Row label={t.usageSites}>
                  <UsageMeter used={String(plan.usage.sites)} limit={limitText(plan.pkg.max_sites)} percent={percentOf(plan.usage.sites, plan.pkg.max_sites)} />
                </Row>
                <Row label={t.usageDbs}>
                  <UsageMeter used={String(plan.usage.dbs)} limit={limitText(plan.pkg.max_dbs)} percent={percentOf(plan.usage.dbs, plan.pkg.max_dbs)} />
                </Row>
                <Row label={t.usageDisk}>
                  <UsageMeter used={diskUsedText(plan.usage, lang)} limit={diskLimitText(plan.pkg.disk_mb, lang)} percent={percentOf(plan.usage.disk_bytes, plan.pkg.disk_mb * 2 ** 20)} />
                </Row>
              </Rows>
              <p className="mt-[10px] mb-0 text-[12.5px] text-muted">{t.usageHow}</p>
              <p className="mt-[2px] mb-0 text-[12.5px] text-muted">{t.unlimitedNote}</p>
            </Card>
          )}

          <Card title={t.attention}>
            {incidents > 0 ? (
              <Link to="/incidents" className="flex min-h-[44px] items-center gap-[10px] font-medium text-fail-text no-underline hover:text-fail-text hover:underline">
                <StatusMark status="fail" size="sm" />
                <span>{t.openIncidents(incidents)}</span>
              </Link>
            ) : (
              <p className="m-0 text-muted">{t.noIncidents}</p>
            )}
          </Card>
        </aside>
      </div>
    </>
  );
}

/** Số trên ô: cùng định nghĩa với số ở menu (mục 3.7). Sự cố chỉ hiện khi lớn hơn 0. */
function countOf(tool: Tool, o: Overview | undefined): number | undefined {
  if (!o || !tool.count) return undefined;
  if (tool.count === "sites") return activeSites(o);
  if (tool.count === "dbs") return o.db_count;
  return o.incidents.length || undefined;
}

function ToolTile({ t, tool, admin, count }: { t: T; tool: Tool; admin: boolean; count: number | undefined }) {
  const label = t.tiles[tool.key];
  return (
    <Link
      to={(admin ? tool.path : (tool.userPath ?? tool.path)) + (tool.hash ?? "")}
      className="flex min-h-[44px] items-start gap-[12px] rounded-ctl border border-hair bg-surface px-[12px] py-[10px] text-ink no-underline transition-colors hover:bg-soft hover:text-ink"
    >
      <span aria-hidden="true" className="flex size-[36px] flex-none items-center justify-center rounded-ctl bg-accent-tint text-accent">
        <Icon name={tool.icon} size={20} />
      </span>
      <span className="flex min-w-0 flex-col gap-[2px]">
        <span className="flex flex-wrap items-center gap-x-[8px] gap-y-[4px] font-medium">
          {label.label}
          {count !== undefined && <span className="font-normal text-muted tabular-nums">{count}</span>}
        </span>
        <span className="text-[12.5px] text-muted">{label.desc}</span>
      </span>
    </Link>
  );
}

function Rows({ children }: { children: ReactNode }) {
  return <dl className="m-0 flex flex-col">{children}</dl>;
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex justify-between gap-[16px] border-t border-hair py-[7px] first:border-t-0 first:pt-0">
      <dt className="text-muted">{label}</dt>
      <dd className="m-0 min-w-0 text-right [overflow-wrap:anywhere]">{children}</dd>
    </div>
  );
}

/** "LEMP · nginx · PHP 8.3", hoặc "chưa cài stack". */
function stackText(s: StackSummary, t: T): string {
  if (!s.installed) return t.noStack;
  return [s.web === "nginx" ? "LEMP" : s.web === "apache" ? "LAMP" : "", s.web, s.php_version ? `PHP ${s.php_version}` : ""].filter(Boolean).join(" · ");
}

/** Giới hạn 0 của gói là không giới hạn: UsageMeter viết "∞". */
function limitText(n: number): string | null {
  return n > 0 ? String(n) : null;
}
