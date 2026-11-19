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
  /** Dòng MariaDB khi db_source là "mariadb.org" (M12). */
  db_line?: string;
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

export type SiteRow = {
  site: Site;
  checks: CheckResult[];
  /** M4: request và 5xx trong 1 giờ gần nhất; không có khi chưa đọc được. */
  traffic?: Traffic;
};

export type Overview = {
  hostname: string;
  stack: StackSummary;
  sites: SiteRow[];
  machine: Machine;
  jobs: Job[];
  stack_checks: CheckResult[];
  /** M4: phép kiểm của chính tpan. */
  tpan_checks: CheckResult[];
  /** M4: sự cố đang mở, mới nhất trước. */
  incidents: Incident[];
  /** M4: lần health chạy đầy đủ gần nhất (RFC 3339); không có khi chưa bao giờ. */
  last_run?: string;
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

// ---- health (M4, mục 6.7; internal/store/health.go, internal/server/types.go) ----

/** monitor.Traffic: đếm từ access.log của site. */
export type Traffic = {
  requests: number;
  errors_5xx: number;
  since: string;
  /** Chỉ đọc được phần cuối log, số thật có thể lớn hơn. */
  partial?: boolean;
};

/** store.Incident. */
export type Incident = {
  id: number;
  check_id: string;
  /** "" máy và stack, domain của site, "tpan" cho chính tpan. */
  target: string;
  stage: Stage;
  title: Text;
  opened: string;
  /** Không có khi còn mở. */
  closed?: string;
  close_reason?: "pass" | "gone";
  open_result: CheckResult;
  close_result?: CheckResult;
  /** Id các job sửa đã chạy cho sự cố, cũ trước. */
  jobs: string[];
};

/** store.StoredResult: CheckResult kèm đối tượng và thời điểm. */
export type StoredResult = CheckResult & {
  target: string;
  checked: string;
  /** Lần gần nhất trạng thái đổi. */
  changed: string;
};

/** GET /api/v1/incidents/{id}. */
export type IncidentDetail = {
  incident: Incident;
  /** Kết quả gần nhất; không có khi phép kiểm không còn được chạy. */
  current?: StoredResult;
  jobs: Job[];
};

/** POST /api/v1/fixes[?dry_run=1]: trả Preview (dry_run) hoặc JobStarted. */
export type FixRequest = { check_id: string };

/** POST /api/v1/files/accept: nhận bản sửa tay làm chuẩn mới. */
export type AcceptFileRequest = { path: string };

/** POST /api/v1/health/run: health.Report. */
export type HealthReport = {
  started: string;
  finished: string;
  installed: boolean;
  groups: { target: string; results: CheckResult[] }[];
  changes: { opened: Incident[] | null; closed: Incident[] | null };
};

/** File log của GET /api/v1/sites/{domain}/log?file=... (SSE, sự kiện "line"). */
export type LogFile = "access" | "error" | "php";

/** data của một sự kiện "line". Dòng log là chữ không tin cậy: chỉ hiện như chữ thường. */
export type LogLine = { line: string };

/** data của sự kiện "end": tail đã thoát; đóng EventSource, không tự nối lại. */
export type LogEnd = { error: string };

// ---- Plan chờ duyệt và cổng agent (M7: engine/pending.go, server/plans_types.go) ----

/** Nguồn gốc của một Plan chờ duyệt: người, AI trong panel, agent qua MCP. */
export type PlanOrigin = "human" | "ai" | "mcp";

export type PlanStatus = "pending" | "applied" | "rejected";

/** core.PlanRequest: đủ để dựng lại Plan. */
export type PlanRequest = {
  kind: "site.add" | "site.rm" | "site.repair" | "stack.repair";
  domain?: string;
  email?: string;
  title?: string;
};

export type PendingPlan = {
  id: string;
  kind: string;
  target: string;
  title: Text;
  request: PlanRequest;
  /** Bản xem trước mới nhất; lần duyệt thấy máy đã khác thì server thay bản này (409 plan_changed). */
  preview: Preview;
  fingerprint: string;
  origin: PlanOrigin;
  session_id?: string;
  /** Lúc đề xuất, phiên đó đã đọc nội dung do người lạ gửi tới (log). Màn duyệt hiện cảnh báo. */
  untrusted: boolean;
  status: PlanStatus;
  created: string;
  decided?: string;
  decided_by?: string;
  job_id?: string;
};

export type AgentInfo = {
  approval: "panel" | "client";
  redact: "all" | "secrets";
  hostname: string;
  claude_command: string;
  codex_command: string;
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

// ---- Phiên bản và cập nhật (M12, internal/core/types_m12.go, server/types_m12.go) ----

/** Nguồn gói: "ubuntu", "nginx.org", "nginx.org-mainline", "mariadb.org", "sury". */
export type Source = string;

/** Hạn hỗ trợ của một bản PHP (php.net). Ngày dạng YYYY-MM-DD. */
export type PHPSupport = {
  state: "stable" | "security" | "eol" | string;
  active_until?: string;
  security_until?: string;
};

export type PHPVersionRow = {
  version: string;
  installed: boolean;
  /** Bản mặc định của stack: site mới dùng bản này. */
  default: boolean;
  /** Domain các site đang dùng bản này. */
  sites: string[];
  source: Source;
  /** Không có khi không hỏi được php.net. */
  support?: PHPSupport;
};

/** GET /api/v1/php */
export type PHPList = {
  versions: PHPVersionRow[];
  /** Kho PHP của Ondřej Surý đã thêm; chưa thì chỉ có bản của Ubuntu. */
  sury_enabled: boolean;
  /** Bản PHP WordPress.org khuyên dùng; không có khi không hỏi được. */
  wp_recommended?: string;
  /** false: không hỏi được ghi chú (không mạng), danh sách vẫn đủ. */
  notes_ok: boolean;
};

/** Thân POST /api/v1/php/install, /php/remove, /sites/{domain}/php. */
export type PHPVersionBody = { version: string };

export type PendingPackage = {
  name: string;
  from: string;
  to: string;
  security: boolean;
  origins: string[];
};

export type MariaDBLine = { line: string; eol: string };

export type MariaDBStatus = {
  source: Source;
  line: string;
  version: string;
  /** Dòng LTS nâng lên được; rỗng khi không hỏi được hoặc đã mới nhất. */
  upgrades: MariaDBLine[];
};

export type SelfStatus = {
  version: string;
  url: string;
  /** Có bản cũ để `tpan self-update --rollback`. */
  has_old: boolean;
};

/** GET /api/v1/updates */
export type UpdateStatus = {
  /** Gói của stack có bản mới. */
  packages: PendingPackage[];
  /** Số gói khác của hệ điều hành đang chờ, và bao nhiêu trong đó là bản vá bảo mật. */
  others: number;
  others_security: number;
  /** Lần cuối danh sách gói được làm mới; không có khi không biết. */
  lists_updated?: string;
  /** Phép kiểm updates.auto (bản vá bảo mật tự cài hằng ngày). */
  auto: CheckResult;
  /** Kho ngoài đang được tự vá. */
  auto_origins: string[];
  mariadb: MariaDBStatus;
  self: SelfStatus;
};

/** Thân POST /api/v1/mariadb/upgrade. */
export type MariaDBUpgradeBody = { line: string };
