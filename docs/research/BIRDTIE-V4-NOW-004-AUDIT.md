# BT-V4-NOW-004 原生七类结果增量审计

2026-10-04；owner sponsored_trust。原任务持续 IN_PROGRESS，依赖 NOW001 / ACT001 已 DONE。root 在自然安全点转交 43 范围，后追加赞助 wrapper 和 Conversation 两文件至 46；worker 不修改 live 队列、共用报告、DDL 或他人 Plans 源。

## 原缺口与实施

原七类 DTO、Dart 卡片/路由及地图已有根代理验证，但 `postgres/agent_result_projection.go` 与原生 HTTP proof 尚不存在，不能据 fixture 声称七类来源闭合。本次新增 NativeStore、同事务 current-source SQL 与 server-only seal；复用原 Task 和原领域接口，不建结果实体 ledger。

来源详见 [规范](../architecture/typed-agent-results-v4.md)。普通人类查询保留原邀请/成员 Activity 权限，机器 PUBLIC 不拓宽。Business 必须核验原070全部具体来源和072公开许可；本人私密 Opportunity 用原 humanSocialSQL/Generate、同 PG 时钟与完整私密 Activity/host/Place/授权源 xmin，分享仅原 Activity。

公开商业目标由 sealed 原生 refs 给原赞助 lane；获邀私密活动与本人机会不进入商业端口。商业审查权限在最后 pool/Session 等待后撤回、ABA 或自然到期，都拒绝旧编码响应。

## 真实失败链

原日志均在 `work/v4-now004-native`，不得将失败轮改写为通过。

| 轮次 | 实际结果与处理 |
| --- | --- |
| native-port-red1 | 原 Store 缺 NativeStore，实际失败；实施后 green1 通过 |
| native1–4 | 最小核心编译/PG prepare 暴露 actor 类型、CTE xmin；修实际代码。坐标 fixture 原本无点位，测试随后明确提供合成坐标，不加 GPS fallback |
| native5 | 8 PASS / 9 FAIL；IN_PERSON Intent 缺必需 Place、组织 reviewAt 缺失、Community 清理顺序不合法；按原领域约束修 fixture |
| native6 | 原生七类及私人机会 ABA 17 PASS |
| native7 | 13 PASS / 12 FAIL；实际 wire Items 解码目标错误、中文 parser 剥词顺序及不相关账号全局 closure 造成无关 Changed；分别修测试、parser 和限定当前城市真实源闭包 |
| native8–10 | 排序中公开活动不一定首位、041当前名不同于原claim输入、Sponsor fixture 缺真实 Context/Personal Agent；修正准确断言与原前置 fixture，不放松权限 |
| native11 | 35 PASS；商业授权三类 Session 等待与 Venue 初始五矩阵均通过 |
| native12 | 新 final barrier fixture 注入了错误 server port，44 PASS / 8 FAIL；按真实 AgentStore 参数位置修 fixture，保留旧失败 |
| native13 | 52 PASS；registered HTTP 源/Task/Agent/Session 晚核、正常 idle 更新通过 |
| native14 | 41 PASS / 2 FAIL；机会匹配合法私密 Activity，但原保存 API 仅 public，测试错误期待201；保留原404边界，不扩保存权限 |
| native15 | 53 PASS / 1 FAIL；新 Session 期限 fixture 的 SQL 别名缺 AS，修 fixture；真实 pool 源到期已经通过 |
| native16 | 56 PASS / 0 FAIL/SKIP，密封商业 refs 和比较闭集等通过 |
| opportunity-filter-red1 | 实际发现“我的社交机会今晚” silently discards filter；parser 明确拒绝，中文提示及注册 HTTP 无假结果回归 |
| native17 | 69 PASS / 2 FAIL，同一新072撤销 fixture 未递增原 revision，trigger 正确拒绝；只按原 CAS 修 fixture |
| native18 | 第一冻结帧71 PASS / 0 FAIL/SKIP/pkgFail，test/vet/build 0；847 复制源稳定、public/catalog 不变、owned DB DROP；不是当前最终源 |
| typed-answer-ui-red1 | 当前界面显示持久 Task 的“正在...”占位而非真实本轮答案；修 display-only，不改 Task 历史或授权；19 controller 目标通过 |
| typed-count-ui-red1 | native 清 legacy 后原对话计数全0；按 typed Items 七类计数，320 / font3 / IME260 通过 |
| root full38m | 根整仓发现旧授权Activity数组/赞助自然供给兼容丢失，及有效Session下Agent退役错误401；原18冻结帧、旧断言均保留，继续修实现 |
| compat-compile1 | 首编译unused time，去除后compile2通过 |
| native19 | 新完整兼容闭包首SQL误用activity_sources.id，实际为双键；29 PASS / 51 FAIL，修正确原键，无DDL |
| native20 | 76 PASS / 4 FAIL来自两个新叶子及父用例：非法Agent paused、整wire误混机器body限制。旧3组已绿；修合法retired，新增最小body断言绑定typed Items，human完整Activity description保持原允许wire。原machine与旧HTTP测试未修改 |
| native21 | 80 PASS / 0 FAIL/SKIP/pkgFail，test/vet/build0、847稳定/publiccatalog不变/DROP；原Safety persistence、Sponsored lifecycle、Task五场景均绿 |
| native22 | 最终重复80 PASS / 0 FAIL/SKIP/pkgFail，test/vet/build0、847稳定/publiccatalog不变/DROP；另验证兼容DTO篡改seal拒绝。14 Go +4 Dart源见新freeze2，旧freeze1保留 |

