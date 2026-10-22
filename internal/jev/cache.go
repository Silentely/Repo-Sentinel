package jev

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxCacheableStateSize 定义允许进入决策缓存的最大状态字节数（超长请求不入缓存）。
const MaxCacheableStateSize = 4096

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
	CredentialGeneration uint64 `json:"credential_generation"`
	ConfiguredModel      string `json:"configured_model"`
	PolicyVersion        string `json:"policy_version"`
	RuntimeGeneration    uint64 `json:"runtime_generation"`
	StateDigest          string `json:"state_digest"`
	QuestionFingerprint  string `json:"question_fingerprint"`
}

// StateDigest 计算状态对象的规范化 SHA256 摘要。
func StateDigest(state json.RawMessage) (string, error) {
	var val any
	if err := json.Unmarshal(state, &val); err != nil {
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

// IsCacheable 判定请求状态是否允许进入决策缓存。
// 敏感请求、超长或截断请求严禁进入缓存。
func IsCacheable(state json.RawMessage) bool {
	if len(state) == 0 || len(state) > MaxCacheableStateSize {
		return false
	}
	rawStr := string(state)
	// 敏感凭据/邮箱
	if strings.Contains(rawStr, "[REDACTED_SECRET]") || strings.Contains(rawStr, "[REDACTED_EMAIL]") {
		return false
	}
	// 超长/截断标记
	if strings.Contains(rawStr, "[TRUNCATED]") {
		return false
	}

	var stateMap map[string]any
	if err := json.Unmarshal(state, &stateMap); err != nil {
		return false
	}

	// 检查顶层布尔标记
	for _, key := range []string{"sensitive", "is_sensitive"} {
		if v, ok := stateMap[key]; ok {
			if b, ok := v.(bool); ok && b {
				return false
			}
		}
	}
	for _, key := range []string{"truncated", "is_truncated", "diff_truncated"} {
		if v, ok := stateMap[key]; ok {
			if b, ok := v.(bool); ok && b {
				return false
			}
		}
	}

	// 检查嵌套 files 中的敏感或截断标记
	if files, ok := stateMap["files"].([]any); ok {
		for _, f := range files {
			if fMap, ok := f.(map[string]any); ok {
				if s, ok := fMap["sensitive"].(bool); ok && s {
					return false
				}
				if t, ok := fMap["truncated"].(bool); ok && t {
					return false
				}
			}
		}
	}

	return true
}

// CanonicalKey 生成跨环境确定性的决策缓存键 SHA256。
// 键由规范化 state 摘要 + question 指纹及运行时上下文共同决定。
func CanonicalKey(options ClientOptions, state json.RawMessage, questions map[string]Question) (string, error) {
	stateDigest, err := StateDigest(state)
	if err != nil {
		return "", err
	}
	qFingerprint, err := QuestionFingerprint(questions)
	if err != nil {
		return "", err
	}

	payload := canonicalPayload{
		Plane:                options.Plane,
		Endpoint:             options.BaseURL,
		CredentialGeneration: options.CredentialGeneration,
		ConfiguredModel:      options.Model,
		PolicyVersion:        options.PolicyVersion,
		RuntimeGeneration:    options.RuntimeGeneration,
		StateDigest:          stateDigest,
		QuestionFingerprint:  qFingerprint,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

type cacheEntry struct {
	key       string
	result    Result
	expiresAt time.Time
}

// CacheStats 统计缓存运行时的命中率与淘汰数。
type CacheStats struct {
	Hits      uint64  `json:"hits"`
	Misses    uint64  `json:"misses"`
	Evictions uint64  `json:"evictions"`
	HitRate   float64 `json:"hit_rate"`
	Size      int     `json:"size"`
	Capacity  int     `json:"capacity"`
}

type decisionCache struct {
	mu        sync.Mutex
	capacity  int
	ttl       time.Duration
	now       func() time.Time
	jitter    func() float64
	items     map[string]*list.Element
	evictList *list.List
	hits      uint64
	misses    uint64
	evictions uint64
}

func newCache(capacity int, ttl time.Duration, now func() time.Time, jitter func() float64) *decisionCache {
	if capacity <= 0 {
		capacity = 5000
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if now == nil {
		now = time.Now
	}
	if jitter == nil {
		jitter = func() float64 {
			// 返回 [-1, 1] 之间的浮点随机数
			return rand.Float64()*2 - 1
		}
	}
	return &decisionCache{
		capacity:  capacity,
		ttl:       ttl,
		now:       now,
		jitter:    jitter,
		items:     make(map[string]*list.Element),
		evictList: list.New(),
	}
}

func (c *decisionCache) get(key string) (Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		c.misses++
		return Result{}, false
	}

	entry := elem.Value.(*cacheEntry)
	if c.now().After(entry.expiresAt) {
		c.evictElement(elem)
		c.misses++
		return Result{}, false
	}

	c.hits++
	c.evictList.MoveToFront(elem)
	return cloneResult(entry.result), true
}

func (c *decisionCache) put(key string, result Result) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 计算 jitter 后的有效 TTL: [0.9, 1.1] * ttl
	j := c.jitter()
	if j < -1 {
		j = -1
	} else if j > 1 {
		j = 1
	}
	actualTTL := time.Duration(float64(c.ttl) * (1.0 + 0.1*j))
	expiresAt := c.now().Add(actualTTL)

	if elem, ok := c.items[key]; ok {
		c.evictList.MoveToFront(elem)
		entry := elem.Value.(*cacheEntry)
		entry.result = cloneResult(result)
		entry.expiresAt = expiresAt
		return
	}

	if c.evictList.Len() >= c.capacity {
		c.evictOldest()
	}

	entry := &cacheEntry{
		key:       key,
		result:    cloneResult(result),
		expiresAt: expiresAt,
	}
	elem := c.evictList.PushFront(entry)
	c.items[key] = elem
}

func (c *decisionCache) evictOldest() {
	elem := c.evictList.Back()
	if elem != nil {
		c.evictElement(elem)
	}
}

func (c *decisionCache) evictElement(elem *list.Element) {
	c.evictList.Remove(elem)
	entry := elem.Value.(*cacheEntry)
	delete(c.items, entry.key)
	c.evictions++
}

func (c *decisionCache) stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()

	total := c.hits + c.misses
	rate := 0.0
	if total > 0 {
		rate = float64(c.hits) / float64(total) * 100
	}
	return CacheStats{
		Hits:      c.hits,
		Misses:    c.misses,
		Evictions: c.evictions,
		HitRate:   rate,
		Size:      c.evictList.Len(),
		Capacity:  c.capacity,
	}
}

func cloneResult(src Result) Result {
	dst := Result{
		Model: src.Model,
		Usage: src.Usage,
	}
	if src.Answers != nil {
		dst.Answers = make(map[string]Answer, len(src.Answers))
		for k, v := range src.Answers {
			ans := Answer{
				Type: v.Type,
			}
			if v.Noul != nil {
				val := *v.Noul
				ans.Noul = &val
			}
			if v.Choice != nil {
				val := *v.Choice
				ans.Choice = &val
			}
			if v.Score != nil {
				val := *v.Score
				ans.Score = &val
			}
			if v.Confidence != nil {
				val := *v.Confidence
				ans.Confidence = &val
			}
			if v.Legend != nil {
				ans.Legend = make(map[string]string, len(v.Legend))
				for lk, lv := range v.Legend {
					ans.Legend[lk] = lv
				}
			}
			if v.Probabilities != nil {
				ans.Probabilities = make(map[string]float64, len(v.Probabilities))
				for pk, pv := range v.Probabilities {
					ans.Probabilities[pk] = pv
				}
			}
			dst.Answers[k] = ans
		}
	}
	return dst
}
