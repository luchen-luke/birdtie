## 最新根验收摘要（2026-10-05，CODE_AND_LOCAL_VERIFICATION）

根代理独立整仓 schema089：10652 PASS、0 FAIL/TEST_SKIP/pkgFAIL，Go test/vet/build及两CLI实际build五命令exit0；921冻结输入（918 API+3原seed）与当前源码逐字节一致。完整48旧合成行、全部原xmin、语义catalog及089 unused down/reapply保持，245个实际独占库已由根SQL独立确认不存在。原AIR049仓库本地AC根核通过，唯一队列状态仍以root维护的live queue为准。

权威核验凭据：`work/v5-age038-resume/air010-air049-current-whole-root38he.json`，SHA256 `15188c2a8d1e7638a52cd292a0af80bed88472b13da034b282ebc53883e849d0`。最终50项目标测试和本轮整仓结果分别保存，不能混算成真实用户或provider验收。

根whole2真实10649 PASS/2 FAIL事件（同一077叶子及父）保留原始源码/日志；089审计外键导致单表TRUNCATE的SQL兼容性收窄，不声称原单表命令保持。显式audit_events与派生agent_enrichment_observations双表维护通过；原077受保护历史P0001拒绝、完整数据不变及合法ABA旧预览拒绝均有当前测试。生产Go/089外键/077guard未为测试放宽。

未验收：正式部署告警值守、生产scheduler、真实provider/账单、schema089真机、TalkBack。本项未新增HTTP/UI，不恢复私人答案或模型派发权限；原模型/Agent出网默认关闭，Closed Pilot及Consumer Beta仍NO。下方先前PENDING与失败记录是完整保留的历史阶段，并由本摘要提供最新仓库核证结论。

---

# Agent Enrichment 观测合同

2026-10-05；BT-V5-AIR-049。复用 AIR Runtime、social-agent-audit-correlation-v4、ModelRequestRun/原062预算及通知路由规范。本文件仅定义观测，不授予任何领域/认知/模型权限。

## 六个指标的发生事实

Memory created/corrected必须来自原事务锁定的missing/current分支，不从现存Memory状态猜过去。deleted只计原删除成功；rejected是候选人工拒绝，不是自然失效；promoted仅候选具体批准同Tx关联真正EXPLICIT Memory并转ACTIVE，不是082 stage或模型评分。原action、actor、ID、审计、幂等和末授权检查保留。

policy_triggered只统计原native_notification_decisions已提交、policy_version>0及reason exact_rule/default_rule/attention_paused，五种disposition均包含。not_configured/disabled/expired、设置编辑、无源、失败和no-op不计。family=NOTIFICATION；SocialPolicy机器执行不由此推断。原typed FK级联使其仅表示当前仍留存决定在窗口内的发生数，无永久历史承诺。

## 元数据与访问

新增仅可过期的派生观测metadata引用原auditID，唯一键避免重复，无Memory正文、私密源JSON、query、相册、聊天、APIkey、proof/Ticket或隐藏思维链。旧未分类或已prune观测记录明确UNKNOWN，不从原put action/当前状态回填生命周期分类。

本人Session从原生身份推导owner，bounded只读窗口和countcap；组织只原组织授权域聚合，不给组织管理员任何个人trace。聚合不显示原源ID/标题。具体source引用需今日原完整ACL/Block/expiry/version复核，否则隐藏source而非重用历史许可。

083确定性候选Run与088模型Run明确区分。088原Run→058固定配置→中央prompt/schema/policy/tools/caps/fingerprint，steps→原062price/reservation得到provider/model及费用/usage。UNKNOWN actual为null，另给held上界；LOCAL_SYNTHETIC不是正式费用证据。受控闭集决定/错误码及耗时不得夹带自由正文。

实际读取接口：`Store.ReadOwnEnrichmentMetrics`、`ReadOrganizationEnrichmentMetrics`、`ReadOwnEnrichmentTrace`、`ReadOwnEnrichmentBudgetAlerts`；它们是原生本人/组织授权只读端口，本轮未新增 HTTP/UI 或组织管理员个人trace入口。Trace始终省略sourceID，并标记`NOT_INCLUDED_REQUIRES_CURRENT_SOURCE_ACL`，不沿用历史许可展示来源正文。088步骤按原上限8完整返回，超限fail closed。088未持久provider错误码，`UNKNOWN_NOT_RECORDED`不能解读为没有失败；083复用原闭集reason。duration是原Run created→updated元数据间隔，不是provider实际延迟。

Trace默认只可读取创建后30天窗口；`ReadOwnEnrichmentTraceWithinWindow`只可缩短，不可延长30天。最终PG clock核窗口到期并缩短`ValidUntil`，Session expiry/idle expiry也进入最短边界；编码后的最终身份检查保护表锁/网络前的原生释放点，不承诺网络发送之后的瞬时撤权。

