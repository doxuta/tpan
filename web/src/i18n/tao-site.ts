// Chuỗi của màn Tạo site. Giọng chữ: mục 8 của docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  title: "Tạo site",
  summary: "Một site hoặc cả danh sách: xem trước từng bước, rồi chạy.",

  modeGroup: "Cách nhập",
  modeOne: "Một site",
  modeList: "Dán danh sách",

  formHeading: "Site mới",
  domainLabel: "Domain",
  domainHint: "Tên miền đầy đủ của site, ví dụ shop.example.com.",
  domainRequired: "Nhập domain của site, ví dụ shop.example.com.",
  titleLabel: "Tiêu đề site (tuỳ chọn)",
  titleHint: "Hiện trên đầu trang WordPress. Để trống thì dùng domain.",
  emailLabel: "Email quản trị",
  emailHint: "Tài khoản quản trị WordPress dùng email này. Mật khẩu của nó hiện một lần sau khi tạo xong.",
  emailRequired: "Nhập email quản trị, ví dụ admin@example.com.",
  ownerLabel: "Chủ site",
  ownerHint: "Site thuộc user này và tính vào gói của họ. Mặc định là chính bạn.",
  ownerSelf: (name: string) => `${name} (bạn)`,

  preview: "Xem trước",
  previewing: "Đang dựng bản xem trước…",
  errorHeading: "Chưa dựng được bản xem trước",
  checksHeading: "Điều kiện chưa đạt",

  // ---- dán danh sách ----
  listHeading: "Danh sách site mới",
  listLabel: "Danh sách domain",
  listHint: "Mỗi dòng một domain. Dòng trống được bỏ qua; lỗi nói “dòng N” thì đếm từ dòng có domain đầu tiên.",
  listRequired: "Dán ít nhất một domain, mỗi dòng một cái.",
  listEmailHint: "Mọi site trong danh sách dùng chung email này cho tài khoản quản trị WordPress. Mật khẩu của từng site hiện một lần sau khi tạo xong.",
  sslLabel: "SSL cho cả lô",
  sslHint: "Bật thì sau khi tạo xong, TPAN xin chứng chỉ cho từng site, lần lượt. DNS của site phải đã trỏ về máy này.",
  itemsHeading: "Việc sẽ làm với từng domain",
  itemsTable: "Việc sẽ làm với từng domain",
  colDomain: "Domain",
  colAction: "Việc",
  colReason: "Ghi chú",
  actions: {
    create: "tạo",
    skip: "đã có, bỏ qua",
    trash: "đưa vào thùng rác",
    refused: "không làm được",
  },
  itemsSummary: (create: number, skip: number, refused: number) =>
    `Tạo ${create} site` + (skip > 0 ? `, bỏ qua ${skip} site đã có` : "") + (refused > 0 ? `, ${refused} site không làm được` : "") + ".",
  nothingHeading: "Không có site nào để tạo",
  nothingToDo: "Mọi domain trong danh sách đã có trên máy hoặc không làm được, nên không có gì để chạy. Xem cột Ghi chú, sửa danh sách rồi xem trước lại.",
};

export const strings = defineStrings(vi, {
  title: "Create a site",
  summary: "One site or a whole list: preview every step, then run it.",

  modeGroup: "Input mode",
  modeOne: "One site",
  modeList: "Paste a list",

  formHeading: "New site",
  domainLabel: "Domain",
  domainHint: "The full domain name of the site, for example shop.example.com.",
  domainRequired: "Enter the domain of the site, for example shop.example.com.",
  titleLabel: "Site title (optional)",
  titleHint: "Shown at the top of the WordPress pages. Leave it empty to use the domain.",
  emailLabel: "Admin email",
  emailHint: "The WordPress admin account uses this email. Its password is shown once, after the site is created.",
  emailRequired: "Enter the admin email, for example admin@example.com.",
  ownerLabel: "Site owner",
  ownerHint: "The site belongs to this user and counts against their package. It defaults to you.",
  ownerSelf: (name) => `${name} (you)`,

  preview: "Preview",
  previewing: "Building the preview…",
  errorHeading: "Could not build the preview",
  checksHeading: "Conditions not met",

  listHeading: "New sites",
  listLabel: "List of domains",
  listHint: "One domain per line. Empty lines are skipped; when an error says “line N”, count from the first line that has a domain.",
  listRequired: "Paste at least one domain, one per line.",
  listEmailHint: "Every site in the list shares this email for its WordPress admin account. Each site's password is shown once, after it is created.",
  sslLabel: "SSL for the whole batch",
  sslHint: "When on, TPAN requests a certificate for each site, one after another, once they are created. Each site's DNS must already point to this machine.",
  itemsHeading: "What will happen to each domain",
  itemsTable: "What will happen to each domain",
  colDomain: "Domain",
  colAction: "Action",
  colReason: "Note",
  actions: {
    create: "create",
    skip: "already exists, skipped",
    trash: "move to trash",
    refused: "cannot be done",
  },
  itemsSummary: (create, skip, refused) =>
    `Create ${create} ${create === 1 ? "site" : "sites"}` +
    (skip > 0 ? `, skip ${skip} that already ${skip === 1 ? "exists" : "exist"}` : "") +
    (refused > 0 ? `, ${refused} cannot be done` : "") +
    ".",
  nothingHeading: "No site to create",
  nothingToDo: "Every domain in the list already exists on the machine or cannot be done, so there is nothing to run. Read the Note column, fix the list, then preview again.",
});
