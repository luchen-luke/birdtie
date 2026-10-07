<!-- 来源：用户附件 C:\Users\chens\.codex\attachments\740182d5-1cae-4a04-b288-7f58711b226f\已粘贴的文本.txt。2026-10-01 收录；状态 QUEUED。此文档为后续 Epic 需求，不替代现行 V4 队列或已验证的代码事实。 -->

# Birdtie BT-V5-AGE
## Agent Enrichment & Cognitive Architecture
### Personal Agent / Organization Agent / Business Agent 成长系统

---

# 0. Codex Master Directive

本文件是 Birdtie 当前 Master Plan 的**追加 Epic**。

Epic ID：

`BT-V5-AGE`

Epic Name：

`Agent Enrichment & Cognitive Architecture`

本 Epic 不替代、不暂停、不重置当前已经在执行的任何需求。

---

# 1. 最高优先级执行规则

## RULE-001 — 不允许中断当前任务

如果 Codex 当前仍在执行上一份 Birdtie Master Requirement List：

**必须继续完成当前任务。**

严禁因为收到本文件而：

- abort 当前执行；
- stop 当前开发；
- reset Git；
- checkout 丢弃现有修改；
- revert 当前尚未完成的代码；
- stash 后切换到本 Epic；
- 删除当前分支；
- 重建项目；
- 修改当前任务优先级；
- 跳过当前 TODO；
- 为 BT-V5-AGE 提前破坏现有实现。

当前任务：

```text
CURRENT MASTER QUEUE
        ↓
继续执行
        ↓
当前 atomic task 完成
        ↓
测试 / build / lint / migration 状态确认
        ↓
安全 checkpoint
        ↓
继续当前 Master Queue
        ↓
当前批次完成
        ↓
BT-V5-AGE
```

---

## RULE-002 — 本 Epic 初始状态必须为 QUEUED

收到本文件后：

```text
BT-V5-AGE = QUEUED
```

只有当前 Master Queue 到达安全切换点后：

```text
BT-V5-AGE = READY
```

然后：

```text
READY
↓
IN PROGRESS
```

不得直接从：

```text
QUEUED
→ IN PROGRESS
```

---

## RULE-003 — 不允许破坏现有 Agent Identity Architecture

现有 Birdtie Agent Identity Architecture 继续作为规范基础：

```text
Person
  └── Personal Agent

Organization
  └── Organization Agent

Business
  └── Business Agent
```

继续保持：

```text
One Human
→ One Personal Agent
```

Organization 和 Business：

```text
真实用户
→ Membership / Role
→ 管理主体
→ Agent
```

CityContext：

```text
不是 Agent
```

Community：

```text
当前阶段默认不是 Agent
```

除非后续单独 ADR 修改。

---

# 2. 本 Epic 的目标

当前 Birdtie 已经主要解决：

```text
WHO DOES THE AGENT REPRESENT?
```

BT-V5-AGE 需要继续解决：

```text
WHAT DOES THE AGENT KNOW?

HOW DOES THE AGENT LEARN?

WHAT CAN THE AGENT REMEMBER?

WHAT IS THE AGENT ALLOWED TO DO?

HOW DOES THE AGENT MANAGE ATTENTION?

HOW DOES THE AGENT UNDERSTAND SOCIAL CONTEXT?
```

最终建立：

```text
Identity
   ↓
Profile
   ↓
Context
   ↓
Memory
   ↓
Policy
   ↓
Intent
   ↓
Agent Runtime
   ↓
Agent-to-Agent
```

---

# 3. 总体架构目标

最终 Birdtie Agent 架构：

```text
                       Agent Runtime
                            │
        ┌───────────────────┼────────────────────┐
        │                   │                    │
 Personal Agent      Organization Agent      Business Agent
        │                   │                    │
 Personal Pack       Organization Pack       Business Pack
        │                   │                    │
 Profile              Profile                Profile
 Memory               Memory                 Memory
 Policy               Policy                 Policy
 Context              Context                Context
 Permissions          Permissions            Permissions
        │                   │                    │
        └───────────────────┼────────────────────┘
                            │
                  Shared Cognitive Layer
                            │
          ┌─────────────────┼─────────────────┐
          │                 │                 │
     Memory Engine     Context Builder    Policy Engine
```

