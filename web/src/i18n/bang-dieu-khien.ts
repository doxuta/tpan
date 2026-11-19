// Chuỗi của Màn Bảng điều khiển (trang chủ). Giọng chữ: mục 8 của docs/GIAO-DIEN.md.
// Tên bốn trạng thái và nhãn "đạt: ..." dùng chung nằm ở khung.ts, không lặp lại ở đây.

import { defineStrings } from "./index";

const vi = {
  title: "Bảng điều khiển",
  summary: "Đường đi request của từng site, sự cố đang mở và số liệu máy.",
  // checked: giờ health chạy đầy đủ gần nhất; null khi chưa chạy lần nào.
  summaryLine: (sites: number, checked: string | null, incidents: number, warns: number) =>
    checked === null
      ? `${sites} site · chưa kiểm lần nào`
      : `${sites} site · kiểm lúc ${checked} · ` + (incidents + warns === 0 ? "không có sự cố hay cảnh báo" : `${incidents} sự cố, ${warns} cảnh báo`),
  recheck: "Kiểm lại",
  rechecking: "Đang kiểm…",
  recheckFailed: "Chưa kiểm lại được.",
  loadFailed: "Chưa tải được dữ liệu của Bảng điều khiển.",
  retry: "Thử lại",

  // Bản đồ tuyến
  mapTitle: "Đường đi request của từng site",
  legend: "Chú giải",
  colSite: "SITE",
  colHttp: "HTTP",
  // Tên các chặng; web, php, wordpress, db cũng là tên ga trên bản đồ.
  stage: {
    dns: "DNS",
    tls: "TLS",
    web: "Web server",
    php: "Pool PHP",
    wordpress: "WordPress",
    db: "DB",
    e2e: "HTTP",
    machine: "Máy",
    tpan: "TPAN",
  },
  noSites: "Chưa có site nào.",
  createFirst: "Tạo site đầu tiên",
  stackMissingTitle: "Chưa cài stack",
  stackMissingBody: "Máy này chưa có web server, PHP và MariaDB nên chưa có tuyến nào để vẽ. Chạy một trong hai lệnh này trên máy (bấm để chép), rồi tải lại trang:",
  stackLemp: "LEMP, dùng nginx",
  stackLamp: "LAMP, dùng apache",
  stackCmdNginx: "tpan stack install --web nginx",
  stackCmdApache: "tpan stack install --web apache",
  stationLabel: (domain: string, stage: string) => `${domain} · ${stage}`,
  httpLabel: (domain: string, code: string | null) => `${domain} · HTTP ${code ?? "chưa có mã"}`,
  httpNone: "chưa có mã",
  failLine: (stage: string, summary: string, shared: boolean) => `Hỏng ở ${stage}: ${summary}` + (shared ? " · lỗi của tầng dùng chung" : ""),
  // Dòng số request dưới tên site. short vừa cột 180 px (≥ là "ít nhất"), full cho trình đọc màn hình và tooltip.
  trafficShort: (requests: string, errors: string, partial: boolean) => `${partial ? "≥ " : ""}${requests} req · ${errors} 5xx`,
  trafficFull: (requests: string, errors: string, partial: boolean) => `${partial ? "Ít nhất " : ""}${requests} request, ${errors} lỗi 5xx trong 1 giờ qua`,

  // Bằng chứng của một ga
  panelTitle: (domain: string, stage: string) => `${domain} · ${stage}`,
  panelClose: "Đóng bằng chứng",
  noChecks: "Chưa có phép kiểm nào cho chặng này, nên chưa nói được đạt hay hỏng.",
  sharedTag: "dùng chung cả máy",

  // Máy
  machineTitle: "Máy",
  cpu: "CPU",
  ram: "RAM",
  disk: "ĐĨA",
  swap: "SWAP",
  notMeasured: "—",
  notMeasuredSub: "chưa đo được",
  cpuSub: (load: string, cpus: number) => `tải ${load} · ${cpus} nhân`,
  ofTotal: (total: string) => `trên ${total}`,
  diskSub: (total: string) => `trên ${total} · vạch 80%`,
  swapNone: "không có",
  swapNoneSub: "nên tạo để tránh hết RAM đột ngột",
  machineErrors: "Chưa đọc được:",

  // Tự kiểm của tpan
  tpanTitle: "TPAN tự kiểm",
  tpanEmpty: "Chưa có kết quả tự kiểm. Bấm Kiểm lại.",
  noEvidence: "Phép kiểm này không ghi lại lệnh nào.",

  // Lịch sử máy
  historyTitle: "Lịch sử máy",
  historyEmpty: "Chưa có việc nào. Việc đầu tiên sẽ hiện ở đây.",
  historyAll: "Xem hết lịch sử",
  evidenceLink: "bằng chứng",
  yesterday: "Hôm qua",
  jobStatus: {
    running: "đang chạy",
    succeeded: "xong",
    rolled_back: "lỗi, đã hoàn tác",
    undo_failed: "lỗi, hoàn tác cũng lỗi",
  },

  // Cột bên
  incidentsTitle: "Sự cố đang mở",
  warningsTitle: "Cảnh báo",
  none: "Không có",
  sharedIncident: "Cả máy",
  incidentMeta: (where: string, stage: string, title: string, since: string) => `${where} · chặng hỏng: ${stage} · ${title} · từ ${since}`,
  openIncident: "Xem bằng chứng và cách sửa",
  allIncidents: "Xem mọi sự cố",
  showEvidence: "Xem bằng chứng",
  hideEvidence: "Ẩn bằng chứng",
};

