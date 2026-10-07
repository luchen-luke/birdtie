# Agent Memory 本人纠正 V5

## 1 原要求与边界

权威来源为 `docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md` 的 AGE-011（627 行）及 AGE-063（1967 行）。本人可检查具体版本后修改、纠正、拒绝保留或删除 Memory。当前五态 Candidate 接口可复用；007 未完成的概率校准不是本人纠正的写入许可，也不是本切片的实现前置。原来源、AC 与发布门槛保持。

默认机器写入、模型出网、Vision、A2A 仍关闭。模型分数不能批准写入。本功能不生成校准概率或 INFERRED ACTIVE；Confidence=1 仅来自既有 EXPLICIT 人工声明规则。

## 2 本人交互与具体版本

设置保留“管理我的记忆”直接入口，不新增导航或长表。页面读取原本人 Memory/Candidate，选择对象和动作，形成具体预览，再由本人确认该版本。NEGATE 仅接受六个已支持类别及本人明确选择，类别来自结构化 key，不能从 summary 猜测。

预览展示原目标、版本、全部 affected Memory 原正文、补充值和期限，本次截止、动作后的后果；候选只展示真实类别及 affected kind/id/version，不制造来源正文。MEMORY REJECT 与 DELETE 使用原 DELETED 丢弃并清正文；Candidate REJECT 使用原 REJECTED。REJECT 操作回执和审计保持拒绝意图，不新造 Memory REJECTED 状态。

明确不偏好某类活动会退休该类待审推断、失效候选，保存本人纠正，并持续阻止该类再次提出。纠正声明有原 365 日上限；该声明删除、到期或编辑都不会恢复旧候选/旧具体批准。本切片没有重新允许类别的 writer。原 Moment、Profile 与本人其他独立声明不被删除。

应用 UX-CHECK-01–16：复用中文直接入口和既有领域动作；先整理本人有效资料；草稿、具体版本、提交与权威回执分开；完整展示后果；未知只核原 ID；主体/Session/连接变化退休；适配 320 宽、动态字号、键盘、48dp、深浅主题与语义标签。Widget/mock 验证不等于真机或辅助技术实机验收。

## 3 Native authority 与事务

新 `agentmemorycorrection` 闭集 DTO 复用原 Memory 校验与闭集类别。Input 包含 operation UUID、target kind/id/version、action；仅 EDIT 有原 PutInput replacement，仅 NEGATE 有明确 category。预览必须完整双向对应 affected MEMORY 与 owned Record，不允许缺少原正文再批准。

原本人 Session/Agent/metadata/current Memory/Candidate 权威检查与原 CAS 复用。事务保存 authority/binding/plan digest；target/source/owner/session/profile/metadata/version/xmin/正文改变或 ABA 均拒绝原批准。等待后使用原 PostgreSQL clock 末核目标 TTL、来源权限和原 deadline，不增加容忍、retry 或机器授权。

确认原 UUID 的相同操作只产生一次领域变更和审计。同事务调用原 Put/Delete/Candidate lifecycle helpers，记录 committed receipt、清预览私密 input，并按 NEGATE 写持久 suppression。新 Session 只能读取历史 metadata，不能恢复原具体批准。历史 COMMITTED 与 currentResultMatches 分开，历史成功不是当前正文仍相同。

`ReadOwnMemories` 在所有关系/行读取等待之后再取 PostgreSQL 当前时刻重投影已过期状态；原 ID/version/body/native row/xmin 保持，不能把早取 now 当当前有效。

## 4 唯一 094 schema

新增三表：

- `agent_memory_corrections`：具体版本预览与 once receipt，闭集 private input，仅待审时保留；提交或真实过期后 scrub。
- `agent_memory_suppressions`：owner + Agent + ACTIVITY_CATEGORY + 六类，绑定已提交的具体 NEGATE；无清除/重新允许 writer，防止换来源/ID/旧批准复活。
- `agent_memory_source_invalidations`：owner + 原 source type/id + 原 source epoch + finite created time 的追加 metadata；无正文/token，只有 source native mutation 触发追加。

所有生产 manual/single/multi/旧接受端口检查实际 094 table/function/enabled trigger 与持久 suppression，缺失/disabled failclosed；不仅依赖新 SQL INSERT trigger。直接虚假 COMMITTED、变更 suppression/失效记录或延期旧预览由 SQL guard 拒绝。