不得分别开发三套完全独立的 Agent Runtime。

---

# 4. 核心概念

Personal Agent 必须至少拥有：

```text
1. Public Profile
2. Private Agent Profile
3. Memory Graph
4. Context Graph
5. Policy
6. Attention Policy
7. Social Interaction Policy
8. Permission / Autonomy Level
```

---

# 5. EPIC A — Agent Profile Foundation

---

## AGE-001 Agent Profile 基础模型

Priority:

`P0`

建立统一：

```text
AgentProfile
```

AgentProfile 必须绑定：

```text
agent_id
owner_type
owner_id
profile_version
created_at
updated_at
```

OwnerType 至少：

```text
PERSON
ORGANIZATION
BUSINESS
```

验收：

- Personal / Organization / Business 可以共享 Profile infrastructure；
- 不复制三份 Agent Profile 系统；
- Profile 与 Agent Identity 明确分离；
- migration backward-compatible。

---

## AGE-002 Public Profile 与 Private Agent Profile 分离

Priority:

`P0`

Personal Agent 必须区分：

```text
Public Profile
```

与：

```text
Private Agent Profile
```

### Public Profile

别人允许看到的信息。

例如：

```text
display_name
avatar
bio
current_city
interests
public_moments
public_activities
public_communities
public_places
```

### Private Agent Profile

只有用户自己的 Personal Agent 可以使用。

例如：

```text
personal_preferences
social_preferences
availability
preferred_activity_types
travel_preferences
interaction_preferences
private_city_history
language_preferences
agent_notes
```

严禁因为 Agent 知道某项信息而自动公开。

---

## AGE-003 Profile Field Visibility

Priority:

`P0`

每一个可配置 Profile field 必须支持 visibility。

至少：

```text
PUBLIC
CONNECTIONS
COMMUNITY
PRIVATE
AGENT_ONLY
```

不要只建立：

```text
is_public: bool
```

必须支持后续扩展。

---

# 6. EPIC B — Agent Memory Foundation

---

## AGE-004 Agent Memory

Priority:

`P0`

建立统一：

```text
AgentMemory
```

推荐最少字段：

```text
id

agent_id

memory_type

memory_key

summary

structured_value

confidence

source_type

visibility

status

valid_from

valid_until

last_reinforced_at

created_at

updated_at
```

MemoryType 示例：

```text
IDENTITY
PREFERENCE
PLACE
CITY
ACTIVITY
COMMUNITY
ORGANIZATION
RELATIONSHIP_CONTEXT
HISTORY
INTENT
ROUTINE
AVAILABILITY
EXPERIENCE
```

---

## AGE-005 Memory Evidence

Priority:

`P0`

Memory 不允许成为没有来源的 AI 结论。

建立：

```text
MemoryEvidence
```

至少包含：

```text
memory_id
source_type
source_id
signal_type
weight
observed_at
```

例如：

```text
Memory:
likes hiking

Evidence:
Moment #18
Activity #92
SavedPlace #37
```

---

## AGE-006 Explicit Memory 与 Inferred Memory

Priority:

`P0`

必须区分：

```text
EXPLICIT
```

和：

```text
INFERRED
```

Explicit：

用户自己明确告诉 Agent：

```text
I like hiking.
```

Inferred：

Agent 根据行为推测：

```text
possible hiking preference
```

Explicit 默认权重大于 Inferred。

---

## AGE-007 Memory Candidate

Priority:

`P0`

低置信度信息不能直接写入稳定 Memory。

必须支持：

```text
CANDIDATE
ACTIVE
REJECTED
SUPERSEDED
EXPIRED
```

例如：

用户上传一次徒步 Moment：

```text
Hiking preference
confidence = 0.25
status = CANDIDATE
```

后续多个 Evidence：

```text
confidence = 0.82
status = ACTIVE
```

---

## AGE-008 Confidence Model

Priority:

`P0`

