package normalizer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// Fingerprint 生成业务事件去重指纹。
// 通过向哈希器流式写入字段，消除 strings.Join 与 []byte 复制的多次堆分配。
func Fingerprint(source, repo, resourceKind, resourceID, action string, sourceUpdatedAt time.Time, stateHash string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(source))
	_, _ = h.Write([]byte{'|'})
	_, _ = h.Write([]byte(repo))
	_, _ = h.Write([]byte{'|'})
	_, _ = h.Write([]byte(resourceKind))
	_, _ = h.Write([]byte{'|'})
	_, _ = h.Write([]byte(resourceID))
	_, _ = h.Write([]byte{'|'})
	_, _ = h.Write([]byte(action))
	_, _ = h.Write([]byte{'|'})
	var timeBuf [32]byte
	_, _ = h.Write(sourceUpdatedAt.UTC().AppendFormat(timeBuf[:0], time.RFC3339))
	_, _ = h.Write([]byte{'|'})
	_, _ = h.Write([]byte(stateHash))

	var sum [sha256.Size]byte
	h.Sum(sum[:0])
	return hex.EncodeToString(sum[:])
}

// StateHash 对关键状态字段做稳定哈希。
// 流式写入各字段，避免堆分配字符串中间态。
func StateHash(parts ...string) string {
	h := sha256.New()
	for i, part := range parts {
		if i > 0 {
			_, _ = h.Write([]byte{'|'})
		}
		_, _ = h.Write([]byte(part))
	}
	var sum [sha256.Size]byte
	h.Sum(sum[:0])
	return hex.EncodeToString(sum[:16])
}

// ResourceIdentity 生成资源标识字符串。number 采用 int64 避免 32 位系统截断大 ID（如 Release ID）。
func ResourceIdentity(kind string, number int64, runID int64) string {
	if runID != 0 {
		return fmt.Sprintf("run:%d", runID)
	}
	return fmt.Sprintf("%s:%d", kind, number)
}
