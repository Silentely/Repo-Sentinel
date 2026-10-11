package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/buildinfo"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func statusOf(matrix SystemCapabilitiesMatrix, name string) string {
	for _, f := range matrix.Features {
		if f.Name == name {
			return f.Status
		}
	}
	return ""
}

// TestBuildSystemCapabilitiesMatrixReportsRealBuildInfo 守护能力清单必须来自真实构建元数据，
// 且不得再出现编造的版本号、里程碑与「轮次」这类执行过程描述。
func TestBuildSystemCapabilitiesMatrixReportsRealBuildInfo(t *testing.T) {
	info := buildinfo.Info{Version: "0.6.1", GitSHA: "0123456789abcdef", BuildChannel: "release"}
	matrix := BuildSystemCapabilitiesMatrix(SystemCapabilitiesInput{
		BuildInfo:       info,
		EnabledChannels: []string{"slack", "telegram"},
		AIEnabled:       true,
		DigestEnabled:   true,
	})

	if matrix.Version != "0.6.1" || matrix.Commit != "0123456789abcdef" || matrix.BuildChannel != "release" {
		t.Fatalf("版本信息应来自构建元数据，got: %+v", matrix)
	}
	if len(matrix.EnabledChannels) != 2 || matrix.EnabledChannels[0] != "slack" || matrix.EnabledChannels[1] != "telegram" {
		t.Errorf("启用渠道应按字典序稳定输出，got: %v", matrix.EnabledChannels)
	}
	if statusOf(matrix, "multichannel_delivery") != "active" {
		t.Errorf("存在启用渠道时多渠道投递应为 active")
	}
	if statusOf(matrix, "ai_assistance") != "active" {
		t.Errorf("AI 开启时 ai_assistance 应为 active")
	}
	if statusOf(matrix, "digest_reports") != "active" {
		t.Errorf("智能简报开启时 digest_reports 应为 active")
	}

	// 无启用渠道 / AI 关闭时必须如实标注 unavailable，避免「清单声明可用、运行时无渠道」。
	dormant := BuildSystemCapabilitiesMatrix(SystemCapabilitiesInput{BuildInfo: info})
	if got := statusOf(dormant, "multichannel_delivery"); got != "unavailable" {
		t.Errorf("无启用渠道时多渠道投递应为 unavailable，got %q", got)
	}
	if got := statusOf(dormant, "ai_assistance"); got != "unavailable" {
		t.Errorf("AI 关闭时 ai_assistance 应为 unavailable，got %q", got)
	}
	if dormant.EnabledChannels == nil {
		t.Errorf("enabled_channels 应为空数组而非 null")
	}

	raw, err := json.Marshal(dormant)
	if err != nil {
		t.Fatalf("序列化能力清单失败: %v", err)
	}
	for _, banned := range []string{"Round", "rounds", "Milestone", "v2.5.0", "enterprise"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("能力清单不应包含 %q: %s", banned, raw)
		}
	}
}

// TestCapabilitiesEndpointUsesRealRuntimeState 覆盖端点接线：未认证拒绝、已认证返回真实版本与渠道状态。
func TestCapabilitiesEndpointUsesRealRuntimeState(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)

	unauthorized := fixture.request(t, http.MethodGet, "/api/v1/system/capabilities", "", "127.0.0.1:45401", nil, nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("未认证请求应返回 401，got %d", unauthorized.Code)
	}

	cookies := fixture.login(t, httpTestPassword)
	if _, err := fixture.store.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID: "ch-capabilities-telegram", ChannelType: store.ChannelTelegram,
		Name: "capability channel", Enabled: true, Target: "-100999", MinSeverity: "low",
	}); err != nil {
		t.Fatalf("创建渠道失败: %v", err)
	}

	response := fixture.request(t, http.MethodGet, "/api/v1/system/capabilities", "", "127.0.0.1:45402", cookies, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("已认证请求应返回 200，got %d body=%s", response.Code, response.Body.String())
	}

	var matrix SystemCapabilitiesMatrix
	if err := json.Unmarshal(response.Body.Bytes(), &matrix); err != nil {
		t.Fatalf("解析能力清单失败: %v", err)
	}
	if matrix.Version != "0.1.0" {
		t.Errorf("版本应来自注入的构建元数据，got %q", matrix.Version)
	}
	if len(matrix.EnabledChannels) != 1 || matrix.EnabledChannels[0] != store.ChannelTelegram {
		t.Errorf("启用渠道应反映真实渠道表，got %v", matrix.EnabledChannels)
	}
	if statusOf(matrix, "multichannel_delivery") != "active" {
		t.Errorf("存在启用渠道时多渠道投递应为 active")
	}
}
