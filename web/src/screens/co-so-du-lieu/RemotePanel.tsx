// M37 (v0.2.17): kết nối DB từ máy khác.
//
//   - RemoteMode: dòng trạng thái của cả máy dưới bảng. Admin có nút Bật / Tắt
//     (qua xem trước như mọi việc khác của màn). Bật cần tường lửa ufw đang bật.
//   - RemotePanel: khối "Kết nối từ xa" của một DB, hiện dưới bảng như khối Sửa.
//     Nhập IPv4 được nối vào, mỗi dòng một địa chỉ; danh sách gửi đi thay cả
//     danh sách cũ. Mật khẩu là mật khẩu user DB trên máy chủ (không có mật khẩu
//     riêng cho máy ngoài).

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { get } from "../../api/client";
import { dbSubPath } from "../../api/m13-dbs";
import type { DatabaseRow, DBRemoteHostsRequest, DBRemoteModeRequest, DBRemoteStatus } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Field, Textarea } from "../../components/Field";
import { Code } from "../../components/MonoText";
import type { Op, usePreviewedOp } from "../../components/PreviewedOp";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/co-so-du-lieu";
import { strings as chung } from "../../i18n/m13-chung";
import { dbOpPath } from "./model";
import { parseHosts } from "./remote";

export const dbRemoteKey = ["db-remote"] as const;
export const dbRemotePath = "/api/v1/db-remote";

export function useDbRemote() {
  return useQuery({ queryKey: dbRemoteKey, queryFn: () => get<DBRemoteStatus>(dbRemotePath) });
}

type OpProp = ReturnType<typeof usePreviewedOp>;

/** Dòng trạng thái chế độ của máy, dưới bảng. */
export function RemoteMode({ op, isAdmin }: { op: OpProp; isAdmin: boolean }) {
  const t = useStrings(strings);
  const q = useDbRemote();
  if (!q.isSuccess) return null;
  const on = q.data.on;
  const ask = (want: boolean) => {
    const body: DBRemoteModeRequest = { on: want };
    const o: Op = {
      id: `remote-mode:${want}`,
      method: "POST",
      path: dbRemotePath,
      body,
      warning: want ? t.remoteOnWarning : t.remoteOffWarning,
      errorHeading: want ? t.remoteOnErrorHeading : t.remoteOffErrorHeading,
    };
    void op.ask(o);
  };
  return (
    <div className="flex max-w-[720px] flex-wrap items-center gap-x-[14px] gap-y-[8px] text-[13px]">
      <span className="text-muted">{on ? t.remoteModeOn : t.remoteModeOff}</span>
      {isAdmin && !on && !q.data.ufw_active && <span className="text-muted">{t.remoteNeedsUfw}</span>}
      {isAdmin && (
        <Button size="sm" disabled={op.pending || (!on && !q.data.ufw_active)} onClick={() => ask(!on)}>
          {on ? t.remoteTurnOff : t.remoteTurnOn}
        </Button>
      )}
    </div>
  );
}

/** Khối "Kết nối từ xa" của một DB. */
export function RemotePanel({ row, op, onClose, isAdmin }: { row: DatabaseRow; op: OpProp; onClose: () => void; isAdmin: boolean }) {
  const t = useStrings(strings);
  const m = useStrings(chung);
  const q = useDbRemote();
  const [text, setText] = useState((row.remote ?? []).join("\n"));
  const [tried, setTried] = useState(false);
  const parsed = parseHosts(text, q.data?.max ?? 10);
  const site = row.kind === "site";

  function preview() {
    setTried(true);
    if (parsed.error) {
      document.getElementById("tu-xa-ip")?.focus();
      return;
    }
    const body: DBRemoteHostsRequest = { hosts: parsed.hosts };
    void op.ask({
      id: `remote:${row.db}`,
      method: "POST",
      path: site ? dbOpPath(row.domain, "remote") : dbSubPath(row.db, "remote"),
      body,
      warning: parsed.hosts.length ? t.remoteSetWarning(row.db, parsed.hosts.join(", ")) : t.remoteClearWarning(row.db),
      errorHeading: t.remoteErrorHeading(row.db),
    });
  }

  return (
    <Card title={t.remoteHeading(row.db)} titleId="ket-noi-tu-xa" actions={<Button onClick={onClose}>{t.editClose}</Button>}>
      <div className="flex flex-col gap-[16px]">
        {q.isSuccess && !q.data.on ? (
          <p className="m-0 max-w-[72ch]">{isAdmin ? t.remoteOffAdmin : t.remoteOffUser}</p>
        ) : (
          <>
            <dl className="m-0 grid max-w-[560px] grid-cols-[max-content_1fr] gap-x-[16px] gap-y-[6px] text-[13px]">
              <dt className="text-muted">{t.remoteHost}</dt>
              <dd className="m-0">{t.remoteHostValue}</dd>
              <dt className="text-muted">{t.remotePort}</dt>
              <dd className="m-0">
                <Code>{q.data?.port ?? "3306"}</Code>
              </dd>
              <dt className="text-muted">{t.remoteUser}</dt>
              <dd className="m-0">
                <Code>{row.user}</Code>
              </dd>
              <dt className="text-muted">{t.remotePassword}</dt>
              <dd className="m-0">{t.remotePasswordValue}</dd>
            </dl>
            <Field label={t.remoteIpsLabel} htmlFor="tu-xa-ip" hint={t.remoteIpsHint(q.data?.max ?? 10)} hintId="tu-xa-ip-goi-y" error={tried && parsed.error ? t.remoteIpBad(parsed.error) : undefined}>
              <Textarea id="tu-xa-ip" mono rows={4} value={text} onChange={(e) => setText(e.target.value)} aria-describedby="tu-xa-ip-goi-y" spellCheck={false} placeholder="203.0.113.5" />
            </Field>
            <div>
              <Button variant="primary" disabled={op.pending} onClick={preview}>
                {op.busy(`remote:${row.db}`) ? m.previewing : t.pwPreview}
              </Button>
            </div>
          </>
        )}
      </div>
    </Card>
  );
}
