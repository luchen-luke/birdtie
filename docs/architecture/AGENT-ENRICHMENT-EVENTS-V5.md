# Agent Enrichment 事件契约 V5

2026-10-03；BT-V5-AGE-064 的唯一增量规范。来源：`docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md` 的 Enrichment Event Bus 要求、既有 `AGENT-INTELLIGENCE-RUNTIME-V5.md`、AIR014 的 `internal/agentevent` 和真实 PostgreSQL 领域实现。扫描 `docs/architecture` 后未发现独立 Enrichment 事件 canonical 文件；复用唯一 `agentevent`，不另建总线。

## 本轮实现范围

**CODE_AND_LOCAL_VERIFICATION**：真实可调用的内部 metadata producer/revalidator。原需求列出 **12** 个 Enrichment 类型；加既有 UserQuery，唯一目录共有 **13** 类型。10 个 Enrichment 类型有当前保留的原生来源，ActivityCompleted、PlaceVisited 明确 Unavailable；UserQuery 继续复用 AIR014。

`Store.ProduceAgentEnrichmentEvent` / `RevalidateAgentEnrichmentEvent` 复用原 Producer、严格 Envelope、稳定 ID 与当前来源校验。旧 `ProduceAgentEvent` 入口仍只解决原 MomentCreated / UserQuery，调用新类型失败关闭。未自动接入业务写入：事件事务 hook、outbox、投递、消费者、可靠去重账本和真正 Enrichment 处理闭环由 AIR015 等后续任务处理。

所有事件 `processing_status=UNAVAILABLE`。目录、版本、当前会话、人工明确声明与测试成功均不授予 Memory 写入、分析、模型、A2A、工具执行或数据出口权限。

## 当前状态快照语义

所有 descriptor 的 `ReceiptSemantics=CURRENT_RETAINED_STATE`。EventType 表达本地目录分类，**不是历史动作证明**：

- RSVP going 不证明到场；cancelled 可以来自取消 pending 请求，ActivityLeft 不证明先参加后离开。
- Community active 可以来自创建 owner、邀请批准、原生加入或角色更新；CommunityLeft 可以来自本人退出或管理员移除。`updated_at` 不证明是哪一次历史动作。
- `actor` 是当前生产者已认证 Person，会话之外的历史操作者不被编造。
- MomentDeleted 只表示原生 WithdrawMoment 保留的 withdrawn author/version 控制记录。物理删除后没有保留源就拒绝，不返回旧正文。
- ProfileUpdated 表示当前普通 Profile 源；未记录历史差分就不声称已核验某次历史编辑。

当前来源重新读取不能成为以后长期访问权。未来消费者须在自己事务、当前主体、source-purpose、撤权与出口边界内再次解析来源。

## 闭集目录与真实版本

| 类型 | 原生来源 / source ID | SourceVersion | `occurred_at` 与要求 |
| --- | --- | --- | --- |
| MomentCreated | 自己 private draft Moment / moment ID | 原生 positive revision；复用014 | 实际 created_at；非空文本 |
| MomentUpdated | 自己 private draft Moment / moment ID | 原生 revision > 1 | 实际 updated_at；非空文本 |
| MomentDeleted | 自己 private withdrawn Moment / moment ID | 原生 revision > 1 | 实际 updated_at；只失效控制，不读取旧正文 |
| ActivityJoined | 自己 going participation / participation ID | UPDATED_AT_DIGEST | 实际 updated_at；当前活动公开发布、当前可见、未取消/过期/结束/blocked |
| ActivityLeft | 自己 retained cancelled participation / participation ID | UPDATED_AT_DIGEST | 实际 updated_at；cancelled_at 存在；只失效控制，不取目标正文 |
| ActivityCompleted | 无原生 attendance/completion 事实 | UNAVAILABLE | 不从活动结束、Plans 投影或 RSVP 推断；不调用 resolver |
| PlaceSaved | 自己 place 类型 saved_item / saved_item ID | CREATED_AT_DIGEST | 实际 created_at（无原生 updated_at）；地点与城市当前发布、地点未过期 |
| PlaceVisited | 无原生明确 visit 事实 | UNAVAILABLE | 不从定位、地图、收藏或活动推断；不调用 resolver |
| CommunityJoined | 自己 retained active membership / membership ID | UPDATED_AT_DIGEST | 实际 updated_at；社区和 owner 当前有效、发布、未过期/blocked；hidden 的真实 active member 可读本人的 metadata |
| CommunityLeft | 自己 retained left membership / membership ID | UPDATED_AT_DIGEST | 实际 updated_at；只失效控制，即便目标后来 hidden/archived 也不返旧目标正文 |
| ProfileUpdated | 自己 user_profile / Person account ID | UPDATED_AT_DIGEST | 实际 updated_at；public/private 均只有本人的 metadata 源，不默认公开任何字段 |
| PreferenceUpdated | 自己 agent_private_profile / exact Personal Agent ID | 原生 written_profile_version > 1 | 实际 updated_at；written <= 当前 metadata aggregate version；至少一个真实非空偏好数组，严格私密字段校验 |
| UserQuery | 自己 active/completed native Task / task ID | 原 QueryVersion UPDATED_AT_DIGEST | 实际 Task updated_at + 当前 canonical Task digest；非空本人 query；不携带会话正文 |

