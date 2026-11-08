import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  executeTmaAction,
  fetchTmaDashboard,
  fetchTmaEvent,
  fetchTmaEventDiff,
  fetchTmaEventLogs,
  TmaApiError,
  TmaConflictError,
  toggleTmaQuietHours,
} from "./tma-api";
import {
  getTmaStartParam,
  initTmaSdk,
  isTelegramWebAppAvailable,
  useTmaHaptics,
} from "./tma-sdk";

describe("tma-sdk", () => {
  const originalTelegram = window.Telegram;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    window.Telegram = originalTelegram;
  });

  describe("isTelegramWebAppAvailable", () => {
    it("returns false when Telegram is undefined", () => {
      delete window.Telegram;
      expect(isTelegramWebAppAvailable()).toBe(false);
    });

    it("returns false when WebApp or initData is empty", () => {
      window.Telegram = {
        WebApp: {
          initData: "",
          initDataUnsafe: {},
          themeParams: {},
          isExpanded: false,
          viewportHeight: 600,
          viewportStableHeight: 600,
          MainButton: {} as any,
          BackButton: {} as any,
          HapticFeedback: {} as any,
          ready: vi.fn(),
          expand: vi.fn(),
          close: vi.fn(),
          onEvent: vi.fn(),
          offEvent: vi.fn(),
        },
      };
      expect(isTelegramWebAppAvailable()).toBe(false);
    });

    it("returns true when Telegram WebApp has initData", () => {
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
          MainButton: {} as any,
          BackButton: {} as any,
          HapticFeedback: {} as any,
          ready: vi.fn(),
          expand: vi.fn(),
          close: vi.fn(),
          onEvent: vi.fn(),
          offEvent: vi.fn(),
        },
      };
      expect(isTelegramWebAppAvailable()).toBe(true);
    });
  });

  describe("initTmaSdk", () => {
    it("calls ready and expand on WebApp when available", () => {
      const readyMock = vi.fn();
      const expandMock = vi.fn();
      window.Telegram = {
        WebApp: {
          initData: "test",
          initDataUnsafe: {},
          themeParams: {},
          isExpanded: false,
          viewportHeight: 600,
          viewportStableHeight: 600,
          MainButton: {} as any,
          BackButton: {} as any,
          HapticFeedback: {} as any,
          ready: readyMock,
          expand: expandMock,
          close: vi.fn(),
          onEvent: vi.fn(),
          offEvent: vi.fn(),
        },
      };

      initTmaSdk();
      expect(readyMock).toHaveBeenCalledTimes(1);
      expect(expandMock).toHaveBeenCalledTimes(1);
    });

    it("does not crash when outside Telegram", () => {
      delete window.Telegram;
      expect(() => initTmaSdk()).not.toThrow();
    });
  });

  describe("getTmaStartParam", () => {
    it("returns start_param when available", () => {
      window.Telegram = {
        WebApp: {
          initData: "test",
          initDataUnsafe: {
            start_param: "event_123",
          },
          themeParams: {},
          isExpanded: false,
          viewportHeight: 600,
          viewportStableHeight: 600,
          MainButton: {} as any,
          BackButton: {} as any,
          HapticFeedback: {} as any,
          ready: vi.fn(),
          expand: vi.fn(),
          close: vi.fn(),
          onEvent: vi.fn(),
          offEvent: vi.fn(),
        },
      };
      expect(getTmaStartParam()).toBe("event_123");
    });

    it("returns undefined when start_param is absent or outside Telegram", () => {
      delete window.Telegram;
      expect(getTmaStartParam()).toBeUndefined();
    });
  });

  describe("useTmaHaptics", () => {
    it("triggers impactOccurred, notificationOccurred, and selectionChanged safely", () => {
      const impactOccurred = vi.fn();
      const notificationOccurred = vi.fn();
      const selectionChanged = vi.fn();

      window.Telegram = {
        WebApp: {
          initData: "test",
          initDataUnsafe: {},
          themeParams: {},
          isExpanded: false,
          viewportHeight: 600,
          viewportStableHeight: 600,
          MainButton: {} as any,
          BackButton: {} as any,
          HapticFeedback: {
            impactOccurred,
            notificationOccurred,
            selectionChanged,
          },
          ready: vi.fn(),
          expand: vi.fn(),
          close: vi.fn(),
          onEvent: vi.fn(),
          offEvent: vi.fn(),
        },
      };

      const haptics = useTmaHaptics();
      haptics.impact("medium");
      haptics.notification("success");
      haptics.selection();

      expect(impactOccurred).toHaveBeenCalledWith("medium");
      expect(notificationOccurred).toHaveBeenCalledWith("success");
      expect(selectionChanged).toHaveBeenCalledTimes(1);
    });

    it("does not crash when outside Telegram", () => {
      delete window.Telegram;
      const haptics = useTmaHaptics();
      expect(() => {
        haptics.impact();
        haptics.notification("error");
        haptics.selection();
      }).not.toThrow();
    });
  });
});

