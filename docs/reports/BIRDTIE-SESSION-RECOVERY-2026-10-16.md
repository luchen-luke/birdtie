# Birdtie 打开后地图无动态：会话接续修复

本轮从 `19ba964bfa1f9e7b2e57a3f79842ea291f71ff47` / APK40 / API31 / schema175 增量修复。当前 API32/schema176 已部署，APK42已替换安装。APK41真实原账号登录后自动出现7个内容位置；42修订地图登录提示交互层。服务器已确认超过30分钟自然idle过期，但最后冷启动时ADB断开：自动续接及42登录按钮真机验证为NOT_RUN，设备依赖BLOCKED，不能标记整体完成。保留其他工作、旧失败及原任务队列；Civu只读，网页延期，正常字号、不录屏。

## 这次空图的实际原因

用户20:46截图后，手机 APK40 侧栏实际为“未登录”。只读核对原账号最后一次会话：18:23:28创建，19:48:33 idle到期，而绝对期限仍到翌日02:23:28，账号active且会话未被人为撤销。原实现仅有8小时access和30分钟idle，没有独立刷新凭据；冷启动 `/v1/me` 拒绝后删除本机access并静默进入guest。短暂断网也会结束一次性恢复，缺少可见重试入口。

地图动态读取需要当前授权，而旧“搜索此区域”只等待typed图层。typed空结果被呈现成整个区域空，不能说明动态是否读取过。两条路径叠加，造成仍有内容却显示空白和“未找到符合条件的结果”。

服务器核对仍为102条动态，其中83条为Civu稳定映射；39条历史消息仍在。本次没有删除或重导入业务内容。证据：`docs/testing/evidence/session-recovery-20261016/root-cause-before.json`、`root-cause-device.json`。

**之前32秒后台验收只覆盖地图约30秒的读取租期，未覆盖会话30分钟idle。** 旧记录保留，但不能用它证明长期登录有效。

## 实施与Civu对应

只读参考 Civu-client `lib/src/auth/session_store.dart`、`auth_controller.dart` 与 Civu-server `api/internal/auth/token.go` 的access/refresh分离。Civu为约30分钟access和30天refresh；本次只借鉴接续语义，不复制其普通偏好存储、非原子轮换或任意写请求自动重发。Birdtie沿用自身账户、Session、权限及安全存储。

| 实际路径（相对 `D:/Project/birdtie`） | 变化 |
| --- | --- |
| `apps/api/internal/sessionrefresh/model.go` | 32随机字节设备凭据、规范operation UUID、同操作响应加密封装 |
| `apps/api/internal/postgres/session_refresh.go`、`dev_phone.go`、`identity.go` | 明确固定码登录签发30天绝对期限family；当前身份/账号校验；原子轮换与旧access/批准退休；退出撤销整组 |
| `apps/api/internal/httpapi/session_refresh.go`、`dev_phone.go`、`identity.go`、`server.go` | 独立刷新与撤销合同；严格小JSON，不在响应或诊断中暴露长期明文数据库凭据 |
| `apps/api/migrations/176_persistent_device_sessions.sql` 及down | 新增family/token/access三表及约束，不回填旧会话，不改变旧动态 |
| `apps/api/native_database_startup.go` 及测试 | 缺少176运行结构时拒绝启动 |
| `apps/client/lib/src/auth/session_vault.dart` | origin绑定secure vault，兼容旧v2，v3完整pair和待重试operation原子存储 |
| `apps/client/lib/src/auth/birdtie_auth_controller.dart` | 401单飞接续；网络失败保留pair；先持久化operation再刷新；退出及迟到响应隔离；dispose不误撤继任控制器凭据 |
| `apps/client/lib/src/app/birdtie_app.dart` | 原认证客户端401回调与恢复失败后的前台重试接线；不自动重发业务写入 |
| `apps/client/lib/src/workspace/{map_workspace,map_canvas,agent_workspace_controller,agent_conversation}.dart` | 未读/恢复/需登录/网络失败可区分；区域结果等待动态源，来源失败为partial；沿用原地图和权限退休 |

首次从旧v2升级仍需一次明确登录获得设备凭据，不能拿已过期access自行升级。固定验证码入口继续使用；本轮不会偷偷调用验证码或创建身份。每次刷新得到普通新Session，旧批准不复活，位置和地图关联不会扩大可见性。

同一个已消费凭据、同operation在60秒内重试返回相同pair，用AES-GCM加密回执支持响应丢失。不同operation重放或超出窗口会拒绝并撤销family。family30天绝对期限不滑动；access最长8小时、idle最长30分钟，均不超过family截止日。本轮持久接续仅接入 `dev_phone`；OIDC/dev_email保留原行为。

