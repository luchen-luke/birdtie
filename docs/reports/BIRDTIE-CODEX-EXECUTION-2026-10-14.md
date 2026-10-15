# Birdtie 执行增量 2026-10-14

执行任务单：`C:\Users\chens\Documents\Codex\2026-10-13\d-project-birdtie-docs-testing-evidence\outputs\BIRDTIE-CODEX-EXECUTION-TASKS-2026-10-14.md`。正式源码为 `D:\Project\birdtie` 当前未提交工作树，启动 HEAD `4637cfb6e7758fc4fa5dd548fad623a8484c4cfb`；没有回滚、reset/clean、覆盖旧提交或更改 `D:\BirdTie`。

本文件是持续执行记录。批次 A 和 B1/C1 已封存内部集成检查点；B2地点照片 API 已编译并接入本机 owned QA，B2/C2 客户端直接检查已完成分批验证，已合包并装入本机preview；组织邀请/位置B3已完成直接检查及API/APK/Web内部编译检查点，媒体140和集合副本141已编译接入本机owned QA，C3/B5身份保护已合包；正在推进142五类二级页控制。唯一队列仍为 `automation/codex_task_queue.json`；启动256项：183 DONE、47 TODO、12 PARTIAL、13 BLOCKED、1 IN_PROGRESS，共73项未完整闭环。父任务 `BT-FIX-NOW-UI-001` 保持 IN_PROGRESS；没有新增替代任务或重开历史 DONE。精确写范围追加于原 lease，启动及每次范围增加保留 source-before 和原队列证据。

## 批次 A：收藏和关注

|任务单差额 / 原来源|当前实现与复用|验证状态|
|---|---|---|
|A1 NOW018/021 → BT-FIX-NOW-UI-001|本人 Saved 全分页 `q/kind/cityId` 查询，服务端先筛后分页；独立明确的“仅筛选已加载内容”模式仍允许本机备注筛选。保留旧无查询 UUID 游标，新查询封闭 opaque cursor 绑定原本人/Session generation/数据库/查询及快照，实时复验来源。私人备注不进入服务端搜索、旧 Agent 投影或他人内容。|Dart直接验证通过；数据库首轮实际发现坐标大小写错误，修为 schema canonical `wgs84` 后仅重验失败2项，通过。首失败完整保留。|
|A2 原 BT-V4-FOL-001 历史来源 → 当前 UI 父任务|Now关注 Organization/Business/Community 行进入原 canonical ID 稳定详情；Community原受控 JPEG版本、SHA、当前 Session/授权/源 proof 投影到 Follow，旧 avatar_url 未自动迁移。|Follow12个不同用例分批通过，含两版本 RawImage 实际像素、迟到内容、删除/隐藏、主体 ABA 和真实续页点击；不是一次最终源12用例全运行。Go HTTP/domain及真实隔离DB Follow通过；根路由实际测试5项通过。|
|A3 NOW018/021 → BT-FIX-NOW-UI-001|Saved选中同城合法 Place → 本人已有Journey/ThemeMap草稿选择 → 原 ID/具体revision预览 → 确认尾部追加；保留原标题/顺序/备注。新条目空私人备注、hidden位置。复用原集合读取/编辑领域，新 canonical append CAS、original operation receipt、STOPPED tombstone，不用普通重读猜成功。|客户端12个不同用例分批通过，包括取消零写、普通认证续期、原公开UNKNOWN阻止选择、提交401仍保留 journal、GET404仍未知、Stop成功保留结果或STOPPED阻止迟到提交。原生DB Journey/ThemeMap CAS、去重、私有隔离、Session ABA与停止通过。|
|A4 NOW018/021 → BT-FIX-NOW-UI-001|实际可见且坐标/来源仍有效的已加载筛选结果投影到原 Now MapCanvas；canonical Place实体、原选择/视口，不建第二地图；最多4选点比较、不复制私人备注。无获准坐标真实不可用。|源模型3项通过；根路由5项含原MapCanvas/状态/输入控制器同实例及视口不变，通过。地图provider/真实手势不由mock回调证明。|

新 API `POST /v1/me/personal-collections/{kind}/{collectionID}/append-places`、原 `GET .../append-operations/{operationID}`、显式 `POST .../append-operations/{operationID}/stop`；客户端持久化仅保存本人/origin绑定的恢复选择器，不保存认证、图片、备注或可重发 body。追加136迁移独立于124/128/129旧集合和公开账本；136已在身份/OID/owner/marker校验的本机owned QA事务中应用一次，原22表计数及旧迁移字节保持，非生产；隔离子库另实际加载及空历史down/up验证。

证据根：`D:\Project\birdtie\docs\testing\evidence\remaining-execution-20261014\batch-a`。Go直接检查 `api/direct01`（11顶层、11子项，0失败0跳过）；原生首轮 `api-native01`（3顶层通过，Saved2失败）、修复后 `api-saved-native02`（Saved2通过）；失败与 source-before 均保留。客户端首次 `client-direct01`、后续定向 `client-append*`、`client-saved-page02`、`client-follow02` 与 `follow-client` 各记录真实 command/cwd/exit、源码前后 SHA 和日志，不能合并为一次成功套件。

批 A 原生证据 `api-native-proof03`组合5不同顶层通过（明确不是一次新Go调用）；旧无过滤Saved兼容2项通过。`client-analyze-a01`零error/warning、129info。Go `api-build-a01`11binary、`apk-build-a01` profile、`web-build-a01` profile42文件均exit0且sourceStable。A包：`D:\Project\birdtie\work\remaining-execution-20261014\apk-build-a01\birdtie-batch-a.apk`；Web：同work下`web-build-a01\artifact`。API18113、Web7357 ready且JS哈希匹配；手机preview包实际`install -r`保留数据，18113 reverse、fontScale1.0、前后台焦点和当前主屏PNG已核对。安装观察首失败及恢复失败保留，最终`device-a-observe03`成功；这张真实Now截图仅证明本机preview，真实多角色动作/E2E NOT_RUN。不可沿用Copy30 APK18091配置作为本批证据。

`batch-a/checkpoint/receipt.json`为不可变A封存，不随B/C源变化改写。收藏正常/键盘/空/503错误之前后隔离截图8项`client-visual-a04`通过，正常字号390×844，不是真实帐号。截图识别重复body标题后已删标题与独占间距；错误页原有“正在读取/暂无收藏”混淆，已区分读取中/未取得结果/保留旧结果并去掉失败时假空态，定向`client-visual-a05`通过。截图在`batch-a/visual-a04/png`，错误终版在`visual-a05/png`；首轮fixture编译/Material错误与CJK按钮字体修正均保留。未录屏。

## 批次 B1：活动和组织受控封面

当前UI父任务增量，复用原活动organizer/manager与组织identity owner/admin，不添加Organization Agent前置。新增137只为受控320×180 JPEG≤256KiB、权利记录及不可变original operation账本；标准化上传/删除、具体version公开预览、当前授权/源/Session generation、固定readAt最多60秒租约和原回执/STOPPED。活动/组织详情、工作台、原编辑入口、结果卡、Saved读取同一version派生图；旧图/撤权/到期/账号或target ABA退出，机器工具/model上下文剔除cover和proof。不用4张AI样例充当真实官方图，旧avatar_url未迁移。

新增`/v1/{activities|organizations}/{id}/cover`读取/PUT，`/preview`、version `/content`、`/operations/{operationId}`与`/stop`。客户端bodyless恢复journal按原origin/owner绑定，非200（含401）UNKNOWN，GET404不当成功、不盲重PUT；审批前需实际JPEG frame成功解码及明确权利/保留截止。

真实执行：Go domain/HTTP19顶层＋19子项通过；native首轮权限/原回执/Session等待2顶层通过，Result失败保留，诊断确认合法完整来源列表有6项；oracle改为canonical原Ref唯一、保留TTL/源/ABA/跨Ref断言，`api-result-fixed-b03`定向1顶层通过。`api-native-proof-b04`组合3不同顶层通过（非新Go调用）；唯一production后变是等价按值机器DTO去图helper提取，只读复核。客户端23不同新增用例分批通过（14＋9），首失败、真实timer清理和解码帧等待修复保留；原活动/组织CRUD/详情18项`client-legacy-media-b03`通过。证据`batch-b/api`、`cover-client/FINAL.md`、各原生receipt。Go `api-build-b01`11binary通过，137已guarded应用于同一本机owned QA一次，原表计数/旧migration字节保留，API18113实际ready，provider/model/visual/video/push/A2A均OFF。

真实授权照片、OS选图/扫描/对象存储/地域链、真实组织经营权限和公开发布：NOT_RUN。本地有界JPEG合同及隔离测试不能关闭真实媒体或角色验收。

B1/C1合包实际产物：`D:\Project\birdtie\work\remaining-execution-20261014\apk-build-bc01\birdtie-batch-b.apk`（profile）；`web-build-bc01\artifact`（profile42文件）；`api-build-b01\artifact`（11binary）。两客户端构建为同一production frame，exit0/sourceStable，目标统一18113。增量`client-analyze-bc02`Saved/visual测试零error/warning、31info，前一次B/C22文件诊断保留。Web7357实际JS哈希匹配，手机preview实际`-r`安装保留数据、reverse18113、font1.0、前后焦点/主显示屏PNG `device-b-observe02`成功，首B观察脚本残留A证据路径导致安全失败已保留、修正路径后没有重装。不可变checkpoint为`batch-b/checkpoint-b1-c1/receipt.json`，包含实际artifact SHA和source frame。机械Impeccable定向detect exit0返回[]，仅Dart检测，不代替媒介编辑UI/真实角色验收。

## 批次 C1：Now本机typed草稿

NOW008–013/019/020既有UI增量，复用原Now提交、人聊writer及同MapCanvas。新增封闭typed envelope保留稳定本机ID、revision、本人/original origin/组织workspace、actual conversation/recipient、task、来源引用和附件历史状态；snapshot/live selector不当作当前权限或批准，跨类型转换为私有草稿并清实际收件人，无万能执行writer。SecureStorage按origin/owner namespace限40对象/512KiB，合并只改过的slot避免覆盖别的组织/收件人，局部正文/引用/附件状态不包含token/approval/image bytes。重启恢复、迟到读取不覆盖本次编辑；显式登出/换账号清该主体namespace，普通续期保留；旧成功只清exact contentVersion。

`client-draft-navigation-c01`17项通过（C新8＋原草稿4＋根地图导航5），随后只读复核发现切workspace/同owner拒绝后resume退休旧读取但未补读；已实际补当前generation restore，`client-restore-race-c02`新增定向1项通过。`client-analyze-bc01`B/C相关22文件零error/warning、175info；Saved错误状态修正后另做增量分析并构建。完整引用UI/转换入口、多站路线、获准副本和外部计划尚在接续，此C1不代表完整C完成，也不领取前置未齐的NAV001。

## B2 地点照片 / C2 路线计划增量

B2 Place photo独立138候选、原city贡献者→不同reviewer、精确图片权利/retention/version、preview/原操作回执/STOPPED；不修改已应用114事实字段或137封面migration字节，不把文字事实许可变成照片权限。PublicPlace/result/Saved/detail/Now按独立`placePhoto` DTO接线，版权/保留/具体JPEG实际预览与跨Session generation proof。仅本切compat safety增加137到期content物理擦除和照片候选唯一bytea清除的内部有界retention CLI；默认不运行、development/disposable/loopback/exact owned DB guard，未启用完整AIR020调度。当前Go直接25顶层＋27子项通过；原生首轮2顶层通过、1失败，实际诊断为两个共享lease clone把非nil空slice改为nil，导致严格来源比较误判。只修两个clone保留nilness，新增比较器1顶层＋4子项通过，原失败断言/源码不变，定向原生1顶层通过；组合证据api-native-place-proof-b07为3不同顶层、不是一次新运行。api-build-place-b02实际12 binary、exit0/sourceStable。138已guarded应用于同一本机owned QA一次，原27表计数及≤137迁移字节保留；runtime-place-b API18113 ready，未运行parent retention CLI或自动调度。首次失败全部保留。