Preference 的 six arrays 为 personalPreferences、socialPreferences、preferredActivityTypes、travelPreferences、interactionPreferences、languagePreferences。仅 availability/privateCityHistory/agentNotes 不被称为偏好更新。原生 clear 物理删除 row 后失败关闭；没有虚拟空确认、tombstone 或假 revision=1。字段可见性变更增加 metadata aggregate version 时，不替换原私密内容的真实 written_profile_version。

## 版本接口与兼容

```go
SnapshotVersion(source SourceType, kind VersionKind, sourceTime time.Time,
    canonicalContent []byte) (SourceVersion, error)
```

合法组合只有 ParticipationSource / CommunityMembershipSource / ProfileSource + UPDATED_AT_DIGEST，SavedPlaceSource + CREATED_AT_DIGEST。真实 sourceTime 与当前 server-loaded canonical row（`to_jsonb(row)`）加入独立 namespace；结果为 lowercase SHA256 opaque token，revision=0。它不是单调 counter、CAS、当前访问许可或历史事实证明。私密 Profile/Task 字段只在本地校验或计算 fingerprint，不进入 Envelope、日志或任何 provider。

旧 QueryVersion namespace/行为与 StableEventID 算法保持；旧 schema=`air.event.v1` 的原两个类型和 wire 字段保持。新增 descriptor 属性只在 Go catalog，不给旧 envelope 添加 required 字段。新增 CREATED_AT_DIGEST 是闭集 variant；旧版本 reader 会拒绝未知类型/variant，新 reader 继续接受既有两个类型，不能将 reader 的旧拒绝误称全部新类型都受支持。

版本 tagged union 严格为 kind+revision 或 kind+token，拒绝零/null 的多余 variant、未知/重复/大小写不同字段、正文注入、未知类型、过大或 trailing 数据。known unsupported 类型不能通过 fake resolver 或 wire fixture 制造有效 envelope。

## 当前身份与失效保护

新来源以同一条最后当前 SQL 读取：session digest → 当前 active Person → exact active Personal Agent → 当前 Person-bound native metadata → 自己的原生源及当前可见状态。请求只有 source ID/类型与 operation/trace/causation ID，不接受 owner/Agent/subject override。匿名、Organization、Business、错主体、撤权、绝对/idle 到期、关闭的 dev_phone、无 metadata、源删除或状态不匹配均拒绝；不恢复 metadata/visibility。

`occurred_at` 精确来自上表源时钟，`received_at` 为解析时数据库真实 clock_timestamp；过未来/无效源时钟拒绝。expiry 永远是原生源时钟 +15分钟，重试不能延长。稳定 UUIDv8 绑定类型、typed Person、exact Agent、逻辑 operation 与真实 source version，不是已持久化可靠去重。重校验再次解析当前源，版本、身份、状态与时间线不匹配就失效。

## 验证与限制

实际命令、首轮失败及修复、纯契约和真实 PostgreSQL 结果、逐表源保护、源码 SHA 与可复现 runner 记录在 `work/v5-age064`，最终归档位于 `docs/testing/evidence/agent-enrichment-events-2026-10-03`。测试数据全部标明本地合成，所有随机 Person/Agent/社区/活动/地点仅为对应 fixture 所有；不借用 CSSA 身份、真实授权活动或手机业务数据库。

本任务没有 Flutter 界面、DDL、部署或外部请求变化。无消费者/出口/处理授权；Completed/Visited 无实源；不声称实时总线或消费者产品可用。Closed Pilot / Consumer Beta 仍 **NO**，保持原生产身份、现实供给授权、HTTPS/地图、部署调度/日志、值守与真实 A→H 发布门槛。
