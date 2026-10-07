# BT-V4-MAP-002 地图运行压力审计

日期：2026-10-04。任务原验收：键盘压力、选中点持续、请求顺序与增量标记保证继续成立；验证为真机压力及回归测试。依赖 `BT-V4-MAP-001` 已 DONE，未导入新任务。唯一实施 worker 为 `sponsored_trust`，产品修复由持有共享源的 root 实施。

## 合同与实施范围

- 依据 `MAP-RUNTIME-PERFORMANCE-CONTRACT.md`、`TYPED-MAP-LAYERS-V4.md`、`GLOBAL-UX-INTERACTION-CONTRACT.md`，适用 UX-CHECK-01/04/06/08/09/10/11/12/13/14/16。
- 本线只新增 `apps/client/test/v4_map_runtime_stress_test.dart` 及此审计、专属证据/work。不改领域授权、DDL、坐标来源、账户队列或发布条件。
- 复用真正 MapWorkspace/MapCanvas/PublicCityMapView、MapLayersController、AgentWorkspaceController、typed ResultSet、EntityPeekCard、聚合与 bitmap cache。Widget harness HTTP 为明确合成闭合 DTO，地图配置为 null；不会访问真实地图 SDK 或外部网络。
- 真机操作者为 root，本线只读其屏幕/日志证据。开发 APK、合成资料与 widget 结果不是真实伙伴供给、六个真人任务或发布验收。

## 审计发现与短计划

现有 Scaffold `resizeToAvoidBottomInset: false`，IME 只扣 overlay 可用高度，地图子树持久。native marker 同步以稳定 ID 比较 fingerprint、只删除过时 ID、只更新变化项，bitmap 缓存复用。相机运动回调不自行查询，显式范围更新采用 epoch 丢弃旧返回。

实施顺序：真实组件压力测试 → 保存首次失败 → 区分 fixture/API 适配与真实产品缺陷 → 共享源由 root 最小修复 → 完整目标回归与 source hash → root 整客户端/build/真机复核。

### 实际产品 RED 与修复

`target3.log` 为 8 PASS/1 FAIL：CITY typed ResultSet 原实体 `activity:runtime-activity` 被选中后，用原 `newTask(preserveMapSelection: true)` 切 ONLINE，MapCanvas 原 `_entities` 只接受非 ONLINE/current task city；线上 Task 没有 city，缓存的 CITY typed 内容被空图层替代，实际 `entities.single` 抛 `No element`。MapWorkspace 轻卡采用相同错误条件。

root 在既有来源链保存 CITY Task 的明确 cityID，MapCanvas 仅同 City 复用 CITY 背景，原 controller 为 MapWorkspace 提供 `mapBackgroundCityID`；不根据当前城市猜原数据归属，不给无坐标 ONLINE 内容造 Pin。root 首次尝试不存在 `AgentResultSet.cityID` 的编译失败由其日志保留。`target4` 新 10 测试全绿；进一步断言原 entityID、Peek ID、contextKey 不变，切不同 City 后 0 Pin/0 Peek，原 ONLINE Task 保持。

### 推断更正与测试适配

- 曾仅从 `MapLayersController(required this._api)` 猜外部 `api:` 不合法；root 提供同 SHA 的实际整包绿，本线实际编译也证明新 Dart initializing formal 可接受。该推断撤回，没有修改 controller。
- `target1` 调错 controller 方法 `searchArea`，真实方法为 `searchThisArea`，属于新测试编译失败。
- `target2` 3 PASS/6 FAIL 保留：PRIVATE route 城市段取错、未先打开图层 modal 就查找读取按钮、未 pump 请求微任务，以及错把显式新查询成功后的 selection 退休当不应发生，均修正为真实现契约；不放宽产品断言。唯一 typed ONLINE 丢 Pin 的真实缺陷仍保留至 `target3`。
- `analyze1` 工作目录错误导致目标文件未找到；`analyze2/3` 正确目录退出 0。
- `target5` 回归 75 PASS，但 before hash 写入相对路径失败，后续 hash 仅 during/post；不声称有完整前后稳定性。`target6` 以绝对路径 runner 正确记录全部 292 lib/test Dart 前后完全一致。

