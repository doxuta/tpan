// Khối "Chế độ .htaccess" của trang một site (tab Cài đặt site; mục 6.13 của
// KE-HOACH.md), chỉ hiện khi web server là Apache: trên nginx không có file cấu hình
// nào do site ghi được, nên cài đặt này không có tác dụng.
//
// Ba chế độ: an-toan (danh sách chỉ thị cho phép), tat (Apache không đọc
// .htaccess), day-du (mọi chỉ thị; chỉ admin đặt được, kèm lời cảnh báo). Đổi
// chế độ đi qua ba chặng như mọi thao tác: chọn → POST
// /sites/{domain}/htaccess?dry_run=1 → PlanReview → POST thật → /jobs/:id.
//
// Sang an-toan mà .htaccess có chỉ thị ngoài danh sách thì máy chủ từ chối
// (htaccess_blocked) và liệt kê từng dòng vướng; lời đó hiện nguyên văn.

import { useEffect, useRef, useState } from "react";
import { usePlanFlow, useWho } from "../../api/m13-users";
import { htaccessRequest } from "../../api/m14";
import type { HtaccessMode } from "../../api/m14-types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Code, MonoText } from "../../components/MonoText";
import { useStrings } from "../../i18n";
import { strings as chung } from "../../i18n/m13-chung";
import { strings } from "../../i18n/site";
import { PlanReview } from "../duyet-plan";
import { ErrorNote } from "./ErrorNote";

const modes: HtaccessMode[] = ["an-toan", "tat", "day-du"];

type Props = {
  domain: string;
  /** SiteInfo.site.htaccess */
  mode: HtaccessMode;
  /** Lý do chưa đổi được (site đang tạm khoá). Có thì nút vô hiệu và lý do hiện ngay dưới. */
  blocked?: string;
};

export function HtaccessBlock({ domain, mode, blocked }: Props) {
  const t = useStrings(strings);
  const m = useStrings(chung);
  // Máy chủ mới là nơi quyết ai đặt được day-du (403 cho user thường); ở đây chỉ khoá lựa chọn để khỏi bấm rồi mới nghe từ chối.
  const { isAdmin } = useWho();
  const flow = usePlanFlow();
  const [choice, setChoice] = useState<HtaccessMode>(mode);
  /** Chế độ đã gửi lúc xem trước: người dùng đổi lựa chọn khi bản xem trước đang mở thì lời cảnh báo và lỗi vẫn nói đúng bản đã xem. */
  const [asked, setAsked] = useState<HtaccessMode>(mode);
  const resultRef = useRef<HTMLDivElement>(null);

  const shown = flow.open ?? (flow.dry.isError ? flow.asked : null);
  useEffect(() => {
    if (shown) {
      resultRef.current?.scrollIntoView({ block: "start" });
      resultRef.current?.focus();
    }
  }, [shown]);

  const preview = () => {
    setAsked(choice);
    flow.ask(htaccessRequest(domain, choice));
  };

  return (
    <>
      <Card title={t.htHeading} titleId="htaccess-site">
        <dl className="m-0">
          <dt className="m-0 text-[12px] font-medium text-muted">{t.htCurrent}</dt>
          <dd className="m-0 mt-[4px]">
            <MonoText>{mode}</MonoText>
          </dd>
        </dl>

        <fieldset className="m-0 mt-[14px] min-w-0 border-0 p-0">
          <legend className="mb-[6px] p-0 text-[14px] font-medium">{t.htChoose}</legend>
          {modes.map((v) => {
            // day-du chỉ admin chọn được. Site đang ở day-du thì ô vẫn bật, để người vừa chọn chế độ khác quay lại được.
            const locked = v === "day-du" && !isAdmin && mode !== "day-du";
            return (
              <label key={v} className="flex max-w-[72ch] items-start gap-[10px] border-t border-hair py-[10px]">
                <input type="radio" name="htaccess-mode" value={v} checked={choice === v} disabled={locked} onChange={() => setChoice(v)} className="mt-[1px] size-[20px] flex-none accent-accent" />
                <span className="min-w-0">
                  <Code>{v}</Code>
                  {v === mode && <span className="text-muted"> · {t.htCurrentTag}</span>}
                  <span className="block text-muted">{t.htModes[v]}</span>
                  {locked && <span className="block text-muted">{t.htAdminOnly}</span>}
                </span>
              </label>
            );
          })}
        </fieldset>

        {/* Chữ đậm, không đỏ: đỏ chỉ dành cho tình trạng máy (mục 2 của GIAO-DIEN.md). */}
        {choice === "day-du" && mode !== "day-du" && !flow.open && (
          <p role="alert" className="mt-[4px] mb-0 max-w-[72ch] font-semibold">
            {t.htDayDuWarn}
          </p>
        )}

        <div className="pt-[12px]">
          <Button onClick={preview} aria-describedby={blocked ? "htaccess-bi-chan" : undefined} disabled={flow.dry.isPending || flow.run.isPending || choice === mode || blocked !== undefined}>
            {flow.dry.isPending ? m.previewing : t.htPreview}
          </Button>
        </div>
        {blocked && (
          <p id="htaccess-bi-chan" className="mt-[10px] mb-0 max-w-[72ch] text-[13px] text-muted">
            {blocked}
          </p>
        )}
        <p className="mt-[10px] mb-0 max-w-[72ch] text-muted">{t.htHint}</p>

        {/* Lời từ chối (kể cả htaccess_blocked với danh sách dòng vướng) hiện nguyên thông điệp của máy chủ; ErrorNote giữ chỗ xuống dòng. */}
        {flow.dry.isError && flow.asked && (
          <div ref={resultRef} tabIndex={-1} className="pt-[12px] outline-offset-0">
            <ErrorNote error={flow.dry.error} heading={t.htFailed(asked)} checksHeading={m.checksHeading} />
          </div>
        )}
      </Card>
      {flow.open && (
        <div ref={resultRef} tabIndex={-1} className="flex flex-col gap-[20px] outline-offset-0">
          {asked === "day-du" && (
            <p role="alert" className="m-0 max-w-[72ch] font-semibold">
              {t.htDayDuWarn}
            </p>
          )}
          <PlanReview preview={flow.open.preview} onRun={flow.start} running={flow.run.isPending} error={flow.run.error} onCancel={flow.close} />
        </div>
      )}
    </>
  );
}
