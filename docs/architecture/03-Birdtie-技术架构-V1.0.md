# Birdtie 技术架构 V1.0

**版本：** 1.0  
**日期：** 2026-09-29  
**状态：** 目标架构建议；不代表已部署、已验证容量或已取得安全认证。  
**品牌：** Birdtie

## 0. 范围与已有基础

本架构以 Birdtie 产品方案为权威模型，并将 Civu 的模块和技术经验作为只读参考。旧资料记录的 Civu 源码快照为客户端 `1bbf0613b`、服务端 `0489536`、后台 `2bca66d`；这些标识不代表 Birdtie 代码或生产状态。Birdtie 已建立自己的 PostgreSQL Foundation 迁移，Civu 表结构与生产数据不作为其迁移起点。部署前仍须核实供应商、权限和数据保留。

本架构新增并贯穿的领域原则：

- **Every experience can enrich both a person and a place.**
- **Memory ≠ Moment：** 私有原始素材和公开业务对象拥有独立生命周期、授权、存储与索引。
- **One object, multiple contexts：** Moment、Journey、Activity 只保留一份权威实体，通过关联边投影到 User、Place、City、Community、地图与搜索。
- 内容闭环：Discovery → Desire → Intent → Connection → Activity → Experience/Moment/Journey → Discovery。
- Personal Agent 与 City Agent 只能在服务端授权边界内读取数据；Agent 建议不构成权限判断或用户确认。

## 1. 目标、非目标与质量属性

### 架构目标

1. 采用 Go/PostgreSQL 与 Flutter 架构，优先模块化单体 + Worker，避免早期微服务分散事务。
2. 让个人、组织、内容、地点、城市和社交关系在明确授权下互相关联。
3. 支持历史素材导入、AI 草稿、用户审阅、显式发布、撤权/删除和索引清理闭环。
4. 对地图、搜索、推荐及 City Agent 执行相同的硬 ACL/受众/屏蔽过滤。
5. 异步任务可重试、幂等、可取消、可审计；外部副作用以领域回执确认。
6. 可先服务单城，之后扩展多城市、内容量和查询负载。

### 非目标

- 自研基础模型或把 Birdtie 建成通用 Agent Builder。
- 模型直接访问数据库、全租户向量索引、对象存储凭证或任意外部 API。
- 自动公开 Memory、未经确认发布 Moment、自动代表用户接受联系/报名/承诺。
- 首期建设专用图数据库、独立向量集群、微服务网格或复杂学习排序。
- 以“恰好一次”描述外部副作用；使用事务发件箱、幂等键、去重、对账与补偿。

## 2. 总体架构与服务边界

建议保留一个 Go 领域 API，按模块隔离代码/事务边界；异步 Worker 从事务发件箱领取任务。只有当规模或独立发布节奏证明必要时再拆服务。

```text
Flutter App / Admin & Organization Console
        │ HTTPS JSON + SSE/WebSocket
API Gateway / Auth / Rate Limit
        │
┌───────┴────────────────────────────────────────────────────┐
│ Go Domain API (modular monolith)                           │
│ Identity & Consent │ Content & Memory │ City & Place       │
│ Activity/Journey   │ Community/Org    │ Intent & Social    │
│ Discovery/Search   │ Map Query         │ Agent Orchestration│
│ Conversation       │ Moderation/Audit  │ Media Metadata     │
└───────┬──────────────────────┬─────────────────────────────┘
        │                      │
 PostgreSQL/PostGIS     Object Storage + CDN
        │                      │
 Outbox/Event Log → Worker Pool → Media/Import/Index/Notify/AI jobs
        │                      │
 Search projection / optional pgvector / map tile or query cache
        │
 External Model Gateway / Map-Geocoder Provider / Push Provider
```

### 模块职责