## 已执行的直接测试

所有测试均使用本地隔离fixture，未向实际环境注入合成数据。重叠回归不累计为新增测试。

| 最终回执 | 实际结果 |
| --- | --- |
| `session-continuity-20261016/backend/receipt.json` | PostgreSQL隔离生命周期2个顶层PASS、0 SKIP；HTTP2 PASS；启动检查1 PASS；协议4 PASS |
| `session-recovery-20261016/auth/auth04.json` | 11项接续测试PASS，含丢响应同operation、并发401、显式退出、跨实例dispose和新账号隔离 |
| `session-recovery-20261016/auth/auth05.json` | 原认证相关45项PASS，sourceStable=true |
| `session-recovery-20261016/area/login-tap03.json`、`compatibility42.json` | 最终42地图/区域相关6项PASS，sourceStable=true；正常字号。旧targeted05保留为41证据 |

修复前证据保留：后端native01的fixture双时钟问题；auth01旧退出语义回归；auth04-before真实复现两项跨实例失败（该runner的failed计数字段未含hidden error，必须同时看errors及退出码）；area targeted01/03记录提示隐藏、取整范围及旧断言问题。测试通过不替代真机。

## 部署与数据边界

API32实际运行路径 `/opt/birdtie/releases/social176-20261016-build32`，SHA256 `7543929ca5569e2501ef04bb00ffe7fdf44c9e57ce5a94478d612d3370e2d9d7`。目标为原Birdtie独立库 BIRDTIE_NATIVE_HZ / OID54087，迁移max=count176。切换前验证原进程路径/SHA、原175个迁移摘要、库owner/marker及服务配置；切换后实际MainPID可执行文件与ready均确认。

176只新增3张空表，没有自动登记旧会话。迁移事务内276张既有业务表摘要不变；这不意味着禁止正常并发业务请求。部署前完整备份SHA256 `4b3819007c9fca897182464864c3f2d091cb43a0c2e4524a6141490aa2f96e16`，归档目录可读；该份新备份完整恢复 **NOT_RUN**。故障回退保护针对服务与二进制，additive176数据库不会自动降级；真实触发回退 **NOT_RUN**。

部署、合包及实际安装分别有独立回执；网页未构建或部署、原Civu库/项目/样式配置未写。原高德Key与自定义样式仍沿用上批获准配置。

## 真机结果与设备断开边界

APK41 SHA256 `fee743a083bcf728398759fee083d55c55e51f1ac64ea87b2e51e027d6214f60`，429文件构建帧稳定。41为交付批次名，实际Android versionCode=1/versionName=0.1.0。签名、包名、现高德Key匹配布尔值、Civu两份原字节样式与许可均核验，替换安装保留应用数据。

新包确实显示“登录后可查看地图动态”，但地图内的登录按钮真机点按未打开sheet；该失败保留在 `android41-device/login-recovery-cta-failure.json`。从既有侧栏个人资料固定码入口明确登录成功，使用原先唯一已接续的真实Civu账号，不新增测试账号。返回Now自动出现7个动态位置，没有拖动地图。原生mapCreations=1、7封面读取/解码，证明同进程登录恢复没有重建地图；不是严格A/B性能测试。

APK42 SHA256 `af7f66f543f8050e790440aa9d3e9a8f77fe197e3843d0efa1e0e441e3b00d66`，429文件源码帧稳定并通过相同签名/样式核验，已替换安装。提示移到TopControls同级宿主交互层，busy状态不再显示不可执行的登录按钮；41现场未响应的具体native命中根因尚未证实，不能用单元测试代替42真机复查。

21:26:59停止应用进程保留数据，服务器实际idle截止21:56:49；21:57:26只读核对已idleExpired=true、activeAccess=0、family仍有效且未被消费。未修改到期时间、未伪造过期。随后冷启动命令退出1，`adb devices -l`为空，因此应用实际续接、动态恢复及42按钮复查NOT_RUN，USB连接BLOCKED。已询问用户重新连接，恢复后可以继续使用当前真实过期会话，不必再制造一次过期。截图只存用户本地outputs，Git只保留路径/摘要；手机号与token没有写入证据。

## 未完成及取消范围

真实超过30分钟的服务端自然过期已观察；手机冷启动自动接续、42地图登录按钮与区域正常结果：**NOT_RUN / BLOCKED_ADB_DISCONNECTED**。真实断网、30天绝对期限耗尽、完整发布链路、iOS、全仓回归及严格前后性能：**NOT_RUN**。网页本轮延期，不索要网页Key。原DGS双人/双设备社交依赖和其他产品缺口仍按原报告保留，父任务继续IN_PROGRESS，不能将本轮登录修复等同全部需求完成。
