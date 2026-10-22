# Jev 判别式决策引擎运维手册

本文档介绍 Jev（基于 TypeSafe AI 架构的判别式决策引擎）在 RepoSentinel 中的接入流程、安全门禁、运维操作与故障应急机制。

---

## 1. 架构定位与安全基线

Jev 在系统中承担三个业务面的轻量判别式决策（YES/NO/CHOICE/SCORE），不生成自由文本：
- **通知降噪面 (`notification`)**：过滤机器人风暴、重复 ping 与琐碎变更，保护高危安全告警穿透。
- **PR 审查门控面 (`review`)**：识别纯文档/琐碎依赖变更实行安全跳过，拦截高风险代码执行全量深度审查。
- **ChatOps 鉴权决策面 (`chatops`)**：判定斜杠指令的操作语义风险，严格隔离单飞并执行零缓存。

### 核心安全原则
1. **默认关闭**：全新安装或升级后，Jev 默认处于禁用状态 (`enabled: false`)。
2. **影子观察先行**：开启后必须首先在 `mode: "shadow"` 模式下运行至少 7 天，仅记录决策比对与指标，不影响实际业务走向。
3. **严格上线门禁**：从 `shadow` 切换到 `active` 生产接管，必须满足离线标注集评估、生产真实 API 冒烟、双数据库回归以及负载故障注入的硬性门禁（系统将在 API 层自动校验 `jev.evaluation.v1` 记录）。
4. **隐私与合规**：发往 Jev 的 State 经严格白名单清洗与脱敏，体积限制在 8KiB 以内；对外发送私有仓元数据前，需管理员确认第三方服务协议与数据留存条款（系统不虚假宣称具备零数据保留 ZDR 或 DPA 资质）。

---

## 2. 生产上线生命周期

```
┌──────────────┐     配置并启用     ┌──────────────┐    满 7 天且人工复核    ┌──────────────┐
│ enabled: false│ ─────────────────> │ mode: shadow │ ─────────────────────> │ 离线与真实门禁 │
└──────────────┘                    └──────────────┘                        └──────┬───────┘
                                                                                   │
                                    ┌──────────────┐       逐能力开启生效           │
                                    │ mode: active │ <─────────────────────────────┘
                                    └──────────────┘
```

