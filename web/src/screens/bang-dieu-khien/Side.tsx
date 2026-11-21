// Cột bên của Bảng điều khiển (mục 3.7 của docs/thay-doi/M16-thiet-ke.md):
// "Sự cố đang mở" (mỗi sự cố một thẻ gọn), "Cảnh báo" (một thẻ, mỗi cảnh báo một
// dòng mở ra được), "Chờ bạn duyệt". Khối nào không có gì thì ghi một dòng
// "Không có", không ẩn đi.
//
// Sự cố đọc từ overview.incidents (health mở khi một phép kiểm chuyển sang
// lỗi, mục 6.7), không còn suy từ phép kiểm lỗi như M3. Ba cách sửa (tự động
// qua xem trước, sửa tay, hỏi AI) nằm ở trang sự cố /incidents/:id, nên thẻ ở
// đây chỉ dẫn tới đó; cảnh báo vẫn là phép kiểm warn và mở bằng chứng tại chỗ.

import type { ReactNode } from "react";
import { Link } from "react-router";
import type { Incident as IncidentData, Overview, Stage } from "../../api/types";
import { buttonClass } from "../../components/Button";
import { Card } from "../../components/Card";
import { ClaudeCommand } from "../../components/ClaudeCommand";
import { Disclosure } from "../../components/Disclosure";
import { EmptyState } from "../../components/EmptyState";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { StatusMark } from "../../components/StatusMark";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as agent } from "../../i18n/agent";
import { strings } from "../../i18n/bang-dieu-khien";
import { strings as khung } from "../../i18n/khung";
import { PendingPlans } from "../plan-cho-duyet/PendingPlans";
import { checksWith, fmtWhen, type Entry } from "./model";

/** Tiêu đề của một nhóm thẻ (các thẻ sự cố đứng riêng, không có thẻ chung bọc ngoài). */
function Heading({ id, children, count }: { id: string; children: ReactNode; count: number }) {
  return (
    <h2 id={id} className="m-0 flex items-baseline gap-[8px] text-[16px] leading-[1.4] font-semibold">
      {children}
      <span className="text-[13px] font-normal text-muted tabular-nums">{count}</span>
    </h2>
  );
}

/** Tên chỗ bị lỗi khi nó không phải một site: chính tpan hay cả máy. */
function useSharedName(stage: Stage): string {
  const t = useStrings(strings);
  return stage === "tpan" ? t.stage.tpan : t.sharedIncident;
}

/** Sự cố theo hợp đồng: target "" là máy và stack, "tpan" là chính tpan, còn lại là domain của site. */
function Incident({ inc, admin }: { inc: IncidentData; admin: boolean }) {
  const t = useStrings(strings);
  const kt = useStrings(khung);
  const at = useStrings(agent);
  const { lang } = useLang();
  const domain = inc.target === "" || inc.target === "tpan" ? null : inc.target;
  const stage = t.stage[inc.stage];
  const r = inc.open_result;
  const shared = useSharedName(inc.stage);
  const where = domain ?? shared;
  const summary = tx(r.summary, lang);

  return (
    <Card>
      <div className="flex items-start gap-[10px]">
        <span className="flex h-[21px] w-[20px] flex-none items-center justify-center">
          <StatusMark status="fail" label={kt.statusLabel(stage, kt.status.fail)} />
        </span>
        {/* Tối đa hai dòng; đủ chữ trong title. */}
        <h3 title={`${where} ${summary}`} className="m-0 line-clamp-2 min-w-0 text-[14px] leading-[1.45] font-semibold [overflow-wrap:anywhere]">
          {domain === null ? (
            where
          ) : (
            <Link to={`/sites/${encodeURIComponent(domain)}`} className="text-ink no-underline hover:text-ink hover:underline">
              {domain}
            </Link>
          )}{" "}
          {summary}
        </h3>
      </div>
      <p title={t.incidentMeta(stage, tx(inc.title, lang), fmtWhen(inc.opened, lang, t.yesterday))} className="mt-[4px] mb-0 truncate pl-[30px] text-[13px] text-muted">
        {t.incidentMeta(stage, tx(inc.title, lang), fmtWhen(inc.opened, lang, t.yesterday))}
      </p>
      <div className="mt-[8px]">
        {/* -ml cho chữ của nút thẳng với chữ phía trên (nút chữ có đệm 10 px). */}
        <Link to={`/incidents/${inc.id}`} aria-label={t.openIncidentFor(where)} className={buttonClass("text", "-ml-[10px]", "sm")}>
          {t.openIncident}
        </Link>
      </div>
      {/* Lệnh nhờ Claude Code chẩn đoán là phần AI: chỉ admin (mục 6.12). Đã nằm trong khối đóng sẵn. */}
      {admin && <ClaudeCommand prompt={at.diagnoseCheck(tx(inc.title, lang), domain ?? at.thisMachine)} />}
    </Card>
  );
}

