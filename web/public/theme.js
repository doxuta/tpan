// Đặt data-theme trên <html> trước khi trang vẽ lần đầu, để người dùng chế độ
// tối không thấy màn hình chớp trắng (mục 5 của docs/GIAO-DIEN.md).
// Nạp bằng <script src="/theme.js"> trong <head> vì CSP của panel
// (default-src 'self') không cho script viết thẳng trong HTML.
// Khoá tpan-theme do nút Sáng/Tối ghi (web/src/theme.ts); không có thì theo hệ điều hành.
(function () {
  var t = null;
  try {
    t = localStorage.getItem("tpan-theme");
  } catch (e) {
    // localStorage bị chặn: theo hệ điều hành.
  }
  if (t !== "light" && t !== "dark") {
    t = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }
  document.documentElement.setAttribute("data-theme", t);
})();
