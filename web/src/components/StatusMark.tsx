// Dấu trạng thái: component DUY NHẤT vẽ bốn trạng thái (mục 2 của
// docs/GIAO-DIEN.md). Mỗi trạng thái một hình riêng để người mù màu và bản in
// đen trắng vẫn đọc được. Không vẽ lại dấu này ở chỗ khác.
//
//   <StatusMark status="fail" label="Pool PHP: lỗi" />
//
// label là nhãn cho trình đọc màn hình, dạng "<chặng>: <trạng thái>" (dùng
// khung.statusLabel). Bỏ label chỉ khi ngay cạnh đã có chữ nói trạng thái (ví
// dụ dòng chú giải); khi đó dấu bị ẩn khỏi trình đọc màn hình. Muốn bấm vào
// dấu để mở bằng chứng thì bọc nó trong <button> hoặc <a> ở màn dùng.

import type { Status } from "../api/types";

type Props = {
  status: Status;
  label?: string;
  /** "lg" phóng 1,5 lần cho trang Site, nơi ga vẽ to hơn (mục 7). */
  size?: "md" | "lg";
};

const shape: Record<Status, string> = {
  // đạt: chấm tròn đặc 12 px
  pass: "block size-[12px] rounded-full bg-ok",
  // cảnh báo: hình thoi 13 px, viền 2 px
  warn: "block size-[13px] border-2 border-warn-border bg-warn rotate-45",
  // lỗi: ô vuông đặc 20 px có dấu ×
  fail: "flex size-[20px] items-center justify-center bg-fail text-on-fail",
  // chưa kiểm được: vòng tròn rỗng 10 px, viền 2 px; ruột màu nền để nét tuyến không lọt qua
  unknown: "block size-[10px] rounded-full border-2 border-unknown bg-paper",
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
  return (
    <span className="inline-flex scale-150" {...a11y}>
      {mark}
    </span>
  );
}
