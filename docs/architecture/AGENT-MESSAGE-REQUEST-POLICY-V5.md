# 消息请求策略 — AGE042

2026-10-06。复用 [Social Interaction Policy](AGENT-SOCIAL-INTERACTION-POLICY-V5.md)、[本人策略 API](AGENT-POLICY-APIS-V5.md)、[实体动作](ENTITY-ACTION-CONTRACT.md) 及原 connection_requests / person_ties / conversations 领域。本文件只补 AGE-042，来源为 [AGE 原文](../product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md) AGE-042 与原唯一队列，不另建需求或授权体系。

## 四种结果与权限

| 结果 | 当前人类入口的含义 |
| --- | --- |
| ALLOW | 当前双方存在 active Tie，且原 friend Request 已 accepted、参与者完全一致；仍逐次检查原聊天 ACL、真实当前会话与双向 Block |
| REQUEST | 本人明确创建原 pending Request；接收人仍须主动接受，不能提前聊天 |
| SCREEN | 创建同一个原 pending Request，标记 PENDING_REVIEW；不走普通申请通知投递，不自动接受、创建 Tie/Conversation 或发送消息 |
| BLOCK | 不产生新请求或聊天效果；不返回对方偏好、私密资料、拒绝原因或可复用来源版本 |

真正的 Agent 内容筛查目前 **Unavailable / OFF**。SCREEN 是待人工审阅的路由，没有模型输入或筛查结果，也没有筛查用途同意。四态设置、041 REVIEW_REQUIRED、044 自治级别、模型 confirmed、沙箱动作批准，均不授消息、Agent、读取或工具权限。没有自动发消息、自动建立关系、批量邀请或付费处理。

默认未配置沿用原人类 REQUEST 路径。这不启用 Agent。配置失效、过期、未知、绑定不一致或撤权时关闭新请求路径，不静默恢复默认。双向 Block 优先于 active Tie。合法已接受的旧 `scope=conversation` 保留其原专用会话 ACL；不将专用同意升级为 Person 全局 ALLOW。

## 本人设置及接口

| 接口 | 返回或提交 |
| --- | --- |
| `GET /v1/me/message-request-policy` | 当前 Person / PersonalAgent 的真实持久版本，UNCONFIGURED / ACTIVE / EXPIRED 状态 |
| `PUT /v1/me/message-request-policy` | 严格三个字段：expectedVersion、incomingRequests、expiresAt；incomingRequests 仅 REQUEST / SCREEN / BLOCK |
| `GET /v1/me/message-request-policy/decisions/{personID}` | 当前人类与该 Person 的四态路由；不返回接收人策略配置或身份资料 |

首次 expectedVersion=0；更新必须匹配原版本并 +1。UTC、微秒精度、有限日期；期限必须晚于当前数据库时钟，最长 30 天。禁止配置 ALLOW，它只由原已接受关系推导。相同内容的 PUT 也增加版本，旧版本重试返回冲突，不能假称幂等成功。未收到保存回执时先重读本人设置，不盲重试。

本人接口从实际当前会话解析身份，不接受 ownerId、agentId、组织工作台、查询参数、confirmed 或用途开关。非法/过期/撤销会话、跨主体与迟到响应拒绝；错误为固定中文恢复提示，不输出原数据库错误、令牌或原始私密数据。

原人类 Request / Decide / Start / Send HTTP 写入口使用 `connection.CurrentStore`，捕获当前 actor 与 digest，在原事务内校验。旧路径、实体 ID、好友接受和专用会话不换真源。可选 `X-Birdtie-Message-Policy-Version` 仅是当前来源摘要条件，必须为 64 位小写十六进制；不授权限、不替代原 EntityAction 条件或具体用户动作。来源变化、xmin 的 ABA、策略更新与撤权须重读。摘要包含双方账号、接收人投影 ACL、策略/绑定、双向 Block 和原 Tie/Request 版本；没有文本、Memory 或模型出口。

## 原事务与迁移

098 仅增加 `agent_message_request_policies` 与 `connection_request_policy_bindings`：前者沿用 053/054 精确 Person / Agent FK 与单调 CAS，后者只给同一个原 Request ID 添加不可变路由说明，不是批准、回执或投递账本。旧未配置申请不增加注释行，原请求和旧 ID 保留。非空配置、注释或已有策略编辑审计时 down 拒绝抹除历史；不在生产执行 down。

