package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"path"
	"strings"
)

// agentDiscoveryLinkHeader 是全部 GET/HEAD 响应携带的 Agent 发现 Link 头（RFC 8288）。
// 使用相对引用（相对请求 URI 解析），无需配置即可工作。
const agentDiscoveryLinkHeader = "</.well-known/api-catalog>; rel=\"api-catalog\"; type=\"application/linkset+json\", " +
	"</openapi.json>; rel=\"service-desc\"; type=\"application/openapi+json\", " +
	"</auth.md>; rel=\"service-doc\"; type=\"text/markdown\", " +
	"</sitemap.xml>; rel=\"describedby\"; type=\"application/xml\""

// spaCanonicalPaths 是管理台 SPA 的规范路由，须与 web/src/app/router.tsx 保持一致。
var spaCanonicalPaths = []string{
	"/",
	"/login",
	"/setup",
	"/notifications",
	"/notifications/outbox",
	"/issues",
	"/pull-requests",
	"/repos",
	"/actions",
	"/security",
	"/github",
	"/about",
	"/settings",
	"/starred-releases",
}

// siteOrigin 返回站点的规范外部 Origin（无尾斜杠）。
// 优先使用配置的 PublicBaseURL；未配置时从请求推导（反代场景尊重 X-Forwarded-Proto）。
func (s *server) siteOrigin(r *http.Request) string {
	base := strings.TrimRight(strings.TrimSpace(s.dependencies.Config.HTTP.PublicBaseURL), "/")
	if base != "" {
		return base
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		if first := strings.TrimSpace(strings.Split(forwarded, ",")[0]); strings.EqualFold(first, "https") {
			scheme = "https"
		}
	}
	return scheme + "://" + r.Host
}

// siteAbsoluteURL 拼接 Origin 与站点内路径。
func (s *server) siteAbsoluteURL(r *http.Request, sitePath string) string {
	return s.siteOrigin(r) + sitePath
}

// escapeXMLText 转义文本中的 XML 特殊字符。
func escapeXMLText(text string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(text))
	return b.String()
}

// handleSitemapXML 输出按规范动态生成的 sitemap.xml（始终与当前路由/Origin 一致）。
func (s *server) handleSitemapXML(w http.ResponseWriter, r *http.Request) {
	origin := s.siteOrigin(r)
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	for _, sitePath := range spaCanonicalPaths {
		b.WriteString("<url><loc>")
		b.WriteString(escapeXMLText(origin + sitePath))
		b.WriteString("</loc></url>")
	}
	b.WriteString("</urlset>")

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

// handleRobotsTXT 输出 robots.txt：站点访问规则 + Sitemap + Content-Signals。
func (s *server) handleRobotsTXT(w http.ResponseWriter, r *http.Request) {
	origin := s.siteOrigin(r)
	body := fmt.Sprintf(`User-agent: *
Allow: /
Disallow: /api/
Disallow: /webhooks/
Disallow: /mcp
# Content Signals（https://contentsignals.org/）
Content-Signal: ai-train=no, search=yes, ai-input=no

Sitemap: %s/sitemap.xml
`, origin)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// agentLinkHeadersMiddleware 为全部 GET/HEAD 响应附加 Agent 发现 Link 头。
func agentLinkHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			w.Header().Add("Link", agentDiscoveryLinkHeader)
		}
		next.ServeHTTP(w, r)
	})
}