Moment/Participation/Saved mutation trigger只追加 metadata，不锁或修改 Candidate/Memory，避免 Moment→Candidate 逆锁。既有当前 resolver 仍使用原 source fingerprint/xmin/版本；先拒绝检索支持和新提交，当前 read/refresh scrub 派生支持。source marker 不代表后台消费完成；AIR018 的 bounded 后台消费/清理尚未在本切片实现。当前没有实际 vector cache/index 或 INFERRED ACTIVE writer可被误称已完成。

unused down/reapply 必须精确保留原 137 public 表、全部原行/xmin/semantic catalog；任一新表已有使用/history，down 拒绝，不删除历史。仅隔离本地运行 down。

## 5 HTTP 与客户端恢复

实际 `httpapi.New` 注册：

- POST `/v1/me/agent-memory-corrections/previews`
- POST `/v1/me/agent-memory-corrections/{operationID}/confirm`
- GET `/v1/me/agent-memory-corrections/{operationID}`

新接口使用实际 bare DTO；原 Memory/Candidate list继续原 data envelope。新客户端独立传输解析，GET 真空 body，POST 精确 JSON，闭集 method/path/body，不扩共享 Candidate API。

Secure journal 只存 env/owner/Agent/Session hash + operation/target/version/action/phase/digest/deadline 引用，POST 前原子 reserve/readback 并再次核身份与期限。不存 token、query、answers、Memory/Moment/review 正文或类别选择。损坏/存储失败/身份变化 0 新 POST，双 State 不能替换未知操作；迟到删除只能 exact captured reference。

关闭重开只 GET 原 operation metadata；不恢复旧 preview/grant。404 不证明原请求未生效，保留 UNKNOWN 与引用，禁止新 POST；无解除 writer 时如实说明待核实，不能客户端造 CLOSED。预览到期清本机具体材料但保留引用，由原 GET 判断 native 状态。

## 6 验证与限制

后端定向 `native3` 279 PASS/five0；额外 captured-only 原063 roundtrip与 reserved INFERRED NEGATE 各 1 PASS/five0。970 API+SQL+seed输入冻结，原失败和 root 独立反例全部保留。最终整仓与客户端收尾见研究审计及证据附录；定向结果不是整仓或上线结果。

证据位置：`docs/testing/evidence/agent-memory-correction-2026-10-05/`，工作冻结：`work/v5-age011-memory-correction/backend-final-freeze1.json`。手机离线，无 ADB；OS Secure/TalkBack/VoiceOver实机、外部运营与部署未运行。Closed Pilot/Beta NO。


## 定向客户端收尾（2026-10-06 本地）

最终 `client-target12` 为78功能+5loading PASS、test/analyze0，342源当前/copied/after全同；16 Widget/mock PNG保留。原Secure read实际1POST、UTC归一年0、旧expiry材料、future server迟到短lease、clock回拨与font3本次完成后结果868偏移的真实RED均保存后按原否定断言GREEN。Stopwatch只保守缩短本机review可用期限，不改变server deadline、Memory历史TTL或批准权；关闭重开仍只GET原引用。明确已核实本次结果才移到可见区域，不重置正在编辑的异步草稿。

根初次whole094虽然11003Go PASS/five0，父库保留断言为FALSE（旧MomentContext来源修改附带metadata0→5）；该原失败保持，等待精确fixture隔离与新帧。最终whole Flutter由root独立进行；本段不提前声明整仓或任务DONE。证据README与immutable `target-delivery1/manifest.json`列完整raw/source/DB和限制。


## 旧 fixture 隔离与中文收尾（2026-10-06 第二冻结帧）

根在相同970冻结源和完整父库保护下独立定位：原两个 MomentContext 测试自身 PASS，但把本应只在隔离库追加的 source invalidation metadata 留入父库，0→5。现仅两获准旧测试在原 disposable guard 后复用 `ownedMigrationDatabase(t)` / `v4PrivacyHTTPDatabase(t)`；HTTP pool 成功后只日志实际隔离库名。原 assertions、权限、ID 和 cleanup 全保留，不删除 marker，不修改094 guard或放宽完整性断言。

实际 `context-isolation6` 原 same2 pattern `^TestMomentContext(HTTP|Links)Integration$` 为2 PASS/0 FAIL-SKIP，test/vet/build/两CLI均0；原137表48行/all xmin/catalog、094全部140表末 rows/xmin/catalog、unused down/reapply全部精确保持。RAW发出 migration与HTTP两自有child，其SQL absence及parent DROP后独立absence均为0。完整970源 only两旧fixture差异；原20 Go/SQL freeze1字节不动。`backend-final-freeze2.json` SHA `ddcafc824614806ec9a458bc913aa58b5212b2f151b0b858b50c09a2c9a0f149`；完整970manifest SHA `34b20553c48c3891070f2f0a307f1dc2b591bc4d1417532a68578ff80f745af6`。

