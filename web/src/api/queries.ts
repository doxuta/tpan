// Truy vấn dùng chung giữa khung trang và các màn. Dùng cùng queryKey thì
// TanStack Query chỉ gọi API một lần dù nhiều chỗ cùng cần (ví dụ menu đếm
// site và Bảng điều khiển vẽ bản đồ tuyến đều đọc overview).

import { QueryClient, useQuery } from "@tanstack/react-query";
import { ApiError, get } from "./client";
import type { CheckResult, Overview, SessionInfo, Status } from "./types";

export const queryKeys = {
  session: ["session"] as const,
  overview: ["overview"] as const,
};

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Lỗi 4xx (chưa đăng nhập, không có, đầu vào sai) gọi lại cũng vậy: không thử lại.
      retry: (failures, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && failures < 2,
    },
  },
});

export function useSession() {
  return useQuery({ queryKey: queryKeys.session, queryFn: () => get<SessionInfo>("/api/v1/session") });
}

export function useOverview(enabled = true) {
  return useQuery({
    queryKey: queryKeys.overview,
    queryFn: () => get<Overview>("/api/v1/overview"),
    enabled,
    // Logo và biểu tượng tab báo tình trạng máy, nên đọc lại định kỳ cả khi tab nằm nền.
    refetchInterval: 30_000,
    refetchIntervalInBackground: true,
  });
}

/** Mọi phép kiểm trong overview: của stack, của chính tpan và của từng site. */
export function allChecks(o: Overview): CheckResult[] {
  return [...o.stack_checks, ...o.tpan_checks, ...o.sites.flatMap((s) => s.checks)];
}

/**
 * Tình trạng cả máy cho logo: có sự cố đang mở là "fail", không có mà có phép
 * kiểm cảnh báo là "warn", còn lại "pass". Chưa có phép kiểm nào đạt (chưa
 * kiểm, hoặc chỉ toàn "chưa kiểm được") thì null: logo một màu, vì không
 * khẳng định điều chưa có bằng chứng.
 *
 * Sự cố mở khi phép kiểm chuyển sang lỗi (mục 6.7), nên bình thường phép kiểm
 * lỗi nào cũng đã có sự cố. Ta vẫn tính cả phép kiểm lỗi chưa có sự cố (vừa
 * chạy xong, sự cố chưa kịp ghi): logo không được báo "đạt" trong khi một phép
 * kiểm đang đỏ.
 */
export function machineStatus(o: Overview): Status | null {
  const all = allChecks(o);
  if (o.incidents.length > 0 || all.some((c) => c.status === "fail")) return "fail";
  if (all.some((c) => c.status === "warn")) return "warn";
  return all.some((c) => c.status === "pass") ? "pass" : null;
}
