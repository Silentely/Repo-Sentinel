package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestSettingRegistryKeysUnique 注册表键必须唯一：重复键会让 GET 默认值/PUT 白名单行为不确定。
func TestSettingRegistryKeysUnique(t *testing.T) {
	seen := make(map[string]bool, len(settingSpecs))
	for _, spec := range settingSpecs {
		if seen[spec.key] {
			t.Fatalf("设置注册表存在重复键 %q", spec.key)
		}
		seen[spec.key] = true
	}
	if len(settingSpecs) == 0 {
		t.Fatal("设置注册表不应为空")
	}
}

// TestSettingRegistryEveryKeyValidatable 注册表中每个键都能被 validateSettingValue 正确处理：
// 防止新增键只写了默认值却漏写校验规则（历史漂移来源之一）。
func TestSettingRegistryEveryKeyValidatable(t *testing.T) {
	for _, spec := range settingSpecs {
		// 用该键的默认值自校验：默认值必须通过自身校验规则。
		got, _, ok := validateSettingValue(spec.key, spec.def)
		if !ok {
			t.Fatalf("设置 %q 的默认值 %v 未通过自身校验", spec.key, spec.def)
		}
		// 归一化结果与默认值等价（JSON 层面比较，容忍 int/float64 数值等价）。
		if !jsonEqual(got, spec.def) {
			t.Fatalf("设置 %q 默认值归一化漂移：want %v got %v", spec.key, spec.def, got)
		}
	}
}

// TestSettingRegistryGetReturnsAllKeys GET /system/settings 必须返回注册表中的全部键，
// 与 PUT 白名单保持一致（回归：burst_window_sec 曾只被 PUT 接受、GET 不返回）。
func TestSettingRegistryGetReturnsAllKeys(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	headers := map[string]string{CSRFHeaderName: cookieByName(t, cookies, CSRFCookieName).Value}

	rec := fixture.request(t, http.MethodGet, "/api/v1/system/settings", "", "127.0.0.1:45200", cookies, headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, spec := range settingSpecs {
		if _, exists := out[spec.key]; !exists {
			t.Fatalf("GET /system/settings 未返回注册表键 %q", spec.key)
		}
	}
}

// jsonEqual 比较两个值是否 JSON 语义等价（int/float64 序列化后均为数字字面量，可直接比较）。
func jsonEqual(a, b any) bool {
	ar, errA := json.Marshal(a)
	br, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ar) == string(br)
}