## 本地可复现验证

从仓库运行：

```powershell
& 'C:/Users/chens/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/python.exe' 'D:/Project/birdtie/work/v4-map002-runtime/verify-client.py'
```

runner 使用 `apps/client` 为 cwd，真实调用 Flutter test 10 文件及 analyze 新文件，输出 `target6.log`、`analyze3.log`、before/after/result JSON。执行重跑会覆盖该轮路径；归档为不可变历史，后续应复制 runner 并修改轮次文件名。

| 场景 | 目标测试实际结果 |
|---|---|
| font1 与 320px/font3 | 共 60 IME 开合、180 输入变化；六层 Pin、选中卡、任务、同 MapCanvas/PublicCityMapView Element；无额外 map GET |
| 拖动/范围读取 | 60 camera motion/settled 回调保持 ID、卡与 selection；实际图层按钮明确读取才新增 1 GET |
| 迟到 viewport | 10 请求最新先回，9 旧回包反序到达不能替换新 Pin/卡 |
| PRIVATE 身份 ABA | 同 token 账号 A→B→A，清旧 cache 与 pending，OPPORTUNITY 默认关闭，不自动重读 |
| transport/City ABA | 同 key endpoint A→B→A 与 City A→B→A 丢弃旧来源；借用 client 关闭次数 0 |
| typed CITY→ONLINE | 原实体与 contextKey/轻卡保留，线上无点不造点，背景不能跨 City |
| 原 Agent 查询顺序 | 20 反序请求保持最新 typed ResultSet；成功新范围按原契约退休 selection，旧回包不能复活 |
| 增量关联基础 | 六类 100 轮 bitmap cache、选中 ID 排除聚合、未选聚合 ID 对反序输入稳定 |

`target6` 实际 75 PASS / 0 FAIL（含本线 10 stress）；`analyze3` 退出 0。source 292 个 Dart 前后 hash 一致。这里没有执行全客户端 build、Go 全仓或实际 native annotation 调用计数，它们由 root 独立负责。

`worker-final1` 不可变归档 37 文件，manifest SHA `d1edbeac8d439bb2d480863cf9c9a7678cd7d62409c1a0d21a4b3baaf4a5f1a0`。归档后独立比较发现 root 的 `map_workspace.dart` 在 target6 结束后继续 WIP，归档产品副本是复制时的新字节，并非 target6 被测字节；准确差异保存在 `final1-frame-delta.json`，不改历史 archive。本线唯一 stress 测试与其余 8 个观察产品源仍同被测 SHA。root 必须在最终共同源码帧重新执行整客户端验证。

## 真机证据边界与待合并结果

root 已实际安装 `59d333816d67f2931c6838e2095bf857ef8f40bada0323c33704c671e5527e42` Debug APK，设备 `c641566b`；只读依据 `work/v5-age038-resume/phone-context-online5/installation-result.json`。`first-installed-screen.png` 实际显示 Aberdeen Mapbox 底图和 cluster 6。这是本地现版本可见性样本，不证明 20 次压力无闪烁。

root 明确初次 Pin 消失发生在原 120 秒 source 自然到期，图层 modal 显示来源已到期；不能归因键盘。刷新后实际 Activity 详情已打开，20 次 IME 与 drag 证据由 root 继续采集。此处将在其结果可检查时增量记录，不用历史 2026-10-01 二十次真机报告替代本轮。

native Mapbox 的私有 annotation diff 仍依据代码审计；widget cache/投影测试不是 SDK 的 create/update/delete 次数仪器测试。暂无 frame timing、屏幕视频或 TalkBack 证据，不宣称所有瞬时帧无闪或 AT 已通过。

Closed Pilot Ready：NO。Consumer Beta：NO。任务状态由 root 按原 AC 与实际当前真机证据判定。

## 增量 2：本轮真机发现透明空白 overlay 吞地图点击

