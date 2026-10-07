# ACTN001 本地动作契约审计

2026-10-04；来源：原队列 BT-V4-ACTN-001、V4 原产品规范，复用现有 Birdtie 原生实现。当前 IN_PROGRESS，不能将阶段测试称完整任务验收。Closed Pilot / Consumer Beta：NO。

## 当前缺口及实施次序

原验收：每类实体按当前用户、权限、状态声明可用动作，UI 与 Agent 共用合同。已有详情/分享引用、FriendTie、Chat、RSVP、Save、Plans、Community 权威动作。原卡片使用回调存在/type/旧数组推断按钮，没有统一六动作合同；获邀私密 Activity 可读，但原收藏仅接受公开 Activity，不能扩大许可。

1. 闭集 CONNECT/MESSAGE/SHARE/JOIN/SAVE/NAVIGATE，服务端当前来源与会话读取；描述只供用户提出动作，绝不作为提交授权。
2. 注册只读 `/v1/me/entity-actions/{entityType}/{entityID}`，编码后原生末核；会话、来源版本/ABA/期限不满足时拒绝。
3. 同一合同接入原生七类结果生产者，再连接结果卡与原领域详情。Opportunity 使用原 Activity 目标，不能公开其私密候选 ID/Intent/理由。
4. 直接详情与规则 Agent 复用合同/原写入/现有确认；未知结果查权威状态，不盲重发。PlaceDetail 与 MapWorkspace 尚未转交，本 worker 不写；完整任务验收须根代理协调最后集成。

不增 DDL、机器 grant、第二账本、通用 execute、模型调用或默认开启自动动作。对应 UX-CHECK-04 至 12、14，中文和 48dp/大字/IME；AT 与真机由根代理另记，未测不称通过。

## 阶段真实记录

- native1：测试 fixture 把 PrincipalRef 当 Actor，编译失败；修 fixture 的真实 Actor，不调整授权。
- native2：SQL 实际引用不存在的 Tie.scope；改复用 accepted Friend request 与原排序 pair。
- native3：Community source.title 列别名缺失；补准确别名。
- native4：独占 fresh083 4PASS/0FAIL-SKIP，test/vet/build0，全 public/catalog 保留、ownedDB DROP。
- native5：5PASS/1FAIL；registered GET 与权限案例通过，最后 POST405 使用 private-resource helper 对 mux 的 no-store 要求不适用。保留该失败，改用真实 mux 请求独立断言405，未新增写路由/未放宽任何领域检查。
- 初 gofmt 工作目录与相对路径不一致、一次 PowerShell 内 Python 引号解析失败均发生在运行前，不计测试通过。
- 结果面板 light/dark 原表面硬编码：两项真实 RED 后最小 Theme ColorScheme 替换，两项 GREEN；不是全 UI 改造。采用 impeccable 的 Operate/现有语义主题规则，界面布局、Task/选择不变。

所有日志位于 `work/v4-actn001-20261004`，各轮保留；当前没有完整全仓/本机/生产验收结论。

## 2026-10-05 当前增量验收（早期 scope 描述为历史）

根已逐项正式移交 Map/Place、原领域 bound writer、Router、Plans controller 等精确范围。只有 root 管队列与共用报告；本文件不执行 DONE。API freeze1/native32：31 owned Go/853全源稳定、65PASS，根对应 wholeGo10147PASS；随后发现真实匿名 AVAILABLE/EXPORT_PUBLIC reason 仍声称“不支持”，pure operation-reason-red2 实际失败。最小文案修复后 native33 为66PASS/0FAIL-SKIP、test/vet/build0/public+catalog相同/ownedDROP，API freeze2 receipt SHA ea4e6f808f71944621292a5953cb6afd762f7386d75e68830d540f9c7c2f2287。根完整 freeze2 Go 回归另记，旧10147只属于旧帧。

### 原生红绿与边界

- native11 两类真实缺陷：cityless Community 收藏的原 writer 不可用，却提案可用；Person COMMUNITY 字段公开依赖目标成员/社区元数据 ABA 未闭合。原失败保留后补原域条件/闭包。
- native18 原 Activity 目标锁等待后费用改稿，effects=1 为真实 RED；原 bound 同事务保护后 native20/21绿。SQL42601 的16/17只作诊断，未冒充授权 RED。
- native24/25 原 CONNECT housekeeping 使合法到期 pending 的新申请 ErrChanged；修为精确原到期行过期变更，并保留其它 request/社区/来源闭包。native26绿；native27真实 INSERT/FK等待后目标社区成员/元数据ABA和自然到期拒，申请/审计/通知/Inbox/outbox 原行与 xmin 相同。
- 原 OPEN_CHAT/私信申请的当前 Tie/Intent/City/会话/current Session 同事务边界、原 ID positive 与真实等待负例保留 native28–33；29旧条件第二次打开的预期错误按真实首次会话 source变化更正为“旧拒绝/新 descriptor 原ID复用”，不删除会话来源。
- 匿名实际原30秒和City自然期限、hidden/coarse/noPoint/sourceABA/pool等待为 native15 证据；receipt/seal不出 wire；sourceVersion/deadline是有界公开并发条件。