| 模块 | 权威职责 | 明确边界 |
|---|---|---|
| Identity & Access | Account、组织成员、认证级别、Consent、受众策略 | 不让模型决定权限；所有查询带 actor/owner/tenant |
| Content & Memory | 私有原始材料、Moment、Experience、媒体引用与发布状态 | Memory 与公开内容分表/分域；原始文件不自动进入公开索引 |
| City & Place | City、Place、城市供给与地理实体 | 地点权威记录不因不同来源重复创建；来源/别名单独记录 |
| Activity & Journey | 活动生命周期、参与、路线、站点 | 活动报名状态由该领域服务确认；历史状态不可误作可参与 |
| Community & Organization | 组织、社区、知识、角色、内容维护 | 组织权限不授权读取用户私人 Memory |
| Intent & Social | Intent、发现授权、连接请求、关系边、会话启动 | 出席、浏览或相似度不自动创建关系 |
| Discovery & Search | 可见对象过滤、检索、排序、解释 | ACL 与屏蔽先于全文/向量排序；LLM 不裁决可见性 |
| Map Query | 视窗、图层、聚合与地点关联查询 | 索引是派生视图；数据库授权真相优先 |
| Agent Orchestration | Personal/City Agent runs、上下文构建、工具调用、审批 | 模型只能调用类型化内部工具；外部写操作需人确认 |
| Media Pipeline | 上传授权、恶意文件扫描、EXIF/派生处理、转码 | 上传原图受限；导出/公开媒体单独授权 |
| Audit & Moderation | 举报、屏蔽、申诉、管理员操作、AI 工具审计 | 私人内容不进入普通运营仪表板；访问本身受审计 |

## 3. 核心数据关系

### 领域关系图

```text
Account ──owns──> PersonalAgentProfile
   │                         │
   ├─authors──────────────> Moment / Experience / Journey / Intent
   ├─member_of────────────> Community / Organization
   ├─requests─────────────> ConnectionRequest ──> Connection/Conversation
   └─consents─────────────> MemoryImport / Media / SharingGrant

Moment / Activity / Journey ──related_to──> Place / City / Community / Account
Activity ──hosted_by──> Organization; Journey ──contains──> ordered JourneyStops
City ──contains (projection)──> Places, Moments, Activities, Journeys, Communities
```

City containment 在逻辑上通过地理归属或关联边查询/投影，不复制 Moment、Journey 或 Activity。相同 Moment 可在多上下文展示，但 ACL 每次按对象 owner、受众、分享关系、屏蔽与内容状态重新检查。

### 逻辑表/聚合

