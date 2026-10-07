# Community and Activity Social Model

Date: 2026-10-01  
Status: Accepted domain contract; implementation status is tracked separately in `automation/codex_task_queue.json`.  
Source: `C:\Users\chens\.codex\attachments\d394a7fd-33f0-41fd-91c7-dbe4a1e2c51b\已粘贴的文本.txt`.

This document extends [Agent Identity and Ownership](AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md). It does not change the Personal Agent, Organization Agent or CityContext authority boundaries.

## 1. Entities and identity

| Entity | Meaning | Account and Agent |
| --- | --- | --- |
| Person | A real human identity | One person account and one Personal Agent |
| Community | A persistent, self-organized social container | No account, login or Agent in this release |
| Organization | A formally managed society, association, club, business or institution | Separate organization principal and one Organization Agent; real people administer it |
| CityContext | Platform-managed public city context | No social identity, account or Agent |
| Activity | A temporary social event | Has exactly one Person, Community or Organization organizer |

An old `organization_type='community'` denotes a formally managed Organization, not a conversion of a Birdtie Community. A Community owner is a real person and remains a separate identity. Neither an Organization nor a Community is a fake human user.

## 2. Community and Organization

Community needs no official or institutional claim and is a durable context for members, activities and future relationships. Organization uses its existing membership, verification and Agent principal model. One person can belong to many of each. Creating a Community must not create an Organization or Community Agent. Creating an Organization must not silently create a Community.

## 3. Community membership and lifecycle

Community visibility is `PUBLIC` (discoverable with public details), `PRIVATE` (discoverable with limited detail), or `HIDDEN` (not publicly discoverable). Join policy is independent: `OPEN`, `REQUEST`, or `INVITE_ONLY`. Membership roles are `OWNER`, `ADMIN`, `MEMBER`; states are `ACTIVE`, `PENDING`, `INVITED`, `LEFT`, `REJECTED`. The creator becomes an active owner in the creation transaction. Every active Community retains at least one active owner. Ownership transfer adds the new owner before demoting the old one. `ARCHIVED` prevents new joins and activities, preserves membership and activity history, and removes the Community from discovery; no hard delete is offered.

## 4. Activity organizer

An Activity has exactly one organizer: its creator Person, an authorized managed Community, an authorized managed Organization, or an independently verified Business. Migration 041 added the fourth nullable foreign key and extended the exactly-one constraint; the relation does not rely on unchecked type/id text. Existing organization activities were backfilled to Organization organizer without losing rows, preserving `organization_id` compatibility for publishing, reminders and map associations. Person organizers may create only for themselves. Community `OWNER` and `ADMIN` may create for the Community; Organization permissions retain their existing server checks. Business requires an active verified principal and active Owner/Admin Person, plus a verified Venue relation when a Place is specified. A Community creator does not gain Organization or Business authority.

## 5. Activity visibility and participation

Activity visibility is independent of Community visibility: `PUBLIC`, `ORGANIZER_MEMBERS`, or `INVITE_ONLY`. A private Community may publish a public Activity. Members-only requires a Community or Organization organizer; a Person organizer gets validation error. Outsiders receive no private activity details (404). Invite-only requires an explicit invitation, not mere Community membership. RSVP to a public Community Activity never joins the Community. Existing activity participation and Community membership are separate records.

## 6. Permission matrix

| Action | Owner | Admin | Member | Outsider |
| --- | --- | --- | --- | --- |
| View public Community | Yes | Yes | Yes | Yes |
| View member content/activity | Yes | Yes | Yes | No |
| Edit Community and archive | Yes | Limited edit; no archive | No | No |
| Approve join request | Yes | Yes | No | No |
| Manage ordinary members | Yes | Yes | No | No |
| Manage admins and transfer ownership | Yes | No | No | No |
| Publish Community Activity | Yes | Yes | No | No |

The Go server resolves actor identity from the verified session and checks every read and write; Flutter controls are only presentation. `401` means no authentication, `403` means insufficient authority, `404` hides inaccessible private resources, `409` means conflicting membership state, and `422` means invalid input or organizer/visibility combination. Hidden Community direct-link access is limited to authorized members or valid invitees.

