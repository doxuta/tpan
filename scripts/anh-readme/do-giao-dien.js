// Đo một màn của TPAN: dài bao nhiêu, chữ cỡ nào, bao nhiêu chữ mono, bao nhiêu ô viền màu chữ.
//
// Hai cách chạy, cùng một file:
//   1. Bằng tay: mở màn cần đo trên Chrome (đã đăng nhập) → bấm ⌥⌘J mở Console → dán cả file này → Enter.
//   2. Bằng máy: `make do-giao-dien` (scripts/anh-readme nhúng file này và chạy nó trong Chrome không giao diện).
// Kết quả là một dòng JSON. Chỉ đọc trang, không bấm gì, không gửi gì đi đâu.
//
// Bản gốc là script chủ dự án dùng để đo bản 1 trên máy thật (mốc M16). Bảy trường đầu giữ nguyên
// cách tính để so được trước và sau; các trường sau thêm cho ngưỡng cứng của M16.
(() => {
  const vis = (e) => {
    const r = e.getBoundingClientRect(), s = getComputedStyle(e);
    return r.width > 1 && r.height > 1 && s.visibility !== "hidden" && s.display !== "none";
  };
  const short = (e) => {
    const cls = (e.getAttribute("class") || "").trim().split(/\s+/).slice(0, 6).join(".");
    return e.tagName.toLowerCase() + (e.id ? "#" + e.id : "") + (cls ? "." + cls : "");
  };
  // 1. Đếm ký tự theo cỡ chữ và theo font (chỉ chữ đang hiện).
  const w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  const sz = {};
  let mono = 0, tot = 0, minPx = Infinity;
  const tiny = [];
  while (w.nextNode()) {
    const t = w.currentNode.textContent.trim();
    const e = w.currentNode.parentElement;
    if (!t || !e || !vis(e)) continue;
    const s = getComputedStyle(e), fs = parseFloat(s.fontSize);
    sz[fs] = (sz[fs] || 0) + t.length;
    tot += t.length;
    if (/mono/i.test(s.fontFamily.split(",")[0])) mono += t.length;
    if (fs < minPx) minPx = fs;
    if (fs < 12 && tiny.length < 8) tiny.push(fs + "px " + short(e) + " «" + t.slice(0, 24) + "»");
  }
  // 2. Đếm ô có viền từ ba cạnh trở lên cùng màu với chữ chính (viền đen ở chế độ sáng).
  const ink = getComputedStyle(document.body).color;
  let inkBoxes = 0;
  const inkSamples = [];
  for (const e of document.querySelectorAll("body *")) {
    if (!vis(e)) continue;
    const s = getComputedStyle(e);
    const sides = ["Top", "Right", "Bottom", "Left"].filter(
      (k) => parseFloat(s["border" + k + "Width"]) > 0 && s["border" + k + "Style"] !== "none");
    if (sides.length >= 3 && sides.every((k) => s["border" + k + "Color"] === ink)) {
      inkBoxes++;
      if (inkSamples.length < 8) inkSamples.push(short(e));
    }
  }
  const m = document.querySelector("main");
  const small = Object.entries(sz).filter(([k]) => parseFloat(k) <= 13).reduce((a, [, v]) => a + v, 0);
  // 3. Thêm cho M16: tên site (liên kết tới trang site) chiếm mấy dòng; đáy hàng thứ năm của bảng đầu tiên.
  const siteLinks = m ? [...m.querySelectorAll('a[href^="/sites/"]')].filter((a) => vis(a) && !a.getAttribute("href").startsWith("/sites/new")) : [];
  let maxLines = 0, longest = "";
  for (const a of siteLinks) {
    const lh = parseFloat(getComputedStyle(a).lineHeight) || parseFloat(getComputedStyle(a).fontSize) * 1.45;
    const lines = Math.max(1, Math.round(a.getBoundingClientRect().height / lh));
    const name = a.textContent.trim();
    if (lines > maxLines || (lines === maxLines && name.length > longest.length)) { maxLines = lines; longest = name; }
  }
  const row5 = m ? m.querySelectorAll("tbody tr")[4] : null;
  return JSON.stringify({
    man: location.pathname,
    cuaSo: [innerWidth, innerHeight],
    trangDaiPx: document.documentElement.scrollHeight,
    soManHinh: +(document.documentElement.scrollHeight / innerHeight).toFixed(1),
    kyTu: tot,
    phanTramTheoCoChu: Object.fromEntries(Object.entries(sz).sort((a, b) => b[1] - a[1]).map(([k, v]) => [k + "px", Math.round((v * 100) / tot)])),
    phanTramChuTu13pxTroXuong: Math.round((small * 100) / tot),
    phanTramChuMono: Math.round((mono * 100) / tot),
    oVienDen: inkBoxes,
    choBamDuocTrongNoiDung: m ? [...m.querySelectorAll("button")].filter(vis).length : null,
    // ---- thêm cho ngưỡng cứng của M16 ----
    coChuNhoNhatPx: minPx === Infinity ? null : minPx,
    chuDuoi12px: tiny,
    mauOVienDen: inkSamples,
    cuonNgangCaTrang: document.documentElement.scrollWidth > innerWidth,
    tenSiteNhieuDongNhat: siteLinks.length ? { soDong: maxLines, ten: longest, soLienKet: siteLinks.length } : null,
    dayHangThu5Px: row5 ? Math.round(row5.getBoundingClientRect().bottom + scrollY) : null,
  });
})();
