package updatecheck

import "testing"

func TestNormalizeAndCompare(t *testing.T) {
	if got := NormalizeVersion("v1.2.3"); got != "1.2.3" {
		t.Fatalf("normalize=%q", got)
	}
	if !IsUpdateAvailable("0.3.1", "0.4.0") {
		t.Fatal("0.4.0 should be newer than 0.3.1")
	}
	if IsUpdateAvailable("0.4.0", "0.4.0") {
		t.Fatal("same version is not an update")
	}
	if IsUpdateAvailable("0.4.1", "0.4.0") {
		t.Fatal("older latest is not an update")
	}
	if !IsUpdateAvailable("0.3.1", "v0.3.2") {
		t.Fatal("v-prefix should compare")
	}
}

// 预发布语义：正式版 > 同版本号预发布版；跨主版本时预发布也按数值比较。
func TestUpdateAvailableWithPrerelease(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"1.0.0-rc1", "1.0.0", true},  // RC 提示升级到正式版
		{"1.0.0", "1.0.0-rc1", false}, // 正式版不应降级到 RC
		{"2.0.0-rc1", "1.9.9", false}, // 更高主版本的 RC 不视为需要更新
		{"1.0.0-rc1", "1.0.0-rc2", true},
		{"1.0.0-rc2", "1.0.0-rc1", false},
		{"1.0.0-alpha", "1.0.0-beta", true},
	}
	for _, tc := range cases {
		if got := IsUpdateAvailable(tc.current, tc.latest); got != tc.want {
			t.Fatalf("IsUpdateAvailable(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestParseSemverIgnoresPreRelease(t *testing.T) {
	maj, min, pat := ParseSemver("1.2.3-beta+build")
	if maj != 1 || min != 2 || pat != 3 {
		t.Fatalf("got %d.%d.%d", maj, min, pat)
	}
}
