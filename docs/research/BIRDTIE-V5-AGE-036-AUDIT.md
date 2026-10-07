# AGE036 Current Context 实际接口审计

2026-10-03。官方仓库 `D:\Project\birdtie`。来源为 [AGE 原文 AGE-036（1305 行起）](../product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md) 与唯一队列 `BT-V5-AGE-036` 的全部 acceptance / verify；未写队列或共用报告。没有 Civu 或旧 Work 目录代码复用。

## 真实来源与背景假设修正

| 原接口 / 表 | 实际能力 | 本项处理 |
| --- | --- | --- |
| `contextgraph.Declaration`、`postgres/person_contexts.go`、033 `person_contexts` | Person 自述 CITY current / home / past / destination 等；本人当前城市原子替换。DTO没有版本/期限；表只有 created_at | 只读当前本人 private CITY 声明，不当GPS、居住或机构资格；使用真实 created_at + 当前原行摘要 |
| `agentworkspace.Task`、`postgres/agent_workspace.go`、009/020/033 `agent_tasks` | 原 task ID、typed principal、query、filters、conversation、状态、created_at/updated_at；GetTask 只按 owner 获取 | task 没有持久 expires_at；只读 ACTIVE Person task，检查原所有权/Context/城市/实际时间，今晚按其真实 updated_at 所在当地日期锚定 |
| `foundation.City`、`postgres/catalog.go`、`cities` / `city_contexts` / `contexts` | published 城市、真实 time_zone、源 updated_at / verified_at / expires_at、城市Context状态 | 复用原 city/context ID与时区；源未发布/过期、Context暂停、未知/Local时区拒绝；未来或无穷 verified_at 不称已验证 |
| `agentprofile.PrivateAccess`、原 Authenticate / native Agent / 053 metadata | 当前会话解析本人、active Person、精确 Personal Agent、native metadata 绑定 | 不让 caller Agent ID 或 JSON Verified 成为授权；缺失/撤权/跨主体 fail closed，不创建缺失metadata |
| AGE004 `agentmemory.Record` 与 056 | 单一长期 Memory ID、独立版本、有效期；EXPLICIT仍为本人声明 | 实测先保存一条长期Memory，再读取/复核短期上下文，原Memory及来源完整行不变；没有任何Memory写入口 |

队列背景句“声明/请求/task有expiry”不能作为现有事实。036 新增的是**本次读取/请求的有界快照租期**，不是给旧 Task / Declaration 添加持久 expiry，也不修改原状态。短期“当前在某城市 / 今晚找活动”不自动成为长期 Memory。

## 实现与权限边界

在独占新包内实现真实 pgx 当前本人 resolver 与可调用 `ReadOwn` / `RevalidateOwn`；未改旧 postgres.Store、HTTP、main/server、迁移或源领域。认证复用原 Authenticate，因而会刷新原会话 idle 期限；“只读”限定源领域数据，不能声称所有 SQL 均无写入。

每次 native load 使用 ReadCommitted / READ ONLY transaction 和 SET LOCAL TIME ZONE UTC，再用最后 payload SELECT 在同一 statement snapshot 中同时核验当前 session / active Person / exact active Personal Agent / metadata / resource owner / published非过期city / active CityContext / 当前源行。第二个 payload SELECT 与初始读的源、权限 fingerprint 一致才返回，期间改删/撤权丢弃内容。精确线性化点是最后 SQL statement snapshot；不承诺其后网络数据可被追溯收回。

源版本复用 `agentevent.SourceVersion`：Task使用真实 updated_at 的 QueryVersion；现有 SnapshotVersion helper 只允许其已登记来源，不支持 person_contexts，因此本项不假称 ProfileSource、不改旧事件注册。Declaration / City 由新包专用 domain hash 绑定真实 created_at / updated_at + 原行；PG xmin 仅作为有界 opaque mutation token，检测同 ID / 同时间 / 同内容重建或修改，不是业务单调revision、CAS或访问凭据，也不证明历史转移。

服务实例HMAC绑定完整快照、原selector、源/权限fingerprint和实际066配置generation；JSON不能构造Request或可恢复Snapshot。Revalidate不续租、不把旧tonight解释成今天；同人另一会话、主体/Agent/metadata变化、开关OFF再ON、跨服务实例、正文/期限/source/model标记篡改均拒绝。快照没有Bearer、长期Memory或原private资料。人类本人读取只供 HUMAN_SELF_REVIEW；认知/模型端口硬Unavailable，Organization / Business / Community 不继承个人声明或Memory。

## 实际验证与自审修正

- 初期代码自审发现不同PG连接 TimeZone会让 JSON 时间字符串与源摘要假冲突，以及未来/无穷 verified_at 风险；根代理指出后在首轮真实PG前修正。未虚构RED运行。
- 首轮 staging 独立001–057/3seed库：95 PASS、0失败/测试skip、vet/build0、全public旧完整行相同、DBdrop。
- 第二轮 staging 独立库：105 PASS，同样0失败/skip、vet/build0、全旧行相同、DBdrop；补实际租期过期、metadata重建、同人不同session、source freshness等。
- 最终 production 固定5源，另一个独立库：107 PASS、0失败/skip、vet/build0、全旧行相同、源码哈希稳定、DBdrop；额外拒绝服务器Local时区，不能猜城市时区。

真实Asia/Shanghai与America/New_York两连接被强制用于初始/final读取，重读及Revalidate通过，SET LOCAL后连接全局时区保持。7个真实并发屏障分别在第一次读完成后、最后payload SQL前提交session/Agent/声明/task/flag撤权变化、deadline跨期或取消，全部无数据返回。城市当地午夜、DST 23/25小时日、短期租期上限与相对日期有纯模型正反例；这些模型例不替代真实SQL证明。

证据、源码快照、初版95/105与最终107差别、可复现命令见[正式归档](../testing/evidence/agent-current-context-2026-10-03/README.md)；唯一实现合同见[canonical](../architecture/AGENT-CURRENT-CONTEXT-V5.md)。根代理负责独立全Go与队列DONE，不将此目标结果称全API结果。

## 未实现 / 未验证

无新UI/Settings、HTTP路由、统一AGE033 ContextBuilder、cognitive/current-purpose模型授权、provider、自动Memory提升/learning、外部动作、视觉或A2A。没有真实IdP/位置/组织/活动授权、正式部署、真机/辅助技术、race instrumentation或运营证据。适用 UX-CHECK-06/08/10/11/16 为领域边界核验；不声称界面整改或普通直接路径端到端已验。Closed Pilot / Consumer Beta 保持 NO。
