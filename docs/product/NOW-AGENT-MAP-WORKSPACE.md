## 2026-10-11 增量20：当前执行中的交互契约

最新用户附件 f3460764… 替换旧单入口与首层三类列表设计。右上角为“联系”和“通知”两个20px图标、48dp触摸区；任一时刻只展开一个区域。联系首层以约四个近期头像、短姓名及真实未读为基线，末尾“全部会话”；首层不显示正文、好友/关注管理或添加菜单。选人后同区显示最近20条真人消息，按需分页历史；全部会话下进入联系人管理，关注、好友与私信授权仍分离。通知首层少量近期事项加查看全部，真人消息不重复计入通知。

访客只有一句登录提示与一个按钮，不请求私密列表；原身份边界、一次重新登录、账号草稿和迟到响应守卫继续有效。统一输入框固定麦克风与发送，Agent转写必须可编辑并手动发送；真人录音预览、试听、取消及发送遵守明确收件人。加号仅使用实际接通的材料/创建能力，原生粘贴仍可用；私人记录加图留在其编辑流程。Moment、旅程、主题地图、视频、收藏、活动等原任务与能力保留，未接通不得以入口或预填文字冒充完成。

透明材质复用NowUiTokens / BirdtieSurfaceTokens，局部裁剪、单层sigma4，头像/文字锐利；减少效果以轻量底色回退。必须以同源profile无模糊/磨砂样本及release复核衡量性能，不将Flutter FrameTiming代替原生地图呈现数据。39条验收已追加原BT-FIX-NOW-UI-001，当前IN_PROGRESS；本节是执行契约，测试与真机结果单独取证，尚不代表验收完成。

以下保留旧批次实际历史。

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

## 2026-10-11：V4 / V4.1 Now 交互整合

本节合并用户最新完整16节要求和手机检查中的调整，继续沿用 BT-FIX-NOW-UI-001；同一语义有冲突时，本节取代上述较早的首屏意图卡、Local Pulse 和右侧 Inbox 呈现方案。完整原文保存在本轮 evidence/source/user-request.txt，不另建重复任务。接入规范不表示所有功能已完成；实际源码、构建、逐项 NOT_RUN 见同日整合报告。

- 默认 Now 保留一张持续存在的原生地图、唯一输入框、左上菜单/城市与右上好友关注入口。菜单与小图标轻量化，但操作区域至少48逻辑像素。展开内容由宿主管理；键盘开合不新建地图、不重置当前结果或选择。
- 左右边缘不使用渐变或模糊。朝北/方向控件位于左侧，仍执行原生回北操作。顶部和底部的纵向可读性处理继续服务现有主控和输入框，不能复制地图作模糊背景。
- 右上为统一社交入口，消息、联系人、通知在同一区域替换内容；默认近期消息。联系人内部筛选好友和关注。访客只出现一次登录提示，不读取私人列表；空数据、权限拒绝、离线和服务异常分别表达。展开宽度能容纳头像与一行姓名，高度按内容增长并有上限，超出内部滚动。
- 地点轻预览依附当前原生点位，折叠宽上限250；详情仍指向原Entity ID。动态横向一次最多呈现三条，不是数据上限；通过原公开权限分页继续查看所有当前可见结果。地点图、作者头像和唯一作者数必须来自真实公开字段，缺失保持诚实缺失。
- 公开动态页先按原ACL过滤，再统计与取页；首五条和后续页保持同一窗口、来源与原期限。点击任一页中的动态仍重新核该具体ID/版本/公开作者。失效或读取错误清掉旧行并给出实际重新读取动作；重新读取不延长原地图/预览许可。
- 城市地理选择、实际所在城市、探索城市与业务查询范围各自拥有状态。城市弹窗不因为旧地图快照到期而丢失选择；关闭后需要时读取当前合法新数据，不延长旧授权。组织与个人资料、草稿、对话目标和未知结果仍严格隔离。
- Agent与人际聊天共用地图内输入、按会话隔离草稿、真实媒体/作者统计、路线旅程、长期记忆与合规数据复用等仍按原领域合同完成与验收；独立人际会话页或本地规则结果不冒充这些完整能力。

继续复用已批准文字角色：页面20/w600，面板18/w600，卡片16/w500，正文与Agent15/w400，输入16/w400，动作14/w500，辅助13/w400；中文通常1.5行高。标题/正文/次要文字映射NowUiTokens与既有主题，深色单独适配，保留原字体和TextScaler。社交文字背后允许局部渐变衬底，不能模糊整张地图或延伸成满高侧栏。真实背景对比度、100/130/200%、窄屏长中文、键盘、AT和性能须逐状态验收。

### 2026-10-11 接续：统一身份、社交空间与材料草稿

本次用户要求并入原 BT-FIX-NOW-UI-001，与上一批语音、图片输入修改合并执行，取代并排或纵向堆叠好友、关注、对话、找朋友和收件箱的方案；保留原任务队列及历史。源码和局部测试完成不等于真机交付验收完成。

