import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// 页面内链接与真实路由解耦：渲染为普通锚点即可。
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children }: { to: string; children?: ReactNode }) => <a href={to}>{children}</a>,
}));

// 测试数据经 vi.hoisted 注入：vi.mock 工厂提升执行时不能引用模块级变量。
const fixtures = vi.hoisted(() => ({
  dashboard: {
    open_issues: 2,
    open_pulls: 1,
    failed_actions: 0,
    open_security: 0,
    events_24h: 3,
    outbox_dead: 1,
    repos_active: 2,
    repos_baseline: 1,
    channels_enabled: 1,
  },
  settings: {
    "feature.issues": true,
    "feature.pull_requests": true,
    "feature.actions": true,
    "feature.security_alerts": true,
  },
  github: {
    app_id: 123,
    client_id: "Iv1.test",
    private_key_path: "/data/github-app.pem",
    public_base_url: "https://example.com",
    webhook_path: "/api/v1/webhook",
    webhook_url: "https://example.com/api/v1/webhook",
    app_id_configured: true,
    client_id_configured: true,
    private_key_configured: true,
    webhook_secret_configured: true,
    external_pat_configured: false,
    app_id_source: "env",
    client_id_source: "env",
    private_key_source: "env",
    webhook_secret_source: "env",
    public_base_url_source: "env",
    app_id_locked: false,
    client_id_locked: false,
    private_key_locked: false,
    webhook_secret_locked: false,
    public_base_url_locked: false,
    can_edit_in_ui: true,
    note: "",
  },
  repos: {
    items: [
      {
        id: "repo-1",
        type: "github_installation",
        sync_status: "active",
        full_name: "owner/repo-a",
        owner: "owner",
        name: "repo-a",
        is_private: true,
        is_archived: false,
        monitor_enabled: true,
        issues_enabled: true,
        pr_enabled: true,
        actions_enabled: true,
        alerts_enabled: true,
        html_url: "https://github.com/owner/repo-a",
        updated_at: new Date().toISOString(),
      },
      {
        id: "repo-2",
        type: "github_installation",
        sync_status: "baseline_sync",
        full_name: "owner/repo-b",
        owner: "owner",
        name: "repo-b",
        is_private: true,
        is_archived: false,
        monitor_enabled: true,
        issues_enabled: true,
        pr_enabled: true,
        actions_enabled: true,
        alerts_enabled: true,
        html_url: "https://github.com/owner/repo-b",
        updated_at: new Date().toISOString(),
      },
    ],
    page: 1,
    per_page: 100,
    total: 2,
  },
  // 分页数据由 beforeEach 用工厂函数重建，便于单测切换截断/未截断场景。
  events: null as null | { items: Record<string, unknown>[]; page: number; per_page: number; total: number },
  outbox: null as null | { items: Record<string, unknown>[]; page: number; per_page: number; total: number },
}));

// 事件/投递记录工厂：与后端返回结构一致，i 从 0 起。
function makeEvent(i: number) {
  return {
    id: `evt-${i}`,
    kind: "issue",
    action: "opened",
    title: `事件 ${i + 1}`,
    severity: "",
    actor: "octocat",
    html_url: "",
    occurred_at: new Date(Date.now() - i * 60_000).toISOString(),
    repository_id: "repo-1",
  };
}

function makeOutbox(i: number) {
  return {
    id: `out-${i}`,
    channel_id: "ch-1",
    channel_type: i % 2 === 0 ? "http_webhook" : "telegram",
    status: i === 3 ? "dead" : "sent",
    title: `投递 ${i + 1}`,
    attempt_count: i === 3 ? 3 : 1,
    last_error_code: i === 3 ? "http_500" : "",
    html_url: "",
    created_at: new Date(Date.now() - i * 60_000).toISOString(),
    updated_at: new Date(Date.now() - i * 60_000).toISOString(),
  };
}

function makeEventsPage(n: number) {
  return { items: Array.from({ length: n }, (_, i) => makeEvent(i)), page: 1, per_page: 15, total: n };
}

function makeOutboxPage(n: number) {
  return { items: Array.from({ length: n }, (_, i) => makeOutbox(i)), page: 1, per_page: 15, total: n };
}

vi.mock("./api", () => ({
  DASHBOARD_FEED_LIMIT: 15,
  dashboardQueryOptions: {
    queryKey: ["test", "dashboard"],
    queryFn: async () => fixtures.dashboard,
  },
  repositoriesQueryOptions: {
    queryKey: ["test", "repos"],
    queryFn: async () => fixtures.repos,
  },
  eventsQueryOptions: (perPage: number) => ({
    queryKey: ["test", "events", perPage],
    // 模拟后端按 per_page 截断：页面信任服务端只返回上限内的条目。
    queryFn: async () => {
      const page = fixtures.events;
      if (!page) return { items: [], page: 1, per_page: perPage, total: 0 };
      return { ...page, items: page.items.slice(0, perPage), per_page: perPage };
    },
  }),
  outboxQueryOptions: (status = "", channelType = "", perPage = 50) => ({
    queryKey: ["test", "outbox", status, channelType, perPage],
    queryFn: async () => {
      const page = fixtures.outbox;
      if (!page) return { items: [], page: 1, per_page: perPage, total: 0 };
      return { ...page, items: page.items.slice(0, perPage), per_page: perPage };
    },
  }),
  settingsQueryOptions: {
    queryKey: ["test", "settings"],
    queryFn: async () => fixtures.settings,
  },
  githubConfigQueryOptions: {
    queryKey: ["test", "github"],
    queryFn: async () => fixtures.github,
  },
  activateRepository: vi.fn(async () => ({})),
  reconcileAll: vi.fn(async () => {}),
  reconcileRepository: vi.fn(async () => {}),
  retryOutbox: vi.fn(async () => {}),
}));

