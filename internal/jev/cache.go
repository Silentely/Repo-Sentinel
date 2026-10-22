package jev

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	MaxAllowedStateSize   = 64 * 1024 // 64 KiB
	MaxCacheableStateSize = 4 * 1024  // 4 KiB
)

type canonicalQuestion struct {
	Type         string   `json:"type"`
	Instructions string   `json:"instructions"`
	CriteriaMap  []kvPair `json:"criteria_map,omitempty"`
	CriteriaList []string `json:"criteria_list,omitempty"`
}

type kvPair struct {
	Key string  `json:"key"`
	Val *string `json:"val,omitempty"`
}

type canonicalPayload struct {
	Plane                string `json:"plane"`
	Endpoint             string `json:"endpoint"`
	Model                string `json:"model"`
	ConfiguredModel      string `json:"configured_model"`
	CredentialGeneration uint64 `json:"credential_generation"`
	PolicyVersion        string `json:"policy_version"`
	RuntimeGeneration    uint64 `json:"runtime_generation"`
	StateDigest          string `json:"state_digest"`
	QuestionFingerprint  string `json:"question_fingerprint"`
}

// StateDigest 计算状态对象的规范化 SHA256 摘要。
// 使用 json.Number 保留原始数值表示，避免不同类型解析为 float64 引起的同键或精度漂移。
func StateDigest(state json.RawMessage) (string, error) {
	d := json.NewDecoder(bytes.NewReader(state))
	d.UseNumber()
	var val any
	if err := d.Decode(&val); err != nil {
		return "", err
	}
	canonicalData, err := json.Marshal(val)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(canonicalData)
	return hex.EncodeToString(hash[:]), nil
}