// handleAuthMD 输出面向 Agent 的认证注册说明（WorkOS auth.md 格式）。
func (s *server) handleAuthMD(w http.ResponseWriter, r *http.Request) {
	body := s.authMDDocument(r)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func (s *server) authMDDocument(r *http.Request) string {
	origin := s.siteOrigin(r)
	return fmt.Sprintf(`# RepoSentinel auth.md

本文件面向希望以编程方式（AI Agent / 脚本 / CI）访问 RepoSentinel 管理 API 的调用方。

## 面向的调用方

- AI Agent：需要查询仓库状态、Issue/PR、Actions 运行、安全告警或通知状态。
- 脚本与自动化：需要只读访问管理 API。

## 资源标识

- 管理 API 基址：%[1]s/api/v1
- OpenAPI 描述：%[1]s/openapi.json
- 状态端点：%[1]s/health/ready
- MCP 网关：%[1]s/mcp
- API 目录：%[1]s/.well-known/api-catalog

## 认证：OAuth 2.0 客户端凭据（推荐）

1. 部署时配置 Agent 客户端凭据：
   - 环境变量 REPOSENTINEL_OAUTH_CLIENT_ID（默认 reposentinel-agent）
   - 环境变量 REPOSENTINEL_OAUTH_CLIENT_SECRET（必填，不写入配置文件）
2. 请求访问令牌（有效期 1 小时，可随时重新签发）：

   POST %[1]s/oauth/token
   Content-Type: application/x-www-form-urlencoded

   grant_type=client_credentials&client_id=reposentinel-agent&client_secret=YOUR_SECRET

   也支持 HTTP Basic Auth（用户名=client_id，密码=client_secret）。
3. 携带令牌访问 API：

   Authorization: Bearer <access_token>

## 浏览器会话（人类用户）

管理台仍使用 Session Cookie 登录（POST /api/v1/auth/login），与 Agent 令牌互不影响。

## 发现元数据

- 授权服务器：%[1]s/.well-known/oauth-authorization-server
- 受保护资源：%[1]s/.well-known/oauth-protected-resource
- 签名公钥：%[1]s/oauth/jwks

## 作用域

当前唯一作用域 read：全部 Agent 工具与查询端点均为只读。

## 项目与源码

- GitHub: https://github.com/Silentely/Repo-Sentinel
`, origin)
}

// handleWellKnownAPICatalog 输出 RFC 9727 API 目录（application/linkset+json）。
func (s *server) handleWellKnownAPICatalog(w http.ResponseWriter, r *http.Request) {
	origin := s.siteOrigin(r)
	body, err := json.Marshal(map[string]any{
		"linkset": []any{
			map[string]any{
				"anchor": origin + "/api/v1",
				"service-desc": []any{
					map[string]any{"href": origin + "/openapi.json", "type": "application/openapi+json"},
				},
				"service-doc": []any{
					map[string]any{"href": origin + "/auth.md", "type": "text/markdown"},
				},
				"status": []any{
					map[string]any{"href": origin + "/health/ready", "type": "application/json"},
				},
			},
		},
	})
	if err != nil {
		s.writeAPIError(w, r, http.StatusInternalServerError, errorCodeInternal, nil)
		return
	}
	w.Header().Set("Content-Type", "application/linkset+json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleWellKnownOAuthAuthorizationServer 输出 RFC 8414 授权服务器元数据。
func (s *server) handleWellKnownOAuthAuthorizationServer(w http.ResponseWriter, r *http.Request) {
	origin := s.siteOrigin(r)
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                origin,
		"authorization_endpoint":                origin + "/oauth/authorize",
		"token_endpoint":                        origin + "/oauth/token",
		"jwks_uri":                              origin + "/oauth/jwks",
		"response_types_supported":              []string{},
		"grant_types_supported":                 []string{"client_credentials"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "client_secret_basic"},
		"scopes_supported":                      []string{"read"},
		"service_documentation":                 origin + "/auth.md",
	})
}

// handleWellKnownOAuthProtectedResource 输出 RFC 9728 受保护资源元数据。
func (s *server) handleWellKnownOAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
	origin := s.siteOrigin(r)
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 origin + "/api/v1",
		"authorization_servers":    []string{origin},
		"scopes_supported":         []string{"read"},
		"bearer_methods_supported": []string{"header"},
	})
}

