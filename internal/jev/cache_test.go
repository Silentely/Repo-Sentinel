package jev

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCacheCanonical(t *testing.T) {
	opts1 := ClientOptions{
		Plane:                "notification",
		BaseURL:              "https://api.typesafe.ai",
		Model:                "jev-1.14.0",
		PolicyVersion:        "jev-policy-v1",
		CredentialGeneration: 1,
		RuntimeGeneration:    1,
	}

	state1 := json.RawMessage(`{"b": 2, "a": 1}`)
	state2 := json.RawMessage(`{"a": 1, "b": 2}`)

	q := map[string]Question{
		"c": ChoiceQuestion{
			Type:     "choice",
			Criteria: map[string]*string{"y": nil, "x": nil},
		},
	}

	k1, err := CanonicalKey(opts1, state1, q)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := CanonicalKey(opts1, state2, q)
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k2 {
		t.Fatalf("map 键重排后应该获得相同的 key: %s != %s", k1, k2)
	}

	// score 数组顺序敏感
	qScore1 := map[string]Question{
		"s": ScoreQuestion{Type: "score", Criteria: []string{"A", "B"}},
	}
	qScore2 := map[string]Question{
		"s": ScoreQuestion{Type: "score", Criteria: []string{"B", "A"}},
	}
	ks1, _ := CanonicalKey(opts1, state1, qScore1)
	ks2, _ := CanonicalKey(opts1, state1, qScore2)
	if ks1 == ks2 {
		t.Fatal("score 数组反序必须产生不同的 key")
	}

	// 改变 credential generation
	opts2 := opts1
	opts2.CredentialGeneration = 2
	k3, _ := CanonicalKey(opts2, state1, q)
	if k1 == k3 {
		t.Fatal("改变 credential generation 必须产生不同的 key")
	}

	// 改变 plane
	opts3 := opts1
	opts3.Plane = "review"
	k4, _ := CanonicalKey(opts3, state1, q)
	if k1 == k4 {
		t.Fatal("改变 plane 必须产生不同的 key")
	}
}

func TestCacheTTLAndEviction(t *testing.T) {
	now := time.Unix(1000, 0)
	c := newCache(2, 10*time.Minute, func() time.Time { return now }, func() float64 { return 0 }) // 无 jitter

	res1 := Result{Model: "m1"}
	res2 := Result{Model: "m2"}
	res3 := Result{Model: "m3"}

	c.put("k1", res1)
	c.put("k2", res2)

	// k1 命中
	got1, ok := c.get("k1")
	if !ok || got1.Model != "m1" {
		t.Fatal("k1 应命中")
	}

	// 放入 k3 导致最久未访问的 k2 淘汰 (k1 刚被访问过)
	c.put("k3", res3)
	if _, ok := c.get("k2"); ok {
		t.Fatal("k2 应该已被 LRU 淘汰")
	}
	if _, ok := c.get("k1"); !ok {
		t.Fatal("k1 应该仍然存在")
	}

	st := c.stats()
	if st.Evictions != 1 {
		t.Fatalf("expected 1 eviction, got %d", st.Evictions)
	}

	// 时间前进 11 分钟后全部过期
	now = now.Add(11 * time.Minute)
	if _, ok := c.get("k1"); ok {
		t.Fatal("过期后 k1 不应命中")
	}
	if _, ok := c.get("k3"); ok {
		t.Fatal("过期后 k3 不应命中")
	}

	st2 := c.stats()
	// k1 和 k3 过期访问被移除各算一次 eviction
	if st2.Evictions != 3 {
		t.Fatalf("expected 3 evictions after TTL expiry, got %d", st2.Evictions)
	}
	if st2.Hits != 2 {
		t.Fatalf("expected 2 hits, got %d", st2.Hits)
	}
	if st2.Misses != 3 {
		t.Fatalf("expected 3 misses, got %d", st2.Misses)
	}
	// 命中率 2 / (2 + 3) = 40%
	if st2.HitRate != 40.0 {
		t.Fatalf("expected 40.0 hit rate, got %f", st2.HitRate)
	}
}

func TestCacheIsCacheable(t *testing.T) {
	// 正常可缓存请求
	normalState := json.RawMessage(`{"kind":"workflow_run","conclusion":"failure"}`)
	if !IsCacheable(normalState) {
		t.Fatal("normal state should be cacheable")
	}

	// 敏感请求：顶层 sensitive 字段
	sensitive1 := json.RawMessage(`{"sensitive":true,"kind":"push"}`)
	if IsCacheable(sensitive1) {
		t.Fatal("sensitive state must not be cacheable")
	}

	// 敏感请求：文件列表中存在 sensitive: true
	sensitiveFiles := json.RawMessage(`{"files":[{"path":".env","sensitive":true}]}`)
	if IsCacheable(sensitiveFiles) {
		t.Fatal("state with sensitive files must not be cacheable")
	}

	// 敏感请求：包含 [REDACTED_SECRET]
	sensitiveSecret := json.RawMessage(`{"text":"token is [REDACTED_SECRET]"}`)
	if IsCacheable(sensitiveSecret) {
		t.Fatal("state with redacted secret must not be cacheable")
	}

	// 超长/截断请求：顶层 truncated
	truncated1 := json.RawMessage(`{"truncated":true,"kind":"push"}`)
	if IsCacheable(truncated1) {
		t.Fatal("truncated state must not be cacheable")
	}
	truncated2 := json.RawMessage(`{"diff_truncated":true,"kind":"pr"}`)
	if IsCacheable(truncated2) {
		t.Fatal("diff_truncated state must not be cacheable")
	}

	// 包含 [TRUNCATED] 标识
	truncatedTag := json.RawMessage(`{"summary":"diff [TRUNCATED]"}`)
	if IsCacheable(truncatedTag) {
		t.Fatal("state with [TRUNCATED] must not be cacheable")
	}

	// 超过缓存体积上限 (MaxCacheableStateSize = 4096)
	oversize := json.RawMessage(`{"padding":"` + strings.Repeat("x", 4100) + `"}`)
	if IsCacheable(oversize) {
		t.Fatal("oversize state must not be cacheable")
	}
}

func TestCacheConcurrentReadWriteRace(t *testing.T) {
	now := time.Unix(1000, 0)
	c := newCache(50, 10*time.Minute, func() time.Time { return now }, nil)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 读 goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = c.get("k1")
					_, _ = c.get("k2")
					_ = c.stats()
				}
			}
		}()
	}

	// 写 goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.put("k1", Result{Model: "m1"})
				c.put("k2", Result{Model: "m2"})
			}
		}(i)
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func TestCacheDeepClone(t *testing.T) {
	now := time.Unix(1000, 0)
	c := newCache(10, 10*time.Minute, func() time.Time { return now }, nil)

	val := 0.5
	choiceStr := "high"
	original := Result{
		Model: "jev-1.14.0",
		Answers: map[string]Answer{
			"a": {
				Type:          "choice",
				Choice:        &choiceStr,
				Noul:          &val,
				Probabilities: map[string]float64{"high": 0.8},
			},
		},
	}
	c.put("k", original)

	got, ok := c.get("k")
	if !ok {
		t.Fatal("应命中")
	}

	// 修改获取到的副本
	*got.Answers["a"].Choice = "low"
	got.Answers["a"].Probabilities["high"] = 0.1

	// 再次从缓存读取，原值必须完好未被篡改
	gotAgain, _ := c.get("k")
	if *gotAgain.Answers["a"].Choice != "high" || gotAgain.Answers["a"].Probabilities["high"] != 0.8 {
		t.Fatal("深拷贝失败，缓存内部被外部修改污染")
	}
}
