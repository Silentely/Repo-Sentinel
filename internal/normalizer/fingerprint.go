package normalizer

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strconv"
	"time"
)

// Fingerprint 生成业务事件去重指纹。
// 通过向哈希器流式写入字段并采用定长栈缓冲区进行十六进制编码，消除 strings.Join、[]byte 复制与 hex 编码的堆分配。
func Fingerprint(source, repo, resourceKind, resourceID, action string, sourceUpdatedAt time.Time, stateHash string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, source)
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, repo)
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, resourceKind)
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, resourceID)
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, action)
	_, _ = io.WriteString(h, "|")
	var timeBuf [32]byte
	_, _ = h.Write(sourceUpdatedAt.UTC().AppendFormat(timeBuf[:0], time.RFC3339))
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, stateHash)

	var sum [sha256.Size]byte
	h.Sum(sum[:0])
	var buf [sha256.Size * 2]byte
	hex.Encode(buf[:], sum[:])
	return string(buf[:])
}

// StateHash 对关键状态字段做稳定哈希。
// 流式写入各字段，避免堆分配字符串中间态与切片副本。
func StateHash(parts ...string) string {
	h := sha256.New()
	for i, part := range parts {
		if i > 0 {
			_, _ = io.WriteString(h, "|")
		}
		_, _ = io.WriteString(h, part)
	}
	var sum [sha256.Size]byte
	h.Sum(sum[:0])
	var buf [32]byte
	hex.Encode(buf[:], sum[:16])
	return string(buf[:])
}

// ResourceIdentity 生成资源标识字符串。number 采用 int64 避免 32 位系统截断大 ID（如 Release ID）。
// 采用 strconv 消除 fmt.Sprintf 的反射与装箱开销。
func ResourceIdentity(kind string, number int64, runID int64) string {
	if runID != 0 {
		return "run:" + strconv.FormatInt(runID, 10)
	}
	return kind + ":" + strconv.FormatInt(number, 10)
}
