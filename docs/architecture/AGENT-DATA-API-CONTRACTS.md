# Agent, Data & API Contracts

Status: proposed target contracts; the endpoint shapes and entities below are not a claim that they exist. Source: `Birdtie_Execution_and_CSSA_Partner_Pack.zip!/Birdtie_Execution_and_CSSA_Partner_Pack/docs/04_AGENT_DATA_API_CONTRACTS.md` (imported 2026-09-30). Agent principal identity and CityContext defer to [the accepted ownership model](AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md); the current deterministic task API is recorded in [ADR 0007](../decisions/0007-agent-workspace-query-boundary.md). User-facing responses are Simplified Chinese first.

V4 extension: [ADR 0017](../decisions/0017-v4-actor-agent-context-place-model.md) defines typed ActorRef, separate Business target semantics, global Context and Place/Venue boundaries. API examples below predate V4 and require compatibility review before implementation.

The staged API compatibility and backfill order are in the [V4 migration plan](../migration/BIRDTIE-V4-MIGRATION-PLAN.md); existing endpoint fields remain valid until their callers migrate and regression checks pass.

## 1. Canonical Entities

建议最小集合：

```text
User
PersonalAgent
Organization
OrganizationMembership
OrganizationAgent
Activity
ActivityParticipation
Place
CityContext
Conversation
ConversationTurn
AgentTask
ResultSet
SavedEntity
Notification
Report
AuditLog
```

## 2. Entity Identity

- 每个实体 Stable ID
- Pin/Card/Sheet/Conversation renderer 共用 Entity ID
- Client 不创建“看起来一样但不同 id”的临时业务实体

## 3. API Principles

1. Production UI 不读 hardcoded result fixtures。
2. Development seed data 通过真实 DB/API 路径。
3. 所有 list/search endpoint 有明确 empty vs error。
4. mutation endpoint backend 授权。
5. 所有关键 request 带 requestId / trace correlation。
6. stale request 可 cancel 或 ignore。
7. client 不根据 HTTP 200 猜业务成功；使用明确状态。

## 4. Suggested API Surface

实际命名遵循仓库风格，不强行复制：

### Area
`GET /areas/pulse?bounds=...&from=...&to=...`

当前实现：`GET /v1/cities/{cityID}/pulse?bounds=west,south,east,north&from=RFC3339&to=RFC3339`。地图范围必填，时间区间可选。返回真实公开活动总数、最多两个实际分类代码和数量，以及最多 100 条同一筛选条件下的活动和 `truncated` 标记；无数据返回 `status: "empty"`、`total: 0`、空分类及活动数组。Now 页仅在地图初始范围可用或用户明确点击区域搜索时请求，不在地图移动时自动请求。

### Activity search
`GET /activities?bounds=...&from=...&to=...&category=...`

当前实现：`GET /v1/cities/{cityID}/activities?bounds=west,south,east,north&from=RFC3339&to=RFC3339&category=code`。任一筛选参数启用真实数据库查询；仅返回未结束、已发布、公开、未取消、未过期的活动。时间区间按重叠匹配，地图范围只匹配有已发布精确 WGS84 地点的活动。空结果为 `200 {"data":[]}`，非法参数为 `400`。无筛选参数时保留原城市活动列表行为。此接口目前尚未由 Now 页直接调用。

### Activity detail
`GET /activities/{id}`

### Organization admin activity
`POST /organizations/{id}/activities`
`PATCH /organizations/{id}/activities/{activityId}`
`POST /organizations/{id}/activities/{activityId}/publish`
`POST /organizations/{id}/activities/{activityId}/cancel`

### RSVP
`POST /activities/{id}/participations`
`DELETE /activities/{id}/participations/me`

实现路径带 `/v1` 前缀。`GET /v1/activities/{id}/participations/me` 只返回当前登录者的记录；无记录时 `data: null`。首次报名和取消后重新报名返回 201，重复报名返回 200 且保持同一记录，取消返回 200 和 `status: cancelled`。已满返回 409 `activity_full`；未发布、非公开、已取消、已结束或过期的活动返回 409 `activity_unavailable`。报名只接受 Person Account，容量计算包含 `going` 和 `pending`，并在活动行锁内完成校验与写入。