root 的真实 phone6 点击无法选 cluster，进一步定位到 `now-native-context-scroll` 的固定高度 ScrollView：实际内容仅 156dp，旧透明 viewport 却高约 367.754dp，额外约 211.754dp 空白区域仍截获 pointer。该问题有实际 native 点击阻碍及专门 widget RED，不能用前述合成 projection 压力绿覆盖。

只读核实 `work/v4-now004-results/overlay-native-hit7/result.json`、`red.log`、`green.log`：root 以旧 MapWorkspace SHA `09c0b145bcb5e956c1caf55fa9ec46bc79ca4899d7d012e79c64b18b8907782f` 执行新增 shell 断言退出 1（367.754 对 156）；改成 ConstrainedBox 最大高度约束、实际内容 shrink 后 SHA `85e74956a6f5766f60a29e0b04a323b4ab82a90cc5f06cd0ad673dbbc3bf5d4d`，同断言退出 0。两个共享源/测试均由 root 在其范围实施，本线未写产品文件。

root 新当前整客户端 `current-map-overlay-fix7/result.json` 可检查结果为：294 源前后稳定，1098 functional PASS、139 loading，0 FAIL/SKIP，analyze/test/Debug build 均 0。新 APK `9e7eb95b6864a99590783f35bc769af498df2302f686a88d2c38bcc23124cd6a` 安装到 `c641566b` 成功，保留 app data，没有执行 migration。原安全 API 未升级新 NOW005/006 后端，安装回执对此明确 `NOT_YET_UPDATED`，本附注只讨论地图路径。

已实际查看并核对当前手机 PNG/节点：

- `phone-map-overlay-fix7/screens/map7-cluster-after-first-tap-nodes.json`：实际点击 cluster 后出现原 6 个 native 成员供用户选择，不是用 fixture 路由替代点击。
- `map7-selected-light-card.png` 与节点：实际选择后 Now 显示“周末羽毛球练习（本地测试）”轻卡与“查看”，底图/选中点继续可见，没有直接强制打开全详情。
- `map7-first-keyboard.png`：真实系统键盘打开，地图和同标题轻卡仍可见，composer 位于键盘上方。这是单次当前帧样本，不是完整 20 轮验收。

root 的 stress1 在 120 秒来源自然到期后失去来源；stress2 在关闭图层后系统恢复 IME 导致脚本前置断言失败。两次真实失败须保留，不泛报键盘导致 map source 消失，不把原断言失败改成通过。本次 stress3 20 轮/拖动结果仍等待 root 完整日志，MAP002 保持 IN_PROGRESS。未执行 native SDK CRUD 次数仪器、视频、frame timing 或 TalkBack，不声称瞬时全部帧无闪；Closed Pilot/Beta 仍 NO。

## 增量 3：stress7 当前真机压力及整仓结果

上述 stress3 等待状态为当时记录。现已实际读取 `work/v5-age038-resume/phone-map-overlay-fix7/stress7/result.json`、`progress.json`、最终 `phone-map-stress7g.py`，并查看 closed-20、before-drag、after-drag PNG：

- 20 轮每轮均使用 dumpsys/input_method 检查系统 `mInputShown=true/false`，仅输入已知合成 `a`，逐轮删除，没有提交查询/消息/邀请。脚本先确认 composer 空，不删除既有真人正文。
- 第 1/10/20 轮实际 PNG/XML 分别采集键盘打开及关闭；这些采样均保留“周末羽毛球练习（本地测试）”原轻卡。其他 17 轮只有 IME/删除系统状态检查，**没有逐轮截卡证据**。原 progress 的 `sameLightCardTitle=false` 在未采样轮表示该脚本未观测，不篡改成 true，也不误报为真实轻卡消失。
- 71.109 秒完成，处于真实 120s source lease 内。实际 swipe 后 geography 从原 Aberdeen 地图移至可见 Bridge of Don/Old Aberdeen，原选中 Pin 与同轻卡继续可见；地理图确实移动，不是仅重复静态截图。
- stress1–6 均原样保留为早期失败/诊断：source自然到期、modal关闭恢复IME、重复选已排除cluster成员、初始modal无composer、IME关闭采样太早及modal误waitIME。它们不是 PASS。最终 stress7 根据真实语义节点及系统状态完成，不以固定时间强行声称关闭完成。

