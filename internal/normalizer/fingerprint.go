package normalizer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Fingerprint 生成业务事件去重指纹。
func Fingerprint(source, repo, resourceKind, resourceID, action string, sourceUpdatedAt time.Time, stateHash string) string {
	raw := strings.Join([]string{
		source,
		repo,
		resourceKind,
		resourceID,
		action,
		sourceUpdatedAt.UTC().Format(time.RFC3339),
		stateHash,
	}, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// StateHash 对关键状态字段做稳定哈希。
func StateHash(parts ...string) string {
	raw := strings.Join(parts, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
}

// ResourceIdentity 生成资源标识字符串。number 采用 int64 避免 32 位系统截断大 ID（如 Release ID）。
func ResourceIdentity(kind string, number int64, runID int64) string {
	if runID != 0 {
		return fmt.Sprintf("run:%d", runID)
	}
	return fmt.Sprintf("%s:%d", kind, number)
}