### 步骤一：初始配置（Shadow 模式）
在管理面板 **Jev 判别引擎** 或配置文件的 `jev` 段中填入 Base URL、API Key 及模型，模式设为 `shadow`。Base URL 使用 HTTPS 主机地址（默认 `https://api.typesafe.ai`），不含路径、查询参数或凭据。完整变量见 [配置参考](/reference/configuration#jev-判别式决策-可选)。
- `enabled: true`
- `mode: "shadow"`
- `noise_filter_enabled: true`
- `cascade_gate_enabled: true`
- `chatops_guardrail_enabled: true`

系统开始收集 `reposentinel_jev_would_suppress_total` 和 `reposentinel_jev_would_skip_total` 观测指标。

### 步骤二：生产影子观测期（>= 7 天）
1. 观察 Grafana / Prometheus 监控中 Jev 决策分布、延迟 (p95 < 500ms) 及断路器状态。
2. 管理员在后台定期抽样检查跳过的 PR 与被标记抑制的通知，确认无漏报和误杀。

### 步骤三：执行上线门禁验证
在将模式切换为 `active` 之前，必须通过本地测试套件完成基线验证，由维护者审核评估结果并将摘要写入 `system_settings` 的 `jev.evaluation.v1`：
- **标注数据集校验**：195 条金标样本测试全通过，误抑制率 = 0，敏感 PR 误跳过 = 0，合法高危动作误拒率 <= 5%。
- **真实 API 冒烟**：设置 `REPOSENTINEL_JEV_LIVE_TEST=1` 与 `REPOSENTINEL_JEV_API_KEY`，验证真实 API 凭据及协议兼容性。缺少任一条件时测试会跳过，不能视为通过。
- **负载演练**：有界测试覆盖并发限制、500 故障熔断与决策面隔离；设置 `REPOSENTINEL_JEV_LOAD_TEST=1` 可追加 30 秒并发循环。该循环复用相同状态并可命中缓存，不能视为持续远程故障注入的证据。

在仓库根目录执行（真实冒烟前先设置 API Key 环境变量）：

```bash
go test -race ./internal/jev -run 'TestCasesDatasetIntegrity|TestOfflineSimulationEvaluation' -v
REPOSENTINEL_JEV_LIVE_TEST=1 go test ./internal/jev -run TestLiveSystemOne -v
REPOSENTINEL_JEV_LOAD_TEST=1 go test -race ./internal/jev -run TestLoadAndFaultInjection_Bounded -v
```

这些测试不会自动写入 `jev.evaluation.v1`。当前 API 校验摘要中的字段和门槛，不验证密码学签名；评估证据的审核与入库流程需由维护者确认。离线模拟结果不能替代生产影子期、真实 API 或双数据库回归证据。

### 步骤四：渐进式激活 (Active)
在管理后台将模式变更为 `active`。API 拒绝变更时，错误码区分为：`activation_evidence_missing`（缺少记录）、`activation_evidence_invalid`（记录无法解析）、`activation_evidence_mismatch`（模型不匹配）或 `activation_gate_failed`（门槛未满足）。

---

## 3. 密钥管理与轮换

Jev API Key 使用系统主密钥进行 AES-GCM 封装加密存储（绑定 AAD `reposentinel:jev_api_key`）：
- **密钥轮换**：执行 CLI 轮换命令：
  ```bash
  reposentinel secret rotate-keys --config configs/reposentinel.yaml
  ```
  按 [主密钥轮换流程](/reference/ops#主密钥轮换) 先停止服务，并设置当前与上一把主密钥，再执行该命令。它会重新加密数据库中的 Jev API Key 信封，不会更换上游 API Key，也不会更新持久化的 `credential_generation`。
- **缓存失效机制**：决策缓存为进程内缓存；重启后为空。通过管理后台保存配置时会重建客户端及缓存。CLI 重加密数据库不会清除仍在运行的服务进程中的缓存。

---

## 4. 故障隔离与断路器机制

### 限流与自适应退避 (429 / 529)
当 Jev 上游返回 HTTP 429 或 529 时：
1. 客户端解析 `Retry-After` 头（支持秒数或 HTTP-Date）。
2. 在退避时间窗口内，后续需要远程判定的请求快速失败。当前请求可在剩余预算允许时退避并重试一次（`retries: 1`，默认值）；预算不足或禁用重试时返回 `ErrRateLimited`。
3. `Retry-After` 达到 15 秒时同时触发断路器，冷却期采用该退避时长。通知回退到静态规则，PR 门控回退到深度审查；ChatOps 高危操作在 `active` 模式下需明确放行，否则拒绝执行。

### 断路器熔断与自愈 (5xx & 网络中断)
1. **熔断阈值**：任一决策面在连续遭遇 5 次上游真正失败（5xx、网络中断、畸形响应）后，断路器进入 `Open` 状态。
2. **快速降级**：`Open` 期间请求瞬间短路并计入 `reposentinel_jev_fallbacks_total`。
3. **半开探测**：30 秒冷却后，断路器进入 `Half-Open` 状态，仅放行 1 个探测请求；探测成功即恢复 `Closed`，探测失败则重置 30 秒冷却。
4. **决策面隔离**：`notification`、`review` 与 `chatops` 拥有相互独立的断路器和并发槽位。例如 review 面触发熔断不会影响 notification 通知的准时投递。

---

## 5. 应急临时降级 (Emergency Advisory)

当 Jev 出现未预期的上游异常或决策偏差，而运维人员需要快速临时旁路时，可通过管理后台启用应急顾问降级：
- **最大有效期**：单次最多 15 分钟（`now < until <= now + 15m`）。
- **强制填写原因**：提交必须附带非空操作原因（`emergency_advisory_reason`），系统全程计入安全审计日志。
- **降级行为**：降级窗口仅影响 ChatOps 护栏：Jev 判定改为仅记录、不拦截，原有鉴权与单次令牌检查仍生效。通知降噪与 PR 审查门控不受该窗口影响；如需三个决策面均只观察，请将模式改为 `shadow`。
- **自动恢复**：截止后，ChatOps 按当前配置模式恢复护栏判定；系统重启会继承尚未过期的截止时间。

---

## 6. 特殊场景操作指南

### ChatOps 操作令牌释放失败
- ChatOps 使用持久化的单次操作令牌；Jev 护栏请求不缓存、不合并。
- 护栏不可用时，系统尝试释放已领取的令牌以允许重试。若释放失败，该令牌仍保持已消费状态，应重新生成操作。令牌在消费时检查到期时间；未指定 TTL 时默认为 15 分钟。当前未提供管理后台强制废弃入口。

### 强制深度审查 (`force_deep_review`)
- 对于被 Jev 级联门控判为跳过的 Pull Request，开发或评审人员可在 PR 详情卡片中点击「⚡ 强制深度审查」。
- 强制审查会绕过 Jev 判别结果，直接调度配置的大模型执行逐行深度审查。

### 总开关彻底关闭与平滑回退
若需彻底停用 Jev：
1. 在管理后台关闭 **启用 Jev 决策引擎**（`enabled: false`）；`mode` 仅支持 `shadow` / `active`，不接受 `off`。
2. 保存后，新请求不再使用 Jev；已有在途请求仍按原上下文和超时预算结束。
3. 数据库保留配置字段，通知使用静态规则，PR 恢复常规深度审查流程，ChatOps 保留原有鉴权与令牌检查。
