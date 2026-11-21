// Các phiên đang đăng nhập: GET /api/v1/account/sessions, và nút xoá từng phiên
// (DELETE /api/v1/account/sessions/{id}). Phiên hiện tại không có nút ở đây:
// đăng xuất phiên này đã có mục Đăng xuất ở menu tài khoản, và xoá nhầm phiên
// đang dùng thì văng ngay ra trang đăng nhập.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { del, errorMessage, get } from "../../api/client";
import type { SessionRow } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { MonoText } from "../../components/MonoText";
import { RowActions } from "../../components/RowActions";
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
    <>
      {q.isPending && (
        <Card title={t.ssHeading} titleId="phien-tieu-de">
          <p className="m-0 text-muted">{k.loading}</p>
        </Card>
      )}
      {q.isError && (
        <Card title={t.ssHeading} titleId="phien-tieu-de">
          <p className="mt-0 mb-[12px]">{errorMessage(q.error, lang)}</p>
          <Button onClick={() => void q.refetch()}>{k.retry}</Button>
        </Card>
      )}
      {q.isSuccess && (
        <TableList
          title={t.ssHeading}
          titleId="phien-tieu-de"
          caption={t.ssTable}
          minWidth={560}
          above={<p className="m-0 max-w-[72ch] px-[16px] py-[12px] text-muted">{t.ssIntro}</p>}
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
            { header: t.ssColCreated, cell: (r) => <span className="text-muted tabular-nums">{formatTime(r.created, lang)}</span> },
            { header: t.ssColLastSeen, cell: (r) => <span className="text-muted tabular-nums">{formatTime(r.last_seen, lang)}</span> },
            {
              header: t.ssColAction,
              align: "right",
              nowrap: false,
              cell: (r) => {
                const label = t.ssSignOutLabel(r.ip, formatTime(r.created, lang));
                const busy = signOut.isPending && signOut.variables === r.id;
                return r.current ? (
                  <span className="text-[13px] text-muted">{t.ssCurrentHint}</span>
                ) : (
                  <RowActions primary={[{ label: t.ssSignOut, busy, busyLabel: t.ssSigningOut, ariaLabel: label, onClick: () => signOut.mutate(r.id) }]} moreLabel={label} />
                );
              },
            },
          ]}
        />
      )}
      {signOut.isError && (
        <Card>
          <Note kind="fail">{errorMessage(signOut.error, lang)}</Note>
        </Card>
      )}
    </>
  );
}
