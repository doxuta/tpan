// Chuỗi của màn /domains (M19, mục 3.9 của docs/thay-doi/M19-thiet-ke.md): bảng
// các site kèm số tên phụ và số luật chuyển hướng, mỗi dòng dẫn tới tab tương ứng
// của trang site. Chuỗi hai tab đó nằm ở ten-mien.ts. Giọng chữ: mục 8 của
// docs/GIAO-DIEN.md.

import { defineStrings } from "./index";

const vi = {
  namesTitle: "Tên miền phụ",
  namesSummary: "Tên phụ của từng site. Bấm tên site để xem và đổi tên ở tab Tên miền.",
  redirectsTitle: "Chuyển hướng",
  redirectsSummary: "Luật chuyển hướng theo đường dẫn của từng site. Bấm tên site để xem và đổi luật ở tab Chuyển hướng.",
  caption: "Tên phụ và luật chuyển hướng của các site",
  colSite: "Site",
  colNames: "Tên phụ",
  colRules: "Luật chuyển hướng",
  count: (n: number) => `${n} site`,
  openNames: (domain: string) => `Mở tab Tên miền của ${domain}`,
  openRedirects: (domain: string) => `Mở tab Chuyển hướng của ${domain}`,
  emptyLead: "Chưa có site nào. ",
  emptyLink: "Tạo site đầu tiên",
  loadFailed: "Chưa đọc được danh sách tên miền.",
};

export const strings = defineStrings(vi, {
  namesTitle: "Domains",
  namesSummary: "The secondary names of each site. Select a site to see and change its names on the Domains tab.",
  redirectsTitle: "Redirects",
  redirectsSummary: "The path-based redirect rules of each site. Select a site to see and change its rules on the Redirects tab.",
  caption: "Secondary names and redirect rules of the sites",
  colSite: "Site",
  colNames: "Secondary names",
  colRules: "Redirect rules",
  count: (n) => `${n} ${n === 1 ? "site" : "sites"}`,
  openNames: (domain) => `Open the Domains tab of ${domain}`,
  openRedirects: (domain) => `Open the Redirects tab of ${domain}`,
  emptyLead: "No sites yet. ",
  emptyLink: "Create your first site",
  loadFailed: "Could not read the list of domains.",
});