人类写者先按 UUID 顺序获取双方 Account，再获取相关表锁；Start / Send / Decide 只预读目标，不先持有 Tie / Conversation / Request 行写锁。原 EntityAction、原好友/会话 ACL、速率限制、卡片双方可见性、审计和真实通知路由继续执行。普通申请只在 REQUEST 分支投递；SCREEN 只留原 pending 项。

同一个 receiver fence 贯穿原 helper 与外层事务。Create、Bound Create、Decide、Start、Send、NewPeople 的最后审计/原条件检查后再复核，再提交；NewPeople 的额外审计不能绕过最后检查。最后检查用同一个数据库 clock_timestamp 点复核当前源、期限与实际执行人的 Session；接收人 accept 的 actor 是接收人，不能改记为原申请人。返回 HTTP 前还校验捕获的实际会话，迟到结果不能串账号。

原 `POST /v1/me/new-people/invitations` 通过 Current invitation port 捕获真实调用人及 Session，再调用原 NewPeople 源/候选/双方 consent/明确人工确认路径。它不借普通 friend endpoint；同一 receiver fence 的期限还与两条原匹配意图及其已发布 City/Place 的期限相交。自己的 pending Request 不会被误当来源撤回。原原生写者仍是唯一人类邀请效果，模型 confirmed 不能调用该 HTTP 路径。

锁设计的静态核对不是并发验收。相关表锁较粗，锁等待、吞吐、死锁与外部旧写者交错仍需后续原生检查。本轮没有运行数据库，不能据此宣布数据库可靠性已通过。

## 当前证据与未验证项

用户 2026-10-06 指定逐项只跑相关单元测试。证据在 [本项证据目录](../testing/evidence/message-request-policy-2026-10-06/)；命令、目录、退出码、原始输出和受影响文件 SHA 由机器清单记录。

- 原 native RED 在规则更新前已实际运行：原注册 Request → Accept → Tie → Chat 成功后，本接口 GET 实际 404。失败与清理记录原样保存，不把随后单元通过说成该 native 场景 GREEN。
- 本轮单元覆盖严格格式、四态优先级、过期/源/会话负分支、同 Request 的 SCREEN 注释与非普通投递、当前 writer 接线、迟到/换主体及中文错误。HTTP spy / SQL 返回替身均标为单元，不是实际数据库或生产登录。
- **NOT_RUN**：098 fresh/current-data up/down/reapply、旧数据/xmin/catalog 保留、真实并发/撤权/ABA/时钟等待、重启后持久化、当前版本原注册 HTTP GREEN、完整 Go、vet/build、Flutter/UI/辅助技术/真机、IdP/生产及 CSSA 试点。
- 无新增 Flutter 控件或设置页面；消费入口及人工审阅显示仍需后续接线和验收。真正 Agent SCREEN 筛查仍不可用。当前任务不能仅凭单元绿灯描述为完整持久化或产品验收完成；Closed Pilot / Consumer Beta 保持 NO。

适用 UX-CHECK-04/05/06/07/08/10/11/12/16：同实体原领域、可检查的主体与后果、未知与过期、设置不等于批准、换主体/迟到拒绝、无模型原路径、持久化如实标未验证、日志不保存文本。UX-CHECK-14/15 的界面及真机证据本轮 NOT_RUN。


## 2026-10-06：本人消息请求设置消费入口

原 Settings 在“隐私与安全”内增加“消息请求设置”，复用原 `_openPersonalRoute` / `NotificationDestinationBoundary`。只管理当前 Person 的原 GET/PUT，不改 Inbox、好友请求、通知、记忆入口或后端真源。页面沿用现有 Material 主题；简体中文；有正常、未设置、过期、加载、不可用与未知状态，320 宽/大字号 2/深浅主题相关 widget 单位覆盖。

只可选 REQUEST（由我决定是否接受）、SCREEN（先留待人工审阅）和 BLOCK（不接收新的请求）。ALLOW 仅解释为原 accepted Tie 推导，不可选择。BLOCK 不删除现有关系或宣称屏蔽全部消息；SCREEN 不投普通申请通知，不自动接受/聊天，也不称模型已筛查。未配置仍为原 REQUEST；过期配置关闭新请求，不静默恢复默认。