| 实体/表 | 关键字段 | 约束与说明 |
|---|---|---|
| `accounts` | `id,type,status,handle,created_at` | Birdtie 新建身份记录；个人/组织主体统一 ID，认证级别单独建模 |
| `agent_profiles` | `owner_account_id,reception_mode,enabled,revision,profile_json` | owner 唯一；`unset` 时禁止 AI 对外接待 |
| `cities` | `id,name,region,country,timezone,boundary_geom,content_status` | 城市边界/别名/时区版本可追溯 |
| `places` | `id,provider_refs,name,category,geom,precision,source,verified_at` | Birdtie 新建 canonical ID；供应商 ID 单独映射，坐标策略化 |
| `place_aliases` | `place_id,locale,alias,source` | 支持同名、翻译和旧名称 |
| `memories` | `id,owner_id,source_type,private_storage_ref,import_batch_id,metadata_json,status,retention_at` | 默认 private；永不直接公开检索；原件可与派生项分开删除 |
| `memory_assets` | `id,memory_id,media_id,timestamp_exif,location_exif_ref,hash,scan_state` | 精确 EXIF 坐标独立受控，不写普通搜索文档 |
| `memory_import_batches` | `id,owner_id,source,scope,consent_id,state,agent_run_id,created_at` | 每批导入范围、授权、处理状态和撤销可追踪 |
| `moments` | `id,author_id,title,body,occurred_start/end,time_precision,visibility,status,source_memory_id,revision` | 只能通过用户确认发布；source ref 不使原件公开 |
| `experiences` | `id,author_id,body,topic_ids,visibility,source_refs,revision` | 可独立发布或从 Moment 提炼；来源保留 |
| `media_assets` | `id,owner_id,object_key,mime,size,checksum,variants,scan_state,visibility` | Birdtie 新建媒体记录；私有原件和公开派生媒体不同访问策略 |
| `activities` | `id,owner_id,org_id,title,start/end,tz,status,place_id,visibility,source,revision` | Birdtie 待建；`draft/upcoming/ongoing/past/cancelled` 派生状态，Past 无报名动作 |
| `activity_participations` | `activity_id,account_id,state,decision_at` | 活动状态与社交边分开；参与不自动加好友 |
| `journeys` | `id,owner_id,title,summary,visibility,status,occurred_range,source_memory_id,revision` | 可复用但不覆盖来源历史 |
| `journey_stops` | `journey_id,sequence,place_id,geom_override,time_range,note_visibility` | 支持站点级隐私、顺序和坐标泛化 |
| `communities` / `organizations` | `id,type,name,city_id,verification,visibility,revision` | 身份核验和内容准确性分别表达 |
| `org_memberships` | `org_id,account_id,role,state,valid_from/to` | 最小权限角色，换届可批量撤销 |
| `content_relations` | `source_type/source_id,relation_type,target_type/target_id,visibility,created_by,source,confidence` | 多上下文关系；对同一对象建立唯一幂等键，不复制对象正文 |
| `content_sources` | `object_type/object_id,source_type,source_ref,owner_attested_at,license_state` | 内容来源、用户权属声明、机构来源 |
| `intents` | `owner_id,topic,time_window,tz,coarse_geom,place_id?,activity_id?,audience,expires_at,state,revision` | 时间/区域可粗化；到期排除发现 |
| `social_edges` | `src,dst,type,state,visibility,source,created_at,ended_at` | follow/block/member/connection 等语义独立 |
| `connection_requests` | `requester,recipient,intent_id?,scope,payload,state,expires_at,decision_actor,version` | 唯一活跃请求和状态机；拒绝原因私密 |
| `consent_grants` | `grantor,grantee,resource,purpose,actions,audience,revision,expires_at,revoked_at` | 资源、用途、动作与接收方颗粒化授权 |
| `agent_runs/steps` | `owner,actor,purpose,audience,state,budget,policy_version,tool_refs,cost` | 不存多余完整 Prompt；记录最小可审计上下文引用 |
| `action_proposals/approvals` | `proposal,payload_hash,object_revision,consent_revision,recipient,approver,expires_at,state` | 内容/受众/对象变更使既有确认失效 |
| `conversations/messages` | `members,control_version,speaker_kind,owner,run_id,source_refs,content` | Birdtie 待建；真人/AI/系统发言明确，交接可终止在途回复 |
| `city_seed_items` | `city_id,object_ref,seed_type,maintainer,source_ref,valid_from/to,state` | Seed 是策展/维护关系，不是复制对象 |
| `outbox_events` | `event_id,aggregate_type,id,version,type,payload_ref,created_at,delivered_at` | 与业务写在一个事务中；去重与补偿 |
| `audit_events/moderation_events` | `actor,owner,resource,purpose,decision,action,outcome,case_id,timestamp` | 高敏操作最小留存、严格角色控制 |

`content_relations` 不应变成未约束的任意图数据库。对象类别与关系类型通过服务端枚举和外键/校验约束；常用强关系可建专用连接表，保留关系语义和查询计划。

## 4. Memory 导入与发布架构

```text
用户选择文件/导出包
   ↓ 认证上传授权、大小/格式限制、批次 consent
私有对象存储 quarantine
   ↓ 病毒扫描、哈希去重、EXIF 读取(精确位置隔离)
Memory Import Worker
   ↓ OCR/元数据解析/可选模型提取/地点候选匹配
Personal Agent Drafts（仅 owner 可见）
   ↓ 用户逐项修订、合并/拆分/删除、设置受众/位置精度/署名
Action Proposal + 当前 Consent/版本重检
   ↓ 用户确认
Moment / Journey / Experience 事务写入 + relations + outbox
   ↓ 派生公开媒体与搜索/地图索引更新
城市/地点/个人上下文展示（每次读仍执行 ACL）
```

### 导入原则

- 外部平台内容以用户自行提供的文件或明确授权的官方接口为准；不假设能登录或抓取微信/小红书私域内容。
- `memories` 与发布对象分域，表层 ACL/存储桶策略隔离；memory 不进入公共检索、向量库、地图或 City Agent。
- 模型输出为 `draft`，包含字段置信度、来源资产引用、候选 Place 和建议时间；低置信/冲突时要求用户编辑。
- 公开前针对对象、受众、时间范围、地点精度、媒体选择和关联城市做显式确认；不得把一次同意扩展到整批素材。
- 用户撤权立即阻断后续读取/处理；未完成任务取消。若用户删除源 Memory，可选择保留其已确认发布的 Moment，提示两者独立。
- 用户删除公开对象时，领域记录标记删除并发出媒体/索引/缓存清理事件；审计保留最小删除回执，不保留可还原的正文。

