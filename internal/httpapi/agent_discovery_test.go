package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"strings"
	"testing"
)

// TestSitemapXML输出规范URL与全部SPA路由 验证 sitemap.xml 可解析且覆盖全部路由。
func TestSitemapXML输出规范URL与全部SPA路由(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{publicBaseURL: "https://reposentinel.example"})
	response := fixture.request(t, http.MethodGet, "/sitemap.xml", "", "127.0.0.1:41001", nil, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/xml") {
		t.Fatalf("Content-Type=%q，期望 application/xml", contentType)
	}
	var document struct {
		URLSet []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("sitemap 不是合法 XML: %v", err)
	}
	if len(document.URLSet) != len(spaCanonicalPaths) {
		t.Fatalf("url 数量=%d，期望 %d", len(document.URLSet), len(spaCanonicalPaths))
	}
	seen := map[string]bool{}
	for _, entry := range document.URLSet {
		if !strings.HasPrefix(entry.Loc, "https://reposentinel.example") {
			t.Fatalf("loc=%q 缺少规范 Origin", entry.Loc)
		}
		seen[entry.Loc] = true
	}
	for _, sitePath := range spaCanonicalPaths {
		if !seen["https://reposentinel.example"+sitePath] {
			t.Fatalf("sitemap 缺少路由 %q", sitePath)
		}
	}
}

// TestRobotsTXT包含Sitemap与ContentSignals 验证 robots.txt 内容完整。
func TestRobotsTXT包含Sitemap与ContentSignals(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{publicBaseURL: "https://reposentinel.example"})
	response := fixture.request(t, http.MethodGet, "/robots.txt", "", "127.0.0.1:41002", nil, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	body := response.Body.String()
	for _, want := range []string{
		"User-agent: *",
		"Disallow: /api/",
		"Sitemap: https://reposentinel.example/sitemap.xml",
		"Content-Signal: ai-train=no, search=yes, ai-input=no",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("robots.txt 缺少 %q；全文=%s", want, body)
		}
	}
}

// TestLinkHeaders中间件为GET附加Agent发现链接 验证 Link 头。
func TestLinkHeaders中间件为GET附加Agent发现链接(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{publicBaseURL: "https://reposentinel.example"})
	response := fixture.request(t, http.MethodGet, "/health/live", "", "127.0.0.1:41003", nil, nil)

	link := response.Header().Get("Link")
	for _, want := range []string{
		`</.well-known/api-catalog>; rel="api-catalog"`,
		`</openapi.json>; rel="service-desc"`,
		`</auth.md>; rel="service-doc"`,
		`</sitemap.xml>; rel="describedby"`,
	} {
		if !strings.Contains(link, want) {
			t.Fatalf("Link 头缺少 %q；Link=%q", want, link)
		}
	}
}