根初次全Flutter为1603功能PASS/5旧入口FAIL/165 loading PASS，analyze0/test1、build未运行，原342输入稳定。五次tap原文字已mounted但在默认800×600视口外；三个原entry fixture在既有真实scroll/ensureVisible后pump并要求 hitTestable，再执行原tap。所有原声明、权限和A-B-A断言保留，不扩大viewport、不屏蔽miss警告。page生产代码仅两中文短语改“更换会话”“仅个人助理范围”，机器标识不变。

`client-target13`与缩进收尾后的 `client-target14` 均83功能+8 loading PASS，test/analyze0；342当前/copied/before/after一致。相对target12仅三个旧entry test和page两短语变化，另九owned Dart字节保持；13 Dart最终冻结。`client-final-freeze2.json` SHA `165edb367a7ccafb4a00f87aa6d842554f010d7245417590c81149949801dc02`；342manifest SHA `5c113880f3245c63289c8a32d2b3024b3cbd4a1bae4728de6f797a7c379ad912`。16新PNG仍为Widget/mock，正常字体/大字与IME流程通过实际scroll确认动作与完成结果可见；手机、真实OS Secure、TalkBack/VoiceOver NOT_RUN。

新不可变附录 `docs/testing/evidence/agent-memory-correction-2026-10-05/fixture-isolation-annex2/manifest.json` 保存174文件/13,215,730字节，SHA `d17e02e4e4351cf2e1804cdac9b31f42502e12081348f9d6b5d15b1ea3c59c29`，含当前35源码、精确commands/runnerbytes、完整父库row/xmin/catalog、实际SQL absence、target13/14 raw与16PNG、root两旧RED证明。旧1007文件target-delivery1及manifest不动。首preparer在LF/CRLF guard假设处于源写入前assert，Go未运行；该work-only失败脚本也保留，不计业务失败或通过。

本段只记录定向GREEN和第二冻结帧。根 whole094 frame2与全Flutter frame2已实际启动，完成结果需根独立proof另追加。旧整仓父库FALSE、旧五入口FAIL都保留，不以定向结果替代原整仓。默认模型/自动写/Vision/A2A OFF，Closed Pilot/Beta NO，未安装APK。


## 根独立整仓收尾与手机重连接续（2026-10-06，root38ny）

上述 RUNNING、未安装及用户离线记录是各原帧的历史事实。本轮根随后完成当前 frozen094 整仓：`go test ./... -count=1 -timeout=30m -json` 11003 PASS/0 FAIL-SKIP/packageFail，`go vet ./...`、`go build ./...` 及两个 CLI build 均0；970输入 before/copied/after/live 逐SHA一致，旧10922和上轮11003通过名字及multiplicity保持。原137表48行/xmin/完整semantic catalog、unused094 down/reapply、末全140表parent精确保持；341个RAW实际随机库由根独立SQL检查全不存在。原首次 parent marker0→5 FALSE及同源same2再现保留，未删除metadata或放宽断言；只隔离两个旧测试自有DB后的新whole真正通过。根证明 `work/v5-age038-resume/whole094-fixed-root38ng.json`，不可变1217文件归档 `docs/testing/evidence/root-joint094-fixed-code-local-2026-10-06/manifest.json`。

Flutter当前完整342输入 before/copied/target/after/live逐SHA一致；`flutter analyze`、`flutter test --machine`、Debug APK build 均0，1608功能+165 loading PASS/0 FAIL-SKIP，旧1534通过名字与当前83目标保留。五个旧Settings tap miss原RED保留，只三个旧entry fixture补真实scroll/pump/hitTestable，未扩大视口或消除原断言。真实registered native五JSON与最终client4互验另PASS，不重复计入1608。根证明 `work/v5-age038-resume/whole-client011-root38nh.json`；不可变370文件归档 `docs/testing/evidence/root-client011-code-local-2026-10-06/manifest.json`，16图为Widget/mock。

BT-V5-AGE-011按原 CODE_AND_LOCAL_VERIFICATION 已由根记DONE，其他252任务对象/source/门槛保持；闭合证明 `work/v5-age038-resume/age011-local-closure-root38no.json`。这不把007尚缺概率校准/INFERRED ACTIVE提升为完成，不把094追加最小metadata称已部署传播consumer或真实vector/provider删除。

