// Khối "Phiên bản và cập nhật" (mục 6.14 của KE-HOACH.md): cập nhật chính TPAN
// ngay trong panel. Dùng ở màn Cài đặt và là khối (d) của màn Cập nhật; tự lấy
// dữ liệu (GET /api/v1/self, chỉ admin) nên nơi dùng chỉ việc đặt <Self />.
// id="phien-ban" để dòng phiên bản cuối trang dẫn tới (/settings#phien-ban;
// việc cuộn tới neo do Layout làm).
//
// Từ trên xuống:
//   bản đang chạy, nguồn tải
//   đang có lượt chạy  → SelfProgress, không nút nào (hỏi lại mỗi 3 giây)
//   có lượt gần nhất   → SelfResult (kết quả, hoặc dòng "lượt gần nhất")
//   không có lượt chạy → "Kiểm bản mới" → bản xem trước → "Cập nhật lên <bản>"
//                        → hộp hỏi lại → POST thật; và "Quay về bản trước"
//   một dòng nhắc lệnh CLI cùng việc
//
// Khối đang ở chặng nào do hàm thuần selfPhase quyết định (selfPhase.ts).

import { useEffect, useState, type ReactNode } from "react";
import { selfPollMs, selfRunning, useSelf, useSelfCheck, useSelfSessionPoll, useSelfStart } from "../../api/m15";
import type { SelfCheck, SelfState } from "../../api/m15-types";
import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { MonoText } from "../../components/MonoText";
import { StatusMark } from "../../components/StatusMark";
import { tx, useLang, useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/phien-ban";
import { formatDateTime } from "../lich-su/format";
import { ErrorNote } from "../site/ErrorNote";
import { selfPhase } from "./selfPhase";
import { SelfRollback } from "./SelfRollback";
import { labelClass, markBox, SelfProgress, SelfResult } from "./SelfRun";

export function Self() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const q = useSelf();

  return (
    // tabIndex -1: Layout đưa focus tới đây khi người dùng theo liên kết #phien-ban.
    <section id="phien-ban" aria-labelledby="pb-tieu-de" tabIndex={-1} className="scroll-mt-[12px] pt-[28px] outline-offset-0">
      <h2 id="pb-tieu-de" className="m-0 mb-[10px] text-[16px] font-semibold">
        {t.heading}
      </h2>
      {q.data ? (
        // Đã có dữ liệu thì lần hỏi lại hỏng (panel đang restart) không xoá khối: vẫn vẽ theo dữ liệu đang giữ.
        <Loaded state={q.data} unreachable={q.isError} />
      ) : q.isError ? (
        <>
          <ErrorNote error={q.error} heading={t.loadError} />
          <Button className="mt-[12px]" onClick={() => void q.refetch()}>
            {k.retry}
          </Button>
        </>
      ) : (
        <p className="m-0 text-muted">{k.loading}</p>
      )}
    </section>
  );
}

/** Đồng hồ cho luật "quá 3 phút": chỉ chạy trong lúc có lượt đang chạy. */
function useNow(ticking: boolean): number {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (!ticking) return;
    const id = setInterval(() => setNow(Date.now()), selfPollMs);
    return () => clearInterval(id);
  }, [ticking]);
  return now;
}

// Nằm ngoài component để còn nhớ khi người dùng sang màn khác rồi quay lại:
// nó sống tới lúc tải lại trang, đúng bằng tuổi của mã giao diện đang mở.
let witnessed = false;

function Loaded({ state, unreachable }: { state: SelfState; unreachable: boolean }) {
  const t = useStrings(strings);
  const live = selfRunning(state);
  const session = useSelfSessionPoll(state.last, live);
  const phase = selfPhase(state, session.data?.version, useNow(live));

  // Trang này đã thấy lượt chạy lúc nó còn đang chạy: mã giao diện đang mở là
  // của bản cũ, nên kết quả kèm lời mời tải lại trang.
  const [fresh, setFresh] = useState(witnessed);
  useEffect(() => {
    if (!live) return;
    witnessed = true;
    setFresh(true);
  }, [live]);

  return (
    <>
      <dl className="m-0 flex flex-wrap gap-x-[40px] gap-y-[12px] border-t border-hair pt-[12px]">
        <div>
          <dt className={labelClass}>{t.version}</dt>
          <dd className="m-0 mt-[4px]">
            <MonoText>{state.version}</MonoText>
          </dd>
        </div>
        <div className="min-w-0">
          <dt className={labelClass}>{t.url}</dt>
          <dd className="m-0 mt-[4px]">
            <MonoText>{state.url}</MonoText>
          </dd>
        </div>
      </dl>

      {phase.kind === "updating" ? (
        // Mất kết nối lúc panel restart là bình thường: chỉ một dòng nhỏ, không báo lỗi.
        <SelfProgress action={phase.action} restarting={unreachable || session.isError} />
      ) : (
        phase.kind !== "idle" && state.last && <SelfResult phase={phase} run={state.last} fresh={fresh} />
      )}

      {/* Còn lượt đang chạy thì không nút nào bấm được (máy chủ cũng sẽ từ chối: self_update_running). */}
      {!live && <Check state={state} />}
      {!live && state.has_old && <SelfRollback />}

      <p className="mt-[18px] mb-0 text-[13px] text-muted">
        {t.cli} <MonoText size={12}>sudo tpan self-update</MonoText>
      </p>
    </>
  );
}

