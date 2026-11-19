// Cột bên của Bảng điều khiển (mục 7): "Sự cố đang mở", "Cảnh báo", "Chờ bạn
// duyệt". Khối nào không có gì thì ghi một dòng "Không có", không ẩn đi.
//
// M3 chưa có màn Sự cố riêng, nên mỗi phép kiểm Fail là một thẻ sự cố; mỗi
// phép kiểm Warn là một thẻ cảnh báo. Sửa tự động (M4) và Hỏi AI (M8) chưa có,
// nên hai nút đó vô hiệu và nói lý do ngay trong thẻ; Sửa tay dùng được ngay.

import { useId, useState, type ReactNode } from "react";
import { Link } from "react-router";
import type { Overview } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { EvidenceBlock } from "../../components/EvidenceBlock";
import { StatusMark } from "../../components/StatusMark";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/bang-dieu-khien";
import { strings as khung } from "../../i18n/khung";
import { checksWith, type Entry } from "./model";

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

/** Domain của site (liên kết tới trang site) hoặc nhãn "Cả máy" cho phép kiểm dùng chung. */
function Where({ domain, size }: { domain: string | null; size: 13 | 15 }) {
  const t = useStrings(strings);
  if (domain === null) return <>{t.sharedIncident}</>;
  return (
    <Link to={`/sites/${encodeURIComponent(domain)}`} className={`font-mono font-semibold text-ink no-underline hover:text-ink hover:underline ${size === 15 ? "text-[15px]" : "text-[13px]"}`}>
      {domain}
    </Link>
  );
}

function Incident({ e }: { e: Entry }) {
  const t = useStrings(strings);
  const kt = useStrings(khung);
  const { lang } = useLang();
  const [manual, setManual] = useState(false);
  const reasonId = useId();
  const c = e.check;
  const stage = t.stage[c.stage];
  const lines = c.manual_fix ?? [];

  return (
    <Card incident>
      <div className="flex items-center gap-[10px]">
        <StatusMark status="fail" label={kt.statusLabel(stage, kt.status.fail)} />
        <h3 className="m-0 text-[17px] leading-[1.3] font-semibold">
          <Where domain={e.domain} size={15} /> {tx(c.summary, lang)}
        </h3>
      </div>
      <p className="mt-[6px] mb-0 text-[13px] text-muted">{t.incidentMeta(e.domain ?? t.sharedIncident, stage, tx(c.title, lang))}</p>
      {c.cause && <p className="mt-[14px] mb-0 leading-[1.5]">{tx(c.cause, lang)}</p>}
      {c.evidence && c.evidence.length > 0 && (
        <div className="mt-[16px]">
          <EvidenceBlock evidence={c.evidence} />
        </div>
      )}
      <div className="mt-[16px] flex flex-wrap gap-[8px]">
        {/* aria-disabled, không phải disabled: nút vẫn focus được nên người dùng bàn phím nghe được lý do. */}
        <Button variant="primary" aria-disabled="true" aria-describedby={reasonId}>
          {t.autoFix}
        </Button>
        <Button aria-expanded={manual} disabled={lines.length === 0} onClick={() => setManual(!manual)}>
          {t.manualFix}
        </Button>
        <Button aria-disabled="true" aria-describedby={reasonId}>
          {t.askAi}
        </Button>
      </div>
      <p id={reasonId} className="mt-[8px] mb-0 text-[12px] text-muted">
        {t.fixesReason}
      </p>
      {manual && (
        <div className="mt-[12px]">
          <EvidenceBlock label={t.manualLabel} text={lines.map((l) => `$ ${l}`).join("\n")} />
        </div>
      )}
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
          <Where domain={e.domain} size={13} /> · {tx(c.summary, lang)}
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

export function Side({ o }: { o: Overview }) {
  const t = useStrings(strings);
  const fails = checksWith(o, "fail");
  const warns = checksWith(o, "warn");
  return (
    <aside className="flex min-w-0 flex-[1_1_372px] flex-col gap-[24px] pt-[24px]">
      <Block title={t.incidentsTitle} count={fails.length}>
        {fails.length === 0 ? (
          <None />
        ) : (
          <div className="flex flex-col gap-[12px]">
            {fails.map((e) => (
              <Incident key={`${e.domain ?? "*"}:${e.check.id}`} e={e} />
            ))}
          </div>
        )}
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
      {/* M3 chưa có Plan nào chờ duyệt (Plan do AI hay agent đề xuất đến từ M8). */}
      <Block title={t.pendingTitle} count={0}>
        <None />
      </Block>
    </aside>
  );
}
