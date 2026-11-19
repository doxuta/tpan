// Các phiên đang đăng nhập: GET /api/v1/account/sessions, và nút xoá từng phiên
// (DELETE /api/v1/account/sessions/{id}). Phiên hiện tại không có nút ở đây:
// đăng xuất phiên này đã có mục Đăng xuất ở menu, và xoá nhầm phiên đang dùng
// thì văng ngay ra trang đăng nhập.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { del, errorMessage, get } from "../../api/client";
import type { SessionRow } from "../../api/types";
import { Button } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
import { TableList } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings, type Lang } from "../../i18n";
import { strings } from "../../i18n/cai-dat";
import { strings as khung } from "../../i18n/khung";
import { Note } from "./Note";

export const sessionsKey = ["account", "sessions"] as const;

function formatTime(iso: string, lang: Lang): string {
  return new Date(iso).toLocaleString(lang === "vi" ? "vi-VN" : "en-GB", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false });
}

export function Phien() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: sessionsKey, queryFn: () => get<SessionRow[]>("/api/v1/account/sessions") });
  const signOut = useMutation({
    mutationFn: (id: string) => del<void>(`/api/v1/account/sessions/${encodeURIComponent(id)}`),
    // Thành công hay lỗi (ví dụ phiên đã hết hạn từ trước) cũng đọc lại danh sách cho đúng.
    onSettled: () => qc.invalidateQueries({ queryKey: sessionsKey }),
  });

  return (
    <section aria-labelledby="phien-tieu-de" className="pt-[28px]">
      <h2 id="phien-tieu-de" className="m-0 text-[16px] font-semibold">
        {t.ssHeading}
      </h2>
      <p className="mt-[6px] mb-[14px] text-muted">{t.ssIntro}</p>

      {q.isPending && <p className="m-0 text-muted">{k.loading}</p>}
      {q.isError && (
        <div>
          <p className="m-0">{errorMessage(q.error, lang)}</p>
          <Button className="mt-[12px]" onClick={() => void q.refetch()}>
            {k.retry}
          </Button>
        </div>
      )}
      {q.isSuccess && (
        <TableList
          caption={t.ssTable}
          rows={q.data}
          rowKey={(r) => r.id}
          empty={t.ssEmpty}
          columns={[
            {
              header: t.ssColIp,
              cell: (r) => (
                <span className="inline-flex min-w-[124px] flex-wrap items-baseline gap-x-[10px] gap-y-[4px]">
                  <MonoText>{r.ip}</MonoText>
                  {r.current && <Tag>{t.ssCurrent}</Tag>}
                </span>
              ),
            },
            { header: t.ssColCreated, cell: (r) => <span className="font-mono text-[12px] whitespace-nowrap text-muted">{formatTime(r.created, lang)}</span> },
            { header: t.ssColLastSeen, cell: (r) => <span className="font-mono text-[12px] whitespace-nowrap text-muted">{formatTime(r.last_seen, lang)}</span> },
            {
              header: t.ssColAction,
              cell: (r) =>
                r.current ? (
                  <span className="text-[13px] text-muted">{t.ssCurrentHint}</span>
                ) : (
                  <Button
                    aria-label={t.ssSignOutLabel(r.ip, formatTime(r.created, lang))}
                    disabled={signOut.isPending && signOut.variables === r.id}
                    onClick={() => signOut.mutate(r.id)}
                  >
                    {signOut.isPending && signOut.variables === r.id ? t.ssSigningOut : t.ssSignOut}
                  </Button>
                ),
            },
          ]}
        />
      )}
      {signOut.isError && (
        <div className="mt-[12px]">
          <Note kind="fail">{errorMessage(signOut.error, lang)}</Note>
        </div>
      )}
    </section>
  );
}
