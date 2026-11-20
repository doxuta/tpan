// Ba chặng chung của mọi thao tác đổi máy (mục 10 của KE-HOACH.md): bấm nút →
// gọi ?dry_run=1 lấy bản xem trước (PlanReview) → bấm chạy → máy chủ trả
// job_id, chuyển tới /jobs/:id. Các màn M13 có nhiều nút cùng kiểu (tạm khoá,
// xoá DB, đổi mật khẩu...), nên gom ba chặng này vào một hook thay vì chép
// cặp mutation vào từng màn như các màn cũ.
//
//   const op = usePreviewedOp([databasesKey]);
//   <Button onClick={() => void op.ask({ id: "x", method: "POST", path, errorHeading: "...", warning: "..." })}>
//   ...
//   {op.panel}   // lỗi dựng bản xem trước, hoặc lời cảnh báo + PlanReview
//
// Cùng lúc chỉ mở một việc: hỏi việc mới thì bản xem trước cũ bị bỏ.

import { useMutation, useQueryClient, type QueryKey } from "@tanstack/react-query";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useNavigate } from "react-router";
import { del, post } from "../api/client";
import { queryKeys } from "../api/queries";
import type { JobStarted, Preview } from "../api/types";
import { useStrings } from "../i18n";
import { strings as khung } from "../i18n/khung";
import { strings as chung } from "../i18n/m13-chung";
import { PlanReview } from "../screens/duyet-plan";
import { ErrorNote } from "../screens/site/ErrorNote";
import { Button } from "./Button";

export type Op = {
  /** Khoá của nút đã bấm, để nút đó đổi chữ thành "Đang dựng…" (busy). */
  id: string;
  method: "POST" | "DELETE";
  path: string;
  body?: unknown;
  /** Dòng đậm khi dựng bản xem trước không được: "Chưa dựng được bản xem trước tạm khoá a.com". */
  errorHeading: string;
  /** Lời nói rõ điều gì sẽ xảy ra, đặt trên bản xem trước (chữ đậm, không đỏ: đỏ chỉ dành cho tình trạng máy). */
  warning?: string;
};

const send = <T,>(op: Op, dry: boolean): Promise<T> => {
  const url = dry ? `${op.path}?dry_run=1` : op.path;
  return op.method === "DELETE" ? del<T>(url) : post<T>(url, op.body);
};

/** invalidate: các khoá truy vấn đổi sau job (danh sách của màn). Số đếm ở menu (overview) luôn được tính lại. */
export function usePreviewedOp(invalidate: readonly QueryKey[]) {
  const k = useStrings(khung);
  const m = useStrings(chung);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = useState<{ op: Op; preview: Preview } | null>(null);
  /** Việc vừa bấm: để chữ nút và lỗi nói đúng việc. */
  const [asked, setAsked] = useState<{ op: Op; quiet: boolean } | null>(null);
  const resultRef = useRef<HTMLDivElement>(null);

  const dry = useMutation({
    mutationFn: (op: Op) => send<Preview>(op, true),
    onSuccess: (preview, op) => setOpen({ op, preview }),
  });
  const run = useMutation({
    mutationFn: (op: Op) => send<JobStarted>(op, false),
    onSuccess: (job) => {
      for (const key of invalidate) void qc.invalidateQueries({ queryKey: key });
      void qc.invalidateQueries({ queryKey: queryKeys.overview });
      void navigate(`/jobs/${encodeURIComponent(job.job_id)}`);
    },
  });

  const errorShown = dry.isError && asked !== null && !asked.quiet;
  // Có bản xem trước hoặc lỗi mới thì đưa người dùng tới đó, vì nó nằm dưới bảng.
  const shown = open ?? (errorShown ? asked : null);
  useEffect(() => {
    if (shown) {
      resultRef.current?.scrollIntoView({ block: "start" });
      resultRef.current?.focus();
    }
  }, [shown]);

  function close() {
    setOpen(null);
    setAsked(null);
    dry.reset();
    run.reset();
  }

  /**
   * Dựng bản xem trước. quiet: người gọi tự hiện lỗi (qua error), ví dụ hộp
   * thoại Tạo DB giữ nguyên hộp để người dùng sửa ô sai. Trả true khi đã có bản xem trước.
   */
  async function ask(op: Op, quiet = false): Promise<boolean> {
    setOpen(null);
    run.reset();
    setAsked({ op, quiet });
    try {
      await dry.mutateAsync(op);
      return true;
    } catch {
      return false;
    }
  }

  const panel: ReactNode = (
    <>
      {errorShown && asked && (
        <div ref={resultRef} tabIndex={-1} className="pt-[20px] outline-offset-0">
          <ErrorNote error={dry.error} heading={asked.op.errorHeading} checksHeading={m.checksHeading} />
          <div className="pt-[12px]">
            <Button onClick={close}>{k.cancel}</Button>
          </div>
        </div>
      )}
      {open && (
        <div ref={resultRef} tabIndex={-1} className="outline-offset-0">
          {open.op.warning && (
            <p role="alert" className="m-0 max-w-[720px] pt-[26px] font-semibold">
              {open.op.warning}
            </p>
          )}
          <PlanReview preview={open.preview} onRun={() => run.mutate(open.op)} running={run.isPending} error={run.error} onCancel={close} />
        </div>
      )}
    </>
  );

  return {
    ask,
    close,
    panel,
    /** Đã có bản xem trước đang mở (PlanReview hiện trong panel). */
    hasPreview: open !== null,
    /** Đang dựng bản xem trước: khoá mọi nút cho tới khi xong. */
    pending: dry.isPending,
    /** Nút có khoá id này đang dựng bản xem trước. */
    busy: (id: string) => dry.isPending && asked?.op.id === id,
    /** Lỗi của lần dựng gần nhất, chỉ khi hỏi với quiet. */
    quietError: dry.isError && asked?.quiet ? dry.error : null,
  };
}
