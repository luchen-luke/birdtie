# BirdTie未完需求续接与实际验收

2026-10-10；用户明确要求继续既有开发。正式仓库D:/Project/birdtie，保留所有既有未提交修改；D:/BirdTie旧目录未改。本报告只说明当前增量，完整Consumer Beta/Closed Pilot仍NO，不擅自部署。

## 第一项：商家公开更新和定时汇总

实际修复了客户端不能识别`business_update`、公开更新误入私有经营权页以及设置仍显示“无独立来源”的问题。公开详情使用当前Read回执中的`targetBusinessId`；Inbox ID、decision `resourceId`和Business ID各自保持，不依赖客户端猜测。普通、立即和已投递摘要可读取，pending/SILENT/BLOCK与混合目标拒绝。旧账号、组织ABA、来源替换及迟到回执不会复活页面。

生产修改文件：

- apps/client/lib/src/workspace/remote_inbox_source.dart
- apps/client/lib/src/workspace/inbox.dart
- apps/client/lib/src/workspace/notification_policy_controller.dart

测试修改文件：两份business_update inbox Flutter测试，notification_schedule_delivery_test.go及business_update_notifications_native_integration_test.go。本轮后端生产代码/DDL没有变化；复用原106公开发布producer及099调度器。

实际检查：

| 检查 | 结果与限制 |
| --- | --- |
| 相关Go单元 | 210个不同叶PASS |
| 原生PostgreSQL | 25个不同叶PASS；实际001–116迁移、自有测试库、并发/幂等/撤权 |
| 注册NORMAL及DIGEST HTTP | 各1叶PASS；DIGEST实际未来due→投递→Read→公开详情 |
| Flutter相关10文件 | 64PASS；digest-final-02 |
| 最后直接消费2文件 | 17PASS；与64重叠，不相加；digest-visual-final |
| 修改的5个Dart文件analyze | 0问题，exit0；digest-visual-final |
| Android Debug构建 | PASS/exit0，239个生产Dart输入所在构建帧稳定；APK01 SHA256 ae22ac28eae145f19d2527d4facf98ff696590a4d239d58153b57b6ef33505af；未安装到手机 |
| 真机通知、OS推送、真实IdP/商户/运营、AT/性能、实际IMMEDIATE HTTP | NOT_RUN |

Go合计237个不同叶PASS，0FAIL/SKIP；不是全仓Go suite。三座本轮随机自有测试库已核实清理，手机库未迁移。组件测试读取实际native DTO原字节，MockClient传输不能替代生产身份或真机。

组件截图（320dp，保留系统文字缩放）：[收件箱100%浅色](../testing/evidence/unfinished-completion-2026-10-10/digest-visual-final/widget-screens/inbox-native-digest-light-100.png)、[收件箱200%深色](../testing/evidence/unfinished-completion-2026-10-10/digest-visual-final/widget-screens/inbox-native-digest-dark-200.png)、[公开商家100%浅色](../testing/evidence/unfinished-completion-2026-10-10/digest-visual-final/widget-screens/business-native-digest-light-100.png)、[公开商家200%深色](../testing/evidence/unfinished-completion-2026-10-10/digest-visual-final/widget-screens/business-native-digest-dark-200.png)。字体为测试专用现有Windows中文字体和Flutter图标，未更换应用字体。200%内容自然换行和滚动，不视为真机视觉验收。

失败与修复：修改前2PASS/4FAIL；新截图夹具在fake async区域异步读文件导致中断，原日志保留，改同步读并使用runAsync导出截图；64项测试通过时analyze仍有2条括号info，修正后直接17PASS/analyze0。首次截图未加载测试图标字体，最终截图补载Flutter已有图标；旧截图不作为最终视觉证据。没有把中断或初次失败计为通过。

## 第二项：实际AI不可用链路

旧真实402/403004错误被Now泛化成500，已沿原Tencent适配器、真实Gateway和个人Now接口修复为受控503/ai_service_unavailable；客户端说明服务不可用、保留旧对话，并移除原样重试按钮。取消、截止、权限、预算、current、wire和settlement失败优先；每条组合错误独立核对可信HTTP来源，未靠错误字符串猜测。原UNKNOWN成本及结算顺序保留。

