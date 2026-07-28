import { defineConfig } from "vitepress";

const repository = process.env.GITHUB_REPOSITORY || "Silentely/Repo-Sentinel";
const repositoryName = repository.split("/")[1] || "Repo-Sentinel";
const isGitHubActions = process.env.GITHUB_ACTIONS === "true";
const editBranch =
  process.env.VITEPRESS_EDIT_BRANCH ||
  process.env.GITHUB_REF_NAME ||
  "main";
const base =
  process.env.VITEPRESS_BASE ||
  (isGitHubActions && !process.env.VERCEL ? `/${repositoryName}/` : "/");
const siteUrl =
  process.env.VITEPRESS_SITE_URL ||
  (process.env.VERCEL
    ? process.env.VITEPRESS_SITE_URL || "https://example.com"
    : isGitHubActions && !process.env.VERCEL
      ? `https://${repository.split("/")[0]}.github.io/${repositoryName}`
      : "http://127.0.0.1:5174");

export default defineConfig({
  lang: "zh-CN",
  title: "RepoSentinel",
  description:
    "自托管 GitHub 仓库动态与安全告警监控平台：Webhook、可靠通知与值守仪表盘。",
  base,
  cleanUrls: true,
  lastUpdated: true,
  ignoreDeadLinks: true,
  srcExclude: ["**/public/**", "**/superpowers/**", "**/operations/**"],

  head: [
    ["link", { rel: "icon", href: `${base}logo.svg`, type: "image/svg+xml" }],
    [
      "link",
      {
        rel: "sitemap",
        href: `${siteUrl}/sitemap.xml`,
        type: "application/xml",
      },
    ],
    [
      "link",
      {
        rel: "alternate",
        type: "text/plain",
        href: `${siteUrl}/llms.txt`,
        title: "llms.txt",
      },
    ],
    ["meta", { name: "theme-color", content: "#C45C26" }],
    ["meta", { name: "author", content: "RepoSentinel" }],
    ["meta", { name: "robots", content: "index,follow" }],
    ["meta", { property: "og:type", content: "website" }],
    ["meta", { property: "og:title", content: "RepoSentinel 文档" }],
    [
      "meta",
      {
        property: "og:description",
        content:
          "自托管 GitHub 仓库监控：Webhook、对账、安全告警与 Telegram 通知。",
      },
    ],
    ["meta", { property: "og:url", content: siteUrl }],
    ["meta", { property: "og:site_name", content: "RepoSentinel" }],
    ["meta", { property: "og:locale", content: "zh_CN" }],
    ["meta", { property: "og:image", content: `${siteUrl}/logo.svg` }],
    [
      "script",
      { type: "application/ld+json" },
      JSON.stringify({
        "@context": "https://schema.org",
        "@type": "SoftwareApplication",
        name: "RepoSentinel",
        description:
          "自托管 GitHub 仓库动态与安全告警监控平台，默认 SQLite，可选 PostgreSQL。",
        url: siteUrl,
        applicationCategory: "DeveloperApplication",
        operatingSystem: "Linux, macOS, Windows, Docker",
        offers: { "@type": "Offer", price: "0", priceCurrency: "CNY" },
        author: { "@type": "Organization", name: "RepoSentinel" },
      }),
    ],
  ],

  markdown: {
    lineNumbers: true,
  },

  themeConfig: {
    logo: "/logo.svg",
    siteTitle: "RepoSentinel",

    nav: [
      { text: "首页", link: "/" },
      { text: "功能介绍", link: "/features" },
      { text: "快速开始", link: "/guide/quick-start" },
      {
        text: "部署",
        items: [
          { text: "Docker 部署", link: "/deploy/docker" },
          { text: "从源码运行", link: "/deploy/source" },
          { text: "反向代理", link: "/deploy/reverse-proxy" },
        ],
      },
      {
        text: "参考",
        items: [
          { text: "配置参考", link: "/reference/configuration" },
          { text: "运维手册", link: "/reference/ops" },
          { text: "系统架构", link: "/reference/architecture" },
          { text: "开发规范", link: "/reference/development" },
          { text: "发布与镜像", link: "/reference/release" },
          { text: "能力与状态", link: "/reference/implementation-status" },
        ],
      },
      { text: "常见问题", link: "/faq" },
    ],

    sidebar: [
      {
        text: "开始",
        items: [
          { text: "功能介绍", link: "/features" },
          { text: "快速开始", link: "/guide/quick-start" },
          { text: "文档总览", link: "/README" },
        ],
      },
      {
        text: "使用指南",
        items: [
          { text: "管理员与 Session", link: "/guide/administrator" },
          { text: "健康检查与版本", link: "/guide/health-and-version" },
        ],
      },
      {
        text: "部署",
        items: [
          { text: "Docker 部署", link: "/deploy/docker" },
          { text: "从源码运行", link: "/deploy/source" },
          { text: "反向代理", link: "/deploy/reverse-proxy" },
        ],
      },
      {
        text: "参考",
        items: [
          { text: "配置参考", link: "/reference/configuration" },
          { text: "运维手册", link: "/reference/ops" },
          { text: "系统架构", link: "/reference/architecture" },
          { text: "开发规范", link: "/reference/development" },
          { text: "发布与镜像", link: "/reference/release" },
          { text: "能力与状态", link: "/reference/implementation-status" },
        ],
      },
      {
        text: "帮助",
        items: [{ text: "常见问题", link: "/faq" }],
      },
    ],

    socialLinks: [
      { icon: "github", link: `https://github.com/${repository}` },
    ],

    search: {
      provider: "local",
      options: {
        translations: {
          button: {
            buttonText: "搜索文档",
            buttonAriaLabel: "搜索文档",
          },
          modal: {
            noResultsText: "无法找到相关结果",
            resetButtonTitle: "清除查询条件",
            footer: {
              selectText: "选择",
              navigateText: "切换",
              closeText: "关闭",
            },
          },
        },
      },
    },

    editLink: {
      pattern: `https://github.com/${repository}/edit/${editBranch}/docs/:path`,
      text: "在 GitHub 上编辑此页面",
    },

    lastUpdated: {
      text: "最后更新于",
      formatOptions: {
        dateStyle: "medium",
        timeStyle: "short",
      },
    },

    outline: {
      level: [2, 3],
      label: "页面导航",
    },

    footer: {
      message:
        '基于 <a href="https://vitepress.dev/">VitePress</a> 构建 · 默认 SQLite，可选 PostgreSQL',
      copyright: "Copyright © 2026 RepoSentinel",
    },

    docFooter: {
      prev: "上一页",
      next: "下一页",
    },
  },
});
