# BT-AUT-002 原生身份边界记录

日期：2026-10-01。结果：**代码路径已补齐，生产登录验收阻塞**。

## 审计与实现

Go API 原有 OIDC discovery、HTTPS 端点检查、ID Token 签名/issuer/audience/nonce 验证、服务端 issuer+subject 账号绑定、一次性 PKCE 交换码，以及仅存摘要的 Birdtie Session。客户端此前只处理 Web 回调。现 Android/iOS 注册 `birdtie-auth://callback`，由系统浏览器返回；客户端在平台安全存储中暂存五分钟 PKCE verifier，仅处理精确回调，先消费 pending 状态再交换代码，以 `GET /v1/me` 核验服务端身份后保存 Session。启动时从安全存储恢复会话并重新校验；401 清除本地凭据，登出调用服务端撤销。会话每次有效请求延长 30 分钟 idle 窗口，但绝对有效期为 8 小时，期满重新登录。客户端 release 不展示开发手机号入口；Go API 启用开发验证码时仍要求 API、数据库和 Web origin 均为本机地址，并拒绝开发会话在关闭该开关后访问。

固定客户端回调由部署配置 `BIRDTIE_OIDC_CLIENT_REDIRECT=birdtie-auth://callback` 指定。Go 只额外允许这一精确自定义 scheme；IdP 回调仍要求 HTTPS API 地址。当前 API 的客户端回调配置只有一个，Web 与原生需使用匹配部署配置。

## 验证

| 检查 | 结果 |
| --- | --- |
| `flutter analyze` | PASS，0 问题 |
| `flutter test` | PASS，74 项，含原生 PKCE 正常/错配回调、会话恢复与失效 |
| `go test ./internal/oidcauth ./internal/httpapi` | PASS，含仅精确原生回调允许的新增测试 |
| `flutter build apk --debug`，显式本地 API/Mapbox 开发配置 | PASS，真机 `c641566b` 安装成功 |
| `adb shell am start -W -a android.intent.action.VIEW -d 'birdtie-auth://callback?error=oidc_login_failed' app.civu.civu_mobile.birdtiepreview` | PASS，冷启动解析到 Birdtie `MainActivity`，应用进程仍运行；这是回调路由检查，不是登录验收 |
| `automation/verify_auth_boundary.py` 对本机 API | PASS：匿名 `/me` 401；两个开发身份各自 `/me` 200 且账号不同；跨账号私密资料 404；登出 204 后 `/me` 401；另一会话仍 200 |
| 本机 `GET /v1/auth/oidc/status` | 200，`configured:false`；无法进行真实 IdP 授权或已验证账号真机验收 |

**恢复条件：** 提供并配置可信 OIDC issuer、客户端 ID/必要 secret、正式 HTTPS API 回调与 `birdtie-auth://callback` 客户端回调，并在目标真机以真实授权用户完成授权、账号绑定、重启恢复、过期/撤销和跨账号权限验证。上述本机开发身份及回调路由不证明生产身份或 SMS 所有权。
