# Now — Agent Map Workspace

Status: canonical product and interaction specification  
Scope: the Birdtie home experience that combines public local context, an Agent task, a persistent map and a conversation.

This document owns the user-facing behavior of Now. The global interaction rules live in [Global UX Interaction Contract](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md), and map lifecycle/performance requirements live in [Map Runtime Performance Contract](../architecture/MAP-RUNTIME-PERFORMANCE-CONTRACT.md). Agent identity and ownership remain governed by [Agent Identity and Ownership Model](../architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md); CityContext is not a City Agent.

The Now requirements from `Birdtie_Execution_and_CSSA_Partner_Pack.zip!/Birdtie_Execution_and_CSSA_Partner_Pack/docs/02_NOW_WORKSPACE_PRD.md` were reconciled here on 2026-09-30. Its example labels are conceptual; actual UI copy follows the Chinese-first rule below. As of 2026-10-01, the Area Pulse API supplies an initial or explicitly requested area snapshot with exact counts and Activity IDs; camera movement still makes no network request and never destroys the current result. The local Android preview renders Aberdeen tiles and pins using the separately approved existing public token; the newly user-provided token still returns 403 for vector tiles. The Chinese city-level Activity fallback remains available when a valid viewport is unavailable.

## Product position

Now is a local discovery workspace where a person states an intent, receives a useful natural-language response, and explores a stable set of public Birdtie entities through a map, cards, a result sheet and follow-up conversation. The map supplies geographic context; it does not own the query, results or conversation.

The interaction pipeline is:

```text
User Intent → Agent Task → ResultSet → Map / Conversation / Action
```

Every projection refers to the same stable Entity ID. Activities, Groups, Places and opted-in coarse public People areas may be shown according to their publication and privacy rules. A private or precise live user location is never a public map entity.

The current API is a deterministic rule-based MVP. The interface must describe only capabilities and results actually returned by the API; it must not imply AI reasoning, semantic matching, recommendations or unsupported actions.

The Now interface follows the product's Chinese-first language contract: Simplified Chinese is the default for all controls, helper text, Agent responses, empty/error/loading states and suggestions. Preserve original-language entity names and user-authored requests. English is secondary and must come from a complete explicit English localization, never from English-only hardcoded UI.

## Main flow

1. Open Now. Keep the selected City and persistent map visible while city data loads; loading or failure does not replace the workspace.
2. Enter a natural-language request, choose a Quick Action and then provide details, tap a suggested prompt, or submit a follow-up in the active task.
3. Keep the current ResultSet and selection visible while a new turn is pending. Show a pending state in the conversation/composer.
4. On success, append the Agent's response message, atomically replace the ResultSet and clear the old Pin preview so the Agent Results Sheet has focus. On failure, preserve the prior result and offer Retry.
5. Project the ResultSet into pins and result cards. Selecting any projection selects the same Entity ID and opens its preview.
6. Open a result sheet for the complete result list or conversation. Follow-ups reuse the task, principal, CityContext, intent, filters, time range, map bounds and selected entity where applicable.
7. “New” explicitly starts a separate task. It does not discard saved Recent tasks.

## State ownership and projection sync

Keep these state axes independent:

- `queryState`: idle, composing, submitting, failed.
- `mapState`: persistent map instance, current viewport, camera motion and pending Search this area bounds.
- `selectionState`: one stable `selectedEntityId` or no selection.
- `sheetState`: closed, preview, result list or conversation, plus extent.
- `inputState`: draft, focus, keyboard and optional voice transcription.
- `conversationState`: task ID, ordered user/assistant messages and pending turn ID.
- `resultState`: the current immutable ResultSet and its provenance.

Pin, preview card, result row and detail/result sheet are Entity Projections. They must resolve from the same entity data and stable ID, never from parallel copies of a title or coordinate. Viewport changes update only `mapState`; a successful Agent turn also clears the preceding Pin selection when replacing the ResultSet. Keyboard and focus changes do not clear selection or results.

## Search this area

Camera movement updates the viewport only. When the camera settles after user movement, expose a “搜索此区域” action with the settled bounds. Do not issue a request from camera callbacks. Only an explicit tap starts a bounds-scoped query. Keep the existing ResultSet until the new request succeeds, then replace it atomically. Ignore or cancel stale responses. A programmatic camera animation must not create a pending search action.

The initial browse state may search public activities in the visible bounds. A follow-up area search carries forward the active intent and filters. Area queries remain limited to the active City and the API's public visibility rules.

The Area Pulse is a compact projection of already-loaded, public Activity data whose coordinates fall inside the settled viewport. It shows the total and at most two category facets by default, no more than three factual counts. It is hidden while the camera moves and does not issue a request on its own. Activating a pulse sends an explicit bounds-scoped query. It is independent of the active Agent intent. The active Agent intent has its own structured action/category/time summary; its original sentence stays in the conversation.

On first open, the top-left panel must immediately explain in Chinese what this map area contains. While the native viewport is starting, show a short loading message; when no public Activity is visible, show an honest empty state. Do not imply access to a precise device location. When markers overlap at street zoom, provide a small member picker so each entity can be selected and shown in a lightweight Entity Card.

If the map style or source is unavailable, replace the map with a clear Chinese failure state and keep the Agent composer usable. Never calculate area counts or offer a bounds search from an uninitialized globe viewport. A closer follow-up must state that the previous activity constraints were kept and that ordering uses the configured city center rather than device location.

## Quick Actions and prompt suggestions

The `+` control opens a Quick Action sheet with these action types:

- Find activities
- Find groups
- Find places
- Ask about this area
- Create an activity
- Use current map area

