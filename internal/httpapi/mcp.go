package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

const (
	// mcpDefaultProtocolVersion 是服务器默认支持的 MCP 协议版本。
	mcpDefaultProtocolVersion = "2025-06-18"
	// mcpMaxBodyBytes 限制 MCP 请求体大小。
	mcpMaxBodyBytes = 1 << 20
)

// mcpJSONRPCErrorCode 对齐 JSON-RPC 2.0 标准错误码。
const (
	mcpParseError     = -32700
	mcpInvalidRequest = -32600
	mcpMethodNotFound = -32601
	mcpInvalidParams  = -32602
	mcpInternalError  = -32603
)

// mcpTool 描述 MCP 工具（name/description/inputSchema 与执行函数）。
type mcpTool struct {
	name        string
	description string
	inputSchema map[string]any
	execute     func(context.Context, map[string]any) (any, error)
}

// mcpJSONRPCRequest 是最小 JSON-RPC 2.0 请求结构。
type mcpJSONRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

// mcpTools 返回本站点暴露给 Agent 的只读工具集。
func (s *server) mcpTools() []mcpTool {
	emptySchema := map[string]any{"type": "object", "properties": map[string]any{}}
	return []mcpTool{
		{
			name:        "get_dashboard",
			description: "获取 RepoSentinel 概览统计：开放 Issue/PR、失败 Actions、开放安全告警、24 小时事件数、Outbox 死信、仓库与渠道状态。",
			inputSchema: emptySchema,
			execute: func(ctx context.Context, _ map[string]any) (any, error) {
				return s.dependencies.Store.Dashboard(ctx)
			},
		},
		{
			name:        "list_repositories",
			description: "列出仓库，可按 type=github_installation|external_public 过滤，支持分页。",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"type":     map[string]any{"type": "string", "enum": []string{"github_installation", "external_public"}},
					"page":     map[string]any{"type": "integer", "minimum": 1},
					"per_page": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				},
			},
			execute: func(ctx context.Context, args map[string]any) (any, error) {
				filter := mcpListFilter(args)
				filter.Kind = mcpStringArg(args, "type")
				items, page, err := s.dependencies.Store.Repositories().List(ctx, filter)
				if err != nil {
					return nil, err
				}
				return mcpListResult(items, page), nil
			},
		},
		{
			name:        "list_work_items",
			description: "列出 Issue/PR，可按 kind=issue|pull_request、state=open|closed、repository_id 过滤，支持分页。",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind":          map[string]any{"type": "string", "enum": []string{"issue", "pull_request"}},
					"state":         map[string]any{"type": "string", "enum": []string{"open", "closed"}},
					"repository_id": map[string]any{"type": "string"},
					"page":          map[string]any{"type": "integer", "minimum": 1},
					"per_page":      map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				},
			},
			execute: func(ctx context.Context, args map[string]any) (any, error) {
				filter := mcpListFilter(args)
				filter.Kind = mcpStringArg(args, "kind")
				filter.State = mcpStringArg(args, "state")
				filter.RepositoryID = mcpStringArg(args, "repository_id")
				items, page, err := s.dependencies.Store.WorkItems().List(ctx, filter)
				if err != nil {
					return nil, err
				}
				return mcpListResult(items, page), nil
			},
		},
		{
			name:        "list_workflow_runs",
			description: "列出 GitHub Actions 运行，可按 conclusion（如 failure）、repository_id 过滤，支持分页。",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"conclusion":    map[string]any{"type": "string"},
					"repository_id": map[string]any{"type": "string"},
					"page":          map[string]any{"type": "integer", "minimum": 1},
					"per_page":      map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				},
			},
			execute: func(ctx context.Context, args map[string]any) (any, error) {
				filter := mcpListFilter(args)
				filter.Status = mcpStringArg(args, "conclusion")
				filter.RepositoryID = mcpStringArg(args, "repository_id")
				items, page, err := s.dependencies.Store.WorkflowRuns().List(ctx, filter)
				if err != nil {
					return nil, err
				}
				return mcpListResult(items, page), nil
			},
		},
		{
			name:        "list_security_alerts",
			description: "列出安全告警，可按 state（open/fixed/dismissed/auto_dismissed/withdrawn 等）、severity 过滤，支持分页。",
			inputSchema: map[string]any{
				// state 自由透传（与 REST 一致）：枚举会挡住 withdrawn 等新状态。
				"state":    map[string]any{"type": "string"},
				"severity": map[string]any{"type": "string"},
				"page":     map[string]any{"type": "integer", "minimum": 1},
				"per_page": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
			},
			execute: func(ctx context.Context, args map[string]any) (any, error) {
				filter := mcpListFilter(args)
				filter.State = mcpStringArg(args, "state")
				filter.Status = mcpStringArg(args, "severity")
				items, page, err := s.dependencies.Store.SecurityAlerts().List(ctx, filter)
				if err != nil {
					return nil, err
				}
				return mcpListResult(items, page), nil
			},
		},
		{
			name:        "list_events",
			description: "列出最近事件流，支持按仓库和类型过滤，支持分页。",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repository_id": map[string]any{"type": "string"},
					"kind":          map[string]any{"type": "string"},
					"page":          map[string]any{"type": "integer", "minimum": 1},
					"per_page":      map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				},
			},
			execute: func(ctx context.Context, args map[string]any) (any, error) {
				filter := mcpListFilter(args)
				filter.RepositoryID = mcpStringArg(args, "repository_id")
				filter.Kind = mcpStringArg(args, "kind")
				items, page, err := s.dependencies.Store.Events().List(ctx, filter)
				if err != nil {
					return nil, err
				}
				return mcpListResult(items, page), nil
			},
		},
		{
			name:        "get_star_trend",
			description: "获取 Star 增长趋势数据，支持按天数范围过滤（7/30/90/0，0 表示全部）。",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"days": map[string]any{"type": "integer", "enum": []int{7, 30, 90, 0}},
				},
			},
			execute: func(ctx context.Context, args map[string]any) (any, error) {
				days := mcpIntArg(args, "days")
				return s.dependencies.Store.StarTrend(ctx, days)
			},
		},
		{
			name:        "list_starred_releases",
			description: "列出 Star Release 追踪列表，支持按 state（tracking/inactive/disabled/unavailable）过滤与分页。",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"state":    map[string]any{"type": "string", "enum": []string{"tracking", "inactive", "disabled", "unavailable"}},
					"page":     map[string]any{"type": "integer", "minimum": 1},
					"per_page": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				},
			},
			execute: func(ctx context.Context, args map[string]any) (any, error) {
				filter := mcpListFilter(args)
				filter.State = mcpStringArg(args, "state")
				items, page, err := s.dependencies.Store.StarredTrackers().List(ctx, filter)
				if err != nil {
					return nil, err
				}
				return mcpListResult(items, page), nil
			},
		},
		{
			name:        "list_outbox",
			description: "列出通知发件箱条目及其投递状态，支持分页与状态过滤。",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status":       map[string]any{"type": "string", "enum": []string{"pending", "sending", "sent", "failed", "dead"}},
					"channel_type": map[string]any{"type": "string"},
					"page":         map[string]any{"type": "integer", "minimum": 1},
					"per_page":     map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				},
			},
			execute: func(ctx context.Context, args map[string]any) (any, error) {
				filter := mcpListFilter(args)
				filter.Status = mcpStringArg(args, "status")
				filter.Kind = mcpStringArg(args, "channel_type")
				items, page, err := s.dependencies.Store.Outbox().List(ctx, filter)
				if err != nil {
					return nil, err
				}
				return mcpListResult(items, page), nil
			},
		},
	}
}

