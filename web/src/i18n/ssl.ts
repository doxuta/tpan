// Chuỗi của Màn SSL (mục 6.3 của KE-HOACH.md). Giọng chữ: mục 8 của
// docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "SSL",
  summary: "Chứng chỉ HTTPS của từng site: xin, xin lại và xem còn hạn bao lâu.",

  // Cấu hình
  settingsHeading: "Cấu hình",
  emailLabel: "Email cho Let's Encrypt",
  emailHint: "Let's Encrypt dùng email này để liên hệ về tài khoản. Cần có trước khi xin chứng chỉ đầu tiên.",
  stagingLabel: "Chứng chỉ thử (staging)",
  stagingGroup: "Staging",
  stagingHint:
    "Nên để Bật khi đang thử. Let's Encrypt chỉ cho xin 5 chứng chỉ cho cùng một bộ tên trong 7 ngày, còn staging thì không tính hạn mức đó. Đổi lại, trình duyệt không tin chứng chỉ thử.",
  stagingOn: "Bật",
  stagingOff: "Tắt",
  save: "Lưu",
  saving: "Đang lưu…",
  saved: "Đã lưu.",
  saveFailed: "Chưa lưu được cấu hình",
  confirmTitle: "Tắt staging?",
  confirmText: "Từ giờ certbot xin chứng chỉ thật của Let's Encrypt. Mỗi lần xin tính vào hạn mức 5 chứng chỉ cho cùng một bộ tên trong 7 ngày.",
  confirmLabel: "Tắt staging và lưu",

  // Bảng site
  sitesHeading: "Site",
  tableName: "Chứng chỉ SSL của các site",
  colDomain: "Domain",
  colSsl: "SSL",
  colCert: "Chứng chỉ",
  colDays: "Còn lại",
  colAction: "Việc",
  sslYes: "Có",
  sslNo: "Chưa có",
  noCert: "—",
  days: (n: number) => `${n} ngày`,
  issue: "Xin chứng chỉ",
  reissue: "Xin lại",
  issueFor: (d: string) => `Xin chứng chỉ cho ${d}`,
  reissueFor: (d: string) => `Xin lại chứng chỉ cho ${d}`,
  previewing: "Đang dựng bản xem trước…",
  emptyLead: "Chưa có site nào nên chưa có gì để cấp chứng chỉ. ",
  emptyLink: "Tạo site đầu tiên",

  // Bằng chứng của phép kiểm chứng chỉ
  noEvidence: "Phép kiểm này không ghi lại lệnh nào.",
  cause: "NGUYÊN NHÂN",
  manualFix: "SỬA TAY",
  manualFixHint: "Chạy trên máy chủ, từng dòng một. Dòng cuối kiểm lại.",

  // Xem trước và lỗi
  issueHeading: (d: string) => `Xin chứng chỉ cho ${d}`,
  reissueHeading: (d: string) => `Xin lại chứng chỉ cho ${d}`,
  reissueNote: "Site này đã có chứng chỉ. Xin lại tốn thêm một lượt trong hạn mức của Let's Encrypt.",
  errorHeading: (d: string) => `Chưa dựng được bản xem trước cho ${d}`,
  checksHeading: "Điều kiện chưa đạt",
};

export const strings = defineStrings(vi, {
  title: "SSL",
  summary: "The HTTPS certificate of each site: issue it, issue it again, and see how long it has left.",

  settingsHeading: "Settings",
  emailLabel: "Email for Let's Encrypt",
  emailHint: "Let's Encrypt uses this email to contact you about the account. It is needed before the first certificate.",
  stagingLabel: "Test certificates (staging)",
  stagingGroup: "Staging",
  stagingHint:
    "Keep it On while you are testing. Let's Encrypt allows only 5 certificates for the same set of names in 7 days, and staging does not count against that limit. The catch: browsers do not trust test certificates.",
  stagingOn: "On",
  stagingOff: "Off",
  save: "Save",
  saving: "Saving…",
  saved: "Saved.",
  saveFailed: "Could not save the settings",
  confirmTitle: "Turn staging off?",
  confirmText: "From now on certbot requests real Let's Encrypt certificates. Each request counts against the limit of 5 certificates for the same set of names in 7 days.",
  confirmLabel: "Turn staging off and save",

  sitesHeading: "Sites",
  tableName: "SSL certificates of the sites",
  colDomain: "Domain",
  colSsl: "SSL",
  colCert: "Certificate",
  colDays: "Left",
  colAction: "Action",
  sslYes: "Yes",
  sslNo: "Not yet",
  noCert: "—",
  days: (n) => (n === 1 ? "1 day" : `${n} days`),
  issue: "Issue certificate",
  reissue: "Issue again",
  issueFor: (d) => `Issue a certificate for ${d}`,
  reissueFor: (d) => `Issue the certificate for ${d} again`,
  previewing: "Building the preview…",
  emptyLead: "There are no sites yet, so there is nothing to issue a certificate for. ",
  emptyLink: "Create the first site",

  noEvidence: "This check did not record any command.",
  cause: "CAUSE",
  manualFix: "FIX BY HAND",
  manualFixHint: "Run on the server, one line at a time. The last line checks again.",

  issueHeading: (d) => `Issue a certificate for ${d}`,
  reissueHeading: (d) => `Issue the certificate for ${d} again`,
  reissueNote: "This site already has a certificate. Issuing it again uses one more slot of the Let's Encrypt limit.",
  errorHeading: (d) => `Could not build the preview for ${d}`,
  checksHeading: "Conditions not met",
});