C2 NOW019/020/023/024 → 当前UI父任务：新增RouteDraftController/Page，Now现有材料菜单进入多站路线或外部计划；当前同城已加载收藏选择、起点/交通方式/日顺序编辑、只显示当前地点到原MapCanvas、复用A3加入已有集合。没有route geometry/估时provider，明确Unavailable，不画线或伪造距离。外部本人提供文字≤6KiB、最多14天，仅日期/Day标题分组，所有地点未知、指令原样保存为编辑文本，选择后才用当前source。原文SHA、本机稳定ID/revision、按origin/owner/city slot恢复，不保存坐标/permission/token，导入与路线typed视图保留同ID且不覆盖Now/其他城市输入；支持旧C1缺slotId本机codec兼容。拒绝会话仅保留本人文字，显式登出/换账号不得晚写恢复。关联7单元/2widget/根route导航新场景已实际运行，client-route-plan-c03共30 PASS/1 ERROR（含旧typed9、原composer4、地图3、根导航6）。失败为测试夹具未滚动生成ListView下方按钮；第一次定向c04又发现keyboard下已生成按钮仍在hit region外，失败保留。仅改测试滚动，新增按钮中心必须位于键盘上方的断言，c05定向1 PASS。31不同用例均有实际通过，非一次最终源31 PASS。三个390×844正常字号隔离PNG已核看（visual-client-route-plan-fixed-c05/png），keyboard为模拟inset、非OS输入法截图；真实路线provider、真实账号/OS流程NOT_RUN。随后导入弹窗由固定350ms等待改为DialogRoute.completed，身份退休立即清输入及取消原route，旧确认回调不能生成草稿或错误pop底层page；client-route-import-c07新增2 PASS，取消/确认/退休均零HTTP、无集合/消息副作用，确认保留本机ID和原指令文字且未解析地点。新增检查为后一源码增量，旧31项不冒称一次当前源33 PASS。B2/C2已实际合包。


B2客户端独立照片模型/API、贡献者→独立审核者、规范JPEG预览及四个实际投影已接线。place-photo-client首轮18 PASS/13 ERROR，定向11 PASS/2 ERROR，再定向4 PASS与2 PASS；31不同用例全部有实际PASS，非一次最终源31 PASS。真实发现弹窗退出动画提前销毁notifier、过期已捕获回调仍可发送两处生产问题，已用DialogRoute.completed和live/current/painted/TTL内守卫修复；夹具故障另记录。原SUCCEEDED/STOPPED与当前读取分开，当前403/404退休内容并禁新动作，不抹去原事实。四张隔离正常字号实际JPEG截图已由worker及root核看，FINAL.json/MANIFEST.json保存完整源码与日志SHA。B1相同弹窗生命周期补丁cover-dialog-lifetime5相关用例通过；后续原回执与当前read分离已实际修补，cover-receipt-recovery5相关用例通过（原成功＋当前403、原停止＋当前404以及3个受影响成功/UNKNOWN场景），没有沿用旧B1检查点冒称当前源全量通过。


B2/C2合包：client-analyze-bc2覆盖当前30个精确相关文件，exit0，零error/warning、234info；APK `D:\Project\birdtie\work\remaining-execution-20261014\apk-build-bc2\birdtie-batch-b.apk`，SHA256 `f1234fc223e234467900113f9f5144c83b42816d2c93e97b937affce74d4a1fc`；Web `D:\Project\birdtie\work\remaining-execution-20261014\web-build-bc2\artifact`，main.dart.js SHA256 `2c86dfb4196b74b1b1b01fbaba10750840e8f0637764f06801c63abf45d22881`。实际两个profile构建exit0/sourceStable、同一完整clientframe，目标API18113，沿既有map配置、局部surface、friends image，未启用model/visual/video/push/A2A/自动维护。API为已编译api-build-place-b02的12binary、owned QA138，原迁移与历史字节保持。Web7357已核对当前JS哈希；实际手机preview -r覆盖安装保留数据、18113 reverse，font1.0和capture前后唯一主屏focus匹配，device-bc2-observe01真PNG已核看，只证明Now正常渲染。batch-b/checkpoint-b2-c2/receipt.json不可变封存3产物及8张核看PNG，组织139下一增量不混入此包/帧。完整SHA、命令、cwd、exit及首次失败均在各receipt；真实角色与双账号E2E、自动retention、全站验收NOT_RUN，发布仍NO。

组织邀请/任意坐标只读差额：原邀请/改角色/撤权/接受均无CAS或原操作账本；原map GET已含revision但客户端丢弃，PUT重发会revision++并重回pending，不能以普通名单/坐标重读猜成功。原human公开people/search目录可复用，不读私人联系人。任意坐标须独立map callback（已有重叠实体选择不是坐标），AMap暂无可验证逆向WGS84合同。此范围21个精确文件已登记并开始139串行领域writer/DDL批次；原canonical point与pending proposal须独立，原成员权限/last effective owner须保留，未发真实邀请。


## B3 组织成员和任意坐标：当前内部增量

引用历史 BT-ORG-004/BT-MAP-004 → 当前 BT-FIX-NOW-UI-001 已授权邀请/地图接线兼容安全；不重开历史DONE或领取其独立下游。21个Go路径、19个Dart路径先登记source-before，root分工坐标7和canonical详情桥5，client分工成员/邀请/位置12；没有新任务、真实邀请/联系人读取或OrgAgent前置。原people/search个人可发现目录复用，缺名仍管理、原known-ID后备，未知不假称已发送。

139增量成员控制版本/组织authority触发器与immutable actor:op账本；具体INVITE/ACCEPT/ROLE/REVOKE preview/PUT/original GET/STOP。移除→原ID重邀使旧version不可再用；最后owner按实际active PERSON校验，保留原多owner/admin层级、audit/notification一次。位置提案单独存储，PROPOSE/拒绝/撤回均不改原approved row/xmin；不同当前city reviewer批准确切提案+原canonicalRevision后才替换，隐藏也须确切版本。旧无op/CAS write端点返回409 control-required，禁止绕新协议；原本人历史回执/停止不借当前管理权。相关普通Session idle续期不改变source seal；原SessionID/gen、最后PG clock及city/approval原deadline在等锁后再验。

Go直接7不同顶层、28子项均有实际PASS（多源码运行，不合成当前同帧35 PASS）；首次native api-native-org-b08两个场景FAIL保留：原Session代际改变后写前Times无row被泛错误映为“组织不存在”。只修绑定帧分类和增加受影响HTTP检查direct05 10 PASS；两份原native测试和139SQL字节没有改。仅重跑两个失败场景api-native-org-fixed-b09，实际2顶层＋1子项PASS、0fail/skip/sourceStable，含成员CAS/实际owner/重邀ABA/旧source、原回执/STOP；地图旧点保留/独立reviewer/Session等待/普通idle续期/实际SQL ledger等待到city与approval过期回滚。API D:\Project\birdtie\work\remaining-execution-20261014\api-build-org-b03\artifact 实际12binary、exit0/sourceStable，api.exe SHA256 `c21f82c0d4ef6d21c42fe0a37d540b3b6bfd35988e4a422b33c0ec21e84251d2`。139已在同一本机owned QA经OID/owner/marker、≤138字节和原31表计数校验后应用一次，runtime-org-b API18113 ready，所有外部model/open places/visual/video/push/A2A/JOIN/自动维护保持OFF；仅本机代码兼容版本，未导入种子或发送真实邀请。

Root已实际新增原Mapbox Web/native手势WGS84 callback（默认null保留普通entity/重叠选择）；独立picker只返回当前本机坐标，cancel无域写、actor/城市ABA/map失败/旧点revision均禁迟到confirm，SDK真实tap函数和现2.26.0源码核对。AMap无可核inverse时明确Unavailable，原手填WGS84保留。原PublicOrganization详情按当前个人control GET角色/具体proposal flags与30s租期显示管理审核入口，不要求Agent/会员；原Now及Chat/Follow canonicalID实际进入同页，未选城市仍可审核原proposal、不猜城市。我的组织邀请注入原当前transport/API；原角色失权/邀请消失的本人操作由最小pending-org索引进入原receipt/stop，不能以普通读取403清journal或当零effect。

客户端39个不同场景已有实际PASS，分布direct02/03/04，不是一次当前同帧39用例全运行。direct01真实编译错误导致5文件加载ERROR，0场景/0截图；direct02实际25 PASS/12 ERROR，两个生产旧行callback引用可变列表的ABA问题已修为不可变行快照和generation，其余真实I/O阶段及取消选点label夹具问题修正，direct03定向17 PASS。角色owner降为admin时旧role不在Dropdown候选的生产错误已修，保留原选择但禁无效确认；搜索重读先退休旧私有行，direct04必要11 PASS。10张390×844正常字号隔离界面PNG已由worker/root全部核看，含具体邀请确认、过期批准、缺姓名仍可管理、邀请消失后原位置操作恢复、非成员reviewer与403原UNKNOWN。只证明隔离样例，不是真实组织邀请。

Root client-org-root-b10实际21 PASS/1 ERROR；失败是canonical Follow→组织位置导航夹具未等待SecureStorage/实async I/O，保留原ID/零write/同MapCanvas与输入断言，仅分三次真实async阶段后client-org-nav-fixed-b11定向1 PASS。22个不同root检查均有实际PASS，含picker7、个人权限4、原导航及map复用，不合成一次全量成功。安装前旧scope同帧分析0error/4warning/171info保留，之后仅两处不必要!和两unused import作语法修正；0error/0warning/171info终版分析通过。已核看offline picker截图发现页脚仍提示点地图，实际只改为返回位置页填写获准坐标；既有unavailable场景定向1 PASS及同尺寸前后PNG保留。邀请入口进一步复用现有身份/城市Boundary退休旧子树，无新收件箱。

B3合包实际APK：`D:\Project\birdtie\work\remaining-execution-20261014\apk-build-org-bc3\birdtie-batch-b.apk`，SHA256 `48d3c0a5d34bf8f5234c1f358e4fecffed45b9754864ae23ff7f0296cb45b672`；Web：`D:\Project\birdtie\work\remaining-execution-20261014\web-build-org-bc3\artifact`（profile42文件），main.dart.js SHA256 `17547bc9f4ef8ee8964dcad50a490e18ce27c167030805dbcd3acfa71c6e7ae7`。两个profile构建exit0/sourceStable、同一client frame，目标API18113；API继续本批12binary/owned139，不把140正在开发的源码混称139产物。Web7357实际JS哈希匹配，手机preview -r覆盖保留数据、reverse18113/font1.0，主屏focus前后匹配的实际PNG已核看。不可变checkpoint：`batch-b/checkpoint-b3/receipt.json`。真实组织身份、人员发现/邀请、独立审核和地图OS手势、双账号E2E与全站体验验收NOT_RUN；Closed Pilot/Consumer Beta仍NO。

B4 media140已实际完成原read-frame增量：137原本已有viewer Session代际hash，保留原事实；真实修补为server-only原SessionID/gen在资源等待前固定，content最终核对第一次metadata/像素/source，不借fresh proof。AreaPulse补最终cover gate；placeActivities/org/managed/social/business实际catalog接线，business双读只PreserveLease，再严格比非media/source字段。失去管理/源隐藏时只核本人原tiny receipt/zero-effect STOPPED，新权限不扩大；140函数保留effect当前manager/上传actor/实际版本，STOPPED只当前PERSON本人namespace，唯一效果index阻止同version重复因果回执，UPDATE/DELETE历史不可改。

14精确Go路径已冻结。HTTP/domain direct01实际34 PASS、direct02域3 PASS、direct03 frame/context4 PASS、direct04受影响catalog1顶层+6子项7 PASS，按不同source帧记录而非一次当前全套。Native api-native-cover-frame-b14实际2顶层PASS、catalog内areaPulse PASS，businessUpcoming首FAIL保留；长supplier SQL超过pg_stat_activity查询文字截断，原oracle未观察到真正等activities锁。仅native fixture改为pg_locks未获准activities relation＋本次LOCK backend PID，原Session ABA、409/无thumbnail断言不变。api-native-cover-frame-fixed-b15定向catalog实际1顶层+2子项PASS、0fail/skip/sourceStable；其余13源/140SQL与首轮相同，3不同native场景都有实际PASS，不是一场同源3top全量运行。

