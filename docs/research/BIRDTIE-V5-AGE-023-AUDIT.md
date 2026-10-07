# BT-V5-AGE-023 审计

日期：2026-10-03。范围：历史 Moment 的发生时间；本人声明，不是现实到访、活动出席或身份核验。任务状态由根代理在唯一队列维护。

## 原生能力与缺口

- `internal/content/moment.go` 与迁移 006 已有 nullable `occurredAt`、闭集 unknown/year/month/day/instant、独立 `createdAt`/`updatedAt` 与 revision。原 registered HTTP POST/GET/PUT/DELETE 和原 PostgreSQL Store 可以保存历史时间；无需新 DDL。
- 原 Flutter controller 未解析三个时间字段，创建/编辑固定 unknown，`isEditableDraft` 只准编辑 unknown，真实历史记录在客户端不可编辑。
- 新输入粗精度按 UTC 日历锚点序列化；未修改的已有历史值保留原精确 timestamp，不以锚点推断实际日子。准确时刻输入和显示标注 UTC，不猜设备或地点时区。
- createdAt 是服务端保存时间；未知发生时间不会回填 createdAt。064 outbox 与 005 Evidence 的源事件时间继续原定义，不改为历史发生时间。Memory/授权/模型端口没有因时间输入激活。

## 权限漏洞与实际修复

`native-red3` 在真实 registered HTTP/native PG 路径证实：持 Moment row lock → HTTP 已认证且等待 UPDATE → 另一连接 revoke 或 expire → 解锁，两路旧实现均 HTTP 200，并产生动态/audit/outbox 效果。根据真实 RED 扩为 17 个精确 scope 后修复；无 server.go 或 DDL 修改。

注册五路入口现在传递首认证的 session digest + trusted Actor 至 `content.HumanMomentStore`；接口缺失直接 503，没有 owner-ID legacy fallback。新 PostgreSQL 网关复用原 Moment/context/audit/outbox 同事务 helper，以当前 active PERSON Account SHARE、本人资源和等待后 Session SHARE、数据库最终 clock_timestamp 核查提交。GET/List 最后一条 payload SQL 也校验 current session；不提前锁 Session 来阻止撤权测试。没有 Agent/Profile 仍可普通本人 CRUD，不创建第二身份或授予 cognition。

锁图复核又发现 Candidate 先 metadata UPDATE 后 Moment SHARE，而 Human Update 先 Moment 后原 outbox metadata SHARE。`native-lock-red4` 真实两领域路径出现数据库 deadlocks 0→1。修复是 Human 网关先对已存在、精确匹配的本人 PersonalAgent/PERSON metadata 取得兼容 SHARE，再等 Moment；没有绑定仍正常人类 CRUD。最终真实 barrier 测试要求候选接受与 Human 编辑均成功，deadlocks 不增加；没有修改 Candidate/outbox 真源。

扩大 natural-expiry 矩阵时 fixture 两次 clock_timestamp 分别设置 expires/idle_expires，偶发微秒差违反 idle<=expires；保留 SQL 失败。改为 MATERIALIZED 单一数据库时钟同时赋两字段，继续等待真实过期，没有增容忍或绕过 future/expiry 检查。UTC year 检查改在转换 UTC 后进行，offset 转换产生 year0 的输入真实拒绝。Withdraw 改用 url.ParseQuery，malformed escape/分号/重复 revision/额外 owner 均 400、零 native 调用。

## 当前真实证据

