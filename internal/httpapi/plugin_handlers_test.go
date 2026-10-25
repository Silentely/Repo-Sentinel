package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/plugin"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

type dummyProvider struct {
	name string
	caps []plugin.Capability
}

func (d *dummyProvider) Name() string { return d.name }
func (d *dummyProvider) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name:         d.name,
		Version:      "1.0.0",
		Capabilities: d.caps,
	}
}
func (d *dummyProvider) Start(ctx context.Context) error { return nil }
func (d *dummyProvider) Stop(ctx context.Context) error  { return nil }
func (d *dummyProvider) Health(ctx context.Context) (plugin.PluginHealth, error) {
	return plugin.PluginHealth{
		Status:    plugin.HealthOK,
		Message:   "running",
		CheckedAt: time.Now().UTC(),
	}, nil
}
func (d *dummyProvider) DispatchEvent(ctx context.Context, ev store.Event) error { return nil }

func TestListPluginsHandler(t *testing.T) {
	mgr := plugin.NewManager(nil)
	_ = mgr.Register(context.Background(), &dummyProvider{name: "feishu-ext", caps: []plugin.Capability{plugin.CapNotification}})

	fixture := newHTTPTestFixture(t, httpTestOptions{pluginManager: mgr})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	// 1. 未登录 -> 401
	unauth := fixture.request(t, http.MethodGet, "/api/v1/system/plugins", "", "127.0.0.1:45001", nil, nil)
	assertAPIError(t, unauth, http.StatusUnauthorized, "unauthorized")

	// 2. 正常查询 -> 200 返回已加载插件
	resp := fixture.request(t, http.MethodGet, "/api/v1/system/plugins", "", "127.0.0.1:45002", cookies, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var body struct {
		Plugins []struct {
			Manifest struct {
				Name string `json:"name"`
			} `json:"manifest"`
			Health struct {
				Status string `json:"status"`
			} `json:"health"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(body.Plugins) != 1 || body.Plugins[0].Manifest.Name != "feishu-ext" {
		t.Errorf("expected feishu-ext plugin, got %+v", body.Plugins)
	}
	if body.Plugins[0].Health.Status != "healthy" {
		t.Errorf("expected healthy status, got %s", body.Plugins[0].Health.Status)
	}
}
