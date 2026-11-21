// Chuỗi của trang "Cài lên máy ảo mới" (/tools/new-server, mốc M18; mục 3.9
// của docs/thay-doi/M18-thiet-ke.md). Trang sinh user-data cloud-init cho một
// máy chưa tồn tại; không đổi gì trên máy này.

import { defineStrings } from "./index";

const vi = {
  title: "Cài lên máy ảo mới",
  summary: "Sinh user-data cloud-init: máy mới tự cài TPAN ở lần khởi động đầu. Việc này không đổi gì trên máy này.",

  formHeading: "Máy mới",
  webLabel: "Web server",
  webNginx: "nginx",
  webApache: "apache",
  privateLabel: "Panel chỉ mở qua đường hầm SSH",
  privateHint: "Panel của máy mới chỉ nghe 127.0.0.1, ngoài mạng không mở được. Từ máy của bạn, vào bằng:",
  privateCmd: "ssh -L 8443:127.0.0.1:8443 <máy mới>",
  privateOff: "Không chọn thì panel mở ở cổng 8443 cho mọi địa chỉ.",

  baseLabel: "Nguồn tải file chạy",
  baseHint: (v: string) => `Để trống là bản phát hành của chính bản TPAN đang chạy (${v}). Máy mới tải tpan-linux-amd64 hoặc tpan-linux-arm64 từ đây.`,
  versionUnknown: "không rõ bản",

  sumAmdLabel: "sha256 của tpan-linux-amd64",
  sumArmLabel: "sha256 của tpan-linux-arm64",
  sumHint: "Lấy từ file SHA256SUMS của bản phát hành mà bạn tin. Máy chủ không tự tải gì.",
  sumRequired: "Chưa có sha256. Dán 64 ký tự hex từ file SHA256SUMS.",
  sumInvalid: (n: number) => `sha256 phải đúng 64 ký tự hex (0-9, a-f); ô này đang có ${n} ký tự.`,
  sumsWhere: "Chạy trên máy của bạn, không cần quyền root. Lấy hai dòng tpan-linux-amd64 và tpan-linux-arm64.",
  sumsWhereEmpty: "Chạy trên máy của bạn, không cần quyền root. Thay <base> bằng địa chỉ của bản phát hành (nơi có tpan-linux-amd64), rồi lấy hai dòng tpan-linux-amd64 và tpan-linux-arm64.",

  sitesLegend: "Site cho máy mới",
  sitesHint: "Máy mới tạo các site này thành WordPress mới, không chép dữ liệu của máy này. Bật SSL sau, khi DNS đã trỏ về máy mới.",
  sitesNone: "Máy này chưa có site nào đang chạy. Máy mới sẽ không có site.",
  emailLabel: "Email quản trị mặc định",
  emailHint: "Email quản trị WordPress của các site đã chọn. Bắt buộc khi chọn ít nhất một site.",
  emailRequired: "Chưa có email quản trị. Đã chọn site thì cần email này.",

  submit: "Sinh user-data",
  submitting: "Đang sinh…",
  errorHeading: "Chưa sinh được user-data",
  tooLargeHint: "Bỏ bớt site rồi sinh lại. Site còn lại tạo sau trên máy mới, ở màn Tạo site.",

  resultHeading: "Kết quả",
  secretLabel: "Mật khẩu quản trị",
  secretTitle: "Panel của máy mới",
  secretBody: "Chỉ hiện một lần. Chép ra chỗ an toàn ngay; user-data chỉ chứa bản băm của nó.",
  secretUser: "Tên đăng nhập",
  secretPassword: "Mật khẩu",
  secretCopy: "Chép",
  secretCopyOf: (label: string) => `Chép ${label.toLowerCase()}`,
  secretDismiss: "Đã lưu, ẩn đi",
  secretHidden: "Mật khẩu đã ẩn. Sinh lại user-data thì có mật khẩu mới.",

  userDataLabel: "User-data",
  userDataSize: (n: string, max: string) => `${n} byte / trần ${max}`,
  userDataCopy: "Chép user-data",

  howHeading: "Cách dùng",
  howPaste: "Dán vào ô user-data khi tạo máy ở nhà cung cấp.",
  howBoot: "Lần khởi động đầu tự cài TPAN, stack và các site đã chọn. Xong thì đăng nhập panel của máy mới bằng admin và mật khẩu ở trên.",
  howSitePasswords: "Mật khẩu quản trị WordPress của từng site nằm trên máy mới, chỉ root đọc được:",
  howStatus: "Xem tiến độ trên máy mới:",
  howStatusWhere: "Chạy trên máy mới, qua SSH, bằng quyền root (lệnh đã có sudo).",
};

