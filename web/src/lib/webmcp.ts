/**
 * WebMCP（Web Machine Comprehension Protocol）接入。
 *
 * 当浏览器支持 navigator.modelContext.registerTool（Chrome EPP 早期预览）时，
 * 把 RepoSentinel 的关键只读操作暴露给 AI Agent；不支持的环境静默跳过，
 * 不影响普通用户。注册工具在页面卸载（AbortController signal）时自动注销。
 *
 * 规范：https://webmachinelearning.github.io/webmcp/
 * 注册与注销：https://developer.chrome.com/blog/webmcp-epp
 */

/** WebMCP 工具定义（对齐 registerTool 入参）。 */
export interface WebMCPToolDefinition {
  name: string;
  description: string;
  inputSchema: Record<string, unknown>;
  execute: (args: Record<string, unknown>) => unknown | Promise<unknown>;
}

/** navigator.modelContext 最小可用子集。 */
export interface WebMCPModelContext {
  registerTool?: (tool: WebMCPToolDefinition, options?: { signal?: AbortSignal }) => void | Promise<void>;
}

declare global {
  interface Navigator {
    modelContext?: WebMCPModelContext;
  }
}

/** 同源只读 API 请求（浏览器上下文自动携带 Session Cookie）。
 * 非 2xx 时解析错误体，抛出带 error_code 的错误（Agent 拿到语义化原因而非裸状态码）。 */
async function fetchJSON(path: string, query: Record<string, string | number | undefined> = {}): Promise<unknown> {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "") {
      search.set(key, String(value));
    }
  }
  const url = `${path}${search.size > 0 ? `?${search.toString()}` : ""}`;
  const response = await fetch(url, { headers: { Accept: "application/json" } });
  if (!response.ok) {
    const apiError = await response.json().catch(() => null) as { error_code?: string; message?: string } | null;
    const detail = apiError?.message || (apiError?.error_code ? `错误码 ${apiError.error_code}` : `HTTP ${response.status}`);
    throw new Error(`请求 ${path} 失败：${detail}`);
  }
  return response.json();
}

function intArg(args: Record<string, unknown>, key: string): number | undefined {
  const value = args[key];
  return typeof value === "number" && Number.isFinite(value) ? Math.trunc(value) : undefined;
}

function stringArg(args: Record<string, unknown>, key: string): string | undefined {
  const value = args[key];
  return typeof value === "string" && value !== "" ? value : undefined;
}

/** RepoSentinel 暴露给 Agent 的 WebMCP 工具集。 */
export function webMCPTools(): WebMCPToolDefinition[] {
  return [
    {
      name: "get_dashboard",
      description: "获取 RepoSentinel 概览统计：开放 Issue/PR、失败 Actions、开放安全告警与仓库状态。",
      inputSchema: { type: "object", properties: {} },
      execute: () => fetchJSON("/api/v1/dashboard"),
    },
    {
      name: "list_repositories",
      description: "列出仓库，可按 type=installation|external 过滤，支持分页。",
      inputSchema: {
        type: "object",
        properties: {
          type: { type: "string", enum: ["installation", "external"] },
          page: { type: "integer", minimum: 1 },
          per_page: { type: "integer", minimum: 1, maximum: 100 },
        },
      },
      execute: (args) =>
        fetchJSON("/api/v1/repositories", {
          type: stringArg(args, "type"),
          page: intArg(args, "page"),
          per_page: intArg(args, "per_page"),
        }),
    },
    {
      name: "list_security_alerts",
      description: "列出安全告警，可按 state=open|closed 与 severity 过滤，支持分页。",
      inputSchema: {
        type: "object",
        properties: {
          state: { type: "string", enum: ["open", "closed"] },
          severity: { type: "string" },
          page: { type: "integer", minimum: 1 },
          per_page: { type: "integer", minimum: 1, maximum: 100 },
        },
      },
      execute: (args) =>
        fetchJSON("/api/v1/security-alerts", {
          state: stringArg(args, "state"),
          severity: stringArg(args, "severity"),
          page: intArg(args, "page"),
          per_page: intArg(args, "per_page"),
        }),
    },
  ];
}

/**
 * 在支持 WebMCP 的浏览器中注册全部工具；不支持时直接返回。
 * 返回的 controller 用于页面卸载时注销工具（abort 即注销）。
 */
export function registerWebMCPTools(): AbortController | null {
  const modelContext = navigator.modelContext;
  if (!modelContext || typeof modelContext.registerTool !== "function") {
    return null;
  }
  const controller = new AbortController();
  for (const tool of webMCPTools()) {
    modelContext.registerTool(tool, { signal: controller.signal });
  }
  return controller;
}
