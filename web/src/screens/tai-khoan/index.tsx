// Trang Tài khoản của tôi (/account, M20; mục 3.11 của docs/thay-doi/M20-thiet-ke.md):
// mọi vai đều có. Thẻ "Đăng nhập hai bước" (neo #hai-buoc) và thẻ "Token API"
// (neo #token). Mật khẩu và các phiên vẫn nằm ở màn Cài đặt.

import { ScreenHeader } from "../../components/ScreenHeader";
import { useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/tai-khoan";
import { HaiBuoc } from "./HaiBuoc";
import { Token } from "./Token";

export default function TaiKhoan() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  return (
    <>
      <ScreenHeader crumbs={[{ label: k.groups.taiKhoan }, { label: t.title }]} title={t.title} summary={t.summary} />
      <HaiBuoc />
      <Token />
    </>
  );
}
