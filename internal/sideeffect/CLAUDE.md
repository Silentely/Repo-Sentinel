# sideeffect

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **sideeffect**

## 模块职责

外部副作用显式生命周期回执原语包：提供跨模块共享的幂等防护函数 `ExecuteWithReceipt`、`ExecuteWithReceiptDetail`、收据数据结构、租约抢占与网络不确定性判定。

作为架构底层的无状态叶子包，被 `internal/rules`（Issue 自动打标）与 `internal/chatops`（复合运维动作）共同引用，彻底避免循环依赖。

## 核心接口与函数

- `ExecuteWithReceipt(ctx, settingsStore, effectType, repoID, subjectNumber, revision, target, fn) error`
- `ExecuteWithReceiptDetail(ctx, settingsStore, effectType, repoID, subjectNumber, revision, target, fn) (*ExecutionDetail, error)`
- `SideEffectReceiptKey(effectType, repoID, subjectNumber, revision, target) string`
- `IsNetworkUnknownError(err) bool`
- `MarkPreSendError(err) error` / `IsPreSendError(err) bool`

## 回执状态与保守阻断语义

- `ReceiptPending`：已抢占租约，外部网络调用在途中。遇到已存在的 pending 回执实行严格保守阻断，杜绝无条件自动删除或重新调用 `fn()` 引发重复外部副作用。
- `ReceiptSucceeded`：外部副作用已确认成功，写入收据持久化记录；后续相同 Key 调用直接幂等跳过。
- `ReceiptUnknown`：网络在途不确定（超时、连接重置、Broken pipe、EOF 或终态回执写入失败等），回执保留为 unknown 状态，严禁重试二次外发。
