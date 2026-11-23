// Phần dùng chung của hai nơi liệt kê backup (màn Backup và tab Backup của trang
// một site) về kho ngoài máy (M18), để hai nơi không chép lại logic:
//
//   - "Khôi phục" một bản chỉ còn trên kho: admin được hỏi tải về trước (hộp
//     thoại), user thường được nhắn nhờ admin; không bên nào gọi API khôi phục,
//     vì máy chủ sẽ từ chối backup_not_on_machine. Bản chỉ còn ở một kho khác
//     (sau khi gỡ hay đổi đích) cũng không gọi API: máy chủ sẽ từ chối kèm lệnh
//     `sudo` mà user thường không chạy được; màn nói thẳng phải làm gì.
//   - Sau khi chạy "Đẩy lên kho": câu `note` của máy chủ (đã xếp hàng). Tải về
//     không có job_id (một lượt đẩy đang giữ khoá kho, có thể tới 30 phút): câu
//     chờ của màn, và bảng tự hỏi lại mỗi 15 giây (tối đa 30 phút) tới khi dòng
//     hết là "chỉ trên kho". Hai câu này không mang dấu đạt: việc chưa xong.
//     Tải về hỏng trước khi job bắt đầu thì máy chủ trả lỗi ngay, và bản xem
//     trước hiện lỗi đó như mọi lần chạy lỗi khác.
//   - Tải về do khôi phục xong (dòng thành "máy và kho") thì báo "Đã tải về".
//     Không tự chạy khôi phục: nó ghi đè site nên người dùng phải tự xem bản xem
//     trước của nó.
//
//   const flow = useStorageFlow((backup) => ask({ action: "pull", backup, fromRestore: true }));
//   useQuery({ ..., refetchInterval: flow.poll });
//   onClick: () => flow.restore(backup) || ask({ action: "restore", backup })
//   run.onSuccess: if (req.action === "pull") flow.ran(req, res);
//   <>{flow.render(items)}</>   // dưới bảng
//   flow.clear()                // khi bắt đầu một việc khác

