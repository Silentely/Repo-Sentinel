import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    rollupOptions: {
      output: {
        manualChunks: (id) => {
          if (id.includes("node_modules")) {
            // 优先匹配更具体的包，防止 @tanstack/react-* 被误归入 vendor-react
            if (id.includes("@tanstack")) {
              return "vendor-tanstack";
            }
            if (id.includes("lucide-react")) {
              return "vendor-icons";
            }
            if (id.includes("react") || id.includes("react-dom") || id.includes("scheduler")) {
              return "vendor-react";
            }
          }
        },
      },
    },
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8080",
      "/health": "http://127.0.0.1:8080",
      // Agent 发现与 OAuth 端点：本地联调登录页版本/MCP/OAuth 不再需要绕过 dev server。
      "/robots.txt": "http://127.0.0.1:8080",
      "/sitemap.xml": "http://127.0.0.1:8080",
      "/auth.md": "http://127.0.0.1:8080",
      "/openapi.json": "http://127.0.0.1:8080",
      "/.well-known": "http://127.0.0.1:8080",
      "/oauth": "http://127.0.0.1:8080",
      "/mcp": "http://127.0.0.1:8080",
    },
  },
});