Memory 必须支持 confidence。

范围建议：

```text
0.0 - 1.0
```

禁止简单：

```text
true / false
```

表示推断偏好。

---

## AGE-009 Memory Reinforcement

Priority:

`P1`

新的 Evidence 应可以强化已有 Memory：

```text
existing memory
+
new evidence
→ reinforcement
```

不能重复产生：

```text
likes hiking
likes hiking
likes hiking
```

三个 Memory。

---

## AGE-010 Memory Decay

Priority:

`P1`

不是所有兴趣永久有效。

长期没有 reinforcement 的 inferred memory：

```text
confidence ↓
```

但：

Explicit Memory 默认不自动 decay。

---

## AGE-011 Memory Correction

Priority:

`P0`

用户必须能够：

```text
edit
correct
reject
delete
```

Agent Memory。

例如用户可以告诉 Agent：

```text
I don't actually like hiking.
```

Agent 必须：

- 降低旧 inferred memory；
- 保存 correction；
- 防止下一次马上重新推断。

---

## AGE-012 Memory Provenance

Priority:

`P0`

所有重要 Memory 必须能够回答：

```text
Why does Birdie think this?
```

例如：

```text
You may like photography because:

• you joined 2 photography activities
• you saved 8 photography places
• you posted 4 photography moments
```

---

# 7. EPIC C — Sensitive Inference Guardrails

---

## AGE-013 Sensitive Attribute Protection

Priority:

`P0`

不得根据 Moment / Activity / Search / Place 自动推断敏感个人属性。

至少包括：

```text
health
medical conditions
mental health
religion
ethnicity
race
political ideology
sexual orientation
sex life
trade union membership
criminal history
```

除非产品未来存在非常明确且合法的 explicit user input 场景。

不得从行为偷偷生成此类 Memory。

---

## AGE-014 Weak Evidence Protection

Priority:

`P0`

例如：

```text
visited bar once
```

不能直接得到：

```text
likes nightlife
```

参加一次：

```text
church event
```

不能推断宗教。

浏览：

```text
political content
```

不能推断政治立场。

---

# 8. EPIC D — Personal Agent Onboarding

---

## AGE-015 Agent Seed Onboarding

Priority:

`P0`

用户完成注册后：

不得要求填写巨大 Profile 表。

设计：

```text
Progressive Agent Enrichment
```

首次至少收集：

```text
display name
current city
language
basic intent
```

---

## AGE-016 What Brings You Here

Priority:

`P0`

Agent Seed 问题：

```text
What brings you to Birdtie?
```

例如：

```text
Find people
Find activities
Explore the city
Meet people with similar interests
Join communities
Discover places
Just explore
```

存入：

```text
User Intent
```

不是公开资料。

---

## AGE-017 Interest Seed

Priority:

`P0`

用户可以选择初始兴趣：

```text
badminton
hiking
photography
coffee
music
travel
gaming
running
food
language exchange
...
```

必须允许：

```text
skip
```

不能强制。

---

## AGE-018 Social Preference Seed

Priority:

`P1`

例如：

```text
Small groups
Large activities
One-to-one
Same university
Same city
Shared hobbies
International communities
```

---

## AGE-019 Progressive Completion

Priority:

`P1`

后续不要重新要求填写长表。

Agent 可以在自然使用过程中：

```text
gradually enrich profile
```

---

# 9. EPIC E — Moment → Memory

---

## AGE-020 Moment Enrichment Pipeline

Priority:

`P0`

Moment 必须成为 Agent Context / Memory Evidence 来源。

流程：

```text
Moment
  ↓
Signal Extractor
  ↓
Candidate Evidence
  ↓
Memory Candidate
  ↓
Confidence
  ↓
Memory Graph
```

---

## AGE-021 Moment Place Context

Priority:

`P0`

Moment 如果关联 Place：

```text
Moment
→ Place
→ City
```

可以成为：

```text
visited place
visited city
```

的 evidence。

---

## AGE-022 Moment Activity Context

Priority:

`P0`

Moment 与 Activity 关联时：

```text
Moment
→ Activity
```

