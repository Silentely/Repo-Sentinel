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
版本：0.1.0（Phase 1 基础平台）

## 产品摘要

RepoSentinel 通过 GitHub App Webhook 与 API 对账监控自有仓库的 Issue、PR、Actions 与安全告警，并支持少量外部公开仓库轮询与 Telegram / HTTP 通知。
**当前已实现**：配置、SQLite/PostgreSQL 存储基础、唯一管理员、Session/CSRF、健康检查、CLI、React 认证壳。
**尚未实现**：Webhook 采集、规则引擎、通知 Outbox、仓库仪表盘、正式 Docker 镜像发布。

## 推荐阅读顺序

1. [功能与路线图](${siteUrl}/features)
2. [快速开始](${siteUrl}/guide/quick-start)
3. [配置参考](${siteUrl}/reference/configuration)
4. [实现状态](${siteUrl}/reference/implementation-status)
5. [运维手册](${siteUrl}/reference/ops)

## 完整文档目录

${DOC_PAGES.map((p) => `- [${p.route}](${siteUrl}${p.route === "/" ? "/" : p.route})`).join("\n")}
`;
  fs.writeFileSync(path.join(publicDir, "llms.txt"), body);
}

ensureDir(publicDir);
copySources();
writeSitemap();
writeLlmsTxt();
console.log("[docs-assets] prepared _sources, sitemap.xml, llms.txt");
