# Organization Agent Memory V5

2026-10-03，BT-V5-AGE-051 唯一专项规范。来源为 `docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md` 的 AGE-051；沿用 Agent Identity / Ownership、Memory、认知 ADR、当前人类权限边界和用途限制。没有复制 Civu、另建任务或替代发布门槛。

## 实际能力与限制

当前活跃组织 owner/admin 可以通过原生 API 管理组织自己的明确声明，仍使用 `agent_memories` 一个内容账本。八类是 PAST_ACTIVITY、UPCOMING_ACTIVITY、VENUE、ANNOUNCEMENT、FAQ、PARTNER、COMMUNITY_RELATIONSHIP、POLICY。每类闭集映射 MemoryType 和 `org.v1.<category>.<key>`；原 Personal Memory 的 PERSON owner 验证保持严格。

这是组织管理员的人工声明：它不证明活动举办、成员出席、合作关系、场地质量或政策真实性。接口固定返回中文说明和 `CURRENT_ORGANIZATION_ADMIN_DECLARATION` 来源。过去/将来活动类别不自行认证活动时间，PARTNER 不建立核验合作关系。结构化内容仅有 `note` 与 `tags`，不解析事实、owner、grant、source ID 或授权。

没有本轮 Organization Memory UI、模型端口、自动学习、机器认知读取或私人用途授权。公共组织问答继续使用原核验公开组织、FAQ 和活动来源，不消费这个私密账本。AGENT_ONLY 是保留的储存枚举，不代表当前允许模型分析；PRIVATE 与 AGENT_ONLY 都仅由当前人类组织管理员审阅。组织知识 Evidence 的 AGE-053、成员具体内容委派与自动认知仍按原独立依赖推进，不扩宽旧 PERSON-only Evidence/candidate/reinforcement。

## 人类入口与当前性

| 注册路径 | 操作与版本 |
| --- | --- |
| GET `/v1/me/organizations/{organizationID}/agent-memories` | 当前管理员只读同组织非删除记录；有效期已过以 EXPIRED 投影呈现，不写数据库版本 |
| PUT 同路径 `/{memoryID}` | 七个严格字段；首写 expectedVersion=0；原记录以独立 Memory version 做 CAS；类别/key 不可重绑 |
| DELETE 同路径 `/{memoryID}` | 严格 expectedVersion；永久 tombstone 清内容，不能恢复旧 ID |

请求使用当前真实 Bearer Session 与 PERSON account；目标组织来自路径，实际组织账号和 Organization Agent 由数据库解析。拒绝查询参数、workspace header、外来 body 权限字段、重复 JSON 键、null/不支持字段、错误内容类型及超限内容。原生 Store 的 Access 是内部结构，拒绝 JSON 序列化/反序列化；拒绝反序列化也清零已有值。该结构的纯形状验证不是角色或会话认证。

匿名或初始无效 Session 401；Org/Business token、非成员、邀请中/撤销成员、普通成员/moderator、失效当前角色或绑定均拒绝。所有回复 no-store 并保留原 request ID。异常内容不回显私人正文。PUT/DELETE 原生事务成功后 HTTP 再做当前角色/Session 复核；若在这之后撤权，响应拒绝并不撤销已提交事务。写结果不明时要以当前身份重新读取，不能称事务已回滚或盲重发。

## 原生事务与账本

显式 READ COMMITTED / UTC。实际锁序为 Organization UPDATE → 两 Account 按稳定 ID SHARE → 当前 membership SHARE → Session SHARE → 当前 Organization Agent SHARE → 同组织 metadata SHARE → Memory UPDATE/SHARE。与原组织成员变更的 Organization-first 及真人 Profile 的 Account-before-Session 边界协调；不持 Session 锁再进入独立领域事务。

每次重要等待后和最终提交前使用新的 PostgreSQL clock 复核当前账号、组织、owner/admin membership、Session absolute/idle/revoked/dev-auth 状态、Agent 与 typed metadata。PUT 的有效期限也在最终 audit/Memory 等待之后复核；时间过期或取消将原生未提交的 Memory 和 audit 一起回滚。权限变更先取得组织锁并提交时，迟到请求不能复用旧角色；写入先完成锁定时，未提交的撤权不被虚构为已生效。

每组织最多 128 条非删除声明，包括可管理的过期记录。创建在组织锁下计数，删除释放槽；已有行可更新/删除。越权 SQL 写入造成 129 条时列表拒绝而不是静默截断，可由合法管理员删除修复。

