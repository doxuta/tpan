// Chuỗi của thẻ "Đích ngoài máy" trên màn Backup (M18, mục 3.3 của
// docs/thay-doi/M18-thiet-ke.md). Giọng chữ và thuật ngữ: mục 8 của
// docs/GIAO-DIEN.md. "Endpoint", "bucket", "access key", "secret key" giữ tiếng
// Anh vì người vận hành thấy đúng các chữ đó ở trang của nhà cung cấp kho.

import { defineStrings } from "./index";

const vi = {
  heading: "Đích ngoài máy",
  unsetLead:
    "Bản sao lưu đang chỉ nằm trên máy này: mất máy là mất cả bản sao lưu. Đặt một kho S3 bên ngoài thì TPAN tự đẩy từng bản lên đó, kèm gói panel để dựng lại máy.",

  // Xem đích đang đặt
  endpoint: "Endpoint",
  region: "Vùng",
  bucket: "Bucket",
  prefix: "Tiền tố",
  keep: "Số bản giữ trên kho",
  keepValue: (n: number) => `${n} bản mỗi site`,
  addressing: "Kiểu địa chỉ",
  pathStyle: "kiểu đường dẫn (endpoint/bucket)",
  hostStyle: "kiểu bucket.host",
  ca: "Chứng chỉ gốc riêng",
  caYes: "có",
  caNo: "không",
  accessKey: "Khoá truy cập",
  keyLen: (n: number) => `${n} ký tự`,
  backupKey: "Khoá sao lưu",
  backupKeyNone: "chưa có",
  panel: "Gói panel gần nhất",
  panelAt: (when: string) => `lúc ${when}`,
  panelNone: "chưa có gói nào trên kho",
  copies: "Bản sao lưu",
  counts: (remoteOnly: number, other: number, failed: number) => `${remoteOnly} chỉ còn trên kho · ${other} ở kho khác · ${failed} đẩy lỗi`,

  noBackupKey: "Chưa có khoá sao lưu nên gói panel không được tạo.",
  noBackupKeyMore:
    "Gói panel giữ danh sách site, user, gói và cài đặt của TPAN, mã hoá bằng khoá sao lưu. Bản sao lưu của site vẫn được đẩy lên kho. Khoá chỉ sinh bằng lệnh trên máy chủ, và được in ra một lần:",
  panelError: (err: string) => `Lần tạo gói panel gần nhất hỏng: ${err}`,
  targetError: "Lần đẩy gần nhất lên kho hỏng.",
  retryAt: (when: string) => `Lượt đẩy thử lại lúc ${when}.`,

  // Nút
  test: "Thử kết nối",
  testing: "Đang bắt đầu thử…",
  testErrorHeading: "Chưa bắt đầu thử kết nối được",
  remove: "Gỡ đích",
  removeErrorHeading: "Chưa dựng được bản xem trước gỡ đích",
  removeWarning: (n: number) =>
    n > 0
      ? `${n} bản chỉ còn trên kho: các bản này sẽ không tải về được cho tới khi đặt lại đúng đích này. Gỡ đích không xoá gì trên kho và không xoá khoá sao lưu.`
      : "Không bản nào chỉ còn trên kho. Gỡ đích không xoá gì trên kho và không xoá khoá sao lưu.",
  change: "Đổi đích",
  preview: "Xem trước",
  previewing: "Đang dựng bản xem trước…",
  setErrorHeading: "Chưa dựng được bản xem trước đích ngoài máy",
  checksHeading: "Điều kiện chưa đạt",

  // Form
  endpointHint: "Địa chỉ của kho, ví dụ s3.example.com, bắt đầu bằng https. http chỉ được cho kho chạy ngay trên máy này, và khi đó dùng kiểu đường dẫn.",
  endpointMissing: "Nhập endpoint của kho.",
  regionHint: "Để trống thì dùng us-east-1.",
  bucketMissing: "Nhập tên bucket.",
  prefixHint: "Thư mục trong bucket. Để trống thì dùng tpan/<tên máy>.",
  keepLabel: "Số bản giữ trên kho cho mỗi site",
  keepHint: "Từ 1 đến 365. Để trống thì giữ 7 bản. Bản cũ hơn bị dọn khỏi kho; bản trên máy này không đổi.",
  keepInvalid: "Nhập số nguyên từ 1 đến 365, hoặc để trống.",
  vhostLabel: "Dùng kiểu bucket.host",
  vhostHint: "Để tắt là kiểu đường dẫn (endpoint/bucket), hợp với hầu hết kho tự dựng. Bật khi nhà cung cấp chỉ nhận bucket.host.",
  caLabel: "Chứng chỉ gốc (CA) riêng",
  caKeep: "Giữ như cũ",
  caCustom: "Dùng chứng chỉ gốc riêng",
  caNone: "Không dùng",
  caPemLabel: "Chứng chỉ gốc dạng PEM",
  caPemHint: "Dán cả nội dung file, từ dòng -----BEGIN CERTIFICATE-----. Dùng khi kho nội bộ có chứng chỉ do bạn tự cấp.",
  caPemMissing: "Dán chứng chỉ, hoặc chọn Giữ như cũ hay Không dùng.",
  accessKeyLabel: "Khoá truy cập (access key)",
  secretKeyLabel: "Khoá bí mật (secret key)",
  keysHint: "Để trống để giữ khoá đang lưu. Đổi endpoint, vùng, bucket, kiểu địa chỉ hay CA thì phải nhập lại cả hai khoá.",
  keysHintNew: "Hai khoá chỉ được gửi lên máy chủ, không hiện lại ở đâu.",
  keysPair: "Nhập cả hai khoá, hoặc để trống cả hai để giữ khoá đang lưu.",
  keysRequired: "Nhập cả hai khoá: máy chưa lưu khoá nào.",
};

