import type { ReactNode } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  TmaConflictError,
  type TmaDashboardData,
  type TmaEventDetailData,
} from "../lib/tma-api";
import * as tmaApi from "../lib/tma-api";
import { tmaHaptics } from "../lib/tma-sdk";
import { TmaDashboardPage } from "./TmaDashboardPage";
import { TmaEventDetailPage } from "./TmaEventDetailPage";
import { TmaLayout } from "./TmaLayout";

let mockRouteParams: Record<string, string | undefined> = { id: "evt-123" };
const mockNavigate = vi.fn();

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params,
    children,
    className,
  }: {
    to: string;
    params?: Record<string, string>;
    children?: ReactNode;
    className?: string;
  }) => {
    let href = to;
    if (params) {
      for (const [k, v] of Object.entries(params)) {
        href = href.replace(`$${k}`, v);
      }
    }
    return (
      <a href={href} className={className}>
        {children}
      </a>
    );
  },
  useParams: () => mockRouteParams,
  useNavigate: () => mockNavigate,
  Outlet: () => <div data-testid="tma-outlet">Outlet Child Content</div>,
}));

vi.mock("../lib/tma-api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../lib/tma-api")>();
  return {
    ...actual,
    fetchTmaDashboard: vi.fn(),
    fetchTmaEvent: vi.fn(),
    fetchTmaEventDiff: vi.fn(),
    fetchTmaEventLogs: vi.fn(),
    executeTmaAction: vi.fn(),
    toggleTmaQuietHours: vi.fn(),
  };
});