describe("tma-api", () => {
  const originalTelegram = window.Telegram;
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
    window.Telegram = {
      WebApp: {
        initData: "auth_token_mock_123",
        initDataUnsafe: {},
        themeParams: {},
        isExpanded: true,
        viewportHeight: 600,
        viewportStableHeight: 600,
        MainButton: {} as any,
        BackButton: {} as any,
        HapticFeedback: {} as any,
        ready: vi.fn(),
        expand: vi.fn(),
        close: vi.fn(),
        onEvent: vi.fn(),
        offEvent: vi.fn(),
      },
    };
  });

  afterEach(() => {
    window.Telegram = originalTelegram;
    globalThis.fetch = originalFetch;
  });

  it("fetchTmaDashboard sends GET to /api/v1/tma/dashboard with X-Telegram-Init-Data header", async () => {
    const mockData = {
      monitored_repos_count: 5,
      pending_work_items_count: 2,
      webhook_deliveries_total: 100,
      webhook_deliveries_success: 98,
      webhook_delivery_rate: 0.98,
      quiet_hours_enabled: false,
      quiet_hours_start: "22:00",
      quiet_hours_end: "08:00",
      quiet_hours_tz: "Asia/Shanghai",
      work_items: [],
    };

    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(mockData), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    globalThis.fetch = fetchMock;

    const controller = new AbortController();
    const result = await fetchTmaDashboard(controller.signal);

    expect(result).toEqual(mockData);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v1/tma/dashboard");
    expect(init.method).toBe("GET");
    expect(init.signal).toBe(controller.signal);
    const headers = new Headers(init.headers);
    expect(headers.get("X-Telegram-Init-Data")).toBe("auth_token_mock_123");
    expect(headers.get("Accept")).toBe("application/json");
  });

  it("fetchTmaEvent sends GET to /api/v1/tma/events/:id", async () => {
    const mockDetail = {
      event: { id: "ev_1", kind: "workflow_run", action: "completed", title: "CI failed", severity: "error", actor: "bot", html_url: "https://github.com", occurred_at: "2026-11-08T00:00:00Z" },
      repository: { id: "repo_1", full_name: "test/repo", owner: "test", name: "repo", is_private: false, is_archived: false, monitor_enabled: true, html_url: "https://github.com/test/repo" },
    };

    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(mockDetail), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    globalThis.fetch = fetchMock;

    const result = await fetchTmaEvent("ev_1");
    expect(result).toEqual(mockDetail);
    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v1/tma/events/ev_1");
  });

  it("fetchTmaEventDiff sends GET to /api/v1/tma/events/:id/diff", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ diff: "@@ -1 +1 @@" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    globalThis.fetch = fetchMock;

    const result = await fetchTmaEventDiff("ev_1");
    expect(result).toEqual({ diff: "@@ -1 +1 @@" });
    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v1/tma/events/ev_1/diff");
  });

  it("fetchTmaEventLogs sends GET to /api/v1/tma/events/:id/logs", async () => {
    const mockLogs = { logs: "error: test failed", truncated: false, job_id: 101, job_name: "test" };
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(mockLogs), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    globalThis.fetch = fetchMock;

    const result = await fetchTmaEventLogs("ev_1");
    expect(result).toEqual(mockLogs);
    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v1/tma/events/ev_1/logs");
  });

  it("executeTmaAction sends POST to /api/v1/tma/actions with JSON body", async () => {
    const mockOutcome = { status: "executed", message: "Action ran successfully", action: "workflow_rerun" };
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(mockOutcome), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    globalThis.fetch = fetchMock;

    const payload = { event_id: "ev_1", action: "workflow_rerun" };
    const result = await executeTmaAction(payload);

    expect(result).toEqual(mockOutcome);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v1/tma/actions");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual(payload);
  });

  it("toggleTmaQuietHours sends POST to /api/v1/tma/quiet-hours", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ enabled: true }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    globalThis.fetch = fetchMock;

    const result = await toggleTmaQuietHours(true);
    expect(result).toEqual({ enabled: true });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v1/tma/quiet-hours");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({ enabled: true });
  });

  describe("error handling", () => {
    it("parses HTTP 409 Conflict with isPendingConflict = true and structured details", async () => {
      const errorBody = {
        error_code: "conflict",
        message: "动作正在执行中，请勿重复操作",
        details: { reason: "action_in_progress", action: "workflow_rerun" },
      };
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(JSON.stringify(errorBody), {
          status: 409,
          headers: { "Content-Type": "application/json" },
        }),
      );
      globalThis.fetch = fetchMock;

      try {
        await executeTmaAction({ event_id: "ev_1", action: "workflow_rerun" });
        expect.unreachable("should have thrown");
      } catch (err: unknown) {
        expect(err).toBeInstanceOf(TmaApiError);
        expect(err).toBeInstanceOf(TmaConflictError);
        const apiErr = err as TmaApiError;
        expect(apiErr.status).toBe(409);
        expect(apiErr.errorCode).toBe("conflict");
        expect(apiErr.message).toBe("动作正在执行中，请勿重复操作");
        expect(apiErr.isPendingConflict).toBe(true);
        expect(apiErr.details).toEqual({ reason: "action_in_progress", action: "workflow_rerun" });
      }
    });

    it("parses general API error response", async () => {
      const errorBody = {
        error_code: "forbidden",
        message: "无权访问此事件",
      };
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(JSON.stringify(errorBody), {
          status: 403,
          headers: { "Content-Type": "application/json" },
        }),
      );
      globalThis.fetch = fetchMock;

      await expect(fetchTmaEvent("ev_forbidden")).rejects.toThrow(TmaApiError);
    });

    it("falls back to raw text if error response is not JSON", async () => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response("Bad Gateway 502", {
          status: 502,
          headers: { "Content-Type": "text/plain" },
        }),
      );
      globalThis.fetch = fetchMock;

      try {
        await fetchTmaDashboard();
        expect.unreachable("should have thrown");
      } catch (err: unknown) {
        const apiErr = err as TmaApiError;
        expect(apiErr.status).toBe(502);
        expect(apiErr.message).toContain("Bad Gateway 502");
      }
    });
  });
});