## 7. Agent boundary

Community is first-class social data but is not an Agent-bearing principal. A Personal Agent may query or act on a Community only through its human's permissions. There is no Community Agent, feed, paid membership or Community map marker in this release. The initial Community baseline excluded chat; V4 `BT-V4-CHT-005` adds a separate member conversation under [Community Conversation V4](COMMUNITY-CONVERSATION-V4.md), with explicit enrollment and current membership authorization. Conversation does not replace public discovery or create friendship.

## 8. Social loop

Discover Activity → Join Activity → Meet people → Discover Community → Join Community → Persistent social relationship → More Activities. The reverse path is Join Community → See Activities → Participate → Strengthen relationships. Neither path silently changes membership.

## 9. CSSA pilot compatibility

The critical path remains production identity, real authorized CSSA/activity data, real A→H validation, and the Closed Pilot gate. A real person may create a pilot-unverified Organization and later transfer authority to a real CSSA representative. No fake CSSA Person account or shared Organization login is created. Community work is tracked separately and becomes a pilot blocker only if it breaks existing identity, Organization, Activity, map, reminder, build or data integrity behavior.

## 10. Migration decisions

Extend the existing `communities` table rather than create a competing table. Preserve legacy community IDs and published group submissions. Backfill an active owner membership for each existing Community. Add lifecycle, join policy and membership constraints without reclassifying Communities as Organizations. Add a constrained organizer relation, backfill every existing Activity according to its Organization or Person host, and reject unmappable rows before enforcing completeness. Keep existing Organization activity API fields during transition. Run up/down/reapply on disposable data and check unchanged activity counts, non-null organizers, no orphan memberships, and existing seeds/E2E before release.

## 11. 活动管理权限复核（BT-V4-ORG-002）

四类主办方活动的创建、编辑、发布、取消均由服务端按真人与主体关系判定：个人仅本人；社群仅活跃 Owner/Admin；组织仅活跃 Owner/Admin 且组织主体账号活跃；商家仅已核验且活跃的 Business 的活跃 Owner/Admin。普通成员、外人、被降权者和已停用的组织主体不得继续管理。`socialActivityManager` 在写入锁内重检这些条件，组织旧 API 同样检查主体账号状态。Business 场地资格被撤销后，公开活动即不可见；045 允许有权主办方只做取消变更，避免活动困在已发布状态，其他 Business 写入仍须通过原核验。

## 12. 当前真人成员管理与发现（BT-V4-COMM-002）

原注册社群 API 使用当前真人 Session 的窄网关，读写均重核活跃 Person、会话、社群和成员权限；旧内部 actor-ID 方法保留兼容，不作为 HTTP 降级路径。HIDDEN + OPEN 也不允许外人仅凭已知 ID 加入；有效邀请只允许查看有限说明并显式接受，尚不是成员。公开活动发现和 RSVP 不以社群成员身份为前置，也不自动加入社群。

管理动作（编辑、归档、邀请、审批、角色变更、移除、转让）先读取原路径的当前预览，再展示对象和后果，使用同一具体源快照提交。缺预览回执返回 428；主体、会话、目标、参数或真实源状态变化及过期后回执拒绝。服务端没有仅 ownerID 的写入 fallback；草稿、回执和成功分别表达。具体协议与中文界面见 [Community Discovery and Membership UX V4](COMMUNITY-DISCOVERY-MEMBERSHIP-UX-V4.md)。

本轮“转让所有权”仅变更当前 `community_memberships` 的 OWNER/ADMIN 管理角色。`communities.owner_account_id` 继续保留原创建人与既有来源授权锚点，`createdByUserId` 不伪造为接任者。059 城市活动来源选择、061 通知来源和旧提交/撤回入口各自保留原来源权限；角色转让不自动转移其授权、改变创建历史或发出通知。需要更广泛来源移交时，应另核原领域合同与真实许可，不能批量改创建人列。本轮没有新增表、Community Agent、关系或自动认知授权。
