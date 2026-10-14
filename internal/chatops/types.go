package chatops

import (
	"time"
)

// ActionData 承载交互式操作按钮绑定的载荷数据。
// 与存量 ChatOpsActionData 的序列化标签和字段类型保持 100% 字节级兼容。
type ActionData struct {
	ID          string    `json:"id"`
	Action      string    `json:"action"` // e.g. "workflow_rerun", "issue_reply_draft", "issue_mark_spam_and_close"
	RepoID      string    `json:"repo_id"`
	RunID       string    `json:"run_id,omitempty"`
	IssueNumber int       `json:"issue_number,omitempty"`
	Payload     string    `json:"payload,omitempty"`
	ActorID     string    `json:"actor_id,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
	Consumed    bool      `json:"consumed,omitempty"`
	ConsumedAt  time.Time `json:"consumed_at,omitempty"`
}

// ActionButton 定义外发消息通知卡片绑定的操作按钮契约。
// 与 notification_outbox.body_json 中 chatops_actions 的字段保持一致。
type ActionButton struct {
	Text   string `json:"text"`
	Action string `json:"action"`
	Token  string `json:"token"`
}

// Status 表示 ChatOps 操作处理的领域状态枚举。
type Status string

const (
	// StatusExecuted 表示外部实体动作（如 GitHub Issue/PR/Workflow）已成功执行并留存回执。
	StatusExecuted Status = "executed"
	// StatusAlreadyDone 表示动作此前已执行或存在有效收据，幂等跳过。
	StatusAlreadyDone Status = "already_done"
	// StatusPending 表示该动作已有并发正在执行中。
	StatusPending Status = "pending"
	// StatusIndeterminate 表示网络在途不确定（超时/EOF等），远端可能已生效，严格禁止释放 Token。
	StatusIndeterminate Status = "indeterminate"
	// StatusRetryableFailure 表示在未发生外部网络副作用或远端明确拒绝且允许安全重试的失败，可释放 Token。
	StatusRetryableFailure Status = "retryable_failure"
	// StatusForbidden 表示单兵运维白名单未命中或 ActorID 绑定不符。
	StatusForbidden Status = "forbidden"
	// StatusInvalidToken 表示 Token 已被消费、过期或格式损坏。
	StatusInvalidToken Status = "invalid_token"
	// StatusCorruptedConfig 表示底层安全配置异常（fail-closed 触发）。
	StatusCorruptedConfig Status = "corrupted_config"
)

// Outcome 结构体表示分发处理完成后的强类型领域结果，不包含任何 HTTP 协议语义。
type Outcome struct {
	Status     Status
	Action     string
	Message    string // 面向运维人员的本地化提示信息
	ExternalID string
	Err        error
}

func (o Outcome) CanReleaseToken() bool {
	return o.Status == StatusRetryableFailure
}