API `D:\Project\birdtie\work\remaining-execution-20261014\api-build-cover-frame-b04\artifact` 实际12binary、exit0/sourceStable，api.exe SHA256 `637c88de100f2a6f5368eb0daa11a559493ec68d2411eb129fd5ffd5096b7a7d`；140已在同一本机owned QA经OID/owner/marker、≤139的253旧migration字节及34旧表计数校验后应用一次，runtime-cover-frame-b ready18113，原表行数不变，无seed/实际邀请，各provider/model/visual/video/push/A2A/JOIN及自动维护OFF。新API兼容已封存B3 APK/Web；C3/B5正在改的Dart不冒称已经合包。证据 `batch-b/checkpoint-b4-media-frame/receipt.json`，真实媒体/角色/E2E仍NOT_RUN。

C3/141已接获准私人副本：作者原PUBLISH具体版本预览明确allowPrivatePlaceCopy，缺省false/旧false digest兼容；copyGrant只当前公开PlaceID和顺序。接收者具体preview/approval、本人原GET/bodyless STOP，新独立ID本人private/draft/rev1，标题只自己输入/中性默认，空summary/notes、hidden精度；不复制作者正文、备注、坐标或hidden/city内部IDs。原source撤回或grant改变退休副本借用name/point，保留本人原ID、标题、备注和顺序；只有原已保存地点selectors可继续本人编辑，不能新增失效地点或直接公开。明确移除并保存后重新选择当前地点才解除该条借用限制，来源历史保留。

15个Go路径与14个Dart路径分别登记。Go domain/HTTP direct01实际67 PASS、direct02受影响26 PASS，68不同场景分版本记录；native api-native-collection-copy-c08实际2顶层＋Journey/ThemeMap两子项PASS，第三场景因fixture漏原追加合同的locationVisibility: hidden FAIL保留。仅补该测试字段，原14其余源/141 SQL不变，受影响append normalization direct03实际1 PASS，api-native-collection-copy-fixed-c09定向第三场景1 PASS/0skip/sourceStable。3不同native顶层均有实际通过，不冒称一次最终源3场景全套。

API D:\Project\birdtie\work\remaining-execution-20261014\api-build-collection-copy-c01\artifact实际12binary、exit0/sourceStable，api.exe SHA256 ce3278850e0debffad24fe9c04562a9e29235195f15335976dfe1d187f29a252。141已在原本机owned QA经OID/owner/marker、≤140的255旧migration字节与34原表计数守卫后应用一次，runtime-copy-c ready18113；未导入seed，各model/visual/video/push/A2A/JOIN和自动维护OFF。142正在开发的API源不混称已编译141产物。

客户端74不同场景已有实际PASS（42新增C3＋32相关旧场景），分五轮记录，不是一次最终源74 PASS。首轮64 PASS/8 ERROR保留；后续实际修了对话框退出/expiry Timer生命周期及撤源后本人编辑被错误阻止两处生产问题，其余I/O与SecureStorage夹具修复独立记录。普通GET403/404退休当前内容，原SUCCEEDED/STOPPED保持；UNKNOWN只原GET/STOP，持久selector不含标题/地点列表/坐标/token。原share-code、公开集合、私人编辑页均复用，无新收件箱。6张390×844正常字号隔离截图已由worker/root查看，4张来自最后direct05，缺许可和过期分支为各自较早源帧；未把截图换算真实作者/账号许可。

B5根代理5文件身份退休增量已完成：FAQ与Support原入口复用现有身份Boundary和借用transport，Console活动列表/动作/编辑在个人与组织ABA后退休旧私有内容及捕获回调，普通同主体认证续期保留合法输入。client-secondary-identity-b16一次实际12 PASS/0skip/sourceStable＝6新＋6旧FAQ/Support检查；该增量是身份/页面保护，未称完整领域CAS/原操作恢复。后续142负责五类实际writer差额。

C3与上述B5增量合包：21个精确相关文件client-analyze-copy-secondary-c10实际0error/0warning、132info；APK D:\Project\birdtie\work\remaining-execution-20261014\apk-build-copy-bc4\birdtie-batch-c.apk，SHA256 68c184f173166f5b9221479a0dfd02e5196bd8af8ae6e24e705a1f12bcd17ef1；Web D:\Project\birdtie\work\remaining-execution-20261014\web-build-copy-bc4\artifact（profile42文件），main.dart.js SHA256 a38a4ec5dab4d37f3fe18cf67a2753e1e3cde9c0ed2c507fa6199f6ed54b6efb。两构建exit0/sourceStable、同一完整client frame，目标API18113。Web7357实际JS匹配；手机preview -r保留数据、reverse18113、font1.0，capture前后主屏focus一致的真实Now PNG已查看，只证明本机预览渲染。不可变checkpoint batch-c/checkpoint-c3-secondary-retirement/receipt.json。真实作者许可、经营/组织角色、双账号E2E、全站体验验收NOT_RUN，Closed Pilot/Consumer Beta仍NO。

## B5 五类个人控制：API142检查点

引用原FAQ/Context/Support/Consent/Block历史来源，归当前已授权二级页 UNKNOWN/CAS/身份兼容增量；不重开DONE、不实施AGE/AIR独立链。24精确Go/DDL路径。FAQ真实owner/admin原writer、Context本人声明集合、Report当前实体/消息可读及原5/24h quota、Grant原profile/profile_view/read撤权、Block原双向关系/授权/follow/通知/会话清理均在各自原native事务实施；共用只帧/批准/最小回执编码，没有万能业务effect服务。

142新增个人Context aggregate epoch、双向Block持久tombstone和本人family:op immutable ledger；FAQ加具体revision。原SessionID/gen在任何等待前固定；原资源/角色/源xmin/实际PG clock在等锁后重验。具体preview/一次PUT/本人原GET/bodyless STOP；原成功事实与当前读权限分开。源失权后原本人tiny receipt/zero-effect STOP可达，不借OrgAgent或扩展权限。所有9个旧无控制write handler返回409 control-required，无绕路。Report actual message source包含双向block历史，并把pair锁置于quota Account和source locks之前；原profile grant到Max/过期仍可按原受限清理语义撤回，不制造超上限revision或其它成功。

相关pure-direct实际41 PASS（8顶层＋33子项），一次稳定Go frame；不是native数据库证据。api-native-human-controls-b17实际2 PASS/2 FAIL：SessionABA真实FAQ行锁、tentativeeffect后Session行锁等待到批准过期均PASS；另外两个fixture误用旧agents列owner_account_id和缺原Grant所需本人user_profiles导致FAIL，首次日志/原24bytes保留。只补native fixture真实principal_account_id与本人private profile，其余23文件和142SQL相同；定向api-native-human-controls-fixed-b18两个失败实际2 PASS/0skip/sourceStable。4不同native顶层均有实际PASS，不合成一次最终同源4场景。检查覆盖FAQ/context CAS与来源退休、失权本人历史/STOP、report quota/audit/一次效果、消息block→unblock ABA、实际双writer锁序、max过期grant cleanup、UNBLOCK不复活旧关系，以及锁后拒绝0effect/0audit/0receipt。fixture-compile02 -run ^$仅编译、0functionalcase，不计PASS用例。

API实际12binary产物 D:\Project\birdtie\work\remaining-execution-20261014\api-build-human-controls-b05\artifact，api.exe SHA256 b67b9c80cd7d06ef75f4041edabe3fdee83a92507d7d24ffc1333be5398caa37，exit0/sourceStable。142仅在同一本机owned QA经OID/owner/marker、≤141旧DDL257bytes与44原表计数保护后应用一次；runtime-human-controls-b ready18113，各模型/visual/video/push/A2A/JOIN/自动维护仍OFF，原表计数保持。检查点 batch-b/checkpoint-b5-api142/receipt.json；下一143正在开发的Go不冒称已编入142。

B5客户端34路径及两个明确委托测试文件已冻结，原FAQ/生活资料/Support所有实际caller、Consent与Block入口复用。五类State/具体preview/一次PUT/本人原GET/bodyless STOP真实接线；本机PREPARED记录删除后旧页面不能再晋级UNKNOWN提交。8个Support caller以当前个人owner、目标/transport的单调逻辑来源退休旧批准；普通header续期保本人草稿，目标A→B→A不复活旧确认。实际修了Community字段放错State、重复didUpdateWidget两个编译错误，以及原聊天凭据边界销毁独立举报稿的问题；仅举报推到根Navigator，保原聊天writer与退休规则。

direct01–09各实际sourceStable；分别56PASS/7ERROR、100PASS/8ERROR、7PASS/1ERROR、0PASS/1ERROR、0PASS/1ERROR、1PASS、9PASS、2PASS、1PASS，0SKIP。167个不同逻辑场景最终均已有实际PASS，跨必要修复帧对账，不是一场同源167全套。所有首编译、fixture、生产生命周期和辅助脚本诊断失败保留；仅重跑失败及实际改动相关guard。冻结manifest SHA256 130bbb5f049f71e988bece979668807e590abbf082480297196a64d67bd21ae3，详细FINAL.md/JSON位于batch-b/human-write-controls-client。8张390×844正常字号选中PNG由worker和root均实际核看；最新FAQ/中文授权时间在direct08，C3确认过期准确文案在direct09，原早版图保留。都是隔离合成资料，不是真实举报/角色/授权操作。

本批B5客户端已与C1/143分享SDK合成一次当前APK/Web，见下方合包检查点；历史C3包保留为修改前证据。PersonalCollection普通CRUD、Moment及Activity旧UNKNOWN差额仍需后续原writer兼容，不称统一草稿全闭合。真实角色/帐号/E2E/全站验收NOT_RUN，发布仍NO。

## C1 本机引用、文本提交与路线转换增量

当前7个生产文件和3个相关检查文件已实际修改。原Now材料菜单接本机引用/本人注释：引用对象与实际收件人分开，来源标题、正文、批准、像素和令牌不入持久草稿。只从当前canonical结果具体版本选择；来源过期/主体ABA后退休旧标签和捕获回调。本人注释明确为自己写的快照，不将它称为原作者正文复制许可。原Now闭集请求未扩大，引用和历史附件尚未消费时由本人确认只发当前文字，成功后保留未消费材料、原草稿ID和当前Task绑定，不静默删除材料。

原Route转换保留本人文字/引用/历史附件、原ID和任务，清实际收件人；普通上下文引用不自动变成路线站点。修改站点只替换route_stop用途引用，原注释和其它材料保留，完整闭集验证通过后才更新草稿。首次读取本机namespace尚未完成时禁止保存，迟到旧草稿与当前编辑冲突须明确选择恢复旧稿或保留当前稿，防自动覆盖。缺合法geometry/provider时仍Unavailable，不以连线猜真实路线。

client-typed-context-c11实际29 PASS/2 ERROR/0skip/sourceStable；两个失败来自隔离widget挂载未配置非空来源地址，闭集草稿编码正确拒绝写入。只补这两fixture的明确隔离origin，client-typed-context-origin-fixed-c12定向2 PASS/0skip/sourceStable，其余生产文件不变。31个不同场景已有实际通过，分两源码帧记录，不冒称一次当前31用例全套。覆盖真实原材料菜单、取消0请求、本人确认文字请求、收件人与引用隔离、来源过期、主体ABA、材料持久恢复、路线转换及首次读竞争。首次语法/夹具失败与原日志均保留。

同尺寸390×844正常字号的引用来源退休前后两PNG已实际查看。client-typed-context-visual-c13只去fixture调试角标并定向1PASS/0skip/sourceStable，新两PNG已查看，旧c11保留。root截图复核发现单项/空态仍强制72%大弹层，实际将local_draft_context_page改为内容高度、长列表最大72%；来源退休后弹层同时收紧，不留空大面板。首次c17辅助脚本错误预建receipt目录，Flutter未启动、0用例，原脚本/空目录与preparation-error.json保留；修为独立visual目录后c18仅原过期回调强场景实际1PASS/0skip/sourceStable及新前后两PNG，均由root实际查看。不是重新全跑原31场景，也不覆盖旧图。

