# Agent City History V5

2026-10-03。AGE028 的仓库内人类私人管理合同。来源：`docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md:1091`；前置 [Context Graph](CONTEXT-GRAPH-V4.md)、[Memory](AGENT-MEMORY-ARCHITECTURE.md)、[认知 ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)。最终检查结果见 [本轮审计](../research/BIRDTIE-V5-AGE-028-AUDIT.md) 与独立证据。本页不代表新增 UI、部署、客观事实核验或认知读写许可。

## 1 四类来源分开

| 类别 | 当前原生来源 | 固定中文含义 |
| --- | --- | --- |
| CURRENT | 本人 `person_contexts` 的 CITY / current 私密声明 | 本人声明的当前城市（非实时定位） |
| LIVED | 本人明确写入的 EXPLICIT CITY Memory | 本人自述曾居住于此（未经核验） |
| VISITED | 独立 EXPLICIT CITY Memory | 本人自述曾到访此城（未经核验） |
| INTERESTED | 独立 EXPLICIT CITY Memory | 本人明确表示对这座城市感兴趣 |

当前城市不是实时位置；到访不是居住；感兴趣不是已经到访。同城后三类可并存。CURRENT 仍由原 Context 接口管理，新模块不写第二份 current Memory；切换当前城市不删除三类声明。

home / past / destination 的原义保留，不自动转换；PrivateCityHistory 文本、普通 CITY / HISTORY Memory、收藏、Moment、报名、活动结束、搜索或图片数量都不生成上述类型。内容未知保持未知，空结果不证明没有现实经历。

## 2 单一 Memory 账本

新 `agentcitymemory` 仅为原 `agent_memories` 提供严格 typed convenience bridge。三种历史人工声明的结构为 `schemaVersion=agent.city_declaration.v1`、原 CityID、闭集 kind、`basis=SELF_DECLARATION`。MemoryType=CITY，sourceType=EXPLICIT；confidence=1 只表示本人直接填写，不是事实可信度或概率。固定中文 summary；key 为 `city.v1.<CityID SHA256摘要>.<kind>`，不包含另一份身份或权限。

写 input 精确接收 expectedVersion / cityId / kind / visibility / validUntil，Memory UUID 是稳定地址；禁止 CURRENT 写、未知字段、重复 / 大小写键、null、owner / Agent / verified / consent / confirmed / 日期 / 推断字段。声明 PRIVATE 或 AGENT_ONLY 都仅支持本人当前审阅，不授予机器使用。期限复用原 Memory 最长365天；UTC年份和微秒精度规范化。

CAS、幂等 retry、独立版本与 tombstone 使用原事务 helper；不增加表、迁移、UserProfile副本、MemoryEvidence 来源或模型 tool。相同 ID 不允许跨 owner、City、kind 或替代普通 Memory。typed 删除在 namespace 锁下先绑定期望版本，再由原 DeleteOwnMemory 最后 CAS；合法 tombstone retry 只接受相应原版本。删除清空正文与 structuredValue，旧 ID 不复活；City hidden / expired 不阻止本人删除。

到期读只隐藏对应声明，不修改版本；原 ACTIVE key 在物理状态仍占地址，未实现后台自动过期 / 清理，更新原 ID 或 tombstone 后新 ID 是现有管理路径。

## 3 时间与真实来源版本

CityID 沿用 `cities.id` 字符串；Context / Agent / Memory ID 是各自 UUID，不能互换。

pc / contexts / city_contexts 没有本轮可复用的 expires_at 或统一 revision。CURRENT source token 仅绑定实际声明行 canonical内容、xmin 与原 Context 行；revision=0，不假造1或 updated_at。它是当前数据库内部失效标记，非长期 event version；VACUUM FREEZE、restore、跨数据库不承诺同 token 稳定。Memory source 使用自己真实正 version，不借 Profile metadata 版本。

`recordCreatedAt` 是原行保存时间，Memory sourceUpdatedAt 是原 updated_at；CURRENT 不填造 sourceUpdatedAt / validUntil。没有居住开始、结束、到访日期；返回 `historicalDates=NOT_PROVIDED`。城市 updated_at / verified_at / time_zone、Moment occurredAt、报名或活动结束时间均不填补个人历史日期。

City 有真实 expires_at，必须在当前检查中核验。person_contexts 中所有本人 CITY/current 关系都计数，包含指向隐藏或到期 City 的关系；大于1拒绝不一致源，不能过滤后猜选唯一。当前源缺失保持缺失，非当前关系不补位。

## 4 人类当前读与撤销

实际 PG bridge 使用 SessionDigest + PERSON WorkspacePrincipal；事务明确 READ COMMITTED / UTC。当前 active account、精确 active PersonalAgent、matching metadata 与本人源相交；metadata 缺失拒绝，Memory 不重新生成控制。锁序复用原 session / account / Agent → metadata；等待 metadata 后重新检查真实会话时钟。

