# BT-V5-AGE-053 组织知识来源审计

## 075 增量审计（当前）

**当前公共边界增量**：root独立复核旧final2发现公开context比较受pool TimeZone JSON格式影响，以及第二Authenticate后最终payload pool等待不复核原session的问题。旧native4/档案保留为历史，不能单独作为修补后当前全部完成依据。已在原scope修复：context时间以原timestamptz瞬间比较，非时间键精确、已发布旧context不重写；公共SELECT携真正digest+首SQL内部session snapshot，最终同一statement核current Session/account、PG absolute/idle时效与ABA，snapshot不序列化到public响应。anonymous无身份字段，Bearer不能降级匿名。native5实测242PASS/0FAIL-SKIP，18owned稳定、public/catalog保持、ownedDROP；global source drift只并行root两条CHT测试，本帧明确诊断，不冒称全API最终稳定。待共同freeze新native6/root全Go。

**最终修补帧native6**：共同freeze后的真实242PASS/0FAIL-SKIP/packageFail0；四目标test/vet/build0，671完整API源和18owned源稳定，完整public/catalog原样、旧PERSON2+四ORG8行/ID的075 up/down/reapply/catalog恢复、ownedDROP均通过。新 `worker-final3`归档当前源/命令/结果/公共边界diagnostic与旧档案manifest，final1/final2不改。源已交根独立target/全Go075验收；没有公告新UI、AT、生产IdP、真实CSSA或外服/模型/通知，ClosedPilot仍NO。

根核验确认原 AGE-053 明确五类来源，073 的四类181PASS不能代表全项完成。最初真实 rg 未找到独立公告 native 领域，只有 Memory ANNOUNCEMENT 类别；同任务 scope 已由 root 增量扩展，在075补真实 resource/preview/publication/withdraw/audit，复用068当前 session/Person/组织 owner-admin，保持模型和广播关闭。以下原审计/073结果是历史原帧；不删除或改写旧归档。

当前新增七条真实 HTTP routes 由 root 注册并全 build0；worker定向 native075 已运行最终 native4：230 TestPASS/0 TestFAIL/0SKIP/0 packageFail，四目标 test/vet/build0。18 owned Go/SQL和670全部观察API源在前后snapshot一致；root独立默认全Go075另核。公告来源已接入原 ledger/validator/resolver，使用真实 revision与当前公开 context。source不存在/未发布/撤回/过期/跨主体拒绝；FAQ和私密 Memory不充当公告。

首轮 native075：215PASS/5FAIL/0SKIP，源stable、vet/build0、完整public/catalog保留、ownedDROP。第五resolver误把Announcement纳入AdminInput self拒绝，已修并保留原帧。第二轮218PASS/2FAIL/0SKIP，源stable、vet/build0、public/catalog保留、ownedDROP；单一发布fixture暴露 SQL 多次 clock_timestamp 求值时序可能使 published_at 晚于 updated_at，改为同一个 materialized PG stamp供发布/撤回，未放宽期限/shape。新帧重跑，不把失败帧改绿。

native3 223PASS后补真实HTTP最后读负例和pre075四类组织旧样本；native4 230PASS。真实管理草稿/具体预览/发布/撤回/重启重读、实际roles/session/变更再恢复旧预览冲突、自然到期与PG组织锁等待后过期拒绝、128/129容量、重复并发发布单审计、公开projection原始blocks/context与deadline、第五Evidence实际revision→编辑退draft使旧reference退役，均运行。HTTP真实新注册routes与迟到角色/session/withdraw/context最终拒绝，不仅decoder样例。

001–074 baseline加既有开发seed与synthetic旧Task/active/deleted Personal Memory、Personal CURRENT/REMOVED Evidence、四类Organization CURRENT/REMOVED metadata形状8行，执行075 up→empty新增domain down→reapply：旧完整public每表旧行/ID保留，down全catalog精确恢复；全部native后完整public/catalog原样，ownedDB DROP成功。旧组织migration samples仅证明存储迁移保留，并非原始来源已授权或real当前resolver；真实source功能由独立native生命周期验证。非空公告/已scrub且true原始组织domain父实体删除后的REMOVED control均使down原子拒绝。

最终证据独立 `worker-final2`，不覆盖73的 `worker-final1`。无新增公告Flutter UI/真机/TalkBack/真实合作方公告/运营通知/模型或外部部署。Closed Pilot与Consumer Beta保持NO，最终DONE由root独立核证及live队列操作决定。

2026-10-03；来源 `docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md` AGE-053，实际队列及 051、057、068、072 源码。只推进仓库与隔离本地验证。

## 实际能力与缺口

