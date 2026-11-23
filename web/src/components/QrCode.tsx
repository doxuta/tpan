// Mã QR (M20, bật đăng nhập hai bước): máy chủ chỉ gửi ma trận (mỗi chuỗi một
// hàng, "1" là ô tối), trình duyệt vẽ thành một <svg> với một <path>, không
// thư viện và không ảnh. Màu qua hai biến --qr-fg và --qr-bg của styles.css, GIỐNG
// NHAU ở sáng và tối: ô tối trên nền sáng, vì nhiều app không đọc được QR đảo
// màu. Vùng trắng 4 ô quanh mã nằm trong hình (chuẩn QR).
//
//   <QrCode rows={setup.qr} label="Mã QR để quét bằng app xác thực" />

import { qrPath } from "../screens/tai-khoan/model";

export function QrCode({ rows, label }: { rows: string[]; label: string }) {
  const { size, d } = qrPath(rows);
  return (
    <svg
      role="img"
      aria-label={label}
      viewBox={`0 0 ${size} ${size}`}
      shapeRendering="crispEdges"
      // Hình vuông cố định 200 px, thu nhỏ theo bề ngang khi màn hẹp.
      className="block h-auto w-[200px] max-w-full flex-none rounded-ctl"
    >
      <rect width={size} height={size} style={{ fill: "var(--qr-bg)" }} />
      <path d={d} style={{ fill: "var(--qr-fg)" }} />
    </svg>
  );
}