/** "Kiểm bản mới", kết quả của lần kiểm gần nhất, và nút chạy khi có bản khác. */
function Check({ state }: { state: SelfState }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const check = useSelfCheck();
  const start = useSelfStart();
  const [confirming, setConfirming] = useState(false);
  // Máy chủ còn nhớ lần kiểm trước thì hiện luôn, không bắt bấm lại.
  const c = state.check;
  const busy = check.isPending || start.isPending;

  return (
    <div className="mt-[16px]">
      <Button
        disabled={busy}
        onClick={() => {
          start.reset();
          check.mutate();
        }}
      >
        {check.isPending ? t.checking : t.checkButton}
      </Button>
      {!c && !check.isError && <p className="mt-[8px] mb-0 max-w-[720px] text-muted">{t.checkHint}</p>}
      {check.isError && (
        <div className="mt-[10px]">
          <ErrorNote error={check.error} heading={t.checkError} />
        </div>
      )}

      {c &&
        (c.error ? (
          // Không hỏi được nguồn tải không phải là máy hỏng: dấu "chưa kiểm được", không dấu lỗi.
          <div role="status" className="mt-[10px] flex items-start gap-[10px]">
            <span className={markBox}>
              <StatusMark status="unknown" />
            </span>
            <div className="min-w-0 flex-1">
              <p className="m-0 font-medium">{t.sourceError}</p>
              <p className="m-0 max-w-[720px] whitespace-pre-line [overflow-wrap:anywhere]">{tx(c.error, lang)}</p>
              <p className="m-0 mt-[4px] max-w-[720px] text-muted">
                {t.sourceErrorHint} {t.checkedAt(formatDateTime(c.checked_at, lang))}
              </p>
            </div>
          </div>
        ) : c.up_to_date ? (
          <p role="status" className="mt-[10px] mb-0">
            {t.upToDate} <span className="text-muted">{t.checkedAt(formatDateTime(c.checked_at, lang))}</span>
          </p>
        ) : (
          <>
            <Preview check={c} />
            <Button variant="primary" className="mt-[14px]" disabled={busy} onClick={() => setConfirming(true)}>
              {t.updateButton(c.latest)}
            </Button>
            {start.isError && (
              <div className="mt-[10px]">
                <ErrorNote error={start.error} heading={t.updateError} />
              </div>
            )}
            <Dialog
              open={confirming}
              onOpenChange={setConfirming}
              title={t.confirmTitle(c.latest)}
              confirmLabel={t.updateButton(c.latest)}
              onConfirm={() => {
                setConfirming(false);
                start.mutate("update");
              }}
            >
              <ul className="m-0 flex list-none flex-col gap-[8px] p-0">
                <li>{t.confirmRestart}</li>
                <li>{t.confirmSession}</li>
                <li>{t.confirmSites}</li>
              </ul>
            </Dialog>
          </>
        ))}
    </div>
  );
}

/** Bản xem trước: sẽ tải gì, kiểm bằng sha256 nào, lên rồi lùi bằng đường nào. */
function Preview({ check: c }: { check: SelfCheck }) {
  const t = useStrings(strings);
  const { lang } = useLang();
  return (
    <div className="mt-[12px] max-w-[720px]">
      <p role="status" className="m-0 mb-[8px]">
        {t.found(c.latest)} <span className="text-muted">{t.checkedAt(formatDateTime(c.checked_at, lang))}</span>
      </p>
      <dl className="m-0 border-t border-hair">
        <Row label={t.pvCurrent}>
          <MonoText>{c.current}</MonoText>
        </Row>
        <Row label={t.pvLatest}>{c.latest ? <MonoText>{c.latest}</MonoText> : t.latestUnknown}</Row>
        <Row label={t.pvUrl}>
          <MonoText>{c.url}</MonoText>
        </Row>
        <Row label={t.pvAsset}>
          <MonoText>{c.asset}</MonoText>
        </Row>
        <Row label={t.pvSha}>
          {/* Đủ 64 ký tự để so từng ký tự với SHA256SUMS; MonoText tự xuống dòng (break-all). */}
          <MonoText size={12}>{c.sha256}</MonoText>
        </Row>
        <Row label={t.pvBack}>{t.backNote}</Row>
      </dl>
    </div>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-wrap gap-x-[16px] gap-y-[2px] border-b border-hair py-[8px]">
      <dt className={`flex-[0_0_220px] pt-[2px] ${labelClass}`}>{label}</dt>
      <dd className="m-0 min-w-0 flex-[1_1_280px]">{children}</dd>
    </div>
  );
}
