# Historical Moment 时间 V5

2026-10-03。对应 BT-V5-AGE-023。本人历史声明复用现有 Moment；不证明现实到访、活动出席、身份、兴趣稳定性或推理授权。不新增表、身份、Memory/许可账本或模型端口。

## 时间语义

| 项目 | 原生字段 | 语义 |
| --- | --- | --- |
| Experience 发生时间 | Moment.occurredAt / moments.occurred_at | 本人选择的历史时间；未知为 null |
| 记得的范围 | timePrecision | unknown、year、month、day、instant 闭集 |
| 服务端创建时间 | createdAt / created_at | 此资源最初保存时刻，编辑不修改 |
| 当前源更新时间 | updatedAt / revision | 编辑版本与源失效依据 |
| 原 enrichment/outbox 事件时钟 | 原 064 occurred_at、005 EventTime | 原生创建/更新控制事件的服务端时刻，保持原定义 |

原需求所说 event_time/created_at 分离在 Moment API 中分别是 occurredAt/createdAt。不会把 2026 年创建的 2025 年声明理解为 2026 年 Experience；也不会把 outbox 服务端事件时钟倒移到 2025。

新客户端粗精度输入按 UTC 日历锚点序列化：year 年首、month 月首、day 当日零时。锚点只表达精度，不声明真实月份、日期或时刻。已有未改的 coarse 值保留原 timestamp；显示只取 UTC 年/月/日，不按设备时区移日。instant 明确 UTC，可保留六位微秒。未知不会以 createdAt、当前日期、设备地点或最近记录回填。非法日历日期、无时区 wire、超出 UTC year1–9999 拒绝；原 HTTP/数据库未来24小时界限继续校验。

## 用户入口与草稿

侧栏“个人资料”→“我的 Birdtie”→“新建动态”；现有设置个人资料入口也保留。列表“编辑或撤回”现在允许所有五种精度的私人草稿。时间选择先选范围，再显示一个对应输入，默认未知，不预填今天。创建时间独立显示为 UTC。其他 Now/Map 内容与旧草稿关联组件保持。

客户端解析 occurredAt、createdAt、updatedAt；原字段与关联 ID 不另造名称/来源。编辑默认保留原 time value，明确选“不确定”才清为 unknown。当前 actor、已加载对象与具体 revision 绑定；刷新后旧对象须重新打开，原 API CAS 409不自动重试。账号切换隐藏旧列表与打开表单；旧请求、错误和保存状态不能恢复到新账号；controller dispose 后迟到响应丢弃。

保存响应丢失表示“结果尚未确认”，原 POST 不具 idempotency key，不能宣称未保存或跨重启 exactly-once。当前 controller 阻止直接再次创建；返回列表检查后，由用户明确选择“已检查，仍要新建”。没有自动重发、后台通知、Memory 写入或对外发布。

## 当前普通人类权限网关

已注册 HTTP 五路入口复用原 `/v1/me/moments` 和 `{id}` 路径，但必须使用 `content.HumanMomentStore`，传入首轮真实 Authenticate 的 session digest + Actor；owner-ID legacy Store 不再是 HTTP 回退。只有当前 active PERSON，本人资源与本人具体版本，组织 workspace header、客户端主体/verified/用途选择字段与额外查询拒绝。Withdraw 仅接受一个正 revision，url.ParseQuery 错误、重复值或额外 selector 均拒绝，不能吞 malformed escape 后继续调用 native store。

PostgreSQL 新网关沿用原 Moment/context/audit/outbox 的同一事务 helper：

1. ReadCommitted + SET LOCAL TIME ZONE UTC；锁 current active PERSON Account FOR SHARE。
2. 若已有 exact 本人 active PersonalAgent 与 PERSON metadata，取得兼容 SHARE 再等 Moment，避免 Candidate metadata→Moment 与旧 outbox Moment→metadata 锁环。没有 Agent/Profile 仍正常 human CRUD；不会创建它们或取得认知许可。
3. resource 锁等待后锁 current Session，校验 digest、account、撤销、expires/idle expires、dev_phone 服务器开关；不在 Moment 等待前锁 Session。
4. 读取最后 payload SELECT 仍带 current session gate；写入、关联、审计、outbox 后用真实数据库 clock_timestamp 最终复核，再同事务提交。撤销/自然过期/取消拒绝后不返回私人 payload、不保留任一效果。

并发有效性以实际数据库锁序与最终检查为边界。已持有 final Session SHARE 的事务与随后等待的撤销按数据库串行顺序完成；不声称已完成动作可被后来撤销追溯取消。

原 owner-ID 方法保留给原受信内部调用/既有测试，注册人类入口无 fallback。普通 self Moment 不继承 profile_view、好友/社群/组织角色、到店或 Feature ON 的模型授权；真正模型/cognition/current purpose 出口维持既有 Unavailable，Business dormant。

## UX 与证据边界

适用 GLOBAL-UX-INTERACTION-CONTRACT UX-CHECK-01/02/04–14/16；03 对应现有结构化表单；15 真实用户观察不由 mock widget 代替。worker Flutter target13PASS/analyze0；实际页面360px/1.6大字编辑与账号切换、组件320px/2.0大字/键盘/Tab/语义/校验恢复。根已报告同SHA完整Flutter analyze/test/Debug build、安装及五种精度真机保存/编辑，正式截图/重启证据由根归档，未取得路径前不把报告当此文自验。

实际原生证据与首次失败见 [审计](../research/BIRDTIE-V5-AGE-023-AUDIT.md) 和专属 `docs/testing/evidence/historical-moment-time-2026-10-03`。native-red3 已证撤销/过期后旧 HTTP200且产生效果；锁环 native-lock-red4 的数据库deadlocks 0→1。保留所有原始失败，不用 mock wiring 或只编译结果替代真实授权/并发证明。最终冻结帧 native-green4/green5 两轮各 target167/0fail/0skip（historical matrix31），各轮 vet/build0、完整400 Go和7owned SHA稳定、完整 public 行集合相等、自有库 DROP；根独立 root-independent1 同 SHA167/0/0并通过同条件。完整Go与手机当前API由根协调，最终收据另附。

Closed Pilot/Consumer Beta 不由这些本地合成声明、测试会话或 Debug APK自动通过；原发布门槛继续。