## 5. Personal Agent 与 City Agent 编排

### Runtime

每次调用创建受限 Run：`run_id, actor_id, owner_id, purpose, audience, object_refs, consent_versions, allowed_tools, budget, deadline, model_policy_version`。上下文由服务端授权后检索。工具由内部 typed registry 暴露，模型不能自选身份或权限。

状态：

```text
queued → context_building → model_running → proposal_ready
                                      ├→ awaiting_user_input
                                      ├→ awaiting_approval
                                      └→ tool_executing → succeeded/partial/failed/reconciling
任意非终态 → cancelling → cancelled
```

### Personal Agent

- 私人模式：读取用户显式指定的 Memory/偏好和草稿；不被其他用户或组织检索。
- 导入模式：仅访问本次 Memory Import Batch；产生候选 Moment/Journey 草稿，不调用 publish 工具。
- 对外接待模式：仅检索用户授权发布的 Profile/Knowledge/Experience；依据 reception mode 答复或转人工。
- 联系/发布/报名：生成 proposal；确认时绑定 payload hash、对象版本、recipient、受众与授权版本；执行前再次鉴权。

### City Agent

- 以 city_id 与请求者权限构建上下文，只检索有效 City Seed、公开 Place/Moment/Activity/Journey/Community/组织知识。
- 输出结构化答案和 `source_refs[]`，附内容发生时间、最后核验时间、来源类型和对象 ID。
- 允许的工具例：`search_city_objects`, `get_place_context`, `get_activity_state`, `get_journey`, `search_public_intents`, `create_intent_draft`。
- 不允许公开创建内容、代表组织确认活动、编造现状或把 private memory 加入公共知识。

### 双 Agent 查询

Personal Agent 可把用户批准的有限查询特征（例如主题标签和选定城市）传给 City Agent 搜索。服务端构建匿名化最小检索条件；不传递私人 Memory 原文或完整个人画像。工具回包仅含已通过权限过滤的对象。

### 审批与审计

对外副作用审批绑定 `proposal_id, payload_hash, target_objects, audience, consent_revision, object_revision, expires_at, approver_id`。目标、文本、位置或受众发生变化时重新确认。审计记 actor、owner、用途、策略版本、数据引用、工具名、批准状态、模型/费用和结果；普通日志不记录完整私密 Prompt、原始相册或精确坐标。

## 6. API 设计

统一 `/v1` JSON API。鉴权主体从 session/token 得出；请求体不能指定可信 `actor_id`。写操作支持幂等键、乐观版本、统一错误码和状态机校验。Agent Run 可通过 SSE 或 cursor 事件读取进度；消息沿用现有 WebSocket 能力前需核实现状。

