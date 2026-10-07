# NOT001 原生与消费审计

2026-10-03；根代理领取 `BT-V4-NOT-001`，worker `new_social_notifications`。只在登记范围实施，queue、共用报告、server 与转交 MOM 的 map 文件不写。原 scope 审计在 `work/v5-age051/next-after-now-audit.md`；原任务只读快照在 `work/v4-not001/task-source.json`。

## 原合同与真实差距

原 acceptance 要求活动变更、邀请、联系申请、强机会类别及偏好控制。旧 061 的 14 种真实 native producer / decision / source guard 支持前三类和独立 policy CAS，缺真实机会 kind、consumer 偏好页与当前安全跳转。原即时机会 Candidate 不是持久事实；Task 状态、客户端 match JSON 或模型 score 不能替代机会通知 producer。

最终设计复用 Activity typed FK 与原 decision / Inbox，同原 native writer 事务生成 `opportunity_available`。069 保留旧 CHECK、FK、guard；不引入 Opportunity 权威副本。强匹配使用本人明确 CITY 和精确 category / Place、当前公开或已有资格供给。没有概率校准、自动推断或 Agent 内容用途授权。

## 已证原生缺口与修复

`native-red1` fresh068 真实三路径：

1. ListInbox 等自有 Inbox relation lock 时撤销 session，旧接口返回 200。
2. ReadInbox 等自有 row lock 时撤销 session，旧接口返回 200 且 read_at 已写。
3. Policy 初始 session row 等待至真实 idle 过期，旧通用 Authenticate 沿语句前时钟刷新，旧接口返回 200。

barrier 均匹配本 fixture locker 的真实 PID / 当前 DB，不扫全库模糊 SQL、不用 spy 代替授权。原始 0 PASS / 5 FAIL events 保留，未当编译失败声称权限 RED。

Inbox 新原生本人方法保持原 source gate，用 digest + 初始实际 actor、Account / Session 锁和 fresh PGclock；资源等待先于 Session，拒绝回滚 read_at。HTTP 改用真实 HumanSocial 初始认证及 JSON 后非刷新检查。Policy 原 PrivateAccess / Agent metadata / CAS 不变。新 `native-human-green1` 对应三路径 4 PASS / 0 fail-skip。

## 机会来源与副作用

- 真实激活、发布、已发布更新、原审核批准的 writer 接同事务 producer；GET / preview 无通知副作用。
- 同活动实际 revision 每本人一个 event，源指纹含真实当前依赖；第二匹配意图、100 个重试不重入 Inbox。
- 五路决策、关闭恢复未来 NORMAL、selected Intent 失效、Block、city expiry、Place hidden、意图 expiry、未知城市、冲突 Context、cancelled Activity、suspended Account 实际 PG 验证。
- 无 Agent 的普通通知不会制造 Agent；个人偏好仍要求原精确 active PersonalAgent / metadata。
- 原邀请来自组织成员生命周期；不是额外实现活动邀请。既有 14 native kinds 在配置的 disposable DB 内实际复验，未用未配置跳过代替。

## 证据历史

| 帧 | 实际结果 | 说明 |
| --- | --- | --- |
| client-target1 | 8 PASS / 1 FAIL | 异步 test assertion 尚未等 MockClient 进入，改为实际 Completer barrier |
| client-target3 | 13 PASS / 3 FAIL | 中文 fixture 缺 UTF8 wire header，修 fixture，未绕产品断言 |
| client-target4 | 16 PASS | 中文偏好 CAS / ABA / 未知响应 / 大字 / 当前读后跳转 |
| analyze-target-frame2 | 5 infos / exit1 | 自有 test lint 四项已修；另 MOM 自有一项由唯一 writer 修 |
| analyze-client-freeze3 | exit0 | 真实 scoped analyze / no issues |
| native-opportunity1 | 10 PASS / 0 | 首次五路 + 100 真实重试，原 SHA 历史保留 |
| native-opportunity2 | 358 PASS / 3 FAIL events | Place 状态非法 fixture 枚举，用真实 hidden 修正 |
| native-opportunity3 | 360 PASS / 0 FAIL-SKIP | 481 internal Go 观察源稳定、14 owned 稳定、vet/build0、完整 public 保持、DROP |
| native-final1 | 369 PASS / 0 FAIL-SKIP | 新矩阵28；482观察源/14owned稳定，069、全public保持，三包vet/build0、DROP |
| native-final2 | 369 PASS / 0 FAIL-SKIP | 同14owned SHA独立fresh库，观察源也保持；完整public/069稳定，vet/build0、DROP |
| schema1 | FAIL | required host_label fixture 缺失，原始日志保留 |
| schema2 | FAIL | down 后 function body CR 差异使完整 catalog 比较不等，修为精确旧定义 |
| schema3 | PASS | up0 / 非空 down3 原子拒绝 / 空 down0 / reapply0；旧全行与 catalog 相等、DROP |

