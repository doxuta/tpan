// Chuỗi của màn Cron (mục 6.10 của KE-HOACH.md). Giọng chữ: mục 8 của
// docs/GIAO-DIEN.md. "cron", "wp-config.php", "DISABLE_WP_CRON", "tpan.db" và
// các lệnh không dịch: người vận hành thấy đúng tên đó trên máy.

import { defineStrings } from "./index";

const vi = {
  title: "Cron",
  summary: "Việc định kỳ của WordPress và lịch backup, chạy bằng cron thật của máy.",
  readOnly: "Màn này chỉ để xem. Cron chỉ chạy các việc TPAN định nghĩa sẵn; chưa cho gõ lệnh tuỳ ý vì đó là một đường chạy lệnh tự do, để làm sau.",

  // Cron của từng site
  sitesHeading: "Việc định kỳ của từng site",
  sitesHint: "WordPress tự chạy việc định kỳ mỗi khi có khách vào trang: site vắng khách thì bài hẹn giờ bị trễ, site đông khách thì mỗi lượt xem gánh thêm việc. TPAN tắt cách đó và để cron thật của máy gọi đúng nhịp.",
  sitesFailed: "Chưa đọc được lịch cron của các site.",
  tableName: "Cron của từng site",
  colSite: "Site",
  colJob: "Việc",
  colEvery: "Bao lâu một lần",
  colLast: "Lần chạy gần nhất",
  colFile: "File cron và lệnh",
  jobWp: "Chạy việc định kỳ của WordPress",
  noSites: "Chưa có site nào nên chưa có cron. ",
  noSitesLink: "Tạo site đầu tiên",

  // Nhịp chạy, đọc từ dòng lịch cron
  everyMinute: "Mỗi phút",
  everyMinutes: (n: number) => `${n} phút một lần`,
  everyHour: (m: string) => `Mỗi giờ, vào phút ${m}`,
  everyDay: (hhmm: string) => `Hằng ngày lúc ${hhmm}`,
  everyOther: "Lịch riêng",
  scheduleRaw: "Dòng lịch cron",

  // Lần chạy gần nhất
  statusWord: { ok: "chạy xong", fail: "lỗi", skipped: "bỏ qua" },
  neverRun: "Chưa chạy lần nào",
  exitCode: (n: number) => `mã thoát ${n}`,
  showOutput: "Xem output",
  hideOutput: "Ẩn output",
  showOutputFor: (d: string) => `Xem output lần chạy gần nhất của ${d}`,
  hideOutputFor: (d: string) => `Ẩn output lần chạy gần nhất của ${d}`,
  evidenceLabel: (d: string, exit: number) => `BẰNG CHỨNG · ${d} · MÃ THOÁT ${exit}`,
  emptyOutput: "Lệnh không in gì ra (nó chạy với --quiet), nên chỉ có dòng lệnh ở trên.",
  skippedNote: "Bị bỏ qua vì site không còn đang chạy.",

  // Site không bật
  notEnabled: "không bật",
  trashedLead: "Site đang trong thùng rác nên file cron đã gỡ. Khôi phục site khỏi ",
  trashedLink: "thùng rác",
  trashedTail: " thì file được ghi lại.",
  missingWhy: "Site này chưa có file cron. Cách thêm ở khối bên dưới.",

  // Site chưa có cron
  missingHeading: "Site chưa có cron",
  missingHint: "Site tạo từ bản TPAN cũ chưa có file cron, nên WordPress vẫn tự chạy việc định kỳ theo lượt khách. Chạy lại lệnh dưới đây cho từng site: nó ghi file cron và bật DISABLE_WP_CRON trong wp-config.php. Site vẫn chạy bình thường.",

  // Lịch backup
  backupHeading: "Lịch backup",
  backupHint: "Mỗi ngày một lần, backup mọi site đang chạy rồi chép tpan.db. Giờ và số bản giữ đổi ở màn Backup.",
  backupTableName: "Lịch backup",
  backupJob: "Backup mọi site đang chạy, rồi chép tpan.db",
  colKeep: "Giữ",
  colChange: "Đổi",
  backupDaily: (at: string) => `Hằng ngày lúc ${at}, giờ của máy`,
  backupOff: "Đang tắt",
  backupKeep: (n: number) => `${n} bản mỗi site`,
  backupFileNone: "chưa có file vì lịch đang tắt",
  backupChange: "Đổi ở màn Backup",

  // Phần chung
  durationMs: (n: string) => `${n} ms`,
  durationSec: (n: string) => `${n} giây`,
  durationMin: (m: number, s: number) => `${m} phút ${s} giây`,
};