export const strings = defineStrings(vi, {
  title: "Set up a new server",
  summary: "Generate cloud-init user-data: the new server installs TPAN by itself on first boot. This changes nothing on this server.",

  formHeading: "New server",
  webLabel: "Web server",
  webNginx: "nginx",
  webApache: "apache",
  privateLabel: "Panel reachable only through an SSH tunnel",
  privateHint: "The new server's panel only listens on 127.0.0.1 and cannot be opened from the network. From your computer, open it with:",
  privateCmd: "ssh -L 8443:127.0.0.1:8443 <new server>",
  privateOff: "Left unticked, the panel listens on port 8443 on every address.",

  baseLabel: "Download source of the binary",
  baseHint: (v) => `Leave it empty for the release of the TPAN version running here (${v}). The new server downloads tpan-linux-amd64 or tpan-linux-arm64 from it.`,
  versionUnknown: "unknown version",

  sumAmdLabel: "sha256 of tpan-linux-amd64",
  sumArmLabel: "sha256 of tpan-linux-arm64",
  sumHint: "Take it from the SHA256SUMS file of a release you trust. The server downloads nothing by itself.",
  sumRequired: "No sha256 yet. Paste the 64 hex characters from SHA256SUMS.",
  sumInvalid: (n) => `A sha256 is exactly 64 hex characters (0-9, a-f); this field has ${n}.`,
  sumsWhere: "Run it on your computer, no root needed. Take the tpan-linux-amd64 and tpan-linux-arm64 lines.",
  sumsWhereEmpty: "Run it on your computer, no root needed. Replace <base> with the address of the release (where tpan-linux-amd64 is), then take the tpan-linux-amd64 and tpan-linux-arm64 lines.",

  sitesLegend: "Sites for the new server",
  sitesHint: "The new server creates these sites as fresh WordPress installs; no data is copied from this server. Turn on SSL later, once DNS points to the new server.",
  sitesNone: "This server has no running site. The new server will have none.",
  emailLabel: "Default admin email",
  emailHint: "WordPress admin email of the chosen sites. Required when at least one site is chosen.",
  emailRequired: "No admin email yet. It is needed once a site is chosen.",

  submit: "Generate user-data",
  submitting: "Generating…",
  errorHeading: "Could not generate the user-data",
  tooLargeHint: "Drop some sites and generate again. Create the others later on the new server, on the Create a site screen.",

  resultHeading: "Result",
  secretLabel: "Admin password",
  secretTitle: "Panel of the new server",
  secretBody: "Shown once. Copy it somewhere safe now; the user-data only holds its hash.",
  secretUser: "Username",
  secretPassword: "Password",
  secretCopy: "Copy",
  secretCopyOf: (label) => `Copy ${label.toLowerCase()}`,
  secretDismiss: "Saved, hide it",
  secretHidden: "The password is hidden. Generating the user-data again gives a new password.",

  userDataLabel: "User-data",
  userDataSize: (n, max) => `${n} bytes / limit ${max}`,
  userDataCopy: "Copy user-data",

  howHeading: "How to use it",
  howPaste: "Paste it into the user-data field when you create the server at your provider.",
  howBoot: "The first boot installs TPAN, the stack and the chosen sites by itself. Then log in to the new server's panel as admin with the password above.",
  howSitePasswords: "The WordPress admin password of each site is on the new server, readable by root only:",
  howStatus: "Follow the progress on the new server:",
  howStatusWhere: "Run it on the new server, over SSH, as root (the command already has sudo).",
});