精确重试不进版、不重复 audit。真正变更只推进 Memory version，不推进 Personal/Organization Agent Profile 版本、不清除或写入个人 Seed/私密资料。原 `admin_audit_events` 增加 organization_memory put/delete，记录类别、版本和 origin，不写私人正文；audit 与 Memory 同事务。tombstone 保留 ID，父 metadata 真正删除时才沿用原级联清理。

## 068 迁移

保持旧所有内容行不变；typed 原 FK 继续约束 Agent / owner / owner type。仅增加 ORGANIZATION owner 的闭集 shape、note/tags 校验及成对 audit 动作；060 reinforcement 当前 Memory 检查继续严格 PERSON，含空 entries 控制记录。057 Evidence、063 candidate 与旧 Personal writer/reader不扩宽。没有回填、Seed 或 production fixture。

down 遇到任意组织 Memory（包括 expired/deleted）或 organization_memory audit 原子拒绝，不删资料或历史。组织内容与 audit 都为空时才恢复旧 PERSON-only 约束、旧 audit 枚举和实际 060 guard，随后可重放 068。隔离本地 current-data/fresh/up/down/reapply 证据见专项档案；未在生产执行迁移。

## 验证状态

最终范围帧 `native6`：321 Test PASS、0 FAIL/SKIP，test/vet/build exit0，11 owned 与453观察Go源前后相同；新 Org Memory 111 个测试事件中包含纯规则和父/子测试，不能称为 111 个独立 native 权限场景。实际 HTTP 32 个事件、PG 35 个事件覆盖八类 CRUD、重连持久化、角色生命周期、跨主体、并发 CAS、原个人源隔离、严格传输、非空私密GET的最终撤权、自然过期锁等待、128/SQL129、旧 reinforcement、审计期限回滚。完整 public 原行和旧非空升级保持，自有数据库已删除。这是具体范围帧，不替代整库默认Go。

`migration2` 仅实际迁移验证：active/expired/deleted/audit-only 四种 down 各真实 exit3、完整 public 原子保持；empty down/reapply exit0，原 060 guard 恢复且自有库已删除。Go 未运行，receipt 对 Go exit 使用 null/NOT_RUN。

整库 `current068-full1` 真实默认并发8044 Test PASS、1 Test FAIL、1 package FAIL；vet/build0、586 API/SQL源稳定、所有public原行和旧非空升级保持、ownDB删除。旧060迁移测试在共享运行库执行down/up覆盖了068最终guard，故组织空reinforcement INSERT被真实允许。修复只将旧迁移测试迁至随机自有数据库并保留原全部断言，生产060/068 SQL不改；新增父库guard与整库每轮最终public函数/trigger/constraint完整比较。完整 `current068-full2` 的实际两轮通过、第三轮另一个原生Outbox失败，详见下方后续记录，不称完整PASS。

本轮无客户端修改，Flutter/真机组织 Memory、TalkBack 不适用或未运行；同批个人资料客户端另有独立证据，不能代替组织界面验收。所有数据都是合成本地资料，不证明真实 CSSA、活动、身份或发布。Closed Pilot Ready **NO**；Consumer Beta **NO**。

## current068-full2 与原生 Outbox 诊断（2026-10-03 后续）

完整默认三轮真实结果为8045 PASS/0 FAIL、8045 PASS/0 FAIL、8044 PASS/1 Test FAIL+1 package FAIL；0 SKIP，vet/build0，587源稳定、旧非空升级与全部public原行保持、每轮最终函数/触发器/约束目录相等、自有数据库已删除。旧060隔离测试与ORG空reinforcement拒绝三轮均通过；第三轮失败来自PlaceMemory循环原生CreateMomentDraft的064 Outbox第一组守卫。原日志没有具体OR命中理由，不能称已证时钟或权限原因。

只读定位见work/v5-age051/full068-place-failure-review.md。根仅在ownedMigrationDatabase创建的独立自有诊断库观察原时钟谓词并增加错误DETAIL，保留每项原拒绝及SQLSTATE；生产064迁移、Moment writer、Outbox writer均未修改。诊断2 generic4000真实创建、诊断3同Place与1999自述参数4000创建均通过；诊断4继续同参数4000创建并用真实future receipt INSERT负例验证原guard仍拒绝且诊断识别具体谓词：12 Test PASS/0 FAIL-SKIP、test/vet/build0、459观察Go源稳定、所有public与旧非空升级保持、ownedDB DROP。首诊断1因原函数存在两处expiry谓词的诊断形状假设失败保留，未进入实际循环。

