// Lỗi của một việc có mật khẩu và mã hai bước (bật, tắt, tạo lại mã, tạo token):
// các mã lỗi của M20 có câu riêng nói thẳng làm gì tiếp; mã khác thì hiện câu của
// máy chủ (ErrorNote, kèm bảng điều kiện nếu có). Hiện ngay cạnh chỗ gây ra nó.

import { ApiError } from "../../api/client";
import { resetCommand } from "../../api/m20-types";
import { Notice } from "../../components/Notice";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/tai-khoan";
import { ErrorNote } from "../site/ErrorNote";

export function MfaError({ error, user }: { error: unknown; user: string }) {
  const t = useStrings(strings);
  if (error instanceof ApiError) {
    let text: string | null = null;
    switch (error.code) {
      case "mfa_code_wrong":
        text = t.errCodeWrong;
        break;
      case "mfa_code_used":
        text = t.errCodeUsed;
        break;
      case "mfa_code_required":
        text = t.errCodeRequired;
        break;
      case "mfa_locked":
        text = t.errLocked(t.waitText(Math.max(1, Math.ceil((error.retryAfter ?? 900) / 60))));
        break;
      case "wrong_password":
        text = t.errWrongPassword;
        break;
      case "mfa_secret_unavailable":
        text = t.errSecret(resetCommand(user || "<tên>"));
        break;
      case "mfa_not_pending":
        text = t.errNotPending;
        break;
      case "mfa_already_enabled":
        text = t.errAlready;
        break;
    }
    if (text !== null) {
      return (
        <Notice kind="fail" role="alert">
          {text}
        </Notice>
      );
    }
  }
  return <ErrorNote error={error} />;
}
