# V5 Profile APIs

状态：AGE-068 的唯一 API 接续规范；2026-10-03。源要求为 `docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md:2106`。不新增第二套 Profile、路由或 schema。旧任务 goal 中「没有 API」是导入时的快照，当前实现复用已完成的 AGE-001/002/003。

## 三项操作与唯一真源

| 操作 | 当前正式路由 | 原生真源与范围 |
| --- | --- | --- |
| GET Agent Profile | `GET /v1/me/agent-private-profile` | 本人当前 PERSON / exact Personal Agent / 已存在 AgentProfile metadata；返回 schemaVersion、六字段 `profile`、私人 `fields`、configured。缺 Private 行读空、不写；缺 metadata 返回 404、不重建。 |
| UPDATE Public Profile | `PUT /v1/me/profile` | 原 `user_profiles` 三字段输入和四字段输出；这是账号本人普通资料，visibility 可为 public 或 private，不是保证一定对外公开。 |
| UPDATE Private Agent Profile | `PUT /v1/me/agent-private-profile` | 本人直接输入九类私人字段，原 aggregate Profile version CAS；显式空 fields 表示清空。 |

附加既有入口继续有效：`GET /v1/accounts/{accountID}/profile` 是普通粗 ACL 与当前字段规则交集后的四字段投影；`GET/PUT /v1/me/agent-profile-visibility` 是当前本人字段规则与同一 metadata CAS；组织工作台的独立组织 Profile 入口仍由原组织领域鉴权。内部 `EnsureAgentProfile` 不是对外 API，也不在公开资料 gateway 中调用。

现有 Private、Field Visibility、Identity/Ownership 与 Memory 规范继续约束各自领域；本文只统一三项现有 API 的接续和横向验证，不复制私人字段、建立影子镜像或把模型权限并入人类资料编辑。

## Public 人类当前写 gateway

唯一原公开资料写 helper `updateOwnProfileInTx` 被两条内部路径复用：

- 旧 `AccessStore.UpdateOwnProfile(ctx,ownerID,input)` 保留签名，供原可信内部调用；它不作为 HTTP 安全缺口的回退路径。
- 新 `HumanProfileStore.UpdateHumanProfile(ctx,digest,initialActor,input)` 仅接受真实 bearer digest 与服务端第一次 Authenticate 的 Actor，再在事务中核验当前 native Session/Account。配置该窄接口缺失时，正式 Public PUT 返回 503，不能退回 ownerID-only writer。

事务显式 `READ COMMITTED` 与 UTC，锁序为 Account `NO KEY UPDATE` → 当前 Session `SHARE` → 原 UserProfile `UPDATE`。Account ID、类型、active 状态与 Session owner/digest/method/撤销/绝对期限/idle 期限都必须一致。现 Authenticate 是 Session UPDATE 与 Account MVCC join，RevokeSession 只 UPDATE Session；二者不先锁 Account。锁顺序不是把初次 Authenticate 当事务授权。

在全部 Profile、旧公开 Intent 和 audit 写后，以 PostgreSQL `clock_timestamp()` 再核 Session/Account；ctx 取消或到期回滚事务。相同输入不写 Profile 或 audit，仍经过最后会话期限检查。成功结果还须 exact 当前 Actor owner、合法原三字段，并与这次请求的归一化值一致，才可返回原四字段。返回 shape 检查是防错误接口接线，不能代替真实数据库授权，也不把 spy 当 native 许可。

旧普通账号类型 Person/Organization/Business 若已有自己的 user_profiles，可继续用本人会话编辑该行。缺行不 upsert。此兼容不创建 organization membership/admin/owner，不把组织实体 ID 当 account ID，也不启用 Business Agent。Private Agent Profile 仍严格 PERSON；组织成员不能把本人的普通 route 选择为组织工作台，不能继承组织或他人的私人资料。

公开资料整体变 private 时，保留原行为：该账号旧 `intents` 中 audience=public 且 state=draft/active 被撤回；fulfilled 和 private 意图不改。不是新 SocialIntent 消费者，不写新的来源事件／outbox／Memory／模型授权。ordinary `user_profiles.updated_at` 与 Agent metadata 的 Profile version 相互独立，不伪造 public CAS counter。

## Public 输入与错误合同

当前 Flutter `public_intent_section.dart` 的原三字段请求保持兼容：

```json
{"displayName":"中文姓名","bio":"本人填写的简介","visibility":"public"}
```

