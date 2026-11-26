// Bảng token (M20), dùng chung cho trang Tài khoản (token của mình) và màn Cài
// đặt (token của mọi user, có cột Chủ). Danh sách chỉ có `hint`, không bao giờ có
// token đầy đủ. Thu hồi nằm trong menu ba chấm của hàng và hỏi lại trước.

import { useState, type ReactNode } from "react";
import { useRevokeToken } from "../../api/m20";
import type { TokenRow } from "../../api/m20-types";
import { Dialog } from "../../components/Dialog";
import { Code } from "../../components/MonoText";
import { RowActions } from "../../components/RowActions";
import { TableList, type Column } from "../../components/TableList";
import { Tag } from "../../components/Tag";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/tai-khoan";
import { ErrorNote } from "../site/ErrorNote";
import { fmtDate, fmtTime, revocable } from "./model";

type Props = {
  rows: TokenRow[];
  /** "mine": DELETE /account/tokens/{id}; "all": DELETE /tokens/{id} (admin). */
  scope: "mine" | "all";
  title: ReactNode;
  titleId: string;
  /** Neo của thẻ (#token). */
  id?: string;
  caption: string;
  actions?: ReactNode;
  above?: ReactNode;
};

export function TokenTable({ rows, scope, title, titleId, id, caption, actions, above }: Props) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const revoke = useRevokeToken(scope);
  const [asking, setAsking] = useState<TokenRow | null>(null);
  const showOwner = scope === "all";

  const columns: Column<TokenRow>[] = [
    { header: t.colName, cell: (r) => <span className="font-medium">{r.name}</span> },
    ...(showOwner ? [{ header: t.colOwner, cell: (r: TokenRow) => r.user }] : []),
    {
      header: t.colScopes,
      nowrap: false,
      className: "min-w-[180px]",
      cell: (r) => (
        <span className="flex flex-wrap gap-[4px]">
          {r.scopes.map((s) => (
            <Tag key={s}>{s}</Tag>
          ))}
        </span>
      ),
    },
    { header: t.colHint, cell: (r) => <Code>{r.hint}</Code> },
    { header: t.colExpires, cell: (r) => fmtDate(r.expires, lang) },
    {
      header: t.colLastUsed,
      nowrap: false,
      className: "min-w-[150px]",
      cell: (r) =>
        r.last_used ? (
          <span>
            {fmtTime(r.last_used, lang)}
            {r.last_ip && (
              <>
                {" · "}
                <Code>{r.last_ip}</Code>
              </>
            )}
          </span>
        ) : (
          <span className="text-muted">{t.neverUsed}</span>
        ),
    },
    { header: t.colStatus, cell: (r) => <Tag>{t.status[r.status]}</Tag> },
    {
      header: t.colAction,
      align: "right",
      cell: (r) => <RowActions primary={[]} more={[{ label: t.tokRevoke, danger: true, disabled: !revocable(r), onSelect: () => setAsking(r) }]} moreLabel={t.tokMoreLabel(r.name)} />,
    },
  ];

  return (
    <>
      <TableList title={title} titleId={titleId} id={id} caption={caption} minWidth={showOwner ? 880 : 820} actions={actions} above={above} rows={rows} rowKey={(r) => r.id} columns={columns} empty={t.tokEmpty} />
      {asking && (
        <Dialog
          open
          onOpenChange={(o) => {
            if (!o) {
              setAsking(null);
              revoke.reset();
            }
          }}
          title={t.revokeTitle(asking.name)}
          description={t.revokeDesc}
          confirmLabel={t.revokeConfirm}
          busy={revoke.isPending}
          danger
          onConfirm={() => revoke.mutate(asking.id, { onSuccess: () => setAsking(null) })}
        >
          {revoke.isError && <ErrorNote error={revoke.error} />}
        </Dialog>
      )}
    </>
  );
}
