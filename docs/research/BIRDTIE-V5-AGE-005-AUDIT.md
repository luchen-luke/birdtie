# AGE005 / AGE012 实际审计、实现与验收

2026-10-03。单一live任务承接来源005/012，先审计原生Moment/Participation/Saved、Person当前会话/Agent/metadata、004版本与064来源合同，之后实际实现，未重复导入任务。初步只读审计与锁序风险见正式归档raw/source-audit.md和raw/store-risk-review.md；前期建议不是最终事实，以canonical与源码为准。

| 核查 | 实际结果 |
| --- | --- |
| 原源引用/版本/时间 | REAL：三本人原生source、精确Memory绑定、当前源及资源ACL复核；事件/媒体位置不证明本人亲历 |
| 人工provenance | REAL：固定中文本人声明/关联计数，真实持久API/撤回/版本失效/无缓存 |
| INFERRED candidate与重要AI Memory | NOT_IMPLEMENTED：没有接纳入口，不生成无来源AI结论；候选/分值/纠正生命周期分别后续任务 |
| 认知/模型/分析许可 | UNAVAILABLE：没有purpose/current model-egress resolver；人类读取不授机器权限 |
| 消费UI/真机/辅助技术 | NOT_RUN：本项未改Flutter/产品入口，不复用旧截图证明新能力 |
| 现实试点/运营发布 | BLOCKED_EXTERNAL：IdP/HTTPS/现实组织活动授权/生产地图/部署日志/值守/提醒运营/真实A→H未齐，Ready NO |

简短实施顺序：复用单一Memory/SourceReference合同 → strictwire/manualEvidence → 057元数据及终态守卫 → 当前来源Store与真实HTTP → domain/spy/PG并发 → fresh/current-data/down/reapply/fullGo/runtime → 归档/根核证后队列。没有复制Civu、改外部服务或放宽原发布门槛。

## AGE005 / AGE012 当前人工 Evidence 与来源解释（2026-10-03）

单一 `agentmemory.Evidence` / `057_agent_memory_evidence` 为本人 EXPLICIT Memory 提供版本绑定的原生来源引用；当前真实本人 Person/精确 Personal Agent/metadata/current session 经 Store 复核。人工 PUT/DELETE `/{memoryID}/evidence/{evidenceID}`、GET `/{memoryID}/provenance` 已接 `/v1/me/agent-memories`，沿用无缓存、主体限定与当前会话网关。input仅声明实际Memory期望版本/来源类别及地址，不能自证来源版本、许可、confirmed、权重或时间；Evidence UUID是稳定控制地址与幂等键。

支持本人私人 Moment、going RSVP参与记录、Place bookmark三源：真实 Moment revision，RSVP updated_at digest，bookmark created_at digest；digest复用064共同版本合同，以事务UTC固定数据库元数据语义。source owner/ID/version、Memory version、server observedAt与native eventTime分别保存；eventTime是对应记录的updated/created时间，不是到访、亲历或照片经历声明时间。signal仅MANUAL_REFERENCE、weight=1表示手动关联，不是概率或AI结论；不保存原文/媒体/坐标。

GET解释只写“本人明确填写”与“本人关联了N条私人记录/N次报名记录/N条收藏记录”，并返回当下仍有效的最小引用。HTTP再核固定说明/实际计数、schema、Memory/owner/Agent一致、重复源/证据及100条上限，避免后端自由解释透传。最初七个HTTP leaf反例真实暴露透传（8 fail事件含parent）；ValidateProvenance与handler修复后197个spy通过。

创建、CAS移除、并发同址重试和终态不可复活；同Memory版本的同源地址唯一。更改本人声明或删除Memory时在同一事务将引用清为REMOVED；失效源编辑/撤回、报名取消、资源隐藏/到期/屏蔽、收藏移除后当前读取停止计入，下一次本人provenance读取事务懒清引用，保留最小控制版本而清source/event/signal/weight。旧引用不复活；新版本关联要新Evidence地址。未读之前旧CURRENT行可能仍占唯一/容量，人工需要先刷新；没有后台删源/到期传播服务。

