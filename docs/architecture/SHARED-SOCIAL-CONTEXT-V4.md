# V4 共同关系与情境授权

`BT-V4-TIE-003` 的信号来源仅为活跃双方好友 Tie、活跃社群成员关系和实际 `going` 报名记录。本人城市、学校、线上兴趣声明仍私密，不参与公开共同情境推导。

自然人可通过本人设置分别允许展示共同好友数量、共同公开社群、共同报名的公开活动。三个开关默认关闭；设置明确说明会向同样允许展示的已登录用户提供对应共同信息。关闭后后续读取立即停止展示。开关不改变个人资料、活动或社群的访问权限。

查询要求活跃 Person 会话、另一位活跃且公开资料的 Person，以及双方没有 Block。共同好友只计双方活跃 Tie 的交集，且候选好友自身公开、允许共同好友计数、未屏蔽双方；不返回好友名单。共同社群要求双方允许、均为活跃成员，且社群活跃、公开、已发布。共同活动要求双方允许、均为 `going`、活动公开已发布且未取消/过期，并通过既有活动授权函数。私密/隐藏社群、成员/邀请活动、pending/cancelled 报名均不展示。公开对象不等于成员/报名默认公开。

API：`GET/PUT /v1/me/social-disclosure` 只读写本人三个布尔开关；`GET /v1/accounts/{accountID}/shared-context` 返回授权后的 `mutualCount`、`communities`、`activities`。列表最多 50 项并明确 `communitiesTruncated/activitiesTruncated`，零值表示“没有可展示的信息”，不能推断用户没有实际关系。所有响应禁止缓存。设置写入审计事件；不存在关系回填或自动身份验证。

这些设置不是对真实用户的默认同意。正式环境、实际身份和用户验收仍按独立发布门槛处理。


## 2026-10-05 ACTN-003 当前共同报名与活动地点投影

这是原 GET 的原生人类只读增量，不新增历史台账、许可真源、报名或消息。原048两侧三类展示开关仍默认关闭；不是051本人Agent取回许可、078匿名公开报名声明或任何033/AGE/AIR分析、模型出口批准。

- 共同报名：原双方 `going` Participation + 当前公开、已发布、未取消/未失效、各方仍可见的 Activity，附原 startsAt/endsAt/timeZone/modality。结束不代表到场；`attendance=UNKNOWN`。取消一侧报名后不再投影该活动。
- 共同公开社群：原双方活跃成员与当前公开社群，支持原合法无城市社群；若绑定城市则该城市必须仍公开且未过期。
- 活动关联公开地点：仅前述活动原明确 `IN_PERSON/HYBRID` + `physical_place_status=confirmed` 所关联、同城且仍公开有效的 Place，返回 id/title/活动IDs。ONLINE/TBD/legacy unknown 不作为已确认实体地点。地点关联不是到访；`visit=UNKNOWN`。不返回精确坐标、个人定位/轨迹、成员地址。Place关联不承诺独立Venue资料仍通过审核或可预约，Venue/Candidate撤核及期限纳入版本闭包，fresh人类投影仍只可称当前公开Place关联。
- 共同好友仅原双方accepted好友Tie交集且好友本人公开/允许、无屏蔽，输出数量，永不输出名单。

原字段继续保留，additive schema=`shared-relationship-history-v1`、viewerId/targetId、PG observedAt/validUntil、places/placesTruncated。三个列表上限50，明确截断。零值不能推断没有现实关系。元数据不是读权限。

新 CurrentStore 必须提供实际原生 Session/完整相关候选版本复核，不能默默降级旧 reader（缺能力503）。Explicit ReadCommitted + source关系锁、原Account/Session行锁后，同一 SQL/MATERIALIZED native PG clock 核 Session当前有效、双方/好友/host/organizer/City/Place/原Venue等权限与源epoch，含被过滤候选。Response先编码、再实际原生重捕获，旧source/许可/到期/账号变更不输出旧正文。正常Authenticate闲置延长不改变稳定Session身份，但原读取截止不续期，最长30秒。原Block/Unblock合法absence ABA还绑定相关原personal_safety/account审计id+xmin；不存/输出审计正文，不新增授权台账。此保护依赖正常领域写入的原审计，不声称能抵抗特权删除审计、修改DB目录或网络已返回后的瞬时撤权。

中文Panel在原公开Person详情复用原identity/workspace监听；目标、client/base、getter/listener或已观测token/workspace变化同步退休旧epoch，迟到响应不得复活，组织态不发个人共同信息GET。借用client不关闭；自有client由创建者关闭。原私密界面状态不构成历史权限或机读上下文。仅已收到通知/当前请求观测到的身份ABA可立即检测；未通知的瞬时变化不能虚称能被客户端感知。

日期展示使用UTC绝对时间并明确原活动IANA时区没有换算，不套用设备中国时区或硬编码BST。刷新≥48dp；320×640、3倍字、IME260正常可滚动。适用UX-CHECK-01/02/03/05/06/07/09/10/11/12/13/14/15/16。证据在 `docs/testing/evidence/shared-relationship-history-2026-10-05/`；native/Widget通过不等于真机、TalkBack、真人或正式试点验收。


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
