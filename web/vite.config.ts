import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8080",
      "/health": "http://127.0.0.1:8080",
      // Agent 发现与 OAuth 端点：本地联调登录页版本/MCP/OAuth 不再需要绕过 dev server。
      "/oauth": "http://127.0.0.1:8080",
      "/.well-known": "http://127.0.0.1:8080",
      "/mcp": "http://127.0.0.1:8080",
      "/openapi.json": "http://127.0.0.1:8080",
      "/metrics": "http://127.0.0.1:8080",
    },
  },
});
