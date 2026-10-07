# AIR039 当前只读检索与匹配适配器

状态：2026-10-07 仓库与隔离本地原生验收完成（CODE_AND_LOCAL_VERIFICATION）；真实用户、生产身份与完整消费端联动仍未验收。唯一任务仍为原队列 BT-V5-AIR-039；本地完成不解除发布门槛。

来源：`docs/product/BT-V5-AIR-REQUIREMENTS.md` 的 AIR039、原 AIR037 注册表、原 typed ResultSet 与 New People 契约。适用 UX-CHECK-06/07/08/09/10/11/16；本文件补充接口边界，不建立另一套搜索、权限或任务体系。

## 实际消费路径

| 已有入口 | 本轮接线 | 权威读取 |
| --- | --- | --- |
| 原城市 Agent Task POST、本人 Task GET，FindPlace/FindPerson | `prepareNativeAgentResults` → `agenttool.ReadCurrentSearch` → `postgres.Store.ReadOwnCurrentSearch` | 同一原 Task/Session、`captureAgentResultProjectionCurrentTx` 的原七类领域投影；本适配器只允许 place/person |
| 本人 `GET /v1/me/new-people/candidates?sourceIntentId=<原ID>` | `ReadCurrentMatch` → `Store.ReadOwnCurrentMatch` | 原 `humanNewPeopleReadTx` → `findNewPeopleTx` → `newpeople.BuildResponse` |
| 普通活动 Task HTTP | 保留原 `ReadAgentResultProjection` 及末次 `RevalidateAgentResultProjection` | 原领域可见性包含已获准的邀请活动，不能改为仅 public 或删除其合法结果 |
| 既有 ModelRun/Planner `activity.search` / `activity.detail` | 原 AIR037/040 路径不改 | PUBLIC 活动及原模型 ACTIVITY schema、当前句柄、预算和最终事务边界 |

审计初案把普通 activity 也接到新 CurrentSearch；核对实际邀请活动范围与模型 PUBLIC 契约后修正为仅 place/person。这个调整来自已有权限差异，不通过改小验收或忽略失败收尾。注册 GET 单元明确验证 activity 不进入新适配器，邀请活动仍可见，最终原 ARP 重验失败则不返回数据。

`FindPerson` 是既有公开发现：当前公开资料、明确公开且确认的来源与原字段 ACL；不是 New People 双方 opt-in。`person.match` 只整理本人选择的有效非私密 FIND_COMPANION 来源，并复用双方 `person_new_people_consent`、双向受众可见性、封禁、关系及待处理请求检查。两条用途不能互换，也不自动邀请、建立关系或发消息。

## 注册表与协议

增量 READ 元数据为 `place.search.v1`、`person.search.v1`、`person.match.v1`，包含封闭输入/输出字段、风险、当前来源、权限、用途与无副作用语义；唯一 WRITE 描述仍为原 `sandbox.write`。活动两个原 descriptor 的 schema/行为保留。新元数据不是 HTTP 工具调用入口，也不扩大原 `DecodeProposal`、Planner 或 ModelGateway 的 ACTIVITY 限制。

地点/人物响应继续原 `typed-agent-results-v1` ResultSet：相同 EntityRef/详情/地图/动作 ID，由原投影生成当前标题、来源版本与获准坐标。Person 本轮原 producer 不生成精确坐标。动作提示仍为原 entityaction proposal，并非已批准或已执行；其有效期不得超过当前读取有效期。filters 仍通过原 `explicitResponseFilters`，不输出内部推断、原比较 ID 或未知字段。

匹配继续原 `newpeople.Response`：`source=RULE_BASED`、当前 `RuleVersion`、原 sourceIntentId/candidateIntentId/accountId、最多50个不同人物及截断标志。理由来自原规则，不是已验证身份、地理距离或模型概率。真实空来源返回非 nil 空数组；源不可见/失效、权限变化或读取失败不冒充空结果。

CurrentSearch/Match 的控制值、原 source seal 和 compound receipt 仅在进程内使用，JSON 编码/解码拒绝。Decision 只描述本次当前校验，不是权限批准；标签 `LOCAL_OWNER` 也不能授予独立机器社交、Memory/Task Context 或 MODEL_EGRESS 权限。原041未获准服务仍不可用，原044等级只能收紧；无人类明确操作或当前原身份时拒绝。

## 当前身份、来源与最终输出

