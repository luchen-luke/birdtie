# Agent 失效传播与已提交效果边界

2026-10-06，AIR018实施中的叶规范。真实验收以根live队列及原命令证据为准，当前生产增量和whole验收尚待完成。

## 权威与顺序

1. 使用原Session/主体/Agent/Task、目的独立consent、来源版本/current ACL和原066票据；receipt、source marker、日志或客户端confirmed都不授权限。
2. 原062/088 Begin commit前撤权阻止新派发；commit后保留IN_FLIGHT/UNKNOWN并只按原ID对账，不能保证收回网络请求或以lease过期假退款。结算不等答案释放，返回后仍核当前原批准/source/config/Session/票据和最短期限；不追加新的步骤。
3. 原063/064/082/091候选Stage同时核源与效果地址。晚到旧源不可新写；已经提交的stable effect及独立人工EXPLICIT声明不因支持失效被重写或删除。报名不等到场。
4. 094原source mutation仅append最小invalidations，不在Moment事务反向锁Candidate/Memory；原revision/xmin/fingerprint/currentACL先拒旧支持。marker的id/OLD epoch是失效事实，不是新许可或消费完成回执。

## 清理范围

无效pending支持可清predicate/category/assessment/sources并转EXPIRED；不删除原audit/effect/budget/terminal Run或EXPLICIT Memory。bounded cleanup不能用未来某轮清理替代即时当前权限检查。原manual Read/Refresh已lazy清理，验证bounded传播时必须在HumanRead前检查，避免隐藏差异。

原queued Run通过有限Claim→Execute复用当前Stage；NO_EFFECT必须原outbox/fence证明，UNKNOWN不能由错误枚举猜测。取消只退役原状态/claim，未知结果仍原ID查询，不重获源批准或新票据。

## 透明限制

仓库没有可声称已完成的远端删除/网络recall能力，也没有应虚构清理的vector持久实现。元数据对账不恢复私人答案。只读metadata有效期不续原授权；默认模型/真实自动写/视觉/A2A不开启。本地合成验收不替代真人、真实provider、生产清理调度、手机或辅助技术证据。

## 当前实施证据

本轮不可变094完整970输入＋两owned test，见 `work/v5-air018-invalidation-propagation/` 和 [实施审计](../research/BIRDTIE-V5-AIR-018-AUDIT.md)。native3确认single source-only bounded cleanup不足，其余当前闭包正负通过；最小生产范围待根授予，不提前宣称AIR018 DONE。


## 2026-10-06 schema095 本地目标交付（前述 PENDING 是初版历史）

根在 native3 两个真实 source-only RED 后登记095及唯一旧迁移测试兼容范围。当前生产改动只为新增 `095_agent_candidate_invalidation_cleanup.sql` / `.down.sql`：唯一替换 `birdtie_expire_candidate_pipeline(batch_size integer)`；不编辑001–094或原Go writer，不建表、授权、状态或失效消费ledger。single只选原local-v1/local-v2 effect及CANDIDATE；multi仍选原multi-v1。原完整current(grant) `IS NOT TRUE` 与候选原deadline共同判断，NULL/0/101拒绝，1..100、SKIP LOCKED、原support清空及EXPIRED语义保持。

native4真实68PASS/0FAIL-SKIP、五命令0，旧140表完整rows/xmin和六类目录保持，仅该函数定义变更；unused095down精确还原094目录，重应用一致。native5扩展验证真实70PASS/1FAIL叶（原CurrentDataMigrationRoundtrip:835），其余四命令0；旧095函数被091up还原后原完整public/catalog/xmin组合等式拒绝。原日志未分项，不能声称已分别核对该失败轮的所有分项。根追加唯一旧test租约后，仅095down→原091/082/063完整往返→095up；原全部row/xmin/catalog、Memory/evidence和拒绝断言不变。原第一至第五轮日志/runner/argv/冻结输入全部保留。