生产5文件：modelgateway/tencent_failure.go、modelgateway/live_gateway.go、httpapi/agent_workspace.go、remote_agent_task_source.dart、agent_request_failure.dart；测试5文件，精确SHA和差异见ai-service/final-manifest.json。

最终同源：Go180不同叶PASS（124模型/4纯单元native wrapper/52注册HTTP），Flutter32PASS，4份Dart analyze0，10文件diff check0。12张320dp组件截图覆盖100/130/200及浅/深色；组件传输与字体仍为测试环境，不代替真实模型可用性验收。原供应商完整响应包NOT_CAPTURED，新测试为根据旧真实receipt合成的最小HTTP响应，记录了初次失败和修复。

API01构建PASS/SHA4808dcad0525c2da2ebd803a3a110f20805670c676ef433624631ca4bb15b5bc；Android APK02构建PASS/SHA1fcf0ac717460e7647512aebe759be25d133e1643cb05e5aeb200caef96cc4ba。构建源码稳定。本地preview仅替换apiExe/apiSha256/apiReceipt三个构建字段，旧模型/预算/数据配置保留；APK02以install -r安装，未清除应用数据，未部署生产。

实际手机最近对话恢复原餐厅答复/卡片，城市选择与地点详情可读，空输入框展开键盘后仍可达。100%浅色原始截图及构建身份位于phone/after-apk02-*.png/.json；本组前图为原APK04，后图为APK02；字体规范在APK04已落地，本次图片核对升级回归，不伪称本轮又完成一次字体变化。地图视口不完全相同，不能作逐像素地图对照。后续大字号/窄屏矩阵单独记录。

另对已知个人Task数据库只读核对：4条消息、两组15地点v2快照及历史/缓存fingerprint与API06阶段完全一致，modelRuns仍10、缓存15地点，没有新增模型调用。此Task与手机guest最近对话是独立证据，不强行绑定；快照采集时间2026-10-10 06:23，当前尚未跨24h新鲜度期限，本次不能宣称真机已验证过期恢复。

真实手机503分支、真实模型恢复/成功回答质量、结构化Answer/Candidate/Tool生产闭环、IdP与生产验收仍NOT_RUN或未完成。AIR009保持PARTIAL及原三项provider门禁；新增付费调用0，不把异常文案修复写成模型智能恢复。

## 第三项：报名未知结果恢复

已完成AIR038本人人类RSVP本地子阶段。发送前保存原operationId；丢响应、404或坏回包不当作失败重报，关闭重开只GET原回执。另行确认“停止提交”只永久关闭这个编号，不取消已有报名。原历史回执与当前报名状态分开，旧JOIN回执不会覆盖后续CANCEL。

生产修改：9份Dart（activity_detail_sheet及5调用者，新增operation/secure pending store/controller）；5份Go（原activity_participations/server及新增domain/store/HTTP）；117 up/down两份迁移仅在本轮自有测试库执行。四端口保持旧无编号API兼容，不启用Agent自动写。具体SHA/差异与原native wire见rsvp/backend/acceptance.json及root-checkpoint/MANIFEST.json。

最终同源验证：Go90不同叶PASS（64单位+26原生PG/注册HTTP），Flutter9相关测试文件90PASS，14份Dart scoped analyze0，diff check0。map_workspace仍有20条既有info/warning，未声称全应用分析0。真实socket丢回复/新连接池GET、同键100并发/锁等待撤权、immutable原历史、停止与执行两种锁顺序、117空down/reapply及used-down拒绝都实测；九座随机自有库清理通过。客户端消费实际原生HTTP回包，传输仍MockClient，安全存储仍插件mock。

API02构建PASS/SHA d4a478f1883821fd0679bd379f4ae74529344807d5e46f9c03948fddca2d5722（555输入/553生产Go）；APK03 PASS/SHA76b1bc59f040dd4956fd7013a1cde1046de2f92482e58f149d1425a149048f4e（283输入/242生产Dart）。均未激活或安装，手机仍API01/APK02，无手机117迁移。