每次 payload SQL 同帧读取当前身份、已发布且未到期 City、所有 currentCount、确切声明 token 与本人的当前 EXPLICIT Memory。重复当前读取比较 snapshot；返回前真实 PGclock 检查有限租期，取消上下文不给结果。新查询不依赖旧 actorID-only Context DTO 或只过滤发布状态的 GetCity。

投影最多4个不同类别，包含最少原生源地址 / 版本和固定中文说明；没有正文、媒体、坐标、第三方 Profile、概率、日期推断。短租期最多2分钟，与当前 session absolute / idle、City期限、选中 Memory期限取最小值。pc自身无期限，不伪造 TTL。

SnapshotID 是内容地址，不是签名或许可。服务器组装时另留不序列化原 issuedObservedAt / issuedExpiresAt / issuedSnapshotID，拒展示字段被改动或人为续期；IssueProjection 只组装 shape，不核验 native来源。Projection 拒 JSON注入，Revalidate 仍必须重新查当前真实身份 / City / 全源、原版本 / token和旧租期。换主体 / session / metadata、源删除 / 重建、类别改变、City或声明失效均不能复用原收据。最终源 SQL 是允许的读取线性化点，不承诺交付后现实世界不能变化。

## 5 写后期限边界

写入锁当前 City、原 metadata、目标 Memory；真实 Memory 行等待后由原 helper再次规范 deadline。写后最终 SQL 验证 City仍发布 / 未到期、本人会话、目标 Memory精确版本和期限；失败整体回滚。City期限会随墙钟前进，持有 row lock 不意味着永不失效。

删除采用原领域 CAS，不复制 executor 或 approval。用户或模型填了 confirmed / verified，或配置 AGENT_ONLY，都不能获得 source / purpose 权限。认知 MemoryReader 当前仍 Unavailable；本轮没有自动 CITY 推断、Org / Business 城市记忆或模型出口。

## 6 交互与未实现范围

适用 UX-CHECK-04、06、08、10、11、12、16：明确本人身份 / 版本、原管理领域、当前来源与迟到拒绝、中文、重试和真实证据；本轮只实现 Go服务，不新增页面 / HTTP路由。UI整改、移动端与辅助技术、Flutter / 真机均 NOT RUN；后续接入应提供私人设置直接管理路径，不把 Now 聊天自由文本静默转成事实。

客观定位 / 到访 / 居住核验、个人历史日期、认知用途 / 推断接纳、真实试点和部署没有本轮证据。Closed Pilot / Consumer Beta 仍 NO。

## 7 2026-10-05 原任务回归复核

`full-actn001-native38ae` 真实整仓失败保留原字节：`session_absolute` fixture 用三个独立 `clock_timestamp()` 生成时间，触发原 `sessions_idle_expiry` CHECK；现改为同一 MATERIALIZED PG时刻，真实过去的 absolute/idle 截止相同，原 Forbidden 与零 payload 断言不变。不修改 Session constraint、生产时间或权限。

同轮 RR pool 用例最终读返回 `ErrInvalid`，原日志未包含失败时 PG时刻或具体形状条件，原因仍为 **UNKNOWN**。原 reader 两次来源读取及 issued shape 后再次获取 PGclock，禁止观测时刻晚于最终 PG时刻、禁止过期或续租；本轮不将时刻取最大值、不重试后声称原错已解释，也不改宿主/数据库时钟。独占诊断副本100次 RR、未插桩实际源码100次 RR 均通过，仅证明该范围未复现，不能证明原故障原因。确定性单位测试补充观测前/期限前/期限当时的严格校验，人工时间输入不称真实 PG回退证据。

本轮结果、原失败、源码 SHA、完整旧数据/可见目录与清理收据在 [独立修复证据](../testing/evidence/agent-city-history-2026-10-03/repair-2026-10-05/README.md)。根代理依据最新整仓核验决定任务状态，历史通过不覆盖新失败。本修复没有 Flutter、真机或现实经历核验。


## 2026-10-05 原AGE028本地验收重新核定（38la）

根重新读取原source/AC和实际六个City源码，并独立解析当前root-whole2：132 CityMemory PASS/0FAIL-SKIP、18顶层（纯域102、真实PG30），包含真实四类分离、当前来源、CAS/跨主体/元数据与会话、删源重建/歧义、真实行锁跨City期限、metadata锁跨Session期限、RR默认pool；六源before/after/current SHA一致。完整10797 Go测试以及vet/build/两个CLI均0，307实际自有隔离库不存在。凭据work/v5-age038-resume/city028-original-ac-root38la.json。

按原CODE_AND_LOCAL_VERIFICATION完成四类native服务语义；原首因UNKNOWN继续保留，没有用后续绿重跑推称解释或修复。此前partial_reason附加解释旧RR首因的要求与原四类功能AC分别记录，未删除历史。CURRENT来自原本人Context；LIVED/VISITED/INTERESTED为独立本人自述，不是客观核验。未新增HTTP/UI、历史日期、客观到访/居住、模型权限；没有直接SHOW transaction_isolation或持有RR读事务并发变源的新验证。手机/TalkBack/性能NOT_RUN。Closed Pilot/Consumer Beta NO。
