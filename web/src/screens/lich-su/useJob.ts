// Đọc một job: lần đầu GET /api/v1/jobs/{id}, rồi nghe SSE để có log trực tiếp.
//
// Ba chỗ dễ sai, nên ghi rõ:
//
//  1. Mật khẩu quản trị WordPress (JobDetail.secret, hoặc JobDetail.secrets của
//     job hàng loạt, mỗi site một cái) chỉ có ở MỘT lần GET, và
//     chỉ khi job đã xong (internal/server/api.go, handleJob). Vì vậy:
//       - queryFn lấy secret ra khỏi kết quả trước khi TanStack Query cất vào
//         cache, để cache không bao giờ giữ mật khẩu;
//       - secret nằm trong một biến của module (không localStorage), giữ tới khi
//         người dùng bấm "Đã lưu, ẩn đi" hoặc tải lại trang. Giữ ở module chứ
//         không ở state của component để lỡ bấm sang mục khác rồi quay lại thì
//         chưa mất mật khẩu (mất nó là mất tài khoản quản trị);
//       - lần GET sau (nếu có) không có secret thì không được đè lên cái đã giữ.
//  2. Luồng SSE mở với after = seq cuối đã có, và vẫn bỏ sự kiện có seq đã
//     nhận (phòng EventSource nối lại phát trùng).
//  3. Job đang chạy thì GET đầu chưa có secret. Khi job.done tới phải GET lại:
//     bản đó mang secret, thời điểm kết thúc và lỗi.

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { get, subscribeJobEvents } from "../../api/client";
import type { AdminSecret, DBSecret, JobDetail, JobEvent } from "../../api/types";

// ---- secret: giữ trong bộ nhớ, ngoài cache ----

// Một job site.add có một tài khoản (JobDetail.secret); một job hàng loạt có một
// tài khoản mỗi site (JobDetail.secrets). Cả hai gộp thành một danh sách; mảng
// trong Map chỉ đổi khi gán lại, nên useSyncExternalStore thấy cùng tham chiếu
// giữa hai lần vẽ.
const secrets = new Map<string, AdminSecret[]>();
const noSecrets: AdminSecret[] = [];
const listeners = new Set<() => void>();

function setSecrets(id: string, list: AdminSecret[]) {
  if (list.length > 0) secrets.set(id, list);
  else secrets.delete(id);
  for (const l of listeners) l();
}