- 未登录时右侧只显示“登录，与朋友保持联系”和“登录”。访客继续浏览合法公开地图。登录返回原右侧类别或对话；发送、发布、申请和未知结果恢复都需要新的明确操作。登录过期只启动一次恢复；403不能被当成登录过期。
- 共用身份边界覆盖私人读取、收藏、分享、发布、材料和消息。退出或切换账号同步清除可见旧内容，迟到响应、同凭证账号变化及 A→B→A 都不得重新填入。服务端继续核验身份、关系、屏蔽和具体资源权限。
- 近期消息只呈现真人会话及历史；消息请求在消息内可达。通知承载好友申请与事项，消息已读与通知已读各自维护。关注不是好友或私信授权。每次只显示一类内容，不追加另一列；空状态只给一句相关提示和一个有用动作。
- 联系人标题旁添加按钮直接打开搜索用户、扫一扫、我的二维码。搜索使用受控公开用户目录；扫码只按需使用相机，拒权、不可识别和扫描错误有恢复操作。二维码只编码公开 UUID 的 `birdtie://person/<UUID>`，无登录凭证、手机号或私密字段；识别后先查看资料，再按既有关系规则明确申请或关注。不得自动打开外部链接或建立关系。
- 社交类别与图标具有清楚名称、访问标签和足够触摸区域；长姓名省略并可进入完整资料。局部渐变保护道路标签上的文字可读性，阅读区域随大字体适当扩大。点击地图或关闭/返回收起浏览；录音中阻止直接切换或丢弃，未发送草稿按收件人保存。
- 真人对话复用 Now 唯一输入框，清楚显示收件人。退出真人对话恢复 Agent 原草稿。语音转写先回填供编辑，不能自动发送；首次相关语音提示可取消，之后不重复。图片作为当前任务材料，用户检查后才提交；助手尚未具备的读图能力必须说明。
- 原验收必须提供同一构建的访客、登录过期、已登录无数据、有联系人与未读消息、网络异常截图，以及登录恢复、类别切换、添加和扫码流程、双账号读写/屏蔽/切换隔离证据。只有扫码入口或模拟识别不能换算成真实扫码验收；缺项保留未完成。


## 2026-10-12：当前19验收边界

本轮保持上述单一社交空间、共享身份与唯一输入框。权限拒绝或历史尚未读取时不显示空对话；文字发送按钮、键盘发送和旧回调都必须再次核当前对话能力，失败/待核实时保留草稿。取消本地录音草稿不能取消真实权限或网络错误。

client10/API02已取得同构建访客、真实空数据、联系人与未读消息、登录过期、网络异常截图；合成双账号真人文字、未读与通知分离、屏蔽后拒绝读取/发送、原Agent草稿和同账号过期回原类别/筛选通过。200%权限提示可读，拒绝后发送禁用，设备字体与本地隧道已恢复。

Agent语音转文字仍无成功证据：08实际服务连接超时，系统输入法听写成功不代替应用识别成功；10未再次采音。真实扫码/好友音频/图片分别保留08/07的实际来源，10完整媒体矩阵、真实认证、读屏、iOS、相机负向场景与完整发布验收仍未闭合。当前任务为PARTIAL，详见[当前整合报告](../reports/BIRDTIE-NOW-V4-V41-INTEGRATION-2026-10-11.md)；这不是全应用完成声明。


## 2026-10-19：内容优先与默认串文顺序线

用户在原设计会话明确要求动态为主、原输入框评论/回复、关联地点完整分页及旅程入口，随后明确要求默认显示合法串文顺序线。当前增量以 [内容优先规格](BIRDTIE-NOW-CONTENT-FIRST-2026-10-19.md) 和 [默认路线补充](BIRDTIE-NOW-CONTENT-ROUTES-2026-10-19.md) 为准，接续 BT-FIX-NOW-UI-001。该要求覆盖本文较早的地点聚合主投影、仅选中后显示线与不增旅程入口的冲突描述，历史内容保留。

每条原 Moment 与合法关联/锚点独立，内容时间采用首次发布或已知历史创建来源；同坐标不合并、不移坐标。默认线只连权威相邻且双方当前获准的已加载节点，缺失/无定位/撤权节点留下断点。私人 Journey 与明确批准的公开版本分开；点击旅程不公开计划。Now 继续只有一个输入框，各评论、回复与原会话草稿隔离，沿用现有领域写入与未知结果恢复。

适用 UX-CHECK-01/04/05/06/07/08/09/10/11/12/13/14（正常字号）/15/16；继续保留78号地图生命周期修复和原任务证据。本增量源码、定向测试与79/API39本地构建已完成，实际结果见原整合报告2026-10-19节；新包手机验收NOT_RUN，不能沿用78号结果。Web与TalkBack仍按用户本轮范围延期，未授权本增量生产部署，不录屏。
