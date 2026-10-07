# BT-V4-NOW-001：结果面板布局恢复审计

2026-10-04。本轮仅恢复已登记局部布局子范围，原任务完整对象与PARTIAL历史保存在 `work/v5-age038-resume/original-now001-layout-partial.json`。实际队列三项依赖 BT-V4-OPP-001／CTX-001／TST-001 均DONE；本worker未修改live队列或总报告。原验收“支持非地图/线上/跨城社交工作区”仍未全部满足。

## 用户结果与规则

个人用户在Now表达意图后，可检查当前结果、来源提示、错误和重试，展开对话；键盘、结果面板高度或文字设置不清空当前结果、所选实体或地图。继续使用中文、原领域动作和48dp可操作拖动柄。适用 GLOBAL-UX 的 UX-CHECK-01/04/06/10/11/13/14/16，规则引用本身不算真机或发布验收。

复用：原 ActiveIntentSummary、_ResultList、AgentConversation、MapCanvas、当前workspace controller和对应领域详情/动作；仅扩展布局。无新查询接口、数据源、同意账本、日志正文或非城市授权。已有城市结果里的无坐标地点仍经原稳定ID详情路径打开。

## 真实现状与历史证据

旧 `work/v5-age038-resume/prior-device-attach-observation.json` 的 d1b736… Debug APK attach35429记录 Column w375.4/h104 底部overflow50，随后Lost connection。具体触发、字号、当时数据未知；不是当前68f或本修补APK的真机失败/成功。新widget用同375.4×104及1.8文字倍数复现Column溢出；不能把这个fixture反推为旧设备已知触发。

**更正之前只读推测**：当前 `map_workspace.dart` 实际已明确 `resizeToAvoidBottomInset:false`，没有默认Scaffold再次扣IME；前次“Scaffold默认true导致双扣”推测错误。当前问题是手动剩余高度可能为负，以及结果sheet固定非滚动header在动态字号/错误长度下超过104或实际父限制。当前证据按实际单扣IME记录。

## 修补

- 所有detent先按原700→301/546等正常比例计算，再限制实际父可用高度，保持正常展示；小空间顺序有界，负可用高度归零。
- 独立48dp handle与48dp对话按钮；tap和上下drag保留原extent动作，文字滚动不抢走handle。header标题/来源提示/长中文错误不截行、不缩字；peek使用可滚动header，medium/expanded通过NestedScrollView协调原结果/对话列表。
- hidden或实际动画高度不足48时不留隐形交互子树；尊重DisableAnimations。动画0/100/260ms覆盖。
- 单次消费IME：body完整，visibleHeight=body−inset，再扣原120保留空间并归零。键盘开且sheet≤48，body键盘上真实空间≥48时显示中文“收起键盘查看结果”48dp入口；unfocus＋medium，原composer与draft保留。正常布局恢复原TopControls。极小body<48不放越界恢复控件。
- 原AgentConversation搜索提示Row的Text加Expanded，320宽3倍字可换行；不改变查询、聊天状态或请求权限。

## 实际检查与失败链

从 `apps/client` 运行Flutter，原始日志和退出码在 `work/v4-now001-layout/`，均不覆盖。

| 帧 | 实际结果 | 原因/修复 |
| --- | --- | --- |
| flutter-red1 | 1功能＋1加载PASS，12FAIL，exit1 | 原Column在同尺寸/大字/小父/长error/动画真实失败 |
| flutter-green1 | 26功能＋3加载PASS，2FAIL | 原700比例错误先裁为600使301变258；改保留比例后只限制最终高度。scrollUntilVisible跨nested ensureVisible把retry卷走，改真实逐次drag直到hit-test，仍必须点到实际retry |
| green2 | 28功能＋3加载PASS，0失败 | 原detent回归及受限布局通过 |
| green3 | 30功能＋3加载PASS，0失败 | 增加IME单扣、恢复与<48没有隐形动作 |
| flutter-search-red1 | 0功能＋1加载PASS，1FAIL | font3 expanded searching Row水平overflow211真实负例 |
| green4 | 59功能＋7加载PASS，1FAIL | Text修补后没有overflow；fixture固定12次drag把lazy搜索行卷出销毁，改在真正可点击/可见行停止 |
| green5 | 60功能＋7加载PASS，0失败 | 搜索内容与原身份/焦点回归通过 |
| green6 | 60功能＋7加载PASS，1FAIL | 语义fixture原只断tap，实际原生drag还有scrollUp/Down；补完整两动作断言，未改产品语义 |
| green7 | 61功能＋7加载PASS，0失败 | 48dp中文button/tap/上下scroll语义通过 |
| flutter-48-red1 | 0功能＋1加载PASS，1FAIL | root独立发现exact48只剩handle、旧<48没有恢复按钮，真实body640/inset472命中负例 |
| green8 | **61功能＋7加载PASS，0FAIL/SKIP，exit0** | 改≤48后exact48及全部上述回归通过 |
| analyze1/2 | exit0，No issues | analyze2为最终冻结源 |

最终命令：

```text
flutter test test/agent_result_sheet_layout_test.dart test/agent_result_sheet_test.dart test/map_workspace_shell_test.dart test/map_workspace_auth_test.dart test/agent_workspace_controller_test.dart test/remote_agent_task_source_test.dart test/active_intent_test.dart --reporter json
flutter analyze lib/src/workspace/agent_result_sheet.dart lib/src/workspace/map_workspace.dart lib/src/workspace/agent_conversation.dart test/agent_result_sheet_test.dart test/agent_result_sheet_layout_test.dart test/map_workspace_shell_test.dart
```

16个新layout widget覆盖375.4×104、320宽font3、90/104/180各peek/medium/expanded、长error真实retry点击、0/100/260ms动画、<48隐藏树、搜索Row及中文操作语义；shell新增IME0/260/450/472/540/610/700/260/0，真实unfocus且draft保留，同MapCanvas Widget/Element、Task/Result对象及selection不变。另跑现有20次focus、鉴权切换/迟到与RemoteTask/ActiveIntent回归。此为Flutter widget证据，不是native GPU Pin性能或真实TalkBack。

六源 `source-freeze4.json` 前后byteSHA稳定。原agent_result_sheet_test.dart保留，本轮未修改；其余未提交内容及之前027档案未重置。worker没有Go/DDL修改或新Go运行。根代理全Flutter/build/device检查另行执行，本证据当前ROOT_PENDING。

## 原NOW001仍未覆盖

已复用当前context chip、private online/cross-city Social Intent入口、既有Opportunity详情/RSVP/Plans动作。原partial里的缺项必须按现代码核对，不能盲重建。

仍未完成：`remote_agent_task_source.dart` 的实际请求仍 `/v1/cities/{cityID}/agent/tasks`，`httpapi/agent_workspace.go` 仍GetCity及task.CityID边界；`agentcurrentcontext/native.go` 的当前选择/Task guard仍明确CITY，获准机器最小上下文033同样City绑定。`person_contexts`人类声明接口不是native机器purpose，不可拿HumanSelfReview或PublicBundle当授权。`postgres/opportunities.go` 仍只匹配 IN_PERSON，没有已获准线上/跨城市真实供给。

后续真正非城市接线需要原current Session/owner/Agent、精确当前context/source版本、purpose及实际供给授权，不能删City guard、造fake City、另造同意账本或用query自述绕过权限。本slice不代替此能力。**原任务仍PARTIAL，Closed Pilot/Consumer Beta NO。** 新APK设备、大字实际操作/TalkBack、场景未知旧overflow复测均由根代理补证；当前不声称设备已修复。
