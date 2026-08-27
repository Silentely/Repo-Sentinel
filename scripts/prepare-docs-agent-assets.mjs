#!/usr/bin/env node
/**
 * 准备文档站 Agent 发现资产：
 * - 同步 markdown 源到 public/_sources
 * - 生成 sitemap.xml
 * - 生成 llms.txt
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(__dirname, "..");
const docsDir = path.join(root, "docs");
const publicDir = path.join(docsDir, "public");
const sourcesDir = path.join(publicDir, "_sources");
const siteUrl = process.env.VITEPRESS_SITE_URL || "https://example.com";

const DOC_PAGES = [
  { route: "/", file: "index.md", priority: "1.0", changefreq: "weekly" },
  { route: "/features", file: "features.md", priority: "0.9", changefreq: "monthly" },
  { route: "/README", file: "README.md", priority: "0.8", changefreq: "monthly" },
  { route: "/faq", file: "faq.md", priority: "0.7", changefreq: "monthly" },
  {
    route: "/guide/quick-start",
    file: "guide/quick-start.md",
    priority: "0.95",
    changefreq: "monthly",
  },
  {
    route: "/guide/administrator",
    file: "guide/administrator.md",
    priority: "0.85",
    changefreq: "monthly",
  },
  {
    route: "/guide/health-and-version",
    file: "guide/health-and-version.md",
    priority: "0.7",
    changefreq: "monthly",
  },
  {
    route: "/deploy/docker",
    file: "deploy/docker.md",
    priority: "0.95",
    changefreq: "monthly",
  },
  {
    route: "/deploy/source",
    file: "deploy/source.md",
    priority: "0.9",
    changefreq: "monthly",
  },
  {
    route: "/deploy/reverse-proxy",
    file: "deploy/reverse-proxy.md",
    priority: "0.7",
    changefreq: "monthly",
  },
  {
    route: "/reference/configuration",
    file: "reference/configuration.md",
    priority: "0.9",
    changefreq: "monthly",
  },
  {
    route: "/reference/ops",
    file: "reference/ops.md",
    priority: "0.85",
    changefreq: "monthly",
  },
  {
    route: "/reference/architecture",
    file: "reference/architecture.md",
    priority: "0.75",
    changefreq: "monthly",
  },
  {
    route: "/reference/development",
    file: "reference/development.md",
    priority: "0.7",
    changefreq: "monthly",
  },
  {
    route: "/reference/implementation-status",
    file: "reference/implementation-status.md",
    priority: "0.8",
    changefreq: "weekly",
  },
];

function ensureDir(dir) {
  fs.mkdirSync(dir, { recursive: true });
}

function copySources() {
  if (fs.existsSync(sourcesDir)) {
    fs.rmSync(sourcesDir, { recursive: true, force: true });
  }
  ensureDir(sourcesDir);

  for (const page of DOC_PAGES) {
    const src = path.join(docsDir, page.file);
    if (!fs.existsSync(src)) {
      console.warn(`[docs-assets] skip missing ${page.file}`);
      continue;
    }
    const dest = path.join(sourcesDir, page.file === "index.md" ? "home.md" : page.file);
    ensureDir(path.dirname(dest));
    fs.copyFileSync(src, dest);
  }

  const readme = path.join(docsDir, "README.md");
  if (fs.existsSync(readme)) {
    fs.copyFileSync(readme, path.join(sourcesDir, "README.md"));
  }
}

function writeSitemap() {
  const urls = DOC_PAGES.map((page) => {
    const loc = page.route === "/" ? `${siteUrl}/` : `${siteUrl}${page.route}`;
    return `  <url>
    <loc>${loc}</loc>
    <changefreq>${page.changefreq}</changefreq>
    <priority>${page.priority}</priority>
  </url>`;
  }).join("\n");

  const xml = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${urls}
</urlset>
`;
  fs.writeFileSync(path.join(publicDir, "sitemap.xml"), xml);
}

function writeLlmsTxt() {
  const body = `# RepoSentinel

> 自托管 GitHub 仓库动态与安全告警监控平台。

文档站：${siteUrl}
源码：https://github.com/${process.env.GITHUB_REPOSITORY || "Silentely/Repo-Sentinel"}
版本：见仓库 VERSION

## 产品摘要

RepoSentinel 通过 GitHub App Webhook 监控自有仓库的 Issue、PR、Actions 与安全告警，支持可靠通知（Telegram / HTTP Webhook）与值守仪表盘。默认 SQLite，可选 PostgreSQL，Docker Compose 可部署。

## 运行时 Agent 发现端点（部署实例）

- OpenAPI 3.1 接口规范：/openapi.json
- 认证与令牌说明：/auth.md
- RFC 9727 API 目录：/.well-known/api-catalog
- Agent Skills 索引：/.well-known/agent-skills
- MCP Streamable HTTP 网关：/mcp
- OAuth 发现元数据：/.well-known/oauth-authorization-server

## 推荐阅读顺序

1. [功能介绍](${siteUrl}/features)
2. [快速开始](${siteUrl}/guide/quick-start)
3. [Docker 部署](${siteUrl}/deploy/docker)
4. [配置参考](${siteUrl}/reference/configuration)
5. [运维手册](${siteUrl}/reference/ops)

## 完整文档目录

${DOC_PAGES.map((p) => `- [${p.route}](${siteUrl}${p.route === "/" ? "/" : p.route})`).join("\n")}
`;
  fs.writeFileSync(path.join(publicDir, "llms.txt"), body);
}

// robots.txt 由脚本生成（gitignore，不再入库）：
// Sitemap 必须是绝对地址——相对路径多数爬虫会忽略。
function writeRobotsTxt() {
  const body = `User-agent: *
Allow: /

Sitemap: ${siteUrl}/sitemap.xml
`;
  fs.writeFileSync(path.join(publicDir, "robots.txt"), body);
}

ensureDir(publicDir);
copySources();
writeSitemap();
writeLlmsTxt();
writeRobotsTxt();
console.log("[docs-assets] prepared _sources, sitemap.xml, llms.txt, robots.txt");