export const strings = defineStrings(vi, {
  heading: "Off-site target",
  unsetLead:
    "Backups live only on this server: lose the server and you lose the backups too. Set an external S3 storage and TPAN uploads each copy there, plus a panel bundle for rebuilding the server.",

  endpoint: "Endpoint",
  region: "Region",
  bucket: "Bucket",
  prefix: "Prefix",
  keep: "Copies kept on storage",
  keepValue: (n) => `${n} ${n === 1 ? "copy" : "copies"} per site`,
  addressing: "Addressing style",
  pathStyle: "path style (endpoint/bucket)",
  hostStyle: "bucket.host style",
  ca: "Own root certificate",
  caYes: "yes",
  caNo: "no",
  accessKey: "Access key",
  keyLen: (n) => `${n} ${n === 1 ? "character" : "characters"}`,
  backupKey: "Backup key",
  backupKeyNone: "none yet",
  panel: "Latest panel bundle",
  panelAt: (when) => `at ${when}`,
  panelNone: "no bundle on storage yet",
  copies: "Backups",
  counts: (remoteOnly, other, failed) => `${remoteOnly} only on storage · ${other} in other storage · ${failed} upload failed`,

  noBackupKey: "There is no backup key, so no panel bundle is made.",
  noBackupKeyMore:
    "The panel bundle holds TPAN's list of sites, users, packages and settings, encrypted with the backup key. Site backups are still uploaded to storage. The key can only be made with a command on the server, and is printed once:",
  panelError: (err) => `The latest panel bundle failed: ${err}`,
  targetError: "The latest upload to storage failed.",
  retryAt: (when) => `The next upload tries again at ${when}.`,

  test: "Test the connection",
  testing: "Starting the test…",
  testErrorHeading: "Could not start the connection test",
  remove: "Remove target",
  removeErrorHeading: "Could not build the preview for removing the target",
  removeWarning: (n) =>
    n > 0
      ? `${n} ${n === 1 ? "copy is" : "copies are"} only on storage: they cannot be downloaded until this exact target is set again. Removing the target deletes nothing on storage and keeps the backup key.`
      : "No copy is only on storage. Removing the target deletes nothing on storage and keeps the backup key.",
  change: "Change target",
  preview: "Preview",
  previewing: "Building the preview…",
  setErrorHeading: "Could not build the off-site target preview",
  checksHeading: "Conditions not met",

  endpointHint: "The storage address, for example s3.example.com, starting with https. http is only allowed for storage running on this server, and then uses path style.",
  endpointMissing: "Enter the storage endpoint.",
  regionHint: "Leave it empty to use us-east-1.",
  bucketMissing: "Enter the bucket name.",
  prefixHint: "The folder inside the bucket. Leave it empty to use tpan/<hostname>.",
  keepLabel: "Copies kept on storage per site",
  keepHint: "From 1 to 365. Leave it empty to keep 7. Older copies are cleaned off storage; copies on this server do not change.",
  keepInvalid: "Enter a whole number from 1 to 365, or leave it empty.",
  vhostLabel: "Use bucket.host style",
  vhostHint: "Off means path style (endpoint/bucket), which suits most self-hosted storage. Turn it on when the provider only accepts bucket.host.",
  caLabel: "Own root certificate (CA)",
  caKeep: "Keep as is",
  caCustom: "Use my own root certificate",
  caNone: "Do not use",
  caPemLabel: "Root certificate in PEM form",
  caPemHint: "Paste the whole file, from the -----BEGIN CERTIFICATE----- line. Use it when internal storage has a certificate you issued yourself.",
  caPemMissing: "Paste the certificate, or choose Keep as is or Do not use.",
  accessKeyLabel: "Access key",
  secretKeyLabel: "Secret key",
  keysHint: "Leave both empty to keep the stored keys. Changing the endpoint, region, bucket, addressing style or CA means entering both keys again.",
  keysHintNew: "Both keys are only sent to the server and never shown again.",
  keysPair: "Enter both keys, or leave both empty to keep the stored keys.",
  keysRequired: "Enter both keys: no keys are stored on this server yet.",
});