// reposentinelAgentSkillMD 是发布给 Agent 的 RepoSentinel API 技能说明（SKILL.md）。
// 与 agent-skills 索引中的 digest 同源，两者必须一致。
func (s *server) reposentinelAgentSkillMD(r *http.Request) string {
	origin := s.siteOrigin(r)
	return fmt.Sprintf(`---
name: reposentinel-api
description: 查询 RepoSentinel 仓库值守平台的仓库、Issue/PR、Actions 运行、安全告警与通知状态。
---

# RepoSentinel API

RepoSentinel 是自托管的 GitHub 仓库值守平台。本技能说明如何通过管理 API 只读查询值守数据。

## 认证

1. 获取令牌：POST %[1]s/oauth/token，表单 grant_type=client_credentials，携带部署时配置的 client_id/client_secret。
2. 调用 API 时携带 Authorization: Bearer <access_token>。

## 查询端点（均只读）

- GET /api/v1/dashboard — 概览统计（开放 Issue/PR、失败 Actions、开放安全告警等）。
- GET /api/v1/repositories?type=github_installation|external_public — 仓库列表（github_installation=自有安装仓，external_public=外部公开仓）。
- GET /api/v1/work-items?kind=issue|pull_request&state=open|closed&repository_id=... — Issue/PR 列表。
- GET /api/v1/workflow-runs?conclusion=failure&repository_id=... — Actions 运行列表。
- GET /api/v1/security-alerts?state=open&severity=... — 安全告警列表（state 可取 open/fixed/dismissed/auto_dismissed/withdrawn 等）。
- GET /api/v1/events — 最近事件流。
- GET /api/v1/notifications/outbox — 通知发件箱（投递状态）。
- GET /api/v1/stats/star-trend?days=7|30|90|0 — Star 增长趋势（0 为全部）。
- GET /api/v1/starred-releases/trackers?state=tracking|inactive|disabled|unavailable — Star Release 追踪列表。
- GET /api/v1/system/version — 版本与 GitHub 配置状态（需令牌）。
- GET /api/v1/system/build-info — 极简构建信息（仅版本号，公开无需令牌）。

## 分页

列表端点支持 page 与 per_page 查询参数，响应包含 items / page / per_page / total 四字段。

完整接口定义见 %[1]s/openapi.json。
`, origin)
}

// handleWellKnownAgentSkillsIndex 输出 Agent Skills Discovery 索引（v0.2.0）。
func (s *server) handleWellKnownAgentSkillsIndex(w http.ResponseWriter, r *http.Request) {
	artifact := []byte(s.reposentinelAgentSkillMD(r))
	digest := sha256.Sum256(artifact)
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"$schema": "https://schemas.agentskills.io/discovery/0.2.0/schema.json",
		"skills": []any{
			map[string]any{
				"name":        "reposentinel-api",
				"type":        "skill-md",
				"description": "查询 RepoSentinel 仓库值守平台的仓库、Issue/PR、Actions 运行、安全告警与通知状态。",
				"url":         s.siteAbsoluteURL(r, "/.well-known/agent-skills/reposentinel-api/SKILL.md"),
				"digest":      "sha256:" + hex.EncodeToString(digest[:]),
			},
		},
	})
}

// handleAgentSkillsArtifact 输出技能索引引用的 SKILL.md 工件。
func (s *server) handleAgentSkillsArtifact(w http.ResponseWriter, r *http.Request) {
	body := s.reposentinelAgentSkillMD(r)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// handleWellKnownMCPCard 输出 MCP Server Card（SEP-1649 / experimental-ext-server-card）。
func (s *server) handleWellKnownMCPCard(w http.ResponseWriter, r *http.Request) {
	origin := s.siteOrigin(r)
	version := s.dependencies.BuildInfo.Version
	if version == "" {
		version = "dev"
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"$schema": "https://static.modelcontextprotocol.io/schemas/v1/server-card.schema.json",
		"name":    "io.reposentinel/admin",
		"version": version,
		"title":   "RepoSentinel",
		"description": "RepoSentinel 管理 API 的 MCP 网关：提供仓库、工作项、Actions 运行、" +
			"安全告警与通知状态的只读查询工具。",
		"websiteUrl": origin,
		"repository": map[string]any{
			"url":    "https://github.com/Silentely/Repo-Sentinel",
			"source": "github",
		},
		"remotes": []any{
			map[string]any{
				"type":                      "streamable-http",
				"url":                       origin + "/mcp",
				"supportedProtocolVersions": []string{"2025-03-26", "2025-06-18"},
			},
		},
		// 兼容早期检查器期望的简化字段。
		"serverInfo": map[string]any{
			"name":    "reposentinel",
			"version": version,
		},
		"endpoint":     origin + "/mcp",
		"capabilities": map[string]any{"tools": map[string]any{}},
	})
}

// ---------- Markdown for Agents（Accept: text/markdown 内容协商） ----------

// acceptsMarkdown 判定请求是否显式要求 text/markdown。
func acceptsMarkdown(r *http.Request) bool {
	for _, headerValue := range r.Header.Values("Accept") {
		for _, part := range strings.Split(headerValue, ",") {
			mediaType := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
			if strings.EqualFold(mediaType, "text/markdown") {
				return true
			}
		}
	}
	return false
}

