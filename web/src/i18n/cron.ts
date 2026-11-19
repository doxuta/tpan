// Chuỗi của màn Cron (mục 6.10 của KE-HOACH.md). Màn giữ chỗ của đợt 0; thợ
// giao diện thay bằng bản thật. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Cron",
  summary: "Việc định kỳ của WordPress và lịch backup, chạy bằng cron thật của máy.",
};

export const strings = defineStrings(vi, {
  title: "Cron",
  summary: "WordPress scheduled tasks and the backup schedule, run by the machine's real cron.",
});