Moment/Activity/Journey/ThemeMap其它实际typed转换和普通写UNKNOWN仍有后续差额，不把本机引用测试或编译换算全站验收。本批C1客户端已纳入下方同源APK/Web合包；真实多身份/E2E与模型引用消费NOT_RUN。

## C1 原实体分享：API143检查点

13精确Go/DDL路径，复用原person/place/activity/community/organization/business/moment七类canonical reader、好友与原消息/通知/audit writer。新CONVERSATION或FRIEND_TIE具体preview核双方当前权限，FRIEND preview不先创建对话；本人明确提交后才同Tx创建所需对话及一次原消息效果。本人actor:op固定收件namespace，30秒内具体sourceVersion/批准，原SessionID/gen在等待前捕获，最终等锁后核双方源/真实PG clock。媒体、标题、位置和正文不进原操作回执；旧source/tie失效不隐藏本人tiny成功事实。新STOP只零效果tombstone，迟到entity producer被挡；原074不同会话同op各自历史保留，不能把其它旧成功误称全局STOPPED。旧entity raw write返回409 control-required，原普通文字writer保留。

直接检查actual4顶层＋15子项19 PASS事件，exit0/sourceStable；后续仅native fixture变化，12其它源及143DDL相同。api-native-entity-share-c14 actual0PASS/5FAIL：合成friend request错误携带city，原034约束拒绝，未进入分享流程。只修fixture city=NULL，c15 actual12PASS/5FAIL：七类双reader顶层已PASS；两个末等待oracle把Session锁等在初始capture阶段，不能证明最后提交。只在持锁前捕获HTTP等价原frame并传到新STOP/Commit，c16定向两个未完成顶层actual9 PASS/0fail/0skip/sourceStable。原3个native顶层跨两fixture帧均有实际PASS，不合成一次最终3-case全套。强断言覆盖真实pg等待、Session/同值来源ABA、末Session等待后旧proof拒绝、venue短TTL经过最终锁等待0消息/通知/audit/receipt，以及原074与STOP双向竞争。所有首失败和原字节保留，fixture-compile各轮0functionalcase不计PASS。

API实际12binary D:\Project\birdtie\work\remaining-execution-20261014\api-build-entity-share-c02\artifact，api.exe SHA256 ed9e8c699d3d1b6564a88f5c04a68bc1acd9e060a65f3fb356efa785dcad52f8，exit0/sourceStable。只在原owned QA经OID/owner/marker、259旧DDL字节和47原表计数保护后应用143一次；先精确停止原owned142 producer再安装late-message guard，避免把旧运行binary冒称新锁序。runtime-entity-share-c ready18113，原表计数不变，模型/visual/video/push/A2A/JOIN与自动维护OFF，无seed/真实消息/生产变化。检查点 batch-c/checkpoint-entity-share-api143/receipt.json。143客户端22登记路径已完成实际接线和当前APK/Web，见下方合包检查点；下一144源码不属于本143 binary。真实双方账号/消息/E2E、旧多binary滚动兼容和全站体验验收NOT_RUN，发布仍NO。

## 批 F 本地选择性迁移准备增量

来源 NOW027/PG25/历史 BT-V4-MIG-001，仅接本任务单明确允许的本地准备，不领取独立 AIR005、重开历史DONE或执行真实迁移。只读核对 Civu 两个指定旧schema字段，当前 Birdtie124已存在ThemeMap原writer；修正复用矩阵/CSV/保全文档的旧“没有目标表”说法，原 source-before 留在 scope-civu-selected-preparation-root。没有复制 Civu 业务代码、数据、凭据或旧迁移。

新增 scripts/prepare_civu_collections.py 接受明确选择的规范JSON及owner/Place映射，闭集校验、原顺序、新原操作提案、源/输入SHA和deferred分类。只生成本地待确认私人候选，server新集合ID尚未生成；空notes、hidden精度，不带旧媒体/成员/公开/复制许可，不发请求、不生成SQL、没有apply。未知字段/重复JSONkey、主体合并、地点合并、跨城、删源及错误Unicode/版本均有具体拒绝或deferred；同名旧输出和输入保留。

实际Python单元/CLI相关7场景一次PASS，exit0/sourceStable；另外一次明确合成选择的CLI运行exit0，生成待确认候选；≤141旧DDL字节保持。证据 D:\Project\birdtie\docs\testing\evidence\remaining-execution-20261014\batch-f\selected-collection-preparation01\receipt.json，含命令/cwd/退出码、源SHA、真实运行日志与合成输入声明。实际Python字节码编译和源码打包产物 D:\Project\birdtie\work\remaining-execution-20261014\civu-collection-preparation01\civu-collection-preparation.zip，SHA256 f6dcf981eff0be48fda1144f82705374ff7fa6ca818924a30718048888e5ede4；此ZIP为本地准备工具，APK/Web仍是之前C3检查点，不把正在开发142/C1混称已经合包。

真实DB/媒体保全、隔离恢复、当前owner/Place权利、真实导出dry-run与导入全部NOT_RUN，缺真实资源身份、读取通道与明确选择/许可；无真实DB、用户、媒体或生产变化。Next仍是B5实际writer/client冻结直接检查，143原实体分享以及C1/Moment/Activity普通旧操作的真实安全差额。

## B5/C1/143 客户端合包检查点

143客户端22精确路径中19实际变化，复用七类原实体reader与好友/既有会话入口。先读取实际本人和收件人，再取得当前卡片具体预览；明确确认后本机PREPARED读回、原记录晋级UNKNOWN读回、一次PUT。未知结果仅原GET或bodyless STOP；本机prepare被另一实例修复/清理后旧确认不能再提交。旧v1不同会话namespace仅原tiny历史/STOP兼容，不重发raw。所有实际caller以当前owner/来源代际/transport和当前header接线，双方名字或来源ABA退休旧批准，普通认证续期保留本人恢复记录。原receipt不借当前目标/对话GET成功猜事实。

worker11轮直接检查72个不同逻辑场景最后均有实际PASS，0未解决FAIL/ERROR/skip；并非一次最终同帧72全套。首轮缺原ConnectionSource辅助方法、导航泛型/夹具类型等编译失败保留；随后实际修了SUC后旧FRIEND_TIE列表未刷新和Navigator销毁后分享全局占用未释放。测试异步response stream等待沿既有B5真实async夹具修复，所有TTL/效果次数/ABA断言保留。direct05截图夹具挂起，只终止核实命令/父进程身份的单个Dart子进程，非零及部分事件/图片保留，未完场景不计PASS。字体缺字形和图标的早版图保留，direct11最新三张390×844正常字号合成截图由worker/root实际查看；具体预览、批准过期、UNKNOWN核实/停止可读，无真实收件人或消息操作。

root scoped分析首c19真实0error/4warning/338info、exit1/sourceStable。仅清理两个unused imports和从未读取的route edit counter，并对延迟到最后dialog卸载的实际super.dispose委托作局部说明/静态ignore，生命周期实现不变。随后c20实际0error/0warning/338info、exit0/sourceStable；没有为这些无行为变化重复167/72全套。原36B5、31C1和72分享不同检查分各自源帧记录，不合成全站用例总通过数。

最后只读审查发现6个State页把旧登录getter函数传入已开Sheet，页面更换函数/监听器后可能继续沿旧header。根新登记7+1精确路径，实际改为动态最新widget header/owner/workspace getter，并沿原SourceSerial转发读取绑定事件、保持logical generation不随普通续期改变；Place在原reader退休后仍独立订阅/准确清理当前workspace监听。新5个实际页面场景及2个直接受影响旧guard最后均实际PASS，跨两个源码帧；首missing import导致loadERROR保留、仅补fixture import后第二帧6PASS/0error/0skip。覆盖getter/新Listenable A→B→A、捕获旧确认0PUT，sameowner真实Secure UNKNOWN保留且仅原GET恢复。旧FINAL/source-final22不覆盖，新FINAL-UPDATE/source-final08记录增量；closure-only复用direct11布局图，不冒原图覆盖新行为。c21最终同源相关分析实际0error/0warning/340info、exit0/sourceStable。先前APKbc5已实际编译但不作为最终provider修补包，原artifact和帧保留；以下bc6才是最终源码配对包。

当前APK D:\Project\birdtie\work\remaining-execution-20261014\apk-build-share-final-bc6\birdtie-batch-c.apk，SHA256 5b7fe69cddbdacc55b376e7b77913d61a02a9e86f48d77d019557ff906152e0e；Web D:\Project\birdtie\work\remaining-execution-20261014\web-build-share-final-bc6\artifact（42文件），main.dart.js SHA256 ffb969a7ced4ab6a2e40c3dc510582aa589399e766af9f974d630f623a253fb0。两次实际profile构建exit0/sourceStable、同一完整client frame，API目标18113；本次未重新编API，配对的是已实际编译的143 api.exe SHA256 ed9e8c699d3d1b6564a88f5c04a68bc1acd9e060a65f3fb356efa785dcad52f8。未完成144未混入服务包。

原本机143进程和Docker引擎已结束，初次observer/runtime恢复失败保留；恢复本机Docker及原DB容器后，OID/owner/marker、143 schema/late guard、旧DDL和48表当前计数核对保持，未应用迁移、导seed或启外部provider。runtime-entity-share-resume02 ready18113。手机修改前先核对实际安装C3 APK原SHA，首次非Birdtie foreground守卫拒绝截图，未保留其它窗口；启动原预览后保存before03真实正常font1.0图并实际查看。新包安装/观察和Web的实际结果见checkpoint，不把正常Now渲染算真实B5操作验收。

不可变记录 batch-c/checkpoint-b5-c1-share-client143/receipt.json；原256任务对象和73未闭合数量保持，不重开DONE、不称父任务完成。真实双方身份、经营/组织角色、消息/E2E、媒体生产链和全站体验验收NOT_RUN，Closed Pilot/Consumer Beta仍NO。下一批立即接Map1地点照片/真实近期动态，再Map2明确批准公开Moment派生图。

## 2026-10-14 地图整改优先接续

用户在地图审查聊天明确选定执行方案并授权实际执行，app将该安排送入当前任务。原件 C:\Users\chens\Documents\Codex\2026-10-13\d-project-birdtie-docs-testing-evidence\outputs\BIRDTIE-MAP-CONTENT-EXECUTION-SPEC-2026-10-14.md 已在登记两个精确文档scope后逐字节复制到 docs/product/BIRDTIE-MAP-CONTENT-EXECUTION-SPEC-2026-10-14.md；复用矩阵新增第10节，四个指定Civu文件只读核查并记SHA。源/目标/字节一致、只读Civu HEAD和无代码/数据/媒体/Session复制记录在 batch-c/map-content-spec-import01/receipt.json。

按新优先安排，B5/C1/143分享客户端先停在可编译合包的自然检查点，随后优先地图第一批：复用138/140地点照片、最近30天当前访问者合法动态完整计数、原轻预览和稳定详情，28/36/16dp圆点及受控缓存/迟到解码退休。第二批先补作者明确批准公开Moment派生图完整合同和详情，再方形照片点。无未读合同不画红点；坐标不移位、不数字聚合、不新增常驻Now面板/收件箱/分类条。点位合法但图失效只回退同点符号，实体失权沿原源退休。

当前144普通Moment兼容Go增量已在自然源码点保存/冻结，未领取后续Activity/Collection批。13登记路径中native测试文件尚缺；其余源码实际格式化并随直接测试编译。moment-control-natural-checkpoint01实际8PASS/5FAIL/0skip/sourceStable：内容2顶层+6子项通过，HTTP夹具用了不符合ParseBearer的文本令牌；原源/失败保留。只改HTTPfixture复用实际NewToken形状与显式raw Authenticate，moment-control-http-fixture-fixed02两个失败顶层+3子项实际5PASS/0skip/sourceStable。4不同顶层跨两帧有通过，不是一次最终同源全套。source冻结在moment-control-source-natural-freeze03；新144原生事务/迁移/二进制NOT_RUN、客户端NOT_IMPLEMENTED。源码raw write guard已返回control-required，不能部署给旧Moment SDK；当前运行API仍143，须先补原生与最低实际客户端配对才能后续切服务。保留这段未完成代码，不以地图优先为由回滚、开启绕路或冒称已经兼容。Root C6媒体metadata/视觉未核实本机摘录13路径仅登记source-before，尚未实际改生产或测试源，现延期让地图优先；只读审查不是已实现。保全部当前未提交文件、旧lease和原队列对象，不将优先变化当成原任务DONE或撤销历史证据。

