// Chuỗi của khối "Phiên bản và cập nhật" (mục 6.14 của KE-HOACH.md): khối nằm
// ở màn Cài đặt và màn Cập nhật, và dòng phiên bản cuối trang dẫn tới nó.
// Giọng chữ: mục 8 của docs/GIAO-DIEN.md. Câu từ chối của máy chủ (409) do
// máy chủ viết, không lặp ở đây.

import { defineStrings } from "./index";

const vi = {
  heading: "Phiên bản và cập nhật",
  version: "Bản đang chạy",
  url: "Nguồn tải",
  loadError: "Chưa đọc được phiên bản và trạng thái cập nhật",

  // Kiểm bản mới
  checkButton: "Kiểm bản mới",
  checking: "Đang hỏi nguồn tải…",
  checkHint: "Chỉ hỏi nguồn tải xem có bản nào khác, chưa tải và chưa thay gì.",
  checkError: "Chưa kiểm được bản mới",
  checkedAt: (when: string) => `Kiểm lúc ${when}.`,
  sourceError: "Không hỏi được nguồn tải",
  sourceErrorHint: "TPAN đang chạy vẫn ổn. Xem máy có ra được mạng không rồi bấm kiểm lại.",
  upToDate: "Đang là bản mới nhất.",

  // Bản xem trước
  found: (v: string) => (v ? `Nguồn tải có bản ${v}.` : "Nguồn tải có bản khác bản đang chạy."),
  pvCurrent: "Bản đang chạy",
  pvLatest: "Bản mới nhất",
  latestUnknown: "Có bản khác bản đang chạy; nguồn tải không ghi tên bản.",
  pvUrl: "Nguồn tải",
  pvAsset: "File sẽ tải",
  pvSha: "SHA256 sẽ kiểm",
  pvBack: "Lên rồi có lùi được không",
  backNote:
    "Được. Bản đang chạy được giữ lại ở /usr/local/bin/tpan.old, và sau khi lên khối này có nút quay về. " +
    "Riêng khi bản mới đổi cấu trúc cơ sở dữ liệu của TPAN thì không quay về bằng nút được; lúc đó đường lùi là bản sao tpan.db bạn giữ từ trước khi lên.",
  updateButton: (v: string) => (v ? `Cập nhật lên ${v}` : "Cập nhật lên bản mới"),
  updateError: "Chưa bắt đầu cập nhật được",

  // Hộp hỏi lại trước khi cập nhật
  confirmTitle: (v: string) => (v ? `Cập nhật TPAN lên ${v}?` : "Cập nhật TPAN lên bản mới?"),
  confirmRestart: "Panel sẽ khởi động lại và mất kết nối chừng vài giây. Trang này tự nối lại.",
  confirmSession: "Phiên đăng nhập giữ nguyên, bạn không phải đăng nhập lại.",
  confirmSites: "Các site không bị đụng tới: web server, PHP và cơ sở dữ liệu vẫn chạy như cũ.",

  // Đang chạy
  updating: "Đang cập nhật TPAN…",
  rollingBack: "Đang quay về bản trước…",
  updatingHint: "Trang này tự hỏi lại máy chủ mỗi 3 giây. Cứ để mở, chưa cần tải lại.",
  restarting: "Panel đang khởi động lại…",

  // Kết quả, và dòng "lượt gần nhất"
  lastLabel: "Lượt gần nhất",
  doneUpdate: (v: string) => `Đã lên ${v}`,
  doneRollback: (v: string) => `Đã quay về ${v}`,
  same: "Đang là bản mới nhất, không thay gì",
  failedUpdate: "Cập nhật không thành, file chạy cũ còn nguyên",
  failedRollback: "Quay về không thành, file đang chạy còn nguyên",
  stuckUpdate: "Lượt cập nhật chưa có kết quả",
  stuckRollback: "Lượt quay về chưa có kết quả",
  stuckBody: "Lượt chạy bị ngắt giữa chừng, hoặc đã quá 3 phút mà panel chưa báo lại. Gõ ba lệnh dưới đây trên máy: xem log của lượt chạy, xem dịch vụ tpan, rồi quay về bản trước nếu panel không lên.",
  manual: "Sửa tay",
  runUpdate: (from: string, to: string) => (to ? `Lên từ ${from} tới ${to}` : `Từ bản ${from}`),
  runRollback: (from: string, to: string) => (to ? `Quay từ ${from} về ${to}` : `Từ bản ${from}`),
  runBy: (when: string, actor: string) => (actor ? `${when}, ${actor} bấm` : when),
  errorLabel: "Lỗi của job",
  jobLink: "Xem job này trong Lịch sử",
  reloadWhy: "Mã giao diện trong trình duyệt còn là của bản cũ. Tải lại trang để dùng giao diện của bản mới.",
  reload: "Tải lại trang",
  hardenNeeded: "Bản này có bản vá cô lập giữa các site mà máy chưa áp.",
  hardenLink: "Áp bản vá cô lập",

  // Quay về bản trước
  rollbackButton: "Quay về bản trước",
  rollbackChecking: "Đang xem bản trước…",
  rollbackHint: "Bản trước còn giữ ở /usr/local/bin/tpan.old. Bấm là xem trước, chưa có gì đổi.",
  rollbackError: "Chưa quay về được",
  rollbackTitle: (current: string, old: string) => `Quay từ ${current} về ${old}?`,
  rollbackWarnDb: "Nếu bản đang chạy đã đổi cấu trúc cơ sở dữ liệu của TPAN thì máy chủ sẽ từ chối, và không có gì thay đổi.",
  rollbackWarnRestart: "Panel cũng sẽ khởi động lại và mất kết nối chừng vài giây. Phiên đăng nhập giữ nguyên, các site không bị đụng tới.",
  rollbackConfirm: (old: string) => `Quay về ${old}`,

  cli: "Thích dòng lệnh thì cùng việc này, gõ trên máy:",

  // Dòng phiên bản cuối trang
  footerNew: "có bản mới",
};

