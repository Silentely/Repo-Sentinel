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
在管理面板「Jev 判别引擎」或配置文件中填入 Endpoint、API Key 及模型，模式设为 `shadow`：
- `mode: "shadow"`
- `noise_filter_enabled: true`
- `cascade_gate_enabled: true`
- `chatops_guardrail_enabled: true`

系统开始收集 `reposentinel_jev_would_suppress_total` 和 `reposentinel_jev_would_skip_total` 观测指标。

### 步骤二：生产影子观测期（>= 7 天）
1. 观察 Grafana / Prometheus 监控中 Jev 决策分布、延迟 (p95 < 500ms) 及断路器状态。
2. 管理员在后台定期抽样检查跳过的 PR 与被标记抑制的通知，确认无漏报和误杀。

### 步骤三：执行上线门禁验证
在将模式切换为 `active` 之前，必须通过本地测试套件完成基线验证，将经过审计签名的评估记录写入 `system_settings` 的 `jev.evaluation.v1`：
- **标注数据集校验**：195 条金标样本测试全通过，误抑制率 = 0，敏感 PR 误跳过 = 0，合法高危动作误拒率 <= 5%。
- **真实 API 冒烟**：使用 `REPOSENTINEL_JEV_LIVE_EVAL=1` 验证真实 API 凭据及协议兼容性。
- **负载演练**：通过 `REPOSENTINEL_JEV_LOAD_TEST=1` 验证 30 秒高并发注入下断路器熔断与降级正常。

### 步骤四：渐进式激活 (Active)
在管理后台将模式变更为 `active`。若门禁未全部满足，API 将拒绝变更并返回 `activation_evidence_missing`。

---

## 3. 密钥管理与轮换

Jev API Key 使用系统主密钥进行 AES-GCM 封装加密存储（绑定 AAD `reposentinel:jev_api_key`）：
- **密钥轮换**：执行 CLI 轮换命令：
  ```bash
  reposentinel secret rotate-keys --config configs/reposentinel.yaml
  ```
  该命令会自动重新加密 Jev API Key 密文并更新其 `credential_generation`。
- **缓存失效机制**：决策缓存 Key 绑定了 `CredentialGeneration` 与 `RuntimeGeneration`。一旦轮换密钥或更新配置，旧凭据下的所有缓存即刻逻辑失效，防止凭据撤销后依然复用旧决策。

---

## 4. 故障隔离与断路器机制

### 限流与自适应退避 (429 / 529)
当 Jev 上游返回 HTTP 429 或 529 时：
1. 客户端解析 `Retry-After` 头（支持秒数或 HTTP-Date）。
2. 在退避时间窗口内，后续请求立即短路返回 `ErrRateLimited`，平滑回退到传统规则或全量审查，绝不重试堆积。

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
- **降级行为**：在降级窗口内，决策引擎表现如同 `shadow` 模式，所有决策面安全旁路，保障主干业务绝对不受阻。
- **自动恢复**：达到指定截止时间后，系统自动退出降级恢复 `active` 状态，无需人工二次操作；系统重启也会自动继承有效截止时间。

---

## 6. 特殊场景操作指南

### ChatOps 临时确认 Token 清理失败
- ChatOps 敏感操作确认采用零缓存单次 Token 机制。
- 若因网络抖动导致操作后清理 Token 失败，该 Token 将在到期（默认 5 分钟）后由系统 GC 自动作废；运维人员亦可在管理后台强制废弃。

### 强制深度审查 (`force_deep_review`)
- 对于被 Jev 级联门控判为跳过的 Pull Request，开发或评审人员可在 PR 详情卡片中点击「⚡ 强制深度审查」。
- 强制审查会绕过 Jev 判别结果，直接调度配置的大模型执行逐行深度审查。

### 总开关彻底关闭与平滑回退
若需彻底停用 Jev：
1. 在管理后台将模式设置为 `off`。
2. 现有在途请求将收到 context 取消并安全退出。
3. 数据库保留配置字段，所有通知和审查流程无缝回退至纯规则引擎模式。