## Map1 地点照片与近期动态、Moment144实际配对检查点

引用原BT-V4-MAP-001/002、BT-V4-MOM-002来源，归当前BT-FIX-NOW-UI-001消费界面增量；保留原256任务对象、183 DONE与73未闭环、原依赖和历史。本节接续并覆盖上文“144客户端/原生未实施”的历史时间点，不删除其失败与未完成记录。未领取AGE/AIR、模型LIVE、真实发送或生产链。

Map1服务端已实际加入明确singleton媒体opt-in的typed-map-layers-v3。旧v1/v2闭合wire保持；仅原PUBLIC PLACE增加原138/140当前获准照片与近30天完整合法公开Moment集合计数。真实count基于publishedAt、当前active PERSON、明确public/published、canonical Place及访问者双向block，不用地图截断/预览项数冒充总数。失败是未知、合法空集合才0；private Profile不撤回作者明确公开正文。原Session/来源proof/lease最终比较，照片到期/撤回只退休可选媒体，地点本身失权才退休原点；计数保留原stamp/窗口/截止与全成员proof。等待后实际PG clock与HTTP编码前wall clock检查，optional查询失败经savepoint回滚保留原地图。CORS只批准origin允许此新header。

客户端TypedMapItem→MapEntity→原MapCanvas→Mapbox native/Flutter Web层/AMap临时PNG bridge已实际接线。有界RAM包含pending/decoded/bridge预算，3并发/24项/16MiB；实体/media/version/SHA/owner/org/city/provider/generation/DPR参与身份，owner/city/sourceABA和迟到解码退休。无持久媒体缓存或认证传bridge。圆Place28/36/16、方Moment记录符号、标签12sp/最大6和144dp沿本方案；同坐标保持原锚点与原选择器。原Place轻预览/关联动态/完整详情原入口已经真实接线，未重复重做。真实不同JPEG实际读取、SHA、解码、像素颜色/PNG差异与同Place版本替换检查通过，未把AI图或几何fixture关联真实POI。

144现已完成最小实际SDK配对：原CREATE/UPDATE/WITHDRAW三writer、具体preview→确认→durable PREPARED/UNKNOWN→原一次PUT；UNKNOWN只原GET或bodyless STOP，原历史成功不依当前列表读权。普通同主体认证续期保留原表单/当前transport，主体/城市/来源ABA退休旧批准与捕获回调，原Now实际创建入口四场景已通过。native principal source包含当前eligible PERSON账户xmin；实际principal ABA生产缺口已修。实际UI补禁UNKNOWN/未修journal的新保存与撤回，不只挡controller；dispose/关闭后的迟到结果不恢复，现有Moment字段/具体版本/显式清与max撤回语义保持。

API直接map-moment-api-direct01实际16top/54 PASS事件，原生首轮native01真实FAIL保留：SQL fixture别名语法、principal ABA与block fixture等待；最终native02实际6top/18 PASS事件，0fail/skip/sourceStable，独立标记子库迁移至144、parent业务计数不变。客户端direct01至07逐轮失败、修复、来源帧和完整结果保留；最终定向direct06 38PASS、direct07历史6PASS，均0error/fail/skip/sourceStable。七轮共192个不同逻辑名称最新结果PASS，跨多源码帧，不是一次最终同源192场景全量执行。首次UNKNOWN视觉active-button图保留为拒绝证据；修后原GET/STOP可见、保存/撤回禁用。最终相关分析c23 exit0、0error/0warning/137info/sourceStable；JS语法检查exit0，不等同真实AMap运行。

本次API D:\Project\birdtie\work\remaining-execution-20261014\api-build-map-moment-c03\artifact 实际12binary，api.exe SHA256 2967ade3cc37fe8cb943bb9fd2c59cc45cf1bf0be58fc085207f0f411a5643c8；APK D:\Project\birdtie\work\remaining-execution-20261014\apk-build-map-moment-final-bc7\birdtie-batch-c.apk，SHA256 aa81e53f48351b7e8f43bf131f4b58b99e5e2ec94046ee051979fe7bfd8047f9；Web D:\Project\birdtie\work\remaining-execution-20261014\web-build-map-moment-final-bc7\artifact（42文件），main.dart.js SHA256 2ddb3274ccdd299fbfcd6f2465c381ece57bbbdb352610ddd9eb3c678a89492b。三构建实际exit0/sourceStable；APK/Web与最终相关分析同一完整client frame，目标本机18113。verified owned QA经OID/owner/marker、原≤143 migration字节与原业务计数守卫后仅应用144一次，runtime-c04 ready；旧业务行数不变、未种子、未发送、各外部模型/视觉/video/push/A2A/JOIN/自动维护OFF。实际当前HTTP smoke四项PASS：旧v1、新v3、未知header400与approved-local-origin CORS204。

手机c641566b已实际 -r 安装bc7到隔离preview身份并保留数据、reverse18113、font1.0；Web7357实际JS哈希匹配。旧C3/API143与新bc7/API144同城同基线真机PNG已查看；真实Mapbox底图和圆Place/方Moment缺图符号可见。初次tap未开IME的PNG保留为NOT_CONFIRMED，后续实际开/关键盘PNG已查看，输入栏在键盘上方、顶部可达，关闭后原地图基线与符号返回。未录屏、未输入/提交、未用正常Now截图冒称真实角色/媒体操作验收。390×844组件图另标隔离widget fixture、几何JPEG、非真机/非真实POI；Map before/after是当前代码缺图与获准fixture分支，并非旧APK图片对比。

不可变检查点：batch-c/checkpoint-map1-moment144/receipt.json，内含源码帧、实际命令/cwd/exit、首失败、产物/SHA、12张已核看截图与来源。真实获准Place照片/rights/reviewer、媒体生产storage/scanner链、双账号/完整动作E2E、AMap Web/Mapbox Web真实媒体、iOS及全站体验验收NOT_RUN。大字号专项已取消；中英混合语音继续延期；Closed Pilot/Consumer Beta仍NO。下一步实际实施Map2作者明确重新选图与独立公开派生图合同，先当前公开详情像素，再同Now方形照片；144不是公开图片合同，不自动公开私人附件。

## Map2 作者另行选图与公开Moment照片145检查点

接续Map1时间点，Map2现已实际编码、直接检查、编译及本机QA应用。仍归BT-FIX-NOW-UI-001已授权界面增量，原MAP/MOM来源、256任务对象、183历史DONE、73未闭环及原发布依赖保留；不自动完成独立AGE/AIR/PLC或真实角色验收。

作者明确重新选择本机图片，声明具体来源/使用/公开派生权与期限，看到实际规范化320×180 JPEG后确认。独立145公共图片slot/本人actor:op账本沿原Moment、人类控制帧与PERSON Session实现SAVE/DELETE；具体preview、durable PREPARED→UNKNOWN→一次PUT、原GET/bodyless STOP已接。照片与正文revision独立，但正文或原Moment公开范围/状态/来源发生变化时清公开派生字节并退休版本。原100私人附件从未自动转换为公开图片；真实原生测试确认私图退休与公开图重新选图是两条动作。删除本人公开slot可沿受限清理语义，不借公开读权或OrgAgent；等待后PG时钟/原Session/source xmin/lease重验，成功事实不因当前来源失权消失。

公开详情显式photo-v1 opt-in读public-moment-v3，媒体11字段闭合、绑定MomentID/revision/photoID/version，实际像素hash/byteLength/MIME/codec核验后展示；旧v1/v2保持。地图单独public-moment-photo-v1读typed-map-layers-v4，原Place v3仍Place-only，SELF_PRIVATE不能借public媒体。可选图片失效只退图，原实体/source/锚点不变；v4 optional proof kind:ID避免同UUID跨类型混淆。原138/140 Place及145 Moment共用有界RAM和最终帧，3并发/24项/16MiB，不写永久媒体缓存。原Flutter/native PNG/AMap bridge及选点行代码均接真实规范化像素；方28/selected36/dense16沿用户方案。只有核验原SUCCEEDED才给同owner/origin Now RAM刷新提示，由原前台GET重读，不从提示注入媒体或权限、不后台拉取。

首次API direct01因错误profile-avatar导入0测试SETUP FAIL保留；修正direct02实际20顶层/100 PASS事件、0fail/skip、sourceStable。native01两顶层11 PASS事件，另1顶层失败暴露原私人图片audit INSERT缺001-required decision/purpose的真实问题；保原DDL及校验，两个原Save/Delete INSERT补具体allowed/private用途。private-audit-direct03原6顶层9PASS；native-fixed02仅失败canonical定向1PASS，真实私人Save/原回放/100退休、145 slot、public-v3/v4、删除留正文均通过。新schema oracle首oracle04将DEFAULT occurred_at误判caller-required失败保留；仅修测试字段分类，oracle05定向1PASS。三不同native顶层跨两个源码帧均有真实PASS，不称一次当前同源完整原生执行；旧263份≤144 DDL字节未改。

客户端direct01实际173PASS/19ERROR，0skip、sourceStable：11个binding fixture、7个页面controller timer生命周期、1个secure storage fixture。实际修controller由editor owns dispose和route.completed清理，原断言保留；direct02三个受影响文件24PASS/0fail/error/skip/sourceStable。192个不同逻辑名最新结果均PASS，跨必要修复帧，不是一场最终同源192全套。最终相关29 Dart分析c24实际0error/0warning/160info/sourceStable；AMap JS语法exit0，仅语法证据。

API D:\Project\birdtie\work\remaining-execution-20261014\api-build-map2-c04\artifact 实际12binary、exit0/sourceStable，api.exe SHA256 cc76421fb9c78529b6d24936344e23c7f5ad4dd9d0422e01bf9579c93765b6e2。APK D:\Project\birdtie\work\remaining-execution-20261014\apk-build-map2-bc8\birdtie-batch-c.apk，SHA256 b58973e3d726165f25fb23d6291e5fa8fd11a40be531b7a1a3737f09d701b0ea；Web D:\Project\birdtie\work\remaining-execution-20261014\web-build-map2-bc8\artifact 实际42文件，main.dart.js SHA256 f12c899c291a617e78676ddc121b5911b66aeac15530bd0e6d28bd19835987ea。APK/Web profile、exit0/sourceStable，与最终相关分析同一完整client源码帧，目标18113。145仅经本机owned QA OID/owner/marker、旧DDL及全部既有public表计数守卫后应用一次，原业务行数不变；runtime-map2-c05 ready18113，模型/visual/video/push/A2A/JOIN/自动维护仍OFF，没有种真实公开图片或发信。实际运行API8个只读HTTP检查PASS，涵盖旧map-v1、Place-v3、Moment-v4、未知header拒绝、旧Moment-v1/新v3真实相同ID/revision/body/photo:null以及批准localhost CORS；无模型调用或业务写。

Web7357服务实际42产物hash匹配。Android c641566b实际-r安装bc8到隔离preview、保数据并reverse18113/font1.0；初观察失败因系统Launcher前景保留failure-receipt，随后只启动本preview再核对前后foreground并保存实际截图。旧bc7/API145截图为已平移镜头，新bc8/API145重启后默认镜头，因此明确NOT_MATCHED_CAMERA，不能冒称固定镜头前后视觉验收；真机只证明当前Now底图/诚实缺图符号正常。固定390×844组件5张before/after/selected/dense/retired为当前源码分支、隔离几何JPEG、同实体锚点，另4张editor/confirm/expired/UNKNOWN均由root实际核看；不是实际照片许可、真人发布或设备媒体验收。未录屏。

