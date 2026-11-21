// Khối (c): MariaDB đang chạy và các dòng LTS nâng lên được. Nâng là một chiều,
// nên câu cảnh báo nằm ngay trước các nút và được nối vào từng nút bằng
// aria-describedby: người dùng trình đọc màn hình nghe cảnh báo cùng tên nút.

import type { MariaDBStatus } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { EmptyState } from "../../components/EmptyState";
import { Code, MonoText } from "../../components/MonoText";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/cap-nhat";
import { Fact, Facts } from "../cai-dat/Rows";
import { mariadbAction, type Flow } from "./model";

export function Mariadb({ status, flow }: { status: MariaDBStatus; flow: Flow }) {
  const t = useStrings(strings);
  // Go có thể trả null cho danh sách rỗng.
  const upgrades = status.upgrades ?? [];
  const installed = status.version !== "" || status.line !== "";

  return (
    <Card title={t.mariaHeading} titleId="cn-maria">
      {!installed ? (
        <EmptyState>{t.mariaNotInstalled}</EmptyState>
      ) : (
        <>
          <Facts>
            <Fact label={t.mariaSource}>
              <MonoText>{status.source}</MonoText>
            </Fact>
            <Fact label={t.mariaLine}>
              <MonoText>{status.line}</MonoText>
            </Fact>
            <Fact label={t.mariaVersion}>
              <MonoText>{status.version}</MonoText>
            </Fact>
          </Facts>

          <h3 className="m-0 mt-[16px] mb-[8px] border-t border-hair pt-[16px] text-[14px] font-semibold">{t.mariaUpgrades}</h3>
          {upgrades.length === 0 ? (
            <p className="m-0 max-w-[72ch] text-muted">{t.mariaNone}</p>
          ) : (
            <>
              <p id="cn-maria-warn" className="m-0 mb-[8px] max-w-[72ch] font-medium">
                {t.mariaWarn}
              </p>
              <ul aria-label={t.mariaUpgradesLabel} className="m-0 list-none p-0">
                {upgrades.map((u) => {
                  const action = mariadbAction(u.line);
                  return (
                    <li key={u.line} className="flex flex-wrap items-center justify-between gap-x-[16px] gap-y-[8px] border-t border-hair py-[10px]">
                      <span className="min-w-0">
                        <Code>{u.line}</Code>
                        {u.eol && <span className="text-muted"> · {t.mariaEol(u.eol)}</span>}
                      </span>
                      <Button id={`act-${action.id}`} aria-describedby="cn-maria-warn" onClick={() => flow.start(action)} disabled={flow.busy}>
                        {flow.pending === action.id ? t.previewing : t.mariaUpgradeButton(u.line)}
                      </Button>
                      {flow.errorFor(action) && <div className="basis-full">{flow.errorFor(action)}</div>}
                    </li>
                  );
                })}
              </ul>
              <p className="mt-[8px] mb-0 text-[13px] text-muted">{t.previewHint}</p>
            </>
          )}
        </>
      )}
    </Card>
  );
}