- 服务端从真实请求和原 Task 捕获 actor、Session digest、原 ID 与完整 sanitized Task；不能从客户端工具参数、模型 confirmed 或 owner 标签重建权限。
- 原 Account/Session/Personal Agent/metadata 与当前 065 策略复用；源 Task 条件、城市、类别、时间、视口及比较条件仍由原读取守卫检查。本轮没有另建权限、效果或来源账本。
- 原 ACCESS SHARE 关系等待先于 Account/Session/Agent；新增 human helper 再取得原 `human-agent-policy` advisory，之后原 settings SHARE/bundle/xmin 指纹。与原 PutOwnPolicy 的串行键一致。这个强表锁可能短暂阻塞不同 owner 的策略写，性能与实际 PG 并发未测试；不声称修复了旧037/040其它路径的锁问题。
- 当前 policy/xmin、真实 Session/来源 ACL 与数据库时钟在同一最终 payload statement 检查。复用原 source HMAC 及同一原生 key 的 compound seal，不仅凭客户端 DTO、旧 Decision 或标题判断。
- 编码完成后，在写 HTTP 响应前重新取得同一原生当前来源，核对 source proof、owner/agent、策略指纹和期限；变更、ABA、撤权与迟到失败时不返回已编码内容，也不回退旧接口。原活动链继续自己原来的最终重验。

## 2026-10-06 历史阶段证据与当时未运行范围

独占原始结果、命令、目录、退出码、输入 SHA 与差异：`work/current-readonly-tools-2026-10-06`；交付索引：`docs/testing/evidence/current-readonly-tools-2026-10-06`。

原注册 GET 的两个适配器缺失 RED、action deadline mapper RED 与后续修复保留。最终新单位90 run/pass、21顶层/3包；11个精确旧相关顶层238 run/pass/4包。仅为 HTTP store spies、纯契约、实际 Tx helper 的 row/call-order spies 和 HMAC 拒绝测试；后者不执行 SQL、锁或 PostgreSQL ACL。记录的 wire 是注册 handler 的合成单位响应，不是原生数据库/真实人员或生产证据。

阶段09另有1项 fixture 失败：模拟 `clock_timestamp()` 给出 sub-microsecond 时间，违反原 `agentpolicysettings.ValidTime` 的 PostgreSQL微秒精度规则。仅把该 spy 时间截为微秒，保留原校验/顺序断言；原89/90失败历史不删除，阶段10为90/90。

NOT_RUN：新实际 PostgreSQL 查询/SQL执行/并发锁/持久化/撤权双序、原生 HTTP、真实供给与匹配、迁移/seed检查、整仓 Go/Flutter、vet/analyze/build、真机/辅助技术/完整UI、正式 IdP/provider/模型出网/运营部署/试点。本轮无新 DDL，未激活新的设备或服务版本。Closed Pilot / Consumer Beta 仍 NO。

## 2026-10-07 当前原生本地验收

证据 [AIR039 本地原生验收](../../work/v5-age038-resume/air039-native-checkpoint-2026-10-07/README.md)、`final-proof.json`、`final-current12/command.json` 与原始结果。最终3包133测试及子测试事件 PASS/0 FAIL/0 SKIP/退出0，新增7顶层45个叶场景；真实自有 PostgreSQL 从001–104迁移和3个开发seed建库，不触碰手机或生产数据库。

实际注册 Place/Person POST与本人Task GET经过当前端口，无旧接口回退；原ID/详情/share、真空、匿名/跨账号/未知ID、双向封禁、Account/Agent失活、source/Task/policy ABA、会话撤销及seal篡改均已验证。实际独立Match经过原来源与双方明确opt-in，失去可见性或期限时不返回旧内容，读取不创建邀请、关系、消息或保存。

精确application_name、locker PID、pg_blocking_pids、未获ShareLock及原SQL共同定位真正请求等待；数据库时钟实际越过策略/会话/来源期限后解锁，最终拒绝已编码旧数据。同owner原writer等待原advisory，不同owner原writer在INSERT的RowExclusiveLock等待表SHARE；原writer成功提交后重验。这个证据只覆盖两种受控并发，不等于完整吞吐、公平性或压力测试，也不证明旧037/040其他模型路径。

原活动链独立保留邀请ACL：实际Join人数0→1、Cancel回0；真实DB改变标题、容量2→1、起止时间+30分钟后typed与原DTO取最新值，未知UUID详情404，撤邀请+取消后真空且无旧ID。原Model ACTIVITY PUBLIC工具的三个实际原生用例也通过，未扩大模型权限、入口或确认语义。

原匹配仍保守绑定Session xmin：普通并发idle刷新会使旧receipt返回409，兼容限制未修复。去掉xmin的尝试导致原撤销/恢复ABA安全测试失败，已完全移除；最终所有既有API源码、迁移及旧测试SHA与上轮全量current06相同，仅新增原生测试文件。失败记录保留，不为续期放宽安全边界。原Task完成通知属于保存链；无副作用取基线在保存之后、READ之前，仍检查READ不能新增通知。

当前未知：真实IdP/provider、真实人员匹配/供给、完整UI/辅助技术/主题/性能、生产部署与A→H。上轮全量Go/Flutter/工具测试、vet/build及实际Debug手机Mapbox录像只引用其明确源帧；本轮未重跑全仓或新客户端构建。模型出网、真实Agent写、视觉/A2A保持OFF；Closed Pilot / Consumer Beta NO。