import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { get } from "../../api/client";
import { useWho } from "../../api/m13-users";
import { targetPath, type QueuedResponse, type TargetView } from "../../api/m18-types";
import type { Backup, BackupsOverview, JobStarted } from "../../api/types";
import { Dialog } from "../../components/Dialog";
import { Notice } from "../../components/Notice";
import { tx, useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/backup";
import { pullPoll, pullPollWindowMs, targetKey, type PullWatch } from "./model";

/** Trả lời 202 của mọi việc chạy: job_id (khôi phục, backup ngay, cài đặt, tải về) hoặc `note` của máy chủ (đẩy lên kho, tải về chưa có job). */
export type Started = Partial<JobStarted & QueuedResponse>;

/** Hai việc kho. fromRestore: tải về do người bấm "Khôi phục" một bản chỉ còn trên kho. */
export type StorageReq = { action: "push"; backup: Backup } | { action: "pull"; backup: Backup; fromRestore?: boolean };

/**
 * info: không dấu, cho việc mới xếp hàng hay đang chờ. Chấm xanh chỉ để nói "đạt"
 * (mục 2 của GIAO-DIEN.md), còn Notice chưa có dấu "chưa kiểm được".
 */
type Msg = { kind: "info" | "warn"; text: string };

export function useStorageFlow(askPull: (backup: Backup) => void) {
  const t = useStrings(strings);
  const { lang } = useLang();
  const { isAdmin } = useWho();
  // Đã đặt đích chưa (cột "Ở đâu", nút "Đẩy lên kho"). User thường không đọc được (403) nên không hỏi.
  const target = useQuery({ queryKey: targetKey, queryFn: () => get<TargetView>(targetPath), enabled: isAdmin });

  /** Bản chỉ còn trên kho mà admin vừa bấm "Khôi phục": hộp hỏi tải về trước. */
  const [pullFirst, setPullFirst] = useState<Backup | null>(null);
  /** Id bản đang chờ tải về để khôi phục. */
  const [awaiting, setAwaiting] = useState<string | null>(null);
  const [watch, setWatch] = useState<PullWatch | null>(null);
  const [msg, setMsg] = useState<Msg | null>(null);
  const msgRef = useRef<HTMLDivElement>(null);

  // Câu báo mới thì đưa người dùng tới đó, vì nó nằm dưới bảng.
  useEffect(() => {
    if (msg) {
      msgRef.current?.scrollIntoView({ block: "start" });
      msgRef.current?.focus();
    }
  }, [msg]);

  return {
    isAdmin,
    /** undefined với user thường và khi chưa đọc được đích. */
    configured: isAdmin ? target.data?.configured : undefined,

    /** Cho refetchInterval của truy vấn danh sách: tự tắt khi hết điều kiện (pullPoll). */
    poll: (query: { state: { data?: BackupsOverview } }) => pullPoll(query.state.data?.items, watch, Date.now()),

    /**
     * Bấm "Khôi phục" một hàng. true: đã xử lý (bản không có trên máy), người gọi đừng gọi API khôi phục.
     * Bản "kho khác" mà file còn trên máy trả false: khôi phục chạy được như bản thường.
     */
    restore(backup: Backup): boolean {
      const elsewhere = backup.location === "other_target" && !backup.local;
      if (backup.location !== "remote" && !elsewhere) return false;
      setAwaiting(null);
      if (elsewhere) {
        // Không có nút tải về cho bản này: phải đặt lại đúng đích cũ trước.
        setMsg({ kind: "warn", text: isAdmin ? t.otherTargetSetAgain : t.otherTargetAskAdmin });
      } else if (isAdmin) {
        setMsg(null);
        setPullFirst(backup);
      } else {
        setMsg({ kind: "warn", text: t.askAdminToPull });
      }
      return true;
    },

    /** Sau khi chạy thật một việc kho (202). */
    ran(req: StorageReq, res: Started) {
      if (req.action === "push") setMsg({ kind: "info", text: tx(res.note, lang) || t.queued });
      // Câu chờ viết ở màn chứ không lấy `note` của máy chủ, để khớp đúng hạn tự tải lại của bảng (pullPollWindowMs).
      else if (!res.job_id) setMsg({ kind: "info", text: t.pullWaiting });
      if (req.action === "pull") {
        if (!res.job_id) setWatch({ id: req.backup.id, until: Date.now() + pullPollWindowMs });
        if (req.fromRestore) setAwaiting(req.backup.id);
      }
    },

    /** Bắt đầu một việc khác: bỏ câu báo cũ. */
    clear() {
      setMsg(null);
      setAwaiting(null);
    },

    /** Hộp hỏi tải về, câu "Đã tải về" và câu báo của máy chủ. items: danh sách đang hiện (để biết dòng đã về máy chưa). */
    render(items: Backup[] | undefined): ReactNode {
      // Suy từ dữ liệu bảng nên không cần effect.
      const pulled = awaiting !== null && items?.find((b) => b.id === awaiting)?.location === "both";
      return (
        <>
          <Dialog
            open={pullFirst !== null}
            onOpenChange={(o) => !o && setPullFirst(null)}
            title={t.pullFirstTitle}
            description={t.pullFirstBody}
            confirmLabel={t.pull}
            onConfirm={() => {
              // Đóng hộp rồi đi tiếp đúng như nút "Tải về máy" của hàng: bản xem trước trước, chạy sau.
              const backup = pullFirst;
              setPullFirst(null);
              if (backup) askPull(backup);
            }}
          />
          {pulled && (
            <Notice kind="pass" role="status">
              {t.pulled}
            </Notice>
          )}
          {/* Đã tải về thì câu chờ cũ ("đang chờ… bấm lại") nói sai, nên ẩn đi. */}
          {msg && !pulled && (
            <div ref={msgRef} tabIndex={-1} className="outline-offset-0">
              {msg.kind === "info" ? (
                <p role="status" className="m-0 max-w-[72ch]">
                  {msg.text}
                </p>
              ) : (
                <Notice kind={msg.kind} role="status">
                  {msg.text}
                </Notice>
              )}
            </div>
          )}
        </>
      );
    },
  };
}