有效期须由本人选择，最长 30 天；可从当前已保存配置形成草稿，未设置/过期不暗填期限。检查预览显示本人、具体版本、收件规则、有效期和后果；返回修改不写入，最终确认才发送 original expectedVersion/incomingRequests/expiresAt 三字段，UTC 微秒以内。没有 owner/Agent/provider/批准开关或工作台 header。

内容/重读来源/身份变化废除具体版本预览；账号、token、组织、transport/base/widget 来源及通知 ABA 后旧页或原目的地永久退役，迟到结果不会恢复旧稿。保存前 notify 期间身份变化也不发出旧 PUT。409 重新读取再审；保存未知（网络/5xx/404/错误200/不符版本或 Agent）仅 GET 核实，不重发。原 handler 在 Store.Put 后还回校 Session，故 401/403 也不能证明未提交：客户端标未知并提示恢复本人登录后只读核实。GET 即便返回原内容和 version+1，只证明当前配置，不伪造上次操作成功回执。

相关 unit 最终 final06：38/38 PASS、0 fail/skip、3 新文件，exit0；24 controller、9 page、5 原 Settings 入口/退役/返回。首次真实 Settings 缺入口 RED 为运行后 No element 的 1 error，非编译失败。units02 的 Semantics 非 const 编译失败、units03 的 SemanticsHandle 清理时机夹具失败均原样保留并单独分类；没有计为产品或原生验收。raw/准确命令/目录/退出码、差异与 SHA 见 `docs/testing/evidence/message-request-policy-consumer-2026-10-06/README.md`。

仍 PARTIAL：本轮没有接同 pending Request 的 Inbox/人工审阅列表，也没有真实 Agent 筛查；不把设置页称为完整 SCREEN 审阅闭环。098 实际数据库/并发/持久化/原注册 HTTP GREEN、完整套件/analyze/build、真机/截图/实机辅助技术、真实 IdP/生产及试点均 NOT_RUN。此前后端证据保持，不以本轮 MockClient 代替真实认证或落库。Closed Pilot / Consumer Beta NO。

## 2026-10-06 原 Inbox 同申请人工审阅消费者（增量）

继续原 AGE042，不新增请求、批准、权限或任务体系。原 `InboxPanel` 的正常、无通知和通知读取失败分支均已有 `SocialInboxSection`，直接读取本人最近 100 条原 Request。因此 SCREEN 不投普通通知，也不意味着申请列表应为空。此前客户端没有消费 `policyDisposition` / `screeningStatus` / 有效期，本阶段补这些字段及中文“待人工审阅；尚未进行 Agent 筛查”和“已过期”。原好友、对话、社交意图入口及通知导航保留；本人申请读取继承该 Inbox 的当前 transport / base，而非另选默认环境。

点击原申请的接受、拒绝或撤回，只打开同 ID 的审阅页，不发出决定。原 `GET /v1/me/connection-requests` 重新读取原对象；显示范围、对方、原说明、有效期和具体后果，再由本人确认。提交前重读同 ID、比较不可变内容及当前状态，过期、缺项或变化时取消旧确认。只有原 `POST /v1/me/connection-requests/{requestID}/decision` 可执行决定；原后端 CurrentStore / Session / 收件人 fence / pending 状态 / 双向 Block 与聊天 ACL 不变。friend 接受只使用原 Tie 结果，不猜 Chat ID；conversation 接受使用原成功 DTO 的 Conversation ID，不自动打开聊天或发送消息。人工预览不是新的持久批准或 CAS 权限。

原成功 decision DTO 比列表 DTO 少对方/方向/筛查说明，Go 原结构会输出这些字段的空值。客户端按同 Request ID、具体动作对应状态、原 scope/note/createdAt/expiresAt 及合法 Conversation ID 验证，兼容原空字段，不手造完整名单式成功。不把接受好友称为已成功开始聊天。200 回执与后续列表刷新失败分别表达。

