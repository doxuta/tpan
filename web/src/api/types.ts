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
  /** M13: chặng không kiểm vì site hay DB đang tạm khoá; status luôn "unknown", vẽ xám với nhãn "tạm khoá". */
  suspended?: boolean;
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
  /** M13: user TPAN đã chạy job; không có khi chạy từ dòng lệnh, MCP, bộ hẹn giờ. */
  actor?: string;
  /** M13: chủ của đối tượng job chạm vào. */
  owner?: string;
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
  | "job.done"
  /** M6: kết quả một site của job hàng loạt; message là JSON của UnitResult. */
  | "unit.done";

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
  /** M6: như secret, cho job hàng loạt (POST /apply), một dòng mỗi site. */
  secrets?: AdminSecret[];
  /** M13: mật khẩu DB rời vừa tạo hoặc vừa đổi. Hiện một lần. */
  db_secret?: DBSecret;
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
  /** M6: hạn xoá hẳn khi site ở thùng rác. */
  purge_at?: string;
  /** M13: chủ của site (tên user TPAN). */
  owner: string;
  /** M13: có khi site đang tạm khoá. */
  suspended?: Suspension;
  suspended_at?: string;
  /** M13: có khi user DB của site đang khoá. */
  db_suspended?: Suspension;
  /** M14: chế độ .htaccess của site (chỉ có tác dụng trên Apache). Server cũ không gửi. */
  htaccess?: "an-toan" | "tat" | "day-du";
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
  /** M14: access.log và error.log của site. */
  access_log?: string;
  error_log?: string;
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
  /** Socket của pool site đang dùng, theo bản PHP của site (M12). Layout không còn trường socket. */
  socket?: string;
  web: WebServer;
  checks: CheckResult[];
};

/** owner (M13): chỉ admin gửi; không có là chính người gọi. */
export type SiteCreate = { domain: string; title: string; email: string; owner?: string };

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
  /** M13: chỉ có với user thường; khi đó machine, stack_checks, tpan_checks rỗng. */
  quota?: Quota;
  /**
   * M16: số cơ sở dữ liệu đang chạy trong phạm vi người xem (DB của site đang
   * chạy cộng DB rời đang chạy), bằng số hàng status "active" của
   * /api/v1/databases. Server cũ không gửi thì menu không hiện số.
   */
  db_count?: number;
};

export type DatabaseRow = {
  domain: string;
  db: string;
  user: string;
  /** -1 khi không đọc được. */
  size_bytes: number;
  tables: number;
  /** M13: "site" (DB đi kèm site, domain có giá trị) hoặc "standalone" (DB rời, domain rỗng). */
  kind: "site" | "standalone";
  owner: string;
  charset: string;
  collation: string;
  note?: string;
  suspended?: Suspension;
  status: "active" | "trashed";
};

// ---- Đăng nhập, tài khoản ----

/** name (M13): rỗng là "admin". */
export type LoginRequest = { name: string; password: string };

export type SessionInfo = {
  logged_in: boolean;
  demo: boolean;
  hostname?: string;
  version?: string;
  expires?: string;
  /** M13: người đang đăng nhập; không có khi chưa đăng nhập. */
  user?: Identity;
  /** M20: chưa có phiên nhưng có phiếu chờ bước hai còn hạn (hiện chặng nhập mã). */
  mfa_pending?: boolean;
  /** M20: chính sách bắt user này bật hai bước mà họ chưa bật: chỉ vào được /bat-hai-buoc. */
  mfa_enroll_required?: boolean;
  /** M20: panel đang được mở bằng tên của một site của admin (màn Đăng nhập cảnh báo vàng). */
  host_is_site?: boolean;
};

/** code (M20): bắt buộc khi user đã bật hai bước; thiếu thì 400 mfa_code_required. */
export type PasswordChange = { current: string; new: string; code?: string };

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
  kind: "site.add" | "site.rm" | "site.repair" | "stack.repair" | "health.fix" | "ssl.issue";
  domain?: string;
  email?: string;
  title?: string;
  /** Chỉ cho health.fix (M8): id phép kiểm. */
  check_id?: string;
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

