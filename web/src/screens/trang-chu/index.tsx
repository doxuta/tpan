// Trang chủ theo vai (mục 3.6 của docs/thay-doi/M16-thiet-ke.md):
//   /           admin: Bảng điều khiển; user thường: chuyển sang /tools
//   /dashboard  admin: chuyển về /;      user thường: Bảng điều khiển của họ
// Chỉ là chuyện hiển thị: quyền thật vẫn do luật của từng route ở server giữ.
// Phiên "xem như" mang vai của user được xem nên đi đường của user thường.

import { Navigate } from "react-router";
import { useWho } from "../../api/m13-users";
import Dashboard from "../bang-dieu-khien";

export function Home() {
  const { isAdmin } = useWho();
  return isAdmin ? <Dashboard /> : <Navigate to="/tools" replace />;
}

export function DashboardAlias() {
  const { isAdmin } = useWho();
  return isAdmin ? <Navigate to="/" replace /> : <Dashboard />;
}