`GET /v1/me/participations` 返回当前 Person Account 的报名摘要和从活动时间推导的 `activityStatus`；只提供有权限看到的活动标题。客户端“即将参加”和“已结束”由该服务器数据生成，收藏和个人提醒分别从现有独立接口读取。

### Plans
`GET /me/plans`

### Saves
`POST /saved-entities`
`DELETE /saved-entities/{entityType}/{entityId}`

### Agent
`POST /agent/query`

Input concept：

```json
{
  "conversationId": "...",
  "message": "Find badminton this weekend",
  "context": {
    "viewport": {},
    "selectedEntityId": null
  }
}
```

Output concept：

```json
{
  "requestId": "...",
  "conversationId": "...",
  "taskId": "...",
  "message": "I found 5 ...",
  "resultSet": {
    "id": "...",
    "entities": []
  },
  "actions": [],
  "followUps": [],
  "mapEffects": {}
}
```

### 当前 MVP 实现（2026-10-01）

当前实际入口是 `POST /v1/cities/{cityID}/agent/tasks`，接受 `query`、可选 `taskId` 和可选 `mapBounds`。`GET /v1/me/agent-tasks/{taskId}` 在登录后恢复当前任务。响应的 `data` 包含：

- `requestId`：每次请求都有，和响应头 `X-Request-ID` 一致；匿名请求只有此 ID，不伪造可恢复任务。
- `taskId`、`conversationId`：登录并保存任务时均指向同一个任务 ID；追问延续该 ID。
- `message`：先供对话显示的中文答复。
- `resultSet`：`id`、`taskId?`、`query`、`cityId`、`entities[{type,id}]`、`filters`、`generatedAt`、`status`。登录任务的结果集 ID 由任务 ID 和更新时间组成，恢复读取时保持稳定；匿名结果集用请求 ID。`status` 为 `ready`、`empty` 或 `unsupported`。实体引用仅由本次真实可见的返回数组生成。
- `actions[{type,label,targetType?,targetId?}]`：目前仅在当前人确有组织所有者/管理员权限时提供 `OPEN_ORGANIZATION_CONSOLE`。它打开真实组织工作台草稿流程，不自动发布。
- `followUps[]`：当前可用的追问文案。
- `mapEffects{camera,pinEntityIds[]}`：`camera` 当前为 `preserve`；标记 ID 仅来自有公开 WGS84 点位的实际结果，不强制移动地图。

为了逐步迁移客户端，顶层 `activities`、`organizations`、`places`、`people`、`groups` 暂时保留；它们与 `resultSet.entities` 来自同一组查询结果。零结果的搜索返回 HTTP 200、`status: empty`、空实体数组和具体中文答复。未知请求为 `unsupported`，不会生成占位实体。结果集可通过任务重新查询恢复，但尚未作为独立历史快照表保存；公开实体失效后不会继续暴露。

## 5. Intent Contract

MVP intents：

- find_activity
- find_organization
- find_place
- area_discovery
- refine_results
- compare_results
- create_activity

Agent 需要输出 normalized intent/slots 到内部日志，便于调试，但 production UI 不必展示。

## 6. ResultSet

ResultSet 是一次 Agent/Search 结果的业务对象，而不是 Widget local state。

应包含：
- id
- query/task reference
- entity refs
- filters/context
- generated_at

便于 follow-up：`Anything closer?`

## 7. State & Persistence

### Server source of truth
- users
- organizations
- memberships
- activities
- participations
- saved entities
- notifications

### Client UI state
- map viewport
- selected entity
- current sheet state
- composer draft

### Mixed / recoverable
- conversation context
- current ResultSet