| Endpoint 草案 | 作用 | 权限/行为 |
|---|---|---|
| `GET /v1/cities?query=` | 搜索可探索城市 | 返回公开城市与内容状态 |
| `GET /v1/cities/{id}/living?layers=&window=` | City Living 聚合入口 | 城市对象逐项 ACL 过滤，附来源与更新时间 |
| `GET /v1/map/objects?bbox=&zoom=&layers=&city_id=` | 地图多层视窗查询 | bbox 限幅；聚类；精确位置按权限泛化 |
| `GET /v1/places/{id}` | Place 详情及关联内容 | 返回当前用户可见关系投影 |
| `GET/POST /v1/moments` | 浏览/创建 Moment 草稿 | 创建先 draft；发布单独确认 |
| `PATCH /v1/moments/{id}` | 修改/关闭/删除 | owner 或明确协作者；版本检查和索引清理 |
| `GET/POST /v1/journeys` | 浏览/创建 Journey 草稿 | 每个 stop 单独设置坐标/受众策略 |
| `GET/POST /v1/activities` | 浏览/创建活动 | 主办角色；past/cancelled 状态保护 |
| `GET/POST /v1/intents` | 浏览/创建 Intent | 受众、粗区域、时区、过期必填/校验 |
| `POST /v1/connection-requests` | 联系请求 | 频控、收件人和文本确认、幂等 |
| `POST /v1/memories/import-batches` | 建立私有导入批次 | owner-only；返回短时上传 URL 和范围 consent |
| `POST /v1/memories/import-batches/{id}/analyze` | 触发私有解析/Agent 整理 | 验证批次仍授权；异步任务可取消 |
| `GET /v1/memories/import-batches/{id}/drafts` | 取回候选 Moment/Journey | owner-only，默认不可分享 |
| `POST /v1/memory-drafts/{id}/publish-proposal` | 创建公开对象方案 | 返回字段/受众/位置精度预览，未发布 |
| `POST /v1/action-proposals/{id}/confirm` | 确认发布/联系/报名等 | 再次检查 ACL、payload hash、名额与 consent |
| `POST /v1/agent-runs` / `GET /v1/agent-runs/{id}` | 启动/查询 Agent | purpose、范围、工具、预算服务端限定 |
| `POST /v1/agent-runs/{id}/cancel` | 取消 Run | 阻止未来步骤；已完成副作用另发回执 |
| `DELETE /v1/me/consents/{id}` | 撤回授权 | 同步阻断，outbox 清理派生索引/缓存 |
| `GET/DELETE /v1/me/memories/{id}` | 查看/删除私人素材 | owner only，触发媒体/索引/备份策略 |
| `/v1/organizations/{id}/seed|knowledge|inbox` | 城市供给管理 | 组织角色授权、来源与有效期审核 |
| `/v1/moderation/reports` | 举报/申诉 | case access 按安全角色隔离 |

地图聚合和城市页响应包含对象 ID、`object_type`、来源摘要、公开时间/地点精度与更新时间。客户端不能把聚合响应缓存成权限真相；返回的短期缓存以 ACL 版本失效。

## 7. 搜索、推荐与 City Graph 检索

### 分阶段实现

1. **首期 PostgreSQL：** B-tree 索引状态、owner、city、时间和对象类型；全文/trigram 检索标题、标签与公开文本；PostGIS（若托管环境支持）执行 bbox、距离、区域关联。
2. **地图索引：** 从领域对象以事务发件箱生成可见投影，含 geometry coarse、时间窗口、类型、city/place IDs 和对象版本；查询时再批量回源校验 ACL 与状态。投影不能作为公开授权来源。
3. **语义检索：** 用户资料/组织知识或公开城市内容达到需求后再考虑 pgvector。每个 chunk 带 tenant、owner、audience、consent version、source object 和有效期；先硬过滤/授权后相似度排序。
4. **外部搜索引擎（可选）：** 仅在 Postgres 实测无法满足文本/地理规模时引入；以 outbox 重建、版本检查和删除 tombstone 保持最终一致。

### 推荐流程

```text
Request + actor scope
 → active state / audience / consent / block / moderation hard filters
 → structured candidate retrieval (city, time, topic, coarse geo)
 → optional semantic recall within allowed set
 → rule ranking (topic overlap, time fit, content freshness, declared proximity)
 → explanation templates backed by public facts
 → paginated results + impression/feedback event
```

首版规则权重可配置且可解释；不使用敏感特征或私人行为做排序。City Agent 检索和 UI Discovery 共享候选访问层与过滤器，避免 AI 路径绕过普通 ACL。

## 8. 地图索引、多层显示与聚合

### 权威与投影

- `places.geom` 与 Activity/公开内容的允许坐标是 PostgreSQL 权威数据。
- `map_object_projection` 为 Moment、Activity、Journey stop、Place 提供快速视窗查询的派生索引，存 `object_type,id,city_id,place_id,coarse_geom,time_bucket,visibility_class,source_revision,deleted_at`。
- 投影采用 object ID 引用，不复制正文；卡片详情从领域 API 读取并再次授权。
- 地图 Cluster 按 zoom、对象类型和区域聚合；不得泄露被权限过滤前的计数。
- Moment 的 EXIF 坐标默认进入私有元数据；只有作者确认后才产生粗化公开 geometry。
- Journey 可按站点独立允许/隐藏；家庭/敏感地点附近进行策略化模糊。
- Activity 必须过滤状态：`upcoming/ongoing` 可显示行动入口；`past/cancelled` 只显示历史内容。

### 地图 API 查询保护