超时、5xx、格式错及可能提交后失效的 401/403 等没有可信结果时，进入 UNKNOWN，当前页面只 GET 核实、禁止重 POST。404 / 列表缺项不是权威未生效；列表 LIMIT 100 的缺项不能判定撤回。读取当前相同状态不是本次操作回执，不猜谁完成决定。409 重新读取并审阅，旧确认不能直接重发。当前没有跨 App 重启的持久决定 operation key/回执恢复机制；本阶段不补第二套账本，也不宣称跨重启未知结果可靠性已完成。

account/token/auth/workspace/source/client/base/widget 变化及收到通知后的 ABA 使旧页面/批准永久退役；发出前固定原参数，再核实当前绑定。复用原 `NotificationDestinationBoundary`，固定路由的监听对象，原 Inbox 自身退役或销毁时通知子路由关闭批准及旧内容。迟到读写不能进入新主体。原 GET 列表并非新增最后 Session 回读 fence，本阶段未改变 Go 读取者；后端原生会话撤权及实际 HTTP 边界尚未本轮实测。

相关单元 final10：81/81 PASS、0 fail/skip/loading error、四文件、exit0：33 controller、10 widget、5 原 Inbox 入口、33 原 ConnectionSource（原 21 cases 保持 + 12 DTO cases）。390 Dart/pubspec 编译输入前后 SHA 稳定。首次真实入口 RED、早期加载编译错误、窄屏 lazy finder 夹具失败，以及真实路由重建后的 disposed notifier 产品错误均保留 raw，不抹去；最后产品错误修复为提交路由前固定监听与工作台参数。证据见 `docs/testing/evidence/message-request-review-consumer-2026-10-06/README.md`。

适用 UX-CHECK-04/05/06/07/08/10/11/12/16；浅色/深色、320 宽和 2 倍文字及人工取消/确认、未知核实、工作身份/来源 ABA 为 widget 单元。真机、屏幕截图、实机辅助技术、原注册 HTTP、098/native DB/迁移/并发/重启、全面检查/analyze/build、真实 IdP/生产/试点均 NOT_RUN。本消费者是原申请的人工审阅；真正 Agent SCREEN 仍 Unavailable / OFF。AGE042 保持 PARTIAL；Closed Pilot / Consumer Beta NO。

## 2026-10-06 原 Request 列表最后会话响应保护（增量）

上一阶段留下的原 `GET /v1/me/connection-requests` 最后 Session 响应保护缺口，现已在该原 handler 接入。保留原 `ListRequests`、同 Request ID、最近 100 条、当前参与者/资料 ACL、SCREEN 标注及领域错误映射，未改其它关系读写者、Store、路由注册或 schema。该本人 GET 只从原 `messageWriteAccess` 捕获当前 Person/Session，不接收组织工作台、owner 查询参数、body 或 confirmed；服务 nil/typednil 与已取消请求关闭，不调用不可用端口。

读取后复用原写者 `messageWriteResponse`：先编码原 DTO，再以捕获的 actor/digest 调用现有 `HumanSessionStore.ValidateHumanSocialResponse`，检查响应前取消，然后返回 no-store JSON。不重新解析变化的 Authorization 为另一个人，不刷新最终 idle 期限，不授予新权限。原 PostgreSQL 校验器仍是现有 Account-before-Session 锁与最后数据库时钟的同一路径；本轮没有修改它，也没有运行数据库来验证实际锁等待或撤权。空列表仍为原真正空结果，nil 服务不伪装为空列表。

实际已注册 handler 的运输单元 RED：旧接口在 ListRequests 替身模拟撤销、换主体、取消之后仍返回 200 含合成私密 canary；red01 为 4 run/4 fail（1 顶层 + 3 分支）。最小修改后 final03：51 run/pass、0 fail/skip、6 顶层、exit0；其中 4 新顶层及 2 原 response/writer 单位，未重跑旧139或整仓。覆盖捕获的同 actor/digest、原 ID/SCREEN/空列表、初始失效/缺当前接口/组织及越权参数拒绝、nil/typednil、迟到401、取消、编码失败先于最后校验、原领域错误与错误正文脱敏。941 Go/module 编译输入各阶段前后 SHA 稳定，仅记录哈希、不复制全库。原失败/raw及精确命令/目录/退出码见 `docs/testing/evidence/connection-request-session-fence-2026-10-06/README.md`。

