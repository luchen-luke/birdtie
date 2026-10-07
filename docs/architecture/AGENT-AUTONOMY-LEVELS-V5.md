# Agent 自治层级

2026-10-03。AGE044 的唯一领域规范，接续 [认知 ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[AIR](AGENT-INTELLIGENCE-RUNTIME-V5.md)、[Memory 边界](AGENT-MEMORY-ARCHITECTURE.md)。来源为 [AGE 原文](../product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md) 1499–1576；四级定义与可调用的进程内限制框架已实现，独立范围测试通过，共同当前全仓冻结帧仍在验证。

## 层级与动作

| 稳定机器值 | 中文说明 | 原文操作 | 当前限制 |
| --- | --- | --- | --- |
| LEVEL_0_OBSERVE | 观察与理解 | observe、understand、build context | 默认配置；获准来源仍须当前用途和原权限 |
| LEVEL_1_ASSIST | 辅助整理 | summarize、recommend、prioritize、remind | 只描述准备范围，不授予私人读取或通知投递 |
| LEVEL_2_PREPARE | 准备草稿 | draft response、prepare invitation、prepare registration、suggest meeting | 用户须确认具体版本；配置和草稿版本不是批准 |
| LEVEL_3_DELEGATE | 委派行动 | take autonomous action | 定义保留；本期066禁止自主动作，不能配置启用 |

等级限制叠加于原领域权限和服务器功能开关，不替代它们。推荐、提醒和报名各复用原领域动作，不能用等级产生关系许可、消息发送权或报名事实。

## 实施范围

新增独占 `internal/agentautonomy` 作为可调用的确定性配置/限制框架。复用原 `actorref`、`agentcognitive.AgentReference`、事件来源版本类型、066 `agentfeature.Controller` 和041 `agentsocialpolicy`，不复制账号、Agent、Memory、审批或执行服务。

设置只绑定精确 Personal Agent 和 PERSON 主体，采用独立配置版本、有限期、CAS与撤销；进程内配置不表示会话鉴权、原生来源版本、持久本人设置或授权凭证。旧快照、主体/Agent变化、期限失效和服务器关闭须拒绝复用。纯离线限制结果明确标记非授权。

实际 Service 没有当前具体用途/来源/批准 resolver 时返回空结果与 Unavailable。构造合法类型、配置 Level2、开功能开关或传草稿版本不能成为执行许可。模型出口、自动业务动作、敏感推断和商业运行继续保持既有门禁；不能从多Agent并行开发推导产品自治已启用。

## 当前验收与消费约束

实际实现位于 `internal/agentautonomy/{registry,model,limits,service}.go`。`Levels/Operations/Lookup` 提供闭集中文说明；`NewStore/Replace/Revoke/Snapshot/Current` 是单Agent进程内控制；`EvaluateOffline` 的模式固定 `OFFLINE_LIMIT_ONLY_NOT_AUTHORIZATION`、`Authorized()`恒false。`Service.Evaluate(ctx, Request)` 接066与041实际框架，合法请求仍空Assessment+Unavailable；不接受OfflineView、provider回调或批准凭证。七种事件来源形状是离线比较范围，不能假称Memory认知source已注册或真实目标存在。

设置默认Observe、最长30天；请求精确15分钟期限及有界16来源。origin绑定同一个Store实例，旧版本/另一Store即使同Agent也不能复用。Level2 `preparationVersion`仅描述草稿版本，不是已签批准。041严格Request校验通过其公开Offline校验且仅使用Unavailable边界，实际社交服务使用原Service；没有合成Allowed/Verified/consent事实。

最终贡献者两轮各221 Test PASS、根独立两轮各221 PASS，0FAIL/SKIP、target vet/build0；贡献者7 owned+9精确依赖稳定，根7 owned及更广只读依赖共26源稳定。首轮204是补强前历史帧，首次工具重定向缺目录没有执行Go，均保存。原命令：`go test ./internal/agentautonomy -count=1 -json`、对应vet/build；根命令 `pwsh -NoProfile -File work/v5-age044/scope.ps1 -Label root-review1 -Rounds 2`。共同全Go结果完成后单独补记，当前不能借旧6654帧代表新源。

没有新DDL、HTTP、持久本人设置、Run接线或手机截图证据。未修改客户端基线另完成analyze、198测试和Debug APK，不是自治界面的证据。原源要求四级定义的本地交付与这些后续接口/消费流程分别核验；配置方法没有原生会话/ownership resolver，不能暴露为本人HTTP设置。

适用 UX-CHECK-06/08/09/10/11/16：未知来源不补事实、复用领域动作、具体版本批准、迟到/重复结果边界、中文名称和真实证据。这里只建立后端规则；不称消费设置界面或移动端/辅助技术验收已通过。Closed Pilot / Consumer Beta **NO**。

## 2026-10-03 根最终冻结帧与归档

原源AGE044的四级定义范围已达到CODE_LOCAL DONE。根独立两轮各221 PASS；当前三任务scope409、默认全Go7063 Test PASS/0fail/testskip，完整vet/build0，504 API Go/SQL/mod及18 owned源SHA稳定，fresh001–064/三个开发seed/原public全行保持/非空down拒绝3/空down-reapply/自有库DROP通过。正式原始日志、504源帧、工具与Gradle首次失败、Flutter未修改基线均在 [根核证](../testing/evidence/agent-autonomy-2026-10-03/root-independent-final.json) 与654文件独立manifest。旧6654与过程PENDING记录保留作历史；此新帧才覆盖最后源码。

进程内自治定义/control已交付；本人持久设置/API由AGE070接续，未有真实用途/来源/批准时实际Service仍空Unavailable，Level3不可开启、无自主业务动作。Flutter基线198/debugbuild不构成自治UI或真机验收。Closed Pilot / Consumer Beta NO。
