## 最新根验收摘要（2026-10-05，CODE_AND_LOCAL_VERIFICATION）

根代理独立整仓 schema089：10652 PASS、0 FAIL/TEST_SKIP/pkgFAIL，Go test/vet/build及两CLI实际build五命令exit0；921冻结输入（918 API+3原seed）与当前源码逐字节一致。完整48旧合成行、全部原xmin、语义catalog及089 unused down/reapply保持，245个实际独占库已由根SQL独立确认不存在。原AIR049仓库本地AC根核通过，唯一队列状态仍以root维护的live queue为准。

权威核验凭据：`work/v5-age038-resume/air010-air049-current-whole-root38he.json`，SHA256 `15188c2a8d1e7638a52cd292a0af80bed88472b13da034b282ebc53883e849d0`。最终50项目标测试和本轮整仓结果分别保存，不能混算成真实用户或provider验收。

根whole2真实10649 PASS/2 FAIL事件（同一077叶子及父）保留原始源码/日志；089审计外键导致单表TRUNCATE的SQL兼容性收窄，不声称原单表命令保持。显式audit_events与派生agent_enrichment_observations双表维护通过；原077受保护历史P0001拒绝、完整数据不变及合法ABA旧预览拒绝均有当前测试。生产Go/089外键/077guard未为测试放宽。

未验收：正式部署告警值守、生产scheduler、真实provider/账单、schema089真机、TalkBack。本项未新增HTTP/UI，不恢复私人答案或模型派发权限；原模型/Agent出网默认关闭，Closed Pilot及Consumer Beta仍NO。下方先前PENDING与失败记录是完整保留的历史阶段，并由本摘要提供最新仓库核证结论。

---

# AIR049 原生发生事实审计

2026-10-05。原task及14精确scope见work/v5-age038-resume/air049-controlled-parallel-start38gj.json；只有root更新live queue和总报告。

已有REAL：原Memory/Candidate/Org writer、commit前授权检查，原审计request关联；原061/069/081通知决定；083确定性Run、088ModelRun→058配置及062四预算/账目。

未覆盖：put原action未分类created/corrected；无五生命周期精确观测投影、六指标aggregate、原源有效的脱敏trace、阈值告警和独立投影prune。旧生命周期无法回溯分类；月度限额真源不存在，不能新增猜测额度。

根38gl已解除共享frame协调。按PLAN实施，原审计动作/ID/request上下文、预算、通知writer与权限不变；新增089唯一派生表agent_enrichment_observations不回填旧表。

## 本轮事实与保留失败

- 初次compile日志 `work/v5-air049-observability/initial-compile.jsonl`：fixture错用Store方法和Reserve参数，未算产品验证；compile2 Currency字段落地顺序错误另存原日志。
- native1：14PASS/1FAIL，FinalClock测试漏FROM c（42703）；native2：17PASS/1FAIL，同测试引用不存在sessions.last_seen_at（42703）。修fixture未改003约束或设备/PG clock。
- native3：18PASS/0FAIL-SKIP；测试/vet/build/两实际CLI build全0；918API+3seed=921稳定。15独占child库逐一SQL不存在，parentDROP；全旧行和xmin/catalog、089 unused down/reapply保持。新正数Memory/audit原行/xmin独立往返无回填，不将原retained48行中空participation称已覆盖参与正行。
- native4最终收敛：新metadata不能UPDATE/续期限/复用原audit或按现态分类历史；原Reserve真实四预算层80%与100%告警。结果以该轮原始result为准，根共同全量未出前不称整仓通过。

## 不扩大能力

真实provider失败细码原088未存，trace明确UNKNOWN_NOT_RECORDED；社交Policy执行未知，不从设置编辑补触发数。月限额无真源，明确UNAVAILABLE；同owner币种不可变，USD配置冲突是正确原领域拒绝，不绕guard造第二币种预算。trace可追溯原088固定配置/原062price与usage，本地合成账目不作真实账单。

sourceID始终不输出且不取私人query/Memory正文作为trace字段；metadata读取不授模型派发、来源分析或恢复历史私答。真实Session/owner/Agent/meta边界加编码后PG clock，trace有限窗口仅可缩短。默认OFF、正式运营/phone/TalkBack/provider/试点NOT_RUN；ClosedPilot/ConsumerBeta NO。

最终worker native4：19PASS/0FAIL-SKIP/pkgFAIL，五命令全部0，918API+3原seed=921完整执行frame稳定；完整旧public/catalog/全部原xmin以及089 unused down/reapply保持，16child库与parent全部实际DROP/独立不存在。源码冻结后只归档，根共同整仓089仍PENDING，当前不是正式部署证据。

## 2026-10-05：089 审计外键与显式维护兼容

根整仓 root-whole2 原始结果为10649 PASS / 2 FAIL事件（一个077叶子及父用例），test exit1，其余vet/build/两CLI exit0。证据：`work/v5-age038-resume/air010-air049-failed-whole-root38gw.json`。该红帧及native1–4、worker-final1保持原字节。

089新增审计引用外键后，即使派生观测为空，单表 `TRUNCATE audit_events` 仍由PostgreSQL以0A000拒绝；ON DELETE CASCADE不等于TRUNCATE级联。原单表SQL兼容性确实收窄。维护无受保护历史时必须显式同时列出 `audit_events, agent_enrichment_observations`，不使用CASCADE。该兼容调整不改077保护：存在Community公开声明审计时，同一双表命令必须到达原077 statement guard，以P0001及原具体消息拒绝，完整审计/上下文/声明/派生观测均不变；合法公开→私密→公开后旧预览仍拒绝。

本轮只维护原生测试，不修改生产Go、089外键或077 guard原字节。新native5同时覆盖完整CommunityInterest与EnrichmentMetrics；结果尚待本轮原始输出，后续根整仓whole3核验仍PENDING。原admin审计外键也有相同单表维护限制，未宣称原单表TRUNCATE行为不变；观测prune不调用审计TRUNCATE。


新增兼容修复 native5 已实际50 PASS / 0 FAIL-SKIP / pkgFAIL0，test/vet/build/两CLI五命令exit0；918 API+3原seed=921冻结输入均稳定。19独占child库和parent实际DROP、独立SQL不存在，完整48旧行/全部原xmin/语义catalog及089 unused down/reapply保持。生产Go/089SQL与077SQL原字节不变；唯一测试增量及新文档摘要在source-freeze2。根整仓whole3仍PENDING；旧root-whole2红帧保留。