describe("TmaLayout & Pages", () => {
  const originalTelegram = window.Telegram;

  beforeEach(() => {
    vi.restoreAllMocks();
    mockRouteParams = { id: "evt-123" };
  });

  afterEach(() => {
    window.Telegram = originalTelegram;
  });

  function setupTelegramEnv() {
    window.Telegram = {
      WebApp: {
        initData: "query_id=AAHd&user=%7B%22id%22%3A123%7D&auth_date=1620000000&hash=abc",
        initDataUnsafe: {
          query_id: "AAHd",
          user: { id: 123, first_name: "Test" },
          auth_date: "1620000000",
          hash: "abc",
        },
        themeParams: {},
        isExpanded: true,
        viewportHeight: 600,
        viewportStableHeight: 600,
        BackButton: {
          isVisible: false,
          onClick: vi.fn(),
          offClick: vi.fn(),
          show: vi.fn(),
          hide: vi.fn(),
        } as any,
        HapticFeedback: {
          impactOccurred: vi.fn(),
          notificationOccurred: vi.fn(),
          selectionChanged: vi.fn(),
        } as any,
        ready: vi.fn(),
        expand: vi.fn(),
        close: vi.fn(),
        onEvent: vi.fn(),
        offEvent: vi.fn(),
      },
    };
  }

  describe("TmaLayout", () => {
    it("renders browser fallback placeholder when opened outside telegram and makes zero API calls", () => {
      delete window.Telegram;

      render(<TmaLayout />);

      expect(screen.getByText("请在 Telegram 客户端中打开")).toBeDefined();
      expect(screen.queryByTestId("tma-outlet")).toBeNull();

      expect(tmaApi.fetchTmaDashboard).not.toHaveBeenCalled();
      expect(tmaApi.fetchTmaEvent).not.toHaveBeenCalled();
    });

    it("initializes TMA SDK and renders Outlet when inside Telegram", () => {
      setupTelegramEnv();

      render(<TmaLayout />);

      expect(window.Telegram?.WebApp?.ready).toHaveBeenCalled();
      expect(window.Telegram?.WebApp?.expand).toHaveBeenCalled();
      expect(screen.getByTestId("tma-outlet")).toBeDefined();
      expect(screen.queryByText("请在 Telegram 客户端中打开")).toBeNull();
    });
  });

  describe("TmaDashboardPage", () => {
    const mockDashboardData: TmaDashboardData = {
      monitored_repos_count: 5,
      pending_work_items_count: 3,
      webhook_deliveries_total: 120,
      webhook_deliveries_success: 118,
      webhook_delivery_rate: 0.983,
      quiet_hours_enabled: false,
      quiet_hours_start: "22:00",
      quiet_hours_end: "08:00",
      quiet_hours_tz: "Asia/Shanghai",
      work_items: [
        {
          id: "item-1",
          repository_id: "repo-1",
          number: 42,
          kind: "pull_request",
          title: "Fix authentication race condition",
          state: "open",
          html_url: "https://github.com/org/repo/pull/42",
          updated_at: "2026-11-08T00:00:00Z",
        },
      ],
    };

    it("renders dashboard KPI cards and quiet hours switch", async () => {
      setupTelegramEnv();
      vi.mocked(tmaApi.fetchTmaDashboard).mockResolvedValue(mockDashboardData);

      render(<TmaDashboardPage />);

      expect(screen.getByText("加载中…")).toBeDefined();

      await waitFor(() => {
        expect(screen.getByText("5")).toBeDefined();
        expect(screen.getByText("3")).toBeDefined();
        expect(screen.getByText("Fix authentication race condition")).toBeDefined();
        expect(screen.getByText("#42")).toBeDefined();
      });

      const quietSwitch = screen.getByRole("switch", { name: /静音/i });
      expect(quietSwitch).toBeDefined();
      expect(quietSwitch.getAttribute("aria-checked")).toBe("false");
    });

    it("toggles quiet hours optimistically with haptics", async () => {
      setupTelegramEnv();
      vi.mocked(tmaApi.fetchTmaDashboard).mockResolvedValue(mockDashboardData);
      vi.mocked(tmaApi.toggleTmaQuietHours).mockResolvedValue({ enabled: true });

      const impactSpy = vi.spyOn(tmaHaptics, "impact");

      render(<TmaDashboardPage />);

      await waitFor(() => {
        expect(screen.getByRole("switch", { name: /静音/i })).toBeDefined();
      });

      const quietSwitch = screen.getByRole("switch", { name: /静音/i });
      fireEvent.click(quietSwitch);

      expect(impactSpy).toHaveBeenCalledWith("light");
      expect(quietSwitch.getAttribute("aria-checked")).toBe("true");

      await waitFor(() => {
        expect(tmaApi.toggleTmaQuietHours).toHaveBeenCalledWith(true, expect.any(AbortSignal));
      });
    });

    it("aborts in-flight request on unmount", () => {
      setupTelegramEnv();
      let capturedSignal: AbortSignal | undefined;
      vi.mocked(tmaApi.fetchTmaDashboard).mockImplementation((signal) => {
        capturedSignal = signal;
        return new Promise(() => {}); // Pending forever
      });

      const { unmount } = render(<TmaDashboardPage />);
      expect(capturedSignal?.aborted).toBe(false);

      unmount();
      expect(capturedSignal?.aborted).toBe(true);
    });
  });

  describe("TmaEventDetailPage", () => {
    const mockDetailData: TmaEventDetailData = {
      event: {
        id: "evt-123",
        kind: "workflow_run",
        action: "completed",
        title: "CI Workflow Failed on main",
        severity: "error",
        actor: "octocat",
        html_url: "https://github.com/org/repo/actions/runs/999",
        occurred_at: "2026-11-08T01:30:00Z",
        workflow_run_id: 999,
      },
      repository: {
        id: "repo-1",
        full_name: "org/repo",
        owner: "org",
        name: "repo",
        is_private: false,
        is_archived: false,
        monitor_enabled: true,
        html_url: "https://github.com/org/repo",
      },
      workflow_run: {
        id: "run-999",
        repository_id: "repo-1",
        run_id: 999,
        workflow_name: "CI",
        head_branch: "main",
        head_sha: "abcd1234efgh5678",
        conclusion: "failure",
        status: "completed",
        html_url: "https://github.com/org/repo/actions/runs/999",
        run_attempt: 1,
      },
    };

    it("wires Telegram BackButton on mount to return to dashboard, and cleans up on unmount", async () => {
      setupTelegramEnv();
      vi.mocked(tmaApi.fetchTmaEvent).mockResolvedValue(mockDetailData);
      vi.mocked(tmaApi.fetchTmaEventLogs).mockResolvedValue({
        logs: "normal logs",
        truncated: false,
      });

      const backBtn = window.Telegram?.WebApp?.BackButton;
      const { unmount } = render(<TmaEventDetailPage />);

      await waitFor(() => {
        expect(backBtn?.onClick).toHaveBeenCalledTimes(1);
        expect(backBtn?.show).toHaveBeenCalledTimes(1);
      });

      unmount();
      expect(backBtn?.offClick).toHaveBeenCalledTimes(1);
      expect(backBtn?.hide).toHaveBeenCalledTimes(1);
    });

    it("renders event detail tabs and action buttons with haptics", async () => {
      setupTelegramEnv();
      vi.mocked(tmaApi.fetchTmaEvent).mockResolvedValue(mockDetailData);
      vi.mocked(tmaApi.fetchTmaEventLogs).mockResolvedValue({
        logs: "Error: Process completed with exit code 1.\nat line 42",
        truncated: true,
        job_id: 101,
        job_name: "test-unit",
      });
      vi.mocked(tmaApi.executeTmaAction).mockResolvedValue({
        status: "executed",
        message: "工作流已重新触发运行",
      });

      const notificationSpy = vi.spyOn(tmaHaptics, "notification");

      render(<TmaEventDetailPage />);

      await waitFor(() => {
        expect(screen.getByText("CI Workflow Failed on main")).toBeDefined();
        expect(screen.getByText("org/repo")).toBeDefined();
      });

      // Verifies CI logs tab content
      await waitFor(() => {
        expect(screen.getByText(/Process completed with exit code 1/)).toBeDefined();
        expect(screen.getByText("[日志已截断至尾部 200 行]")).toBeDefined();
      });

      // Clicks rerun action button
      const rerunBtn = screen.getByRole("button", { name: /重新运行/ });
      expect(rerunBtn).toBeDefined();

      fireEvent.click(rerunBtn);

      await waitFor(() => {
        expect(tmaApi.executeTmaAction).toHaveBeenCalledWith(
          expect.objectContaining({
            event_id: "evt-123",
            action: "workflow_rerun",
          }),
          expect.any(AbortSignal),
        );
        expect(notificationSpy).toHaveBeenCalledWith("success");
        expect(screen.getByText("工作流已重新触发运行")).toBeDefined();
      });
    });

    it("switches to diff tab and fetches diff", async () => {
      setupTelegramEnv();
      vi.mocked(tmaApi.fetchTmaEvent).mockResolvedValue(mockDetailData);
      vi.mocked(tmaApi.fetchTmaEventLogs).mockResolvedValue({
        logs: "normal logs",
        truncated: false,
      });
      vi.mocked(tmaApi.fetchTmaEventDiff).mockResolvedValue({
        diff: "--- a/file.go\n+++ b/file.go\n@@ -1,3 +1,3 @@\n-old\n+new",
      });

      render(<TmaEventDetailPage />);

      await waitFor(() => {
        expect(screen.getByRole("tab", { name: /文件改动|Diff/i })).toBeDefined();
      });

      const diffTab = screen.getByRole("tab", { name: /文件改动|Diff/i });
      fireEvent.click(diffTab);

      await waitFor(() => {
        expect(tmaApi.fetchTmaEventDiff).toHaveBeenCalledWith("evt-123", expect.any(AbortSignal));
        expect(screen.getAllByText(/file\.go/).length).toBeGreaterThan(0);
      });
    });

    it("handles 409 Conflict pending state by showing toast and locking button", async () => {
      setupTelegramEnv();
      vi.mocked(tmaApi.fetchTmaEvent).mockResolvedValue(mockDetailData);
      vi.mocked(tmaApi.fetchTmaEventLogs).mockResolvedValue({
        logs: "error log",
        truncated: false,
      });
      vi.mocked(tmaApi.executeTmaAction).mockRejectedValue(
        new TmaConflictError({
          errorCode: "conflict",
          message: "该动作已在后台处理中，请勿重复操作",
          details: { reason: "action_in_progress", action: "workflow_rerun" },
        }),
      );

      render(<TmaEventDetailPage />);

      await waitFor(() => {
        expect(screen.getByRole("button", { name: /重新运行/ })).toBeDefined();
      });

      const rerunBtn = screen.getByRole("button", { name: /重新运行/ });
      fireEvent.click(rerunBtn);

      await waitFor(() => {
        expect(screen.getByText(/该动作已在后台处理中，请勿重复操作/)).toBeDefined();
        // The rerun button should be disabled / locked to prevent duplicate clicks
        expect(rerunBtn.hasAttribute("disabled")).toBe(true);
      });
    });

    it("submits comment action on PR event", async () => {
      setupTelegramEnv();
      const prDetailData: TmaEventDetailData = {
        ...mockDetailData,
        event: {
          id: "evt-pr-1",
          kind: "pull_request",
          action: "opened",
          title: "PR #50: Add feature",
          severity: "info",
          actor: "octocat",
          html_url: "https://github.com/org/repo/pull/50",
          occurred_at: "2026-11-08T01:30:00Z",
          subject_number: 50,
        },
        work_item: {
          id: "wi-50",
          repository_id: "repo-1",
          number: 50,
          kind: "pull_request",
          title: "PR #50: Add feature",
          state: "open",
          html_url: "https://github.com/org/repo/pull/50",
          updated_at: "2026-11-08T01:30:00Z",
        },
        workflow_run: null,
      };

      vi.mocked(tmaApi.fetchTmaEvent).mockResolvedValue(prDetailData);
      vi.mocked(tmaApi.fetchTmaEventLogs).mockResolvedValue({ logs: "", truncated: false });
      vi.mocked(tmaApi.executeTmaAction).mockResolvedValue({
        status: "executed",
        message: "评论发表成功",
      });

      render(<TmaEventDetailPage />);

      await waitFor(() => {
        expect(screen.getByText("PR #50: Add feature")).toBeDefined();
      });

      // Find comment area or trigger
      const commentInput = screen.getByPlaceholderText(/输入评论内容/i);
      fireEvent.change(commentInput, { target: { value: "LGTM! Approved." } });

      const submitBtn = screen.getByRole("button", { name: /发送|发表|提交评论/i });
      fireEvent.click(submitBtn);

      await waitFor(() => {
        expect(tmaApi.executeTmaAction).toHaveBeenCalledWith(
          expect.objectContaining({
            event_id: "evt-123",
            action: "pr_comment",
            comment_body: "LGTM! Approved.",
          }),
          expect.any(AbortSignal),
        );
      });
    });
  });
});