不可变检查点 batch-c/checkpoint-map2-public-moment145/receipt.json 保留每次actual command/cwd/exit、源码帧、首次失败、产物/SHA和11张来源清楚的已查看截图。真实获准公开图片/权利与角色、存储/scanner、AMap Web/Mapbox Web真实媒体、iOS、双账号E2E和全站验收NOT_RUN，Closed Pilot/Consumer Beta仍NO。正常字号继续，中英混合语音延期。下一步C6：现有图片/视频材料→本机typed附件selector、历史重选状态，以及明确未核验的可编辑视觉问题/备注；继续复用原视觉合同，不启新模型或记忆/地点事实writer。

## 后续接续与门槛

批 B 正在核对既有受控媒体 producer/model/当前render。Activity沿原 organizer/manager、Organization沿原身份owner/admin writer，不能增加 Organization Agent依赖；Place须沿原 city editorial贡献者→不同reviewer审核，并具有精确图片版权/公开派生权，旧事实字段权限不能变为图片许可。现真实 storage/scanner 配置与获准实图缺失不阻断本地接口/隔离合同；对应真实上传、官方图片和运营身份仍 NOT_RUN。既有4张AI插画只作隔离样例。

批 C 复用现有集合CRUD/公开批准/原回执、Moment/附件及地图导航；typed draft、获准副本/外部计划等仅补当前代码实际差额。独立 NAV001、AGE/AIR 下游保留原依赖，不能借UI父任务开启。D/E/F按 live 队列原理由与真实条件接续，不把人工子片替代AGE007完整合法数据校准，也不自行启用模型LIVE/A2A、生产迁移或Post-Pilot。

正常系统字号、简体中文、窄屏/键盘/滚动/读屏与授权/后果提示继续检查；Now保持联系＋通知、短头像列和局部透明，不新建收件箱入口。未录屏；中英混合语音继续延期；大字号专项按用户要求取消，不列为待办或阻塞。Closed Pilot/Consumer Beta发布门槛仍 NO。只证明实际执行范围，不宣称全站体验验收完成。


## 2026-10-15 C5/C6、首页内创建与来源隔离合包检查点

接续原任务单C1/C5/C6及NOW008–013/019/020、NOW023/024来源；归当前已授权Now界面与草稿兼容增量，不替独立NAV001/PLC005、AGE/AIR或真实视觉任务。用户明确授权向共享Now文件的另一聊天发一次协调消息；root已发送，对方在自然源码点冻结了四个生产文件。原未提交代码被保留并整合；没有reset、旧提交覆盖、重新领取或关闭历史DONE。对方独占说明见 [首页创建修复交接](BIRDTIE-NOW-CREATION-REPAIR-2026-10-15.md)，不把其旧测试结果冒称本次最终帧。

本次实际行为：现有图片/视频/语音材料接本机typed附件selector；仅保存opaque ID/kind/state，不保存像素、路径、批准或上传回执。恢复仍是相同草稿ID及历史selector，明确仅有选择记录需重选；取消重选不删历史，未知原操作不被新材料覆盖。图片问题与实际已核验报告的未核验摘录可编辑、具体预览后仅保存本机文字，来源、未知项与非事实语义保留；不启分析、发送、Memory、Place或到访writer。旧报告与当前图片无法匹配时，不冒称当前图的报告。Now真实入口直接检查了same-owner普通认证续期、持久读回、零视觉请求/Task提交与地图实例不变；本机像素保留但旧批准serial/revision退休，账号/来源ABA不复活。

外部计划接严格6KiB JSON、逐日编辑、未知字段保留、明确当前可用收藏地点匹配与稳定当前dayID恢复。原输入地点ID/名称、日期和无时区时间只作未核验材料，不自动匹配POI或上图，不执行输入中的指令。复用原路线/多站/同地图/加入已有集合；真实geometry/估时仍Unavailable，NAV001不借此关闭。首页四类创建使用同一composer与可折叠预览，视频直接原位选择/播放/移除；清掉两条已无调用的旧独立创建路由及其专用缓存/导入，不删除现有管理页。新创建面板完整结构跨进程恢复、真正动态feed、原子串文、多图/视频公开与Activity immutable writer、Collection UPDATE/WITHDRAW原事实仍未完成，不称当前面板已全闭环。

认证会话及native/Web OIDC pending增加canonical API origin v2 key与payload，旧无来源v1保留但不猜迁移、不发往另一个API；foreign记录不借当前owner/token清其它origin，原late-issued session只向原入口注销。borrowed网络客户端不被Auth dispose关闭。此改动是切换到杭州之前必要的凭据隔离，不是生产登录已通；原手机登录仍无真实SMS服务，历史verified flag不授予本人新Session。

相关Flutter直接记录direct01–09首失败全部保留：初missing gestureCurrent、真实图片codec夹具异步区域、对方中间帧missing import、实际续期丢图、旧Inbox文案断言都分别修复并定向复验。191个不同正常字号逻辑名称最新均有实际PASS，跨多个源帧；不是一次最终源码191全仓测试。direct06实际52PASS，direct07为42PASS/1旧文案ERROR，direct08受影响9PASS，direct09实际28PASS；direct07/08偶然执行原历史200%夹具，其证据保留但从当前范围/汇总剔除，helper已加入排除，后续不再安排。c25真实0error/7warning/249info；仅清理dead routes/imports与无效断言/未用fixture参数后c26最终0error/0warning/244info，exit0/sourceStable。Go启动保护直接9个不同顶层用例跨两测试帧通过（39含子项PASS事件），旧API其余源及≤145 SQL字节未改；未重复全仓测试。

此冻结帧产物均实际构建exit0/sourceStable：

- APK：`D:\Project\birdtie\work\remaining-execution-20261014\apk-build-typed-auth-bc9\birdtie-batch-c.apk`，SHA256 `e4dccdd0c8f76bdddc4689512081245706d4e12a11ab865d488c419f25886dfc`。
- Web：`D:\Project\birdtie\work\remaining-execution-20261014\web-build-typed-auth-bc9\artifact`，42文件，main.dart.js SHA256 `df12ee201e968adc16bf02b8ebfa54711ee3e4ca8f2c4e5427e39c1d2337c40c`。
- Windows API：`D:\Project\birdtie\work\remaining-execution-20261014\api-build-runtime-c05\artifact`，12程序，api.exe SHA256 `c6113b2e2c8ffbd0f7141aace7c767ae0bc8effaf491fc29f694329dbdfaf91c`。
- Linux amd64 API：`D:\Project\birdtie\work\remaining-execution-20261014\api-linux-runtime-c01\birdtie-api-linux-amd64`，SHA256 `b78cb8ea534cc9fa457d622f3e333d7792fea0a6054210163cf9e4c95bbb1613`；仅实际交叉编译，Linux服务执行与部署NOT_RUN。

APK/Web仍明确本机profile、API18113，尚不是杭州最终包。新API在原verified-owned QA核对name/OID/owner/marker及原schema后实际启动；同一只读原生schema/type/OID检查用于启动和每次readyz。后台维护现真实默认OFF、未启动提醒/purge线程，原全部public表计数前后相同，未应用新migration、未连Civu源库。actual HTTP8项只读检查PASS；Web7357实际JS hash匹配。手机c641566b实际-r安装bc9、保原数据、reverse18113、font1.0，实际前后foreground守卫截图已核看；只证明原生首页/底图/诚实缺图符号，不证明媒体、创建、真人或组织角色动作。7张组件样例另标synthetic/not device；部分fixture字段/图标仍缺字形，完整正常CJK/辅助技术/键盘/所有错误态实际验收NOT_RUN。旧图和当前图镜头一致性未证明，不伪称严格前后验收。

不可变检查点 [receipt](../testing/evidence/remaining-execution-20261014/batch-c/checkpoint-c5-c6-inline-origin-runtime/receipt.json) 绑定所有命令/cwd/exit、首失败、源码帧、产物与截图SHA。原256任务对象仍一致：183 DONE、47 TODO、12 PARTIAL、13 BLOCKED、1 IN_PROGRESS，73未完整闭环；代码子片不自动关闭原完整验收。正常字号、不录屏、中英混合语音延期；全站体验、真实视觉/媒体权利、双账号/经营角色、iOS、Web真实媒体验收NOT_RUN，Closed Pilot/Consumer Beta仍NO。

## 2026-10-15 杭州来源真实抽取、媒体完整性与部署准备

这是后来直接人类授权的原Civu升级/真实数据适配接续，来源NOW027/PG25与既有迁移矩阵；不将旧MIG001历史规划DONE冒称真实ETL已完成，也不借UI父项豁免独立AGE/AIR或发布门槛。原授权消息ID、原文件和AES备份receipt绑定于hangzhou-handoff01，原入口为`https://47.110.154.133`。用户授权受控内部升级，不代表全站消费验收、真实模型、云盘订单或不可逆删除批准。

已实现固定29表/396列抽取器，校验561条实际metadata及约束、REPEATABLE READ READ ONLY、固定已验证隔离container/DB、完整原值/关系/Decimal精度；16合成测试PASS。root实际真实source SQL/结构守卫/抽取exit0、sourceStable：20 users、21 identities、339 cities、5511 places、189 saved、34 journeys/181 arrangements、3 themes/12 entries、103 notes/215媒体关系、407 assets、26 thread/5 legs。完整受限source.json/manifest只在本机仓库外CurrentUser DACL私存，密码/身份/私人正文/媒体路径未复制仓库或日志；源库与目标库零写，不是已经导入20个新账户或5511个可公开地点。

媒体matcher实际7直接纯检查PASS（另外早版6项初检查曾通过，非额外新场景），再将1,638,819,840bytes原AES-GCM媒体备份直接解密流入安全tar matcher。实际cipher/plain SHA及GCM认证、reader exit0、完整837成员/828文件核对。按DB storage_key精确匹配，335/407选择项通过大小/MIME magic完整性；剩余72项保留各差额，原因交集不可累加：21缺key、25MIME不符、24未知MIME、26未知size、3缺owner、21非ready。旧ready/rights/public_url从未转换为新授权；没有提取明文媒体、没有扫描或清EXIF，也没有导入/公开媒体。真实扫描、真实owner proof、用途/保留/公开权利仍NOT_RUN/NOT_AUTHORIZED。actual过程见 [抽取receipt](../testing/evidence/remaining-execution-20261014/batch-c/hangzhou-extractor/real-read01/receipt.json) 与 [流匹配receipt](../testing/evidence/remaining-execution-20261014/batch-c/hangzhou-media-matcher/real-stream01/receipt.json)。

部署代码现补显式native库名、Civu拒绝、实际schema/OID ready检查、默认关后台维护；Compose仅本机绑定、Caddy可导入snippet保留原TLS，修Nginx1m与实际图片base64传输上限。仅本机owned QA运行，未改杭州Caddy、未启新目标库/服务、未切流/停搜索或下云订单。新Linux二进制已产出，实际远端部署仍NOT_RUN。旧auth审计确认18无密码、1 bcrypt形状、1不支持hash；旧phone固定开发码及email直接verified不能作为新身份验证。合法密码/真实SMS或受信IdP、永久用户mapping与新bts Session桥仍需实际实施，不复制JWT/refresh/Session。

此检查点之后继续146稳定native迁移台账、唯一owner映射与本人旧内容档案；保留83存活Notes多地点/串文/空标题/未知发生时间及20删除tombstone，不造当前Moment、到访、公开图或地点许可。146及相应Postgres/CLI/HTTP当前只精确登记待编码，未应用、未当新服务可用；后续源码/产物另立实际帧。手机最终远端包在真实身份和迁移接线完成后再合包；仍可独立做稳定映射/归档与原source reader，不等待缺失SMS而停所有代码。


## 2026-10-15 原生146与本人旧内容档案自然检查点

接续当前未提交工作树及原任务单，保留原18项整改/所有任务ID、依赖、历史失败和发布门槛。用户授权的首页接线协调已发送一次并冻结相关Now源；本段是新的实际代码/验证/交付增量，不将前面时间点的NOT_RUN或未实现改写成当时已完成。当前原队列仍256项：183 DONE、47 TODO、12 PARTIAL、13 BLOCKED、1 IN_PROGRESS，73未闭环；原task对象与batch-a起点完全相同，scope只增加元数据，没有批量关闭任务。

