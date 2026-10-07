# 活动参与信号 V5

2026-10-03；BT-V5-AGE-024 的唯一实现契约。来源为 [AGE 原文 AGE-024](../product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md)、原生 RSVP、[Enrichment 事件契约](AGENT-ENRICHMENT-EVENTS-V5.md) 和 [认知 ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)。未找到既有独立 Participation Signal canonical；本文件新增该只读桥的规范，不复制原生 Participation、Memory 或事件总线。

## 本地能力与保留边界

`internal/agentparticipationsignal.Reader.Collect` 从实际 PostgreSQL 原生记录产生本人当前参与状态的最小元数据；`Reader.Revalidate` 再读当前源并拒绝失效回执。它可直接由可信 Go 调用方运行，本轮未接 HTTP、业务写 hook、outbox 或候选消费者。

原文要求 join activity 可以产生 participation signal、单次不能得出稳定兴趣。本轮只落实可调用的信号及当前校验：**CODE_AND_LOCAL_VERIFICATION**。队列 goal 提及的自动候选消费尚未实现；没有通用机器用途授权 resolver、接纳流程或运行端口，不以本轮信号代替那些能力。

`processing_status=UNAVAILABLE` 表示未开放自动 enrichment/分析/Memory/模型处理，不表示本地元数据读取函数不可调用。生产身份、真实活动、真实到场来源、部署与发布验收不在本轮证据中。Closed Pilot Ready 仍为 **NO**。

## 状态与事实

| 原生保留状态 | Signal kind | 能说明什么 | 不能说明什么 |
| --- | --- | --- | --- |
| `going` | `RSVP_GOING` | 本人当前报名状态 | 实际到场、完成活动、稳定兴趣 |
| `pending` | `RSVP_PENDING` | 原生表当前保留的待处理形状 | 已获批准、已经加入、实际候补名次 |
| `cancelled` | `RSVP_CANCELLED` | 本人当前保留的撤销状态，用于失效 | 先到场再离开、活动内容仍可见、实际出席 |
| WAITLIST / attendance / completed | 不支持 | 无原生权威来源，失败关闭 | 从结束时间、Plans、Moment 或定位补造事实 |

`receipt_semantics=CURRENT_RETAINED_STATE`；状态和 `updated_at` 不证明历史操作的发生者或经过。取消 pending 也可能形成 cancelled。`attendance` 和 `stable_interest` 固定为 `UNKNOWN`，不会生成长期记忆、画像或兴趣得分。

当前 `Store.JoinActivity` 实际生产 going，`CancelParticipation` 实际保留 cancelled。pending 的测试使用原生合法 SQL 状态形状，不能称为已实现真实申请审批流程。

## 精确调用合同

```go
NewReader(pool *pgxpool.Pool, devPhoneEnabled bool) *Reader
(*Reader).Collect(ctx context.Context, access agentevent.Access, request Request) (Signal, error)
(*Reader).Revalidate(ctx context.Context, access agentevent.Access, old Signal) error
Validate(signal Signal, now time.Time) error
```

Request 只含 `ParticipationID` 和 `LogicalOperationID`；二者只是选择器和幂等关联，不是权限。Access 复用服务端已计算的会话 SHA256，不接受客户端自述身份、confirmed 或角色。nil pool、缺配置返回 Unavailable；匿名、错主体、撤销、资源不可见返回 Denied；失效源返回 Expired。nil/取消 context 不返回载荷。

`Validate` 只检查结构、闭集状态、固定未知断言和寿命，不能自行证明数据库当前权限。Signal 的 JSON 只允许输出；直接反序列化始终拒绝并清空结果，不能把 JSON 变为服务器源或许可。可信调用方仍必须调用 Revalidate，不把 Signal ID、digest 或 Validate 成功当成授权。

devPhoneEnabled 的布尔参数由可信服务环境决定，默认调用为 false；明确开发路径不证明生产登录已实现。本轮未修改原生认证或新增绕过路径。