完整命令与原始日志在 `work/v4-not001`。最终两轮为同 owned 源的独立 fresh001–069 库，原有070 Business surface未作为目标运行；根共同全Go窗口才按完整 current schema 验证。27产品源冻结，正式 worker archive 在 `docs/testing/evidence/social-opportunity-notifications-2026-10-03/worker-final1`；根独立验收收据另存，不把阶段帧或 worker范围证明当最终全仓回归。

新增六路径 post-JSON 测试使用实际 AccessStore 的 final validator；barrier仅延迟调用，不能提供Actor或授权结果。List/Read/Policy响应期间撤权、实际Session row等待后自然idle过期均401。PG实证无Agent本人List/Read正常、同事务真实Activity revision/audit/decision/Inbox全部rollback、八个独立RC事务相同事件不重复。rollback是直接调用实际同事务producer的显式rollback证明，不冒称public HTTP provider故障注入。

## 消费与权限边界

中文 Settings / Inbox 通知设置、当前版本预览与确认、首读不写、409 重读、未知结果实际 GET、token / workspace ABA 与异步失效。关闭 = 未来 ordinary NORMAL；SILENT / BLOCK 无 Inbox，DIGEST pending 未投递，IMMEDIATE 是站内优先级。

读失败和迟到不跳旧目标；markRead 成功也仅按当前返回 target 导航。表关系锁不是每一源 row 的撤权锁，最后 SQL snapshot 之后的变更不宣称已包含；JSON 后重新检查实际 session。领域已提交后响应拒绝不能声称领域回滚。

依原协议，本地可调用闭环、widget 和根 APK 证据分开。当前模型 / memory processing purpose / Business Agent / 推送运营均没有新增授权。根已保存完整 Client188 / 316PASS 与实际安装 APK；最终069手机与对应帧见下文独立收据。此前 autoapproval 拒绝 App / API 停止重启记 NOT_RUN，TalkBack / 真实用户 / 生产送达未运行。旧发布门槛不变。

## 最终069手机收据

根已独立复验369/0并核验3591 worker文件及27产品SHA。worker作为唯一手机操作者，原MOM DB只读克隆→owned069、原全public前后相等、旧两个Moment全列及PrivateProfile/Memory/Evidence/Context/原Seed保留。当前编译619 API Go/SQL hash前后相同、独立PID24872/3705 health ok、ADB3697→3705；当前根已安装APK7d2a7ce...未重装/重启。

真机双入口与中文preview/cancel/save，V0→BLOCKV1→disabledV2；native独立QA登录、PRIVATE CITY/Place明确Intent confirmed激活、另一合成Person真实发布Activityrevision2→BLOCK无Inbox；真实改期revision3→NORMAL一行，第二active匹配Intent不重复。当前手机读后打开同一活动详情，read_at非空且无报名记录。字体1.6控制可达、恢复1.0。

旧请求真实relation PID等候后页面dispose取消，服务503/21701ms，之后logout/另账号devLogin/受控释放锁，另账号Inbox空/V0，恢复原ownerV2。准确限制：不是跨新身份后到达的200；真实迟到合同在widget与postJSON native各自证据中。没有人为代理权限、修改原源或制造公开Moment。

准备夹具的实际失败保留：`me.accountId`误用（真实`me.id`）、activate缺具体`confirmed:true`、活动时区错误（恢复实际City Europe/London而非猜UTC），手机号UI多输入一位在有效登录前修正。两QA bearer只在ignored精确文件保存，不打印/归档，不冒称手机同会话。

独立device-final1保存新手机证明，原worker3591manifest不重写。owned DB/API保留为根可继续使用的本地交接，禁止StopProcess/force-stop/卸载或重装；正式restart NOT_RUN。根全当前Go仍由根共同冻结验证，手机当前具体EXE帧不冒称随后BIZ修复源码已包含。
