# BT-V5-AGE-060 当前源码审计

2026-10-03，owner memory_isolation。原AGE052/057/060/072及队列验收复核，依AGENTS/V5协议。此前只读预审来源 `work/v5-age007/preaudit-age060.md`；实施计划位于 `work/v5-age060/plan.md`。

## 发现与实施

1. 原goal“没有PrivateProfile/Memory及跨主体测试”已过时：AGE002/004/005有真实持久Store+registered human HTTP。沿用原native服务及唯一身份、metadata、Memory/Evidence版本链。
2. 原Private本人共享锁/当前session绑定、HTTP Person-only/no-store/query与workspace拒绝已经落实禁止条件。新增三测试文件补明确canary和activeOrg/admin/entity-principal矩阵；没有修改旧权限、Storage、router、DDL或原fixture。
3. `profile_view`与人类11字段投影正向能力保持，只允许原粗ACL与field audience相交的字段；不泄露Memory/privateProfile/Evidence。组织角色、本人切组织workspace、收藏地点不继承PRIVATE；saved不是visit。
4. 实际FeatureGatedDomains.ReadMemory不消费真实NativeMemory，ON仍固定Unavailable。观察wrapper只是委托真实Store的计数，不是resolver；计数只观察传入适配对象，不审计所有数据库访问。实际固定Unavailable端口与CurrentDomainStore没有Memory方法是可检源码边界，真native保存/读取正例证明canary确实存在。纯Runtime输入另标OfflineContract；随机TaskID不证明已批准认知任务。
5. Social/Attention只是进程内Policy基础与ServiceUnavailable，不宣称持久用户设置、通知/分类流程或成员privatePolicy reader。当前具体内容跨主体机器授权桥缺失，禁止JSONconfirmed/self Verified/旧grant/066 ON顶替。
6. Org fixture含原生active Agent和独立Org entity/account ID；Business仅suspended预留或HTTP无Agent。本地synthetic fixture走真实原生SQL/API，不代表生产身份或CSSA试点。

## 历史与当前实证

`work/v5-age060/compile-only`是无DB编译检查，三个新测试SKIP，仅说明编译通过，不能记实际隔离PASS。

初次 `native1`：387 PASS、4 FAIL事件（2 test + 2 package）、0 SKIP；testExit1，vet/build0，完整public行保持且ownedDB drop。新cognitive夹具漏accounts.id（无默认UUID）；新HTTP PrivateFields未初始化所有list导致JSON null遭严格输入拒绝400。修复只在新测试夹具显式UUID/空array，没有放宽权限或输入规则。PG新矩阵已实际通过。该轮全API sourceUnchanged=false仅因并行新增 `httpapi/profile_apis_integration_test.go`，旧源未变化；该轮按FAIL完整保留，不作最终source stable证据。

`native2`修复后实际453 PASS、0 FAIL/测试SKIP；新矩阵54 PASS，test/vet/build0，完整public行相等且ownedDB drop。全部API源hash仅因并行新增agentautonomy的limits/model/registry/service四文件而sourceUnchanged=false，故runner按整体证明未通过保留；不虚称稳定冻结轮。三新测试源码本轮前后保持。

后续固定源码结果由[正式归档](../testing/evidence/agent-memory-isolation-2026-10-03/README.md)记录，独立根复核与共同全Go由根补证；不将初次失败、无DB skip或旧历史PASS混作新证据。

`native3`进一步实际复验同样453/0/0、新矩阵54、test/vet/build0、完整public行相等且ownedDB drop。三owned源保持true；全internalGo仅新增并行httpapi/public_profile_test.go而sourceUnchanged=false，functionalPass=true但整体runner仍按FAIL保留。三测试保持冻结，完整源稳定帧待根协调窗口。

## 完成与限制

原四项默认禁止边界可在真实当前负例与owner正例、原适用回归、独立复验和共同回归后CODE_LOCAL DONE，不新增Storage/权限服务来凑实现。052“除非明确授权具体内容”是必要条件，本项不提供该授权正向功能。若范围要求真正供Org认知读取获准成员内容，该功能PARTIAL，恢复须实际当前用途/具体源/主体Agent/组织角色/期限授权适配及撤权竞态正负证据。

无DDL故不占迁移号；当前验证baseline001–064。旧生产仅读取/hash快照，三个新增测试文件为唯一Go改动。全public行比较含现有Memory/Evidence/candidate/reinforcement/outbox控制，不排除任何表。只清理实际owned随机DB与fixture。无客户端、race instrumentation、模型、现实Business激活、部署或试点证据，Closed Pilot/Consumer Beta NO。

## 根完成核证（2026-10-03）

真正owner正例+跨Person/Org/dormantBusiness负例来自native Memory/Profile/Evidence。根453（新矩阵54）PASS/0fail/skip、379源冻结、vet/build0、全public相等/自有库清理；worker1545份原记录及根392份全部SHA核验。当前共享504API/18owned源码冻结+全Go7063PASS/0fail/testskip/fullvet-build0，补齐worker早期并行源变动的独立验收。见[根receipt](../testing/evidence/agent-memory-isolation-2026-10-03/root-independent-final.json)。默认隔离CODE_LOCAL DONE不表示特定内容cognitive授权桥、原生私人Policy端口或Business已开放。旧失败保留，Closed Pilot/Consumer Beta NO。
