// Đầu màn (mục 6 và 7 của docs/GIAO-DIEN.md, bản 2): đường dẫn nhỏ, h1 24/600,
// một dòng tóm tắt rộng tối đa 72ch, hành động chính nằm bên phải (xuống dòng
// ở màn hẹp), và một hàng nhãn nhỏ (meta) khi cần.
//
//   <ScreenHeader crumbs={[{ label: "Tài khoản" }]} title="Người dùng"
//     summary="…" actions={<Button variant="primary" icon="plus">Thêm user</Button>} />

import type { ReactNode } from "react";
import { Link } from "react-router";

export type Crumb = { label: ReactNode; to?: string };

type Props = {
  title: ReactNode;
  summary?: ReactNode;
  crumbs?: Crumb[];
  actions?: ReactNode;
  meta?: ReactNode;
};

export function ScreenHeader({ title, summary, crumbs, actions, meta }: Props) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-[16px]">
      <div className="flex min-w-0 flex-[1_1_420px] flex-col gap-[6px]">
        {crumbs && crumbs.length > 0 && (
          <nav aria-label="breadcrumb" className="text-[13px] text-muted">
            {crumbs.map((c, i) => (
              <span key={i}>
                {i > 0 && <span aria-hidden="true"> / </span>}
                {c.to ? (
                  <Link to={c.to} className="text-muted no-underline hover:text-accent hover:underline">
                    {c.label}
                  </Link>
                ) : (
                  c.label
                )}
              </span>
            ))}
          </nav>
        )}
        <h1 className="m-0 text-[24px] leading-[1.25] font-semibold tracking-[-0.01em] [overflow-wrap:anywhere]">{title}</h1>
        {summary && <p className="m-0 max-w-[72ch] text-muted">{summary}</p>}
        {meta && <div className="flex flex-wrap items-center gap-[6px] pt-[2px]">{meta}</div>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-[8px]">{actions}</div>}
    </div>
  );
}