// ---- Thùng rác và hàng loạt (M6, internal/server/types_m6.go) ----

/** dbs (M13): DB rời trong thùng rác. */
export type TrashOverview = { days: number; sites: Site[]; dbs: StandaloneDB[] };

export type TrashSettings = { days: number };

export type SitesDefaults = { admin_email?: string; ssl?: boolean };

export type SitesEntry = { domain: string; title?: string; admin_email?: string; ssl?: boolean };

/** Thân của POST /api/v1/apply: cùng trường với sites.yaml, thêm prune. */
export type ApplyRequest = { defaults: SitesDefaults; sites: SitesEntry[]; prune?: boolean };

export type ApplyAction = "create" | "skip" | "trash" | "refused";

export type ApplyItem = {
  domain: string;
  action: ApplyAction;
  ssl?: boolean;
  /** Chỉ có khi refused. */
  code?: string;
  reason?: Text;
};

/** POST /api/v1/apply?dry_run=1. preview vắng khi không có gì để làm. */
export type ApplyPreview = { items: ApplyItem[]; preview?: Preview };

export type UnitStatus = "succeeded" | "rolled_back" | "undo_failed" | "not_run";

/** message của sự kiện unit.done (JSON). */
export type UnitResult = {
  target: string;
  status: UnitStatus;
  /** "prepare" | "commit" | "finish" | "verify"; vắng khi succeeded. */
  phase?: string;
  step_id?: string;
  error?: string;
};

// ---- AI trong panel (M8: internal/ai/brain.go, panel_types.go, server/ai_types.go) ----

/** Một mục của panel. */
export type AIView = { path: string; label: string };

/** Thứ người dùng vừa bấm "Hỏi AI", "Giải thích", "Chẩn đoán" trên đó. */
export type AIPointer = {
  kind: "check" | "incident" | "job" | "job_step" | "log_line" | "plan" | "site";
  /** check: id phép kiểm; incident: id; job: id; job_step: "<job>/<step>"; log_line: "<domain>/<access|error|php>"; plan: id; site: domain. */
  ref: string;
  label?: string;
  /** Chỉ cho log_line: đúng dòng log đang nhìn. */
  text?: string;
};

export type AIChipKey = "view" | "pointer" | "trail" | "evidence" | "machine";

/** Thân POST /api/v1/ai/preview và /api/v1/ai/ask. */
export type AskInput = {
  /** Rỗng: phiên mới. */
  session_id?: string;
  question: string;
  lang: "vi" | "en";
  view: AIView;
  pointer?: AIPointer;
  /** Cũ trước mới sau, tối đa 10. */
  trail?: AIView[];
  /** Khoá các khối người dùng đã bỏ. */
  omit?: AIChipKey[];
};

/** Một khối ngữ cảnh: nội dung là bản đã che, đúng như gửi đi. */
export type AIChip = { key: AIChipKey; label: Text; content: string };

/** POST /api/v1/ai/preview: đúng thứ sẽ gửi (đã che). */
export type AskPreview = {
  session_id?: string;
  question: string;
  chips: (AIChip & { omitted: boolean })[];
  playbook?: string;
  prompt: string;
};

/** POST /api/v1/ai/ask (202). */
export type AskStarted = { session_id: string; seq: number };

export type AIEventKind = "init" | "text" | "tool_call" | "tool_result" | "done" | "error" | "refused";

/**
 * Sự kiện hiển thị của một lượt (SSE "ai" của GET /api/v1/ai/sessions/{id}/turns/{seq}/events).
 * text và done mang TOÀN BỘ câu trả lời tới lúc đó, đã đổi về tên thật: thay chữ cũ bằng chữ mới.
 * done, error, refused là sự kiện cuối.
 */