import { DashboardPage } from "./dashboard-page";

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <DashboardPage />
    </QueryClientProvider>,
  );
}

describe("仪表盘", () => {
  beforeEach(() => {
    // 默认回到「超出展示上限」场景，截断相关的用例各自覆写。
    fixtures.events = makeEventsPage(20);
    fixtures.outbox = makeOutboxPage(20);
  });

  it("渲染关键指标且接入进度卡片隐藏（全部就绪）", async () => {
    renderPage();

    const metrics = await screen.findByRole("region", { name: "关键指标" });
    expect(within(metrics).getByText("开放 Issue")).toBeInTheDocument();
    expect(within(metrics).getByText("24h 事件")).toBeInTheDocument();
    await within(metrics).findByText("3");

    // 等待仓库数据落定（接入进度五步全部就绪）后，卡片应整体隐藏。
    const reposPanel = await screen.findByRole("region", { name: "仓库与基线" });
    await within(reposPanel).findByText("owner/repo-a");
    expect(screen.queryByText("接入进度")).toBeNull();
  });

  it("通知投递与最近事件只展示最近 15 条，超出时徽标显示 15+ 并给出脚注", async () => {
    renderPage();

    const outboxPanel = await screen.findByRole("region", { name: "通知投递" });
    await within(outboxPanel).findByText("投递 1");
    expect(outboxPanel.querySelectorAll(".feed-row")).toHaveLength(15);
    expect(within(outboxPanel).getByText("15+")).toBeInTheDocument();
    expect(within(outboxPanel).getByText(/共 20 条投递记录，仅显示最近 15 条/)).toBeInTheDocument();

    const eventsPanel = await screen.findByRole("region", { name: "最近事件" });
    await within(eventsPanel).findByText("事件 1");
    expect(eventsPanel.querySelectorAll(".feed-row")).toHaveLength(15);
    expect(within(eventsPanel).getByText("15+")).toBeInTheDocument();
    expect(within(eventsPanel).getByText(/共 20 条事件，仅显示最近 15 条/)).toBeInTheDocument();
  });

  it("投递记录行展示渠道标签，超限时提供指向发件箱页的查看全部链接", async () => {
    renderPage();

    const outboxPanel = await screen.findByRole("region", { name: "通知投递" });
    await within(outboxPanel).findByText("投递 1");

    // 偶数下标为 http_webhook、奇数下标为 telegram，前 15 条内各占 8 / 7。
    expect(within(outboxPanel).getAllByText("HTTP Webhook")).toHaveLength(8);
    expect(within(outboxPanel).getAllByText("Telegram")).toHaveLength(7);

    const viewAll = within(outboxPanel).getByRole("link", { name: "查看全部" });
    expect(viewAll).toHaveAttribute("href", "/notifications/outbox");
  });

  it("最近事件行关联仓库名，且事件面板无查看全部链接（暂无独立事件页）", async () => {
    renderPage();

    const eventsPanel = await screen.findByRole("region", { name: "最近事件" });
    await within(eventsPanel).findByText("事件 1");
    expect(within(eventsPanel).getAllByText("owner/repo-a").length).toBeGreaterThan(0);
    expect(within(eventsPanel).queryByRole("link", { name: "查看全部" })).toBeNull();
  });

  it("数据量未超上限时按实际条数展示，不出现截断脚注与查看全部", async () => {
    fixtures.events = makeEventsPage(10);
    fixtures.outbox = makeOutboxPage(10);
    renderPage();

    const outboxPanel = await screen.findByRole("region", { name: "通知投递" });
    await within(outboxPanel).findByText("投递 1");
    expect(outboxPanel.querySelectorAll(".feed-row")).toHaveLength(10);
    expect(within(outboxPanel).getByText("10")).toBeInTheDocument();
    expect(within(outboxPanel).queryByText(/仅显示最近 15 条/)).toBeNull();
    expect(within(outboxPanel).queryByRole("link", { name: "查看全部" })).toBeNull();

    const eventsPanel = await screen.findByRole("region", { name: "最近事件" });
    await within(eventsPanel).findByText("事件 1");
    expect(eventsPanel.querySelectorAll(".feed-row")).toHaveLength(10);
    expect(within(eventsPanel).queryByText(/仅显示最近 15 条/)).toBeNull();
  });

  it("数据量恰好等于上限时不出现截断标记（不误报 15+）", async () => {
    fixtures.events = makeEventsPage(15);
    fixtures.outbox = makeOutboxPage(15);
    renderPage();

    const outboxPanel = await screen.findByRole("region", { name: "通知投递" });
    await within(outboxPanel).findByText("投递 1");
    expect(outboxPanel.querySelectorAll(".feed-row")).toHaveLength(15);
    expect(within(outboxPanel).getByText("15")).toBeInTheDocument();
    expect(within(outboxPanel).queryByText("15+")).toBeNull();
    expect(within(outboxPanel).queryByText(/仅显示最近 15 条/)).toBeNull();
    expect(within(outboxPanel).queryByRole("link", { name: "查看全部" })).toBeNull();

    const eventsPanel = await screen.findByRole("region", { name: "最近事件" });
    await within(eventsPanel).findByText("事件 1");
    expect(within(eventsPanel).queryByText("15+")).toBeNull();
    expect(within(eventsPanel).queryByText(/仅显示最近 15 条/)).toBeNull();
  });
});