可以作为：

```text
participation / experience
```

Evidence。

---

## AGE-023 Historical Moment

Priority:

`P1`

允许用户上传：

```text
past Moment
```

例如：

```text
2025
Isle of Skye
```

不能因为创建时间是 2026 就认为 Experience 时间是 2026。

需要：

```text
event_time
created_at
```

分离。

---

# 10. EPIC F — Activity → Memory

---

## AGE-024 Activity Participation Signal

Priority:

`P0`

用户：

```text
join activity
```

可以产生：

```text
participation signal
```

但不能单次直接得出稳定兴趣。

---

## AGE-025 Repeated Activity Preference

Priority:

`P1`

连续参加：

```text
badminton
badminton
badminton
```

可以强化：

```text
badminton preference
```

---

## AGE-026 Activity Social Context

Priority:

`P1`

允许建立：

```text
Person
→ attended activity with
→ Person
```

但不得立即推断：

```text
close friend
```

Relationship strength 必须使用独立规则。

---

# 11. EPIC G — Place / City / Life Graph

---

## AGE-027 Place Memory

Priority:

`P0`

支持：

```text
visited
saved
liked
created moment at
attended activity at
```

不同 Place 信号。

---

## AGE-028 City History

Priority:

`P1`

支持：

```text
current city
lived city
visited city
interested city
```

必须区别。

例如：

```text
visited London
```

不等于：

```text
lived in London
```

---

## AGE-029 Life Map

Priority:

`P1`

为 Personal Agent 构建：

```text
Life Map
```

可能包含：

```text
cities
places
activities
moments
communities
```

第一阶段 Life Map 可以只作为数据模型和简单 UI。

---

## AGE-030 Life Import

Priority:

`P2`

未来允许：

```text
manual city import
manual place import
photo import
past activity import
past journey import
```

第一阶段不得为了 Life Import 延迟 P0。

---

# 12. EPIC H — Community / Membership Context

---

## AGE-031 Community Membership Signal

Priority:

`P0`

用户加入 Community：

```text
membership
```

进入 Personal Agent Context。

但：

```text
member of photography community
```

不自动等于：

```text
photography enthusiast
```

只能作为 Evidence。

---

## AGE-032 Organization Membership

Priority:

`P0`

例如：

```text
Aberdeen CSSA
```

Membership 可以进入 Personal Agent Context。

必须区分：

```text
member
moderator
admin
owner
```

---

# 13. EPIC I — Personal Agent Context Builder

---

## AGE-033 Context Builder

Priority:

`P0`

建立统一：

```text
AgentContextBuilder
```

用于 Agent Runtime 调用。

Input：

```text
agent_id
request
current_context
```

Output：

```text
relevant profile
relevant memories
relevant places
relevant activities
relevant relationships
relevant policies
```

不能每次把用户全部 Memory 全部塞给模型。

---

## AGE-034 Context Relevance

Priority:

`P0`

必须：

```text
retrieve relevant context
```

而不是：

```text
load entire user history
```

---

## AGE-035 Context Budget

Priority:

`P1`

Context Builder 必须允许：

```text
limit
priority
confidence threshold
recency weighting
```

---

## AGE-036 Current Context

Priority:

`P0`

必须区别：

```text
long-term memory
```

和：

```text
current context
```

例如：

```text
currently in Aberdeen
looking for something tonight
```

不一定成为长期 Memory。

---

# 14. EPIC J — Attention Policy

---

## AGE-037 Attention Policy Model

Priority:

`P0`

建立：

```text
AttentionPolicy
```

Agent 可以根据 Event 判断：

```text
IMMEDIATE
NORMAL
DIGEST
SILENT
BLOCK
```

---

## AGE-038 Notification Routing

Priority:

`P0`

未来 notification pipeline：

```text
Event
 ↓
Attention Policy
 ↓
Priority
 ↓
Notification
```

而不是：

```text
Event
↓
Notification
```

---

## AGE-039 Notification Categories

Priority:

`P0`

至少：

```text
MESSAGE
ACTIVITY
COMMUNITY
ORGANIZATION
BUSINESS
SYSTEM
AGENT
SOCIAL
```

