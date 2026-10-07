# BT-V4-MAP-001 增量审计与实现

2026-10-04。原任务恢复，旧 PARTIAL 对象、依赖和证据由根代理保留。本 worker 不修改 live 队列或共用完成报告。

## 初始实际差距

原地图投影只覆盖 Place / Activity / Organization；Moment、Business、本人 Opportunity 没有统一当前来源。旧 enum、native bitmap 和 Flutter marker 缺三类；MapCanvas / MapWorkspace 有重复投影逻辑，图层可见性无闭集控制。已有私人机会、明确组织坐标审核、Moment 人工公开、Business 经营场地和七类稳定详情均可复用，不新增账本/DDL/权限。

计划：闭合来源/HTTP → 原生当前性和隐私验收 → 单一客户端投影与图层控件 → 身份/transport 生命周期与移动端验收 → 冻结/归档 → 根代理整仓 build 和真机。

## 实际失败链（保留原日志）

- `model-red1`：新闭集 contract 未实现，编译 undefined。实现后 `model-green1` 通过。
- `native1`：17 PASS。`native2` 复制帧遇另一并行 AIR-016 的 candidateRunGuard 尚未落地，编译失败；不改其源，待根实际文件落地后继续。
- `native3`：18 PASS / 1 FAIL；实际原 Activity modality 是小写 in_person，初实现错误用大写导致合法活动缺失。改原查询闭集并要求 confirmed；不改原数据/断言。
- `native4`：fixture 未填写 IN_PERSON 必需明确地点。`native5`：fixture 用 LOCAL 却无本人 City 声明，原激活 correctly conflict。fixture 改为本人 PRIVATE、具体 Place 原约束和原激活路径，不伪造 Context 或公共供给。
- `client-target1`：fixture 错 package name 编译失败。`target2`：MockClient 中文 Response 缺 UTF8 header；修 fixture。`target3`：font3 列表 scroll 动画未 settle，点击在屏幕外；加入 pumpAndSettle 仍保持真实点击和原 ID。
- `target4`：fixture SchedulerBinding 未初始化 / Matcher 非 Pattern；另真实发现默认 Place 的 selected native bitmap 与未选相同。增加选中 stroke6/默认4，保留 glyph/缓存，正负测试通过。
- `map-entry-target1`：退役详情仍是可返回的当前 route，root Map offstage，测试误用默认 finder；改先确认退役文案、A→B→A 不复活，再真实 pageBack 核原 Element，并未削弱失效断言。`map-entry-target2` 通过。

native6 实际 22 PASS、0 FAIL/SKIP/pkgFail、test/vet/build0；源快照816稳定，原 public 不变，owned 库 DROP。target6 实际37功能通过，analyze3=0；这些是目标证据，后续接续代码仍需最终同帧回归，不冒称全仓或真机通过。

## 边界

公开五类型与本人私人机会分开。没有私人成员/地址/位置来源，无模型调用、机器用途升级、联系人/RSVP 写入、隐式发布。旧任务/query/CITY map 生命周期不重建；具体绑定退休后的旧正文/来源不复活。原 canonical 增量补合同，不覆盖历史材料。最终状态由根核证原 AC 后决定。


## 最终本地目标和冻结

- active-entry-red1 实际首屏原生 Card 数量为0；接入原 Card/Pulse 有界滚动后 active-entry-green1 通过，常驻48dp入口保持同一 Map Element。
- map-refresh-red1 实际刷新开始清空仍有效Pin；保持旧快照/旧lease且不让新请求延长后 map-refresh-green1 通过，包括等待期间自然到期。
- active-entry-mobile1 实际320px/font3、当前本人原生 selector、旧 endpoint A→B→A 退休、返回、0非GET通过。
- client-target7 实际39 PASS；client-target8 实际41 PASS，包含原20次焦点、IME/Task/result/selection、typed Pin/卡ID、原详情与迟到响应。
- analyze4 只有新增 if 的 curly-braces style info；补块不改变逻辑，analyze5 实际0。冻结23源的 source-freeze1 清单由根独立核验；根全仓/构建/手机仍待执行。
- 首次 freeze manifest 脚本用错 `utf8-sig` 名称，启动 LookupError、未写任何文件；修正为 `utf-8-sig` 后成功。这不是测试或实现通过证据。

原32scope receipt 实际列29路径项，其中包/文档/evidence/work为目录；实际产品/测试源共23文件，未改server/main/DDL/队列或他人源码。NOW-002 worker拥有原Card/Page实现，本线唯一接MapWorkspace入口。所有历史失败和 copied native2 WIP 编译故障保留，不冒充全仓或真实供给。

## freeze2 账号边界 RED→GREEN

根授权仅MapWorkspace/auth test精确修补。map-account-aba-red1：同opaque token账号B时旧6条图层仍显示；map-account-aba-green1真实通过。最终test同时加载本人PRIVATE缓存并挂起第二private回包、A→B→A后完成迟到响应，验证public/private清空、OPPORTUNITY关闭、ownReads固定2无自动重读、原selection/Map Element保留、零非GET。client-target9全8测试文件42PASS；analyze6全部16项0，源freeze2精确23文件只改2Dart。7Go不变，native6还是本轮实际原生证据。历史freeze1/archive保留，root整仓/手机待其当前帧独立记录。
## 匿名入口 freeze3 差分

根代理实际手机发现匿名「我的社交意图」打开空面板。本 worker 不操作手机；按已有 lease 仅修改 MapWorkspace 和 shell test，与 NOW002 Card owner 协调中文提示和可选 `onClose`。匿名 Card 的说明与权限状态由原 controller 决定，无新认证或授权路径。

本地 `anonymous-modal-red1.log` 实际缺「关闭」0widget；仅 modal 包装传 `Navigator.of(sheetContext).pop()` 后 `green1` 1PASS。320px 原入口/中文提示/48dp关闭/同MapElement/保留内联提示且无内联关闭均通过。`client-target10` 42PASS/1FAIL 是原线上草稿测试的全树中性标题 finder 重名，保留日志，改为实际 `SocialIntentDraftPage` 子树定位后 `client-target11` 43PASS；`analyze7` 16items无issue0。

source-freeze3 23源仅2Dart变化；7Go与native6逐字节相同。final1/2与历史已绿帧不覆盖，final3仅不可变差分。最新整仓/构建/真机由根代理验收；本地合成资料及 Debug 不解除 Closed Pilot/Consumer Beta NO。
