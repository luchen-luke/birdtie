# 社交与明确规则机会通知

2026-10-03；对应 `BT-V4-NOT-001`。本地实现与验证记录，正式状态由根代理核验原队列。来源为 `BirdTie_V4_Execution_Package.zip!/BirdTie_TAPD_Backlog_V4.xlsx#Requirements`。遵循 [持续交互规则](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md) UX-CHECK-01–16；沿用现有 Material、中文文案、领域动作与真实身份体系。

## 用户结果与入口

普通本人查看当前仍有效的社会依赖、重要变更或明确规则活动匹配；在设置或收件箱打开“通知设置”，检查具体版本并确认未来通知路由。组织工作身份不能读取或保存本人偏好。收件箱先取得当前权威已读结果，再打开返回结果指向的同一资源；失败、换主体或迟到响应不打开缓存目标。

| 类别 | 真实来源与动作 | 当前入口 |
| --- | --- | --- |
| 活动安排变更、取消 | 原 Activity / 当前报名依赖、原发布和更新事务 | 同一活动详情 |
| 邀请 | 原组织成员邀请和资格变更；保留原生命周期 | 当前组织邀请页；查看后由用户决定 |
| 联系申请与处理 | 原 connection request / decision | 原联系人申请界面 |
| 明确规则活动机会 | 当前本人明确 ACTIVE 意图与当前已发布活动、城市、地点、资格 | 同一活动详情；不会自动报名 |

这里的邀请沿用原组织成员邀请来源；没有新增活动邀请事实。旧消息、审核、真实 Task 状态等原 14 类通知及其 source gate 保留。Business 独立通知 producer 继续 Unavailable。

## 真源、触发与匹配

新增服务端闭集 kind `opportunity_available`，语义类别 ACTIVITY。客户端和模型不能选择 kind、接收人、label 或授权事实。069 只扩展原 `native_notification_decisions` / Inbox 合同，复用 Activity typed FK；没有另建 Opportunity 权威表。

可触发的实际事务动作：

- 本人明确激活已有 SocialIntent；草稿、读取和预览不触发。
- 原社会活动发布、已发布活动更新。
- 原普通活动发布、已发布活动更新以及活动建议批准产生的实际活动。

producer 在原 Read Committed 写事务内收集 native source，再通过原 `routeNativeNotification` 重查当前来源、偏好与 immutable decision guard。decision / Inbox 和原领域动作同事务提交。普通 GET 机会或通知不会制造机会事件。

强规则资格是闭集精确匹配，不是校准概率：

- 当前 active PERSON 的本人意图，在原本人最多 100 个管理结果中，ACTIVE、FIND_ACTIVITY、IN_PERSON、有限且未过期。
- 明确 CITY Context 或实际 LOCAL City audience target；两者并存必须相交。浏览城市默认值、位置猜测、未知城市不算来源。
- 精确 category 或具体 Place 至少一项；所有已声明 category、Place、area 条件均相交。模糊区域文本单独不足以产生通知。
- 活动 published、未取消、有限且有效的时间；地点 published、城市 published、各自期限仍有效。
- 原活动 ACL、Block、当前 accepted Tie、成员资格、组织或商业供给状态等按实际来源求值。公开活动可在原公共资格下匹配；成员不是 Friend，报名不是出席。
- 接收者与主办者不同；账号、组织 principal 与 entity 的原命名空间和资格保留。

商业供给的原 verified Venue / candidate 期限与来源可参与原资源可见性，这不激活 Business Agent，不授予模型或 Memory 处理许可。

## 版本、幂等与回收

同一 Activity 与本人多个匹配意图，按原确定性顺序选一个有效意图。事件版本为真实持久 `activities.revision` 的 `activity:<revision>`。原唯一键 `(recipient, kind, source, event_version)` 保证一名本人每一活动版本至多一个 decision；相同事件重试和第二个匹配意图不重复提醒。

