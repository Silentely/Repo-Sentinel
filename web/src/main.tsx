import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { AppProviders } from "./app/providers";
import { registerWebMCPTools } from "./lib/webmcp";
import "./styles/tokens.css";
import "./styles/globals.css";

// 页面加载即尝试注册 WebMCP 工具；不支持的环境静默跳过。
registerWebMCPTools();

const root = document.getElementById("root");
if (!root) {
  throw new Error("RepoSentinel root element is missing");
}

createRoot(root).render(
  <StrictMode>
    <AppProviders />
  </StrictMode>,
);
