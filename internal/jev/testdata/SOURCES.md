# Jev Contract Test Fixture Sources

- **抓取日期**：2026-10-20
- **官方参考文档**：
  - [TypeSafe API Reference](https://docs.typesafe.ai/api)
  - [TypeSafe Models](https://docs.typesafe.ai/models)
  - [TypeSafe llms.txt](https://docs.typesafe.ai/llms.txt)

- **固定版本策略理由**：
  - 本项目显式锁定模型版本为 `jev-1.14.0`。
  - 虽然官方推荐别名 `jev-latest`，但别名随官方发布浮动可能导致阈值判决漂移。锁定具体发布版本使得阈值评估具备确定性与可复现性。
  - 升级模型必须经由 runtime generation 递增并重新执行影子与故障评估。

- **本地样例身份声明**：
  - `systemone.request.json` 与 `systemone.response.json` 为严格依照官方协议规范构造的本地合成测试样例（Synthetic Test Fixtures），非生产在线真实请求录制。
  - 测试样例中未包含任何真实 API 密钥、私有仓库代码、真实 Token 或未脱敏的用户身份标识。
  - 后续针对在线服务的 Opt-in 冒烟与录制测试将单独建立独立用例，并在归档前剥离凭据与真实 state。