验证 bbox 面积和 zoom；限制返回对象数与频率；用户位置不作为长期轨迹存储。城市级远程浏览可不带 GPS。对于 Place 聚合数，按调用者权限进行计数或使用公开计数的安全阈值，避免通过计数侧信道推断隐藏内容。

## 9. 媒体处理与地理坐标

上传使用签名短时 URL 到 quarantine bucket。Worker 做文件类型/大小校验、恶意文件扫描、checksum、图片方向纠正、缩略图/视频转码、EXIF 解析和可选 OCR。公开媒体生成去除敏感 EXIF 的派生版本；保留原件需用户授权和生命周期策略。

坐标拆分为：

- `raw_exif_location`：Memory 级私有坐标，加密/隔离存储。
- `candidate_place_id`：Agent/地理服务建议的地点匹配，附 confidence 和来源。
- `confirmed_place_id`：用户确认关联的 Place。
- `public_geom`：按用户选择精度生成的公开/地图投影坐标，可空或粗化。

不得由系统把 raw EXIF 自动提升为 public geometry。用户可选择只关联城市、Place、不显示地图，或模糊到网格/区域。日志、embedding 和缩略图文件名不得包含精确坐标。

## 10. 事件流、异步任务与可靠性

### 事务发件箱

同一数据库事务写入业务实体和 `outbox_events`。Worker 用租约领取、幂等消费、有限重试和退避；每个副作用使用事件 ID/领域幂等键。超过重试上限转死信队列/人工恢复。撤权/删除事件有优先级，消费者处理时重新检查当前授权。

### 关键事件

`memory.uploaded`, `memory.analysis.requested`, `memory.deleted`, `draft.created`, `content.published`, `content.updated`, `content.visibility.changed`, `content.deleted`, `relation.created`, `activity.state_changed`, `intent.expired`, `consent.revoked`, `moderation.reported`, `agent.proposal.approved`, `connection.accepted`, `media.variant.ready`。

事件 payload 尽量只包含 ID、版本、任务类型和受限引用；不把私人文本/原图直接写进常规事件总线。消费者按 event version 处理，重复事件不重复发布或发送通知。

### Worker 类别

- 私有素材病毒扫描、媒体转码和元数据提取。
- Memory 去重、照片组批、地点候选匹配、Agent 整理草稿。
- 搜索/地图投影、embedding 生成和删除清理。
- City Seed 到期提醒、Activity/Intent 生命周期任务。
- Agent Run、通知、SSE/WebSocket 扇出、审计导出与 moderation 队列。

单体初期可用 PostgreSQL outbox + Worker 队列；并发和长时编排增长后再评估 Redis Streams/托管队列/Temporal，不能让队列成为领域状态真相。

## 11. 权限、隐私与风控

### 统一授权谓词

```text
ALLOW = authenticated actor
  ∧ account/organization membership active
  ∧ object exists and is not deleted/moderated
  ∧ visibility permits actor/audience
  ∧ current consent permits purpose + action + resource
  ∧ owner/tenant scope matches
  ∧ no block or safety restriction
  ∧ agent run/tool scope permits request
  ∧ (external side effect: current human approval binds payload/version/recipient)
```

授权在领域服务执行。服务端只向模型提供已过滤数据；撤权同步写入主库并阻断新读取，之后异步清理缓存、向量、索引、派生媒体和排队任务。组织成员撤权会撤销其管理凭证和接待 lease。

### Memory 与公开内容策略

- Memory 表/桶默认 owner-only，独立加密密钥策略和访问审计；不纳入通用备份导出分享链接。
- 人脸识别、人物标记、敏感属性推断不作为默认导入步骤。
- Memory 的原始内容不能被 City Agent、组织工作台、公共搜索或其他用户使用。
- Moment/Experience/Journey 的发布需明确选择受众、地点精度、时间精度和关联上下文。
- 共同参与者、被拍摄者或引用作者的权利与报告路径要在产品流程中支持。

### 滥用治理

上传频率、批量导入、Intent/联系请求和消息均限流；防恶意文件、垃圾内容、冒充、地图坐标滥用和自动化骚扰。身份核验、内容来源、组织认证与履约能力分别表达。举报处置、账号限制与申诉留审计；运营不默认获得浏览私人 Memory 的权限。

## 12. 隐私删除、撤权与审计

### 撤权流程