function subscribeSecrets(l: () => void) {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

/** dismiss(i) ẩn tài khoản thứ i (theo thứ tự đang hiện); mỗi khối tự ẩn, vì từng cái là một mật khẩu phải lưu. */
export function useAdminSecret(id: string): { secrets: AdminSecret[]; dismiss: (index: number) => void } {
  const list = useSyncExternalStore(
    subscribeSecrets,
    () => secrets.get(id) ?? noSecrets,
    () => noSecrets,
  );
  return { secrets: list, dismiss: (index) => setSecrets(id, list.filter((_, i) => i !== index)) };
}

// M13: mật khẩu DB rời vừa tạo hoặc vừa đổi (JobDetail.db_secret) cũng chỉ có ở một
// lần GET sau khi job xong, nên giữ y như trên: ngoài cache, trong bộ nhớ của
// module, tới khi người dùng bấm "Đã lưu, ẩn đi".
const dbSecrets = new Map<string, DBSecret>();

function setDbSecret(id: string, s: DBSecret | null) {
  if (s) dbSecrets.set(id, s);
  else dbSecrets.delete(id);
  for (const l of listeners) l();
}

export function useDbSecret(id: string): { secret: DBSecret | null; dismiss: () => void } {
  const secret = useSyncExternalStore(
    subscribeSecrets,
    () => dbSecrets.get(id) ?? null,
    () => null,
  );
  return { secret, dismiss: () => setDbSecret(id, null) };
}

// ---- job ----

type JobBase = Omit<JobDetail, "secret" | "secrets" | "db_secret">;

export type StreamState = "idle" | "live" | "lost";

/**
 * Cất mật khẩu của một câu trả lời GET /jobs/{id} vào bộ nhớ của trang. Máy chủ
 * chỉ trả mật khẩu ở đúng một lần GET sau khi job xong, nên MỌI chỗ đọc job
 * (trang chi tiết, thẻ theo dõi ở góc dưới) phải gọi hàm này: chỗ nào đọc mà
 * không cất là làm mất mật khẩu của người dùng. Trả true khi có mật khẩu.
 */
export function stashJobSecrets(id: string, d: JobDetail): boolean {
  const list = [...(d.secret ? [d.secret] : []), ...(d.secrets ?? [])];
  if (list.length > 0) setSecrets(id, list);
  if (d.db_secret) setDbSecret(id, d.db_secret);
  return list.length > 0 || Boolean(d.db_secret);
}

/** Chờ gom sự kiện trước khi vẽ: apt in hàng nghìn dòng, vẽ từng dòng sẽ giật. */
const flushMs = 100;

export function useJob(id: string) {
  const qc = useQueryClient();
  const key = useMemo(() => ["job", id] as const, [id]);

  const query = useQuery({
    queryKey: key,
    queryFn: async (): Promise<JobBase> => {
      const d = await get<JobDetail>(`/api/v1/jobs/${encodeURIComponent(id)}`);
      stashJobSecrets(id, d);
      return { job: d.job, events: d.events };
    },
    // Tiến độ đi qua SSE, không đọc lại theo thời gian hay khi chuyển tab: mỗi lần GET
    // sau job xong có thể là lần cuối còn mật khẩu để lấy.
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
  const data = query.data;

  const lastSeq = useRef(0);
  const [live, setLive] = useState<JobEvent[]>([]);
  const [stream, setStream] = useState<StreamState>("idle");
  const [attempt, setAttempt] = useState(0);
  const [finished, setFinished] = useState(false);

  // Đổi sang job khác (cùng component, đổi :id) thì bỏ sự kiện của job cũ.
  useEffect(() => {
    setLive([]);
    setFinished(false);
    setStream("idle");
    lastSeq.current = 0;
  }, [id]);

  const running = data !== undefined && data.job.status === "running" && !finished;
  const dataRef = useRef(data);
  dataRef.current = data;
  const refetch = query.refetch;

  useEffect(() => {
    if (!running) return;
    lastSeq.current = Math.max(lastSeq.current, dataRef.current?.events.at(-1)?.seq ?? 0);
    let buf: JobEvent[] = [];
    let timer: number | undefined;
    const flush = () => {
      timer = undefined;
      const batch = buf;
      buf = [];
      if (batch.length > 0) setLive((prev) => prev.concat(batch));
    };
    setStream("live");
    const stop = subscribeJobEvents(
      id,
      (e) => {
        if (e.seq <= lastSeq.current) return;
        lastSeq.current = e.seq;
        buf.push(e);
        timer ??= window.setTimeout(flush, flushMs);
      },
      (reason) => {
        window.clearTimeout(timer);
        flush();
        if (reason === "done") {
          setFinished(true);
          setStream("idle");
          void refetch(); // bản này mang secret, thời điểm kết thúc và lỗi
        } else {
          setStream("lost");
        }
      },
      lastSeq.current,
    );
    return () => {
      stop();
      window.clearTimeout(timer);
    };
  }, [id, running, attempt, refetch]);

  const events = useMemo(() => {
    const base = data?.events ?? [];
    if (live.length === 0) return base;
    const last = base.at(-1)?.seq ?? 0;
    return base.concat(live.filter((e) => e.seq > last));
  }, [data, live]);

  const reconnect = useCallback(() => {
    // Kiểm lại trạng thái thật trước: job có thể đã xong trong lúc mất kết nối.
    setStream("idle");
    void qc.refetchQueries({ queryKey: key }).then(() => {
      setFinished(false);
      setAttempt((n) => n + 1);
    });
  }, [qc, key]);

  return { query, job: data?.job, events, running, stream, reconnect };
}