最终native6：**72PASS/0FAIL-SKIP/pkgFAIL0，test/vet/build/两实际CLI build五命令均0**。完整输入为原根094不可变970＋五owned overlay（四新增、一个旧test替换）=974；不是移动live整仓。974复制前后同，五owned当前同；movingLiveBaselineDifferences只比较原baseline路径，不枚举另一069新增live文件。根联合当前whole仍PENDING，由根独立合并069后验证。

实际新增验收：

- single edit/withdraw在任何HumanRead前，完整current=false且原grant与candidate原期限仍有效，cleanup恰1并清support；NEGATE原094已同事务清除，后cleanup0。额外multi来源失效同样清除；不修改原批准/期限。
- 精确holder PID、owned DB与原cleanup SQL匹配的关系锁等待：等待前candidate未过期，PG时钟跨原截止后放行清1；三次后续0。NULL/0/101原P0001闭集错误且所有public rows/xmin同。
- 实际候选行锁→cleanup SKIP LOCKED返回0，释放后两个并发cleanup总1；人工非pipeline候选完整row/xmin不变，独立EXPLICIT Memory与原effect/inbox/outbox完整row/xmin保持。原实际CLI两个并发进程总1，随后两个新进程restart/repeat0；不称生产调度。
- 非空094实际candidate/effect/checkpoints及独立EXPLICIT Memory先建立再迁移095：up所有旧row/xmin保持，清理原无效候选后used095down/reapply所有row/xmin保持、已scrub支持不复活。095不是持久新状态，没有used-down拒绝新表语义；down只恢复原函数，不能复活业务数据。
- queued原083Run失效后真实Claim→Execute为FAILED/AUTHORITY_CHANGED及同fence NO_EFFECT，零新候选/effect/Memory；取消lateExecute只原Cancelled同ID/version receipt。原062/088及单/多source闭包相关目标复用原测试，原审批/账目/票据不扩权。
- dispatch commit前实际等待后撤权→Denied/零请求；提交后只一次本地synthetic adapter，撤权后零私人答案释放，SETTLED或UNKNOWN按原ID对账、UNKNOWN四预算层held上界不退款/不重发。注册HTTP原withdraw/receipt/取消/跨主体闭集通过。

最终真实27migration子库＋2registered HTTP子库＋parent=**30个**，各自SQL独立absence；不是仅依进程退出推断。retained-old-data输入另存副本/bytes/SHA，不属于974源码；根baseline旧合成48行并不证明每个业务表有正数，非空candidate/effect/EXPLICIT由上述专用真实fixture另验。

原始结果：`work/v5-air018-invalidation-propagation/native6/result.json`；源冻结：`work/v5-air018-invalidation-propagation/source-freeze1.json`。schema095仅一函数目录delta，原领域ID、独立人工声明、已提交账目与UNKNOWN保留；未实现或声称远端recall/删除、生产清理scheduler、正式provider、手机/OSSecure/TalkBack/性能证据。模型出网/真实自动写/视觉/A2A默认OFF；Pilot/Beta NO。当前任务状态由根live队列核定，本worker目标GREEN不自行DONE。


## root current095联合终态（2026-10-06 root38pb）

原本地AC已DONE。当前981输入Go全仓11126 PASS/0FAIL-SKIP/packageFail，五命令0；原11003/72/103分支及multiplicity保留（仅随机fixture UUID归一，首次24 raw名字差异的harness失败保持）。094原137/48与095全140 rows/xmin/完整语义catalog保护、unused down/reapply、末parent精确同，369实际RAW owned库根SQL不存在。root不可变1431文件manifest 33b1d43ef35a96b8fbab9e638e0a425c8f42f431ff13e0c28661d34ed61c97be，根证明work/v5-age038-resume/whole095-joint-root38ox.json、状态证据local018-and069-closure-root38pb.json。原定向/RED/archive资料完整保留；非current095手机/生产调度器/真实IdP/CSSA证据。原007校准/INFERRED ACTIVE及attendance门槛未改，model/真实自动写/Vision/A2A OFF，Pilot/Beta NO。