---

## AGE-040 Digest

Priority:

`P1`

允许：

```text
community updates
recommended activities
business updates
```

进入 digest，而不是全部 push。

---

# 15. EPIC K — Social Interaction Policy

---

## AGE-041 Social Policy

Priority:

`P0`

Personal Agent 必须存在：

```text
SocialInteractionPolicy
```

例如：

```text
same university
shared community
shared activity
existing connection
unknown person
business
organization
```

---

## AGE-042 Message Request Policy

Priority:

`P1`

允许：

```text
ALLOW
REQUEST
SCREEN
BLOCK
```

---

## AGE-043 Introduction Policy

Priority:

`P1`

未来 Agent 推荐人与人认识时：

```text
shared activity
shared community
shared interest
shared city
```

必须遵守 Social Policy。

---

# 16. EPIC L — Agent Autonomy

---

## AGE-044 Autonomy Level

Priority:

`P0`

定义：

```text
LEVEL_0_OBSERVE

LEVEL_1_ASSIST

LEVEL_2_PREPARE

LEVEL_3_DELEGATE
```

---

### Level 0

Agent：

```text
observe
understand
build context
```

---

### Level 1

Agent：

```text
summarize
recommend
prioritize
remind
```

---

### Level 2

Agent：

```text
draft response
prepare invitation
prepare registration
suggest meeting
```

必须用户确认。

---

### Level 3

Agent：

```text
take autonomous action
```

当前版本：

**不得默认启用。**

---

## AGE-045 Outbound Action Safety

Priority:

`P0`

MVP / Pilot：

Personal Agent 不允许未经确认：

```text
send message
accept invitation
join activity
leave community
make booking
create commitment
```

---

# 17. EPIC M — Personal Agent UI

---

## AGE-046 Agent Profile Page

Priority:

`P0`

用户应该存在：

```text
My Agent
```

入口。

至少展示：

```text
What Birdie knows about you
Preferences
Places
Activities
Communities
Agent settings
```

---

## AGE-047 Memory Center

Priority:

`P1`

建立：

```text
Agent Memory
```

页面。

用户可以：

```text
view
edit
delete
correct
```

---

## AGE-048 Why This?

Priority:

`P1`

推荐内容允许：

```text
Why am I seeing this?
```

例如：

```text
Because you:

• joined Aberdeen Badminton
• attended 2 badminton activities
• saved this venue
```

---

## AGE-049 Agent Privacy Controls

Priority:

`P0`

必须有：

```text
Public profile
Agent private profile
Memory
Personalization
Agent learning
```

相关控制。

---

# 18. EPIC N — Organization Agent

---

## AGE-050 Organization Agent Profile

Priority:

`P0`

Organization Agent 使用统一 Agent Runtime。

Organization Profile：

```text
name
description
mission
location
members
admins
links
categories
```

---

## AGE-051 Organization Memory

Priority:

`P1`

Organization Agent 可以记忆：

```text
past activities
upcoming activities
venues
announcements
FAQs
partners
community relationships
policies
```

---

## AGE-052 Organization Memory Boundary

Priority:

`P0`

Organization Agent 不允许读取：

```text
member private agent profile
member private memory
member private social policy
```

除非用户明确授权具体内容。

---

## AGE-053 Organization Knowledge Source

Priority:

`P1`

Memory Evidence 可以来自：

```text
organization profile
organization activity
announcement
admin input
public organization content
```

---

# 19. EPIC O — Business Agent

---

## AGE-054 Business Agent Profile

Priority:

`P0`

Business Agent 使用统一 Runtime。

Profile：

```text
business name
description
category
services
locations
contact
opening information
```

---

## AGE-055 Business ≠ Venue

Priority:

`P0`

继续保持：

```text
Business
  ↓ owns / operates
Venue
```

禁止重新把：

```text
Business == Place
```

合并。

---

## AGE-056 Business Memory

Priority:

`P1`

可以包含：

```text
locations
services
menus
opening hours
events
offers
reservation rules
customer FAQ
```

---