组件视觉12PNG，320dp×720，中文长内容/混排、100/130/200%与浅深色；[未核实状态100%](D:/Project/birdtie/docs/testing/evidence/unfinished-completion-2026-10-10/rsvp/client/final-09/widget-screens/rsvp-unknown-light-100.png)、[停止确认200%深色](D:/Project/birdtie/docs/testing/evidence/unfinished-completion-2026-10-10/rsvp/client/final-09/widget-screens/rsvp-stop-confirm-dark-200.png)。本轮沿已确认文字规范：页面20/w600、面板18/w600、卡片16/w500、回答15/w400、输入16、辅助13，正文行高1.5；浅色语义20242A/374151/566273，深色独立语义，现有字体/系统缩放保留。测试字体不替换生产字体。

已补齐当前API01/APK02的27张真实手机原PNG/XML/构建哈希，包含窄320dp下100/130/200%浅色与200%深色的Now回答/城市/卡片/详情/空IME及滚动补图，最后恢复手机原字号1.0/浅色/density520。大字号可换行滚动；主要按钮完整显示时>=48dp。内层EditText的40.13dp不是整个输入区触控尺寸，Mapbox attribution17.84dp为保留的观察例外。只验证截图覆盖状态；静态图不能验证地图闪烁/帧率，其他页面/全部15卡片/全图对比度审计NOT_RUN。详情[200%深色键盘](D:/Project/birdtie/docs/testing/evidence/unfinished-completion-2026-10-10/phone/matrix-apk02-narrow320-200-dark-keyboard.png)，27图清单phone/matrix-worker-apk02-manifest.json。此真实手机组不包含新RSVP功能。

保留初次路由404 RED、客户端旧fixture断言/认证缺项、截图未含Navigator overlay及新时序fixtureUTF8错误等失败。最后final-09才是90PASS/analyze0；截图导出边界已覆盖真实确认dialog。已修复来源/transport替换期间旧busy阻塞，并由实际点击及迟到响应测试覆盖。

真机RSVP/真实IdP/OS安全存储跨进程/正式运营恢复/获批Agent工具适配器/全仓和发布NOT_RUN或未完成。AIR038整体PARTIAL，未把本地人类链写成完整Agent自动执行。

### 同批复核后的最终报名客户端

独立复核发现初版的“停止请求根本未抵达服务器”恢复死路，已实际修复。重新打开只GET；只有新的独立人工确认才可再次发送同编号close，不会重报、换键或取消报名，旧pending对象确认被拒绝。新增两个真实客户端反例失败后修复；最后reclose-final-04为93PASS、14 scoped analyze0、diff0，取代此前90项作为本轮最终客户端结果，计数不能相加。新变量写入相邻测试导致的final02编译失败已纠正并保留日志。

16张320dp组件截图包含长中文、混排、100/130/200%浅深色及再次停止确认；[再次确认200%深色](D:/Project/birdtie/docs/testing/evidence/unfinished-completion-2026-10-10/rsvp/client/reclose-final-04/widget-screens/rsvp-reclose-confirm-dark-200.png)。RSVP retry构建01（实际路径rsvp/client-build-01，独立于此前APK编号）SHA28eec12504719ba327caf2f61aab69213ddd072bda9a76af7e6a492f7deeddb8，283输入/242生产Dart稳定、PASS、未安装；之前APK03属初版历史构建。117/原API02的11份后端源码逐SHA保持，本次只修改客户端，未重新跑Go/native迁移，不把之前90Go说成本次重跑。手机仍原API01/APK02，真机报名恢复NOT_RUN。最终证据rsvp/reclose/root-checkpoint/MANIFEST.json。



### 2026-10-10 同批报名恢复地址兼容收口