新增原生模型、146 DDL、迁移CLI、Store/API及设置「本人内容 → 旧内容档案」列表/正文页。永久source-ID映射绑定本人Account/PersonalAgent、原版本、原摘要、原导入run；导入默认账号与Agent暂停、私人资料、UNCLAIMED，不搬手机号、邮箱、密码/hash或旧Session，不按同邮箱合并。原文、空标题、未知日期、多地点、媒体/位置/记录组/路线段按历史材料保留；删除只留墓碑且DB正文为空/关系NULL。旧公开标记、地图开启、坐标、URL不成为当前公开/地图/Memory授权；媒体仅UNAVAILABLE，无占位照片和自动取图。

本人只读接口GET `/v1/me/migration-archive`及`/{id}`已沿原真实PERSON/Session接线，20行分页/21行探测，cursor绑定本人和全局来源摘要。账号/源变化、等待后撤权、过期Session、编码尾变化及外来selector拒绝正文，组织身份不借本人接口。native首帧发现首次Session capture等待账户锁会吞掉暂停→恢复ABA，实际修为同事务先无锁原Session+source，等待后复核；编码尾先原Session再source proof，防最后等待窗口。没有修改通用Session权限或放宽源检查。客户端同本人续期重新读当前资料；账号/工作区/来源/provider/listener ABA与迟到GET永久退休旧视图，借用客户端不close。适用UX-CHECK-01/03/06/07/08/09/10/14（正常字号范围）/16。

真实受限目录source/manifest已由本批实际编译CLI执行prepare：20 owners、103 notes，其中83现存、20删除墓碑；SHA/schema/snapshot与原396列/29表完整对账，原输入字节未变，状态`PREPARED_NOT_IMPORTED`。真实JSON与密钥仍在原CurrentUser受限仓外目录，仓库仅计数、摘要与执行证据。这是实际真实输入预检，尚未向native staging/运行库导入或激活旧用户。Place/189收藏/34Journey/3Theme/407媒体/Auth identities仍按原依赖待适配，不借已导入账号的说法跳过权利、CRS和认证。

| 本批实际检查 | 结果与范围 |
| --- | --- |
| Go直接相关 | 29不同顶层 PASS，含84子事件，当前API源码帧、0FAIL/0SKIP；启动/146 ready依赖、closed domain/CLI、非native importer、档案HTTP及原普通人控制响应。首广选择器误含22 unrelated native SKIP的帧保留，随后收窄实际执行，未把SKIP计为PASS。 |
| 原生PostgreSQL | retained native02：4顶层+2子检查 PASS，独立随机146子库中的原文/分页/本人隔离、账户ABA/过期、编码后证明、导入故障回滚/原run幂等/不可变/非空down拒绝；首ABA失败保留。后续nil-context guard与HTTP尾序直接检查/只读review通过，不称新全套native。thread与部分stale/foreign cursor负向仍NOT_RUN。 |
| Flutter直接相关 | 51不同普通字号逻辑的最新PASS，38档案+13受影响的旧Settings/auth，跨direct01/02/03/05/06/07/visual08帧，不是一次全站或最终源码全套测试。首AppBar缺真实签名、背景/前景标题finder、旧无origin凭据fixture及旧跨origin GET期待失败均保留并针对修复；未弱化生产auth。direct04拼错文件在调用Flutter前NOT_RUN，不计测试。 |
| Scoped analysis | c28实际7个相关Dart文件0ERROR/0WARNING/65INFO；c27的1unused import warning保留。 |
| 真实输入CLI prepare | exit0，20/83/20，未连接数据库/未导入/未激活/未搬媒体。 |
| 当前运行库 | 仅核验过的owned QA146，原263已应用DDL与全部原表计数不变，新3表空；实际当前二进制启动/readyz核146列类型和immutable/deferred binding triggers，提醒/purge默认OFF，原Civu不作运行库。 |
| 当前HTTP | 6实际只读case PASS：ready、无Session列表/详情、无效凭据、无writer、原origin CORS；没有新Session、archive写入或真实旧owner body E2E。 |
| 构建与交付 | Windows API/工具13个exe、Linux API+guarded CLI2个、Android profile APK、Web42文件均exit0/sourceStable；Linux仅cross-build，远端执行NOT_RUN。APK已`-r`装preview包保留数据，font1.0/reverse18113；Web7357实际main.dart.js字节匹配。 |

本批产物：

- APK：`D:\Project\birdtie\work\remaining-execution-20261014\apk-build-legacy-archive-bc10\birdtie-batch-c.apk`。
- Windows API/CLI：`D:\Project\birdtie\work\remaining-execution-20261014\api-build-legacy-archive-c06\artifact`。
- Linux：`D:\Project\birdtie\work\remaining-execution-20261014\api-linux-legacy-archive-c02`。
- Web：`D:\Project\birdtie\work\remaining-execution-20261014\web-build-legacy-archive-bc10\artifact`。
- 封存总证据：`docs/testing/evidence/remaining-execution-20261014/batch-c/checkpoint-legacy-owner-archive146/receipt.json`（SHA 9f91b8937b0f68f18d92369297350f630cbba5ae280febb7b3e912eeddcd64b7）。每个artifact的大小/SHA和实际命令/退出码在对应receipt。

根已查看实际已装phone Now图与3张修正后的390×844隔离档案正常/历史滚动/权限PNG。新页没有历史before，不伪造对比；原18项同数据/镜头前后验收仍保留未闭环范围。前一组footer字体不足/两图未区分滚动的截图保留，visual08补清楚的「隔离样例·非真实旧用户·NOT_DEVICE」和真正下滚历史图。组件不是手机真实旧owner内容验收。没有录屏，没有大字号专项；中英混合语音按用户延期。

NOT_RUN：旧owner真实fresh认证/认领/激活、真实native durable staging apply、真实媒体scan/EXIF/rights/派生/上线、Linux/公网Caddy/TLS/OIDC/current包真机远端、最终增量/切流/退旧服务、iOS/原商店签名覆盖升级及全站所有角色/键盘/辅助技术/错误分支验收。Closed Pilot与Consumer Beta仍NO。下一本机可编码片为普通Activity CREATE原事实/未知结果恢复和Collection UPDATE/WITHDRAW不可变原操作事实，不把它们算作外部短信阻塞；原124/136/128-129/141已闭合实现继续复用。


## 2026-10-15 收藏修改／移除147：编码与合批编译优先

按用户最新指示减少相关测试，先推动实际接线和合批产物。原任务单C1/C3、NOW008–013、NOW018/021及当前BT-FIX-NOW-UI-001继续承接；原ID、依赖、历史DONE/失败与发布门槛保留。原队列仍256项：183 DONE、47 TODO、12 PARTIAL、13 BLOCKED、1 IN_PROGRESS，73未闭环，未把局部编译算父任务完成。

本批接好本人Journey/ThemeMap修改与移除：保留原集合ID，修改绑定当前具体revision及完整本人编辑稿；预览后明确确认，只提交一次。147新增不可变原操作事实，同事务保存原效果、审核记录和回执；核实只读原operation，不按当前标题/正文或withdrawn状态猜上次成功。结果未知时保留仅metadata的本人记录，只提供核实/停止；停止先于迟到提交可阻止原操作，旧批准不能换新operation。移除在最大revision仍可终止。源资料、借用来源、原Session及本人身份边界保留，普通续期保留本人稿并废弃旧批准。旧raw UPDATE/DELETE已引导至新控制流程，API与客户端成套交付；124创建、136加入已有集合、128/129公开关闭和141副本继续复用。

实际合批检查：

| 检查 | 实际结果和范围 |
| --- | --- |
| 最小界面验证 | minimal03两项普通390宽 PASS：具体修改确认/原ID/7→8 revision/一次PUT；UNKNOWN禁止新提交，捕获旧保存和确认回调不重放。minimal01/02的真实等待超时保留，最终仅修fixture在DialogRoute退场和真实I/O结束后断言，未等待TextField光标无限动画；4产品源码未因fixture失败改动。其它已定义用例未运行。 |
| 最小原生验证 | retained binding-native04原批准A→STOP A→绑定B拒绝，1项 PASS；此前native02有2顶层PASS及2顶层FAIL，错误fixture已修而没有重跑全套，保留原日志，不改称全通过。 |
| 启动与代码检查 | 只选既有startup一主测试 PASS（含15子用例），147真实10列类型和enabled guard与145/146共同接入；4个变动Dart产品文件分析0ERROR/0WARNING/99INFO。测试fixture之后改动仅按SHA对账，未为此重复分析或全套测试。 |
| 编译 | Windows API/工具13exe、Android profile APK、Web实际编译exit0/sourceStable，同源新147配对。Linux本批NOT_RUN，旧146产物保留。 |
| 本机接线 | 仅原owned QA应用147，核身份OID/owner/marker及原≤146 DDL字节，accounts/journeys/theme_maps直接相关计数不变，新操作表0行；实际当前exe启动和readyz成功，Web7357配对当前字节。未整检全库、导seed、导Civu、启付费/模型/媒体provider或公开部署。 |

产物路径：APK `D:\Project\birdtie\work\remaining-execution-20261014\apk-build-collection-control-bc11\birdtie-batch-c.apk`；Web `D:\Project\birdtie\work\remaining-execution-20261014\web-build-collection-control-bc11\artifact`；Windows API/工具 `D:\Project\birdtie\work\remaining-execution-20261014\api-build-collection-control-c07\artifact`。精确命令、目录、退出码、构建源码帧和产物SHA见各receipt；封存检查点 `docs/testing/evidence/remaining-execution-20261014/batch-c/checkpoint-collection-control147/receipt.json`，SHA 22309e3339066f3328aed6fbe5e6c5a7fd88f8afb15f5c0c04138ed2e308df8a。

未安排本批新装包、截图、全站验收或扩大测试矩阵，均记NOT_RUN；当前手机仍为此前bc10已安装包，不冒充新147手机验收。正常字号、不录屏、中英混合语音继续延期。原18项整改的同数据前后截图、各可达页/角色/键盘/辅助技术验收尚未闭环，编译不代表全站完成。Closed Pilot/Consumer Beta仍NO。

下一本机可编码片：旧124 CREATE原tiny事实＋STOP桥、Activity CREATE原操作事实和已创建ID恢复；Shared Now创建宿主普通token续期的草稿作用域差额保留，未偷改已冻结共享首页。真实Civu认领/导入与媒体权利/scan/派生、公开Hangzhou切流及原商店升级依赖继续记录，不把缺短信当作这些本机代码的统一阻塞。

## 2026-10-15 用户启动动态通知和好友实际执行

本聊天用户明确要求开始执行，并指出看不到 Codex 工作。本组已实际编码、必要直接检查、编译和配对本机服务；继续原 `BT-FIX-NOW-UI-001` 及 [社交闭环清单](../product/BIRDTIE-SOCIAL-CORE-EXECUTION-SPEC-2026-10-15.md)，不把前一轮分析接入记成功能完成。原256项、历史DONE/失败、依赖与发布门槛保留。

实际改动：

