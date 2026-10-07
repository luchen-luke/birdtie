# AIR018 撤权、删除、取消、过期传播实施审计

2026-10-06。任务原对象由根在011验收后领取；本文件不改变队列或发布门槛。

## 原要求与本轮顺序

原 source 要求实际授权检查/审批消费/效果登记/dispatch commit 可排序；提交前撤权阻止新提交，提交后保留在飞并对账，不追加步骤；晚到旧源不能产生新记忆。不保证收回网络请求，不以 lease 过期抹去未知效果，不伪造供应商远端删除。

根已验基线为 root-whole094-fixed38ni：11003 Go PASS、五命令0、970冻结输入；旧 public rows/xmin/catalog与清理核证由根持有。执行本片使用其不可变完整源＋两个已租新测试覆盖，不把另一069并行变动纳入旧基线通过声明。

初版与最终094只读审计保留在 `work/v5-air018-readonly-audit/`。094真源为原 source transaction 的 append-only最小失效标记和原 source xmin/revision/current ACL，不是新授权。人工NEGATE已同事务清 pending；Run实际Stage与manual readers都复用原gate。FIRST RED前没有修改生产、迁移或原测试。

## 原始诊断轮次（不可覆盖）

| 轮次 | 实际事件 | 分类 |
| --- | --- | --- |
| native1 | 15PASS/11FAIL事件，test1、vet/build/两CLI0，972复制源稳定，旧row/xmin/catalog保持、11migration子库+parentabsence | single edit/withdraw前HumanRead cleanup=0仍CANDIDATE是真实受控RED。其余新fixture错把NEGATE同步已清计为cleanup1、Cancelled原terminal receipt要求Conflict、他人withdraw409期望403、pending pool统计时点错误；分别保留。 |
| native2 | 22PASS/4FAIL事件（single两个leaf+父及HTTP新decoder错误） | 063 data envelope被新bare helper误解造成零值Record，非旧payload泄漏。runner误把仍在运行的parent记录为已drop HTTP child并提前AssertionError，test raw保留；该轮未完成vet/build及末native snapshots，不能称五命令全过。parent finally实际DROP/absence。 |
| native3 | 23PASS/3FAIL事件（仅single edit/withdraw两个leaf+父）；test1，vet/build/两CLI0；11migration+2HTTP+parent实际absence | 新fixture修正后仅真实source-only bounded cleanup缺口。旧rows/xmin/catalog与094unuseddown/reapply保持。972复制框稳定；当时moving live差异为069 server.go，明确非current whole。 |

日志、argv、每轮runner bytes、完整输入和清理结果在 `work/v5-air018-invalidation-propagation/native1–3/`，不把重复轮绿推断成原失败已消失。

## 已证实当前正负边界

- 原SQL cleanup在HumanRead前：single source edit/withdraw仍CANDIDATE/support；multi额外来源withdraw清EXPIRED；NEGATE已由原094事务先清single/multi，后cleanup0是正确no-op。
- queued真实原Run：withdraw/NEGATE后Claim→Execute为FAILED/AUTHORITY_CHANGED，NO_EFFECT由原outbox/fence查询证明，零Candidate/effect/Memory；未来deadline Expire并不伪称source批处理。取消后晚Execute只返回原Cancelled同ID/version幂等receipt。
- 原062提交前：真实held single-connection pool阻塞Begin，原Revoke先提交，释放后Denied/无私有request/仍RESERVED；记录实际完成acquire wait。提交后：adapter只在Begin commit后调用一次，原Revoke在返回前完成，私有答案不释放；SETTLED/UNKNOWN按实际usage保留账目。UNKNOWN仍held上界，原IDRecover不调用adapter，不能假退款或重发。
- 注册HTTP：原具体retention批准→Stage→原HumanMoment DELETE(withdraw)→原IDreceipt保留committed事实但不含旧candidate payload→063 GET清无效支持；跨owner/组织拒绝，零重复effect/自动Memory。取消接口留下原state audit与stable Run ID。

全部是已授权隔离本地合成资料/adapter，不是模型出网、真实供应商、真人试点或手机证据。两新HTTP测试有真实独立fresh数据库；第一轮原HTTP用runner已隔离parent，末完整不变量亦核，不能冒称当时另有未记录HTTPchild。

## 最小待根裁决的改动

只对native3确认的清理缺口建议新迁移（编号/范围由根授予）替换原bounded `birdtie_expire_candidate_pipeline(integer)` single分支，用原完整 `birdtie_candidate_pipeline_current` 而非只看两grant期限。固定原effect handler/地址、不新增ledger，不授执行、不删除独立EXPLICIT。旧063/082/091/092/094不得编辑；down应精确恢复094原函数catalog及原数据，清理后的终态不复活。

当前原Run/worker与094抑制路径已真实拒旧提交，没有证据要求新增queued消费者或修改其状态机。若后续真实负例显示缺口再申请准确范围，不能把source marker没有cursor本身当授权复活。

## 验收范围

当前production修复、目标最终GREEN、current whole均PENDING。初阶段没有客户端改动；沿原UX-CHECK-05/06/07/08/09/10保留当前主体、未知、具体审批、重复效果与迟到拒绝。phone/TalkBack/远端实际删除/生产调度/外部provider均NOT_RUN；defaultOFF、Pilot/Beta NO。


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