Choosing an action sets the requested capability/context; it does not silently submit a canned query. Unsupported action types must be disabled or answered with an honest capability message until their API action exists. Prompt suggestions are a separate, independently selectable layer and may include time- or City-relevant examples. Suggestions populate a draft; the user confirms Send.

## Voice

Only render a microphone control when a supported speech-to-text implementation can manage permission, listening, transcription and an editable draft. Recognition never auto-sends. If that foundation is absent, do not display a microphone affordance.

## Result and conversation presentation

Every submitted turn is represented by a user message followed by an Agent response message before structured results are presented. Render actual Agent messages from the response/task; do not use hardcoded category-specific response prose. `AgentResponse` consists of a message, a ResultSet, map effects, available actions and follow-up suggestions. Empty results include an explanation and relevant next steps. Errors keep the workspace and prior result intact with Retry.

The compact selection preview keeps the identifying title readable across at least two lines. Selecting a Pin opens an Entity Peek Card and never creates an Agent task. “查看” opens the selected entity's detail projection. The Agent Results Sheet is separate and uses the structured Active Intent as its collapsed heading; the original query remains in the conversation. Its extents are hidden, peek, medium and expanded; tapping or dragging the handle changes one level at a time. The expanded sheet displays complete titles and supports production text lengths, narrow widths and enlarged text. CTAs must not take the title's reading space. Loading never blanks the map or flashes away existing results.

Map markers use distinct visual identities for Activities, Places, Groups, People and clusters. Nearby public map entities may be clustered according to the current map zoom. Cluster IDs are stable for their member set; tapping a cluster opens a compact member picker so each item can be selected without moving the camera or creating a pending area search.

Follow-up messages reuse the active `conversationId`/task ID and retain the task's prior intent, filters, CityContext, time range, ResultSet and optional map bounds. A pending turn keeps the previous Pin selection visible. A successful turn clears that preview when replacing the ResultSet so the new Agent results take focus; map movement alone never clears selection.

## Privacy and actions

Only published, visible Activity, Organization/Group, Place, Public Event, Public Community and coarse CityContext entities may appear on this map. A People marker is allowed only when its owner explicitly opted in to an approximate public area. Never request or publish precise real-time user location through Now. Entity actions must use the current visibility and authorization checks; a pin does not grant access.

## Acceptance criteria

- Keyboard open/close preserves the native map instance, map viewport, marker IDs, selection and current ResultSet.
- A stable entity ID resolves to the same entity from pin, preview, list row and expanded sheet.
- Pin selection opens a compact Entity Peek Card without changing the Agent task, result, conversation or result-sheet extent.
- Area Pulse counts reflect only already-loaded public Activities inside the visible bounds, show at most three factual facets and remain hidden while the camera moves.
- Cluster marker IDs remain stable for the same member set; tapping a cluster opens its member picker without creating an area-search request.
- Camera movement produces no network request and does not clear task, conversation, results or selection.
- Search this area appears only after a settled user camera move and issues one bounds-scoped request only after explicit activation.
- A slower older search cannot replace a newer successful response.
- A pending search keeps the prior map pins and cards; success swaps both from one ResultSet.
- Each turn appears in conversation as user text then an actual Agent response before structured results.
- Quick Actions open as action types; no `+` action sends a hardcoded badminton query.
- No inert microphone control is displayed without speech support.
- Long titles remain identifiable in preview and complete in expanded detail; controls remain tappable with enlarged text and safe areas.
- Empty/error/loading states preserve the workspace and present honest next steps.
- Public People map positions remain coarse and opt-in; no live location is exposed.
- On a physical Android device, typing, opening/closing the keyboard, selecting a pin and moving the camera do not recreate the map, clear results or visibly stall the composer.

## Current implementation boundary

As of 2026-10-01, the deterministic MVP router handles public Activity, Organization and Place searches, explicit bounded area discovery, contextual refinement and comparison of previously returned Activities. Create Activity offers the real Organization Console only to an authorized owner or admin; it does not publish automatically. People and Group natural-language searches remain outside this task router. All results retain the actual source and public visibility checks.


## 2026-10-04：六类当前地图来源与独立本人意图入口（MAP-001）

本轮复用原实体与权限：公开 Place、Activity、Moment、Organization、Business 进入同一闭合图层；本人明确开启的 Opportunity 为默认关闭的 PRIVATE 叠层，原 Candidate ID 只指向仍 PUBLIC 的 Activity 和真实公开 Place。ONLINE Intent、私人 Moment、未审核坐标、私人用户位置不成为地图点位。具体合同见 [六类来源](../architecture/TYPED-MAP-LAYERS-V4.md)。

Pin、EntityPeekCard 和图层列表共享 `kind:originalID`；原详情接口重新核当前 ACL。图层开关和刷新不会创建 Task、改变摄像机或清除选择；刷新保留尚未到期的快照，旧有效期绝不因等待新请求延长。身份、组织、City、API/client frame 变化立即退休旧源和详情。

复用 NOW-002 的原生本人意图卡：无 Task/选择时首屏可滚动容器承载 Card 与 Local Pulse；常驻 48dp「我的社交意图」入口打开独立可滚动 Card，保留地图和当前 Task。正文来自本人原生 selector，不是固定摘要；管理继续使用原 ID 和本人具体批准。容器只占可用区域，不缩小字体；320px/font3、旧端点 A→B→A、20 次输入/IME 回归见本轮目标证据。

上述能力为 CODE_LOCAL；整仓与真机由根代理对新冻结帧验收。TalkBack、真人供给、模型、部署和正式发布不由目标测试推定通过。
