// Mục "Tên miền dùng chung" ở Cài đặt (admin). Mặc định hai khách không thể có
// hai tên con của cùng một tên miền (để khách này không lấy được www. của
// khách kia). Máy chia tên con của một tên miền cho nhiều khách thì khai tên
// miền đó ở đây: từ đó mỗi tên con trực tiếp của nó thuộc riêng một khách.
// Chỉ ghi một dòng cài đặt, có hiệu lực ngay, không có job.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { del, errorMessage, get, post } from "../../api/client";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { TextField } from "../../components/Field";
import { MonoText } from "../../components/MonoText";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/cai-dat";

type SharedDomains = { domains: string[] };
const key = ["settings", "shared-domains"] as const;
const path = "/api/v1/settings/shared-domains";

export function TenMienChung() {
  const t = useStrings(strings);
  const { lang } = useLang();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: key, queryFn: () => get<SharedDomains>(path), retry: false });
  const [name, setName] = useState("");
  const done = (d: SharedDomains) => qc.setQueryData(key, d);
  const add = useMutation({
    mutationFn: (domain: string) => post<SharedDomains>(path, { domain }),
    onSuccess: (d) => {
      done(d);
      setName("");
    },
  });
  const remove = useMutation({ mutationFn: (domain: string) => del<SharedDomains>(`${path}/${encodeURIComponent(domain)}`), onSuccess: done });
  const busy = add.isPending || remove.isPending;
  const error = add.error ?? remove.error ?? (q.isError ? q.error : null);
  const domains = q.data?.domains ?? [];

  function submit(e: FormEvent) {
    e.preventDefault();
    remove.reset();
    if (name.trim() !== "") add.mutate(name.trim());
  }

  return (
    <Card title={t.sharedHeading} titleId="ten-mien-dung-chung">
      <p className="m-0 max-w-[72ch]">{t.sharedIntro}</p>
      <p className="mt-[6px] mb-0 max-w-[72ch] text-muted">{t.sharedExample}</p>
      {domains.length === 0 ? (
        <p className="mt-[12px] mb-0 text-muted">{q.isPending ? t.sharedLoading : t.sharedNone}</p>
      ) : (
        <ul className="mt-[12px] mb-0 flex list-none flex-col gap-[6px] p-0">
          {domains.map((d) => (
            <li key={d} className="flex flex-wrap items-center gap-[12px]">
              <MonoText>{d}</MonoText>
              <Button
                variant="text"
                disabled={busy}
                aria-label={t.sharedRemoveFor(d)}
                onClick={() => {
                  add.reset();
                  remove.mutate(d);
                }}
              >
                {t.sharedRemove}
              </Button>
            </li>
          ))}
        </ul>
      )}
      <form onSubmit={submit} className="mt-[14px] flex flex-wrap items-end gap-[12px] border-t border-hair pt-[14px]">
        <div className="min-w-[260px] flex-1 sm:max-w-[420px]">
          <TextField id="ten-mien-chung-moi" label={t.sharedAddLabel} hint={t.sharedAddHint} value={name} onChange={(e) => setName(e.target.value)} mono spellCheck={false} autoCapitalize="none" autoComplete="off" />
        </div>
        <Button type="submit" disabled={busy || name.trim() === ""}>
          {t.sharedAdd}
        </Button>
      </form>
      {error && (
        <p role="alert" className="mt-[10px] mb-0 max-w-[72ch] font-medium">
          {errorMessage(error, lang)}
        </p>
      )}
      <p className="mt-[10px] mb-0 max-w-[72ch] text-[13px] text-muted">{t.sharedNote}</p>
    </Card>
  );
}