实际复现同一个API地址末尾有无“/”导致本机UNKNOWN恢复记录分裂：HTTP请求地址一致，但地图/对话入口或新界面可能读不到原编号并重新创建提交。三项cross-entry反例全部失败后，controller/Memory/Secure store统一同一HTTP base规范；只去掉单个末尾斜线，不合并不同主机拼写/端口/path，不接受凭据/query/fragment或双末尾斜线。

原两个地址命名空间均兼读，读取不迁移不删旧值；只有完整binding一致才能合并，stop=true优先。新写入仅canonical；终态清除先删所有stop=false再删stop=true，同状态canonical最后，任一步失败立即保留未知状态。冲突、损坏、写入或部分清除失败均阻止新执行，恢复仍只GET原编号。Secure原同isolate串行队列保留，不声称OS跨进程CAS。Memory本轮新static Future队列导致完整widget回归等待未结束，日志按INTERRUPTED保留；删除该多余队列，校验和Map操作无await，未禁用或弱化测试。

最终namespace-final-02：9个相关Flutter测试文件106PASS/0FAIL/SKIP、14份scoped analyze0、注册15源SHA稳定、diff0；此前36直接PASS是同组子集和历史字节，不能相加。16张320dp组件PNG延续已确认规范，root实际看100%浅色UNKNOWN及200%深色再次停止dialog。独立只读复核四源/原日志SHA及状态规则无阻断。RSVP retry构建02 SHA0e506699a858ca4f0d66e3a39f811532ef2f9929177fb2dae1a118916a2f6555（283输入/242生产Dart）PASS、完整构建帧稳定；包含本轮消息请求时间解析和地址修复，未安装。

原117及报名后端11源逐SHA保持，本地址阶段未跑Go/迁移；共享EntityAction此前四态阶段已经改两处SQL，不能称旧90Go为当前全部后端重跑。手机仍API01/APK02，没有117/新报名路由，未将新客户端直接装入不匹配后端。真机报名/IdP/OS安全存储重启与跨进程CAS/获批Agent效果工具/全应用视觉、性能和发布NOT_RUN或未完成；AIR038整体PARTIAL，Consumer Beta/Closed Pilot仍NO。新增付费调用0、未部署、未生成ZIP。最终证据rsvp/namespace/root-checkpoint/MANIFEST.json。


## 第四项：消息请求四态本地验收完成

原AGE042的ALLOW/REQUEST/SCREEN/BLOCK已完成登记本地范围。SCREEN保留原申请人工待审，模型筛查OFF。修复本人GET/PUT/Decision返回前源/期限/Session代际保护、Block→Unblock旧摘要复活，以及Session撤销再恢复后的旧具体确认仍实际201提交。共享EntityAction仅两处SQL纳入原111代际，真实正常登录续期仍可提交；无新schema/权限系统。

Go最终101原生PG/注册HTTP检查及82相关单位共183不同叶PASS/0FAIL/SKIP，同一稳定源码。098空down/reapply/CAS并发/事务audit回滚/旧rows-xmin-catalog保留/used-down拒绝、六原写入口与专用会话ACL/等待/ABA实测；本轮自有parent+3child已精确清理，3历史数据库身份不变。原103完整因果验收保留历史，本次仅sameRequest keyed accept原回包与消费，未完整重跑103。

实际原SCREEN申请回包带+08时间被客户端误判坏数据，已修严格RFC3339解析。最终Dart final-04为82PASS/源稳定/新native-wire测试analyze0/diff0；只有新测试分析0，不能称全应用分析0。4张320dp组件图加载现有Windows中文测试字体与Flutter图标，已实际看图：[100%浅色](D:/Project/birdtie/docs/testing/evidence/unfinished-completion-2026-10-10/message-policy/client/final-04/widget-screens/policy-native-screen-light-100.png)、[200%深色及可达操作](D:/Project/birdtie/docs/testing/evidence/unfinished-completion-2026-10-10/message-policy/client/final-04/widget-screens/policy-native-refresh-dark-200.png)。final03测试字体缺Roboto导致方框，保留而不作视觉通过；生产字体未换。