// TestWellKnownAPICatalog返回linkset结构 验证 RFC 9727 目录。
func TestWellKnownAPICatalog返回linkset结构(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{publicBaseURL: "https://reposentinel.example"})
	response := fixture.request(t, http.MethodGet, "/.well-known/api-catalog", "", "127.0.0.1:41004", nil, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/linkset+json") {
		t.Fatalf("Content-Type=%q，期望 application/linkset+json", contentType)
	}
	var catalog struct {
		Linkset []struct {
			Anchor      string `json:"anchor"`
			ServiceDesc []struct {
				Href string `json:"href"`
				Type string `json:"type"`
			} `json:"service-desc"`
			Status []struct {
				Href string `json:"href"`
			} `json:"status"`
		} `json:"linkset"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("目录不是合法 JSON: %v", err)
	}
	if len(catalog.Linkset) != 1 {
		t.Fatalf("linkset 条目数=%d，期望 1", len(catalog.Linkset))
	}
	entry := catalog.Linkset[0]
	if entry.Anchor != "https://reposentinel.example/api/v1" {
		t.Fatalf("anchor=%q", entry.Anchor)
	}
	if len(entry.ServiceDesc) != 1 || !strings.Contains(entry.ServiceDesc[0].Href, "/openapi.json") {
		t.Fatalf("service-desc 未指向 OpenAPI: %+v", entry.ServiceDesc)
	}
	if len(entry.Status) != 1 || !strings.Contains(entry.Status[0].Href, "/health/ready") {
		t.Fatalf("status 未指向健康端点: %+v", entry.Status)
	}
}

// TestWellKnownOAuthAuthorizationServer元数据完整 验证 RFC 8414 元数据。
func TestWellKnownOAuthAuthorizationServer元数据完整(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{publicBaseURL: "https://reposentinel.example"})
	response := fixture.request(t, http.MethodGet, "/.well-known/oauth-authorization-server", "", "127.0.0.1:41005", nil, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	var metadata map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &metadata); err != nil {
		t.Fatalf("元数据不是合法 JSON: %v", err)
	}
	if metadata["issuer"] != "https://reposentinel.example" {
		t.Fatalf("issuer=%v", metadata["issuer"])
	}
	if metadata["token_endpoint"] != "https://reposentinel.example/oauth/token" {
		t.Fatalf("token_endpoint=%v", metadata["token_endpoint"])
	}
	if metadata["jwks_uri"] != "https://reposentinel.example/oauth/jwks" {
		t.Fatalf("jwks_uri=%v", metadata["jwks_uri"])
	}
	grants, ok := metadata["grant_types_supported"].([]any)
	if !ok || len(grants) == 0 {
		t.Fatalf("grant_types_supported=%v", metadata["grant_types_supported"])
	}
}

// TestWellKnownOAuthProtectedResource元数据完整 验证 RFC 9728 受保护资源元数据。
func TestWellKnownOAuthProtectedResource元数据完整(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{publicBaseURL: "https://reposentinel.example"})
	response := fixture.request(t, http.MethodGet, "/.well-known/oauth-protected-resource", "", "127.0.0.1:41006", nil, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	var metadata map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &metadata); err != nil {
		t.Fatalf("元数据不是合法 JSON: %v", err)
	}
	if metadata["resource"] != "https://reposentinel.example/api/v1" {
		t.Fatalf("resource=%v", metadata["resource"])
	}
	servers, ok := metadata["authorization_servers"].([]any)
	if !ok || len(servers) == 0 {
		t.Fatalf("authorization_servers=%v", metadata["authorization_servers"])
	}
	methods, ok := metadata["bearer_methods_supported"].([]any)
	if !ok || len(methods) == 0 || methods[0] != "header" {
		t.Fatalf("bearer_methods_supported=%v", metadata["bearer_methods_supported"])
	}
}

// TestAuthMD文件包含标题与令牌获取指引 验证 /auth.md 内容。
func TestAuthMD文件包含标题与令牌获取指引(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{publicBaseURL: "https://reposentinel.example"})
	response := fixture.request(t, http.MethodGet, "/auth.md", "", "127.0.0.1:41007", nil, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	body := response.Body.String()
	if !strings.HasPrefix(body, "# RepoSentinel auth.md") {
		t.Fatalf("H1 未包含 auth.md：%q", strings.SplitN(body, "\n", 2)[0])
	}
	for _, want := range []string{
		"https://reposentinel.example/oauth/token",
		"grant_type=client_credentials",
		"Authorization: Bearer",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("auth.md 缺少 %q", want)
		}
	}
}

// TestAgentSkillsIndex摘要与工件一致 验证 skills 索引的 digest 对应实际工件。
func TestAgentSkillsIndex摘要与工件一致(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{publicBaseURL: "https://reposentinel.example"})
	response := fixture.request(t, http.MethodGet, "/.well-known/agent-skills/index.json", "", "127.0.0.1:41008", nil, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	var index struct {
		Schema string `json:"$schema"`
		Skills []struct {
			Name   string `json:"name"`
			Type   string `json:"type"`
			URL    string `json:"url"`
			Digest string `json:"digest"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &index); err != nil {
		t.Fatalf("索引不是合法 JSON: %v", err)
	}
	if !strings.Contains(index.Schema, "agentskills.io") {
		t.Fatalf("$schema=%q", index.Schema)
	}
	if len(index.Skills) != 1 || index.Skills[0].Type != "skill-md" {
		t.Fatalf("skills=%+v", index.Skills)
	}
	artifactResponse := fixture.request(
		t, http.MethodGet, "/.well-known/agent-skills/reposentinel-api/SKILL.md", "", "127.0.0.1:41009", nil, nil,
	)
	if artifactResponse.Code != http.StatusOK {
		t.Fatalf("工件 status=%d", artifactResponse.Code)
	}
	computed := "sha256:" + sha256Hex(artifactResponse.Body.Bytes())
	if index.Skills[0].Digest != computed {
		t.Fatalf("digest=%q，实际工件 sha256=%q", index.Skills[0].Digest, computed)
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TestMCPCard字段完整 验证 server-card.json 的关键字段。
func TestMCPCard字段完整(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{publicBaseURL: "https://reposentinel.example"})
	response := fixture.request(t, http.MethodGet, "/.well-known/mcp/server-card.json", "", "127.0.0.1:41010", nil, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	var card map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &card); err != nil {
		t.Fatalf("卡片不是合法 JSON: %v", err)
	}
	if card["name"] != "io.reposentinel/admin" {
		t.Fatalf("name=%v", card["name"])
	}
	if _, ok := card["version"].(string); !ok {
		t.Fatalf("version=%v", card["version"])
	}
	remotes, ok := card["remotes"].([]any)
	if !ok || len(remotes) == 0 {
		t.Fatalf("remotes=%v", card["remotes"])
	}
	endpoint, ok := card["endpoint"].(string)
	if !ok || endpoint != "https://reposentinel.example/mcp" {
		t.Fatalf("endpoint=%v", card["endpoint"])
	}
	if _, ok := card["serverInfo"].(map[string]any); !ok {
		t.Fatalf("serverInfo=%v", card["serverInfo"])
	}
}

