// Kiểu JSON của API /api/v1, khớp từng trường với internal/server/types.go và
// các kiểu Go mà nó trả về (engine, checks, core, site). Đổi một bên thì đổi
// bên kia trong cùng commit. Thời gian là chuỗi RFC 3339.

/** Câu viết sẵn hai ngôn ngữ (i18n.Text). */
export type Text = { vi: string; en: string };

// ---- Lỗi (mục 9 của KE-HOACH.md) ----

export type ErrorBody = {
  error: {
    code: string;
    message: Text;
    checks?: CheckResult[];
  };
};

// ---- Phép kiểm (internal/checks) ----

export type Status = "pass" | "warn" | "fail" | "unknown";

export type Stage =
  | "dns"
  | "tls"
  | "web"
  | "php"
  | "wordpress"
  | "db"
  | "e2e"
  | "machine"
  | "tpan";

export type Evidence = {
  command: string;
  exit_code: number;
  output: string;
};

export type CheckResult = {
  id: string;
  stage: Stage;
  title: Text;
  status: Status;
  summary: Text;
  evidence: Evidence[] | null;
  cause?: Text;
  manual_fix?: string[];
  auto_fix?: string;
};

// ---- Engine: Plan, job, sự kiện (internal/engine) ----

export type FileChange = {
  path: string;
  /** base64 của nội dung mới; không có khi delete. */
  new?: string;
  delete?: boolean;
  /** base64 của nội dung đang có trên máy (engine điền lúc xem trước). */
  old?: string;
  old_exists: boolean;
};

export type StepInfo = {
  id: string;
  title: Text;
  why: Text;
  commands?: string[];
  files?: FileChange[];
  danger?: boolean;
  no_undo?: Text;
};

export type StepPreview = {
  info: StepInfo;
  /** Máy đã đúng, bước sẽ được bỏ qua. */
  done: boolean;
  check_error?: string;
};

export type Preview = {
  kind: string;
  target: string;
  title: Text;
  steps: StepPreview[];
};

export type JobStatus = "running" | "succeeded" | "rolled_back" | "undo_failed";

export type Job = {
  id: string;
  kind: string;
  target: string;
  title: Text;
  status: JobStatus;
  started: string;
  finished?: string;
  error?: string;
};

export type EventKind =
  | "job.start"
  | "step.start"
  | "step.skip"
  | "command"
  | "output"
  | "step.done"
  | "step.fail"
  | "undo.start"
  | "undo.done"
  | "undo.fail"
  | "check"
  | "job.done";

export type JobEvent = {
  job_id: string;
  seq: number;
  time: string;
  kind: EventKind;
  step_id?: string;
  message?: string;
  check?: CheckResult;
};

export type JobStarted = { job_id: string };

export type AdminSecret = { url: string; user: string; password: string };

export type JobDetail = {
  job: Job;
  events: JobEvent[];
  /** Chỉ có ở lần đầu đọc job site.add vừa cài WordPress. Hiện một lần. */
  secret?: AdminSecret;
};

// ---- Site (internal/site, internal/core) ----

export type SiteStatus = "active" | "trashed";

export type Site = {
  domain: string;
  slug: string;
  status: SiteStatus;
  php_version: string;
  ssl: boolean;
  created: string;
  trashed_at?: string;
};

export type Layout = {
  domain: string;
  slug: string;
  user: string;
  db: string;
  dir: string;
  public: string;
  tmp: string;
  wp_config: string;
  log_dir: string;
  php_error_log: string;
  pool: string;
  socket: string;
  nginx_conf: string;
  nginx_link: string;
  apache_conf: string;
  apache_link: string;
};

export type WebServer = "nginx" | "apache" | "";

export type SiteInfo = {
  site: Site;
  layout: Layout;
  web: WebServer;
  checks: CheckResult[];
};

export type SiteCreate = { domain: string; title: string; email: string };

// ---- Stack (internal/core) ----

export type StackConfig = {
  web: WebServer;
  web_source: string;
  db_source: string;
  php_source: string;
  php_version: string;
};

export type StackStatus = {
  config: StackConfig;
  installed: boolean;
  services: CheckResult[];
};

export type Health = {
  installed: boolean;
  config: StackConfig;
  results: CheckResult[];
};

export type StackSummary = {
  installed: boolean;
  web: WebServer;
  php_version: string;
};

// ---- Bảng điều khiển ----

export type DiskUsage = { path: string; total: number; used: number };

export type Machine = {
  /** -1 khi chưa đo được. */
  cpu_percent: number;
  cpus: number;
  load1: number;
  mem_total: number;
  mem_used: number;
  swap_total: number;
  swap_used: number;
  disk: DiskUsage;
  uptime_seconds: number;
  measured: string;
  errors?: string[];
};

export type SiteRow = { site: Site; checks: CheckResult[] };

export type Overview = {
  hostname: string;
  stack: StackSummary;
  sites: SiteRow[];
  machine: Machine;
  jobs: Job[];
  stack_checks: CheckResult[];
};

export type DatabaseRow = {
  domain: string;
  db: string;
  user: string;
  /** -1 khi không đọc được. */
  size_bytes: number;
  tables: number;
};

// ---- Đăng nhập, tài khoản ----

export type LoginRequest = { password: string };

export type SessionInfo = {
  logged_in: boolean;
  demo: boolean;
  hostname?: string;
  version?: string;
  expires?: string;
};

export type PasswordChange = { current: string; new: string };

export type SessionRow = {
  id: string;
  current: boolean;
  ip: string;
  created: string;
  last_seen: string;
};

// ---- SSL (M5, internal/server/types_m5.go, internal/core/ssl.go) ----

export type SSLConfig = {
  email: string;
  /** Chưa đặt thì máy chủ trả true (mặc định bật staging). */
  staging: boolean;
};

export type SSLSiteRow = {
  domain: string;
  ssl: boolean;
  /** Phép kiểm chứng chỉ (tls.cert:<domain>); chỉ có khi ssl là true. */
  cert?: CheckResult;
};

export type SSLOverview = { config: SSLConfig; sites: SSLSiteRow[] };

/** Thân POST /api/v1/ssl/settings; trường vắng mặt thì giữ nguyên. */
export type SSLSettings = { email?: string; staging?: boolean };

// ---- Bảo mật (M5, internal/core/security.go) ----

export type SecurityStatus = { results: CheckResult[] };