export const strings = defineStrings(vi, {
  title: "Cron",
  summary: "WordPress scheduled tasks and the backup schedule, run by the machine's real cron.",
  readOnly: "This screen is read-only. Cron only runs the tasks TPAN defines; free-form commands are not allowed yet because that would be an open way to run commands. Maybe later.",

  sitesHeading: "Scheduled tasks per site",
  sitesHint: "WordPress runs its scheduled tasks whenever a visitor loads a page: a quiet site posts scheduled posts late, a busy one makes every page view carry extra work. TPAN turns that off and lets the machine's real cron call it on time.",
  sitesFailed: "Could not read the sites' cron schedules.",
  tableName: "Cron per site",
  colSite: "Site",
  colJob: "Task",
  colEvery: "How often",
  colLast: "Last run",
  colFile: "Cron file and command",
  jobWp: "Run WordPress scheduled tasks",
  noSites: "No sites yet, so no cron yet. ",
  noSitesLink: "Create the first site",

  everyMinute: "Every minute",
  everyMinutes: (n) => `Every ${n} minutes`,
  everyHour: (m) => `Every hour, at minute ${m}`,
  everyDay: (hhmm) => `Daily at ${hhmm}`,
  everyOther: "Custom schedule",
  scheduleRaw: "Cron schedule line",

  statusWord: { ok: "finished", fail: "failed", skipped: "skipped" },
  neverRun: "Has not run yet",
  exitCode: (n) => `exit code ${n}`,
  showOutput: "Show output",
  hideOutput: "Hide output",
  showOutputFor: (d) => `Show the output of the last run of ${d}`,
  hideOutputFor: (d) => `Hide the output of the last run of ${d}`,
  evidenceLabel: (d, exit) => `EVIDENCE · ${d} · EXIT CODE ${exit}`,
  emptyOutput: "The command prints nothing (it runs with --quiet), so only the command line is shown above.",
  skippedNote: "Skipped because the site is no longer running.",

  notEnabled: "off",
  trashedLead: "The site is in the trash, so its cron file was removed. Restore the site from the ",
  trashedLink: "trash",
  trashedTail: " and the file is written again.",
  missingWhy: "This site has no cron file yet. How to add it is in the block below.",

  missingHeading: "Sites without cron",
  missingHint: "Sites created by an older TPAN have no cron file, so WordPress still runs its scheduled tasks on visitor traffic. Run the command below for each site: it writes the cron file and turns on DISABLE_WP_CRON in wp-config.php. The site keeps running.",

  backupHeading: "Backup schedule",
  backupHint: "Once a day, back up every running site and copy tpan.db. Change the time and how many to keep on the Backups screen.",
  backupTableName: "Backup schedule",
  backupJob: "Back up every running site, then copy tpan.db",
  colKeep: "Keep",
  colChange: "Change",
  backupDaily: (at) => `Daily at ${at}, machine time`,
  backupOff: "Off",
  backupKeep: (n) => `${n} per site`,
  backupFileNone: "no file while the schedule is off",
  backupChange: "Change on the Backups screen",

  durationMs: (n) => `${n} ms`,
  durationSec: (n) => `${n} s`,
  durationMin: (m, s) => `${m} min ${s} s`,
});