/** Một cảnh báo là một dòng mở ra được: dấu cảnh báo, nơi, câu tóm tắt trên một dòng; mở ra là nguyên nhân và bằng chứng. */
function Warning({ e }: { e: Entry }) {
  const t = useStrings(strings);
  const kt = useStrings(khung);
  const { lang } = useLang();
  const shared = useSharedName(e.check.stage);
  const c = e.check;
  const where = e.domain ?? shared;
  const line = `${where} · ${tx(c.summary, lang)}`;
  const cause = tx(c.cause, lang);

  return (
    <Disclosure
      summary={
        <span className="flex items-center gap-[12px]">
          <span className="flex h-[20px] w-[20px] flex-none items-center justify-center">
            <StatusMark status="warn" label={kt.statusLabel(t.stage[c.stage], kt.status.warn)} />
          </span>
          <span title={line} className="block min-w-0 flex-1 truncate">
            <span className="font-medium">{where}</span> · {tx(c.summary, lang)}
          </span>
        </span>
      }
    >
      <div className="flex flex-col gap-[12px] pl-[32px]">
        {cause && <p className="m-0">{cause}</p>}
        {c.evidence && c.evidence.length > 0 ? <EvidenceBlock evidence={c.evidence} clamp={20} /> : <p className="m-0 text-muted">{t.noEvidence}</p>}
        {e.domain !== null && (
          <Link to={`/sites/${encodeURIComponent(e.domain)}`} className="self-start">
            {t.openSite}
          </Link>
        )}
      </div>
    </Disclosure>
  );
}

export function Side({ o, admin }: { o: Overview; admin: boolean }) {
  const t = useStrings(strings);
  const warns = checksWith(o, "warn");
  return (
    <aside className="flex min-w-0 flex-[1_1_340px] flex-col gap-[20px]">
      <section aria-labelledby="bdk-su-co" className="flex flex-col gap-[12px]">
        <Heading id="bdk-su-co" count={o.incidents.length}>
          {t.incidentsTitle}
        </Heading>
        {o.incidents.length === 0 ? (
          <Card>
            <EmptyState>{t.none}</EmptyState>
          </Card>
        ) : (
          o.incidents.map((inc) => <Incident key={inc.id} inc={inc} admin={admin} />)
        )}
        <p className="m-0 text-[13px]">
          <Link to="/incidents">{t.allIncidents}</Link>
        </p>
      </section>
      <Card title={t.warningsTitle} titleId="bdk-canh-bao" count={warns.length}>
        {warns.length === 0 ? (
          <EmptyState>{t.none}</EmptyState>
        ) : (
          <div>
            {warns.map((e) => (
              <Warning key={`${e.domain ?? "*"}:${e.check.id}`} e={e} />
            ))}
          </div>
        )}
      </Card>
      {/* Plan do AI hoặc agent đề xuất: chỉ admin thấy và duyệt (GET /plans là route admin). Khối này ở màn plan-cho-duyet, đợt 3 mới đổi sang bản 2. */}
      {admin && <PendingPlans />}
    </aside>
  );
}