## AGE-057 Business Privacy Boundary

Priority:

`P0`

Business Agent 不允许：

```text
read private personal memory
```

不能因为用户来到店铺就获取 Personal Agent Profile。

---

# 20. EPIC P — Community

---

## AGE-058 Community Agent Decision

Priority:

`P0`

当前：

```text
Community ≠ Agent
```

Community 继续：

```text
members
admins
rules
moments
activities
chat
```

不得因为 BT-V5-AGE 自动生成 Community Agent。

---

## AGE-059 Future Extension Point

Priority:

`P2`

架构允许未来：

```text
Community
→ optional Community Agent
```

但本 Epic 不实现。

---

# 21. EPIC Q — Privacy & Permission

---

## AGE-060 Memory Isolation

Priority:

`P0`

必须保证：

```text
Personal Agent A
```

无法读取：

```text
Personal Agent B private memory
```

---

## AGE-061 Cross-Agent Sharing

Priority:

`P0`

Agent-to-Agent 共享：

默认只能共享：

```text
public data
explicitly authorized data
task-scoped data
```

---

## AGE-062 Purpose Limitation

Priority:

`P0`

因为某个 Activity 临时授权的信息：

不能永久进入 Organization / Business Memory。

---

## AGE-063 Delete Cascade

Priority:

`P0`

用户删除：

```text
Moment
```

需要评估其：

```text
Memory Evidence
```

如果某 Memory 唯一 Evidence 已不存在：

需要：

```text
invalidate
recalculate
or remove
```

不能留下幽灵 Memory。

---

# 22. EPIC R — Event Architecture

---

## AGE-064 Agent Enrichment Events

Priority:

`P0`

优先复用现有事件架构。

至少考虑：

```text
MomentCreated
MomentUpdated
MomentDeleted

ActivityJoined
ActivityLeft
ActivityCompleted

PlaceSaved
PlaceVisited

CommunityJoined
CommunityLeft

ProfileUpdated

PreferenceUpdated
```

---

## AGE-065 Async Enrichment

Priority:

`P1`

Agent Enrichment 不应该阻塞主用户操作。

例如：

```text
Create Moment
```

必须先完成用户操作。

然后：

```text
async enrichment
```

不要因为 Memory extraction 导致上传 Moment 失败。

---

# 23. EPIC S — Feature Flags

---

## AGE-066 Feature Flag

Priority:

`P0`

新增：

```text
agent_enrichment
agent_memory
agent_attention_policy
agent_social_policy
life_map
```

不得一次性全量开放。

---

## AGE-067 Pilot Defaults

Priority:

`P0`

Pilot 默认：

```text
Memory = basic
Inference = conservative
Autonomous action = OFF
Sensitive inference = OFF
```

---

# 24. EPIC T — API

---

## AGE-068 Profile APIs

Priority:

`P0`

需要提供：

```text
GET Agent Profile

UPDATE Public Profile

UPDATE Private Agent Profile
```

API 名称按照现有项目 conventions。

---

## AGE-069 Memory APIs

Priority:

`P0`

至少支持：

```text
GET memories
GET memory detail
UPDATE memory
DELETE memory
REJECT memory
```

---

## AGE-070 Policy APIs

Priority:

`P0`

支持：

```text
GET policies
UPDATE attention policy
UPDATE social policy
UPDATE autonomy level
```

---

# 25. EPIC U — Tests

---

## AGE-071 Unit Tests

Priority:

`P0`

覆盖：

```text
confidence
reinforcement
candidate promotion
memory correction
visibility
policy evaluation
```

---

## AGE-072 Privacy Tests

Priority:

`P0`

必须明确测试：

```text
Person A private memory
→ Person B cannot read

Person private memory
→ Organization Agent cannot read

Person private memory
→ Business Agent cannot read
```

---

## AGE-073 Sensitive Inference Tests

Priority:

`P0`

确保系统不会自动产生敏感属性 Memory。

---

## AGE-074 Regression Tests

Priority:

`P0`

本 Epic 不得破坏：

```text
authentication
profile
moment
activity
community
organization
business
notification
agent runtime
```