用户明确重新连接手机后，根实际ADB安装并pull核对同一APK SHA `4611f432efd67ee5f911462eebb1b3a564bdd9449d2e26e3f0ceec20a7a0b9cf`，打开独立preview包并连接Flutter VM/DevTools；应用数据未清、原store Civu不动、已保存Mapbox配置复用。手机c641566b/Android16，独占新本地094合成API，不使用旧手机库。当前首屏PNG已实际捕获，`work/v5-age038-resume/phone094-first38nw/result.json`。初两次截图probe因Android16 window字段与多物理display前置warning停止，原输出保留；明确主物理display重试成功，不称app失败。此阶段只有安装/启动/attach/首屏与匿名设置登录限制真实观察；记忆修改/重启安全存储、完整Now六项、TalkBack/VoiceOver及Profile性能比较尚未验收。实际后续结果独立追加，不能从首屏或mock反推绿。

完成后018撤权传播与069普通Memory详情/reject API立即受控并行启动；复用本任务真源，不重复backlog。模型/provider/真实自动写/vision/A2A保持默认OFF；生产IdP、真实组织授权活动、HTTPS与生产地图、部署日志/值守、提醒运营和真实A→H仍缺，Closed Pilot Ready=NO，Consumer Beta=NO。


## 2026-10-07：本人活动偏好与当前明确记忆同体核对消费（AIR024，CODE_LOCAL）

- 在原「管理我的记忆」页每条 ACTIVE / EXPLICIT 记忆旁提供「核对活动偏好与这条记忆」，直接复用原 API transport 的唯一只读 `self-review` suffix。请求固定选择 preferredActivityTypes + 当前1条 memoryID + 空 policyFamilies；单个 registered SelfReview 响应提供同一快照，不拼独立 Profile / Memory GET，不创建新导航、授权或批准路径。
- 专用闭集 DTO 按实际本人 owner、Agent、选中列表对象的 ID / version / summary / source time / 已知区间，以及精确 1/1/0 selection、sections 和 FieldEvidenceSet 校验。wrapper UTF8 JSON 含换行 <=64KiB，来源声明/版本与实际所选字段一致；隐藏 StructuredValue / MemoryKey / Authority / grant 不消费。固定已封存 registered-handler wire 的原日期和未提供 confidence 保留；别的 selection（包括原 UNCONFIGURED policy wire）只作拒绝样本，不能换成这份成功。
- 页面中文并列双方明确声明、版本、实际已知来源时间、未知创建/拍摄/置信描述和短读租期。原闭集同活动类别 true/false 冲突保持 AWAITING_CONFIRMATION「待本人确认」；不从自由文字、GPS或照片推导矛盾，也不把未列出冲突说成全体无冲突。直接声明的 confidence1（若实际提供）不是事实概率或权限。
- 新读状态与原 correction draft / preview / journal / confirm 独立。token/owner/组织、直接 Page transport / clock / workspace 来源替换、通知 ABA、同 ID 对象替换、迟到/旧 serial、原快照期限与单调耗时不能复活说明或续期。关闭/读取失败/到期仅退役说明；修改记忆仍走原 fresh version review。活动偏好可返回设置「我的智能体」检查，不声称此处能修改资料。原单条说明统一为「这份单条来源说明不包含跨来源比较。」，不暗示过期或 INFERRED 行存在联合入口。
- 适用 UX-CHECK-05/06/08/10/16。320宽、2倍字、浅深主题、48dp、真实点击/滚动、原更正入口保留仅相关 widget 单位覆盖。有效缺入口 RED、首次67场景66PASS与1个新 preview fixture charset 失败均保留；只补该精确 fixture场景，不重刷67。另1条原独立读/journal控制和2条受单条 caption影响的旧场景通过，合计70个去重行为有通过证据；loading 不当需求数。首次整条单位命令仍 exit1，最终组合整套并未重新运行。
- 证据：`docs/testing/evidence/human-self-review-consumer-2026-10-07`，原 wire 是 SYNTHETIC_REGISTERED_HANDLER / synthetic transport，未重写时间或伪造 native 数据。既有 Settings 父 route 源替换仅下一帧 NotificationDestinationBoundary 退休，本切片只覆盖直接 Page/current 通知，父入口下一帧前 dispatch 集成保护 NOT_RUN，未在此修复。真实 PG/Session/ACL/Source 时序、媒体、多来源外部确认、IdP、手机/辅助技术/性能、全量/analyze/build 与发布验收均 NOT_RUN，整体 AIR024 PARTIAL，Closed Pilot / Consumer Beta NO。
