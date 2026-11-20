// Gọi API của M13 (mục 6.12 của KE-HOACH.md): người đang đăng nhập và vai,
// danh sách user, gói, "xem như user này", và luồng "xem trước rồi chạy" dùng
// chung cho mọi thao tác đổi máy của màn Người dùng. Đường dẫn và JSON theo
// internal/server/types_m13.go; kiểu nằm ở types.ts (khối M13).
//
//   const who = useWho();            // who.isAdmin, who.name, who.viewedBy
//   const users = useUsers(who.isAdmin);
//   const flow = usePlanFlow();      // flow.ask({ method: "POST", path, body }) → flow.open.preview → flow.run()

import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useNavigate } from "react-router";
import { del, get, post } from "./client";
import { queryKeys, useSession } from "./queries";
import type { Identity, Job, JobStarted, PackageRow, Preview, SessionInfo, UserRow } from "./types";

export const usersKey = ["users"] as const;
export const packagesKey = ["packages"] as const;

export const usersPath = "/api/v1/users";
export const packagesPath = "/api/v1/packages";
export const userPath = (name: string, sub = "") => `${usersPath}/${encodeURIComponent(name)}${sub}`;
export const packagePath = (name: string) => `${packagesPath}/${encodeURIComponent(name)}`;

export type Who = {
  identity: Identity | undefined;
  /** Tên đang đăng nhập; trong phiên "xem như" là tên user được xem. */
  name: string;
  /** Vai admin. Phiên chưa biết người dùng (server cũ) coi như admin: giao diện chỉ ẩn bớt, quyền thật do server giữ. */
  isAdmin: boolean;
  /** Có khi admin đang "xem như" một user: tên admin thật. */
  viewedBy: string | undefined;
};

/** Người đang đăng nhập, đọc từ GET /api/v1/session (cùng truy vấn với khung trang). */
export function useWho(): Who {
  const u = useSession().data?.user;
  return { identity: u, name: u?.name ?? "", isAdmin: u === undefined || u.role === "admin", viewedBy: u?.viewed_by };
}

export function useUsers(enabled = true) {
  return useQuery({ queryKey: usersKey, queryFn: () => get<UserRow[]>(usersPath), enabled });
}

export function usePackages(enabled = true) {
  return useQuery({ queryKey: packagesKey, queryFn: () => get<PackageRow[]>(packagesPath), enabled });
}

/** Sau mọi thay đổi về user hay gói: đọc lại các thứ có thể đổi theo. */
export function refreshAccounts(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: usersKey });
  void qc.invalidateQueries({ queryKey: packagesKey });
  void qc.invalidateQueries({ queryKey: queryKeys.overview });
  void qc.invalidateQueries({ queryKey: ["jobs"] });
}

/**
 * Vào phiên "xem như user này". Đổi người thì mọi dữ liệu đang giữ trong bộ nhớ
 * là của người cũ, nên tải lại hẳn trang (như đăng xuất) thay vì dọn từng truy vấn.
 */
export function useViewAs() {
  return useMutation({
    mutationFn: (name: string) => post<SessionInfo>(userPath(name, "/view-as")),
    onSuccess: () => window.location.assign("/"),
  });
}

/** Thoát phiên "xem như". Chạy được cả trong phiên chỉ đọc. */
export function useExitViewAs() {
  return useMutation({
    mutationFn: () => post<SessionInfo>("/api/v1/session/view-as/exit"),
    onSuccess: () => window.location.assign("/"),
  });
}

/** Một lệnh đổi máy: gửi hai lần, lần đầu có ?dry_run=1 (xem trước), lần sau chạy thật. */
export type PlanRequest = { method: "POST" | "DELETE"; path: string; body?: unknown };

function send<T>(r: PlanRequest, dry: boolean): Promise<T> {
  const path = dry ? `${r.path}?dry_run=1` : r.path;
  return r.method === "DELETE" ? del<T>(path) : post<T>(path, r.body);
}

/**
 * Ba chặng của mọi thao tác đổi máy (mục 10 của KE-HOACH.md): ask → bản xem
 * trước (open.preview, vẽ bằng PlanReview) → run → chuyển sang /jobs/:id.
 */
export function usePlanFlow() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = useState<{ req: PlanRequest; preview: Preview } | null>(null);
  const [asked, setAsked] = useState<PlanRequest | null>(null);

  const dry = useMutation({
    mutationFn: (req: PlanRequest) => send<Preview>(req, true),
    onSuccess: (preview, req) => setOpen({ req, preview }),
  });
  const run = useMutation({
    mutationFn: (req: PlanRequest) => send<JobStarted>(req, false),
    onSuccess: (job) => {
      refreshAccounts(qc);
      void navigate(`/jobs/${encodeURIComponent(job.job_id)}`);
    },
  });

  return {
    open,
    asked,
    /** Lỗi dựng bản xem trước. */
    dry,
    /** Lỗi chạy thật nằm ở run.error. */
    run,
    ask(req: PlanRequest) {
      setOpen(null);
      run.reset();
      setAsked(req);
      dry.mutate(req);
    },
    start() {
      if (open) run.mutate(open.req);
    },
    close() {
      setOpen(null);
      setAsked(null);
      dry.reset();
      run.reset();
    },
  };
}

/** Xem trước một lệnh (không gắn trạng thái nào); dùng cho thao tác hàng loạt. */
export function previewOf(req: PlanRequest): Promise<Preview> {
  return send<Preview>(req, true);
}

/** Chạy thật một lệnh và trả mã job. */
export function startOf(req: PlanRequest): Promise<JobStarted> {
  return send<JobStarted>(req, false);
}

/**
 * Đợi một job chạy xong (máy chỉ chạy một job một lúc, mục 4.2: lệnh kế tiếp
 * sẽ bị từ chối job_running nếu job này chưa xong). Hỏi lại mỗi giây, tối đa
 * `limitMs`; quá hạn thì trả job đang chạy để bên gọi báo "còn chạy".
 */
export async function waitJob(id: string, limitMs = 10 * 60_000): Promise<Job> {
  const until = Date.now() + limitMs;
  for (;;) {
    const d = await get<{ job: Job }>(`/api/v1/jobs/${encodeURIComponent(id)}`);
    if (d.job.status !== "running" || Date.now() >= until) return d.job;
    await new Promise((r) => setTimeout(r, 1000));
  }
}
