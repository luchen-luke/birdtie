# BT-V5-AGE-035 Context Budget audit

2026-10-05。实施 owner `city_history_audit`；根已按受控并行领取原任务。完成范围为 `CODE_AND_LOCAL_VERIFICATION`，live状态与根全仓核证由根负责。仅写原lease中的八份Go、两份既有canonical和本审计/证据/work；无新DDL、UI、provider/tokenizer或062账本。Closed Pilot / Consumer Beta 均 NO。

## 原source与去重

唯一原source `AGE-035` 为 `docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md:1288`：Context Builder 必须允许 `limit / priority / confidence threshold / recency weighting`。原任务同时要求保留旧主体、领域服务、ID和证据；未知、过期、撤销、跨主体来源拒绝，客户端/模型 confirmed 不授权限。队列早期goal中的token/fullRun缺项不是新增source；AIR011原四层预算真源已经独立完成，不再复制。

既有 AGE033 Builder/current用途与 AGE034 relevance 提供真实授权、选择和更新时间；AIR022 Adapter 已有完整JSON字节预算与中文实际消费。故四项控制增量并入唯一 [Adapter](../architecture/AGENT-CONTEXT-ADAPTER-V5.md)，native confidence 只增量记录在既有 [Builder](../architecture/AGENT-CONTEXT-BUILDER-V5.md)，未新建竞争预算canonical。

## 实际增量

| 文件 | 最小变化 |
| --- | --- |
| `agentcontextadapter/model.go` | 服务内部Budget可选ItemLimit、五类Priority、ConfidenceThreshold、RecencyWeight；真实控制/省略原因数量；可选Memory confidence |
| `agentcontextadapter/service.go` | 先验证完整投影内容；有界门槛/排序/数量控制；逐完整项字节计量；最终envelope变大时撤下低顺序完整项重算 |
| `agentcontextbuilder/model.go` | ReviewMemory可选DIRECT_DECLARATION元数据及合法性检查；旧缺值仍缺值，原Bundle JSON seal/deep clone自然覆盖 |
| `postgres/agent_context_builder.go` | 从现有scanAgentMemory的实际已校验confidence生成评估；保留原共享human/machine查询、ACTIVE EXPLICIT PRIVATE约束/版本/时效 |
| 两份原model_test及两个新native集成测试文件 | 原测试不放宽；增加纯控件/边界、native原值/原排序/实际时间、完整seal及注册HTTP反例 |

ItemLimit未提供=19，提供0..19；Task/City/原所选Policy不计此上限且不能省略。Priority提供时必须是profile/memories/places/activities/relationships完整闭集排列。阈值与近期权重均须有限0..1。默认MaxEncodedBytes-only调用保持原顺序，token仍UNKNOWN_NOT_TOKENIZED。

当前native ACTIVE EXPLICIT confidence实际只能是1；它表示本人明确声明，并非正确概率。新增门槛没有制造可用的低分INFERRED ACTIVE来源；原生测试只验真实1和相同的DIRECT_DECLARATION语义，缺值/非法/非DIRECT门槛反例由明确标记的纯合同fixture覆盖。其它领域没有评分，不补1，不按Memory门槛移除。

近期权重用原NativeTime更新时间与最终数据库ObservedAt；未使用活动开始、访问/居住时间或新的墙钟。native两个实际Memory lexical Score为8>3且高分ID后排：数量/门槛默认保留高分；另通过原生人类写入令低分Memory较新，显式weight=1选入较新项。此改变只影响已经批准且相关的投影纳入顺序。

## 原权限与seal保持

原Runtime仍接受grantId-only；真实Preview→Approve→current grant/Actor→完整Build→related projection→Budget→中文结果→**原完整Revalidate**。未更改Request、Builder Service、relevance或HTTP runtime生产源码。所有原选择源（包括相关性、threshold、item limit、字节省略项）仍留在原Request/seal，来源改变、真实版本/xmin ABA、账号或grant变更、跨主体、等待后到期均拒绝。内部xmin/seal、被删ID/正文、StructuredValue/私聊不入回应。

Budget报告字节、省略数量及不同原因；relevanceExcluded独立。答案仅来自最终保留事实，UNKNOWN空值不编造；省略不能冒称未请求、底层Memory缺失、到访或出席。ModelAccess仍UNAVAILABLE，MemoryPromotionAllowed=false，contentIsInstruction=false。

## 三份真实RED及修正

| 原帧 | 实际结果 | 原因与最小修正 |
| --- | --- | --- |
| `ordering-red` | 0PASS/3FAIL（含父）/0SKIP/pkgFail1 | 仅limit/threshold触发ID重排，逆ID高相关8分被3分替换；改稳定保留原数组的relevance顺序 |
| `envelope-red` | 0PASS/1FAIL/0SKIP/pkgFail1 | 4335字节处必需锚点能容纳，后续混合理由增长误ErrBudget；最终撤下完整低顺序项、重新状态/来源/字节计量 |
| `zero-red` | 0PASS/3FAIL（含父）/0SKIP/pkgFail1 | 零可选条目真实envelope可容纳却先用字节占位理由误拒；计量前使用真实LIMIT省略原因 |

均为真正运行到断言的功能RED，非编译/harness错误。所有原raw/source冻结保留；未改失效source/时效门槛。新排序下巨大首项+后续混合理由也单独扫测，确保回退分支实际执行；不能只借排序修复消掉旧envelope反例。

## 本地核证与覆盖

最终 `native3` 默认包并发、fresh实际001-088、原非空seed/current-data，468PASS/0FAIL-SKIP/pkgFail；Go test/vet/build和两个既有CLI build均exit0。910 API源码+3原seed=913冻结输入稳定，四个实际生产文件及四份测试SHA均交根；48条旧完整public数据、全部旧public行xmin、可见语义catalog未改变；原088未用down/reapply也保持原资料，自有库实际DROP且独立查询pg_database=0。

Adapter整体95.9% statements，service文件302/315=95.87%；Builder包79.5%，Builder model文件185/241=76.76%；relevance包82.4%；native Builder文件234/273=85.71%。这是实际定向pattern覆盖，不是全仓覆盖。具体裸命令、sourceSHA、包计数与未覆盖block见 [证据](../testing/evidence/agent-context-budget-2026-10-05/README.md)。

正常：四控制/native已知confidence/稳定原相关性/真实近期/默认registered Runtime。异常：闭集/数量/NaN Inf/字节/非法confidence。撤权/跨主体/ABA/真实账号锁等待后Session过期均执行。原注册HTTP最终pool等待session撤销/expiry/grant expiry/source变化/healthy及原UNKNOWN/空字段、NOT_REQUESTED、NOT_RELEVANT场景包含在最终pattern中，无fixture skip。

## 已知与未测

原四source的仓库和隔离本地实现/测试已交付，根同API帧whole与live状态为后续唯一最终核证。没有新接口/DDL/UI依赖或共享账本冲突。

UX-CHECK-02/04/06/08/09/10/12/16适用于中文真值、具体批准与权威结果分离、当前source/主体与迟到拒绝。Flutter/真机/AT/截图本项未运行（没有client源码变化）；race未运行（CGO=0，未建GCC环境）；provider精确token/生产模型、现实到访/居住/真实用户验收未声称。发布门槛不因本地synthetic供给或根整仓绿而改变。
