import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { AppProviders } from "./app/providers";
import { registerWebMCPTools } from "./lib/webmcp";
import "./styles/tokens.css";
import "./styles/globals.css";

// 页面加载即尝试注册 WebMCP 工具；不支持的环境静默跳过。
// 持有 AbortController：页面卸载（pagehide）时 abort 注销工具，避免工具残留在
// SPA 前进后退后的旧页面上下文（此前 controller 被丢弃，注销逻辑从未触发）。
const webmcpController = registerWebMCPTools();
window.addEventListener("pagehide", () => webmcpController?.abort(), { once: true });

const root = document.getElementById("root");
if (!root) {
  throw new Error("RepoSentinel root element is missing");
}

createRoot(root).render(
  <StrictMode>
    <AppProviders />
  </StrictMode>,
);
