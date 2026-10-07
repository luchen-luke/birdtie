# Agent Policy APIs V5

2026-10-03。AGE-070 本人人类设置 API 的唯一 canonical。复用 [Attention](AGENT-ATTENTION-POLICY-V5.md)、[Social](AGENT-SOCIAL-INTERACTION-POLICY-V5.md)、[Autonomy](AGENT-AUTONOMY-LEVELS-V5.md) 的闭集领域校验；这些原模型的进程版本和机器授权边界继续有效。本文描述持久设置，不把偏好转换为认知读取、模型出口、通知投递、业务动作或具体版本批准。

## 用户结果与四个正式接口

本人在当前个人身份下查看和保存三类行为偏好。所有资源由当前原生会话解析，不接受 ownerId、agentId、组织工作台、认知用途、来源、功能开关或 confirmed 参数。

| 接口 | 行为 |
| --- | --- |
| `GET /v1/me/agent-policies` | 返回三类持久配置、明确未配置或过期状态；没有写入、副本修复或续期 |
| `PUT /v1/me/agent-policies/attention` | 更新事件类型对应的本人 Attention 偏好 |
| `PUT /v1/me/agent-policies/social` | 更新七类社交互动偏好 |
| `PUT /v1/me/agent-policies/autonomy` | 更新本人 0/1/2 级未来行为上限；3 级不可配置 |

请求示例只是参数格式，不是实际保存结果：

```json
{
  "expectedVersion": 0,
  "settings": {"level": "LEVEL_2_PREPARE"},
  "expiresAt": "2026-10-04T00:00:00Z"
}
```

`expectedVersion=0` 仅是该类首次创建的未配置 sentinel。版本冲突返回 409，先重读该类真实版本，再让用户检查后保存。即使同样内容，成功 PUT 仍产生该类真实下一版本；重复携带旧版本的请求冲突，不声称幂等成功。GET 是安全重复读取。

## 三类设置合同

- Attention：`defaultRoute` 必需，五枚举 `IMMEDIATE/NORMAL/DIGEST/SILENT/BLOCK`；`rules` 必需数组，最多当前 13 种闭集事件，各类型只一次；可选绝对 `pauseUntil`，在保存起点至期限之间。事件偏好不能制造 `PlaceVisited/ActivityCompleted` 事实或恢复尚未具备的真实处理端口。暂停是绝对时刻，不是周期、时区日程或后台定时器。
- Social：必需 `rules` 数组；只接受原七类 category 与 `DISABLED/REVIEW_REQUIRED`。缺少的类别规范化为明确 DISABLED，存储和回执完整七类。没有 ALLOW、已建立关系、verified 学籍、双向同意或自动发消息语义。
- Autonomy：只接受原 `LEVEL_0_OBSERVE/LEVEL_1_ASSIST/LEVEL_2_PREPARE`。复用 044 validator，066 `Limits().AutonomousAction=false` 拒绝 `LEVEL_3_DELEGATE`。LEVEL_2 只表达准备上限，不能执行邀请、报名、发消息或付款。

JSON 独立 strict decoder 拒绝重复键（包含转义后的重复）、大小写近似字段、null、缺项、未知字段、嵌套未知声明、多个 JSON 值、非法 UTF-8 和超长正文。正文最多 8192 字节。版本为非负 int64，溢出/耗尽拒绝；时间必须有限且可无损落入 PostgreSQL 微秒精度。接受明确 RFC3339 时区后规范化 UTC；有效期限由事务内真实 PG clock 检查，最长 30 天。wire 仅检查形状，不能证明当前期限。

## 唯一持久模型与版本

迁移 `065_agent_policy_settings.sql` 新建 `agent_policy_settings`。键为 `(agent_id,family)`，以现 `agent_profiles(agent_id,owner_id,owner_type)` 三列外键绑定精确 PERSON。没有复制 Account、Agent、Memory、Profile、通知或 consent 表。

每类独立 `native_revision`：首次 1，以后真实 +1；其余两类完整行不变。该版本不是 Profile/Source/Feature/consent/进程内 policy 的版本。确切本人 Agent 派生 advisory transaction key 将短编辑事务串行化，避免两个类别更新后返回整组时发生交叉行锁；并发三个类别各版本独立，不能以串行锁计数冒充共同版本。

GET 对缺行输出 `configured=false/nativeRevision=0/status=UNCONFIGURED` 和保守展示默认（Attention BLOCK、Social 全 DISABLED、Autonomy OBSERVE），不创建任何配置。已有过期行输出 `configured=true/status=EXPIRED`、真实 nativeRevision、原 settings 和原三个时间，不覆盖为默认或延长期限。保存新的明确配置须带保留版本。