- 148 扩展原144 Moment操作命名空间，新增 `PUBLISH` 与 `UPDATE_PUBLIC`，同事务保存原操作事实、效果和审计；原回执先于当前对象状态核实，原逻辑操作重放不另写效果。公开编辑仅改标题/正文，保留原ID、城市、地点、发生时间、关系和发布日期。144/145/147原文件未改，148 down拒绝丢弃已有公开操作历史。
- 本人动态列表和公开详情接“查看动态”“修改或删除”。新管理页读取确切本人对象，具体预览后才提交；取消、编辑、换号或来源退休不能复用旧确认。只显示和恢复这条动态对应的操作，不把共享controller的其它草稿/动态预览当本页批准，延迟销毁仅退休本页仍持有的预览。
- 公开正文版本变化沿145退休旧图片；确认处明确显示图片将撤回、之后可重新添加。照片slot纳入目标快照；没有暗中改旧图片批准版本或关闭触发器保图。本组未实现继续保留图片的再次批准分支。
- Now纯文字发布接原MomentControl的稳定操作和本机metadata恢复，不再用当前published状态猜那次提交。检查公开前对比已保存权威标题/正文/城市/地点与当前输入；有未保存修改时要求先保存，避免显示B却发布A。恢复绑定原controller、身份代际、创建scope及确切operationId；无法知道归属时转本人全局恢复，不猜其它CREATE。成功后提供“查看这条动态”并重新读取当前地图来源。
- 通知点击标已读后独立读取服务端剩余未读计数，保留原列表/游标/滚动；失败保留上次权威计数和真实重试按钮。好友行新增资料/管理菜单，管理页复用原SocialInboxSection的移除、屏蔽、举报、亲密好友及申请/会话动作；取同一联系人来源的transport/base并绑定来源变化。没有新增收件箱入口。
- 新API startup/readyz识别原Moment操作列及148公开动作guard；当前客户端、API和本机QA已配对。没有联系真人、试发消息、导入Civu或启用付费/模型/视频/推送provider及后台维护。

证据根：`docs/testing/evidence/social-core-execution-20261015/`。精确命令、cwd、退出码、执行前后源码帧和完整日志各在receipt；`source-before.json`保存本组开始前选定文件SHA。各结果分别表达：

| 维度 | 实际结果与范围 |
|---|---|
| 代码实现 | 上述文本发布/公开修改/删除管理、通知计数和好友菜单已接线。首批单图Now发布、候选审核通知结果入口、完整分页等仍未完成，不关闭父项。 |
| 必要直接验证 | `api-direct01`原Moment输入/回执、新公开边界及startup缺schema检查：27 PASS事件、0 FAIL/0 SKIP，exit0。`native01`仅新发布→编辑→删除→原发布事实保留的一个实际隔离PG链 PASS，原已有QA不由测试迁移；真实用户/真人关系流程不由其证明。 |
| Flutter验证 | 原Moment协议9项在client-direct01 PASS；其同次新增2个widget失败保留。仅修异步等待及测试滚动选择器后，client-target03两个新公开管理widget PASS，覆盖具体修改/稳定ID/旧确认不提交、删除确认前零写和换号退休。social-target02两个通知/好友菜单widget PASS。通知首跑因旧fixture缺origin没登录而失败，改本测试本人同源Session，不放松生产认证。不是全站回归。 |
| 代码检查 | analyze01确实1ERROR，定位错误落入图片函数的operation引用；修到确切保存方法后analyze02九个改动Dart产品文件0ERROR/0WARNING/163INFO、exit0。此前未引用旧_inbox warning已去除其死方法，现有通知组件保留。Impeccable一次机械检查[]、diff-check退出0，均不代表视觉验收。 |
| 编译交付 | Windows API、Android profile APK和Web profile均exit0/sourceStable，产物路径及SHA在api-build01/apk-build01/web-build01。仅编译实际改动所需API，未为无关CLI重复编译。 |
| 本机配对 | runtime01核原owned QA名/OID/owner/marker及≤147 DDL SHA后仅应用148；账号、Moment、公开图片、原操作表计数不变。新进程startup/readyz实际成功，后台维护及外部provider关闭。web-runtime01核原服务器PID/出生时间/命令后配对7357，实际提供main.dart.js字节与新包相同。 |
| 视觉和真实交付 | 本组真机安装、截图、实际键盘/辅助技术、真实两账号好友与动态操作、生产/远端/iOS验收均NOT_RUN。没有录屏，没有大字号专项；中英混合语音继续延期。 |

当前可用产物：APK `work/social-core-execution-20261015/apk-build01/birdtie-social-core.apk`；Windows API `work/social-core-execution-20261015/api-build01/birdtie-api.exe`；Web `work/social-core-execution-20261015/web-build01/artifact`，本机预览 `http://localhost:7357` 配对 `http://127.0.0.1:18113`。新APK尚未安装，不能沿用手机旧包验收。

后续继续原SOC清单：单图与私人材料分开的明确公开批准、候选审核结果的本人读取与稳定入口、完整动态历史/关系筛选及互动；旧Activity/Collection创建原事实、迁移和全部其他任务也保留。当前没有把本组或20个检查点称全完成；Closed Pilot/Consumer Beta仍按原门槛为NO。

### 2026-10-15 同轮继续：Now 单图和本人审核结果已编码并合批编译

上节为第一组的保留快照；本节接续补完其两个具体代码缺口。适用 UX-CHECK-01/04/05/06/07/08/09/10/11/12/13/14/15/16；真实视觉、键盘和辅助技术仍没有本次运行证据，正常字号和不录屏范围继续有效。

- Now 有单张本机图片时，先明确检查并确认公开文字，提示图片仍未公开；文字成功后在原创建预览内嵌既有145编辑器，载入这张确切本机图，重新确认权利、保留期限及真实派生图后提交图片。没有自动继承私人附件许可，没有新增常驻首页大面板或另开创建表单。视频仍明确阻断这条发布路径，多图和视频新合同尚未实施。
- 嵌入图片编辑器保留原默认完整页行为；本人素材读取失败后可显式重试，确认默认不勾选。具体图片批准与原 Moment revision、账号/来源、本机图片revision/选择代际/SHA相连；服务端规范JPEG SHA与原本机像素SHA分别绑定，未知结果沿原145 journal核实。只对本页面实际准备过且完全匹配的 SAVE 成功事实清原素材；恢复其它历史操作不清新图。成功文案为“图片公开操作已完成，可打开动态查看当前图片”，不把历史回执称当前图片展示验收。Now“查看动态”改取原Moment客户端同源，详情子树绑定原来源代际。
- 增加本人通知审核结果 GET 和稳定结果页，无新增DDL。同时校验 Inbox recipient、候选 submitted_by、原资源ID以及 routed kind/source/typed candidate；只返回当前终态与当前可见的正式目标ID，不返回候选正文、来源网址、审核备注或通知缓存摘要。正式地点/活动失效则省目标ID并保留审核事实；编码后重新检查原Session代际和来源。GET不写已读，标已读继续沿原动作；链接只用当前 resolved_place/activity_id，点击时重新读取结果和正式详情，换号及迟到结果不再显示。

| 维度 | 本次新增结果 |
|---|---|
| 代码实现 | SOC-06单图和SOC-07候选结果差额已接线；SOC-01至05/通知计数/好友菜单第一组保留。SOC-08/09/10既有申请/关系/会话/分享动作复用，未新建替代社交系统；真实完整流程仍待整合检查。 |
| 客户端直接验证 | phase2-client02四项PASS、exit0/sourceStable：嵌入真实本机图/初读失败可显式重试/零自动上传与默认许可unchecked；具体派生图预览后确认仅写一次并回同一原操作；审核结果严格通知绑定、未知私有字段拒绝、正式目标失效保留事实、换号迟到内容隐藏。phase2-client01三项早期PASS也保留。没有截图取证或全仓测试。 |
| 本机数据库直接验证 | phase2-native04单个实际隔离PG链PASS，0FAIL/0SKIP：本人结果、已公开真实resolved地点、wire无私人字段、GET零已读写、目标隐藏后原快照409、当前审核事实保留/ID退休、他人提交404、Session撤销不复用。前三次失败均为新fixture未遵循已有114权利约束、通知detail非空及places合法hidden状态，修fixture后保留原失败证据，未放松产品约束。 |
| 必要代码检查 | phase2-analyze02仅六个改动产品Dart文件0ERROR/0WARNING/118INFO、exit0/sourceStable；选定改动diff-check退出0。只读协作检查发现双重提交tx、派生图/原图SHA误比较、重试无法继续和历史成功文案问题，均修复。 |
| 编译交付 | api-build02 Windows API、apk-build02 Android profile APK、web-build02 profile Web均exit0/sourceStable，产物SHA与精确命令在receipt。Android包未安装；未重复无关CLI/Linux/iOS编译。 |
| 本机运行 | runtime02核原owned数据库身份、已有148文件/guard和旧进程身份后换当前API02，readyz实际成功，无DDL/数据写入。web-runtime02核前一同源服务器PID/出生时间/命令后提供新Web02；实际main.dart.js字节与此产物匹配。后台维护与付费/模型/视频/推送provider仍关闭。 |
| 未运行范围 | 真机安装、真实认证/双账号好友闭环、真实单图详情和地图显示、键盘/视觉/辅助技术、生产/远端/iOS均NOT_RUN。编译和上述局部验证不表示第一批完整体验或发布验收完成。 |

最新产物：Android `work/social-core-execution-20261015/apk-build02/birdtie-social-core.apk`；API `work/social-core-execution-20261015/api-build02/birdtie-api.exe`；Web `work/social-core-execution-20261015/web-build02/artifact`。本机预览 `http://localhost:7357` 已配对当前API `http://127.0.0.1:18113`。完整历史、城市动态、筛选/互动、动态通知清理、多图/视频和串文继续保留原SOC-12至19实施项；SOC-11/20可用编译证据已增量保存，完整交付验收没有关闭。原父项IN_PROGRESS、其余255项和Closed Pilot/Consumer Beta门槛不改。

## 2026-10-15 接续：GitHub、固定码、SOC核心与真实公网交付

公网唯一入口已成为 `https://47.110.154.133`，当前 API 为 `social155-20261015-build05`／原生155；Web02与Android APK02均指向该来源。APK SHA-256 `53b79dc5813c9ad1bd64274043e16d7b55f86056dd5c6606a08568f31b5f6b0c`，API SHA-256 `d4f81a65f0059328baabbbec587e9d7af40ca600d45948378b2b54ad0caa0e3a`。原 Civu app/data 保留，预览包已安装。采用用户指定固定验证码 `123456`，18个旧手机号绑定原owner；真实旧账号两次登录与16Note／11Journey／2ThemeMap读取通过，首次接续1人、Agent仍暂停、两个会话均退出。

新代码补齐城市动态、本人／作者完整历史、城市／关注／好友PUBLIC筛选、点赞评论与通知管理、有序九图或一视频、2–20条同事务串文；Now沿用原输入框和首页面板，包含已发布查看／正文修改／撤回，结构化本机草稿和媒体续期隔离。普通社交沿原Connection／Follow／聊天／143分享领域；没有复刻Civu ACL或启模型／付费provider。最后真实远端检查补发现并修复“城市动态分享仍强制Place”的查询差额，隔离PG一项与API05好友／聊天／分享有限流程均PASS。

实际Web完成单图动态发布／详情／编辑／撤回；Web02关闭草稿PASS。最终APK02真实竖屏Now创建／固定登录恢复／单一输入框／图片入口启用／关闭／退出PASS，原旋转恢复、字号1.0。服务器实际九图／视频Linux转码／两条原子串文流程在未变的相应模块源码帧通过；不冒称整站或手机全部媒体触摸验收。图库权限、手机播放器／发布、好友聊天分享双角色UI、辅助技术、iOS和后台系统推送仍未运行。正常字号、不录屏、中英混合语音延期继续有效。

旧Civu writer已停且禁用（failed/timeout、MainPID0、8080拒绝）；四Pelias和旧网关停止，旧库／媒体／容器／配置保留。停服务90秒SIGTERM超时记录及第一次状态断言失败保留。完整旧数据适配仍PARTIAL：20永久owner映射、103Note／34Journey／3ThemeMap／189SavedPlace私人档案已提交；5511Place／339城市合法当前映射、407旧媒体的72差额及扫描／权利、旧关系／消息／组织和2个仅邮箱账号未全接续。私人档案不等于当前可编辑／公开对象；停写后最终增量核对尚未完成。原Activity／Collection创建原事实等历史差额和发布门槛不因这批关闭。

详细事实及首失败保留见 [当前GitHub、远端与迁移记录](../reports/BIRDTIE-GITHUB-REMOTE-MIGRATION-2026-10-15.md)。本轮源码、测试、迁移、部署脚本与报告一起提交原GitHub分支；全量检查点位于 `docs/testing/evidence/social-remote-migration-20261015/checkpoint-final.json`。保留原256项、历史状态与父项IN_PROGRESS；Closed Pilot／Consumer Beta仍NO。