export const strings = defineStrings(vi, {
  title: "Dashboard",
  summary: "The request path of every site, open incidents and machine metrics.",
  summaryLine: (sites, checked, incidents, warns) =>
    checked === null
      ? `${sites} ${sites === 1 ? "site" : "sites"} · never checked`
      : `${sites} ${sites === 1 ? "site" : "sites"} · checked at ${checked} · ` +
        (incidents + warns === 0 ? "no incidents or warnings" : `${incidents} ${incidents === 1 ? "incident" : "incidents"}, ${warns} ${warns === 1 ? "warning" : "warnings"}`),
  recheck: "Check again",
  rechecking: "Checking…",
  recheckFailed: "Could not check again.",
  loadFailed: "Could not load the dashboard data.",
  retry: "Try again",

  mapTitle: "Request path of every site",
  legend: "Legend",
  colSite: "SITE",
  colHttp: "HTTP",
  stage: {
    dns: "DNS",
    tls: "TLS",
    web: "Web server",
    php: "PHP pool",
    wordpress: "WordPress",
    db: "DB",
    e2e: "HTTP",
    machine: "Machine",
    tpan: "TPAN",
  },
  noSites: "No sites yet.",
  createFirst: "Create the first site",
  stackMissingTitle: "No stack yet",
  stackMissingBody: "This machine has no web server, PHP or MariaDB yet, so there are no routes to draw. Run one of these two commands on the machine (press to copy), then reload the page:",
  stackLemp: "LEMP, uses nginx",
  stackLamp: "LAMP, uses apache",
  stackCmdNginx: "tpan stack install --web nginx",
  stackCmdApache: "tpan stack install --web apache",
  stationLabel: (domain, stage) => `${domain} · ${stage}`,
  httpLabel: (domain, code) => `${domain} · HTTP ${code ?? "no code"}`,
  httpNone: "no code",
  failLine: (stage, summary, shared) => `Failed at ${stage}: ${summary}` + (shared ? " · shared-layer failure" : ""),
  trafficShort: (requests, errors, partial) => `${partial ? "≥ " : ""}${requests} req · ${errors} 5xx`,
  trafficFull: (requests, errors, partial) => `${partial ? "At least " : ""}${requests} requests, ${errors} 5xx errors in the last hour`,

  panelTitle: (domain, stage) => `${domain} · ${stage}`,
  panelClose: "Close evidence",
  noChecks: "There is no check for this stage yet, so it cannot say pass or fail.",
  sharedTag: "shared by the whole machine",

  machineTitle: "Machine",
  cpu: "CPU",
  ram: "RAM",
  disk: "DISK",
  swap: "SWAP",
  notMeasured: "—",
  notMeasuredSub: "not measured yet",
  cpuSub: (load, cpus) => `load ${load} · ${cpus} ${cpus === 1 ? "core" : "cores"}`,
  ofTotal: (total) => `of ${total}`,
  diskSub: (total) => `of ${total} · mark at 80%`,
  swapNone: "none",
  swapNoneSub: "add some so running out of RAM is not sudden",
  machineErrors: "Could not read:",

  tpanTitle: "TPAN self-check",
  tpanEmpty: "No self-check results yet. Press Check again.",
  noEvidence: "This check did not record any command.",

  historyTitle: "Machine history",
  historyEmpty: "No jobs yet. The first one will show up here.",
  historyAll: "See all history",
  evidenceLink: "evidence",
  yesterday: "Yesterday",
  jobStatus: {
    running: "running",
    succeeded: "done",
    rolled_back: "failed, rolled back",
    undo_failed: "failed, rollback failed too",
  },

  incidentsTitle: "Open incidents",
  warningsTitle: "Warnings",
  none: "None",
  sharedIncident: "Whole machine",
  incidentMeta: (where, stage, title, since) => `${where} · failed stage: ${stage} · ${title} · since ${since}`,
  openIncident: "See evidence and fixes",
  allIncidents: "See all incidents",
  showEvidence: "Show evidence",
  hideEvidence: "Hide evidence",
});
