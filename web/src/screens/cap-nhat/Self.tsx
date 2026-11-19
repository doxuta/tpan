// Khối (d): cập nhật chính TPAN. Không có nút và không có API: thay file chạy từ
// chính panel là cắt job đang chạy (kể cả job cập nhật này), nên màn chỉ hiện
// lệnh để người tự gõ trên máy, bằng CommandBlock.

import type { SelfStatus } from "../../api/types";
import { CommandBlock } from "../../components/CommandBlock";
import { MonoText } from "../../components/MonoText";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/cap-nhat";

const labelClass = "m-0 font-mono text-[11px] font-normal tracking-[0.08em] text-muted";

export function Self({ status }: { status: SelfStatus }) {
  const t = useStrings(strings);
  // Lệnh thứ ba (quay về) chỉ có khi máy còn giữ bản cũ.
  const commands = ["sudo tpan self-update --dry-run", "sudo tpan self-update", ...(status.has_old ? ["sudo tpan self-update --rollback"] : [])];

  return (
    <section aria-labelledby="cn-self" className="pt-[26px] pb-[8px]">
      <h2 id="cn-self" className="m-0 pb-[10px] text-[16px] font-semibold">
        {t.selfHeading}
      </h2>
      <dl className="m-0 flex flex-wrap gap-x-[40px] gap-y-[12px] border-t border-hair pt-[12px]">
        <div>
          <dt className={labelClass}>{t.selfVersion}</dt>
          <dd className="m-0 mt-[4px]">
            <MonoText>{status.version}</MonoText>
          </dd>
        </div>
        <div className="min-w-0">
          <dt className={labelClass}>{t.selfUrl}</dt>
          <dd className="m-0 mt-[4px]">
            <MonoText>{status.url}</MonoText>
          </dd>
        </div>
      </dl>
      <p className="mt-[14px] mb-0 max-w-[720px]">{t.selfWhy}</p>
      <p className="mt-[6px] mb-[10px] max-w-[720px] text-muted">
        {t.selfDry} {t.selfRun}
        {status.has_old && ` ${t.selfRollback}`}
      </p>
      <h3 className={`mb-[6px] uppercase ${labelClass}`}>{t.selfCommands}</h3>
      <CommandBlock commands={commands} />
    </section>
  );
}