`source_version` 是当前数据库 tuple / xmin / 原资格关系形成的 opaque SHA256 指纹，含选定意图、活动、本人和主办者、城市、地点、明确 target / context、组织者与相关资格。它不是业务 revision、概率或长期许可。

源取消、过期、Block、撤回、改身份或改资格后，原 source resolver / `birdtie_native_notification_visible` 使旧通知不可见。选定意图替换会改变指纹，但不会把同一活动版本变成新事件；下次真实活动版本才可产生新 decision。偏好版本独立，不制造或重放业务事件。

最后来源 SQL 是该语句的当前快照；之后其他事务提交的新状态不被描述为已经包含于旧快照。关系 ACCESS SHARE 锁用于真实关系级等待，不能阻止或证明每一 source row 的并发撤权已提交。

## 路由与偏好

复用原 061 Policy、本人 PrivateAccess、精确 active PersonalAgent / metadata binding、独立 CAS 与服务端时钟。普通收件箱不要求 Agent 或 PrivateProfile；管理 Agent 绑定的偏好沿用原资格。

| 路由 | 真实本地结果 |
| --- | --- |
| IMMEDIATE | 当前站内 Inbox，高优先级；不表示操作系统推送 |
| NORMAL | 当前普通站内 Inbox |
| DIGEST | durable pending decision；尚无摘要汇总投递 |
| SILENT | decision 记录，不进入 Inbox |
| BLOCK | decision 记录，不进入 Inbox |

未配置、关闭、过期的自定义偏好恢复未来普通 NORMAL；关闭不等于静音。旧 DIGEST、SILENT、BLOCK 不会因关闭而重放。分类规则最多八类，期限最长 720 小时；暂停和实际当前期限沿原验证器。

中文页面首次 GET 不写。默认路由首屏可选，分类、期限和暂停渐进展开；确认预览展示当前版本与关键结果。只批准具体身份、Agent、版本和草稿对象。单次保存、双击防重、409 权威重读、账号/token/workspace ABA 及迟到请求失效均由 controller 守卫。

未知保存结果会实际 GET 核实；相同下一版本和已批准内容只说明“当前设置与确认内容一致”，不推断该请求是唯一原因。未知且未核实不能盲目重发。PUT 领域提交后仍可能在 HTTP 最终会话检查中被拒绝，不能把这种结果宣称为已回滚；重新登录与权威读取才可确认当前状态。

## 当前人类 HTTP 与跳转

已注册的 `/v1/me/inbox`、`/v1/me/inbox/{id}/read`、GET/PUT `/v1/me/notification-policy` 使用实际 HumanSocial 初始认证，避免旧 UPDATE 语句沿等待前时钟刷新已经自然过期的 idle session。缺原生能力 fail closed，没有 owner ID 或 fake fallback。

Inbox 的 RC 事务按关系等待、Account SHARE、资源等待、Session SHARE、fresh PGclock 与最后来源 predicate 求值。markRead 的 Inbox row 等待先于 Session 锁；期间已提交撤权会拒绝并回滚 read_at。JSON materialization 后再调用明确 Account SHARE → Session SHARE 的非刷新原生校验，检查当前 digest、主体、状态、绝对与 idle 期限、dev 开关与 context。

一个已授权、已经提交的 markRead 在随后响应会话失败时仍是完成的领域动作；最终检查只抑制 payload。客户端不会把失败视为已拿到有效目标。已读条目点击仍重新 markRead 并按当次返回 target 跳转；原缓存 item 不承担当前授权。

没有新 server 路由、通用许可 resolver、第二身份或通知投递 provider。本文实现不开放模型、私密认知、自动报名、联系、聊天或组织权限继承。

## 本地证据与限制

