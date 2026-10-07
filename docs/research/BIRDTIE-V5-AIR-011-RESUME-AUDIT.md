# AIR011 人审 HTTP 增量审计

2026-10-04。唯一live状态在原队列；原PARTIAL完整保存在 `work/v4-cht003-resume/original-air011-partial-2026-10-04.json`。本轮不重建或重置队列。实现范围由 root 精确 lease 登记，worker 不改 live 状态或共享报告。

## 实际来源与复用

- 原062 `postgres/model_egress_budget.go` 已有 native SELF_TASK_QUERY Preview/Approve/Revoke、四层预算与独立待请求/在飞/未知账目。
- `modelegressbudget/model.go` 的 `Preview.Display` 是人审只读 DTO；不是 client confirmed，也不是 machine tool 许可。
- `modelresilience/offline.go` 仍只有 OFFLINE_CONTRACT 的 fake resolver；正常 Service/Gateway 均无真正 provider 出口。
- 058固定配置/pre-run binding 不等于持久 AgentRun。AIR016 TODO 仍依赖 AIR015 PARTIAL；不在本轮另造 ledger 或降依赖。
- 新033 TASK_CONTEXT_READ 与034相关性、022预算投影不授予 MODEL_CONTEXT_EGRESS，不能自动继承为模型许可。

## 本轮实际补齐

原生本人权限的人审 Preview → 具体 digest Approve → Revoke → 四层只读预算，以现有 HTTP server 注册，并复用严格 JSON、Bearer和 no-store 契约。具体路由与 closed DTO 在 canonical `docs/architecture/MODEL-EGRESS-BUDGET-V5.md`。

真实 PG registered HTTP 验证匿名/失效会话、非PERSON、组织 workspace、跨owner、错误digest、原会话普通idle更新、同owner换会话、Task变更、Account/Agent停用恢复和metadata重建。重复批准/撤销不重复审计，REVOKED不复活；全部人审动作产生0个reservation、预算使用保持0。

人审表示“本地预览已确认”，不表示模型调用、付费或业务效果完成。API默认Unavailable、价格仍LOCAL_SYNTHETIC，缺port503，原native缺price/root等403。没有价格/额度/Reserve/Begin/Settle/Runtime公开路由，未复制Memory、conversation或私人资料。

## 原失败与修复

1. 并行尚未完工的 AGE043 源导致初次 compile failure；不修改另一worker文件。随后本任务 unit test 的 net/http unused 在本范围修复；历史结果如实记录。
2. native1：96 PASS / 13 FAIL。新 HTTP 与 postgres 两个包在同一测试库激活全局配置路由，发生真实 conflict。HTTP fixture 改用每次随机命名的独占迁移库；没有改产品约束或默认Go并发。原ROOT scope误写ROOT_TRACE按真实原契约修fixture。
3. native2：108 PASS / 1 FAIL。原 owner advisory 等待后预览过期，INSERT先触发062 CHECK，native ErrUnavailable→HTTP503。根代理在已登记原062范围补 owner锁后的 INSERT前 egressFinish，保留末复核和CHECK；测试继续要求实际锁等待、403与零残留。
4. native3、native4：两轮均110 PASS / 0 FAIL / 0 SKIP，704源 stable、所有旧public行 unchanged、所有owned库 DROP。vet/build均0；证据不替代根代理当前全仓独立核证。

## 未实现与阻碍

| 项目 | 恢复条件 |
| --- | --- |
| AIR010 每次 retry/fallback 原生预算 | 真正 native permit 接线，精确目标批准；换目标不得继承原批准，UNKNOWN结果不得盲重试 |
| 持久 Run/恢复 | 先补AIR015真实候选效果，然后依赖齐全后AIR016实现唯一Run/Step真源；058不充当Run |
| 多源模型 Context | 分别明确出口目的、目标、字段/来源版本与撤权，不能复用TASK_CONTEXT_READ批准 |
| 消费人审 UI | root/price/config的可发现流程和中文人审入口、移动/身份迟到/AT实际验收；现HTTP不可称完成 |
| 正式 provider 与费用 | 批准地域/保留、secret管理、正式价格/tokenizer、费用对账与实际网络边界验收 |

完整 AIR011 仍 PARTIAL。AGE035依赖不降低。适用 UX-CHECK-01/03/05/09/10/13作为后续界面验收要求；本轮没有界面整改、Flutter、真机、AT或现实运营证据。Closed Pilot / Consumer Beta **NO**。