修改5份Go生产（model、own Store、Decision Store、HTTP、共享EntityAction）及1份Dart生产connections；精确源/测试/原失败/命令见backend/manifest.json、client/final-04/receipt.json及root-checkpoint/MANIFEST.json。新API/APK构建、消息请求真机/IdP/OS跨进程CAS/完整应用视觉与性能/模型SCREEN/正式运营部署NOT_RUN。此前API01/APK02真机证据是其构建的Now矩阵，不能算本阶段手机验收。

## 队列与边界

起点256项：180DONE、76未闭合（47TODO/15PARTIAL/14BLOCKED）。摘要本地任务完成后：181DONE、75未闭合（47TODO/13PARTIAL/14BLOCKED/1IN_PROGRESS）。消息四态本地任务完成后182DONE、74未闭合（47TODO/13PARTIAL/14BLOCKED）。当前状态以唯一automation/codex_task_queue.json为准，下一原任务接续时状态分布可变化。任务DONE只是其登记范围，不能由此推断整应用或137条原始需求全部验收。

本轮不新增付费模型调用、不修改外部服务、不联系他人、不发布、不生成ZIP。旧Tencent/DeepSeek真实服务受限及未知现金保持原记录；没有原供应商response packet，后续测试若合成最小402响应必须明确其来源和限制。


### 最后唯一队列核对与剩余缺口

本批最后validate通过：256个工作项中182DONE、47TODO、13PARTIAL、14BLOCKED，共74未闭合，其中29为P0；不是74个已知故障，也不是消费者完成率。47TODO中45的原前置尚未全DONE，另2个是Post-Pilot（A2A协调、原生预约）。实际next --parallel无候选且active=0；普通next仍返回Post-Pilot A2A，不能写成所有模式都无候选，原试点后阶段不自动放行。

主要剩余范围是实际模型及Answer/Candidate/Tool闭环、记忆证据与置信/来源冲突、图片媒体出口和视觉、获批Agent写工具完整恢复、全页面/地图性能/辅助功能/真实端到端，以及正式登录、授权数据和运营试点。14BLOCKED不都属于外部账号：SAF002原“Follow不存在”理由已陈旧，FOL001及该项三个依赖现均DONE；但普通Profile仍private/public，Agent字段策略没有FOLLOWERS/CLOSE，Social Intent亦没有这两档，原Close本人管理与移除不存在，五档服务器矩阵仍真实未完成。本次仅只读核对，不以Friend/grant代替Close，不因旧依赖完成就改DONE；具体实现和矩阵验收仍需原任务接续。

最后队列原快照、精确74/29计数、原TODO前置及Post-Pilot分类、实际源SHA保存在final-queue-audit/receipt.json。该审计不改生产代码或队列、不启动新测试；本轮通过的各组局部检查和27张原API01/APK02真机图不能推断上述剩余范围通过。未运行的真机报名/新消息策略、IdP/OS跨进程、全应用视觉/性能和生产验收保持NOT_RUN，完整消费者验收NO。


## 第五项：原SAF002五档权限矩阵与Close

原任务BT-V4-SAF-002已按登记的五档服务器权限矩阵及本地验收DONE。普通真人Profile和Intent受众、本人明确Close选择及撤销、原字段ACL与grant交集、会话和来源ABA/到期保护已实际实现。不是完整消费者/Agent模型/Memory/试点验收。合同与完整证据分别见 [五档合同](../architecture/RELATIONSHIP-VISIBILITY-MATRIX-V4.md) 与 [本地验收](../testing/evidence/unfinished-completion-2026-10-10/visibility/README.md)。

实际补齐public/followers/connections/close/private，Close绑定当前真实好友Tie及accepted request，移除/屏蔽/重加好友/恢复账号不复活旧选择。资料读取使用原生来源和会话证明，编码后撤权不返回旧正文；授权到期不变数据库行的反例已修。Profile与账号类型并发修改的非法关系受众亦由真实锁等待反例修复。原Profile、联系人和Intent入口已消费；父路由换身份/传输时旧Close确认退休，未知写结果只GET核对。