当前登录实现把 `taskId`、初始问题、每轮对话、归一化意图和筛选条件保存在 `agent_tasks`；`filters.currentQuery` 指向最近一轮问题。区域搜索继承当前活动类别和时间，替换地图范围；恢复时重新检查实体公开可见性。匿名会话由客户端在本次运行中拼接前一轮问题，不发送本地 `local-*` 任务 ID，也不提供跨重启恢复。

当前客户端以递增的本地 turn 序号决定哪一次查询有权更新正在显示的任务、对话、ResultSet 和选中状态；较早请求即使晚返回也被忽略。Now 区域 Pulse 用独立序号执行同样的检查，切回已经显示的区域会使未完成的其他区域请求失效。`taskId` 关联已登录任务，`requestId` 用于服务端追踪；两者均不替代本地 turn 顺序。当前 HTTP 请求不主动取消，服务端可能继续处理旧请求；旧响应不得更新当前界面。跨设备同时写入同一已保存任务的最终持久化顺序尚未建立专门的版本冲突协议，不能把此本地显示保证表述为跨设备强一致。

关键业务成功不能只存在 client memory。

### Core Activity and Participation schema

Migration `021` keeps the accepted Organization principal model: `activities.organization_id` points to `organizations.id`, while `activities.host_account_id` must be that Organization's principal Account when the Organization link is present. `created_by_account_id` identifies a person actor, never the Organization itself. Existing sourced City Seed Activities have no Organization link and keep their independent review path.

Activity `publication_status` stores draft/published/hidden; `cancelled_at` records cancellation of a published Activity. Completed is derived from `ends_at` at read time. Only a published, public, current, uncancelled Activity is eligible for public discovery or a new RSVP; write APIs must enforce that eligibility.

公开 Activity 读取增加 `description`、`categoryCode`、`capacity`、`participantCount`、`priceMinor`、`currency`、`eligibility`、`languageCode`、`officialUrl` 和 `endSchedule`。容量人数只计算 `going`/`pending`；价格以最小货币单位存储。`officialUrl` 只取公开组织资料中的 HTTPS 链接；客户端只把 HTTPS 官方/来源链接显示为可打开链接。`schedule` 和 `endSchedule` 按活动城市时区生成中文本地时间。

`activity_participations` stores one row per person/Activity, with `going`, `pending` or `cancelled` status and a unique `(activity_id, participant_account_id)` constraint. Rejoining updates the existing row. It is distinct from `activity_plans`, which records only a private intention to attend. A membership or Organization Account cannot be used as a participant identity.

### Activity notifications

Migration `023` extends `inbox_items` with `target_activity_id` and the `activity_change`, `activity_cancelled`, `activity_reminder` resource types. The target field is the canonical navigation reference; individual change events have separate `resource_id` values so multiple legitimate edits can be delivered. Only participants with `going`/`pending` status receive activity changes. Published schedule or Place changes and cancellation insert their Inbox items in the same database transaction as the Activity write. The public Activity detail route remains available after cancellation and reports `status=cancelled`.

The reminder runner selects public, published, uncancelled Activities that start within two hours and still have an end time in the future. It runs at API startup and every five minutes; the same operation is available as a one-shot command. The Inbox uniqueness constraint makes each reminder idempotent. A schedule change removes the old reminder and permits a new reminder for the revised start time; cancellation removes the now-invalid reminder. Flutter loads the owner's Inbox, marks tapped items read, and resolves `targetActivityId` through the public detail API.

Migration `024` adds nullable `target_conversation_id` to Inbox items and backfills existing `conversation_message` items. A new human message writes this target in the same transaction as the message. `resourceId` remains the individual message ID; Flutter uses `targetConversationId` to open the member's actual conversation. Connection request notifications lead the user to the real requests section already inside Inbox. Local preview does not fabricate contacts or messages when no API is configured.

### Public Organization profile

`GET /v1/organizations/{id}` returns only an active, public Organization's public name, type, description, stored `verificationStatus`, owner-provided HTTPS links and future public Activities. The verification state is not inferred from an Organization's ability to publish an Activity. `Activity.organizationId` is included only when its Organization profile is publicly available. The client labels links as “组织提供的链接”. `agentAvailable` reflects an active Organization Agent, but does not imply the organization is verified.