func mcpListFilter(args map[string]any) store.ListFilter {
	return store.ListFilter{
		Page:    mcpIntArg(args, "page"),
		PerPage: mcpIntArg(args, "per_page"),
	}
}

func mcpIntArg(args map[string]any, key string) int {
	if value, ok := args[key].(float64); ok {
		return int(value)
	}
	return 0
}

func mcpStringArg(args map[string]any, key string) string {
	if value, ok := args[key].(string); ok {
		return value
	}
	return ""
}

func mcpListResult(items any, page store.PageResult) map[string]any {
	return map[string]any{
		"items":    items,
		"page":     page.Page,
		"per_page": page.PerPage,
		"total":    page.Total,
	}
}

// mcpJSONError 构造 JSON-RPC 错误响应体。
func mcpJSONError(requestID any, code int, message string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"error":   map[string]any{"code": code, "message": message},
	}
}

// mcpJSONResult 构造 JSON-RPC 成功响应体。
func mcpJSONResult(requestID any, result any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"result":  result,
	}
}

// handleMCP 实现 MCP Streamable HTTP（POST 单请求 JSON-RPC，无状态）。
// 认证复用管理 API：Session Cookie 或 OAuth Bearer 均可。
func (s *server) handleMCP(w http.ResponseWriter, r *http.Request) {
	mediaType := strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0])
	if mediaType != "application/json" && mediaType != "text/plain" {
		s.writeAPIError(w, r, http.StatusUnsupportedMediaType, errorCodeValidationFailed, nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, mcpMaxBodyBytes))
	if err != nil {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}
	var request mcpJSONRPCRequest
	if err := json.Unmarshal(body, &request); err != nil {
		// 请求体不可解析：仍是合法 JSON-RPC 响应载体。
		writeMCPJSON(w, mcpJSONError(nil, mcpParseError, "Parse error"))
		return
	}
	if request.JSONRPC != "2.0" || request.Method == "" {
		writeMCPJSON(w, mcpJSONError(request.ID, mcpInvalidRequest, "Invalid Request"))
		return
	}
	// 通知（无 id）不返回响应体。
	if request.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	response := s.dispatchMCP(r, request)
	writeMCPJSON(w, response)
}