## 预算与保留

真实预算四层为TENANT_PERSON/SUBJECT_PERSON/ROOT/TASK，仅观察原额度与分配量并计算near/exhausted告警。原仓库无MONTH额度真源，月度只原账目聚合，月限额/告警Unavailable，不新增费用台账或默认额度。

阈值采用精确整数比例：分配量达到原额度80%为NEAR_LIMIT，达到100%为EXHAUSTED，四种维度REQUESTS/INPUT_TOKENS/OUTPUT_TOKENS/COST_MICROS分别报告。月统计明确currency，仅该币种账目；原062合法配置中owner/scope币种不可变，不能绕原guard制造同owner多币能力。knownActual仅SETTLED已知费用之和，UNKNOWN/reserved/in-flight另列held上界，月额度为null/`UNAVAILABLE_NO_MONTHLY_QUOTA`。

window查询不是物理清理。仅新增派生meta采用有限expiry并通过有界prune真实删除；原audits/077/078/083/088、effect/费用绝不由本接口删改。原业务回滚会同时回滚metadata；旧schema/不可用观测不得生成假成功。

原writer默认派生meta留存30天，089约束最多90天。`PruneExpiredEnrichmentObservations(ctx,limit)`限批1–1000，仅删除已过期派生行，并发使用SKIP LOCKED；不是定时部署或原账目清理。原auditID唯一引用、原native事务xmin和真实Memory/Candidate转态校验拒绝后来凭现态给旧审计贴分类；UPDATE元数据拒绝。非空089 down原子拒绝，prune或所有原父主体真正移除后才可unused down。原audit/费用保留未由此缩短。

## 证据边界

本轮本地实现验收仍待根整仓089核证。native1实际14PASS/1FAIL，native2实际17PASS/1FAIL，两个42703均新等待fixture引用错误，原日志保留；native3实际18PASS/0FAIL-SKIP，五命令0和原public/catalog/xmin/up/down/reapply及15独占子库清理已核。最终native4新增元数据guard与原四层80%/100%演练，结果另记不覆盖旧帧。

核验使用随机独占本地库、合成主体和资料，默认模型出网/自动Agent动作/视觉/A2A关闭。敏感形状canary写在原本人私密资料中，真实原Task query/Moment正文/会话文本和SessionID也不出trace；这不是保存真实provider APIkey或真实相册/聊天，也不是外部DLP证据。正式运营告警、provider、真机和试点不因仓库指标通过而通过；Closed Pilot与Consumer Beta NO。

最终worker native4：19PASS/0FAIL-SKIP/pkgFAIL，五命令全部0，918API+3原seed=921完整执行frame稳定；完整旧public/catalog/全部原xmin以及089 unused down/reapply保持，16child库与parent全部实际DROP/独立不存在。源码冻结后只归档，根共同整仓089仍PENDING，当前不是正式部署证据。

## 2026-10-05：089 审计外键与显式维护兼容

根整仓 root-whole2 原始结果为10649 PASS / 2 FAIL事件（一个077叶子及父用例），test exit1，其余vet/build/两CLI exit0。证据：`work/v5-age038-resume/air010-air049-failed-whole-root38gw.json`。该红帧及native1–4、worker-final1保持原字节。

089新增审计引用外键后，即使派生观测为空，单表 `TRUNCATE audit_events` 仍由PostgreSQL以0A000拒绝；ON DELETE CASCADE不等于TRUNCATE级联。原单表SQL兼容性确实收窄。维护无受保护历史时必须显式同时列出 `audit_events, agent_enrichment_observations`，不使用CASCADE。该兼容调整不改077保护：存在Community公开声明审计时，同一双表命令必须到达原077 statement guard，以P0001及原具体消息拒绝，完整审计/上下文/声明/派生观测均不变；合法公开→私密→公开后旧预览仍拒绝。

本轮只维护原生测试，不修改生产Go、089外键或077 guard原字节。新native5同时覆盖完整CommunityInterest与EnrichmentMetrics；结果尚待本轮原始输出，后续根整仓whole3核验仍PENDING。原admin审计外键也有相同单表维护限制，未宣称原单表TRUNCATE行为不变；观测prune不调用审计TRUNCATE。


新增兼容修复 native5 已实际50 PASS / 0 FAIL-SKIP / pkgFAIL0，test/vet/build/两CLI五命令exit0；918 API+3原seed=921冻结输入均稳定。19独占child库和parent实际DROP、独立SQL不存在，完整48旧行/全部原xmin/语义catalog及089 unused down/reapply保持。生产Go/089SQL与077SQL原字节不变；唯一测试增量及新文档摘要在source-freeze2。根整仓whole3仍PENDING；旧root-whole2红帧保留。
