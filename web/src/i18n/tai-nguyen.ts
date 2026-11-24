// Chuỗi của màn Tài nguyên (/usage, mốc M17; mục 7 và bảng thuật ngữ mục 8 của
// docs/GIAO-DIEN.md). "Mức tối đa" thay cho "trần"; "lồng" viết thường trong
// câu; "chạm mức tối đa" là số lần site bị giết vì RAM hoặc bị từ chối vì
// tiến trình.

import { defineStrings } from "./index";

const vi = {
  title: "Tài nguyên đang dùng",
  summary: (n: number) => `${n} site trong lồng. Số liệu lấy thẳng từ máy, tự làm mới mỗi 30 giây.`,

  // Bảng Theo site
  siteTitle: "Theo site",
  siteCaption: "Tài nguyên đang dùng của từng site trong lồng",
  colSite: "Site",
  colOwner: "Chủ",
  colRam: "RAM",
  colRamPeak: "Đỉnh RAM",
  colTasks: "Tiến trình",
  colHits: "Chạm mức tối đa",
  colCpu: "CPU",
  notRunning: "lồng chưa chạy từ khi máy khởi động",
  notRunningHint: "Lồng chưa chạy nên chưa có số liệu.",
  mib: (n: string) => `${n} MiB`,
  percent: (n: string) => `${n}%`,
  hits: (ram: number, tasks: number) => `RAM ${ram}, tiến trình ${tasks}`,
  hitsNone: "0",
  hitsHint: "RAM: số lần bị giết vì chạm mức RAM. Tiến trình: số lần bị từ chối vì chạm mức tiến trình. Đếm từ khi lồng chạy.",
  cpuWait: "Chưa tính được CPU: cần hai lần đọc liên tiếp, lần đọc sau sẽ tới trong vòng 30 giây.",
  cpuHint: "CPU trung bình giữa hai lần đọc gần nhất, tính theo một nhân.",

  // Bảng Theo user
  userTitle: "Theo user",
  userCaption: "Tài nguyên đang dùng cộng theo user",
  colUser: "User",
  colSites: "Site",
  colRamTotal: "RAM đang dùng",
  colPeakMax: "Đỉnh RAM lớn nhất",
  userNote: "Mỗi site có một mức tối đa riêng. Số của user là tổng các site của họ, không phải một mức chung.",

  // Thẻ Gợi ý mức tối đa
  sugTitle: "Gợi ý mức tối đa",
  sugNote: "Tính từ 7 ngày số liệu. Chỉ là gợi ý: muốn áp thì sửa gói.",
  editPackages: "Sửa gói",
  sugRam: "RAM",
  sugCpu: "CPU",
  sugTasks: "Tiến trình",
  sugCurrent: (label: string, cur: string, sug: string) => `${label}: đang ${cur}, gợi ý ${sug}`,
  sugRamValue: (n: number) => (n > 0 ? `${n} MiB` : "không giới hạn"),
  sugCpuValue: (n: number) => (n > 0 ? `${n}%` : "không giới hạn"),
  sugTasksValue: (n: number) => (n > 0 ? String(n) : "không giới hạn"),
  sugFine: "đang hợp lý",
  sugSites: (names: string[], rest: number) => names.join(", ") + (rest > 0 ? ` và ${rest} site khác` : ""),
  sugDetail: (pkg: string) => `Số liệu từng site của gói ${pkg}`,
  sugSamples: (n: number) => `${n} mẫu`,
  sugRange: (from: string, to: string) => `từ ${from} đến ${to}`,
  sugLowData: "Ít số liệu",
  sugLowDataHint: "Dưới 24 giờ số liệu: gợi ý có thể chưa sát.",
  sugNone: "Chưa có gợi ý: chưa site nào trong lồng có số liệu để tính.",
  sugError: "Chưa đọc được gợi ý mức tối đa.",

  // Rỗng
  emptyLead: "Chưa có site nào trong lồng. Site ngoài lồng dùng chung một dịch vụ PHP nên không có số liệu riêng.",
  emptyAdmin: "Muốn có số liệu, bật lồng ở gói rồi bấm “Vào lồng” ở trang site.",
  emptyLink: "Mở màn Gói",
};

export const strings = defineStrings(vi, {
  title: "Resource usage",
  summary: (n) => `${n === 1 ? "1 site" : `${n} sites`} in a cage. Read straight from the machine, refreshed every 30 seconds.`,

  siteTitle: "By site",
  siteCaption: "Resource usage of each caged site",
  colSite: "Site",
  colOwner: "Owner",
  colRam: "RAM",
  colRamPeak: "RAM peak",
  colTasks: "Processes",
  colHits: "Hit the limit",
  colCpu: "CPU",
  notRunning: "the cage has not run since the machine started",
  notRunningHint: "The cage is not running, so there are no numbers yet.",
  mib: (n) => `${n} MiB`,
  percent: (n) => `${n}%`,
  hits: (ram, tasks) => `RAM ${ram}, processes ${tasks}`,
  hitsNone: "0",
  hitsHint: "RAM: times killed for hitting the RAM limit. Processes: times refused for hitting the process limit. Counted since the cage started.",
  cpuWait: "Not known yet: CPU needs two readings in a row, the next one comes within 30 seconds.",
  cpuHint: "Average CPU between the last two readings, in cores.",

  userTitle: "By user",
  userCaption: "Resource usage added up per user",
  colUser: "User",
  colSites: "Sites",
  colRamTotal: "RAM in use",
  colPeakMax: "Largest RAM peak",
  userNote: "Each site has its own limit. A user's numbers are the sum over their sites, not one shared limit.",

  sugTitle: "Suggested limits",
  sugNote: "Worked out from 7 days of numbers. Only a suggestion: to apply it, edit the package.",
  editPackages: "Edit packages",
  sugRam: "RAM",
  sugCpu: "CPU",
  sugTasks: "Processes",
  sugCurrent: (label, cur, sug) => `${label}: now ${cur}, suggested ${sug}`,
  sugRamValue: (n) => (n > 0 ? `${n} MiB` : "no limit"),
  sugCpuValue: (n) => (n > 0 ? `${n}%` : "no limit"),
  sugTasksValue: (n) => (n > 0 ? String(n) : "no limit"),
  sugFine: "looks right",
  sugSites: (names, rest) => names.join(", ") + (rest > 0 ? ` and ${rest} more ${rest === 1 ? "site" : "sites"}` : ""),
  sugDetail: (pkg) => `Numbers for each site of package ${pkg}`,
  sugSamples: (n) => (n === 1 ? "1 sample" : `${n} samples`),
  sugRange: (from, to) => `from ${from} to ${to}`,
  sugLowData: "Little data",
  sugLowDataHint: "Less than 24 hours of numbers: the suggestion may be off.",
  sugNone: "No suggestions yet: no caged site has numbers to work from.",
  sugError: "Could not read the suggested limits.",

  emptyLead: "No site is in a cage yet. Sites outside a cage share one PHP service, so they have no numbers of their own.",
  emptyAdmin: "To get numbers, turn on the cage in a package, then press “Put in a cage” on the site's page.",
  emptyLink: "Open the Packages screen",
});