现有能力。

---

## AGE-075 Migration Tests

Priority:

`P0`

所有 schema migration：

必须：

```text
forward compatible
backward safe
```

不得：

```text
DROP existing important columns
```

除非有明确 migration plan。

---

# 26. EPIC V — Observability

---

## AGE-076 Enrichment Metrics

Priority:

`P1`

至少统计：

```text
memory_created
memory_rejected
memory_corrected
memory_deleted
candidate_promoted
policy_triggered
```

---

## AGE-077 Quality Metrics

Priority:

`P1`

未来用于判断：

```text
recommendation relevance
memory correctness
memory rejection rate
```

---

# 27. EPIC W — Documentation

---

## AGE-078 Cognitive Architecture ADR

Priority:

`P0`

新增 ADR：

建议：

```text
docs/architecture/ADR-AGENT-COGNITIVE-ARCHITECTURE.md
```

描述：

```text
Identity
Profile
Memory
Context
Policy
Autonomy
```

关系。

---

## AGE-079 Memory Architecture

Priority:

`P0`

建议新增：

```text
docs/architecture/AGENT-MEMORY-ARCHITECTURE.md
```

---

## AGE-080 Personal Agent Product Spec

Priority:

`P0`

建议：

```text
docs/product/PERSONAL-AGENT-ENRICHMENT.md
```

---

## AGE-081 Organization / Business Agent Spec

Priority:

`P1`

新增：

```text
ORGANIZATION-AGENT.md
BUSINESS-AGENT.md
```

---

# 28. 实施阶段

不要同时实现全部 81 项。

必须按照以下顺序。

---

# PHASE 0 — Repository Audit

状态：

`P0`

先检查当前项目已有能力。

建立：

```text
BT-V5-AGE-CAPABILITY-MATRIX.md
```

每项标记：

```text
DONE
PARTIAL
TODO
BLOCKED
REUSE
SUPERSEDED
```

不得重复实现已经存在的能力。

---

# PHASE 1 — Foundation

完成：

```text
AGE-001
AGE-002
AGE-003
AGE-004
AGE-005
AGE-006
AGE-007
AGE-008
AGE-011
AGE-012
AGE-013
AGE-014
```

目标：

```text
Profile
Memory
Evidence
Privacy
```

基础架构稳定。

---

# PHASE 2 — Personal Agent Seed

完成：

```text
AGE-015
AGE-016
AGE-017
AGE-033
AGE-034
AGE-036
AGE-046
AGE-049
```

用户注册后：

Personal Agent 不再是 Empty Agent。

---

# PHASE 3 — Behaviour Enrichment

完成：

```text
AGE-020
AGE-021
AGE-022
AGE-024
AGE-027
AGE-031
AGE-032
AGE-064
```

建立：

```text
Moment
Activity
Place
Membership
↓
Memory Evidence
```

---

# PHASE 4 — Policy

完成：

```text
AGE-037
AGE-038
AGE-039
AGE-041
AGE-044
AGE-045
```

建立：

```text
Attention
Social
Autonomy
```

---

# PHASE 5 — Organization / Business

完成：

```text
AGE-050
AGE-052
AGE-054
AGE-055
AGE-057
AGE-058
```

先实现 boundary。

高级 Memory 可以后续继续。

---

# PHASE 6 — Advanced Enrichment

包括：

```text
Memory reinforcement
Memory decay
Life Map
Life Import
Digest
Social screening
Why this?
Organization Memory
Business Memory
```

这些不得阻塞 MVP/Pilot。

---

# 29. 优先级重新分类

## P0 — 必须先有

```text
Profile separation
Memory schema
Evidence
Confidence
Explicit vs inferred
Candidate state
Correction/delete
Sensitive inference protection
Agent seed
Moment evidence
Activity evidence
Place evidence
Context builder
Attention policy foundation
Social policy foundation
Autonomy boundary
Privacy boundary
Feature flag
Tests
ADR
```

---

## P1 — Pilot 后可持续完善

```text
reinforcement
decay
historical moment
city history
Life Map
digest
message screening
Why this?
Organization Memory
Business Memory
metrics
```