- 已实现：068 同一 Memory ledger 的组织管理员人工声明、实际 Person session、当前 owner/admin、组织实体与 account principal 分离、Agent/metadata 当前绑定、版本/期限/删除。051 不含 Organization Evidence。
- 已实现：057 Evidence ledger 和不可变绑定、单次 tombstone、Memory 版本改变同步清除；现有 Go validator/来源均限 PERSON，不能直接放宽。
- 已实现：组织公共资料、已发布原生 FAQ、原生组织活动及真实 revision。公共可读许可不能代替模型分析 purpose。
- 部分实现：来源存在，但缺组织人工来源关联及当前版本解释；本任务只补该人工管理路径。
- 未实现/不可用：独立 authoritative announcement 数据域、通用公开内容 feed、组织认知 source-purpose resolver、推理/自动写/模型出口。ANNOUNCEMENT 记忆类别是注释，不是公告发布源。

## 最小计划

1. 新组织 Evidence 包复用元数据结构，封闭五类来源；新 validator 不改 PERSON validator。来源地址由客户端选择，归属/状态/真实版本/时间只由原生库解析。
2. 073 在同一 evidence ledger 加 ORGANIZATION 分支，保持原 PERSON shape 原样；沿用 FK、binding guard、tombstone、Memory-change trigger。存在任何组织行时 down 原子拒绝，不清历史。
3. 新 PG 文件复用 068 当前管理员事务/边界。Profile/FAQ 仅当前已公开 verified 组织原生资料；Activity 复用明确组织主办、公开发布、未取消、当前原始 ACL/expiry；admin input 引用同组织另一有效 explicit Memory，禁止自引用；公告返回 Unavailable。
4. Profile/FAQ 用真实 updated_at 与原生规范内容摘要；Activity、admin Memory 用真实 revision/version。metadata 不复制正文，人工 reference 不叫核验事实或分析批准。
5. 不在 Organization FOR UPDATE 之后锁 FAQ/Activity：真实 FAQ/Activity 更新先锁源行，再写审计 FK，逆序会死锁。用最终一个 SQL snapshot 重解全部来源与 session/Memory 时效；撤回/改版来源在人工读时退役，不返回旧地址。
6. 人工 PUT/DELETE/GET handler，root 注册路由。strict JSON、no-store、精确响应绑定、最终 access 验证。独立 native 正负、锁等待/并发、fresh/current up/down/reapply、PERSON 约束回归；源冻结后 root 统一全 Go。

## 边界与验收

UX-CHECK-02/03/05/08/09/11/13/16：人工声明、特定版本、当前授权、撤权、未知来源、错误/重读中文说明。无新 UI；Flutter/真机/辅助技术不由该 backend 任务冒称通过。数据库 fixture 不是真实 CSSA/生产认证或运营资料；Closed Pilot / Consumer Beta 仍受原门槛约束。

## 本轮实际结果

专属 `python work/v5-age053/verify.py --round native5` 实际创建、迁移、运行并清理 isolated schema073。181 TestPASS，0 TestFAIL/SKIP/packageFail；三目标包 test/vet/build exit0。原生 owner/admin、anonymous/nonmember/member/moderator/revoked、ORG/Business token、实际跨组织/Memory、source改版/删除/隐藏/撤回/过期、真实session/Memory/source等待期限、FAQ与Activity真实writer无反向锁、同ID并发幂等、100/101来源容量及恢复、HTTP registered route/strictwire/迟到角色和源版本、PERSON三个旧来源路径均运行。

从001–072+既有开发 seed+旧 Task/active/deleted Personal Memory+旧 CURRENT/REMOVED Personal Evidence 进行073 up/down/reapply：完整public各表旧行/IDs保留，empty-org down全catalog恢复，reapply旧行保留。存在任何CURRENT或REMOVED Organization Evidence时down原子拒绝，PERSON/Organization shape NULL与互串拒绝。全部native测试后完整public行/catalog无变化；ownedDB DROP成功。9源 Go/SQL SHA 与测试前后相同，root独立全量Go门槛另行核证。

初次失败未清理：native1缺城市必填region；native2先变Activity city而Place未变，真实guard拒绝并因旧cleanup登记较晚留了本测试owned row，隔离库已DROP。修为创建即完整City/Place/Activity且提前登记cleanup；native3/4/5通过。早期PERSON-only JSON method与fixture/test编译错误也记录在 `work/v5-age053/initial-failures.json`。

native5 `commands.json` 继承runner文案曾将vet/build写成whole命令；实际持久 `vet-process.json`/`build-process.json` 明确仅三目标包。原帧保留，`final-native-receipt.json` 重建实际命令并纠正未来runner文案；不把定向命令说全Go。最终文件清单/hash、完整结果和原失败帧在专属 evidence `worker-final1`。worker 未修改队列、共享总报告或server.go。
