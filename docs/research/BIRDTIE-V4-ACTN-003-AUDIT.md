# BT-V4-ACTN-003 共同历史增量审计

2026-10-05。本项15精确scope见 `work/v5-age038-resume/e2e002-partial-actn003-start-proof38cb.json`。只复用原领域与原GET shared-context，不建历史聚合台账，不改变好友/报名/社群/认知授权。

## 已有与缺口

048三开关默认关闭；双方开关、另一Person公开资料、Block、当前原Tie/Community membership/going参与共同事实已有。旧 reader 使用 RepeatableRead、owner-only鉴权且HTTP直接编码；缺编码后原Session/完整候选源版本/截止复核，缺活动关联公开Place。旧面板绑定late-final client并只对目标ID刷新，缺同keytransport/getter/listener/workspace ABA与借用client关闭边界。

## 本轮最小计划

1. 原Signals保留旧字段；加当前活动日期/时区/报名事实、活动关联地点、截断与native观察/截止元数据。attendance/visit始终UNKNOWN。
2. 新CurrentStore实际Session+当前双方权限/完整相关源metadata-xmin闭包，当前RC语义与PG统一时间；构建同statement数据/版本，编码后再次核验；旧能力不足503，不回落旧reader。
3. 原Panel中文消费，严格原生DTO，借用client零关闭、owned关闭一次、当前listener/transport/target/token/workspace epoch永久作废旧响应；PublicPerson原入口传既有identity/workspace。
4. 隔离frozen085（原PLN880+已冻结E2E新test+本项overlay）native正负/源ABA/真实等候/自然expiry/零域写与完整public/catalog/xmin，Widget含320大字/48dp/异常/迟到，适用UX-CHECK-01/02/03/05/06/07/09/10/11/12/13/14/15/16。OBS086移动live不称验收源。

## 精确事实语义

双方going是共同报名记录，已结束不证明到场。地点仅来自同一获准当前公开活动关联的当前公开Place；不称共同到访，不读定位/私密情境/成员地址/Memory。原048目的允许人类查看，不是匿名078公开报名许可或任何机器/模型/A2A出口批准。关闭/隐藏后不继续展示旧历史正文。

## 当前结果

最终目标证据：native7 55 Test PASS / 0 FAIL-SKIP、test/vet/build及两CLI build exit0；883 Go/SQL/mod +3原seed字节核验，frozen085 + 已冻结E2E002 + own7Go overlay，非OBS移动live。完整旧public行、可见7类semantic catalog、085 unused down/reapply、原Participation xmin均保留，owned parent/child清理成功。Flutter四目标（含原Person/051页面）19 PASS，4文件analyze exit0；源冻结见 work/v4-actn003-history/source-freeze2.json。联合当前086全量/Build由root尚待核验。外部生产/真人/真机/TalkBack门槛保持，Closed Pilot / Consumer Beta NO。仅已收到通知的ABA可立即在UI退休；未通知的身份变化仍由请求捕获与原生最终许可复核限制，不能称UI能侦测不可见变化。


## 失败保留与真实因果

- 初次根cwd误用于apps/api相对路径、client-analyze2根cwd用于client相对路径，是HARNESS路径失败，原日志保留。
- client-analyze1 11 lint、analyze4新test 1 braces lint均真实FAIL；最终analyze5 0。
- native4 48PASS/4FAIL（3叶+父）：新ONLINE/TBD/unspecified fixture仍带PlaceID违反原042 activity_physical_place_consistent 23514。改成真实合法NULL Place/Venue组合及unknown/not_applicable，不弱化原CHECK/业务断言。native6全通过。
- native5-red-block-aba 0PASS/1FAIL：真实原BlockAccount→UnblockAccount后旧capture callback返回nil。这是实际实现缺口，不是波动。新完整frame加入相关原personal_safety/account block/unblock审计id+xmin；native6/7原同一断言GREEN。无新ledger/DDL/写权限。
- client-target3/4：新原公开Person接线fixture的profile Response缺实际UTF8 JSON Content-Type，原页面正确拒绝资料，historyGET=0。补与实际native一致UTF8 JSON header后target5/6 19PASS。原target3/4 raw保留，不改页面权限/期待。

## 覆盖与分类

REAL CODE_LOCAL：双方defaultOFF、原无城市社群、共同going+Place关联/日期、GET原域不写；当前完整source/session/natural期限/Filtered source/隐私与ABA/真实关系锁等待；未知schema/主体/到场/visit伪值/闭包缺失503；原注册GET输出前真实撤权拒绝；source隐藏/两向Block/取消/普通Session闲置与原截止；原048和051原生兼容。真实registered HTTP native3原样JSON经Dart严格parser测试；不是手写DTO冒充原生wire。