这些是 handler + 明确单元替身证据，不是 native PostgreSQL、生产身份登录、真实 HTTP 服务/用户、实际98持久化、锁并发、手机或发布结果。DB/迁移/nativeHTTP/重启/全量/vet/build/设备仍 NOT_RUN；真正 Agent SCREEN 仍 OFF，跨重启 UNKNOWN 操作回执恢复仍未具备。AGE042 保持 PARTIAL，Closed Pilot / Consumer Beta NO。

本切片严格是同一捕获 Person/Session 的返回前复验，不代表整份 Request、对方 profile/Block ACL 与最终资源版本绑定。名单及说明仍由原 `ListRequests` 查询保护，本轮未改变或原生验证该域读取。原 SQL 的 `Unavailable account` 英文 fallback 尚存，未因本任务越过获租范围修改 PostgreSQL；它是后续中文消费核验项，不能把本切片描述为全部中文或资源生命周期验收。

## 2026-10-06 原申请列表系统账号占位文案（增量）

原 AGE042 的中文小切片：`postgres/connections.go` 中实际 `ListRequests` SQL 唯一系统占位 `Unavailable account` 改为“账号暂不可用”。只改该 literal，原参与者/资料字段 ACL、双向 Block、说明隐藏、列/ID/排序/LIMIT100/Scan 及文件其它字节完全保持。文案不说明是否封禁或停用，也不用于决定读取、接受或聊天权限。未改其它接口的文案，不在客户端对英文字符串做替换；用户自行填写的英文昵称保留原样。

Go 新单位明确标记 `UNIT_STATIC`：从实际 `ListRequests` 的 Query AST 取 SQL literal，确认唯一中文系统占位；替换回原英文后的完整 SQL SHA 必须等于 baseline 的 `fe248d6d8ab5f4ec502da4a96844f0688364e256706b4cb0f1dd244de7dbbf96`，而不是另建未使用的生产 helper。首次 red01 为 1 run/1 fail/exit1 的静态契约 RED，不称 native 业务 RED；改实际源后 green02 为 1 run/pass/exit0。仅两个新增原审阅页 widget 单位（synthetic DTO）验证中文系统名显示及英文用户昵称保留，widget03 为 2/2 PASS/exit0，393 Dart/pubspec 前后 SHA 稳定。原 10 widget case 正文逐字节保留，未重跑 51/81/139、全量或 build。

精确命令、目录、退出码、静态和合成单位性质、旧源/差异及 hash 见 `docs/testing/evidence/connection-request-chinese-fallback-2026-10-06/README.md`。没有 native SQL/PG/098/实际 HTTP/锁/权限实测、设备/截图/真实无障碍/登录/全面分析或构建证据，均 NOT_RUN。仅此原申请列表系统占位已修，不将静态 SQL 字节保持称为真实权限验收；真正 SCREEN 仍 OFF，原 AGE042 其它 PARTIAL 缺项及发布门槛继续保留。


## 2026-10-06 接续：原申请决定的本机待核实引用

本接续补原 AGE-042 的审阅消费者，保留此前设置、Inbox、Session 返回前复验与中文占位证据，不改变原 Request/Tie/Conversation 权限或新增服务批准账本。适用 UX-CHECK-04 至 12、16。

