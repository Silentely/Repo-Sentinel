import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { registerWebMCPTools, webMCPTools } from "./webmcp";

describe("webmcp 工具集", () => {
  it("暴露关键只读数据工具", () => {
    const tools = webMCPTools();
    const names = tools.map((tool) => tool.name);
    expect(names).toEqual(["get_dashboard", "list_repositories", "list_security_alerts"]);
    for (const tool of tools) {
      expect(tool.description.length).toBeGreaterThan(0);
      expect(tool.inputSchema.type).toBe("object");
      expect(typeof tool.execute).toBe("function");
    }
  });

  it("list_repositories 拼接分页与过滤查询参数", async () => {
    const fetchImpl = vi.fn(async (_input: string, _init?: RequestInit) => {
      return new Response(JSON.stringify({ items: [], page: 1, per_page: 10, total: 0 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchImpl);
    try {
      const tool = webMCPTools().find((item) => item.name === "list_repositories");
      await tool!.execute({ type: "external", per_page: 10 });

      expect(fetchImpl).toHaveBeenCalledTimes(1);
      const url = fetchImpl.mock.calls[0]?.[0];
      expect(url).toBe("/api/v1/repositories?type=external&per_page=10");
    } finally {
      vi.unstubAllGlobals();
    }
  });
});

describe("registerWebMCPTools", () => {
  const originalModelContext = navigator.modelContext;

  afterEach(() => {
    Object.defineProperty(navigator, "modelContext", {
      value: originalModelContext,
      configurable: true,
    });
    vi.restoreAllMocks();
  });

  it("浏览器不支持时静默跳过并返回 null", () => {
    Object.defineProperty(navigator, "modelContext", { value: undefined, configurable: true });
    expect(registerWebMCPTools()).toBeNull();
  });

  it("支持时逐个注册工具并携带 AbortController signal", () => {
    const registerTool = vi.fn();
    Object.defineProperty(navigator, "modelContext", {
      value: { registerTool },
      configurable: true,
    });

    const controller = registerWebMCPTools();

    expect(controller).toBeInstanceOf(AbortController);
    expect(registerTool).toHaveBeenCalledTimes(3);
    for (const call of registerTool.mock.calls) {
      const [tool, options] = call as [unknown, { signal: AbortSignal }];
      expect(tool).toMatchObject({ name: expect.any(String) });
      expect(options.signal).toBe(controller!.signal);
    }
    // 页面卸载触发 abort 即注销工具。
    controller!.abort();
    expect(controller!.signal.aborted).toBe(true);
  });
});