### 客户端红绿及实际入口

- 同源 widget rebuild 旧 dispatcher busy丢失的实际 RED 后保留原实例/fence；取消0写、未知一次、不盲 retry。
- Activity费用变更与 Place/Activity point A→B：真实先失败后闭合详情/版本读取，0报名/外部导航；保留首次失败。
- Person transport同keyA→B→A旧批准真实 POST1；修 transport epoch 后绿；另 getter/监听同值替换真实 person-getter-red2 POST1，修统一 epoch 退休后负例。
- Now真实留言 dialog 在320×640/font3/IME260 下 contact-layout-red7实际 overflow；前1–6仅夹有fixture/工作目录诊断，不计产品 RED。原 dialog scrollable 最小修复，检查/取消可达48dp，0写；不以共享确认对话测试代替留言入口。
- typed Activity 原私人提醒入口丢失实际 reminder-red2；独立提醒按钮复用原 Plans POST/DELETE 原ID，0RSVP。Plans写迟到A→B→A的 reminder-red1实际额外 GET calls2；generation/token防旧结果新身份重读。原收藏不可见清理不需要读取已隐藏内容。
- target11–18失败全部保留：旧系统分享 fixture 缺明确 EXPORT_PUBLIC/最终确认；新fixture未选对象/constructor错误；主题测试误将 RawTooltip 当 IconButton；MESSAGE原操作无需第二确认，真实开会话后测试须实际返回释放等待原 NavigatorFuture。不会将诊断都当授权缺陷或通过。

最终Dart/整包/真机以新 receipt 与根证据为准。当前只本地合成资料；不宣称真实双方用户、生产、AT/原生暗地图或性能仪器完成。


## 最终本线冻结（2026-10-05）

API冻2 `ea4e6f808f71944621292a5953cb6afd762f7386d75e68830d540f9c7c2f2287` / native33 66 PASS；客户端40已租源冻1 `7894c489cc4dee1a53edb80cac75e0ab6433bcc6138e39527d1e8b66c876dad8` / target20 140 PASS、整包 analyze7 0。target19视觉disabled假设FAIL保留，最终实际点击旧批准0POST（不声称即时disabled）；root最终整包与真机待核。具体原入口和ownership-only私人提醒/隐藏收藏例外见专属 `CLIENT-AND-ENTRY-MATRIX-2026-10-05.md`。


## 根整包38af社群入口回归增量

完整客户端真实2FAIL已保留并在旧test重新RED；新增scope仅原community_lifecycle_entry_test。原源闭合校验正确拒绝错误Mock，不改生产源。公开/私密本人撤回/接受/拒绝三操作明确确认与取消0写，原三header/空body/ID/Auth均验证，13测试GREEN；target21合计153功能PASS。详见专属 COMMUNITY-LIFECYCLE-COMPATIBILITY-2026-10-05.md。冻1/worker-final1保持不可变，新41Dart冻2和差分档案后续独立交付。

最终client冻2 / target22 153 PASS、analyze9=0，原40Dart与31Go字节相同，仅新增生命周期test更新。旧38af和worker-final1不覆写；root按新41Dart执行整包/真机，新本线不自行DONE。


## 正常取消提示实际RED→GREEN

安全点授权仅dispatcher+test：按钮/系统Back/barrier三类误Snackbar真实3FAIL，保留原字节和red1；最小记录review关闭结果后仅抑制正常取消generic错误，权限/source/unknown四类仍显示原提示，11精确目标PASS。最终target23共160功能PASS/analyze10=0，41Dart冻3保持；Go31/API冻2不动，完整旧archive1/2不覆写。详见专属 CANCEL-FEEDBACK-2026-10-05.md。


## 真机匿名恢复文案精确修正

Settings缺登录而个人资料实际有本地测试登录，root移交Card+test仅改指引句。原期望对旧source真实1FAIL/3PASS→新copy4PASS；22target164功能PASS/analyze11=0，43Dart冻4原41字节全同，Go31不动。根最终构建/真机后再评任务，不自动扩大IdP/模型/真实pilot；详见专属ANONYMOUS-RECOVERY-GUIDANCE-2026-10-05.md。
