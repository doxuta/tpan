// Màn Stack (mục 6.11 và mục 10 của KE-HOACH.md). Hai khối, mỗi khối tự đọc API
// của nó để một bên lỗi (ví dụ php.net không trả lời) không che bên kia:
//   Config  cấu hình và dịch vụ (GET /api/v1/stack)
//   Php     các bản PHP, cài và gỡ (GET /api/v1/php, POST /api/v1/php/install|remove)

import { ScreenHeader } from "../../components/ScreenHeader";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/stack";
import { Config } from "./Config";
import { Php } from "./Php";

export default function StackScreen() {
  const t = useStrings(strings);
  return (
    <>
      <ScreenHeader title={t.title} summary={t.summary} />
      <Config />
      <Php />
    </>
  );
}
