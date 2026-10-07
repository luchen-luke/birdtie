# AGE004 审计与实际收尾

初始只读工作审计原文及原始失败保留在 [证据审计](../testing/evidence/agent-memory-2026-10-02/audit.md)。原建议不是最终合同；尤其053真实metadata trigger/backfill、UUID地址/CAS及confidence声明语义见以下源码核验决定。

## AGE004 当前实现与验收（2026-10-03）

本次新增单一原生 `agentmemory.Record` / `056_agent_memory`，由当前 Person 本人直接管理明确声明。GET `/v1/me/agent-memories`、PUT/DELETE `/{memoryID}` 使用真实会话、活跃 Person / 精确 Personal Agent / native metadata 绑定和最终数据库会话检查；PRIVATE / AGENT_ONLY 都只供本人管理，不因此开放认知读取、模型或跨主体访问。UUID 是对象地址和幂等键，不是身份选择器。

13种类型、17个推荐字段加schema/owner/独立Memory版本已实现。EXPLICIT confidence=1 表示本人声明，绝不表示概率、客观事实或已核验身份；INFERRED 只预留 PENDING_REVIEW/EXPIRED/DELETED 的受约束持久形状，人工API拒绝推断写入。实际 Evidence、候选审阅/接纳、置信评估、自动学习及消费界面分别属于后续任务，认知 MemoryReader / CandidateSubmitter 仍硬 Unavailable。

创建version1、更新CAS+1；等值重试使用实际 jsonb 精确语义和微秒期限，避免指数/负零或相邻大整数误判。取得行锁后刷新数据库时间，写入与等值重试最终同时复核当前会话和保存期限；实际锁等待及触发器等待跨期限均拒绝并回滚。储存shape预检将越界数值返回400，不吞为503；wire与jsonb两层数字/字节约束明确记录，jsonb不能还原重复键，wire先拒绝。

删除清空正文和值、保留终态tombstone；重复删除不增版本，旧ID不可复活。过期读取仅投影EXPIRED，不伪造已持久的版本或过期调度；物理ACTIVE key仍由本人更新原ID或删除。已有Profile/Private/metadata源码版本不因Memory保存改变。任何含Memory行（包括tombstone）的down原子拒绝；仅真实父记录移除允许FK清理。

**源码复核修正**：053已有 agents AFTER INSERT metadata bootstrap trigger及既有Agent metadata backfill。004没有Ensure/重建被删除的metadata；缺metadata本人Memory入口拒绝，防止重新恢复已删隐私控制。前期审计中建议“server生成UUID / confidence NULL / Memory路径Ensure”不是最终实现决定，应以此节和真实代码为准。

正式证据：[168文件归档、14源码hash与原始失败](../testing/evidence/agent-memory-2026-10-02/README.md)。官方命令 `pwsh -NoProfile -File automation/verify_agent_memory_migration.ps1 -EvidencePrefix production-final2 -FullGoRounds 3` 实际三轮完整Go各3812 PASS、0失败/测试skip，19无测试包单列；Memory域/Store/HTTP scope433，全vet/build0。修复验证脚本路径清理后另以 `-EvidencePrefix production-final3-cleanup -FullGoRounds 0` 实测scope433、vet/build0及自动停止自有API；该轮没有另跑完整Go。领域252、最终Store68、107严格SQL拒绝/33正shape断言均已在对应日志核对。

自有fresh001–055/三开发seed/056、完整旧public行与nativeProfilev3保留、ACTIVE/EXPIRED/DELETED非空down exit3原子保护、清空自有数据后down/reapply与DB清理通过。实际编译API ready200，三本人Memory入口匿名401/no-store；重组API/Store后读取持久数据通过，不冒称真机或正式部署重启验收。最初重试冲突、数字503、跨期限保存以及fixture/清理脚本失败完整保留，修复后复验；只清理已核对路径/SHA的自有进程，手机预览未改。

Windows CGO0无gcc，race detector未运行；真实数据库并发已测。本项无新Flutter/UI、真实IdP/provider、现实组织/活动授权、HTTPS/地图、提醒调度运营、部署日志和值守/真实A→H证据。**Closed Pilot / Consumer Beta：NO。**