- `work/v4-not001/native-red1` 保留真实原先 200 泄露 / read_at 写入 / idle 复活；`native-human-green1` 对应原生修复。
- `native-opportunity1` 包含五路与每路 100 个实际事务重试；`native-opportunity2` 保留错误 Place enum fixture 的真实失败；`native-opportunity3` 是当前阶段 360 PASS / 0 FAIL-SKIP、完整 public 保持、owned DB DROP。
- `schema1`、`schema2` 保留实际首次 fixture / catalog 失败；`schema3` 真实 up、非空 down 原子拒绝、空 down、reapply、完整旧 public 与 catalog 相等、owned DROP。
- 客户端 target 16 项、中文 360×640 / 1.6 倍字体、语义与交互为真实 widget contract；根完整 Client 188 源同帧 analyze/test/Debugbuild 成功、316 个命名测试。该 APK 与新 native 来源的具体帧分别记录，不能混称生产验收。
- `native-final1`、`native-final2` 两个 fresh001–069 库均真实 369 Test PASS / 0 FAIL-SKIP，新矩阵28；三目标包 vet/build0、482 internal Go 观察源及14 owned源稳定、完整 public 相等、069稳定、owned DROP。六个 post-JSON 原生 session 负例、无 Agent 当前 Inbox List/Read、真实同事务源/audit/decision/Inbox rollback、八个并发事务重试均通过。
- 27个产品源冻结（14 Go / 11 Dart / 069上下迁移），正式 worker archive 在 `docs/testing/evidence/social-opportunity-notifications-2026-10-03/worker-final1`。根独立回归与新通知手机证明在独立收据归档，不重写 worker manifest；本文不先宣称根验收完成。

### 当前069真机补充

根独立 `work/v5-age051/root-notifications1` fresh069 实际369 PASS / 0 fail-skip、test/vet/build0、full public保持、DROP。worker随后只读克隆根自有 MOM 开发库 `birdtie_moment_phone_3ceff83b5365` 至 `birdtie_notification_phone_d93a7ca2c75e`，apply069 前后全部旧 public 行相同；最终原库全部 public 仍相同，两个旧 Moment 全列与私人源均保持。

当前 API Go/SQL619 hash 在编译前后相同，hidden 新 API PID24872 / loopback3705，ADB3697对应3705。EXE SHA `E1C916F1E5830A3B1071623D2A47D91DC6686B3FB8D81B55E4E6147CFB12AC6D`；已安装根188Client帧 APK `7d2a7ceffc152c35459b0c67995416544e89c17c03f074a261ef9da04bebd819`。这是具体编译帧，之后根BIZ代码变化不被混称该EXE的源码。

实际手机证据：Inbox / Settings双入口、V0首读、取消预览选定领域 bytes相等、确认BLOCK V1、关闭保存V2、1.6字体行动可达并恢复1.0。两个独立QA原生本地登录不是手机同token：原owner PRIVATE明确CITY/Place意图实际confirmed激活，另一合成Person发布新活动revision2，BLOCK decision无Inbox；UI关闭后真实活动改期revision3产生NORMAL Inbox，第二明确匹配意图没有重复。手机点击当前通知实际markRead后进入该活动当前详情，未报名。

原生自有 relation PID屏障让旧Inbox请求真实等待；关闭页面取消旧请求，API503/21.7秒。随后真实退出、另一合成账号登录、释放锁，另一账号Inbox空且偏好V0；恢复原owner实际重读V2。不能把这个取消路径称为跨新账号迟到200实证；该负例由独立widget ABA / late contract及native postJSON会话证明补充。机上未做TalkBack、强停或重启。

新手机收据与原始截图/SQL在 `work/v4-not001/phone` 和独立 `device-final1` 归档；ignored QA bearer文件不打印、不归档。保留独有DB/API供根协调交接，没有停进程或DROP正在使用的库。

尚无推送 provider、摘要投递、现实用户试点、生产身份验证、TalkBack 真机全流程或生产运营送达证据。已被自动审批拒绝的当前 App / API 停止重启动作记 NOT_RUN，不通过替代命令规避。Closed Pilot / Consumer Beta 原门槛保持。
