// Màn Cơ sở dữ liệu (mục 6.9 của KE-HOACH.md), M3 chỉ xem: bảng kẻ mảnh các
// cơ sở dữ liệu của site (GET /api/v1/databases). Chưa có thao tác nào (M9).

import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { Link } from "react-router";
import { errorMessage, get } from "../../api/client";
import type { DatabaseRow } from "../../api/types";
import { Button } from "../../components/Button";
import { MonoText } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { TableList } from "../../components/TableList";
import { useLang, useStrings, type Lang } from "../../i18n";
import { strings } from "../../i18n/co-so-du-lieu";
import { strings as khung } from "../../i18n/khung";

/** Byte thành KB, MB hoặc GB, một số lẻ, dấu thập phân theo ngôn ngữ. */
function formatSize(bytes: number, lang: Lang): string {
  const num = new Intl.NumberFormat(lang === "vi" ? "vi-VN" : "en-GB", { maximumFractionDigits: 1 });
  if (bytes < 1024 * 1024) return `${num.format(bytes / 1024)} KB`;
  if (bytes < 1024 ** 3) return `${num.format(bytes / 1024 ** 2)} MB`;
  return `${num.format(bytes / 1024 ** 3)} GB`;
}

/**
 * Độ rộng tối thiểu cho chữ mono dài (tên DB, domain). min-width đặt lên ô <td>
 * không được trình duyệt tôn trọng, và MonoText cho ngắt giữa chữ, nên không có
 * khối này thì cột bị ép hẹp và tên bị cắt ra nhiều dòng. Hẹp quá thì cả bảng
 * cuộn ngang trong khung riêng của TableList.
 */
function Min({ w, children }: { w: string; children: ReactNode }) {
  return <span className={`block ${w}`}>{children}</span>;
}

export default function CoSoDuLieu() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const q = useQuery({ queryKey: ["databases"], queryFn: () => get<DatabaseRow[]>("/api/v1/databases") });

  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />
      <div className="pt-[20px]">
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
          <>
            <TableList
              caption={t.tableName}
              rows={q.data}
              rowKey={(r) => r.db}
              empty={
                <>
                  {t.emptyText} <Link to="/sites/new">{t.emptyLink}</Link>
                </>
              }
              columns={[
                { header: t.colDb, cell: (r) => <Min w="min-w-[216px]"><MonoText>{r.db}</MonoText></Min> },
                { header: t.colUser, cell: (r) => <Min w="min-w-[216px]"><MonoText>{r.user}</MonoText></Min> },
                {
                  header: t.colSite,
                  cell: (r) => (
                    <Min w="min-w-[170px]">
                      <Link to={`/sites/${encodeURIComponent(r.domain)}`} className="font-mono text-[13px]">
                        {r.domain}
                      </Link>
                    </Min>
                  ),
                },
                {
                  header: t.colSize,
                  className: "text-right whitespace-nowrap tabular-nums",
                  cell: (r) => (r.size_bytes < 0 ? <span className="text-muted">{t.unreadable}</span> : formatSize(r.size_bytes, lang)),
                },
                {
                  header: t.colTables,
                  className: "text-right whitespace-nowrap tabular-nums",
                  cell: (r) => (r.tables < 0 ? <span className="text-muted">{t.unreadable}</span> : r.tables),
                },
              ]}
            />
            <p className="mt-[16px] mb-0 text-[14px] text-muted">{t.later}</p>
          </>
        )}
      </div>
    </>
  );
}
