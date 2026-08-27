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
  build: {
    rollupOptions: {
      output: {
        // 框架与图表拆为独立 vendor chunk：版本升级时仅业务 chunk 变化，
        // vendor 保持长缓存（recharts 仅仪表盘 Star 面板使用）。
        // rolldown 仅支持函数形态（对象映射是 Rollup 特性）。
        manualChunks(id) {
          if (!id.includes("node_modules")) return;
          if (id.includes("node_modules/recharts/") || id.includes("node_modules/victory-vendor/") || id.includes("node_modules/d3-")) {
            return "vendor-charts";
          }
          if (id.includes("node_modules/@tanstack/")) return "vendor-query";
          if (id.includes("node_modules/react-dom/") || id.includes("node_modules/react/") || id.includes("node_modules/scheduler/")) {
            return "vendor-react";
          }
        },
      },
    },
  },
});