验收：Go348不同叶PASS，190单位+158真实PG/注册HTTP；Flutter130+7新增原始回包消费共137，后续31API回归含24旧项不能重复累加。22份Dart scoped analyze0，新测试1份另analyze0；不是全应用分析。gofmt空输出，scoped diff-check exit0。精确原始命令、版本帧、失败和SHA见visibility/README及root检查点。

28张320×720生产widget图，100/130/200%浅色与200%深色，长中文混排及可滚动确认后果；[受众200%浅色](../testing/evidence/unfinished-completion-2026-10-10/visibility/ui/batch-final-02/widget-screens/profile-audience-picker-200-light.png)、[Close100%](../testing/evidence/unfinished-completion-2026-10-10/visibility/ui/batch-final-02/widget-screens/close-list-top-100-light.png)、[确认200%深色](../testing/evidence/unfinished-completion-2026-10-10/visibility/ui/batch-final-02/widget-screens/close-confirm-consequence-200-dark.png)。沿已确认文字规范/颜色/系统缩放及48px主要触控，未修改Now地图/输入框/键盘规则。这些是测试字体和MockClient下实际widget，新增Close真机/前后构建对照NOT_RUN；不复用旧手机图宣称新功能通过。

1父库+18子库精确OID/owner/marker验证并清理，3座旧库只读身份保持；手机和共享DB未迁移。Close GET也持有关系表锁，其延迟/吞吐未验。当前手机未连接；新功能真机、正式IdP、OS重启/AT、全应用视觉与地图性能、full suite/build/install、真实模型成功质量、生产部署/运营均NOT_RUN。付费调用0。

本检查点唯一队列256项：183DONE、47TODO、13PARTIAL、13BLOCKED，73未闭合，28P0。本期仅原SAF002闭合，其他定义、依赖和Gate逐一与注册前一致；73不是73个已知故障或消费者完成率。独立只读复核未找到可直接全闭合的P0，仍有AIR020 Memory风暴原生压力及AIR024 SelfReview组合原生路径可继续本地验证；这两项未因此改DONE。模型实际可用性、合法校准/到场证据、正式身份/授权供给/真实用户E2E及整应用设备质量仍缺。Closed Pilot与Consumer Beta NO。


## 第六项：记忆原生控制与个人复盘会话修复

继续原AIR020与AIR024，未新增队列。实际修复ContextBuilder在会话撤销再恢复后仍返回旧个人复盘数据的问题：原真实PG/注册HTTP反例200包含旧合成私有内容；现在绑定原111会话授权代次，开始、结束及最终公开来源校验一致。普通仍有效的idle续期和实际Authenticate续期继续通过。只改agent_context_builder.go一份生产Go，无新schema、模型、授权或Agent自动效果。

当前同源相关Go共273不同叶PASS：161单位、112真实PG/注册HTTP。完整Memory原生11，新增SelfReview原生22，原ContextBuilder及SelfReview回归130，domain91，Memory单位19；各组无重复，不把父事件或RED/早期green再相加。最初会话ABA失败日志完整保留。gofmt无输出、scoped diff-check exit0；不是全仓测试。加入会话代次后，修复前生成的旧上下文许可可能被拒绝，需要本人重新预览并批准；不会自动恢复许可。

Memory实际验证全局4/本人1配额与五主体公平、真实突发、有界root6与深度1、501份原批准上下文的分段撤销、不产生自己的MemoryUpdated循环、来源乱序/ABA/历史重放、原人工撤回事务锁等待、两次原30秒期限自然失效，以及真实OS进程退出后原维护CLI恢复和已确认回执丢失后的历史读取。最后一项不是网络unknown COMMIT。仅追加原Memory测试并保留原two-stage正文；Memory生产代码未改。

SelfReview实际组合原注册路由与Store/Service：同快照的明确双方冲突保留等待本人确认、未选来源不泄露、跨主体拒绝、Profile/Memory/Owner/Agent/Policy和Session编码后改变或ABA、来源及原会话到期、真实锁等待越过原期限、正常续期。连接池重开只称持久化，不称OS重启；真实OS恢复仅属上述Memory测试。

