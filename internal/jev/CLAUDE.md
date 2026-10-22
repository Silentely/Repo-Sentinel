# internal/jev 架构设计与开发规范

`internal/jev` 封装了 TypeSafe AI（SystemOne）判别式决策引擎客户端。提供轻量级 YES/NO/CHOICE/SCORE 判别式决策能力，服务于通知降噪、PR 审查门控及 ChatOps 指令安全护栏。

## 1. 架构定位与单向依赖铁律

```
  internal/rules (通知降噪)   internal/webhooksvc / ai (PR门控)   internal/chatops (指令护栏)
              │                               │                              │
              └───────────────────────┬───────┴──────────────────────────────┘
                                      ▼
                                internal/jev (Client / Breaker / Cache / Runtime)
```

- **单向无环依赖**：上层业务包（`rules`、`webhooksvc`、`ai`、`chatops`、`httpapi`）单向依赖 `internal/jev`。
- **严禁反向依赖**：`internal/jev` 严禁引入任何上层业务包（不得 import `ai`、`rules`、`webhooksvc`、`chatops`）。所有传递给 Jev 的业务上下文必须解耦为通用的 State 原始结构或标准 JSON。

## 2. 决策面特性与隔离原则

系统分为三个独立的决策面（Plane）：
1. **notification**（通知）：允许缓存（TTL 10m），允许 singleflight。
2. **review**（PR 代码审查）：允许缓存（TTL 10m），允许 singleflight。
3. **chatops**（指令鉴权）：**严禁缓存，严禁 singleflight**。每次请求独立签发、即时鉴权。

### 隔离保证
- 各 Plane 拥有独立的熔断器（`circuitBreaker`）实例与并发信号量（`semaphore`）。
- 某一决策面的上游连续故障（如 review 面熔断）绝不影响其他决策面（如 notification）的正常判别。

## 3. 并发与硬限制基线

- **State 大小限制**：序列化后体积严格 `<= 8KiB`，超限立即返回 `ErrStateTooLarge` 并中断，防止 prompt 污染或资源耗尽。
- **超时与预算**：各 Plane 预算硬上限为 `min(ctx.Deadline, Timeout, 60s)`。单次决策网络请求超时不得超过 60s。
- **最大并发度**：每个 Plane 独立配置信号量槽位，默认 8，硬上限为 32。
- **退避与熔断**：
  - 遇到 429/529 遵循 `Retry-After` 头自适应退避，退避窗口内直接快速失败（Fast Fallback）。
  - 连续 5 次 5xx 故障触发熔断，冷却期为 30 秒，冷却后单请求探针进入半开（Half-Open）状态。

## 4. 缓存与失效机制

- 采用 LRU 缓存，键值基于 `CanonicalKey`：包含 Endpoint、Model、PolicyVersion、CredentialGeneration、RuntimeGeneration、Plane、State、Questions 经统一标准化排序后的 SHA256。
- 管理后台保存配置会重建客户端与缓存；进程重启后缓存为空。CLI 主密钥轮换仅重加密数据库中的 API Key 信封，不更新持久化的 credential generation，也不清除其他运行中进程的缓存。
