// Gọi API của M15 (mục 6.14 của KE-HOACH.md): nút cập nhật TPAN trong panel.
// Đường dẫn và kiểu nằm ở m15-types.ts. Tách khỏi queries.ts để các mốc chạy
// song song không sửa chung một file.
//
//   const self = useSelf();                    // GET /self (chỉ admin); tự hỏi lại mỗi 3 giây khi đang có lượt chạy
//   const check = useSelfCheck();              // check.mutate() = "Kiểm bản mới"
//   const start = useSelfStart();              // start.mutate("update" | "rollback") → 202
//   const preview = useSelfRollbackPreview();  // bản xem trước của "Quay về bản trước"
//   const poll = useSelfSessionPoll(run);      // GET /session mỗi 3 giây trong lúc panel restart
//   const hasNew = useSelfAutoCheck(admin);    // dòng phiên bản cuối trang
//
// Không lời gọi nào gửi thân request (hợp đồng: trình duyệt chỉ nói "kiểm",
// "chạy", "quay về").

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { get, post } from "./client";
import {
  selfPath,
  selfRollbackPath,
  selfUpdatePath,
  type SelfAction,
  type SelfRollbackPreview,
  type SelfRun,
  type SelfStarted,
  type SelfState,
} from "./m15-types";
import { queryKeys } from "./queries";
import type { SessionInfo } from "./types";

export const selfKey = ["self"] as const;

/** Nhịp hỏi lại trong lúc đang cập nhật. */
export const selfPollMs = 3000;

/** Dòng phiên bản cuối trang tự kiểm khi lần kiểm gần nhất cũ quá chừng này (cùng số với SelfCheckTTL của máy chủ). */
const autoCheckAfterMs = 6 * 60 * 60 * 1000;

/** Máy chủ còn coi là đang có lượt chạy: đơn vị tạm còn sống, hoặc lượt gần nhất chưa ghi kết quả. */
export function selfRunning(s: SelfState | undefined): boolean {
  return s !== undefined && (s.busy || s.last?.status === "running");
}

/**
 * Trạng thái của khối "Phiên bản và cập nhật". Đang có lượt chạy thì tự hỏi
 * lại mỗi 3 giây, kể cả khi lượt đó do trình duyệt khác bấm. retry: false vì
 * lúc panel restart mỗi nhịp chỉ nên thử một lần; lần hỏng giữ nguyên dữ liệu
 * cũ (data) và nhịp sau hỏi tiếp.
 */
export function useSelf(enabled = true) {
  return useQuery({
    queryKey: selfKey,
    queryFn: () => get<SelfState>(selfPath),
    enabled,
    retry: false,
    refetchInterval: (q) => (selfRunning(q.state.data) ? selfPollMs : false),
  });
}

/** "Kiểm bản mới": máy chủ hỏi nguồn tải rồi trả SelfState kèm check vừa làm. */
export function useSelfCheck() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => post<SelfState>(`${selfUpdatePath}?dry_run=1`),
    onSuccess: (state) => qc.setQueryData(selfKey, state),
  });
}

/**
 * Chạy thật. Máy chủ trả 202 kèm lượt chạy vừa ghi; ghi nó vào bộ đệm của
 * useSelf ngay để màn chuyển sang "đang cập nhật" mà không chờ nhịp hỏi sau
 * (lúc đó panel có thể đã restart).
 */
export function useSelfStart() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (action: SelfAction) => post<SelfStarted>(action === "update" ? selfUpdatePath : selfRollbackPath),
    onSuccess: ({ run }) => qc.setQueryData<SelfState>(selfKey, (old) => old && { ...old, busy: true, last: run }),
  });
}

export function useSelfRollbackPreview() {
  return useMutation({ mutationFn: () => post<SelfRollbackPreview>(`${selfRollbackPath}?dry_run=1`) });
}

/**
 * Hỏi GET /session mỗi 3 giây trong lúc một lượt đang chạy: route công khai,
 * trả version, nên là thứ trả lời sớm nhất khi panel lên lại. Khoá riêng theo
 * từng lượt (không dùng chung khoá ["session"]) vì hai lẽ: lỗi mạng lúc
 * restart không được làm khung trang báo lỗi, và version nhớ từ lượt trước
 * không được lẫn sang lượt này. Lần hỏi được thì chép vào bộ đệm ["session"]
 * để dòng phiên bản cuối trang đổi theo; chỉ chép khi còn đăng nhập, để một
 * câu trả lời lạ lúc panel vừa lên không đẩy người dùng về trang đăng nhập
 * (phiên mất thật thì GET /self trả 401 và client.ts lo việc đó).
 */
export function useSelfSessionPoll(run: SelfRun | undefined, enabled: boolean) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: ["self-session", run?.started ?? ""],
    queryFn: async () => {
      const s = await get<SessionInfo>("/api/v1/session");
      if (s.logged_in) qc.setQueryData(queryKeys.session, s);
      return s;
    },
    enabled,
    retry: false,
    refetchInterval: selfPollMs,
  });
}

/**
 * Cho dòng phiên bản cuối trang: true khi lần kiểm gần nhất thấy bản mới.
 * Chưa có lần kiểm nào, hoặc lần kiểm cũ quá 6 giờ, thì nhờ máy chủ kiểm một
 * lần (auto=1: máy chủ tự dùng lại lần kiểm chưa quá 6 giờ). Mỗi lần tải
 * trang chỉ thử một lần, và lỗi nào cũng bỏ qua: đây là thông tin thêm, không
 * có nó trang vẫn dùng bình thường.
 */
export function useSelfAutoCheck(enabled: boolean): boolean {
  const qc = useQueryClient();
  const state = useSelf(enabled).data;
  const tried = useRef(false);

  useEffect(() => {
    if (!enabled || !state || tried.current || selfRunning(state)) return;
    if (state.check && Date.now() - Date.parse(state.check.checked_at) < autoCheckAfterMs) return;
    tried.current = true;
    post<SelfState>(`${selfUpdatePath}?dry_run=1&auto=1`).then(
      (s) => qc.setQueryData(selfKey, s),
      () => {},
    );
  }, [enabled, state, qc]);

  const c = state?.check;
  return !!c && !c.error && !c.up_to_date;
}