// QuestionFingerprint 计算问题集合的规范化 SHA256 指纹。
func QuestionFingerprint(questions map[string]Question) (string, error) {
	normQuestions := make(map[string]canonicalQuestion, len(questions))
	for qName, q := range questions {
		cq := canonicalQuestion{
			Type: q.questionType(),
		}
		switch val := q.(type) {
		case NoulQuestion:
			cq.Instructions = val.Instructions
			if len(val.Criteria) > 0 {
				var pairs []kvPair
				for k, v := range val.Criteria {
					valCopy := v
					pairs = append(pairs, kvPair{Key: k, Val: &valCopy})
				}
				sort.Slice(pairs, func(i, j int) bool { return pairs[i].Key < pairs[j].Key })
				cq.CriteriaMap = pairs
			}
		case ChoiceQuestion:
			cq.Instructions = val.Instructions
			if len(val.Criteria) > 0 {
				var pairs []kvPair
				for k, v := range val.Criteria {
					pairs = append(pairs, kvPair{Key: k, Val: v})
				}
				sort.Slice(pairs, func(i, j int) bool { return pairs[i].Key < pairs[j].Key })
				cq.CriteriaMap = pairs
			}
		case ScoreQuestion:
			cq.Instructions = val.Instructions
			cq.CriteriaList = append([]string(nil), val.Criteria...)
		}
		normQuestions[qName] = cq
	}

	data, err := json.Marshal(normQuestions)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// CanonicalKey 计算客户端调用的完整缓存键（严格区分不同 Model、Endpoint 与配置代次）。
func CanonicalKey(opts ClientOptions, state json.RawMessage, questions map[string]Question) (string, error) {
	sd, err := StateDigest(state)
	if err != nil {
		return "", err
	}
	qf, err := QuestionFingerprint(questions)
	if err != nil {
		return "", err
	}

	model := opts.Model
	if model == "" {
		model = "default"
	}

	payload := canonicalPayload{
		Plane:                opts.Plane,
		Endpoint:             NormalizeEndpoint(opts.BaseURL),
		Model:                model,
		ConfiguredModel:      opts.Model,
		CredentialGeneration: opts.CredentialGeneration,
		PolicyVersion:        opts.PolicyVersion,
		RuntimeGeneration:    opts.RuntimeGeneration,
		StateDigest:          sd,
		QuestionFingerprint:  qf,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// IsTruncatedState 判定请求状态是否包含截断标记（截断状态严禁自动放行，必须保守终局）。
// 必须在解码后递归检查，防止 Unicode 转义（如 \u0074runcated、\u005bTRUNCATED\u005d）绕过。
func IsTruncatedState(state json.RawMessage) bool {
	if len(state) == 0 {
		return false
	}
	// 快速原串预检
	if strings.Contains(string(state), "[TRUNCATED]") {
		return true
	}

	var decoded any
	if err := json.Unmarshal(state, &decoded); err != nil {
		return false
	}

	return containsTruncatedDecoded(decoded)
}

func containsTruncatedDecoded(val any) bool {
	switch v := val.(type) {
	case string:
		if strings.Contains(v, "[TRUNCATED]") {
			return true
		}
	case map[string]any:
		for k, child := range v {
			lowerK := strings.ToLower(k)
			if lowerK == "truncated" || lowerK == "is_truncated" || lowerK == "diff_truncated" {
				if b, ok := child.(bool); ok && b {
					return true
				}
				if s, ok := child.(string); ok && (strings.EqualFold(s, "true") || strings.Contains(s, "[TRUNCATED]")) {
					return true
				}
			}
			if containsTruncatedDecoded(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsTruncatedDecoded(child) {
				return true
			}
		}
	}
	return false
}

// IsCacheable 判定请求状态是否允许进入决策缓存。
// 敏感请求、超长或截断请求严禁进入缓存。
func IsCacheable(state json.RawMessage) bool {
	if len(state) == 0 || len(state) > MaxCacheableStateSize {
		return false
	}

	if IsTruncatedState(state) {
		return false
	}

	var stateMap map[string]any
	if err := json.Unmarshal(state, &stateMap); err != nil {
		return false
	}

	// 检查顶层敏感布尔标记
	for _, key := range []string{"sensitive", "is_sensitive"} {
		if v, ok := stateMap[key]; ok {
			if b, ok := v.(bool); ok && b {
				return false
			}
		}
	}

	// 检查嵌套 files 中的敏感标记
	if files, ok := stateMap["files"].([]any); ok {
		for _, f := range files {
			if fMap, ok := f.(map[string]any); ok {
				if s, ok := fMap["sensitive"].(bool); ok && s {
					return false
				}
			}
		}
	}

	// 深度遍历 JSON 文本内容：检查解码后文本中的敏感/截断标记及高熵 Token
	if containsSensitiveOrTruncatedDecoded(stateMap) {
		return false
	}

	return true
}

func containsSensitiveOrTruncatedDecoded(val any) bool {
	switch v := val.(type) {
	case string:
		if strings.Contains(v, "[REDACTED_SECRET]") || strings.Contains(v, "[REDACTED_EMAIL]") || strings.Contains(v, "[TRUNCATED]") {
			return true
		}
		if hasHighEntropySecretToken(v) {
			return true
		}
	case map[string]any:
		for _, child := range v {
			if containsSensitiveOrTruncatedDecoded(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsSensitiveOrTruncatedDecoded(child) {
				return true
			}
		}
	}
	return false
}

// hasHighEntropySecretToken 检测字符串是否包含疑似高熵敏感 Token（如密钥、hash、token）
func hasHighEntropySecretToken(s string) bool {
	words := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '"' || r == '\'' || r == ',' || r == ';'
	})
	for _, w := range words {
		// 常见密钥前缀
		if strings.HasPrefix(w, "ghp_") || strings.HasPrefix(w, "gho_") || strings.HasPrefix(w, "ghu_") ||
			strings.HasPrefix(w, "ghs_") || strings.HasPrefix(w, "ghr_") || strings.HasPrefix(w, "sk-") {
			return true
		}
		// 长度 >= 24 且高香农熵的连续随机串
		if len(w) >= 24 && len(w) <= 128 {
			if calcShannonEntropy(w) >= 4.5 {
				return true
			}
		}
	}
	return false
}

// calcShannonEntropy 计算字符串的香农信息熵
func calcShannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	freq := make(map[rune]float64)
	for _, r := range s {
		freq[r]++
	}
	l := float64(len(s))
	var entropy float64
	for _, count := range freq {
		p := count / l
		entropy -= p * math.Log2(p)
	}
	return entropy
}

type cacheItem struct {
	result    Result
	expiresAt time.Time
}

type CacheStats struct {
	Hits      uint64  `json:"hits"`
	Misses    uint64  `json:"misses"`
	Evictions uint64  `json:"evictions"`
	HitRate   float64 `json:"hit_rate"`
}

type decisionCache struct {
	mu        sync.Mutex
	capacity  int
	ttl       time.Duration
	now       func() time.Time
	jitter    func() float64
	items     map[string]cacheItem
	accessLog []string // 维护访问顺序以实现 LRU 淘汰
	hits      uint64
	misses    uint64
	evictions uint64
	onEvict   func()
}

func newCache(capacity int, ttl time.Duration, now func() time.Time, jitter func() float64) *decisionCache {
	return newCacheWithOnEvict(capacity, ttl, now, jitter, nil)
}

func newCacheWithOnEvict(capacity int, ttl time.Duration, now func() time.Time, jitter func() float64, onEvict func()) *decisionCache {
	if capacity <= 0 {
		capacity = 1000
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if now == nil {
		now = time.Now
	}
	if jitter == nil {
		jitter = func() float64 { return 0 }
	}
	return &decisionCache{
		capacity:  capacity,
		ttl:       ttl,
		now:       now,
		jitter:    jitter,
		items:     make(map[string]cacheItem),
		accessLog: make([]string, 0, capacity),
		onEvict:   onEvict,
	}
}

func (c *decisionCache) get(key string) (Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, exists := c.items[key]
	if !exists {
		c.misses++
		return Result{}, false
	}

	// 检查是否过期
	if c.now().After(item.expiresAt) {
		delete(c.items, key)
		c.removeAccessKey(key)
		// 规则 11：过期访问按 miss 统计，不虚高计入 capacity evictions
		c.misses++
		return Result{}, false
	}

	c.hits++
	c.updateAccessOrder(key)
	return cloneResult(item.result), true
}

func (c *decisionCache) put(key string, res Result) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.items[key]; exists {
		c.updateAccessOrder(key)
	} else {
		// 容量达到上限，淘汰最久未访问的项
		if len(c.items) >= c.capacity {
			c.evictOldest()
		}
		c.accessLog = append(c.accessLog, key)
	}

	// 规则 3：TTL 加 ±10% 抖动，防止雪崩
	jitterRatio := c.jitter() * 0.1
	jitteredTTL := time.Duration(float64(c.ttl) * (1.0 + jitterRatio))
	if jitteredTTL <= 0 {
		jitteredTTL = c.ttl
	}

	c.items[key] = cacheItem{
		result:    cloneResult(res),
		expiresAt: c.now().Add(jitteredTTL),
	}
}

func (c *decisionCache) updateAccessOrder(key string) {
	c.removeAccessKey(key)
	c.accessLog = append(c.accessLog, key)
}

func (c *decisionCache) removeAccessKey(key string) {
	for i, k := range c.accessLog {
		if k == key {
			c.accessLog = append(c.accessLog[:i], c.accessLog[i+1:]...)
			break
		}
	}
}

func (c *decisionCache) evictOldest() {
	if len(c.accessLog) == 0 {
		return
	}
	oldestKey := c.accessLog[0]
	c.accessLog = c.accessLog[1:]
	delete(c.items, oldestKey)
	c.evictions++
	if c.onEvict != nil {
		c.onEvict()
	}
}

func (c *decisionCache) stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()

	total := c.hits + c.misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(c.hits) / float64(total) * 100.0
	}

	return CacheStats{
		Hits:      c.hits,
		Misses:    c.misses,
		Evictions: c.evictions,
		HitRate:   hitRate,
	}
}

func cloneResult(r Result) Result {
	clone := r
	if r.Answers != nil {
		clone.Answers = make(map[string]Answer, len(r.Answers))
		for k, v := range r.Answers {
			ansCopy := v
			if v.Choice != nil {
				s := *v.Choice
				ansCopy.Choice = &s
			}
			if v.Score != nil {
				sc := *v.Score
				ansCopy.Score = &sc
			}
			if v.Noul != nil {
				n := *v.Noul
				ansCopy.Noul = &n
			}
			if v.Probabilities != nil {
				pMap := make(map[string]float64, len(v.Probabilities))
				for pk, pv := range v.Probabilities {
					pMap[pk] = pv
				}
				ansCopy.Probabilities = pMap
			}
			clone.Answers[k] = ansCopy
		}
	}
	return clone
}