1. 主库事务标记 Grant revoked、递增授权版本、停止当前 run/lease。
2. 所有读 API、工具执行器、缓存读取即时校验当前版本并拒绝旧授权。
3. 写入高优先级 outbox，清除 Search/Map/Vector projection、缓存和待处理队列中的派生引用。
4. 记录完成/失败回执；失败进入可见运营队列，不延迟主权限阻断。

### 删除对象流程

- 对象软删除/不可见立即生效；公开 URL 失效，媒体下载授权撤回。
- 异步删除派生图像、搜索词条、embedding、Map projection 和 CDN 缓存。
- 关系边/城市计数投影同步重算；引用方显示已删除或移除关联，不保留正文副本。
- 备份按既定周期过期，恢复流程必须重放 tombstone；删除完成范围和不可即时清除的备份窗口应如实说明。
- 审计保留对象 ID、动作、操作者、时间与结果等最小信息，不保留可重建正文。

审计字段建议：`actor_id, owner_id, purpose, resource_refs, decision, policy_version, consent_revision, run_id, tool, approval_id, provider/model, cost, result_code, timestamp`。访问私人原件、修改 City Seed、人工查看举报内容均单独记录。

## 13. 存储选型与数据分区

| 数据 | 首选 | 说明 |
|---|---|---|
| 权威业务对象与关系 | PostgreSQL | 事务处理；迁移与备份复用现有能力 |
| 地理查询 | PostgreSQL + PostGIS（候选） | 上线前核实托管扩展/备份/版本；否则供应商适配或有限地理网格索引 |
| 私人 Memory 与媒体 | 加密对象存储 | quarantine/private/public 派生分桶或策略分层，短时签名 URL |
| 全文检索 | PostgreSQL FTS/trigram 起步 | ACL 过滤；规模实测后才拆 OpenSearch 等 |
| 向量检索 | pgvector（可选） | 仅在确有语义召回需求后；与权限版本一起索引 |
| 缓存/限流 | Redis（可选） | 短时非权威缓存；key 包含 tenant/ACL 版本 |
| 分析事件 | 受控事件仓/聚合表（后续） | 最小字段、去标识；不复制私人原文 |

精确 EXIF、私人文字、公开图层和派生 embedding 物理/逻辑分区应独立控制。托管地区、供应商留存、训练用途、删除能力和数据传输要按合同核实。

## 14. 客户端与后台

### Flutter/Riverpod

按领域拆分 `CityExplore`, `MapViewport`, `Place`, `Moment`, `MemoryImport`, `Journey`, `Activity`, `Intent`, `Discovery`, `ConnectionRequest`, `AgentRun`, `Consent` 状态。所有写入展示草稿/处理中/已确认/失败及服务器回执。地图和列表共享查询条件与 cursor。离线可缓存公开对象与本地草稿；公开、连接接受、报名、授权变更等动作联网重新校验。

Memory 页面始终标明“仅自己可见”；导入草稿与公开个人页/城市页分区展示。发布确认页逐项显示文本、媒体、地点精度、时间、受众和城市关联。

### Organization / Safety Console

组织后台管理 City Seed、来源、有效期、活动、组织知识、成员角色和人工接待；平台安全后台处理举报、内容权限争议和故障队列。组织管理员不能搜索/查看个人 Memory。平台特权读取需 break-glass 理由、短时授权和审计。

## 15. 部署、可扩展性与可靠性

- **应用层：** 现有 Go API 横向扩展；Worker 按媒体、AI、索引、通知任务独立并发池与资源限额。
- **数据层：** PostgreSQL 主从/托管 HA（现有部署核验），业务事务与 outbox 原子写；备份加密与恢复演练。
- **区域：** 先确定主数据区域和第三方模型/地图/媒体处理路径；试点真实测 UK RTT、媒体上传、模型可用性及适用数据规则。
- **大对象：** 客户端直传对象存储，API 管理短时授权和元数据，不经 Go API 转发大文件。
- **扩容触发：** P95 查询、地图视窗负载、队列年龄、索引写延迟、媒体成本和 DB 资源实测后决定读副本、搜索引擎、队列或服务拆分。
- **一致性：** 业务数据库强一致；搜索/地图投影最终一致并显示新鲜度；ACL 检查回源，权限紧急撤销不等待投影。
- **降级：** AI/向量检索不可用时保留地点、分类、全文和列表浏览；地图服务不可用时保留列表；Worker 堵塞时禁止把未发布草稿展示为已发布。