只允许三个完整的 string 字段。局部 decoder 最大 8KiB，单个 JSON object、UTF8、Content-Type application/json（可 charset=utf-8）。拒绝未知字段、重复键（含转义等价）、大小写别名、null、非 string、缺项、额外 JSON、无效 surrogate、非法 UTF8、过大输入与 selector/confirmed/purpose/owner/agent 参数。不改共享 cityseed decoder 或既有 Private decoder。

保留原 trim 与字节限额：displayName 2–80 bytes，bio ≤500 bytes，visibility 只小写 public/private。名称不含控制字符；bio 可换行／制表／回车，其他控制字符拒绝。合法 emoji pair 和历史原文的 literal U+FFFD 可接受。名称/简介输入是本人的直接陈述，不能推断稳定兴趣、位置或事实。

本人 Public route 拒任何 query（含裸 `?`）和 organization workspace header presence（含空值），不接受目标切换。正常成功响应仍是 `data:{accountId,displayName,bio,visibility}`。所有响应 no-store、保留 request ID。Public 错误只固定安全分类和简体中文 message，不返／记原 JSON、SQL DETAIL、session/token 或私人 source。

| 情况 | 状态码与 code |
| --- | --- |
| 未登录、失效／撤销／错 owner/method/current Account | 401 unauthorized |
| workspace presence | 403 forbidden |
| query 或局部 wire 内容无效 | 400 invalid_profile |
| 当前普通资料行缺失 | 404 not_found |
| secure port 缺失、事务错误、ctx 取消、错误绑定的成功输出 | 503 profile_unavailable |

Private 与 visibility 原错误／版本规范不变；本表不扩展到无关旧接口。

## 版本、来源隔离与验证

Private 和字段规则共享真实 `AgentProfile.profileVersion` CAS；并发同旧版本只允许一个成功，另一个 409。Public 保存不改变 metadata、Private、policy、Memory、permission；反向 Private 保存/clear 不改变原 public、旧 Intent 或 public audit。Profile grant 只影响普通可读投影，不允许读别人的本人 Private。AGENT_ONLY 不授予分析／模型许可。

本项实际用 fresh001–064、真实注册路由、独占随机身份和 PostgreSQL 锁屏障验证。原 Public 实现在首次 Authenticate 后 Account 等待期间 session 过期与撤销均实测 200 且写入；RED 和原 source SHA 保留。新实现分别在 Account 等待、Profile 等待、no-op 最后 clock、ctx 迟到取消、默认 RR pool 下当前 Account 变化等场景拒绝并保持源／audit／Intent 不变。非空 Private、policy、Memory、普通 grant 完整行也实测与 Public 编辑隔离。

正式证据：[Profile APIs evidence](../testing/evidence/profile-apis-2026-10-03/README.md)。专属 native4 两轮各134 Test PASS（42真实 native/API、92 transport/pure），0FAIL/测试SKIP；scope vet/build=0，完整旧 public 行与登记8源码前后相同，自有 DB DROP。transport spies 只验证 wire/错误输出，不算真人身份或生产运行；完整 Go 共享回归由根协调，须以其最终实际归档为准，不能借前一任务的 6654 当本次新源全仓 PASS。

本项没有客户端改动，因此 Flutter、真机、辅助技术截图未运行。适用 UX-CHECK-01/03/06/08/10/16 的中文、明确本人目标、当前性、错误／恢复和人类显式提交边界已接入 API；规则接入不等于 UI 消费验收。生产 IdP、真实组织/活动、部署/值守/地图及 A→H 门槛不因本项开放，Closed Pilot / Consumer Beta 继续 NO。

## 根完成核证（2026-10-03）

根独立134 PASS/0fail/skip、targetvet/build0，8 owned SHA稳定、真实001–064/全public原行一致/自有库DROP；worker578原档逐SHA核验，原公开会话expiry/revoke RED与修复、编译及fixture首失败均保留。当前共同scope409/default全Go7063 PASS/0fail/testskip/fullvet-build0、504 API+18owned冻结帧，覆盖最终3原路由和会话漏洞修复。见[根receipt](../testing/evidence/profile-apis-2026-10-03/root-independent-final.json)、88根文件index及共享044全部源帧。原3 API要求达到CODE_LOCAL DONE；新Profile UI、真实IdP与部署/手机API场景未验证。Closed Pilot/Consumer Beta NO。
