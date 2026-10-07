# AIR022 能力审计与增量

2026-10-04。原材料 `work/v5-materials/BT-V5-AIR-BACKLOG.json:545–568` 与live source mapping。实施前根核证AGE033已DONE；依赖INT001/AIR007/AGE033均DONE。旧goal「AGE接口未成」已过时，不新建Profile/Memory真源。

| 要求 | 审计前 | 本次增量 |
| --- | --- | --- |
| 当前真实AGE最小读取与Runtime | 033已有native exactpurpose/currentActor/六类+Policy真实消费 | 全部复用，不新SQL/DDL |
| 任务相关选择 | 033精确人工选择；034独立实现相关性 | 与034按接口交错，adapter不重复检索 |
| 输入token预算 | 数量/字节限制和007输出token，未有输入计量 | 版本化最终UTF8 JSON byte单位、确定性完整单位裁剪；provider精确token未知 |
| 字段native来源 | Bundle全局Sources，Fact未逐字段绑定版本 | 保留字段Provenance/native版本与时间，不输出RowToken |
| 缺值 | 本人空字段尚未填写，机器状态未明确 | UNKNOWN/NOT_REQUESTED/OMITTED_BUDGET区分；原生denied/expired不转unknown |
| 权限保持 | 原033具体preview/grant/源/会话/Task/ABA与末复核 | 预算/相关性遗漏源仍封在原完整seal；不恢复旧grant |
| 真实消费 | 033 consumeLocalTaskContext | 034唯一hook调用新helper，最终answer/counts仅基于预算后内容 |

实现文件为 `internal/agentcontextadapter/model.go/service.go` 与新 `httpapi/agent_context_adapter.go`；测试是package闭集/预算/中文UTF8/escaping/unknown/来源/无authority及新增真实PG registeredHTTP文件。共享purposeRuntime由memory_decay唯一持有；本worker未写Builder/PG/DDL/server/queue/sharedreport。

初次真实集成缺陷和未测不能被pure通过掩盖，native1→native2修复过程保留 `work/v5-air022`；最终冻结证据另写本项evidence。无model/provider出网、Memory/action/消息副作用，无Vector DB前置，无真实CSSA/手机/AT/发布证据。代码本地完成和Closed Pilot/Consumer Beta NO各自记录。

最终native5真实96PASS/0FAIL-SKIP、694stable、旧完整public保持、自有DB已DROP，包含后续根审查发现的selected empty field真实三态验收；不相关字段为NOT_RELEVANT与count-only元数据，未选NOT_REQUESTED和预算OMITTED_BUDGET分开。target pure覆盖94.7%、vet/build0；源码freeze后交根整仓核证，live DONE由根决定。
