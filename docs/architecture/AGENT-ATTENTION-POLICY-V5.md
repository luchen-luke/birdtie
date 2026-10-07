# Agent Attention Policy — V5

2026-10-02。BT-V5-AGE-037（源AGE-037）唯一AttentionPolicy foundation正文。来源为[AGE原文](../product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md) §14 AGE037；[137来源映射](../../automation/v5_requirement_mapping.json)保留任务归属。本文增量遵守[认知ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[Memory边界](AGENT-MEMORY-ARCHITECTURE.md)、[AIR事件](AGENT-INTELLIGENCE-RUNTIME-V5.md) §7.1及[066开关](AGENT-FEATURE-FLAGS-V5.md)。066当时“Attention领域服务未实现”是其交付时范围，本项新增如下后端模型与拒绝边界；实际Attention用途授权和通知管线仍未实现。

## 1. 模型与五路结果

实现独占包`apps/api/internal/agentattention`，复用`actorref.PrincipalRef/ActorRef`、`agentcognitive.AgentReference`及`agentevent.Envelope`。当前目录只注册`MomentCreated`和`UserQuery`；未知事件不静默归为NORMAL。Organization/Business/Community没有可调用本项Person策略服务，不从公开资源、角色或开关继承私人处理权限。

| 路由 | 本项确定性含义 | 不代表的效果 |
| --- | --- | --- |
| IMMEDIATE | 当前事件偏好即时处理 | 没有push、消息或真实送达 |
| NORMAL | 当前事件偏好常规处理 | 没有调度队列/执行器 |
| DIGEST | 当前事件偏好摘要处理 | 没有摘要集合、内容或计划任务 |
| SILENT | 当前事件无需打扰，包括策略暂停 | 不能跳过处理授权后暗中分析 |
| BLOCK | 当前事件偏好阻止；无效/失效输入亦为拒绝 | 不是已删除来源或撤回在飞数据 |

`Specification`仅为服务端内部设置：DefaultRoute、至多两条精确事件Rule、ValidFrom、ExpiresAt及可选绝对PauseUntil。没有通配、事件正文、关键词、模型分数、类别推断或供应商调用。所有五route枚举均可作为精确规则/default；没有擅自固定Moment或query必须采用哪一路。AGE038通知路由、039八类notification、040digest为独立后续任务，本项不伪造新事件类型来提前实现它们。

## 2. 独立策略版本与当前时间

`NewStore`对准确规范typed Person/PersonalAgent引用和完整合法设置做形状校验，初始policy revision=1；正确引用不是ownership授权。Store属于一个精确Agent，内部mutex保护Snapshot/Replace/Revoke；复制Rules数组，输入/输出不共享可改引用。

`Replace(expectedRevision, spec)`CAS精确+1；旧版本、零版本、overflow与非法设置原子拒绝。`Revoke(expectedRevision)`撤销并+1，旧policy不可再视为current；后续显式内部Replace可恢复设置但不授Runtime处理许可，也不复活旧revision。该过程只是本进程模型/服务，未持久化数据库，没有本人编辑HTTP、管理员网关或owner/session resolver。

本policy revision独立于AgentProfile/native版本、普通源updated_at/digest、Memory版本、consent revision和Feature Controller revision。当前来源必须沿用Event的实际native版本：Moment为REVISION，UserQuery为UPDATED_AT_DIGEST；不能以policy=1补造源版本。FeatureTicket亦不是policy/consent/来源版本。

所有时间为绝对时间，有效区间为`[ValidFrom, ExpiresAt)`；暂停为到PauseUntil之前有效，到达边界即可恢复匹配route。零时刻、不合法年份、逆向时限、超策略期限的pause均拒绝。没有周期静音、当地日历、时区推断或digest时间表。运行Service取真实`time.Now()`，不得用历史ReceivedAt延长事件15分钟期限。

## 3. 确定性裁决顺序

1. schema、事件目录、typed tenant/subject/actor、精确Agent、policy revision、policy状态、有效期及原Event TTL先检查；unknown/跨主体/过期失败关闭。
2. 离线合同还检查当前source exact reference/version、当前policy revision、正consent revision与current consent revision一致、无撤回/删除、授权ALLOWED及当前检查时刻。未知/缺失/拒绝/Unavailable从不放大权限；CheckedAt必须等于本次now，不能用缓存检查时间替代当前解析。
3. 精确event rule覆盖default；未匹配的事件使用default。重复event rule直接非法，输入排列不改变结果。
4. 匹配所得BLOCK优先于pause；其余route在有效pause中归SILENT；pause结束使用匹配rule/default。

此优先级由后端规则执行。自然语言、事件文本、模型声明或客户端`Verified`不能改写。default BLOCK仅用于未匹配事件，配置的明确event规则可覆盖default；匹配rule的BLOCK不能被暂停降成SILENT。

`EvaluateOffline(policy,event,OfflineBoundary,now)`为明确命名的synthetic合同函数，所有Decision带`Mode=OFFLINE_CONTRACT`。成功正例只证明规则分类；Denied/Expired/Invalid/Unavailable返回BLOCK和固定reason，绝不表示源已获准处理。OfflineBoundary持有source/consent/policy合同事实，其JSON编码/解码拒绝；Policy/Specification同样禁止JSON传递控制。它没有verified字段，实际Service不接受这个对象。

## 4. 可调用后端Service及真实授权缺口

`NewService(Store, agentfeature.Controller)`构造真实Go后端领域入口。`Decide(ctx,event)`检查服务端AttentionPolicy及Enrichment父开关、当前policy、Event、真实时间、取消状态；返回前再核同Controller Ticket与当前Policy，期间换配置/撤权失效。

已有`agentevent.Producer.Revalidate`真实解析session/source，但明确不授权处理来源，也不是Attention用途consent resolver。本项不将其包装成授权，不接受调用者传入fake resolver、client Verified或wire authority。因此**当前合法事件及两开关ON的Service请求仍返回ErrUnavailable和空Decision**；不返回可交给通知管线的IMMEDIATE结果。关闭开关同样Unavailable；原当前源/ownership/consent resolver后续实现前服务不消费私人正文、不读Memory、不调用模型。

最终check是本进程当前配置/策略核验，不是事务化通知dispatch commit。未实现通知账本、推送、模型、外部动作或网络撤回；不能将旧offline快照、policy revision或配置Ticket缓存为永久许可。后续AGE038须在实际投递边界重新解析当前identity/subject/Agent/source/policy/consent并保留独立授权与版本检查。

## 5. 证据与范围

审计计划及初次staging相对路径错误见[计划](../../work/v5-age037/AUDIT-PLAN.md)与[初次失败](../../work/v5-age037/INITIAL-FAILURE.md)；修复仅staging module引用路径，不隐藏首轮setup失败。[复现脚本](../../work/v5-age037/verify.ps1)可按staging或production执行新pkg Go tests/vet/build，保留源码前后SHA256与原始jsonl。

2026-10-02 staging stable1真实结果：[机器汇总](../../work/v5-age037/staging-stable1/result.json)、[测试原始日志](../../work/v5-age037/staging-stable1/tests.jsonl)、[vet](../../work/v5-age037/staging-stable1/vet.log)、[build](../../work/v5-age037/staging-stable1/build.log)。89 PASS事件（含父/子，不是89功能或用户）、0 fail、0 skip；vet/build exit0，5个Go源码前后hash相同。两现有event×五route、优先级/pause精确边界、权限unknown/revoked/deleted/版本变化/到期、不同valid Person/Agent、真实wall clock、并发32CAS仅一个胜者、运行取消与并发配置kill的拒绝结果均实际执行。

staging通过不是最终生产源码证据；copy生产后独立target日志与root统一全仓回归另行增量记录。本项无DDL和DB读写；不把无DB目标测试当作真实来源/consent解析证据。Windows当前CGO_ENABLED=0且无gcc，race detector未运行，mutex并发场景不冒充race instrumentation。

适用[UX规则](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md)为UX-CHECK-06（当前源/未知）、08（撤权/版本）、10（typed主体隔离）、11（未开放服务）、16（无私人正文埋点）；本项后端正负合同仅对应范围内证据，不构成这些功能全部端到端通过。没有Flutter/UI，UX-CHECK-01–05/07/09/12–15对应界面、入口、确认、设备、键盘、读屏、恢复和消费者截图均未运行且不宣称PASS。

无通知投递、categories/digest pipeline、Memory读写、provider/视觉/A2A/真实自主动作或部署。**Closed Pilot Ready：NO；Consumer Beta Ready：NO。** IdP/HTTPS、真实组织/授权活动、生产地图API、日志/值守/提醒运营、真实A→H仍由原发布Gate约束，offline/本地后端基础完成不解除。

### 5.1 最终生产源码目标检查

根代理批准单次复制五个Go文件后，2026-10-02T15:36:17Z在正式`apps/api/internal/agentattention`运行了独立target检查：[最终机器汇总](../../work/v5-age037/production-stable1/result.json)、[实际测试](../../work/v5-age037/production-stable1/tests.jsonl)、[vet](../../work/v5-age037/production-stable1/vet.log)、[build](../../work/v5-age037/production-stable1/build.log)。**90 PASS事件，0 fail、0 skip；vet/build exit0；5文件前后SHA256相同。** 最终比89 staging多一个UTC规范化跨9999年负例，保证规范化后仍合法；没有修改其他生产包。全量API/真实数据库统一回归由根代理另行执行，不用这90结果替代。

归档见[独立证据](../testing/evidence/agent-attention-policy-2026-10-02/README.md)。初次setup失败、修复、staging89、final90均保留；sourcehash可以区分各自实际版本。最终Go源码已冻结供根代理共享056回归，未操作live queue/共用报告。