表约束/trigger检查闭集 schema/family/JSONB、合法原生 Person/active PersonalAgent/metadata、初始版本、精确 +1、不可变绑定、有限时限及 pause。数据库 JSONB 已转换后不可能恢复重复输入键；该输入责任在 strict wire。删除 metadata/Agent 的真实外键级联移除设置，GET 不修复身份或旧配置。

down 在任何设置行仍保留时原子拒绝，不能擦除人类配置。只在一次性库中验证非空 down 拒绝和空 down/reapply；禁止将测试说明作为生产回退授权。

## 当前会话与事务

真实 Store 用 `agentprofile.PrivateAccess` 的 digest 与服务器解析 PERSON，独立显式 READ COMMITTED/UTC 事务。先锁派生 Account，再复用原当前 Session/active exact PersonalAgent 与 metadata 锁；持久行固定 family 顺序读取共享锁，保存时加当前行更新锁与精确版本条件。

首次 HTTP Authenticate 只得到请求主体。Account 等待期间会话撤销可真实提交；随后 Session 查询用新的当前快照。会话绝对/idle 过期、撤销、停用 Account/Agent、缺 metadata、dev_phone 关闭、错 digest/主体、上下文取消均拒绝。所有读取/写入后、提交前，用同一个最终 PG clock 再检查当前 Session/Account/Agent/metadata；写入还检查该类真实版本与期限。最终回执 `observedAt` 使用这个实际检查时刻，不是来源发生时间或长期许可。

返回 Bundle 仅包含 schema、当前 ownerType/ownerId/agentId、最终 observedAt 与三类 ordinary preference Record。Record 包含 family/configured/nativeRevision/status/settings 与已配置时的 validFrom/expiresAt/updatedAt。有效区间 `[validFrom,expiresAt)`，updatedAt=server validFrom。HTTP 检查 schema/字段/owner 与本次保存内容/精确下一版本/expiry，不将 spy 的形状测试说成鉴权证明；exact Agent 归属由原生 Store 的同事务绑定保证。

错误固定中文，401/400/403/404/409/503，`Cache-Control: no-store` 与原 request ID。数据库、私密正文、bearer、provider、费用或输入 raw JSON 不进入错误。缺真实 Store 能力返回 503；没有 ownerID-only 或进程内策略回退。

## 与已有能力的边界

061 `GET/PUT /v1/me/notification-policy` 继续管理八类人类通知偏好，不是 037 的事件规则；070 不写它或通知 decisions/inbox，不接发送/订阅 hook。Profile、field visibility、Private、Memory、旧 grants 独立保存和独立版本；070 不读取其私人内容，除必要身份 metadata 外不改变它们。

066 默认全 OFF/硬限制保持；普通人类设置可保存，不代表触发认知执行。037/041/044 实际 Service 未有当前专用用途/来源/批准 resolver 时仍空 Unavailable，070 不把本人的设置、进程内 OfflineBoundary、组织 admin 或旧 profile_view 授权当替代。没有新 runtime consumer、模型出口、provider、A2A、自动业务动作或补写认知事实。

## 验收和未运行项目

正常/异常/权限/版本/重连/实际锁等待/SQL形状/旧非空数据/up-down-reapply 的原始本地证据由本任务独立归档；根代理须复核及运行共享默认全 Go 回归后判断任务状态。过程失败和首次 fixture/编译错误保留，不借早期 7063 或其他任务结果声称新065全量 PASS。

本轮无 Flutter UI 变更、没有设置页面、真机/辅助技术/移动端截图验收。UX-CHECK-06/08/09/10/11/16 用于不补猜授权、复用现领域、准确版本、冲突与迟到、安全中文状态和实际证据；规则接入或四 API 成功不能替代消费级界面验收。Closed Pilot / Consumer Beta **NO**。


## 2026-10-06 AGE042 独立消息路由设置

新增 GET/PUT `/v1/me/message-request-policy` 与 GET `/v1/me/message-request-policy/decisions/{personID}`，完整合同见 [消息请求策略](AGENT-MESSAGE-REQUEST-POLICY-V5.md)。沿用本人会话、精确 Person/Agent、原事务、原审计与 CAS；098 两表只存接收人偏好及同 Request 的注释，不是批准真源。incomingRequests 仅 REQUEST/SCREEN/BLOCK，ALLOW 只由当前 accepted Tie 推导；不会把既有065 Social七类设置变成消息权限。SCREEN 待人工审阅、真正筛查仍 Unavailable/OFF。当前只有源码与相关单元证据，098实际迁移/数据库并发/重启/新HTTP native GREEN/UI/正式身份均 NOT_RUN。
