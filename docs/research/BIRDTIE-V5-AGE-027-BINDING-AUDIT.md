# AGE027 地点记录连接绑定接续审计

2026-10-04；原 PARTIAL 的 CODE_LOCAL 人工入口修复，非到访/出席或认知能力。

## 前置核验与范围

原任务依赖 AGE005/064、V4MOM001/PLC001、INT001 全 DONE，由根显式 resume；完整旧对象保留 `work/v5-age038-resume/original-age027-binding-partial.json`。准确11范围见 `age027-binding-lease.json`，原038的 PlaceDetail 与对应 test 已实际移交。本线不改 Go/HTTP API/SQL/078/Settings、live 队列或共用总报告。

AIR010/011 当前 Service 仍调用无 provider 的 Gateway；027 RunReference/058 Task binding 并非真实 AgentRun/Step。016原依赖015仍 PARTIAL，不能借027影子 Run 绕过原前置。其它已有人审界面和一次性 outbox 维护不再重复实现。此次独立缺口来自本人 Place 现有消费者入口。

## 审计与实际 RED

1. PlaceDetail 的 `_current` 仅检查 token/workspace/place/epoch，`didUpdateWidget` 未处理 API/client/getter/listener 配置替换。旧 `/v1/me` 完成后，以旧 owner 进入新 `_base` 的私密页。
2. PlaceDetail 自有 client 初次创建后为 final，dispose 却按当前 widget.client 决定 ownership；borrowed→null 错关借用 client，owned→borrowed漏关自有 client。
3. 私密页更新 credential getter/listener 时只比较读出的身份值；同值更换来源仍显示旧具体批准。`_confirm` 仅保存 generation 数值，后续动作取动态 `_data`；“停止核实”尤其没有原操作目标参数。

修复前 `red1` 三项实际 FAIL：旧 Me 响应后向 `http://new-fixture/.../declarations` 与 `/memory` 发出两个 GET；原 borrowed client close1；同 key 更换 getter 后确认按钮仍存在。全部仅 MockClient，不对真实网络发送凭据。

## 短计划与实现

捕获创建时 transport、ownership、base、身份与 workspace getter/listener/pendingStore；配置替换永久退役该 State。旧 Me/详情迟到结果拒绝，已开 nested 私密页面和弹窗通过原 boundary 一并清除。A→B→A 不恢复旧批准；重新从入口创建 State 才读取当前配置。

PrivatePlaceMemoryPage 复用原 controller、原人审动作，确认明确绑定具体 Controller/generation；保存/撤回/停止核实在 await 前捕获 Controller，停止核实另核对原 pending 对象。不自动删除、迁移或重发原安全存储。普通同一 getter 的 token/workspace 通知继续原 generation/serial、当前来源与 Agent/CAS 复核。

Controller dispose 幂等并清空本机私密正文/来源控制/草稿/结果，closed 后不读身份、不再 load 改状态，不通知 dead page。没有删除持久 pending；收到迟到回执也不假显示成功。自有 IO client 关闭一次，借用 client 由 owner 负责关闭。

额外真实嵌套测试发现父连接在 build 内失效时，同步通知 private page setState 导致 Flutter 异常；本页状态立即失效，UI setState/弹窗 removeRoute 在必要时延期到 frame 末并检查生命周期。dispose 先退役，旧异步确认不能在拆除中操作新来源。

## 命令及保留失败链

工作目录 `work/v5-age027-binding`。

| 帧 | 结果 | 原因/恢复 |
| --- | --- | --- |
| red1 | 0 PASS/3 FAIL，exit1 | 产品修复前的三个真实缺陷 |
| green1 | 3 PASS，exit0 | 初始修复正回归 |
| target1 | 49 PASS/1 FAIL，exit1 | 原测试每次 pump 新 getter 实例，被正确判连接替换；改同一个 currentAuthorization closure，仍改变 token且保留所有私密清空/新空列表断言 |
| green2 | 29 PASS/1 FAIL，exit1 | 新嵌套批准用例揭露 build 阶段同步 setState；修生命周期，未弱化无异常断言 |
| green3 | 18 功能可见通过/2加载 FAIL，exit1 | 并行 Settings 暂引用尚未落地的043 Page；原日志保留，不能当联合通过 |
| green4 | 55 PASS/2 FAIL，exit1 | closed Controller 再load写入新登录错误；增加 closed load guard，保持私密状态清空断言 |
| green5 | 74 PASS，exit0 | 六个目标测试文件通过 |
| analyze1 | 8 info，exit1 | scoped代码/test花括号；修样式 |
| analyze2 | 7 items，无问题，exit0 | 最终源分析 |
| target2 | 74功能+6加载 PASS，0 FAIL/SKIP，exit0 | machine精确计数，owned7与当次全client247 SHA均前后相同 |

可复现（client目录）：

```text
flutter test test/private_place_memory_api_test.dart test/private_place_memory_controller_test.dart test/private_place_memory_page_test.dart test/private_place_memory_binding_test.dart test/place_detail_sheet_test.dart test/place_history_controller_test.dart --machine
flutter analyze lib/src/workspace/place_detail_sheet.dart lib/src/workspace/private_place_memory_page.dart lib/src/workspace/private_place_memory_controller.dart test/place_detail_sheet_test.dart test/private_place_memory_page_test.dart test/private_place_memory_controller_test.dart test/private_place_memory_binding_test.dart
```

`verify-client.py` 记录命令、raw machine日志、计数与前后SHA；`freeze1.json`列实际3产品+4测试文件。本次247源稳定只是定向短帧观察，不是全Flutter测试/build通过证明。根在027/043共同冻结后执行全客户端、构建与手机验收。此前全Go9423/077为根历史证据，当前本线无Go改动、不称新Go实测。

## 验收覆盖与边界

- 正常同 captured Me 获 native本人ID→具体声明检查→原 Agent ID/CAS PUT；原撤回/当前版本/权限/失效/返回修改、原Place ID、公开资料/活动、组织往返、未知结果原key回放仍通过。
- 旧Me迟到不能创建新API私密页；嵌套批准后base A→B→A清批准且无后续GET/PUT；borrowed→null close0与owned→borrowed真正IO factoryclose1。
- 私密页base/client/listener/pendingStore/ownerGetter/workspaceGetter替换永久退役，旧listener移除，无新源请求或持久记录迁移。
- 晚GET/PUT/DELETE/unknownPUT/unknownDELETE不恢复结果，不自动重试，原pending key/value保持；closed Controller不通知、清空内容，持久恢复说明仍可找回。
- 旧“停止本机核实”弹窗在改源后撤掉，A/B两原key都保留；显式新State重开原API读取原待核实操作，0自动写。Controller身份与原pending对象双绑定是 await 后保护，不作为服务端许可。
- 320px/font2退役页内容可滚动、语义返回>=48dp；原320大字/键盘160与声明语义场景通过。真实 TalkBack、前后台、原安全Vault App重启、真机新绑定切换未由worker运行。

适用 UX-CHECK-05/07/08/09/10/12/13/14/16；本轮不是对话补全，不新建提問轮次或UI backlog。未增加埋点/私人日志。Full027仍 PARTIAL：真正 verifiedVisit/attendance 与 cognition purpose/model UNAVAILABLE；本人VISITED、收藏、Moment、RSVP均不替代真实证明。Closed Pilot/Consumer Beta 保持 NO。