---

## P2 — 后续版本

```text
Life Import automation
Community Agent
Level 3 autonomous action
advanced Agent-to-Agent negotiation
```

---

# 30. 明确禁止事项

Codex 在本 Epic 中不得：

### 1.

因为引入 Memory 而重构整个 Birdtie。

### 2.

复制：

```text
Personal Agent Runtime
Organization Agent Runtime
Business Agent Runtime
```

三套系统。

### 3.

把：

```text
Community
```

直接改成 Agent。

### 4.

把：

```text
Business
```

和：

```text
Place / Venue
```

合并。

### 5.

让 Agent 自动公开 Private Profile。

### 6.

让 Business / Organization 读取 Person 私有 Memory。

### 7.

使用一次行为直接生成高 confidence 个性结论。

### 8.

自动推断敏感身份属性。

### 9.

默认启用 Autonomous Messaging。

### 10.

为了实现本 Epic：

```text
reset
revert
discard
```

当前工作。

---

# 31. Codex 自动执行协议

当前 Master Queue 完成后：

Codex 不需要等待用户逐项输入。

按照：

```text
Audit
↓
Plan
↓
Implement
↓
Test
↓
Document
↓
Report
↓
Next Phase
```

自动推进。

每个 Phase 完成：

更新：

```text
BT-V5-AGE-CAPABILITY-MATRIX.md
```

并继续下一 Phase。

只有遇到真正 BLOCKED：

例如：

```text
external credentials
legal decision
production secrets
real organization account
real user identity
third-party approval
```

才标记：

```text
BLOCKED
```

并继续处理其他不依赖 Blocker 的任务。

不得因为一个 Blocker 停止整个 Epic。

---

# 32. Definition of Done

BT-V5-AGE Foundation Ready 需要至少满足：

```text
[ ] Person 有 Personal Agent
[ ] Public / Private Profile 分离
[ ] Memory model 存在
[ ] Memory 有 Evidence
[ ] Memory 有 confidence
[ ] Explicit / Inferred 分离
[ ] Candidate / Active 状态存在
[ ] 用户可以删除/纠正 Memory
[ ] Moment 可以形成 Memory Evidence
[ ] Activity 可以形成 Memory Evidence
[ ] Place 可以形成 Memory Evidence
[ ] Membership 可以形成 Context
[ ] Agent Context Builder 存在
[ ] Attention Policy foundation 存在
[ ] Social Policy foundation 存在
[ ] Autonomy 默认安全
[ ] Organization / Business private boundary 测试通过
[ ] Sensitive inference tests 通过
[ ] 所有现有核心测试通过
[ ] 文档同步
```

---

# 33. 最终产品目标

不要把 Personal Agent 实现成：

```text
User
↓
LLM Chat
```

目标应该是：

```text
                  Person
                     │
       ┌─────────────┼─────────────┐
       │             │             │
     Profile       Moments       Activities
       │             │             │
     Places       Communities      Ties
       │             │             │
       └─────────────┼─────────────┘
                     ↓
              Context Graph
                     ↓
               Memory Graph
                     ↓
                  Policy
                     ↓
             Personal Agent
                     ↓
        Discovery / Social / Action
                     ↓
               Real Experience
                     ↓
                  Memory
                     ↺
```

最终 Birdtie 应形成：

```text
Life
→ Context
→ Memory
→ Agent
→ Connection
→ Experience
→ More Memory
```

这才是 Birdtie Personal Agent 的长期核心。

---

# 34. 收到本文件后的 Codex 第一条动作

如果上一份 Master Queue 尚未完成：

必须输出状态类似：

```text
BT-V5-AGE received.

Status: QUEUED.

Current Birdtie Master Queue remains active.

No current work will be aborted, reverted, reset, or interrupted.

BT-V5-AGE will begin automatically after the current queue reaches a safe checkpoint.
```

然后继续当前工作。

如果当前 Queue 已完成：

执行：

```text
PHASE 0 — Repository Audit
```

并自动继续。

---

# END OF BT-V5-AGE