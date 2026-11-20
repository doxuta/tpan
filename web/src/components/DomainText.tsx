// Tên miền dài (ví dụ wp1.shop.example.com) được xuống dòng ngay sau dấu chấm,
// để bảng không phải cuộn ngang và không phải cắt chữ bằng "…" trên màn hẹp.
// <wbr> chỉ là chỗ được phép ngắt dòng, không thêm ký tự nào: chữ chép ra vẫn
// đúng tên miền. Chỗ dùng không được đặt whitespace-nowrap (nowrap tắt <wbr>).
//
//   <DomainText domain={site.domain} />

import { Fragment } from "react";

export function DomainText({ domain }: { domain: string }) {
  const parts = domain.split(".");
  return (
    <>
      {parts.map((p, i) => (
        <Fragment key={i}>
          {p}
          {i < parts.length - 1 && (
            <>
              .<wbr />
            </>
          )}
        </Fragment>
      ))}
    </>
  );
}