- Flutter `flutter-red1`: target11 PASS、analyze7 info/exit1，保留原日志；`flutter-green1` target11 PASS/analyze0；`flutter-red2` target12 PASS（目录名保留，没有失败）；冻结的 `flutter-green2` target13 PASS/analyze0。真实页面及组件的小屏、大字、键盘、Tab、语义、账号切换/异步/版本保护覆盖见原日志。
- `native-red1`: 6 PASS/1 fail event，未使用 import 的 compile/vet 失败；`native-red2` 12 PASS/4 fail，包含真实 revoke RED 与 expiry fixture 约束失败；`native-red3` 12 PASS/4 fail，合法 fixture 后 revoke/expiry 两路均真实漏洞。
- `native-green1`: 24 PASS/0 fail，首次会话修复通过；`native-lock-red1` 31 PASS/1 fail 为新测试未使用 import；`native-lock-red2` 39 PASS/2 fail 为 barrier 对软排队阻塞链观察不足；`native-lock-red3` 39 PASS/2 fail 为真实并发失败；`native-lock-red4` 37 PASS/5 fail 为真实 deadlock 加 fixture SQL 失败。
- `native-green2`: 164 PASS/5 fail，保留原 fixture 及修复后旧 barrier 假设失败；`native-green3`: 167 PASS/0 fail，锁图与单时钟 fixture 修复通过。green3 早于最终 strict query 修改，其 SHA 不作为最终帧。
- **当前冻结帧 `native-green4` / `native-green5` 两轮各167 PASS/0 fail/0 skip，historical matrix31，HTTP31/PG136；各轮 test/vet/build exit0，完整400 Go 与7 owned SHA稳定、完整旧 public 行集合相等、owned DB DROP。** 根独立 `root-independent1` 同 SHA 同167/0/0且同样通过；三轮原始结果分别归档，不合并为虚拟轮次。

所有 PASS 为 go test JSON 的具名测试/子测试事件计数，不含 package PASS；fail events 保留 package 失败事件。每轮 fresh001–064+3 dev seeds、独占随机库，064 outbox/controls 的 fixture 由所属 Account/Agent 级联回收，完整 public 行比较不只抽数表。原测试包含 MomentContext、Outbox、EnrichmentNative 和 Candidate100 replay；wiring spy 只证明接口无 legacy fallback，不能代替 native 权限证明。

## UX 与限制

对应 GLOBAL-UX-INTERACTION-CONTRACT 的 UX-CHECK-01/02/04–14/16；03 是现有结构化表单；15 独立真实用户观察不由合成 fixture 代替。五精度单一选择后显示对应字段；中文错误可恢复，键盘/语义/大字测试是 widget 证据，不能冒充手机 screenshot。根报告同 SHA 完整 Flutter analyze/test/debug build exit0、安装真实设备及五精度/非法日期取消/1.6大字，手机与最终 API/App 重启原始证据由根归档并另附引用。

账号变化清除旧列表/errors/state，表单监听原 auth 与 controller；旧具体记录对象/版本拒绝重新提交，CAS 409不自动重试，关闭后迟到响应不恢复。创建 API 没有 idempotency key，网络响应丢失不能证明未保存；客户端阻止直接再次 POST，并明确要求用户检查列表后批准新建。此恢复状态仅本次 controller 生命周期，不声称跨重启 exactly-once。

## 复现与限制

在仓库根运行 `& ./work/v5-age023/verify.ps1 -Round <新的字母数字连字符目录名>`。脚本使用当前本地 Docker PostgreSQL，独占创建/删除随机库，拒绝覆盖旧轮次；target 为 `Test(HistoricalMoment|HumanMomentHTTP|MomentContext|AgentOutbox|AgentEnrichmentNative|MemoryCandidateNative)`，随后对 postgres/httpapi vet/build。Flutter 在 apps/client 跑三份 private_moment_context_test/moments_historical_test/moment_time_choice_test 与 analyze；完整 build/全Go/手机由根协调，避免共享 Gradle cache 并发。

没有运行 race（CGO_ENABLED=0）；真实锁 barrier 是独立证明，不等同 race detector。本人历史时间不自动进入长期 Memory、不证明到访/出席、不授予模型分析或 source-egress；Business dormant。worker 本地 fresh064 与根 current065/手机证据应分别记录。原 Closed Pilot/Consumer Beta 门槛保持，未部署外部服务或使用真实 IdP。
