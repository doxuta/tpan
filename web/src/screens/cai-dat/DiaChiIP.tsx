// Mục "Địa chỉ IP" ở Cài đặt (admin, chỉ đọc): IP trên card mạng của máy,
// panel nghe ở đâu, bản ghi DNS của từng site trỏ về IP nào, và ô kiểm trước
// khi chuyển máy sang một IP mới. Không đổi cấu hình mạng hay DNS: thêm IP vào
// máy là việc của nhà cung cấp VPS, bản ghi DNS nằm ở nơi quản lý tên miền.

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { errorMessage, get } from "../../api/client";
import type { MachineIPs, SiteDNS } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { TextField } from "../../components/Field";
import { MonoText } from "../../components/MonoText";
import { Notice } from "../../components/Notice";
import { StatusLine } from "../../components/StatusMark";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/cai-dat";
import { strings as khung } from "../../i18n/khung";
import { planMove } from "./ip-model";
import { Row, Rows } from "./Rows";

export const machineIPsKey = ["machine", "ips"] as const;

export function DiaChiIP() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({ queryKey: machineIPsKey, queryFn: () => get<MachineIPs>("/api/v1/machine/ips"), retry: false });
  const [newIP, setNewIP] = useState("");
  const d = q.data;
  const plan = d && newIP.trim() !== "" ? planMove(d, newIP) : null;
  const host = window.location.hostname;

  const dnsCell = (s: SiteDNS) => {
    if (s.error) return <StatusLine status="unknown">{t.ipDnsError}</StatusLine>;
    if (s.a.length === 0) return <StatusLine status="fail">{t.ipDnsNone}</StatusLine>;
    return <StatusLine status={s.here ? "pass" : "warn"}>{s.here ? t.ipDnsHere : t.ipDnsElsewhere}</StatusLine>;
  };

  return (
    <>
      <Card title={t.ipHeading} titleId="dia-chi-ip-tieu-de">
        <p className="mt-0 mb-[14px] max-w-[72ch] text-muted">{t.ipIntro}</p>
        {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
        {q.isError && (
          <div role="alert">
            <Notice kind="fail">
              {t.ipLoadFailed} {errorMessage(q.error, lang)}
            </Notice>
            <Button className="mt-[12px]" onClick={() => void q.refetch()}>
              {k.retry}
            </Button>
          </div>
        )}
        {d && (
          <Rows>
            <Row label={t.ipHostLabel}>
              <MonoText>{d.hostname}</MonoText>
            </Row>
            {d.os && <Row label={t.ipOSLabel}>{d.os}</Row>}
            <Row label={t.ipAddrsLabel}>
              {d.addrs.length === 0 && <p className="m-0 text-muted">{t.ipNoAddrs}</p>}
              <ul className="m-0 flex list-none flex-col gap-[6px] p-0">
                {d.addrs.map((a) => (
                  <li key={a.iface + a.ip} className="flex flex-wrap items-center gap-[8px]">
                    <MonoText>{a.ip}</MonoText>
                    <Tag>{a.public ? t.ipPublic : t.ipPrivate}</Tag>
                    <span className="text-[13px] text-muted">{t.ipIface(a.iface, a.family === "ipv4" ? "IPv4" : "IPv6")}</span>
                  </li>
                ))}
              </ul>
              {!d.addrs.some((a) => a.public && a.family === "ipv4") && <p className="mt-[8px] mb-0 text-[13px] text-muted">{t.ipNatHint}</p>}
            </Row>
            <Row label={t.ipPanelLabel}>
              {d.panel_private ? (
                <StatusLine status="warn">{t.ipPanelPrivate(d.panel_port)}</StatusLine>
              ) : (
                <StatusLine status="pass">{t.ipPanelPublic(d.panel_port)}</StatusLine>
              )}
            </Row>
            <Row label={t.ipBrowserLabel}>
              <MonoText>{host}</MonoText>
            </Row>
          </Rows>
        )}
      </Card>

      {d && (
        <TableList
          title={t.ipSitesHeading}
          titleId="dns-cac-site"
          count={d.sites.length}
          label={t.ipSitesHeading}
          minWidth={640}
          rows={d.sites}
          rowKey={(s) => s.domain}
          empty={t.ipNoSites}
          columns={[
            { header: t.ipColDomain, cell: (s) => <MonoText>{s.domain}</MonoText>, nowrap: false },
            { header: t.ipColA, cell: (s) => (s.a.length ? <MonoText>{s.a.join(", ")}</MonoText> : "—"), nowrap: false },
            { header: t.ipColDns, cell: dnsCell },
          ]}
          footer={<span>{t.ipSitesFoot}</span>}
        />
      )}

      {d && (
        <Card title={t.ipMoveHeading} titleId="chuyen-ip-moi">
          <p className="mt-0 mb-[14px] max-w-[72ch] text-muted">{t.ipMoveIntro}</p>
          <div className="max-w-[360px]">
            <TextField
              id="ip-moi"
              label={t.ipMoveLabel}
              hint={t.ipMoveHint}
              autoComplete="off"
              spellCheck={false}
              inputMode="decimal"
              placeholder="203.0.113.10"
              value={newIP}
              onChange={(e) => setNewIP(e.target.value)}
            />
          </div>
          <div aria-live="polite" className="mt-[14px]">
            {newIP.trim() !== "" && !plan && <StatusLine status="fail">{t.ipMoveInvalid}</StatusLine>}
            {plan && (
              <ol className="m-0 flex max-w-[760px] flex-col gap-[10px] pl-[20px]">
                <li>
                  {plan.onMachine ? (
                    <StatusLine status="pass">{t.ipMoveOnMachine}</StatusLine>
                  ) : (
                    <StatusLine status="warn">{t.ipMoveNotOnMachine}</StatusLine>
                  )}
                </li>
                <li>
                  {plan.toFix.length === 0 ? (
                    <StatusLine status="pass">{t.ipMoveDnsDone}</StatusLine>
                  ) : (
                    <>
                      <StatusLine status="warn">{t.ipMoveDnsFix(plan.toFix.length, plan.family === "ipv4" ? "A" : "AAAA")}</StatusLine>
                      <p className="mt-[4px] mb-0 text-[13px]">
                        <MonoText>{plan.toFix.join(", ")}</MonoText>
                      </p>
                    </>
                  )}
                </li>
                <li>{t.ipMoveFirewall}</li>
                <li>{t.ipMoveSsh}</li>
                <li>{plan.panelPrivate ? <StatusLine status="warn">{t.ipMovePanelPrivate}</StatusLine> : t.ipMovePanel(newIP.trim(), d.panel_port)}</li>
                <li>{t.ipMoveCert}</li>
              </ol>
            )}
          </div>
        </Card>
      )}
    </>
  );
}