export const strings = defineStrings(vi, {
  heading: "Version and updates",
  version: "Running version",
  url: "Download source",
  loadError: "Could not read the version and update state",

  checkButton: "Check for a new version",
  checking: "Asking the download source…",
  checkHint: "This only asks the download source whether it has a different version; nothing is downloaded or replaced.",
  checkError: "Could not check for a new version",
  checkedAt: (when) => `Checked at ${when}.`,
  sourceError: "Could not reach the download source",
  sourceErrorHint: "The running TPAN is fine. See whether this machine can reach the network, then check again.",
  upToDate: "This is the latest version.",

  found: (v) => (v ? `The download source has version ${v}.` : "The download source has a version different from the running one."),
  pvCurrent: "Running version",
  pvLatest: "Latest version",
  latestUnknown: "The source has a version different from the running one; it does not name the version.",
  pvUrl: "Download source",
  pvAsset: "File to download",
  pvSha: "SHA256 to verify",
  pvBack: "Can you go back afterwards",
  backNote:
    "Yes. The running version is kept at /usr/local/bin/tpan.old, and after the update this block has a button to go back. " +
    "Only when the new version changes the structure of TPAN's database can the button not go back; the way back then is the copy of tpan.db you kept from before the update.",
  updateButton: (v) => (v ? `Update to ${v}` : "Update to the new version"),
  updateError: "Could not start the update",

  confirmTitle: (v) => (v ? `Update TPAN to ${v}?` : "Update TPAN to the new version?"),
  confirmRestart: "The panel will restart and lose its connection for a few seconds. This page reconnects by itself.",
  confirmSession: "Your login session stays; you do not have to log in again.",
  confirmSites: "The sites are not touched: the web server, PHP and the database keep running as they are.",

  updating: "Updating TPAN…",
  rollingBack: "Going back to the previous version…",
  updatingHint: "This page asks the server again every 3 s. Leave it open; no need to reload yet.",
  restarting: "The panel is restarting…",

  lastLabel: "Latest run",
  doneUpdate: (v) => `Updated to ${v}`,
  doneRollback: (v) => `Went back to ${v}`,
  same: "This is the latest version; nothing was replaced",
  failedUpdate: "The update failed; the old running file is untouched",
  failedRollback: "Going back failed; the running file is untouched",
  stuckUpdate: "The update has no result yet",
  stuckRollback: "Going back has no result yet",
  stuckBody: "The run was interrupted, or more than 3 min passed without the panel reporting back. Type the three commands below on the machine: read the log of the run, look at the tpan service, then go back to the previous version if the panel does not come up.",
  manual: "Fix by hand",
  runUpdate: (from, to) => (to ? `Update from ${from} to ${to}` : `From version ${from}`),
  runRollback: (from, to) => (to ? `Back from ${from} to ${to}` : `From version ${from}`),
  runBy: (when, actor) => (actor ? `${when}, pressed by ${actor}` : when),
  errorLabel: "Job error",
  jobLink: "See this job in History",
  reloadWhy: "The interface code in your browser is still the old version's. Reload the page to use the new version's interface.",
  reload: "Reload the page",
  hardenNeeded: "This version has isolation hardening that this machine has not applied yet.",
  hardenLink: "Apply isolation hardening",

  rollbackButton: "Go back to the previous version",
  rollbackChecking: "Looking at the previous version…",
  rollbackHint: "The previous version is kept at /usr/local/bin/tpan.old. Pressing it shows a preview; nothing changes yet.",
  rollbackError: "Could not go back",
  rollbackTitle: (current, old) => `Go back from ${current} to ${old}?`,
  rollbackWarnDb: "If the running version has changed the structure of TPAN's database, the server will refuse, and nothing changes.",
  rollbackWarnRestart: "The panel will also restart and lose its connection for a few seconds. Your login session stays and the sites are not touched.",
  rollbackConfirm: (old) => `Go back to ${old}`,

  cli: "Prefer the command line? The same thing, typed on the machine:",

  footerNew: "new version available",
});