// isMarkdownNegotiablePath 判定路径是否适合返回 markdown 站点说明：
// 无扩展名的 SPA 路由视为页面；带扩展名的资源（JS/CSS/图片等）不参与协商。
func isMarkdownNegotiablePath(requestPath string) bool {
	extension := path.Ext(requestPath)
	return extension == "" || requestPath == "/"
}

// markdownNegotiationMiddleware 包装 SPA 兜底：Accept: text/markdown 时返回站点 markdown 说明。
func (s *server) markdownNegotiationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			acceptsMarkdown(r) && isMarkdownNegotiablePath(r.URL.Path) {
			s.writeSiteMarkdown(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// siteMarkdownDocument 返回站点的 markdown 说明（面向 Agent 的首页入口）。
func (s *server) siteMarkdownDocument(r *http.Request) string {
	origin := s.siteOrigin(r)
	return fmt.Sprintf(`# RepoSentinel

自托管的 GitHub 仓库值守平台：通过 Webhook 实时接收 Issue / PR / Actions / 安全告警，
经规则引擎推送 Telegram 或 HTTP Webhook，可选智能简报与安全告警分诊。

## 使用入口

- 管理台（登录后）：%[1]s/
- 登录：%[1]s/login
- 首次设置：%[1]s/setup

## 面向 Agent 的资源

- 认证与注册说明：%[1]s/auth.md
- OpenAPI 接口描述：%[1]s/openapi.json
- API 目录（RFC 9727）：%[1]s/.well-known/api-catalog
- 授权服务器：%[1]s/.well-known/oauth-authorization-server
- 受保护资源：%[1]s/.well-known/oauth-protected-resource
- MCP 网关（Streamable HTTP）：%[1]s/mcp
- MCP Server Card：%[1]s/.well-known/mcp/server-card.json
- 技能索引：%[1]s/.well-known/agent-skills/index.json
- 站点地图：%[1]s/sitemap.xml
- 健康检查：%[1]s/health/ready

## 数据模型（简）

管理台围绕仓库（Repository）组织：Issue/PR（WorkItem）、Actions 运行（WorkflowRun）、
安全告警（SecurityAlert）与事件流（Event），通知经 Outbox 投递并可查看状态。
`, origin)
}

// markdownTokenCount 估算 markdown 文本的 token 数（按空白分词）。
func markdownTokenCount(markdown string) int {
	return len(strings.Fields(markdown))
}

// writeSiteMarkdown 以 text/markdown 输出站点说明并附带 token 计数头。
func (s *server) writeSiteMarkdown(w http.ResponseWriter, r *http.Request) {
	body := s.siteMarkdownDocument(r)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Markdown-Tokens", fmt.Sprintf("%d", markdownTokenCount(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// openAPISpec 构建 OpenAPI 3.1 描述（随 Origin 动态生成）。
func (s *server) openAPISpec(r *http.Request) map[string]any {
	origin := s.siteOrigin(r)
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	jsonResponse := func(description string, schema map[string]any) map[string]any {
		return map[string]any{
			"description": description,
			"content": map[string]any{
				"application/json": map[string]any{"schema": schema},
			},
		}
	}
	listResponse := func(itemSchema map[string]any) map[string]any {
		return jsonResponse("分页列表", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items":    map[string]any{"type": "array", "items": itemSchema},
				"page":     map[string]any{"type": "integer"},
				"per_page": map[string]any{"type": "integer"},
				"total":    map[string]any{"type": "integer"},
			},
		})
	}
	errorResponse := func() map[string]any {
		return jsonResponse("错误响应", ref("Error"))
	}
	authed := map[string]any{"bearerAuth": []string{}}

	paths := map[string]any{
		"/health/live": map[string]any{
			"get": map[string]any{
				"summary":     "存活检查",
				"operationId": "healthLive",
				"responses": map[string]any{
					"200": jsonResponse("存活", map[string]any{
						"type":       "object",
						"properties": map[string]any{"status": map[string]any{"type": "string", "enum": []string{"ok"}}},
					}),
				},
			},
		},
		"/health/ready": map[string]any{
			"get": map[string]any{
				"summary":     "就绪检查",
				"operationId": "healthReady",
				"responses": map[string]any{
					"200": jsonResponse("就绪", map[string]any{
						"type":       "object",
						"properties": map[string]any{"status": map[string]any{"type": "string", "enum": []string{"ready"}}},
					}),
					"503": jsonResponse("未就绪", map[string]any{
						"type":       "object",
						"properties": map[string]any{"status": map[string]any{"type": "string", "enum": []string{"not_ready"}}},
					}),
				},
			},
		},
		"/metrics": map[string]any{
			"get": map[string]any{
				"summary":     "Prometheus 指标（可选 Bearer Token）",
				"operationId": "metrics",
				"responses": map[string]any{
					"200": map[string]any{
						"description": "Prometheus 文本格式指标",
						"content": map[string]any{
							"text/plain": map[string]any{"schema": map[string]any{"type": "string"}},
						},
					},
				},
			},
		},
		"/oauth/token": map[string]any{
			"post": map[string]any{
				"summary":     "OAuth 2.0 client_credentials 令牌签发",
				"operationId": "oauthToken",
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{
						"application/x-www-form-urlencoded": map[string]any{
							"schema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"grant_type":    map[string]any{"type": "string", "enum": []string{"client_credentials"}},
									"client_id":     map[string]any{"type": "string"},
									"client_secret": map[string]any{"type": "string", "format": "password"},
								},
								"required": []string{"grant_type", "client_id", "client_secret"},
							},
						},
					},
				},
				"responses": map[string]any{
					"200": jsonResponse("令牌", map[string]any{
						"type": "object",
						"properties": map[string]any{
							"access_token": map[string]any{"type": "string"},
							"token_type":   map[string]any{"type": "string", "enum": []string{"Bearer"}},
							"expires_in":   map[string]any{"type": "integer"},
							"scope":        map[string]any{"type": "string"},
						},
					}),
					"400": jsonResponse("grant_type 不支持或请求非法", map[string]any{"type": "object"}),
					"401": jsonResponse("客户端凭据无效", map[string]any{"type": "object"}),
				},
			},
		},
		"/oauth/jwks": map[string]any{
			"get": map[string]any{
				"summary":     "OAuth 签名公钥（JWKS）",
				"operationId": "oauthJwks",
				"responses": map[string]any{
					"200": jsonResponse("JWKS", map[string]any{
						"type":       "object",
						"properties": map[string]any{"keys": map[string]any{"type": "array"}},
					}),
				},
			},
		},
		"/api/v1/setup/status": map[string]any{
			"get": map[string]any{
				"summary":     "首次设置状态",
				"operationId": "setupStatus",
				"responses": map[string]any{
					"200": jsonResponse("状态", ref("SetupStatus")),
				},
			},
		},
		"/api/v1/setup": map[string]any{
			"post": map[string]any{
				"summary":     "创建初始管理员",
				"operationId": "setup",
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{"schema": ref("SetupRequest")},
					},
				},
				"responses": map[string]any{
					"200": jsonResponse("创建成功", ref("AuthResponse")),
					"409": errorResponse(),
				},
			},
		},
		"/api/v1/auth/login": map[string]any{
			"post": map[string]any{
				"summary":     "管理员登录",
				"operationId": "authLogin",
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{"schema": ref("LoginRequest")},
					},
				},
				"responses": map[string]any{
					"200": jsonResponse("登录成功", ref("AuthResponse")),
					"401": errorResponse(),
					"429": errorResponse(),
				},
			},
		},
		"/api/v1/auth/session": map[string]any{
			"get": map[string]any{
				"summary":     "当前会话",
				"operationId": "authSession",
				"security":    []any{authed},
				"responses": map[string]any{
					"200": jsonResponse("会话信息", ref("AuthResponse")),
					"401": errorResponse(),
				},
			},
		},
		"/api/v1/system/version": map[string]any{
			"get": map[string]any{
				"summary":     "版本信息",
				"operationId": "systemVersion",
				"security":    []any{authed},
				"responses":   map[string]any{"200": jsonResponse("版本信息", map[string]any{"type": "object"})},
			},
		},
		"/api/v1/system/build-info": map[string]any{
			"get": map[string]any{
				"summary":     "公开极简构建信息（仅版本号）",
				"operationId": "systemBuildInfo",
				// 公开端点：登录页页脚展示真实构建版本，不要求认证。
				"responses": map[string]any{"200": jsonResponse("构建信息", map[string]any{
					"type":       "object",
					"properties": map[string]any{"version": map[string]any{"type": "string"}},
				})},
			},
		},
		"/api/v1/dashboard": map[string]any{
			"get": map[string]any{
				"summary":     "概览统计",
				"operationId": "dashboard",
				"security":    []any{authed},
				"responses":   map[string]any{"200": jsonResponse("统计", ref("DashboardStats"))},
			},
		},
		"/api/v1/stats/star-trend": map[string]any{
			"get": map[string]any{
				"summary":     "Star 增长趋势",
				"operationId": "starTrend",
				"security":    []any{authed},
				"parameters": []any{
					map[string]any{"name": "days", "in": "query", "schema": map[string]any{"type": "integer", "enum": []int{7, 30, 90, 0}}},
				},
				"responses": map[string]any{"200": jsonResponse("按日趋势", map[string]any{"type": "object"})},
			},
		},
		"/api/v1/repositories": map[string]any{
			"get": map[string]any{
				"summary":     "仓库列表",
				"operationId": "listRepositories",
				"security":    []any{authed},
				"parameters": []any{
					map[string]any{"name": "type", "in": "query", "schema": map[string]any{"type": "string"}},
					map[string]any{"name": "page", "in": "query", "schema": map[string]any{"type": "integer"}},
					map[string]any{"name": "per_page", "in": "query", "schema": map[string]any{"type": "integer"}},
				},
				"responses": map[string]any{"200": listResponse(ref("Repository"))},
			},
		},
		"/api/v1/repositories/external": map[string]any{
			"post": map[string]any{
				"summary":     "添加外部公开仓库",
				"operationId": "addExternalRepository",
				"security":    []any{authed},
				"responses":   map[string]any{"201": jsonResponse("已添加", ref("Repository")), "409": errorResponse()},
			},
		},
		"/api/v1/work-items": map[string]any{
			"get": map[string]any{
				"summary":     "Issue/PR 列表",
				"operationId": "listWorkItems",
				"security":    []any{authed},
				"parameters": []any{
					map[string]any{"name": "kind", "in": "query", "schema": map[string]any{"type": "string", "enum": []string{"issue", "pull_request"}}},
					map[string]any{"name": "state", "in": "query", "schema": map[string]any{"type": "string"}},
					map[string]any{"name": "repository_id", "in": "query", "schema": map[string]any{"type": "string"}},
					map[string]any{"name": "page", "in": "query", "schema": map[string]any{"type": "integer"}},
					map[string]any{"name": "per_page", "in": "query", "schema": map[string]any{"type": "integer"}},
				},
				"responses": map[string]any{"200": listResponse(ref("WorkItem"))},
			},
		},
		"/api/v1/workflow-runs": map[string]any{
			"get": map[string]any{
				"summary":     "Actions 运行列表",
				"operationId": "listWorkflowRuns",
				"security":    []any{authed},
				"parameters": []any{
					map[string]any{"name": "conclusion", "in": "query", "schema": map[string]any{"type": "string"}},
					map[string]any{"name": "repository_id", "in": "query", "schema": map[string]any{"type": "string"}},
					map[string]any{"name": "page", "in": "query", "schema": map[string]any{"type": "integer"}},
					map[string]any{"name": "per_page", "in": "query", "schema": map[string]any{"type": "integer"}},
				},
				"responses": map[string]any{"200": listResponse(ref("WorkflowRun"))},
			},
		},
		"/api/v1/security-alerts": map[string]any{
			"get": map[string]any{
				"summary":     "安全告警列表",
				"operationId": "listSecurityAlerts",
				"security":    []any{authed},
				"parameters": []any{
					map[string]any{"name": "state", "in": "query", "schema": map[string]any{"type": "string"}},
					map[string]any{"name": "severity", "in": "query", "schema": map[string]any{"type": "string"}},
					map[string]any{"name": "page", "in": "query", "schema": map[string]any{"type": "integer"}},
					map[string]any{"name": "per_page", "in": "query", "schema": map[string]any{"type": "integer"}},
				},
				"responses": map[string]any{"200": listResponse(ref("SecurityAlert"))},
			},
		},
		"/api/v1/events": map[string]any{
			"get": map[string]any{
				"summary":     "事件流",
				"operationId": "listEvents",
				"security":    []any{authed},
				"parameters": []any{
					map[string]any{"name": "page", "in": "query", "schema": map[string]any{"type": "integer"}},
					map[string]any{"name": "per_page", "in": "query", "schema": map[string]any{"type": "integer"}},
				},
				"responses": map[string]any{"200": listResponse(ref("Event"))},
			},
		},
		"/api/v1/notifications/outbox": map[string]any{
			"get": map[string]any{
				"summary":     "通知发件箱",
				"operationId": "listOutbox",
				"security":    []any{authed},
				"parameters": []any{
					map[string]any{"name": "page", "in": "query", "schema": map[string]any{"type": "integer"}},
					map[string]any{"name": "per_page", "in": "query", "schema": map[string]any{"type": "integer"}},
				},
				"responses": map[string]any{"200": listResponse(map[string]any{"type": "object"})},
			},
		},
		"/api/v1/notifications/channels": map[string]any{
			"get": map[string]any{
				"summary":     "通知渠道",
				"operationId": "listChannels",
				"security":    []any{authed},
				"responses":   map[string]any{"200": jsonResponse("渠道列表", map[string]any{"type": "array", "items": map[string]any{"type": "object"}})},
			},
		},
		"/api/v1/github/installations": map[string]any{
			"get": map[string]any{
				"summary":     "GitHub App 安装列表",
				"operationId": "listInstallations",
				"security":    []any{authed},
				"responses":   map[string]any{"200": jsonResponse("安装列表", map[string]any{"type": "array", "items": map[string]any{"type": "object"}})},
			},
		},
		"/api/v1/github/config": map[string]any{
			"get": map[string]any{
				"summary":     "GitHub 配置（掩码）",
				"operationId": "getGitHubConfig",
				"security":    []any{authed},
				"responses":   map[string]any{"200": jsonResponse("配置", map[string]any{"type": "object"})},
			},
		},
		"/api/v1/ai/config": map[string]any{
			"get": map[string]any{
				"summary":     "AI 配置（掩码）",
				"operationId": "getAIConfig",
				"security":    []any{authed},
				"responses":   map[string]any{"200": jsonResponse("配置", map[string]any{"type": "object"})},
			},
		},
		"/api/v1/starred-releases/config": map[string]any{
			"get": map[string]any{
				"summary":     "Star Release 追踪配置",
				"operationId": "getStarredReleasesConfig",
				"security":    []any{authed},
				"responses":   map[string]any{"200": jsonResponse("配置", map[string]any{"type": "object"})},
			},
			"put": map[string]any{
				"summary":     "保存 Star Release 追踪配置",
				"operationId": "putStarredReleasesConfig",
				"security":    []any{authed},
				"responses":   map[string]any{"200": jsonResponse("配置", map[string]any{"type": "object"})},
			},
		},
		"/api/v1/starred-releases/trackers": map[string]any{
			"get": map[string]any{
				"summary":     "Star Release 追踪列表",
				"operationId": "listStarredTrackers",
				"security":    []any{authed},
				"parameters": []any{
					map[string]any{"name": "page", "in": "query", "schema": map[string]any{"type": "integer"}},
					map[string]any{"name": "per_page", "in": "query", "schema": map[string]any{"type": "integer"}},
				},
				"responses": map[string]any{"200": listResponse(map[string]any{"type": "object"})},
			},
		},
		"/api/v1/system/settings": map[string]any{
			"get": map[string]any{
				"summary":     "系统设置",
				"operationId": "getSystemSettings",
				"security":    []any{authed},
				"responses":   map[string]any{"200": jsonResponse("设置", map[string]any{"type": "object"})},
			},
		},
	}

	stringProps := func(props map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": props}
	}
	schemas := map[string]any{
		"Error": stringProps(map[string]any{
			"error_code": map[string]any{"type": "string"},
			"message":    map[string]any{"type": "string"},
		}),
		"SetupStatus": stringProps(map[string]any{
			"required": map[string]any{"type": "boolean"},
		}),
		"SetupRequest": stringProps(map[string]any{
			"username": map[string]any{"type": "string"},
			"password": map[string]any{"type": "string", "format": "password"},
		}),
		"LoginRequest": stringProps(map[string]any{
			"username": map[string]any{"type": "string"},
			"password": map[string]any{"type": "string", "format": "password"},
		}),
		"AuthResponse": stringProps(map[string]any{
			"admin":   map[string]any{"type": "object"},
			"session": map[string]any{"type": "object"},
		}),
		"DashboardStats": stringProps(map[string]any{
			"open_issues":      map[string]any{"type": "integer"},
			"open_pulls":       map[string]any{"type": "integer"},
			"failed_actions":   map[string]any{"type": "integer"},
			"open_security":    map[string]any{"type": "integer"},
			"events_24h":       map[string]any{"type": "integer"},
			"outbox_dead":      map[string]any{"type": "integer"},
			"repos_active":     map[string]any{"type": "integer"},
			"repos_baseline":   map[string]any{"type": "integer"},
			"channels_enabled": map[string]any{"type": "integer"},
		}),
		"Repository": stringProps(map[string]any{
			"id":              map[string]any{"type": "string"},
			"type":            map[string]any{"type": "string"},
			"sync_status":     map[string]any{"type": "string"},
			"full_name":       map[string]any{"type": "string"},
			"is_archived":     map[string]any{"type": "boolean"},
			"is_private":      map[string]any{"type": "boolean"},
			"monitor_enabled": map[string]any{"type": "boolean"},
			"html_url":        map[string]any{"type": "string"},
			"default_branch":  map[string]any{"type": "string"},
		}),
		"WorkItem": stringProps(map[string]any{
			"id":                   map[string]any{"type": "string"},
			"repository_full_name": map[string]any{"type": "string"},
			"number":               map[string]any{"type": "integer"},
			"kind":                 map[string]any{"type": "string"},
			"state":                map[string]any{"type": "string"},
			"title":                map[string]any{"type": "string"},
			"author":               map[string]any{"type": "string"},
			"draft":                map[string]any{"type": "boolean"},
			"merged":               map[string]any{"type": "boolean"},
			"html_url":             map[string]any{"type": "string"},
			"ignored":              map[string]any{"type": "boolean"},
		}),
		"WorkflowRun": stringProps(map[string]any{
			"id":                   map[string]any{"type": "string"},
			"repository_full_name": map[string]any{"type": "string"},
			"workflow_name":        map[string]any{"type": "string"},
			"run_number":           map[string]any{"type": "integer"},
			"event":                map[string]any{"type": "string"},
			"head_branch":          map[string]any{"type": "string"},
			"head_sha":             map[string]any{"type": "string"},
			"status":               map[string]any{"type": "string"},
			"conclusion":           map[string]any{"type": "string"},
			"actor":                map[string]any{"type": "string"},
			"html_url":             map[string]any{"type": "string"},
			"ignored":              map[string]any{"type": "boolean"},
		}),
		"SecurityAlert": stringProps(map[string]any{
			"id":                   map[string]any{"type": "string"},
			"repository_full_name": map[string]any{"type": "string"},
			"alert_kind":           map[string]any{"type": "string"},
			"alert_number":         map[string]any{"type": "integer"},
			"state":                map[string]any{"type": "string"},
			"severity":             map[string]any{"type": "string"},
			"rule_or_dependency":   map[string]any{"type": "string"},
			"html_url":             map[string]any{"type": "string"},
			"ignored":              map[string]any{"type": "boolean"},
		}),
		"Event": stringProps(map[string]any{
			"id":          map[string]any{"type": "string"},
			"source":      map[string]any{"type": "string"},
			"kind":        map[string]any{"type": "string"},
			"action":      map[string]any{"type": "string"},
			"title":       map[string]any{"type": "string"},
			"severity":    map[string]any{"type": "string"},
			"actor":       map[string]any{"type": "string"},
			"occurred_at": map[string]any{"type": "string", "format": "date-time"},
		}),
	}

	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "RepoSentinel 管理 API",
			"description": "自托管 GitHub 仓库值守平台的管理 API。Agent 可使用 OAuth 2.0 client_credentials 获取只读令牌。",
			"version":     s.dependencies.BuildInfo.Version,
		},
		"servers": []any{
			map[string]any{"url": origin},
		},
		"security": []any{},
		"paths":    paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
					"description":  "OAuth 2.0 client_credentials 签发的访问令牌（POST /oauth/token）。",
				},
			},
			"schemas": schemas,
		},
	}
}

// handleOpenAPIJSON 输出当前实例的 OpenAPI 3.1 描述。
func (s *server) handleOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/openapi+json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, http.StatusOK, s.openAPISpec(r))
}