原001–119迁移和模块保持。新自有1父库与8子库按精确OID/owner/marker检查清理，remaining=[]；3座旧库只读身份不变，手机与共享DB未迁移。仅维护CLI02构建用于原生OS恢复（SHA b6ae64d867790909c274043cf6317d03300b787a03e77fc7367a5c4ce29999b2）；API/APK应用构建、安装、真机、IdP、外部模型成功/质量、媒体/GPS/外部多来源、网络unknown COMMIT、全应用性能/辅助技术及生产部署均NOT_RUN或未完成。本轮后端无UI修改，无新增截图，之前widget/真机图不能作为本次新行为验收。

AIR020整体仍PARTIAL：通用13类认知事件完整调度与跨事件合并、运营负载和相关未知结果尚未完成；AIR024整体仍PARTIAL：媒体/GPS/外部证据、本人冲突确认完整链与真实端到端等未完成。唯一队列保持256项、183DONE/47TODO/13PARTIAL/13BLOCKED，73未闭合，其中28P0；这次完成的是两项内的验证与修复，不是假关闭整项。Closed Pilot与Consumer Beta NO。证据见 [后端检查点](../testing/evidence/unfinished-completion-2026-10-10/backend-native-checkpoint/root/README.md)。


## 第七项：当前手机配对集成、实际恢复与并发修复

已接续原AIR038，完成本地真机检查点并安装匹配构建。最终API03 SHA `3a883eceaa3c8e4615fe094da3b0fc34d4774ac2e8d0d25907a9d1c4ce2119c5`，APK04 SHA `02ad4d8ca4d94e629b70a86e7ae509d97eb81348354745908dcd73093ed0dce9`；API01–02/APK01–03为历史阶段，不混作当前构建。独立审核核对最终8份生产/6份测试及其他初始输入、3个原忽略配置哈希；原dirty字节保留，没有新任务、ZIP、付费调用或部署。完整文件/命令/截图/失败分类见 [手机检查点](../testing/evidence/unfinished-completion-2026-10-10/phone-integration/README.md) 与 root-checkpoint/MANIFEST.json。

本期实际修复收藏活动入口缺失、Plans把正常会话续期误判来源改变、Close/详情/两种父入口在主题字号重建后误判身份变化，以及旧Authenticate锁等待后的早期时钟覆写。最后一项真实3叶RED复现普通并发续期增代次和自然800ms到期后错误续期；现在复用原Account-before-Session锁后PG时钟resolver，原来源/ACL/代次/期限/组织/借用客户端守卫不放宽。Go生产为activity_plans_current.go及identity.go；Dart生产为saved_items.dart、activity_plans.dart、map_workspace.dart、public_intent_section.dart、connections.dart、chat_entity_router.dart。测试为原plans原生测试追加、新participations原生测试，以及4份新导航/Close/RSVP/父路由widget测试。精确SHA及原字节见source-final02与before-fix。

最终相关自动检查91不同叶PASS：66 Flutter（9文件单次最终调用）+25原生PostgreSQL（包括先前19，不重复累加）。真实API03手机在100/130/200%条件三次打开我的活动，共12四路并行GET全200，已报名状态可见。gofmt两份认证相关文件无差异、scoped diff-check exit0；新增RSVP文件scoped analyze0，三个原活动文件仍有20条info及1既有warning，不声称全应用分析0。不是全仓或发布测试。原失败、helper守卫错误和初次fixture/编译/异步区日志保留，最后通过结果另列。

报名已实测已提交201回复丢失→UNKNOWN→Android force-stop/start换进程→同编号一次GET恢复已报名；执行POST始终一次，实际生产Secure store/plugin，无mock存储或原controller复用。另一请求在到达服务器前丢失，APK04 install-r保留原journal；GET404保持UNKNOWN，200%深色本人确认停止，原同编号close200，原participant仍null；未重报、换键或取消报名。最终API03再GET原两种终态验证持久化，未重复故障注入。OS跨进程CAS/Windows重启和完整Agent写工具链仍NOT_RUN。