## 当前来源校验

在显式 UTC、READ COMMITTED + READ ONLY 事务中，两次完整 SQL 均检查；不继承连接池或数据库的 REPEATABLE READ 默认设置：

1. 当前会话未撤销、绝对及 idle 期限有效、当前 active Person。
2. 精确 active Personal Agent、原生 owner/type 相符的 Profile metadata；无第二个 active Personal Agent。
3. Participation 确实属于该 Person；来源状态和取消时间形状有效。
4. going/pending 的 Activity/City 仍发布、当前未取消/结束/过期、正原生 revision、当前 `birdtie_activity_visible_to` 可见。
5. 当前主办方账号有效、原生 organizer 资格仍有效；Person active、Community active/published、Organization 及其账号 active、Business verified/active 及其账号 active。
6. 与当前 host 不存在任一方向的 block。

第二次 SQL 是本次读取的权限线性化点；源版本、主体、Agent、活动绑定变化会拒绝，context 在最终返回前再检查。该点之后发生的新撤权不能靠旧回执抵消，后续消费必须再次核验。没有长期授权或防止所有未来变更的保证。

cancelled 分支只读取本人保留的失效源，不要求旧活动仍可见，也不输出 Activity ID、revision、目标 digest、名称、正文或坐标。Source ID 是本人 Participation 的控制引用，仍需当前会话、Agent、metadata 和来源归属校验；该信号不是再次读取活动的凭据。

## 来源、版本与租期

- Participation 复用 `agentevent.SnapshotVersion(PARTICIPATION, UPDATED_AT_DIGEST, actual_updated_at, to_jsonb(native_row))`。实际 opaque digest 检测相同时间戳下的原生状态变更；它不是单调 counter、CAS 或假 revision=1。
- going/pending 同时绑定真实 Activity revision 和最小生命周期元数据 digest。目标摘要只包含 ID、revision、更新时间、起止/取消/到期时间、发布/可见状态、city ID、host account ID；不复制活动正文、地点坐标、Profile 或对话。
- `metadata_created_at` 是真实 `agent_profiles.created_at`，用于拒绝 metadata 删除后重建的旧回执；不是 consent epoch、Profile 内容版本或认知许可。
- `source_updated_at` 来自实际原生 Participation；`observed_at` 是当前 PostgreSQL clock。租期严格为来源更新时间加现有 `agentevent.MaxEventTTL`（15 分钟），重试、重新构造 Reader 和重验证均不能续租；它不新增原生 Participation expiry 字段。
- 同主体/Agent/operation/源版本/状态/目标绑定产生稳定 UUIDv8 Signal ID；观察时间不进入 ID。它是地址，不是历史事件或权限 grant。
- UTC 事务固定 PostgreSQL 的 JSON 时间序列化；已验证 Asia/Shanghai、Pacific/Honolulu、Europe/London 连接配置结果相同。真实最终 SQL barrier 在池默认 REPEATABLE READ 的情况下重验，不能继承旧授权快照；初始失败及修复证据一并保留。

## 验证与未测范围

定向复现：

```powershell
& D:/Project/birdtie/work/v5-age024/scope.ps1 -EvidenceLabel reviewer -Production
```

该命令创建独占随机 PostgreSQL 库、执行实际001–058迁移及现有开发 seed、运行本包实际正负测试/vet/build、比较所有 public 表完整行并精确清理测试源、最后 DROP 自有库。开发合成用户、test session、合成活动与 seed 只证明本地实现。

详见 [审计记录](../research/BIRDTIE-V5-AGE-024-AUDIT.md) 和 [独立证据](../testing/evidence/agent-participation-signal-2026-10-03/README.md)。本轮没有新 schema 或 down 脚本；未运行 Flutter、真机、辅助技术、真实 IdP 或正式 A→H。后端对应 UX-CHECK-06/08/09/10/16；没有借结构断言声称消费 UI、截图或发布验收通过。