## 可复现命令与结论

在仓库根：

```powershell
python work/v4-now004-native/verify-native.py --round <新的独占轮名> --pattern '^(TestAgentResultProjection|TestContextBuilderHTTP|TestEntityShareNativeSevenKinds|TestAgentHTTPResponseSafetyPersistenceIntegration|TestSponsoredHTTPRegisteredLifecycleAndOrganicInvariance|TestNotificationDestinationHTTPCurrentTaskIntegration)'
```

实际解释器及命令数组保存在 native22/commands.json、进程回执及源 manifest。runner 从当前源复制新 immutable frame，独占 fresh083，原3 dev seeds 与 retained fixture；只在随机带 ownership 前缀的本地库验收和 DROP，不触共用 public 或手机库。此命令的 test/vet/build 只覆盖目标包；不是整仓 go test ./... 已通过的声明。

在 apps/api：`go test ./internal/agentresultprojection ./internal/agentworkspace -count=1 -json`，pure-final2.log 为兼容修复后的实际纯规则/契约结果；第一冻结帧173 PASS保留。

在 apps/client：`flutter test` 的11文件目标数组见 client-target-final1.log 对应根执行说明；89 PASS。`flutter analyze` 两产品/两测试，4 items / 0 issues。本轮四 Dart 源分别为 controller、Conversation 及其测试；其他既有 seven-card/map 代码保持根冻结实现。

## 验收覆盖与未测

- 七原 ID、registered POST/GET restore、原详情、明确聊天分享与同 key 回执、原保存 ACL；Opportunity 私密原候选打开/分享原 Activity，不能伪装为可公开收藏的新实体。
- 每类源 xmin ABA 与初始隐藏/双向 block/撤审/撤邀/072撤权；私密供给 Activity/host/Place/invite ABA；商业 Venue 当前性。
- 原 Task/Agent ABA、Session 撤销、正常 idle 刷新、跨账号 GET404；真实 pool 等待源到期和 Session 行等待到期。
- 最后响应 buffer 后源隐藏/撤销/ABA 不返回旧敏感标题或 ID；机器 ContextBuilder 旧 PUBLIC 上限回归通过。
- 当前末条回答/七类计数及 restore、旧回答保持，原身份/迟到/地图/IME/widget 回归89通过。新检索不自动消息/RSVP/Memory。

当前联合 whole Go/Flutter/build、新 APK 与七类真机结果由 root 独立执行，暂为 PENDING。旧10027/1098及压力截图是旧帧历史，不作为当前原生生产者证明。读屏、真实双人、真实主办方、CSSA、生产身份、真实赞助/付费/运营均 NOT_RUN；Closed Pilot Ready / Consumer Beta = NO。worker 不自行 DONE。