原当前 APK 为 `9e7eb95b6864a99590783f35bc769af498df2302f686a88d2c38bcc23124cd6a`，安装及294客户端源核对见增量2；本线独立 `root-proof2-receipt.json` 检查所有294当前源码与 root 完整 Flutter7 帧逐字节匹配。最终补充的 phone/script/command 证据将收入新 worker-final2，worker-final1 不覆盖。

另实际只读 `work/v5-air016-runs/full083-parallel38/result.json`：独立 fresh083 整仓 Go `go test ./... -count=1 -timeout=20m -json` 实际 10027 PASS、0 FAIL/SKIP/package fail，vet/build及2 CLI build均0；840源＋3seed执行帧、原public/catalog/participation xmin、unused down/reapply及owned DROP均保留。其源码帧为复制执行帧，不是当前手机API；手机保持原 owned081 binary，不能把083后端全测当已在手机运行。raw runner 的 `finalAcceptance=PENDING_FROZEN_FULL_REGRESSION` 是原初始化字段，实际最终命令/functionalPass/drop字段已终止成功；最终任务状态仍由 root 核定，不改该历史 JSON。

本项现已有 CODE_AND_LOCAL_VERIFICATION 的实际代码、回归及 native 手机压力证据。Native SDK CRUD次数、所有瞬时帧无闪、视频/frame timing、TalkBack、真实伙伴/真人六任务、正式部署与生产身份仍不属于此证据。Closed Pilot Ready：NO；Consumer Beta：NO。worker不修改live队列。

final2 归档前独立解析 Go tests.jsonl：实际10027 test PASS、77 package PASS、0 test FAIL/SKIP，另有19个无测试文件 package 的 skip 事件。首次归档脚本混淆 package 与 test skip，断言失败（attempt1保留且当时未创建archive）；改准确分类后继续，不改产品测试、原结果或门槛。

## 增量 4：归档 manifest SHA 记录错误及独立更正

root 的 `accept-map002-root38b.py` 在固定 manifest SHA 首断言真实失败。原因已逐字节复核：归档程序用 `Path.write_text` 保存 JSON，Windows 将 LF 转为 CRLF；原 receipt 却对保存前内存 LF 字符串计算 SHA。这是原始记录错误，不是测试失败、未 flush 或已证实的后续档案变更。上文旧 manifest SHA 与原 receipt 保留作为错误历史，不能再用于落盘文件核验。

| 不可变档案 | 实际落盘 manifest SHA256 | 文件独立核验 |
|---|---|---|
| worker-final1 | `ab100cc248ee8efdfafab041d2d7b4c6a4a571a3fa4b8aa29e5ec384d86ab25d` | 37/37 SHA 与字节数匹配 |
| worker-final2 | `22806836e010d5f36d6cfaf9ecc5c24297a02d077d82dd0f1bc134c57da2704d` | 132/132 SHA 与字节数匹配 |

final2 原错误 SHA 为 `0edf7480753c5dd56797e6bc758c632070620e9841fffeb4fb501937670830d9`；内存24464字节，经661处 LF→CRLF 转换后，实际25125字节恰好逐字节相等。final1 原错误 SHA 为 `d1edbeac8d439bb2d480863cf9c9a7678cd7d62409c1a0d21a4b3baaf4a5f1a0`；6263→6449字节、186处转换，同样精确复现。

新独立 `work/v4-map002-runtime/verify-manifest-correction.py` 执行退出0；`manifest-byte-correction1.json` 保存每一个169文件的实际核验，落盘 SHA 为 `4598b8efff3ab35874e34c8fc20bf71fc543e91a0dcc14921ae3d1045599deaf`。原两档案、manifest 与 receipt 核验前后保持不变。更正采用 `write_bytes`，再从真实保存字节计算 SHA；没有重跑或改写测试、产品、手机及旧压力结果。MAP002 状态仍由 root 独立核证后决定。