CLIENT CODE_LOCAL：原PublicPerson真实注入identity/workspace；samekey transport A→B→A、listener账号/工作区ABA、迟到响应、过期清除、during-build通知、借用client0close、320/font3/IME260/48dp/UTC+原IANA标签。原普通朋友、分享、私信直接路径保留。 

NOT_RUN：本项新真机/UI截图、TalkBack/真实读屏、生产IdP/真实两人关系/合作方授权活动/正式部署；完整当前联合Flutter analyze/test/build及Go086依root收据另核。本项不生成attendance事实；ACTN002真实到场权威仍独立阻塞，不拿going替代。未运行RACE（CGO0/noGCC）。

本项仅建议 root 按真实code/local AC核验，不自行改队列DONE。Closed Pilot / Consumer Beta NO保持。


## 2026-10-05 客户端相对租期修复

根代理复核发现真实客户端时钟/网络租期缺口：旧面板用服务端 validUntil 减设备 DateTime.now，并从接收时新建最多30秒 Timer。设备时间落后时能延长短来源租期，迟到响应也可能首次展示。原生Session/来源守护不是此缺口原因。

修复只在原Panel和对应Widget test：请求发送前启动 Stopwatch，接收/解析后以 `min(validUntil-observedAt-requestElapsed, validUntil-deviceNow)` 计算剩余期限。前者在严格DTO上限30秒内；设备墙钟只能收紧，不能续期。耗尽不展示，不自动请求新许可；身份epoch/借用client边界不变。

真实 RED：client-lease-red2 0PASS/2FAIL，服务器时刻领先设备5分钟、源lease300ms；600ms后旧数据仍在，runAsync实际网络延迟600ms后也首次展示。未修改设备/宿主/PG时钟。首次red1仅cwd错误造成0tests/exit79，单列HARNESS保留。修复后四目标 client-lease-green1 21PASS/0FAIL；四文件analyze0。第一例验证相对短lease Timer，第二例使用真实单调等待而非fake时钟延迟。未称真机/读屏已验证。

新的source-freeze3与worker-final2独立保存；原freeze2、19PASS、1319文件档案及根314源1243功能/150loading构建帧保留为修前历史。7Go字节不变，本次未运行Go或改API/数据库。根当前新版全Flutter/build另行验证，不用旧构建证明此差异。


## 2026-10-05 最终根代理独立本地核验

按根代理原始收据更新本轮终态，不覆盖此前 PENDING、实际RED或不可变档案。

- `work/v5-age038-resume/full-actn003-lease-client38cx/result.json`：全部314 Dart/pubspec前后字节稳定；1245功能测试、150 loading事件 PASS，0 FAIL/SKIP；Flutter analyze/test/Debug build 均exit0。此范围不包含全部平台工具链字节。
- `work/v5-age038-resume/joint-final-archives38cy.json`：旧 worker-final1 的1319文件/58,605,791 bytes与新worker-final2的21文件/159,605 bytes逐字节/SHA独立核对，无多余文件；11个当前source与freeze3一致，7Go与此前独立native帧一致。该较早收据中 wholeGo087 是 RUNNING，保留原文。
- 后续 `work/v5-age038-resume/joint087-whole-root-proof38cz.json`：joint087全Go真实10345 PASS、0 FAIL/SKIP、packageFAIL0，test/vet/build及两CLI共5命令exit0；全部895执行输入与live逐byte/SHA一致（892 Go/SQL/mod+3seed）；完整原public行/可见semantic catalog/原Participation xmin/087 unused down-reapply保留，自有数据库经根独立确认不存在。原runner895输入对892末扫描的计数差异保留诊断，不是源码漂移。
- 新不可变Debug APK：SHA256 `75c36833f0ceca38c47c1e42ccc51307855abf7d75f2284785f04398ec2a5b90`，242,893,717 bytes，位于 `work/v5-age038-resume/full-actn003-lease-client38cx/artifacts/app-debug.apk`。尚未安装，不能以旧APK界面或本次构建证明新Panel真机效果。

本轮代码和隔离本地检查已完成根核验；任务状态只由根代理更新。本项新增共同信息真机正向、该APK安装、TalkBack/读屏、Profile受控比较、真实两人/授权供给均NOT_RUN；RACE为CGO0/noGCC未运行。此前旧OBS全仓真实失败和独立Run UNKNOWN保留，不以本轮绿色抹去或宣称已修复未知因果。Closed Pilot Ready / Consumer Beta：NO。
