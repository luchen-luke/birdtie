# BT-V4-NOW-001 审计与实施边界

状态：PARTIAL；2026-10-02。对应 live 队列 `automation/codex_task_queue.json`，不作为完整 Now/在线 Agent 能力完成证据。

现有 Now 的地图、Area Pulse、Agent 输入、结果卡和结果列表已共存；结果列表本身可显示没有地图点位的 Activity/Place，地图投影仅取公开坐标。拖图和键盘稳定性已有 Flutter 回归。侧栏选择城市可以切换本地情境，Person 会话及对话 ID 不归属城市。第一类社交 Intent 已能通过本人鉴权页面创建线上、线下、混合草稿；线上草稿不要求城市或地点，激活受众由服务端控制。

当前缺口：`RemoteAgentTaskSource._submit` 强制 `selectedCity` 并只调用 `/v1/cities/{cityId}/agent/tasks`，因此不能把线上或跨城请求当成已经可用的 Agent 查询。Now 首屏完全由地图占据，线上意图入口只在一次 Agent 结果的“保存草稿”动作中出现；若没有当前城市或地图，用户无法从 Now 直接进入非地图社交路径。`Context Graph` 已有 ONLINE/CITY 类型和无城市任务存储，但 HTTP Agent 查询尚未消费非城市 Context。

实施次序：先在 Now 增加明确的非地图入口和当前情境显示，复用本人鉴权的社交意图页面，不编造在线 Agent 回答或线上活动供给；同时保留地图 Widget 实例、输入与当前结果，不因切换情境清空。接着让结果与行动对无坐标实体保持可达，并以 Flutter Widget/集成回归检查地图实例、城市切换、登录/退出与在线入口。若要让在线 Agent 直接查询，需要先增量提供非城市 HTTP 路由、上下文授权和可核验的在线供给；缺少这些时本项只能 PARTIAL，不能写成完整跨城市 Agent 能力。

本轮实现：Now 顶部以中文标注当前城市；独立的线上/跨城入口覆盖在持续存在的地图 Widget 之上，默认“线上”，复用本人鉴权的社交意图记录与私人草稿页。用户返回后保留地图和现有 Agent 工作区状态。没有把在线草稿自动公开、自动邀请或当作 Agent 搜索任务。已有 Agent 结果列表可操作无公开坐标的 Place，地图投影继续只用公开 point 坐标。

验证：`flutter analyze` 无问题、`flutter test` 114 PASS（新增线上入口/返回保持同一个地图 Element、现有键盘 20 次焦点回归与无坐标 Place 动作）；隔离 Gradle home `flutter build apk --debug` PASS，Android 16 `c641566b` 安装预览包后在隔离本地 API/DB 下查看地图与线上页，截图见 [地图](../testing/evidence/now-context-2026-10-02/01-map.png)、[线上页](../testing/evidence/now-context-2026-10-02/02-social.png)。这些均是开发合成证据，未验收真实在线供给或跨城匹配。

恢复 DONE 条件：增量实现并测通非城市 HTTP Agent Context 路由、服务端 Context/受众授权、无伪造城市的在线/跨城活动或机会供给，以及 Now 中从在线意图到实际结果/行动/Plans 的路径；对首屏、地图状态、错误/空态和真机完整回归。当前城市 Agent 查询仍限制于 `/v1/cities/{cityId}/agent/tasks`，所以此任务为 PARTIAL。