五档Profile真实手机保存后原生核对、Close加入/撤销、REQUEST/SCREEN/BLOCK设置均已核对；SCREEN仍只人工待审。200%深色消息确认的版本、有效期、后果末尾通过滚动补图，最后返回修改取消，没有再保存，原revision4保持。全部使用明确标注的本地合成dev-phone与活动，不验证短信/手机号所有权、不替代真实用户端到端。

原访客餐厅对话在升级、合成账号退出和实际进程重启后仍可打开；两家餐厅显示相同卡片，最终城市实际GET200并可打开选择。没有新提交餐厅查询或联网采集；原公开库15资料/1查询缓存仍存在，model_request_runs保持10无新增。此轮验证升级/进程恢复和旧缓存消费，不把当天几小时当长期验收；超过24小时真机历史、全部15卡片、新联网推荐成功/餐厅质量NOT_RUN。旧‘无法读取城市’历史发生原因没有原故障包，不能仅据本地模式断言；当前原API未覆盖117，本期仅自有副本迁移并配对，USB转发移除实测可自动恢复。

文字沿已确认规范：页面20/w600/1.4、面板18/w600/1.5、卡片16/w500/1.5、正文/Agent15/w400/1.5、输入16/w400、按钮14/w500、菜单15/w500、辅助13/w400；浅色20242A/374151/566273，深色独立，保留原MiSansVF/Roboto及系统TextScaler，无全页面缩放。本轮是已统一字体的集成回归，非新字体替换。Now/城市/IME100/130/200%浅深色与窄320dp、大字号活动/消息确认已拍真实原PNG/XML。主要可见操作>=48dp；100%内层EditText40dp，实际原opaque中央min56dp留白上下两次点击仍聚焦唯一输入，分别保存原图与位置，不伪称内层节点48dp。输入在键盘上方，200%深色关闭后回原位置；静态截图不能验证地图闪烁/AT，长中文输入编辑及全应用视觉/合成背景对比度/连续性能仍NOT_RUN。

相同手机/浅色/字号100%升级对照：[Now前](../testing/evidence/unfinished-completion-2026-10-10/phone-integration/phone/before-history-now-100-light.png)／[最终Now](../testing/evidence/unfinished-completion-2026-10-10/phone-integration/phone/apk04-api03-final-original-guest-history-after-os-restart-100-light.png)，[城市前](../testing/evidence/unfinished-completion-2026-10-10/phone-integration/phone/before-city-100-light.png)／[最终城市](../testing/evidence/unfinished-completion-2026-10-10/phone-integration/phone/apk04-api03-final-city-after-guest-os-restart-100-light.png)。地图视口不保证像素一致，不能当新字号前后图。所有帧构建归属、部分文字视口、初期两个误名UNKNOWN和stop标识活动实际‘报名参加’均如实分类。

原共享库只SELECT/pg_dump，无117–119迁移或fixture写入；仅精确归属/OID/marker核对的自有手机副本迁移。原临时PG子库和失败副本已精确清理，当前副本保留QA。只核对指定计数/表名指纹，不声明完整DDL/私有全行一致。外部模型/公开采集OFF，本地agent_enrichment标志保留，不宣称全部Agent关闭。故障代理按精确身份/源哈希/端口关闭，手机直连当前18112；本地QA watcher只对验证匹配的APK/API恢复18091转发，已实测移除后自动恢复，物理拔线和Windows自启/重启NOT_RUN。手机已恢复原默认字号1.0/浅色/density520，合成账号退出、原访客对话前台。

AIR038整体仍PARTIAL；本批补齐人类真机恢复，不是假关闭完整获批Agent工具任务。收口validate通过且root lease释放，唯一256项：183DONE/47TODO/13PARTIAL/13BLOCKED，73未闭合/28P0；其他任务定义/依赖/Gate逐项不变，73不是73个已知故障。实际模型成功/质量、Agent完整闭环、真实IdP/授权供给、全页面/地图性能/AT/完整E2E与正式运营仍缺；Closed Pilot/Consumer Beta NO。