这些诊断没有复现完整第三轮拒绝，不能代替完整默认Go，也不能称生产问题已修复。当前组织Memory状态仍IN_PROGRESS，待其他worker安全冻结及完整新帧实际验收；原始失败不删除，未使用-p1、跳过或吞错。

## current068-full3 已确认时序拒绝与有限恢复（2026-10-03）

- **AGE051 IN_PROGRESS，最新组织记忆与完整回归核证**：native7在当前13 MOM Go及三个registered routes与bounded capture恢复帧真实321 Test PASS/0 FAIL-SKIP，test/vet/build0、11owned+475观察Go源同帧、所有public旧非空升级保持/自有DB删除；组织新范围111父子/纯规则事件，实际HTTP32/PG35。migration2四类非空down3/原子保持、emptydown/reapply0，Go null/NOT_RUN。完整full1失败旧060共享down/up已移ownedDB，原SQL/断言保留；full2三轮8045/8045/8044，第三原生capture fail且当次确切原因未录；full3 8133/8133/8131，第三诊断父子2fail+pkg1fail，实际receiver.909864较PGclock.909588未来276us、binding/control true，OS原因未证。原064guard不改；仅可信native capture SAVEPOINT/最多3次/context2ms/重新原resolver+严格NewPending，source tuple/ID/time/fixedTTL保持，不作clock-only归因/时间容差。旧writer RED2PASS/3TestFAIL+pkg1FAIL；真实新scope47PASS/0FAIL-SKIP/vetbuild0/462Go+4owned/public/up稳定/DROP，含4000原生与future1sec严格SQL拒绝。当前full4默认完整三轮正在执行，尚无完整PASS或本项DONE声明。

真实命令：`verify-outbox-retry-red.ps1 -Round outbox-retry-red1`、`verify-outbox-retry.ps1 -Round outbox-retry-green1`、`verify.ps1 -Round native7`，路径均work/v5-age051。完整`shared-current068.ps1 -Label current068-full4 -Rounds 3`正在执行；之前失败日志及各source-frame原样保留，不用过滤/串行-p1/跳过获得完成。限定3次混合native拒绝恢复，不改变原064/068 SQL，原source不可用/过期/另SQLSTATE仍错误退出；并不保证墙钟永不倒退。CurrentApp/API重启的自动审批阻碍不等于OrganizationMemory真机通过。本项无组织Memory客户端/自动认知/外部授权事实。

## current068-full4 最终仓库回归（2026-10-03）

- **AGE051 IN_PROGRESS，最终完整回归已通过，正归档与队列核证**：native7当前321 Test PASS/0 FAIL-SKIP、test/vet/build0，组织范围111父子/纯规则事件，HTTP32/PG35；068 migration2四类非空down exit3且全部public原子保持，emptydown/reapply0、Go NOT_RUN。当前current068-full4默认完整Go连续三轮各8218 Test PASS/0 testFAIL-packageFAIL-SKIP、vet/build0，608 API/SQL/mod/sum源码帧稳定，完整原public行/原非空升级与全部function/trigger/constraint catalog保持，自有DB实际DROP。此前full1/2/3失败原样保留：full1旧060共享往返已隔离，full2当次具体原因未证明；full3实际PG比较未来276us，OS原因未证明。可信native capture最多3次SAVEPOINT恢复、原resolver/严格NewPending/原064guard及固定source tuple和TTL不放宽；old-writer RED保留，新scope47PASS、4000原生及future1秒严格拒绝。无组织记忆Client/手机、自动认知、外部授权或试点完成声明。

实际命令：`pwsh -NoProfile -File work/v5-age051/shared-current068.ps1 -Label current068-full4 -Rounds 3`。三轮完整默认包并发均通过，没有过滤失败测试、-p1或skip；608 source before/after及实际副本、全部public行与catalog、DROP原始证据见同work目录。Race仍NOT_RUN（CGO0/无GCC）；组织Memory界面/真机/生产IdP/部署/外部事实未验收，Closed Pilot与Consumer Beta仍NO。该闭环仅Organization Memory的CODE_AND_LOCAL_VERIFICATION范围。
