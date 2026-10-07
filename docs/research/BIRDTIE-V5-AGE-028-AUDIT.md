# AGE028 City History：审计与实施记录

2026-10-03。来源 AGE028 四类别及队列背景；本轮只贡献该任务独占 scope，根代理负责状态和共用报告。原双只读审计在 `work/v5-age044/preaudit-age028.md` 与 `preaudit-age028-review.md`，完整语义在 [City History canonical](../architecture/AGENT-CITY-HISTORY-V5.md)。

## 已有 / 缺项

- 033 / person_contexts 已有 current/home/past/destination 私密声明，未有 lived/visited/interested 独立信号。Declaration DTO没有 revision / 时间，原 Store actorID-only不作为新exactAgent/current source证明。
- 原 EXPLICIT CITY Memory有本人会话 / Agent / 版本 / 期限 / CAS / 删除；privateCityHistory只是文本。027 地点 typed声明桥可作为原生事务设计参考，不能从地点来源推断城市到访 / 居住。
- City目录 GetCity只检查 published；新桥另核实际 expires_at。pc/context/city_contexts 没有可借用TTL。未发现同职责 City History canonical，新增唯一领域说明，无新DDL。

## 实际实现

新 `internal/agentcitymemory` 的严格四类投影 / 三类人工输入、固定中文 provenance、有限租期、内部原 issued收据与拒JSON；新 `postgres/agent_city_memory.go` 真实 self-binding / 原生源 SQL / typed Memory写删。CURRENT保持原Context真源，后三类使用原Memory账本，纯源shape永不授权。

本轮不触碰旧身份 / Profile / Context / Memory模型或SQL，不映射旧文本、其他relations、收藏、Moment、报名；没有历史日期、事实核验、模型处理、HTTP / UI / 部署。

## 首次结果与修正

| 记录 | 实际结果与分类 |
| --- | --- |
| `work/v5-age028/domain-first.jsonl` | 89 Test PASS，0 FAIL/SKIP；仅纯域，不是native来源证据 |
| `domain-second.jsonl` | 96 Test PASS，0 FAIL/SKIP；新增 UTC边界 / issued原收据安全形状 |
| `compile-first.jsonl` | 编译+纯域通过，但21 Native Test SKIP；缺DSN，不能作实库验收 |
| `native1-runner-error.log` | 旧PowerShell把PostgreSQL正常NOTICE转异常，未进入功能测试；清理保留 |
| `native2-result.json` / raw | 自有001–064+3seed；121 Test PASS、3 Test FAIL（两expiry fixture+parent）及1 package FAIL，0 SKIP，vet/build0，6源码稳定、全public原行相等、ownDB DROP |
| `native3-result.json` / raw | 新随机001–064+3seed；126 Test PASS（96 pure +30 native含父/子），0 FAIL/SKIP，vet/build0；6源码稳定、全public完整原行相等、ownDB DROP |

Native2两失败是在测试把新Session截止写到created_at之前，违反原003约束，不是生产鉴权通过或泄漏；修为真实合法历史created_at / expiry记录，不弱化DDL / 原断言。其余场景含确切 Memory行锁等待跨City deadline、零payload及完整owned rows回滚已实际运行，raw保留。

根只读代码审查另发现两阶段typedDelete未来expectedVersion可能与间隙合法换Memory类型混淆；保留 `review-delete-precondition-before.go`，按原namespace锁下先绑当前expected版本，再交原Delete最终CAS。此为代码审查修正，没有注入race的实测RED，不冒称曾泄漏。补真实future expected拒绝、普通Memory合法改类后typedDelete拒绝与原行不变。

Native3 包含 issued原lease篡改、UTC年份、typedDelete前置版本、合法expired sessions、实际metadata锁等待跨session期限及真实RR默认pool。最终6源码已经冻结，成功 / 首次失败 / 源副本 / 清理与收据均在本任务独立 evidence。根独立review与共同完整Go回归另记，不把126 scoped结果视为全仓通过。

## 原生验收边界

真实来源正例：原DeclareContext当前城市、新typed三人工CITY Memory、独立连接持久读取、nativeCAS/重复/删除、当前源移除重建、private自述零自动生成。真实负例：另人/Org/Biz/workspace、session/account/Agent/metadata、City状态期限、多current和旧快照、未来版本误删、实际Memory或metadata锁等待跨期限。默认RR pool通过桥的明确RC行为，不将纯fixture称曾证实RR越权。

所有数据是本地合成、自有随机身份 / City / session；原生DB/领域服务检查可以验证代码，不是生产登录、现实居住 / 到访、CSSA授权或试点。

## 缺项与外部门槛

无新DDL，down/reapply不适用；无客户端/路由修改，Flutter/真机/可访问性 NOT RUN。客观城市经历、个人历史日期、模型用途resolver与UI尚未实现；没有后台expiry清理或现实定位。Closed Pilot / Consumer Beta NO；本次不联系 / 部署 / 发布外部服务。


## 2026-10-05 原AGE028本地验收重新核定（38la）

根重新读取原source/AC和实际六个City源码，并独立解析当前root-whole2：132 CityMemory PASS/0FAIL-SKIP、18顶层（纯域102、真实PG30），包含真实四类分离、当前来源、CAS/跨主体/元数据与会话、删源重建/歧义、真实行锁跨City期限、metadata锁跨Session期限、RR默认pool；六源before/after/current SHA一致。完整10797 Go测试以及vet/build/两个CLI均0，307实际自有隔离库不存在。凭据work/v5-age038-resume/city028-original-ac-root38la.json。

按原CODE_AND_LOCAL_VERIFICATION完成四类native服务语义；原首因UNKNOWN继续保留，没有用后续绿重跑推称解释或修复。此前partial_reason附加解释旧RR首因的要求与原四类功能AC分别记录，未删除历史。CURRENT来自原本人Context；LIVED/VISITED/INTERESTED为独立本人自述，不是客观核验。未新增HTTP/UI、历史日期、客观到访/居住、模型权限；没有直接SHOW transaction_isolation或持有RR读事务并发变源的新验证。手机/TalkBack/性能NOT_RUN。Closed Pilot/Consumer Beta NO。