// TestMarkdown协商返回markdown与token头 验证 Accept: text/markdown 内容协商。
func TestMarkdown协商返回markdown与token头(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{publicBaseURL: "https://reposentinel.example"})
	response := fixture.request(
		t, http.MethodGet, "/", "", "127.0.0.1:41011", nil,
		map[string]string{"Accept": "text/markdown"},
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/markdown") {
		t.Fatalf("Content-Type=%q，期望 text/markdown", contentType)
	}
	if response.Header().Get("X-Markdown-Tokens") == "" {
		t.Fatal("缺少 X-Markdown-Tokens 头")
	}
	body := response.Body.String()
	if !strings.HasPrefix(body, "# RepoSentinel") {
		t.Fatalf("markdown 首行=%q", strings.SplitN(body, "\n", 2)[0])
	}
}

// Test无markdownAccept保持默认 浏览器请求不受影响。
func Test无markdownAccept保持默认(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{publicBaseURL: "https://reposentinel.example"})
	response := fixture.request(t, http.MethodGet, "/", "", "127.0.0.1:41012", nil, nil)
	if contentType := response.Header().Get("Content-Type"); strings.Contains(contentType, "text/markdown") {
		t.Fatalf("无 Accept 头时不应返回 markdown：%q", contentType)
	}
}

// TestOpenAPIJSON结构完整 验证 OpenAPI 3.1 文档关键字段。
func TestOpenAPIJSON结构完整(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{publicBaseURL: "https://reposentinel.example"})
	response := fixture.request(t, http.MethodGet, "/openapi.json", "", "127.0.0.1:41013", nil, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	var spec map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &spec); err != nil {
		t.Fatalf("OpenAPI 不是合法 JSON: %v", err)
	}
	if spec["openapi"] != "3.1.0" {
		t.Fatalf("openapi=%v", spec["openapi"])
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok || paths["/api/v1/dashboard"] == nil || paths["/oauth/token"] == nil || paths["/health/ready"] == nil {
		t.Fatalf("paths 缺少关键端点: %v", paths)
	}
	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("缺少 components")
	}
	securitySchemes, ok := components["securitySchemes"].(map[string]any)
	if !ok || securitySchemes["bearerAuth"] == nil {
		t.Fatalf("缺少 bearerAuth 安全方案: %v", securitySchemes)
	}
}