## 16. 可观测性与质量门

以 `request_id, actor_scope, object_id, run_id, event_id` 关联日志与追踪；敏感标识受控。监测 API P95/error、数据库查询/锁、地图候选数、ACL 拒绝、Worker backlog、索引延迟、导入失败率、EXIF 处理、AI token/cost、Agent 工具拒绝、审批后副作用、撤权清理时长、内容过期率、举报和地图图层转化。

建议发布检查覆盖：跨租户读取、Memory 进入公共检索、EXIF 坐标意外公开、确认后受众变化、撤权与 Worker 并发、重复事件、删除投影重建、同对象多关系无副本、Past Activity 报名防护、Agent 接管竞态、过期 City Seed、向量 ACL、导入包恶意文件、错误地点匹配和离线重放。本文未声称这些测试已运行。

## 17. 分阶段技术路线

| 阶段 | 技术交付 | 准入/验收重点 |
|---|---|---|
| T0 审计与领域契约 | Civu 只读事实盘点；Birdtie 对象/关系、权限、来源与供应商决策 | 代码存在/部署/验收状态分列；数据权属和复用许可明确；不以 Civu schema 作为 Birdtie 权威模型 |
| T1 Foundation | Account/User、Consent/ACL/审计；City/Geo、canonical Place/City Seed；Media/Memory 私有与公开派生边界 | 多来源地点可去重；不带定位可选城市；来源/维护者/时效可追溯；私有原件和 EXIF 不进公共索引 |
| T2 City Graph 内容对象 | Moment/Experience、Activity、Journey、Intent 及 typed relations；对象版本、来源和生命周期 | 同一对象多上下文不复制；Past Activity 状态正确；Intent 有有效期；坐标精度受控 |
| T3 城市发现 | Now/Explore 查询、地图/列表投影与筛选、确定性排序、来源/新鲜度 | UI/地图/搜索共享 ACL；空城真实；地图聚合无权限侧信道；定位可选 |
| T4 双向连接 | Connection Request 状态机、接受后的会话/活动协作、频控、举报与屏蔽 | 双方明确同意；共同参加不自动成为好友；拒绝/屏蔽后无继续触达 |
| T5 Agent | Personal Agent 私有草稿辅助；City Agent 授权检索、来源引用；副作用提案审批 | 私人/公共上下文隔离；撤权即时阻断新读取；发布、联系和报名必须由人确认 |
| T6 Pilot hardening 与扩展 | 运营/备份/恢复/观测、组织轮替、多城、全文/地理索引、按需语义检索 | 删除/撤权投影一致；时效/维护成本与现实行动价值可测；供应商/隐私审查通过 |
| T7 商业与高负载 | 组织席位、清晰标记的商业内容、搜索/Worker 独立扩容 | 单位经济、供应商合同、隐私与支持能力均通过评审 |

每阶段使用 feature flags/组织白名单，数据库变更向前兼容并可回滚。Birdtie 既有对象产生后，迁移和入口调整需保护其可读性与权限。

## 18. 关键技术决策待验证

1. 哪些 Civu 隔离算法值得单独迁移，相关文件的许可、依赖和 Birdtie 适配条件是否通过审查？Birdtie 领域表不沿用 Civu schema。
2. 当前 PostgreSQL 托管服务是否支持 PostGIS/pgvector、索引备份与恢复？
3. 地图供应商的聚合/瓦片/地理编码条款、坐标存储、缓存和城市边界来源是什么？
4. 历史媒体上传格式、相册规模、EXIF 处理准确率、成本和目标保留周期是什么？
5. Personal/City Agent 使用哪些模型区域、日志保留和供应商处理条款？
6. Birdtie 的组织、消息、屏蔽/举报和活动报名 API 应如何实现权限状态机与事务回执？
7. 何种实测负载才需要独立搜索、Redis/托管队列、Temporal 或服务拆分？

## 19. 文档关系

用户体验、城市内容、历史 Memory 与产品验收见《01-Birdtie-产品文档-V3.0.md》；城市供给、Alumni 冷启动、收入假设与经营指标见《02-Birdtie-商业规划-V3.0.md》。术语、状态或权限策略变更需三份文档同步更新。

