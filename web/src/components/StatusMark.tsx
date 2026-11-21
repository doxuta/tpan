// Dấu trạng thái: component DUY NHẤT vẽ bốn trạng thái (mục 2 của
// docs/GIAO-DIEN.md). Mỗi trạng thái một hình riêng để người mù màu và bản in
// đen trắng vẫn đọc được. Không vẽ lại dấu này ở chỗ khác.
//
//   <StatusMark status="fail" label="Pool PHP: lỗi" />
//   <StatusLine status="pass">Đang hoạt động</StatusLine>
//
// label là nhãn cho trình đọc màn hình, dạng "<chặng>: <trạng thái>" (dùng
// khung.statusLabel). Bỏ label chỉ khi ngay cạnh đã có chữ nói trạng thái (ví
// dụ dòng chú giải); khi đó dấu bị ẩn khỏi trình đọc màn hình. Muốn bấm vào
// dấu để mở bằng chứng thì bọc nó trong <button> hoặc <a> ở màn dùng.

import type { ReactNode } from "react";
import type { Status } from "../api/types";

type Props = {
  status: Status;
  label?: string;
  /** "sm" cho dấu nằm trong một dòng chữ (khoảng 70%); "lg" phóng 1,5 lần cho tuyến của trang một site. */
  size?: "sm" | "md" | "lg";
};

const shape: Record<Status, string> = {
  // đạt: chấm tròn đặc 12 px
  pass: "block size-[12px] rounded-full bg-ok",
  // cảnh báo: hình thoi 13 px, viền 2 px
  warn: "block size-[13px] border-2 border-warn-border bg-warn rotate-45",
  // lỗi: ô vuông đặc 20 px có dấu ×
  fail: "flex size-[20px] items-center justify-center bg-fail text-on-fail",
  // chưa kiểm được: vòng tròn rỗng 10 px, viền 2 px; ruột màu nền thẻ để nét tuyến không lọt qua
  unknown: "block size-[10px] rounded-full border-2 border-unknown bg-surface",
};

export function StatusMark({ status, label, size = "md" }: Props) {
  const a11y = label ? { role: "img", "aria-label": label } : { "aria-hidden": true };
  const mark = (
    <span className={shape[status]} {...(size === "md" ? a11y : {})}>
      {status === "fail" && (
        <svg aria-hidden="true" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      )}
    </span>
  );
  if (size === "md") return mark;
  if (size === "sm") {
    // Hộp bố cục đúng 14 px để không đẩy cao dòng chữ; dấu 20 px bên trong thu
    // còn 70% quanh tâm (flex canh giữa cả khi con to hơn hộp).
    return (
      <span className="inline-flex size-[14px] flex-none items-center justify-center" {...a11y}>
        <span className="flex scale-[0.7]">{mark}</span>
      </span>
    );
  }
  return (
    <span className="inline-flex scale-150" {...a11y}>
      {mark}
    </span>
  );
}

/** Dấu trạng thái rồi chữ, trên một dòng (cột Trạng thái của bảng). Chữ đã nói trạng thái nên dấu ẩn khỏi trình đọc màn hình. */
export function StatusLine({ status, children }: { status: Status; children: ReactNode }) {
  return (
    <span className="inline-flex items-center gap-[8px]">
      <span className="flex size-[20px] flex-none items-center justify-center">
        <StatusMark status={status} />
      </span>
      <span>{children}</span>
    </span>
  );
}
