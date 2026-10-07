# BT-V5-AGE-010 能力审计

2026-10-03。来源 `docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md` 原 AGE-010（长期无 reinforcement 的 inferred confidence 降低，显式默认不自动 decay），唯一 live 状态与 lease 由根代理维护。

| 核查项 | 实际结果 | 本轮处理 |
| --- | --- | --- |
| 原生 Memory/主体/版本/有效期 | 已实现 056，严格本人绑定 | REUSE |
| confidence 语义 | AGE008 已有直接声明/未校准描述；Reader 拒绝 inferred | REUSE，不修改旧 reader |
| reinforcement | AGE009 是 EXPLICIT 来源支持 ledger，非 inferred 时间强化 | REUSE；不把 last_support_at 当 last_reinforced_at |
| 时间衰减 | 未实现 | 新 effective metadata projection |
| inferred producer / activation | 当前未启用，人工 API 拒绝，存储 pending 预留 | 保持边界；本地 fixture 明确非生产 |
| 外部来源核验 / model purpose | 由既有域服务负责，衰减不授予许可 | 不替代 |
| 新 DDL/HTTP/UI/调度 | 本原需求不要求修改这些真源 | 无新增 |

实施计划见 `work/v5-age010/plan.md`、架构见 `docs/architecture/agent-memory-decay-v5.md`。所有 Go 先暂存于本任务 work，根原 071 全 Go 冻结释放后才落产品 Go；全程仅写 lease scope，不写 live 队列或共享总报告。

## 当前实现与实际核验

- `apps/api/internal/agentdecay/model.go`：baseline、真实时间 anchor、分数语义与默认策略；只产生 metadata view，无写入。
- `apps/api/internal/postgres/agent_memory_decay.go`：两个原生当前元数据 statement，无长期 transaction snapshot、client clock、源正文或角色绕过。
- 纯数学/shape 测试与 native integration 实现实际权限、重连、metadata 等数据变化与最终边界 gate；fixture gate 只暂停原生第二 statement，不提供权限或伪造结果。
- native1：57 PASS / 0 FAIL / 0 SKIP；native2（最终源）：59 PASS / 0 FAIL / 0 SKIP。两轮真实 fresh 001–071 + 非空旧数据 + Go vet/build exit 0，完整源和完整 public/catalog 相同，独占数据库实际清理。
- native2 4 个交付源 SHA：model `8773dc6bce2ef8766cce326c925eac88fdd33af391bb2bd5d6353715bea213c0`，model_test `f202e485e85ff0030bb518379f402bbed0ea954ed85cb90a203563fd8da928cd`，PG reader `0ccb62652c5f727bccd143937f94f122f6d45c723b39bf062927c19cedbff2ae`，PG tests `1217764a09c34e3ebe9c6ce53371160aebdb8094e56883629a464cb440725269`。

命令与结果：`python work/v5-age010/verify.py --round native2 --schema 71` 执行 `go test ./internal/agentdecay ./internal/postgres -run ^TestDecay -count=1 -json`、`go vet ./...`、`go build ./...`，CGO_ENABLED=0，实际结果 `work/v5-age010/native2/result.json`。专属不可变归档保留两轮源 bytes、日志、完整 row/catalog 快照和 manifest，根独立复核后才决定状态。

## 限制

CODE_AND_LOCAL_VERIFICATION：当前存在真实 native projection 和数学规则，不是已上线的自动推断/用户界面。可信 inferred producer、来源长期核验、model purpose、HTTP/UI 接入、时间策略现实校准和应用真机均未新增或声称可用。无新 schema；race 因 Windows CGO0/no GCC 未运行。Flutter 未修改、未在此任务运行。生产身份、部署、CSSA 授权与现实 A→H 条件缺失仍使 Closed Pilot / Consumer Beta NO。将 INFERRED fixtures 明确标合成本地形状，不伪称真实用户兴趣。
