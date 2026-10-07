# BT-V5-AGE-051 审计与实施记录

2026-10-03；精确 root lease，唯一 live 队列保留原状态与历史。原需求 AGE-051 八类 Organization Memory，不由既有公开 FAQ/Activity 问答替代。唯一专项正文在 `docs/architecture/ORGANIZATION-AGENT-MEMORY-V5.md`。

## 审计与计划

原 056 Memory ledger、typed AgentProfile FK、CAS/tombstone、057 Evidence、060 reinforcement、063 candidate 与真实 Current Session/组织角色可复用。旧 PERSON-only writer/reader必须保留；不得伪装组织为 PERSON、不复制私人资料、不另造后台模型许可。原公共组织 capability与私密人工 Memory 分开；现组织 Agent 元数据存在，但不是机器推理授权。

实施为八类原账本 typed Organisation declaration、当前 Session+owner/admin 同事务服务、原注册 HTTP 路由和 068 增量约束。没有客户端、本轮知识 Evidence、外部服务、组织身份模拟登录或正式部署。初版先建立真实八类持久 CRUD，再补 actual registeredHTTP、当前角色/会话最终复核、事务等待、容量与迁移保护。

## 实际发现与修复

- 初次工作命令相对 cwd 创建了 `apps/api/work/v5-age051` 空目录，因根 log 路径不存在 Go 未运行；精确空目录清理被自动审批拒绝，未重试删除，保留目录。随后使用 root 绝对 work 路径正常编译。没有用户内容删除或覆盖。
- native1 为 264 Test PASS、1 Test FAIL+1 package FAIL。旧本人快照 helper 把 fixture 后来创建的组织 Memory 也算入本人变更；改为真实 PERSON 行和本人 source/control快照，不修改权限或数据库约束。native2 为 265 PASS/0 FAIL-SKIP。
- 只读安全复核确认 expectedVersion:null、创建容量上限和 Access JSON 明确拒绝应补齐。实际代码修正并加入原生/纯规则测试；Unmarshal 还清零已有 receiver。审核只读报告未冒称自己运行数据库。
- 新 HTTP 测试首编译有 unused net/http import，原失败保留，修掉后编译正常；PUT/DELETE commit 后 final revoke 拒绝的测试明确验证提交仍存在，避免“拒绝=回滚”的错误报告。
- native3 320 PASS；native4 321 PASS，在原 Seed HTTP helper修复期间 non-owned源码改变；native5 补实际 SQL129 overflow/recovery 后 321 PASS，Community non-owned源码变化。全部范围功能与 owned freeze通过，未将 non-owned变动当整仓稳定。
- migration1 实际四种非空拒绝和 empty down/reapply 都通过，但初版观测器在未运行 Go 时静态填 exit0；该结果不作为 Go 证明。修正为 null/NOT_RUN 后独立 migration2 重新验证所有条件。历史结果保留，不重写来隐藏错误。

## 当前证据

命令：`pwsh -NoProfile -File work/v5-age051/verify.ps1 -Round native6`。真实 fresh001–068 + 三开发 seed +八类旧非空源；321 Test PASS/0 FAIL-SKIP、test/vet/build0、11 owned与453观察Go源稳定、完整 public 原行/旧升级保持/ownDB DROP。JSONL 中 OrgMemory111事件包含纯规则、父子测试；HTTP32、PG35为实际新范围事件，旧相关测试另计。非空private GET最终拒绝与自然expiry不泄漏已在此帧实际覆盖。

命令：`pwsh -NoProfile -File work/v5-age051/verify-migration.ps1 -Round migration2`。fresh001–068/旧全部 public 行保持；四种 nonempty down exit3且逐JSON原子相等，empty down/reapply0、actual060guard还原、ownedDB DROP；没有 Go 命令。

整库首帧 `pwsh -NoProfile -File work/v5-age051/shared-current068.ps1 -Label current068-full1 -Rounds 1`：8044 Test PASS、1 Test FAIL、1 package FAIL、0 SKIP，vet/build0、586 API源同帧、全public与旧非空升级保持、ownDB DROP。实际唯一失败是组织空reinforcement写入；原旧060 migration test在共享运行库down/up恢复了旧guard，从而覆盖068追加的PERSON条件。原行oracle未覆盖schema函数，因此不能依据其稳定称权限完整。只读报告full068-failure-review.md确证时间顺序。

根已登记旧060测试和新owned_migration_database_test.go两个精确scope，仅将该迁移测试在自身随机24hex自有库执行，真实全up和开发seed、真实fixture原断言保持；同步t.Setenv受测试生命周期控制，不影响其它package进程，当前PG无t.Parallel。cleanup先fixture、再恢复env/关自有池/核父库guard相同/DROP只成功创建的自有库；绝不在共享库事后恢复DDL。编译-run^$0仅编译证明。完整068三轮默认并发已结束，并增加每轮最终public函数、trigger和constraint内容oracle；真实第三轮Outbox失败与后续诊断见下方，不用-p1/过滤/skip掩盖。

当前任务仍由 root 核证后更新状态。没有 Organization Memory 客户端UI、真实IdP/CSSA/活动授权、自动认知、外部部署与运营证据；不把这一批个人编辑 APK 或旧066真机API当068组织验收。Closed Pilot/Consumer Beta均NO。

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