### Organization Agent FAQ and grounded answers

Migration `025` stores Organization owned FAQ entries (`question`, `answer`, `published`, creator and timestamps). An active owner/admin can list, create, update and delete them through `GET/POST /v1/me/organizations/{id}/faqs` and `PUT/DELETE /v1/me/organizations/{id}/faqs/{faqID}`. Draft entries never appear in a public answer. Membership is checked on each write; changing the Organization principal does not turn an admin into a separate Organization Account.

`POST /v1/organizations/{id}/agent/ask` accepts `{ "query": string }` and returns `{ "data": { "organizationId", "status": "known|unknown", "answer", "sources": [{ "type": "faq|profile|activity|link", "id", "label", "url"? }], "mode": "verified_rules" } }`. The route only uses an active public Organization, its active Agent, its stored verified identity state, its published FAQ, its public profile and future published public Activities. Exact FAQ questions and a small set of explicit profile, link and activity prompts can receive sourced answers; unsupported questions return `unknown` with an empty source list. Organization verification confirms identity, while FAQ and profile content remains organization provided. This endpoint does not claim independent fact checking of that content or general conversational understanding.

### Pilot activity analytics

Migration `026` stores only `activity_id`, a constrained event type (`impression`, `detail`, `rsvp`, `save`), a constrained entry source, an optional conversion idempotency key and event time. It stores no person ID, exact location, search query, phone or device identifier. `POST /v1/activities/{id}/analytics/events` accepts only public `impression` and `detail` events for published public Organization Activities. The Android client records one visible Now/Agent card impression per activity/source per app session and one detail event per successfully loaded sheet. RSVP/save conversion events are generated by the API only after a successful write; repeat requests are deduplicated using the participation update or saved item ID. Conversion entry source comes from a constrained client header and is attribution metadata, not an authorization or verified provenance signal.

`GET /v1/me/organizations/{id}/analytics` requires an active owner/admin membership and returns each activity's event counts and per-source breakdown. These are raw event counts, not unique visitors or a person-level funnel; multiple sessions and public endpoint traffic can increase view counts. Zero-event activities remain visible with zero counts. No user-level export is provided.

### Incident reports and admin audit baseline

Migration `027` adds `incident_reports` and `admin_audit_events`. A signed-in person can submit `POST /v1/me/reports` for a public Activity, active Organization, active Account or general support issue, with a constrained reason and 10–1000 character explanation. The API returns a report ID and stores `open` status; `GET /v1/me/reports` returns only that person's latest 50 reports. Five submissions per person per rolling 24 hours are allowed. The reporter's account can be removed without deleting the incident case; the account reference then becomes null. The client links the form from settings, Activity detail and Organization profile, and warns against unnecessary private data.

Organization creation/profile edits, Activity create/edit/publish/cancel, and FAQ create/edit/delete append `admin_audit_events` with person actor, Organization ID, resource ID, action and time in the same database transaction or data-modifying SQL statement as the change. The audit table does not copy profile, FAQ or Activity bodies. It is an operator-only baseline with no public or Organization admin read route. The local pilot operator can inspect open cases using `automation/inspect_open_reports.ps1`; this is not yet a staffed moderation dashboard, automated dispatch, appeal flow or emergency contact channel. The UI must not promise a response time.

## 8. Observability

每个关键 request 日志最低字段：
- request_id
- user_id (safe identifier)
- endpoint/action
- environment
- latency_ms
- status/error_code
- agent_intent (if relevant)
- result_count

Debug build 可展示 Diagnostics：
- API connected
- auth status
- backend environment
- last request id
- last HTTP/business status
- result count
- last error

Production 禁止暴露内部诊断信息。

## 9. Privacy & Safety

- 不把普通用户实时精确位置作为 public map entity
- public map anchor 优先 Activity / Organization / Place / coarse context
- 个人资料最小化
- report/block 能力至少预留 domain/interface
- Organization admin 不得访问与活动运营无关的私人用户数据