- 原审阅页默认复用现有 `FlutterSecureStorage` 具体适配器。API 环境、本人和同一 Request 分区，仅保存本地 `referenceID`、原 Request/action/scope/direction 与有限观察/创建/到期时间；不保存 token、Session、正文、昵称、照片或批准。`referenceID` 不是服务端 decision operationID，不发给原 API，也不是操作成功凭据。
- 仍由本人审阅并确认当前申请。提交前先落盘，再检查原捕获身份、source/transport/API 环境、工作范围与页面代际，重读同 ID 的当前原申请及内容/期限。返回前后、同步通知和异步等待导致的 ABA 永久退役旧页面；本机恢复读取 helper 完成后，caller 在原 GET 之前再复验，不能发退休页面的旧请求。
- 网络、5xx、坏格式成功、一般 400/409/422、身份或权限失败等无法确认的响应保留引用。关闭重开会先读本机引用，之后仅用原 GET 列表核实当前状态，禁止任何新决定 POST；pending/accepted/expired、读取失败和最近 LIMIT 100 列表缺项，都不等于本次因果回执或权威 NO_EFFECT。当前原 API 没有决定 operation key 或读取原决定因果结果的接口，本切片不能补全这种能力。
- 只有原 POST 的严格同 Request/动作/原字段成功 DTO 可显示本次回执，并比较删除准确引用。已知尚未发出 POST 的内容变化或读失败可清理该准确本机引用；原 HTTP 中明确在提交前产生的 `invalid_decision`（400）与 `connection_conflict`（409）拒绝码可清理对应引用。状态码本身、未知/非闭集/坏格式码均不能这样处理，原源码没有可供本路径使用的 422 拒绝码。
- 原 `messageWriteResponse` 是提交后的响应 Session 复验接口，错误映射允许 `message_policy_changed`（409）；该捕获接口的单元覆盖保持 UNKNOWN。**当前实际 PostgreSQL `ValidateHumanSocialResponse` 仅返回 Unauthorized/Unavailable，本次没有运行或复现原生提交后 409**。不能把合成响应端口的 409 当原生数据库结果。
- 读/写/清理失败、损坏或归属不符的本机引用均阻止新决定。清理必须匹配同一原 reference/action/内容；不同引用不能被旧结果删除。同 isolate 的适配器序列化不是平台跨进程原子 CAS，真实多窗口与跨进程竞争未验证。重开不恢复旧批准，不自动接受、建立第二套 Tie/Chat 或发送消息；真实 Agent SCREEN 继续 OFF。

相关 controller/widget 与实际 SecureStorage 适配器插件 mock 单元、首次关闭重开 RED、异步 handler 时序误判与同步 send 核验、所有命令/目录/退出码、原失败日志及轻量源差异见 [本机恢复证据](../testing/evidence/message-request-review-operation-recovery-2026-10-06/README.md)。插件 mock 与对象重建不等于真实平台安全存储或 App 进程重启验收。原 098/真实 PostgreSQL/授权 HTTP/并发锁、51/81/139 旧大组、全量分析/构建/真机/生产身份均未在本切片运行；AGE-042 继续 PARTIAL，Closed Pilot / Consumer Beta 仍 NO。


## 2026-10-07 增量：原人工申请决定的操作关联与结果核实

原 AGE042 的决定沿同一 Request/Tie/Conversation writer 执行，SCREEN仍仅人工审阅，模型/出网/自动接受维持关闭。服务 key 绑定本人 + 原 Request + 原 action 摘要；Session、参与角色、当前路由策略与来源先验，权限先于私有绑定差异。原 accepted friend 仅原 Tie，原 conversation scope 仅原 Conversation；无自动打开聊天或发送消息。新回执不是许可、恢复批准、最新关系状态或聊天授权。

当前真实 ReviewPage 显式选择 v2。controller default legacy 仅保原调用合约；没有其它活跃决定入口（原 Inbox `_decide` 进入此 Page，未调用的旧 `ConnectionSource.decide` 保留旧版本）。当前 API/103不可用必须保留待核实，不自动退回无 key。v1 journal 只本地引用，不升级为服务 operation；v2 的 referenceID 仍本地，operationID 单独关联服务结果，两者都不授予权限。

写前 journal → 原最新 Request 再读/具体预览复验 → 原 keyed POST。未知后重开只查询同一服务 operation，服务当前列表/同状态/LIMIT100缺项均不是因果回执；错主体、迟到、Source/账号 ABA永久退役，不能清新引用。只有完整匹配的服务 COMMITTED/NO_EFFECT 才 exact compare-clear 原 v2 引用；无自动重发、旧确认恢复或自动 winner。一般 HTTP409也不是拒绝凭证，独立操作绑定冲突不进入旧 `connection_conflict` 清理。已发写不可取消或声称回滚。

本切片相关单元通过仅证明 registered handler/严格 wire parser/现消费者/原 native Tx 代码在 scripted SQL 下的执行。103未执行；SQL NULL、text resourceID cast、同 Request 与审计 XID guard/down仅 UNIT_STATIC。真实 Session、数据库 Commit/restart/concurrency/锁序和跨进程存储、手机与OS、完整 SCREEN与所有五族隐私/发布能力未验，整个 AGE042 仍 PARTIAL，Closed Pilot/Beta NO。