export type AIEvent = {
  kind: AIEventKind;
  time: string;
  text?: string;
  /** Tên đầy đủ, ví dụ mcp__tpan__site_health. */
  tool?: string;
  input?: unknown;
  is_error?: boolean;
  plan_id?: string;
  version?: string;
  model?: string;
};

/** GET /api/v1/ai/brain. */
export type BrainStatus = {
  /** off: AI tắt (bật bằng sudo tpan mcp config --brain bridge); bridge: chờ tpan brain; demo: bộ não giả. */
  mode: "off" | "bridge" | "demo";
  connected: boolean;
  host?: string;
  claude_version?: string;
  /** Phiên bản Claude Code đã chạy bài kiểm; khác claude_version thì có cảnh báo. */
  tested_version?: string;
  model?: string;
  auth_method?: string;
  warnings?: Text[];
  since?: string;
  /** Lệnh người vận hành gõ trên máy của mình để nối, ví dụ "tpan brain ubuntu@<máy>". */
  connect_command: string;
};

export type AISessionEntry = "mcp" | "panel";

export type AISession = {
  id: string;
  entry: AISessionEntry;
  client?: string;
  question?: string;
  untrusted: boolean;
  created: string;
  last: string;
};

/** Một dòng của GET /api/v1/ai/sessions. */
export type AISessionSummary = AISession & { turns: number; calls: number; plans: number };

export type AITurnStatus = "running" | "done" | "error" | "refused";

export type AITurn = {
  seq: number;
  lang: "vi" | "en";
  /** Bản đã che, đúng như gửi. */
  question: string;
  chips: AIChip[];
  playbook?: string;
  prompt: string;
  /** Đã đổi về tên thật. */
  answer: string;
  events: AIEvent[];
  status: AITurnStatus;
  error?: string;
  started: string;
  ended?: string;
};

/** Một lần gọi công cụ (bảng ai_calls): đúng thứ đi qua lại với AI, bản đã che. */
export type AICall = {
  session_id: string;
  seq: number;
  time: string;
  tool: string;
  input: unknown;
  output: unknown;
  error?: string;
  plan_id?: string;
};

/** GET /api/v1/ai/sessions/{id}. */
export type AISessionDetail = {
  session: AISession;
  turns: AITurn[];
  calls: AICall[];
  plans: PendingPlan[];
};

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

// ---- M10: chuyển LEMP ⇄ LAMP (internal/server/types_m10.go) ----

/**
 * Thân POST /api/v1/stack/switch[?dry_run=1] → Preview | JobStarted. Từ chối
 * 409: web_invalid, stack_not_installed, stack_same_web, precondition_failed
 * (checks là các site đang không trả lời đúng trên web server hiện tại).
 */
export type StackSwitchBody = { to: "nginx" | "apache" };

// ---- M9: backup (mục 6.5), thao tác DB (6.9), cron (6.10); khớp internal/core/types_m9.go và internal/server/types_m9.go ----

export type BackupKind = "full" | "sql";
export type BackupReason = "manual" | "schedule" | "before-restore" | "before-purge" | "before-import";

/** Một dòng của bảng backups (core.Backup). */
export type Backup = {
  id: string;
  domain: string;
  kind: BackupKind;
  reason: BackupReason;
  path: string;
  size: number;
  sha256: string;
  wp_version?: string;
  php_version?: string;
  job_id?: string;
  created: string;
  // M18 (sao lưu ra kho S3, m18-types.ts). local false: file không còn trên máy.
  local: boolean;
  remote: "" | "stored" | "failed";
  remote_at?: string;
  /** Khoá object trên kho; user thường không nhận trường này. */
  remote_key?: string;
  /** Lỗi lần đẩy gần nhất, một dòng; user thường không nhận trường này. */
  remote_error?: string;
  /** Nơi bản nằm, máy chủ tính (core.BackupLocation). Không có khi setting đích hỏng. */
  location?: BackupLocation;
};

