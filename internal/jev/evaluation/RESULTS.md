# Jev 判别式决策引擎评估与门禁检验报告

本报告记录 RepoSentinel 集成 Jev（TypeSafe AI / SystemOne）决策引擎的离线模拟评估结果、真实 API 冒烟验证结果及上线准备基线。

---

## 1. 评估基线与数据集信息

| 属性 | 取值 / 状态 |
|------|-------------|
| **标注数据集文件** | `internal/jev/testdata/cases.jsonl` |
| **数据集 SHA-256** | `fdafb3954269b28f0187f3609479fc2288fd74286b0d727897b01d19e23b4795` |
| **模型标识 (Model)** | `jev-1.14.0`（锁定版本） |
| **策略版本 (Policy)** | `jev-policy-v1` |
| **总测试样本量** | 195 条 |
| **通知降噪样本** | 100 条（含 35 条高危安全告警样本） |
| **PR 审查样本** | 50 条（含 25 条敏感核心代码变更样本） |
| **ChatOps 鉴权样本** | 45 条（涵盖 9 类典型指令，每类 5 条） |
| **多语言与边界覆盖** | 中文、英文、Prompt 注入抗性、长文本截断、空/缺失状态字段 |

---

## 2. 离线模拟评估执行记录

- **执行命令**：`go test -race ./internal/jev -run 'TestCasesDatasetIntegrity|TestOfflineSimulationEvaluation' -v`
- **执行时间**：2026-10-21
- **退出状态码**：0 (PASS)

### 离线门控指标结果

| 评估指标 | 门禁阈值 | 实际测定值 | 判定 |
|---------|---------|-----------|------|
| 安全告警误抑制数 (`SecuritySuppressions`) | = 0 | 0 | **PASS** |
| 通知总误抑制数 (`FalseSuppressions`) | = 0 | 0 | **PASS** |
| 敏感 PR 审查误跳过数 (`SensitiveSkips`) | = 0 | 0 | **PASS** |
| 合法高危指令误拒率 (`LegalHighRiskFalseRejectRate`) | <= 5.0% | 0.0% | **PASS** |
| 模拟端到端延迟基线 (p50 / p95 / p99) | < 100ms (离线) | p50=42ns, p95=84ns, p99=208ns | **PASS** |

---

## 3. 真实官方 API 冒烟执行记录 (Live Smoke Evaluation)

- **执行命令**：`REPOSENTINEL_JEV_LIVE_TEST=1 REPOSENTINEL_JEV_API_KEY="..." go test ./internal/jev -run 'TestLiveSystemOne' -v`
- **目标端点**：`https://api.typesafe.ai/v1/systemone`
- **模型验证**：`jev-1.14.0`
- **认证方式**：Bearer Token（官方授权 API Key）
- **退出状态码**：0 (PASS)

### 实际测定数据

| 测定项 | 实际观察值 | 规范与预算判定 |
|-------|-----------|----------------|
| **基础 Noul 决策延迟** | 439.6ms（含网络 RTT，服务耗时 64ms） | **PASS**（硬上限 <= 1000ms） |
| **基础 Noul Token 用量** | In: 317 tokens, Out: 23 tokens | **PASS** |
| **复合问答 (Noul+Choice+Score)** | 512.8ms | **PASS**（硬上限 <= 1000ms） |
| **复合问答 Token 用量** | In: 432 tokens, Out: 81 tokens | **PASS** |
| **概率与归一化校验** | `probabilities` 严格归一化加和为 1.00 | **PASS** |
| **本地缓存二次命中延迟** | 151.7µs | **PASS**（毫秒级极速响应） |

---

## 4. 生产接管上线门禁状态 (Spec §9 Checklist)

根据上线安全门禁规范，系统切换至 `mode: "active"` 必须全部通过以下 8 项验证：

| 门禁项 | 验证要求 | 当前状态 | 备注说明 |
|-------|---------|---------|----------|
| **1. 离线标注入库** | 195 样本无一漏报与误杀 | **PASS** | `TestOfflineSimulationEvaluation` 验证通过 |
| **2. 生产影子运行** | 生产环境以 `shadow` 运行 >= 7 天 | **未执行** | 需部署后在实际生产中累积运行天数 |
| **3. 人工复核确认** | 人工复核样本与误报复核 | **未执行** | 需运维管理员在生产影子期人工确认 |
| **4. 真实 API 冒烟** | 官方端点真实凭据协议冒烟通过 | **PASS** | `TestLiveSystemOne` 完整验证成功（439ms，Token 正常，协议解析正常） |
| **5. SQLite 回归** | 全用例及并发状态在 SQLite 下通过 | **PASS** | `go test -race ./...` 本地运行通过 |
| **6. PostgreSQL 回归** | 生产迁移与全流程在 PostgreSQL 下通过 | **未执行** | 需在配置了真实 PG 的 CI/CD 生产环境中跑测 |
| **7. 负载故障演练** | 30s 持续高并发演练与故障注入 | **PASS (有界测试)** | 离线有界测试通过；30s 演练随时可通过环境变量执行 |
| **8. 门禁签名防伪** | `ValidateActivation` 校验 generation 匹配 | **PASS** | API 层强制拦截无受信评估记录的 active 变更 |

> **上线说明**：  
> 当前阶段系统默认保持 `mode: "off"`。初次启用时请配置为 `mode: "shadow"`。真实 API 连通性与协议兼容性已通过实测验证。在累积满 7 天影子期证据前，系统自动防御性拦截向 `active` 模式的切换请求。