// dispatchMCP 分派 MCP 方法并返回 JSON-RPC 响应。
func (s *server) dispatchMCP(r *http.Request, request mcpJSONRPCRequest) map[string]any {
	switch request.Method {
	case "initialize":
		protocolVersion := mcpDefaultProtocolVersion
		if requested, ok := request.Params["protocolVersion"].(string); ok {
			switch requested {
			case "2025-03-26", "2025-06-18":
				protocolVersion = requested
			}
		}
		return mcpJSONResult(request.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    "reposentinel",
				"version": mcpServerVersion(s.dependencies.BuildInfo.Version),
			},
		})
	case "ping":
		return mcpJSONResult(request.ID, map[string]any{})
	case "tools/list":
		tools := make([]any, 0, len(s.mcpTools()))
		for _, tool := range s.mcpTools() {
			tools = append(tools, map[string]any{
				"name":        tool.name,
				"description": tool.description,
				"inputSchema": tool.inputSchema,
			})
		}
		return mcpJSONResult(request.ID, map[string]any{"tools": tools})
	case "tools/call":
		return s.handleMCPToolCall(r.Context(), request)
	default:
		return mcpJSONError(request.ID, mcpMethodNotFound, "Method not found")
	}
}

func (s *server) handleMCPToolCall(ctx context.Context, request mcpJSONRPCRequest) map[string]any {
	name, _ := request.Params["name"].(string)
	arguments, _ := request.Params["arguments"].(map[string]any)
	for _, tool := range s.mcpTools() {
		if tool.name != name {
			continue
		}
		result, err := tool.execute(ctx, arguments)
		if err != nil {
			reqID := requestIDFromContext(ctx)
			if s.dependencies.Logger != nil {
				// 完整错误进日志：DB/网络细节不对 MCP 客户端透出，避免暴露内部结构。
				s.dependencies.Logger.Warn("mcp tool call failed",
					"tool", name, "request_id", reqID, "error_code", "mcp_tool_failed", "error", err.Error())
			}
			return mcpJSONResult(request.ID, map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": "工具执行失败，请稍后重试。"},
				},
				"isError": true,
			})
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return mcpJSONError(request.ID, mcpInternalError, "Internal error")
		}
		return mcpJSONResult(request.ID, map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": string(encoded)},
			},
			"isError": false,
		})
	}
	return mcpJSONError(request.ID, mcpInvalidParams, fmt.Sprintf("Unknown tool: %s", name))
}

// mcpServerVersion 返回对外展示的版本：dev 构建的 BuildInfo.Version 为空时回退 "dev"，
// 与 MCP Server Card 的取值一致。
func mcpServerVersion(version string) string {
	if version == "" {
		return "dev"
	}
	return version
}

// writeMCPJSON 以 application/json 输出 MCP 响应并携带协议版本头：
// initialize 协商出的版本随响应回写，其余方法回默认版本。
func writeMCPJSON(w http.ResponseWriter, body map[string]any) {
	version := mcpDefaultProtocolVersion
	if result, ok := body["result"].(map[string]any); ok {
		if v, ok := result["protocolVersion"].(string); ok && v != "" {
			version = v
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Mcp-Protocol-Version", version)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}
