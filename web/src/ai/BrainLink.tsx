// Nút "Nối bộ não" một lần bấm (dùng ở ngăn AI và ở Cài đặt → AI và bộ não).
//
// Trang web nằm trên máy chủ nên không chạy được gì trên máy cá nhân: nút chỉ
// mở link tpan-brain://connect. Mac nào đã chạy `tpan brain-install <đích>`
// thì macOS mở app TPAN Brain, app mở Terminal chạy `tpan brain <đích>` (đích
// ghi lúc cài, link không mang tham số). Chưa cài thì trình duyệt im lặng, nên
// sau vài giây chưa thấy nối thì mở sẵn cách cài. Trong lúc chờ, hỏi lại tình
// trạng bộ não mỗi 2 giây (tối đa 60 giây) để "Đã nối" hiện ngay.

import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Button } from "../components/Button";
import { CommandBlock } from "../components/CommandBlock";
import { Disclosure } from "../components/Disclosure";
import { useStrings } from "../i18n";
import { strings } from "../i18n/ai";

export const brainLink = "tpan-brain://connect";
const pollMs = 2_000;
const slowMs = 8_000;
const giveUpMs = 60_000;

/** Lệnh cài nút trên Mac, chạy trong thư mục mã nguồn TPAN (make build dựng tpan cho macOS). */
export function brainInstallCommands(target: string): string[] {
  return ["make build", `bin/tpan brain-install ${target}`];
}

export function BrainLinkButton({ target }: { target: string }) {
  const t = useStrings(strings);
  const qc = useQueryClient();
  const [startedAt, setStartedAt] = useState<number | null>(null);
  const [elapsed, setElapsed] = useState(0);
  const [setupOpen, setSetupOpen] = useState(false);
  const waiting = startedAt !== null && elapsed < giveUpMs;

  useEffect(() => {
    if (startedAt === null) return;
    const id = window.setInterval(() => {
      const e = Date.now() - startedAt;
      setElapsed(e);
      if (e >= slowMs) setSetupOpen(true);
      if (e >= giveUpMs) window.clearInterval(id);
      else void qc.invalidateQueries({ queryKey: ["ai", "brain"] });
    }, pollMs);
    return () => window.clearInterval(id);
  }, [startedAt, qc]);

  const press = () => {
    setStartedAt(Date.now());
    setElapsed(0);
    window.location.assign(brainLink);
  };

  return (
    <div>
      <div className="flex flex-wrap items-center gap-x-[12px] gap-y-[6px]">
        <Button variant="primary" onClick={press}>
          {t.linkButton}
        </Button>
        <span aria-live="polite" className="text-[13px] text-muted">
          {startedAt === null ? "" : waiting ? t.linkWaiting : t.linkGaveUp}
        </span>
      </div>
      <p className="mt-[6px] mb-0 text-[13px] text-muted">{t.linkHint}</p>
      <div className="mt-[8px]">
        <Disclosure flush open={setupOpen} onOpenChange={setSetupOpen} summary={<span className="text-[14px] font-medium">{t.linkSetup}</span>}>
          <p className="mt-0 mb-[8px] text-[13px] text-muted">{t.linkSetupBody}</p>
          <CommandBlock commands={brainInstallCommands(target)} addSudo={false} where={t.linkSetupWhere} />
        </Disclosure>
      </div>
    </div>
  );
}