最终单条payload SQL snapshot重核所有来源当前版本、资源ACL、session/Memory有效期；实际锁等待跨Memory/session期限拒绝、零提交。只锁本人的源记录，不在Participation之后追加Activity锁而反转原生Join锁序；最后SQL另核活动/City/主办与Block。线性化点是最终SQL snapshot，不能承诺之后已传出的数据绝对即时撤回。

057严格三值逻辑形状、当前Memory绑定/版本、独立Evidence控制与清除；所有非空Evidence包括REMOVED的down均原子拒绝，仅自有父记录正常cascade后empty down/reapply。完整旧public行和nativeProfilev3/Memoryv2保留。SQL形状fixture的随机source ID不作真正来源证明；原生Store/已注册HTTP另用真实开发域动作验证。

正式证据：[113归档文件/14源与脚本hash、命令、原始失败和完整快照](../testing/evidence/agent-memory-evidence-2026-10-03/README.md)。057最终三轮完整Go各4837 PASS、0FAIL/测试SKIP、19无测试包单列；目标scope905、新Evidence域246/fullMemory域498、HTTP spy197；全vet/build0，58 SQL拒绝/5正shape、两非空down exit3、empty down/reapply、自有DB清理通过。另当前编译API ready200，三个evidence/provenance匿名401/no-store，PID/路径/SHA核对后已停自有API；此runtime轮未另跑完整Go。Store/API重组持久读取已测，不冒称手机或部署进程重启。

**完成范围：CODE_AND_LOCAL_VERIFICATION。** 所有当前Memory仍为本人明确填写；没有无来源AI推断结论的接纳入口。INFERRED候选审阅/接纳、分析用途resolver、认知MemoryReader、model gateway写入、自动学习和中文消费UI仍分别待后续。PRIVATE/AGENT_ONLY人类管理不授机器许可。Windows CGO0缺gcc未跑race；100条满容量清理、所有第三方ACL竞态、视觉/辅助技术和现实试点未验。原IdP/HTTPS、已授权现实组织活动、生产地图、部署日志、值守/备用支持、提醒运营与A→H仍缺，**Closed Pilot / Consumer Beta NO**。

## 2026-10-03 共享065回归后的时钟域修正

根连续执行023/028/070时，current065-full1第一轮7437 Test PASS，第二轮7436 PASS、TestNativeCurrentContextConnectionTimeZoneCanonicalization一个Test FAIL及一个package FAIL。527源码SHA、完整旧public行、vet/build和自有库清理通过。原raw未记录各clock，不声称测得具体主机偏差或确认该次命中的分支。

发现数据库签发的ObservedAt直接与host时钟比较。仅work内控制RevalidateOwn的host clock−1秒、真实native session/source/SQL/HMAC保持的overlay三次拒绝。将ObservedAt未来检查移至当前native SQL加载后，与同数据库clock比较后，同一控制三次通过。Host到期/deadline、PG到期、source未来事实/版本、撤权、HMAC/generation、ctx与原租期不续期均保留，没有epsilon放宽未来用户事实。新十个纯clock case和实际native snapshot/current SQL的不同clock域检查均在完整回归中通过。Overlay不是生产源码或真实权限签发。

修复后，命令 pwsh -NoProfile -File work/v5-age028/root-shared.ps1 -Label current065-full2 -Rounds 3 每轮 **7449 Test PASS、0FAIL/SKIP/packageFAIL**，18无测试包另计；Go vet/build0、528全部API Go/SQL/mod及归档副本一致、fresh001–065/三个开发seed/非空旧up保持、每轮全public完整原行相等、自有DB DROP。完整原始失败、控制、源帧与收据见[根共享065归档](../testing/evidence/agent-city-history-2026-10-03/root-current065-final/root-current065-receipt.json)。本段不增加任务或认知/模型许可，后续015/033/062须新冻结帧。

Closed Pilot / Consumer Beta **NO**；无正式身份、部署、真实授权活动、race instrumentation或生产构建证据。
