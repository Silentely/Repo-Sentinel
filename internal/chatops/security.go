package chatops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

var (
	ErrSettingsUnavailable  = errors.New("chatops: settings store unavailable")
	ErrSecurityCorrupted    = errors.New("chatops: security config corrupted")
	ErrOperatorUnauthorized = errors.New("chatops: operator not in allowed whitelist")
	ErrActorMismatch        = errors.New("chatops: operator actor mismatch")
)

// ReadSecureConfig 读取并反序列化安全配置项。
// 严守 fail-closed：遇到 null、缺失、损坏或空白字符串时明确返回错误；仅在 key 不存在时返回空串和 nil。
func ReadSecureConfig(ctx context.Context, st store.SettingsStore, key string) (string, error) {
	if st == nil {
		return "", nil
	}
	setting, err := st.Get(ctx, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("read security config failed: %w", err)
	}
	trimmedRaw := bytes.TrimSpace(setting.ValueJSON)
	if len(trimmedRaw) == 0 || bytes.Equal(trimmedRaw, []byte("null")) {
		return "", fmt.Errorf("security config %q is empty or null", key)
	}
	var val string
	if err := json.Unmarshal(setting.ValueJSON, &val); err != nil {
		return "", fmt.Errorf("security config %q corrupted: %w", key, err)
	}
	val = strings.TrimSpace(val)
	if val == "" {
		return "", fmt.Errorf("security config %q is blank string", key)
	}
	return val, nil
}

// VerifyOperatorWhitelist 检查操作人员是否属于已授权的单兵白名单。
// 白名单未配置时放行；配置非空时，operatorIDs 中任一 ID 匹配即放行。
func VerifyOperatorWhitelist(ctx context.Context, st store.SettingsStore, platformKey string, operatorIDs ...string) (bool, error) {
	allowedStr, err := ReadSecureConfig(ctx, st, platformKey)
	if err != nil {
		return false, err
	}
	if allowedStr == "" {
		return true, nil
	}

	allowedMap := make(map[string]bool)
	for _, uid := range strings.Split(allowedStr, ",") {
		clean := strings.TrimSpace(uid)
		if clean != "" {
			allowedMap[clean] = true
		}
	}

	for _, op := range operatorIDs {
		cleanOp := strings.TrimSpace(op)
		if cleanOp != "" && allowedMap[cleanOp] {
			return true, nil
		}
	}
	return false, nil
}

// VerifyActorBinding 校验 Token 签发时锁定的 ActorID。
// 若 Action 未指定 ActorID（即为空），则单兵白名单通过即视为合法；
// 若指定了 ActorID，则必须与传入的操作者身份之一匹配。
func VerifyActorBinding(action *ActionData, operatorIDs ...string) bool {
	if action == nil {
		return false
	}
	if action.ActorID == "" {
		return true
	}
	for _, op := range operatorIDs {
		if strings.TrimSpace(op) == action.ActorID {
			return true
		}
	}
	return false
}

// MaskToken 安全截断掩码敏感令牌，仅用于日志留痕。
func MaskToken(token string) string {
	if len(token) <= 6 {
		return "***"
	}
	return token[:6] + "..."
}
