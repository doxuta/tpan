// Cột bên của Bảng điều khiển (mục 7): "Sự cố đang mở", "Cảnh báo", "Chờ bạn
// duyệt". Khối nào không có gì thì ghi một dòng "Không có", không ẩn đi.
//
// Sự cố đọc từ overview.incidents (health mở khi một phép kiểm chuyển sang
// lỗi, mục 6.7), không còn suy từ phép kiểm lỗi như M3. Ba cách sửa (tự động
// qua xem trước, sửa tay, hỏi AI) nằm ở trang sự cố /incidents/:id, nên thẻ ở
// đây chỉ dẫn tới đó; cảnh báo vẫn là phép kiểm warn và mở bằng chứng tại chỗ.

import { useState, type ReactNode } from "react";
import { Link } from "react-router";
import type { Incident as IncidentData, Overview, Stage } from "../../api/types";
import { Button, buttonClass } from "../../components/Button";
import { Card } from "../../components/Card";
import { ClaudeCommand } from "../../components/ClaudeCommand";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { StatusMark } from "../../components/StatusMark";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as agent } from "../../i18n/agent";
import { strings } from "../../i18n/bang-dieu-khien";
import { strings as khung } from "../../i18n/khung";
import { PendingPlans } from "../plan-cho-duyet/PendingPlans";
import { checksWith, fmtWhen, type Entry } from "./model";

function Block({ title, count, children }: { title: string; count: number; children: ReactNode }) {
  return (
    <section>
      <h2 className="m-0 mb-[10px] flex items-baseline gap-[8px] text-[16px] font-semibold">
        {title}
        <span className="font-mono text-[13px] font-normal text-muted">{count}</span>
      </h2>
      {children}
    </section>
  );
}

function None() {
  const t = useStrings(strings);
  return <p className="m-0 text-muted">{t.none}</p>;
}

/** Tên chỗ bị lỗi khi nó không phải một site: chính tpan hay cả máy. */
function useSharedName(stage: Stage): string {
  const t = useStrings(strings);
  return stage === "tpan" ? t.stage.tpan : t.sharedIncident;
}

/** Domain của site (liên kết tới trang site) hoặc tên "Cả máy" / "TPAN" cho phép kiểm không thuộc site nào. */
function Where({ domain, stage, size }: { domain: string | null; stage: Stage; size: 13 | 15 }) {
  const shared = useSharedName(stage);
  if (domain === null) return <>{shared}</>;
  return (
    <Link to={`/sites/${encodeURIComponent(domain)}`} className={`font-mono font-semibold text-ink no-underline hover:text-ink hover:underline ${size === 15 ? "text-[15px]" : "text-[13px]"}`}>
      {domain}
    </Link>
  );
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

  return (
    <Card incident>
      <div className="flex items-center gap-[10px]">
        <StatusMark status="fail" label={kt.statusLabel(stage, kt.status.fail)} />
        <h3 className="m-0 text-[17px] leading-[1.3] font-semibold">
          <Where domain={domain} stage={inc.stage} size={15} /> {tx(r.summary, lang)}
        </h3>
      </div>
      <p className="mt-[6px] mb-0 text-[13px] text-muted">{t.incidentMeta(domain ?? shared, stage, tx(inc.title, lang), fmtWhen(inc.opened, lang, t.yesterday))}</p>
      {r.cause && <p className="mt-[14px] mb-0 leading-[1.5]">{tx(r.cause, lang)}</p>}
      <div className="mt-[16px]">
        <Link to={`/incidents/${inc.id}`} className={buttonClass("secondary")}>
          {t.openIncident}
        </Link>
      </div>
      {/* Lệnh nhờ Claude Code chẩn đoán là phần AI: chỉ admin (mục 6.12). */}
      {admin && <ClaudeCommand prompt={at.diagnoseCheck(tx(inc.title, lang), domain ?? at.thisMachine)} />}
    </Card>
  );
}

function Warning({ e }: { e: Entry }) {
  const t = useStrings(strings);
  const kt = useStrings(khung);
  const { lang } = useLang();
  const [shown, setShown] = useState(false);
  const c = e.check;
  return (
    <Card>
      <div className="flex items-center gap-[12px]">
        <StatusMark status="warn" label={kt.statusLabel(t.stage[c.stage], kt.status.warn)} />
        <div className="leading-[1.45]">
          <Where domain={e.domain} stage={c.stage} size={13} /> · {tx(c.summary, lang)}
        </div>
      </div>
      {c.cause && <p className="mt-[8px] mb-0 text-[13px] text-muted">{tx(c.cause, lang)}</p>}
      {c.evidence && c.evidence.length > 0 && (
        <>
          <div className="mt-[12px]">
            <Button aria-expanded={shown} onClick={() => setShown(!shown)}>
              {shown ? t.hideEvidence : t.showEvidence}
            </Button>
          </div>
          {shown && (
            <div className="mt-[12px]">
              <EvidenceBlock evidence={c.evidence} />
            </div>
          )}
        </>
      )}
    </Card>
  );
}

export function Side({ o, admin }: { o: Overview; admin: boolean }) {
  const t = useStrings(strings);
  const warns = checksWith(o, "warn");
  return (
    <aside className="flex min-w-0 flex-[1_1_372px] flex-col gap-[24px] pt-[24px]">
      <Block title={t.incidentsTitle} count={o.incidents.length}>
        {o.incidents.length === 0 ? (
          <None />
        ) : (
          <div className="flex flex-col gap-[12px]">
            {o.incidents.map((inc) => (
              <Incident key={inc.id} inc={inc} admin={admin} />
            ))}
          </div>
        )}
        <p className="mt-[12px] mb-0 text-[13px]">
          <Link to="/incidents">{t.allIncidents}</Link>
        </p>
      </Block>
      <Block title={t.warningsTitle} count={warns.length}>
        {warns.length === 0 ? (
          <None />
        ) : (
          <div className="flex flex-col gap-[12px]">
            {warns.map((e) => (
              <Warning key={`${e.domain ?? "*"}:${e.check.id}`} e={e} />
            ))}
          </div>
        )}
      </Block>
      {/* Plan do AI hoặc agent đề xuất: chỉ admin thấy và duyệt (GET /plans là route admin). */}
      {admin && <PendingPlans />}
    </aside>
  );
}
