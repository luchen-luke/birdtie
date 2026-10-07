# BT-V4-NOW-006 审计与交付记录

## 实际缺口与复用

原033 ContextGraph 已有具体 Person 声明与 typed Relation；Now001 已有有限 City/本人 ONLINE 查询，但没有明确 current/destination/history 查看选择、短期原生来源密封核验、中文选择页与切换状态约束。此次复用原声明/City/Community 和 current Session 真源，没有重做 CRUD或公开/模型许可。

独占7 Go源、6 Dart源：nowcontextselection/model、PG reader/native tests、registered HTTP/errors/native tests；API/controller/page和三测试。ROOT唯一维护 shared routes/main/MapWorkspace 与整体队列/报告。界面主要简体中文，按 GLOBAL-UX-INTERACTION-CONTRACT.md UX-CHECK-01..16执行；可复查规则在 architecture/now-context-selection-v4.md。

## 原始失败与修复

- compile1：root registration 尚未落的临时帧；compile2：测试诊断变量 shadow fixture，f.p/f.a编译失败；compile4：http.Handler错误类型转换。均保留原日志，修 fixture/接口，不删除权限断言。
- native1：read unavailable，真实 SQL42601 Community 条件多余右括号；仅修语法。native2 23PASS、native3 registered 25PASS。native4增加真实 Community/无City源/Block/expiry与真实 OS process restart后34PASS。
- client-analyze1：Dart构造 super参数冲突；client-analyze2/3：curly braces lint。原日志保留，最终 analyze4=0。
- root实际审查网络无限pending：client-timeout-red1 为真实1FAIL（GET在13秒仍未终止）；新增12秒GET/POST timeout，API两方法、controller未知结果清选择/迟到忽略/无重发绿色。target3 15功能+3loading。

## 可复现目标命令

从 work/v4-now006-context-switch/prepare-frame.py 按实际当前源复制独占执行帧，再用同目录 verify-native.py native4。runner的 commands.json 记录真实参数/工作目录；目标为 go test ./internal/nowcontextselection ./internal/postgres ./internal/httpapi -run '^(TestNowSelection)' -count=1 -json，随后同三包 go vet/build。每轮真实001–083 +3原seed和既有 retained数据；每个修改fixture另 ownedMigrationDatabase隔离真实子库。NoDDL，完整 parent public/catalog字节相等并实际DROP；无跳过、没有把fixture当 production来源。

客户端在 apps/client：flutter analyze 六个 now_context_selection产品/tests文件；flutter test 三个同名tests --reporter json。原始日志 client-analyze4.log/client-target3.jsonl 和 freeze1.json 逐 SHA对应。timeout RED原文完整保存；其他正负 native Wait/ABA 没有为测试通过放宽。

## 正负验收覆盖

真实两个Person与两City/current/destination/past/private ONLINE/机构，与外国私密声明隔离；direct Gateway GET/Resolve/Revalidate全public域行零变化；Context/声明合法ABA/CityABA/metadata/Agent/Session/跨人/receipt伪造/source隐藏；最后Session锁等待后revocation、自然idle expiry、Agent退休、SourceABA、City隐藏；真实Community PRIVATE interest声明/当前公开源、hidden/expiry/block、无City来源、真实等待后source期限和Block；OS进程新key旧token拒绝。

registeredHTTP true mux/Options→Resolve/rawJSON，未知/多余/重复/尾部body与query拒绝、org/biz拒绝、nilgate503、响应前真实source变化拒绝；JSON→严格Dart actual wire。

客户端局部选择不触发query、明确resolve、组织/匿名中文占位、同keytransport pending退休、workspace/account/tokenABA、父build通知、source到期、unknown与12s timeout、late结果、320×640/font3/IME220/48dp/语义。宿主具体 Task/Result/Pin/Camera/身份/Ties保留与fresh submit集成属于ROOT验收范围。

## 分类与未测

独占实现及目标 CODE_LOCAL 证据已齐、13源冻结交ROOT核验。原任务 DONE 由ROOT按全仓/宿主和其运行证据决定，不由worker自行改队列。当前快照 full/build/设备未由worker运行；TalkBack未运行。原资料/current SQL最终观察不等于永久授权。未真实生产 IdP、没有外部服务或部署，Closed Pilot/Consumer Beta NO。
