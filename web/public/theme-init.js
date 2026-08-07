// 主题预置：首帧渲染前同步恢复深色并设置 theme-color，避免深色用户刷新时先闪浅色。
// 取值与判定逻辑须与 src/components/theme-toggle.tsx 保持一致。
// 该脚本必须保持为同源外部文件而非内联：全局 CSP 为 default-src 'self'（无 unsafe-inline），
// 内联脚本会被浏览器拦截，导致主题预置静默失效。
(function () {
  var stored = null;
  try {
    stored = window.localStorage.getItem("reposentinel-theme");
  } catch (e) {
    stored = null;
  }
  var mode = stored === "light" || stored === "dark" || stored === "system" ? stored : "system";
  var dark =
    mode === "dark" ||
    (mode === "system" && typeof window.matchMedia === "function" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
  // theme-color 色值取自 src/styles/tokens.css 的 --bg-canvas：浅色 #efe9df，深色为 oklch(0.205 0.018 48)。
  var meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute("content", dark ? "#1e1510" : "#efe9df");
})();
