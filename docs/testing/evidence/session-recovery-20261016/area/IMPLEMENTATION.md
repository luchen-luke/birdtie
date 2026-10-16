# Now 地图与区域搜索的会话恢复

基线：`19ba964bfa1f9e7b2e57a3f79842ea291f71ff47`。2026-10-16 本轮增量，仅本目录与根代理登记的 6 个源码/测试文件；保留其他工作中的认证、API 与任务队列修改。

## 实际修复

- `apps/client/lib/src/workspace/map_workspace.dart`：401 交给现有认证控制器 `recoverSession(expectedAuthorization:)` 单飞恢复。网络失败不清除持久凭据、不重复退休、不自动打开登录。登录/重试按钮复用现有登录与恢复入口。前台恢复只在已退出且上次恢复网络失败时触发。当前授权对象、媒体与旧批准仍按原身份监听退休。
- `apps/client/lib/src/workspace/map_canvas.dart`：保持原 `final MapCanvas` 与原生地图子树，通过独立通知更新“恢复中／需登录／网络失败”提示；无 owned query 的初始地图也能说明尚未读取的动态，提供登录或重试按钮。
- `apps/client/lib/src/workspace/agent_workspace_controller.dart`：`AgentAreaContentRead` 明确表达 authenticated owned source 是否实际可读、获准位置数、部分返回及恢复动作。旧 typed public 空结果不代表 owned 内容为空。读取全部成功且确实无结果才能 empty；源不可用为 partial。
- `apps/client/lib/src/workspace/map_workspace.dart` 的 `_searchThisArea`：同时等待 typed 和 owned 读取；按已选图层判断是否请求 owned，不把 owned DTO 写成 POI。使用既有 `OwnedMapController.queryBounds` 预取取整规则核对范围，再按用户原始视口计数。结果绑定身份、来源 epoch、当前 grant、视口与 zoom，以及 typed/private/owned 中最早的租期；过期与退休不保留旧计数。恢复结束但 guest 身份未变化时，只更新已经请求过的区域结果一次。
- `apps/client/lib/src/workspace/agent_conversation.dart`：源不可用时提供相应登录/重试操作；正常 typed 结果与其他区域动作保持原有路径。
- `apps/client/test/now_area_content_recovery_test.dart`：新增 3 项隔离完整宿主 widget 测试；`now_content_geometry_test.dart` 仅更新默认未授权 owned 场景的过时 empty 断言和将结果文案查找限定在对话组件内。

未新增媒体、账户或内容写入；不绕过 ACL、不延长授权租期、不改底图引擎、缓存或服务器协议。会话持久化与 API 176 由其他登记写域负责，不能以本目录测试替代其验证。

## 本轮直接验证

最终回执：`targeted04.json` / `targeted04.log`，退出码 **0**，**5 PASS / 0 FAIL**，记录文件 sourceStable **true**。

命令与目录完整记录于回执；可使用 `run-targeted.ps1 -Label <新标签> -Compatibility` 复现，禁止覆盖既有失败或通过回执。实际选择了以下 5 项，未运行整个测试文件矩阵：

1. Guest 区域为 partial，并有显式登录入口，typed 空不伪装全无动态；同一 null 身份的冷恢复失败能更新提示，无自动登录弹窗。
2. typed 已返回空但 owned 在途时保持 loading；owned 成功后显示 1 处动态位置；取较短 30 秒租期，来源退休后旧结果立即无效。
3. 实际 owned HTTP fixture 401 触发恢复；恢复网络失败后点击重试，新 token 重新读取 pin；旧 pin grant 永久失效，地图 widget/element、视口保留，无自动登录弹窗。
4. 既有区域命令显式重试继续使用已授权 reader，不创建 Agent turn，typed-only 权威空结果语义保留。
5. 既有正常字号 1.0 区域搜索几何用例：保持地图实例、原视口、零 Agent 提交、一次 typed 地图读取。未运行取消范围中的放大字号用例。

`git -c core.whitespace=cr-at-eol diff --check -- <已登记源码/旧测试路径>` 退出 0；旧测试保留仓库换行方式，仅有 Git 换行提示。

## 失败记录

- `targeted01`：1 PASS / 2 FAIL。新提示原来被“owned query 未初始化”条件隐藏；新区域接线错误地用原始 bounds 对比已有取整预取 query。随后修复实际接线，未放宽权限或删除测试。
- `targeted02`：3 PASS，首次新增宿主测试通过；随后补了同一 guest 身份恢复状态收尾，以及与旧正常字号测试的兼容验证。
- `targeted03`：4 PASS / 1 FAIL。旧布局测试全局查找状态文案，新增地图提示和原对话摘要各一份，导致 findsOneWidget 失败。改为限定原 AgentConversation 的结果文案；未删除任一产品恢复入口。
- `targeted04`：最终上述 5 项全通过。前序证据保留。

## 复用与验收边界

对应 UX-CHECK-04/06/07/08/09/10/12/13：复用原登录、恢复与区域领域读取；区分未读/失败/空/过期；保留原权限退休与地图状态；不产生发布、登录提交或内容创建等隐式副作用。正常字号使用既有 TextButton、主题与对话 tokens。

本 worker 的真机登录、重启/长停留、真实弱网、服务器刷新轮换、性能、APK 编译安装、视觉截图与读屏操作均 **NOT_RUN**。本轮 5 项是隔离 HTTP/认证 fixture 的宿主测试，不是真实手机或线上端到端验收。根代理可另以新 APK/API 证据覆盖这些状态，不能沿用本目录测试称真实验收通过。
