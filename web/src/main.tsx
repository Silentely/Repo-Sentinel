import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { AppProviders } from "./app/providers";
import { registerWebMCPTools } from "./lib/webmcp";
import "./styles/tokens.css";
import "./styles/globals.css";

// 页面加载即尝试注册 WebMCP 工具；不支持的环境静默跳过。
// 持有 AbortController：页面卸载（pagehide）时 abort 注销工具；
// 从 bfcache 后退恢复（pageshow.persisted）时重新注册，避免工具被永久注销。
let webmcpController = registerWebMCPTools();
window.addEventListener("pagehide", () => {
  webmcpController?.abort();
  webmcpController = null;
});
window.addEventListener("pageshow", (event) => {
  if (event.persisted && !webmcpController) {
    webmcpController = registerWebMCPTools();
  }
});

const root = document.getElementById("root");
if (!root) {
  throw new Error("RepoSentinel root element is missing");
}

createRoot(root).render(
  <StrictMode>
    <AppProviders />
  </StrictMode>,
);
