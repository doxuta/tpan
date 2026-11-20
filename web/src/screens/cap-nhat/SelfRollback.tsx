// "Quay về bản trước" trong khối "Phiên bản và cập nhật" (mục 6.14 của
// KE-HOACH.md, quyết định 7). Chỉ vẽ khi máy còn giữ bản cũ (has_old). Bấm →
// POST /self/rollback?dry_run=1 trả {current, old} → hộp hỏi lại → POST thật
// (202) → useSelfStart ghi lượt chạy vào bộ đệm và khối chuyển sang "đang cập
// nhật". Máy chủ từ chối (409 self_no_old, self_rollback_db, job_running...)
// thì câu của nó hiện ngay dưới nút.

import { useState } from "react";
import { useSelfRollbackPreview, useSelfStart } from "../../api/m15";
import type { SelfRollbackPreview } from "../../api/m15-types";
import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/phien-ban";
import { ErrorNote } from "../site/ErrorNote";

export function SelfRollback() {
  const t = useStrings(strings);
  const preview = useSelfRollbackPreview();
  const start = useSelfStart();
  const [asked, setAsked] = useState<SelfRollbackPreview | null>(null);
  const error = start.isError ? start.error : preview.isError ? preview.error : null;

  return (
    <div className="mt-[18px] border-t border-hair pt-[14px]">
      <Button
        disabled={preview.isPending || start.isPending}
        onClick={() => {
          start.reset();
          preview.mutate(undefined, { onSuccess: (p) => setAsked(p) });
        }}
      >
        {preview.isPending ? t.rollbackChecking : t.rollbackButton}
      </Button>
      <p className="mt-[8px] mb-0 max-w-[720px] text-muted">{t.rollbackHint}</p>
      {error !== null && (
        <div className="mt-[10px]">
          <ErrorNote error={error} heading={t.rollbackError} />
        </div>
      )}

      {asked && (
        <Dialog
          open
          onOpenChange={(open) => !open && setAsked(null)}
          title={t.rollbackTitle(asked.current, asked.old)}
          confirmLabel={t.rollbackConfirm(asked.old)}
          onConfirm={() => {
            setAsked(null);
            start.mutate("rollback");
          }}
        >
          <p className="m-0">{t.rollbackWarnDb}</p>
          <p className="mt-[8px] mb-0">{t.rollbackWarnRestart}</p>
        </Dialog>
      )}
    </div>
  );
}
