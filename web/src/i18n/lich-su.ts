// Chuỗi của Màn Lịch sử: danh sách job và trang một job. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Lịch sử",
  summary: "Các việc đã chạy trên máy, mới nhất ở trên.",
  jobSummary: "Từng bước của job này, output và bằng chứng.",
};

export const strings = defineStrings(vi, {
  title: "History",
  summary: "Jobs that ran on this machine, newest first.",
  jobSummary: "Every step of this job, its output and evidence.",
});
