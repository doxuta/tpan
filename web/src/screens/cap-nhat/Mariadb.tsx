// Khối (c): MariaDB đang chạy và các dòng LTS nâng lên được. Nâng là một chiều,
// nên câu cảnh báo nằm ngay trước các nút và được nối vào từng nút bằng
// aria-describedby: người dùng trình đọc màn hình nghe cảnh báo cùng tên nút.

import type { MariaDBStatus } from "../../api/types";
import { Button } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/cap-nhat";
import { mariadbAction, type Flow } from "./model";

const labelClass = "m-0 font-mono text-[11px] font-normal tracking-[0.08em] text-muted";

export function Mariadb({ status, flow }: { status: MariaDBStatus; flow: Flow }) {
  const t = useStrings(strings);
  // Go có thể trả null cho danh sách rỗng.
  const upgrades = status.upgrades ?? [];
  const installed = status.version !== "" || status.line !== "";

  return (
    <section aria-labelledby="cn-maria" className="pt-[26px]">
      <h2 id="cn-maria" className="m-0 pb-[10px] text-[16px] font-semibold">
        {t.mariaHeading}
      </h2>

      {!installed ? (
        <p className="m-0 border-t border-hair py-[10px] text-muted">{t.mariaNotInstalled}</p>
      ) : (
        <>
          <dl className="m-0 grid grid-cols-2 gap-x-[16px] gap-y-[12px] border-t border-hair pt-[12px] sm:flex sm:flex-wrap sm:gap-x-[40px]">
            <div>
              <dt className={labelClass}>{t.mariaSource}</dt>
              <dd className="m-0 mt-[4px]">
                <MonoText>{status.source}</MonoText>
              </dd>
            </div>
            <div>
              <dt className={labelClass}>{t.mariaLine}</dt>
              <dd className="m-0 mt-[4px]">
                <MonoText>{status.line}</MonoText>
              </dd>
            </div>
            <div>
              <dt className={labelClass}>{t.mariaVersion}</dt>
              <dd className="m-0 mt-[4px]">
                <MonoText>{status.version}</MonoText>
              </dd>
            </div>
          </dl>

          <h3 className={`mt-[18px] mb-[6px] uppercase ${labelClass}`}>{t.mariaUpgrades}</h3>
          {upgrades.length === 0 ? (
            <p className="m-0 max-w-[720px] text-muted">{t.mariaNone}</p>
          ) : (
            <>
              <p id="cn-maria-warn" className="m-0 mb-[10px] max-w-[720px] font-medium">
                {t.mariaWarn}
              </p>
              <ul aria-label={t.mariaUpgradesLabel} className="m-0 list-none border-t border-hair p-0">
                {upgrades.map((u) => {
                  const action = mariadbAction(u.line);
                  return (
                    <li key={u.line} className="flex flex-wrap items-center justify-between gap-x-[16px] gap-y-[8px] border-t border-hair py-[10px] first:border-t-0">
                      <span className="min-w-0">
                        <span className="font-mono text-[13px]">{u.line}</span>
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
              <p className="mt-[8px] mb-0 text-muted">{t.previewHint}</p>
            </>
          )}
        </>
      )}
    </section>
  );
}
