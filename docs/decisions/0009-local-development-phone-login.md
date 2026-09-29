# ADR 0009: 本机开发手机号登录

日期：2026-09-30
状态：已采用，仅限本地开发

## 背景

Birdtie 的 OIDC 服务端流程已经实现，但尚未注册身份提供方。OIDC 是外部身份登录协议，与短信验证码不同。为让开发者在真实身份服务到位前使用私有 Agent 历史、Moment 草稿和 Inbox，产品方允许暂时沿用 Civu 开发体验中的固定验证码 `123456`。固定码不能证明用户拥有该手机号。

## 决定

- 新增 `BIRDTIE_DEV_PHONE_AUTH` 开关，默认关闭。只有 API 监听地址、数据库地址及浏览器允许来源全部为 loopback 时才能启用。无公网或局域网入口。
- 先请求 challenge，再校验 `123456`。Challenge 五分钟过期，同号码请求间隔至少一分钟，每次最多五次错误尝试，成功后一次性删除。不发送短信；接口也不返回验证码。
- 号码标准化后只以带开发用途前缀的 SHA-256 摘要参与持久化。身份 issuer 固定为 `urn:birdtie:local-dev-phone`，与未来真实短信身份和 OIDC 身份分离。首次登录创建私有 Account/Profile，不自动授予编辑角色，不自动匹配 Civu 用户。
- 登录成功使用现有 Birdtie opaque Session；关闭开关后所有 `dev_phone` Session 立即不能通过认证。客户端在 Profile 提供清楚标识为本地测试的入口，默认填写 `123456`。

## 后续

正式手机号登录需要实际短信发送、风险控制、号码所有权验证、凭据恢复和部署审查。Civu 现有手机号、用户 ID 与 Birdtie Account 的绑定必须经单独、可审计的迁移设计；不得凭开发测试码自动合并账户。上线前保持此开关关闭，生产数据库不得使用该模式。

## 来源和复用边界

只读参考 `D:\Program\Civu\Civu-server\api\internal\store\phone.go`、`D:\Program\Civu\Civu-server\api\internal\httpapi\phone_auth_handlers.go` 和客户端 `welcome_screen.dart` 的交互行为。未复制这些文件或其 schema；实现完全属于 Birdtie。见 `docs/architecture/CIVU-REUSE-MATRIX.md`。
