# APK41 地图登录入口无响应后的窄修

2026-10-16。延续同一已登记任务，不修改认证、凭据或服务器文件。

## 实际问题和证据边界

根代理在 APK41 真机上记录：地图显示“登录后可查看地图动态。”，右侧“登录”按钮两次物理点击未出现登录弹层；同机侧栏及原账户入口可点击。截图原件由根代理保存为 `outputs/BirdTie-APK41-login-required.png`。根代理只读日志摘要为 fatal / unhandled / Flutter EXCEPTION / afterDispose 均 0。本 worker 未操作设备，也未存储原始日志。

补充的实际 widget 点击测试在修改产品源码前已通过（`login-tap01`），能从地图进入原 `BirdtieLoginSheet`、再进入 `DevPhoneLoginSheet`。因此不能把模拟测试当作复现了真实手机缺陷，也不能宣称已确定原生 hit-test 是根因。

## 窄修

- `apps/client/lib/src/workspace/map_workspace.dart`：将登录恢复提示放到现有宿主 Stack 交互层，与顶部控件同级，在 `MapCanvas` 外；直接调用原 `_recoverMapContent` / `_signInHere`，保留原组织、身份和单飞约束。宿主已有 auth AnimatedBuilder 负责状态更新，地图对象不重建。
- `apps/client/lib/src/workspace/map_canvas.dart`：移除仅用于登录提示的嵌套覆盖与通知接线，原地图内容错误、分页、权限与媒体机制保留。
- 同时核对到确定性守卫不一致：原地图登录按钮可见可点，但原登录弹层在 `auth.busy` 时会直接返回。现在登录处理中显示短提示并隐藏该操作，和原入口守卫一致。未证实 APK41 当时是否处于 busy，因此这也不能代称现场根因。
- `apps/client/test/now_area_content_recovery_test.dart`：新增真实点击链路；使用 1220×2656、DPR 3.25、普通字号，断言控件在宿主覆盖层且不在 MapCanvas 内，点击登录方法、进入固定码页、不提交就返回、再次打开；忙碌时不显示可执行按钮。固定码请求和登录提交均未发出。

## 本轮验证

- `login-tap01`：改动前 4 PASS，说明隔离环境未重现设备问题。
- `login-tap02`：覆盖层调整后 4 PASS。
- `login-tap03`：最终 4 PASS / 0 FAIL，退出 0；本轮源文件/认证依赖 sourceStable=true。包括新增点击链路与原 3 项区域/会话恢复直接回归。完整命令及哈希在同名 JSON，未覆盖此前 APK41 的 `targeted04` 或 `freeze.json`。
- `freeze42.json` 保存此后 3 个变动文件哈希；原 agent_conversation、agent_workspace_controller 与旧 geometry 测试不再改动。

符合 UX-CHECK-04/07/12/13：复用原登录动作与状态、保留返回和地图实例，明确处理中不可执行。正常字号，不录屏。

**APK42 真机再次点击、原生地图 hit-test 根因确认、真实登录与 31 分钟闲置刷新：NOT_RUN（本 worker）**。根代理将在合包后继续验证。此修复是具体可审阅的交互层调整；不能仅凭 widget 通过关闭 APK41 真机缺陷。