/** core.Location*: chỉ trên máy, chỉ trên kho (đích đang đặt), cả hai, ở một đích khác, đẩy lỗi. */
export type BackupLocation = "machine" | "remote" | "both" | "other_target" | "push_failed";

/** core.BackupSettings: at "HH:MM" hoặc "" (tắt lịch). */
export type BackupSettings = { keep: number; at: string };

/** GET /api/v1/backups[?domain=D]. items mới nhất trước, gồm cả backup của site đã xoá hẳn. */
export type BackupsOverview = { settings: BackupSettings; items: Backup[] };

/** POST /api/v1/backups/settings[?dry_run=1] → Preview | JobStarted. */
export type BackupSettingsRequest = { keep: number; at: string };

/** POST /api/v1/databases/{domain}/import[?dry_run=1] → Preview | JobStarted. */
export type DBImportRequest = { backup_id: string };

export type CronStatus = "ok" | "fail" | "skipped";

/** core.CronRun: lần chạy gần nhất của cron WordPress một site. */
export type CronRun = {
  domain: string;
  started: string;
  duration_ns: number;
  status: CronStatus;
  exit_code: number;
  output: string;
};

/** core.CronEntry: một dòng của màn Cron. */
export type CronEntry = {
  domain: string;
  file: string;
  schedule: string;
  command: string;
  enabled: boolean;
  last?: CronRun;
};

/** GET /api/v1/cron (core.CronOverview). */
export type CronOverview = { sites: CronEntry[]; backup: BackupSettings; backup_file?: string };

// ---- M13: nhiều người dùng, gói, tạm khoá, DB rời (internal/server/types_m13.go) ----

export type Role = "admin" | "user";
export type UserStatus = "active" | "suspended";
/** Vì sao bị tạm khoá: "self" là khoá riêng, "user" là khoá theo user chủ. */
export type Suspension = "self" | "user";

/** account.Identity. viewed_by có khi admin đang "xem như" user này (phiên chỉ đọc). */
export type Identity = { name: string; role: Role; viewed_by?: string };

export type User = {
  name: string;
  full_name: string;
  email: string;
  role: Role;
  package: string;
  status: UserStatus;
  created: string;
  suspended_at?: string;
  suspend_reason?: string;
};

/** 0 ở mọi giới hạn là không giới hạn. */
export type Package = {
  name: string;
  max_sites: number;
  max_dbs: number;
  disk_mb: number;
  max_backups: number;
  created: string;
};

/** disk_bytes -1 khi chưa đo lần nào. */
export type Usage = { sites: number; dbs: number; disk_bytes: number; disk_measured?: string };

export type UserRow = { user: User; limits: Package; usage: Usage; over_quota: boolean };
export type UserCreate = {
  name: string;
  full_name: string;
  email: string;
  role: Role;
  package: string;
  /** Không có thì tpan sinh và trả về một lần trong UserCreated.password. */
  password?: string;
};
export type UserCreated = { user: UserRow; password?: string };
export type UserEdit = { full_name?: string; email?: string; role?: Role; package?: string };
export type PasswordSet = { password?: string };
export type PasswordSetResult = { password?: string };
export type SuspendRequest = { reason?: string };

export type PackageRow = { package: Package; users: number };
export type Quota = { package: Package; usage: Usage };

export type Collation = { charset: string; collation: string; default: boolean };
export type DBCreate = { owner?: string; name: string; charset?: string; collation?: string; note?: string };
export type DBEdit = { charset?: string; collation?: string; note?: string };
export type DBCharset = { charset: string; collation: string };
export type DBSecret = { db: string; user: string; password: string };

/** account.Database: một DB rời (trong thùng rác thì status "trashed"). */
export type StandaloneDB = {
  name: string;
  owner: string;
  charset: string;
  collation: string;
  note: string;
  status: "active" | "trashed";
  suspended?: Suspension;
  suspended_at?: string;
  created: string;
  trashed_at?: string;
  purge_at?: string;
};
