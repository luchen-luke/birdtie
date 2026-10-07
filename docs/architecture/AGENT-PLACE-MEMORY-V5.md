# Agent Place Memory V5

2026-10-03。本文件是 AGE027 私人 Place Memory 的唯一增量合同，复用 [Place 节点](PLACE-CONTEXT-MODEL-V4.md)、[Moment 关联](MOMENT-CONTEXT-LINKS-V4.md)、[单一 Memory 账本](AGENT-MEMORY-ARCHITECTURE.md) 与 [原生事件来源](AGENT-ENRICHMENT-EVENTS-V5.md)。公共地点社交聚合 V4-MOM002 是不同的可见性和验收范围。本项没有新增数据库表、领域 ID、消费事件目录、公开接口或长期位置轨迹。

## 已实现的信号与准确含义

| 信号 | 实际来源／basis | 可以表达 | 不能表达 |
| --- | --- | --- | --- |
| SAVED | 本人当前 `saved_items` Place 收藏；CURRENT_NATIVE_BOOKMARK | 当前保留了一条地点收藏 | 喜欢、到访、稳定兴趣 |
| CREATED_MOMENT_AT | 本人 private/draft Moment 的明确 Place 关联；CURRENT_NATIVE_MOMENT_LINK | 当前一条私人记录关联了该地点 | 本人去过／出席、照片亲历、公开地点内容 |
| LIKED | 本人严格 typed EXPLICIT PLACE 声明；SELF_DECLARATION | 本人明确表示喜欢这个地点 | 从收藏、正文或模型分数推断的偏好 |
| VISITED | 本人严格 typed EXPLICIT PLACE 声明；SELF_DECLARATION | 本人自述到访过该地点，未经核验 | 导航／定位／到访记录或真实亲历证明 |
| ATTENDED_ACTIVITY_AT | 当前无权威出席服务；UNAVAILABLE | 明确当前能力缺失 | RSVP going/pending/cancelled、Plans completed、活动结束、Moment 关联自动算到场 |

`verifiedVisit`、`attendance`、`processingStatus` 恒为 `UNAVAILABLE`。未知不等于从未去过／没有出席。当前原生 visit/check-in/attendance writer 不存在，本人到访声明也不补齐客观出席来源。

## 单一私人账本与写入

真实 Go Store 是 `postgres.Store`，满足 `agentplacememory.Store`：

- `ReadOwnPlaceMemory(ctx, access, placeID)`：只读当前本人一个可见地点的来源元数据。
- `RevalidateOwnPlaceMemory(ctx, access, old)`：重新验证当前来源、主体、目标与旧读取租期；不续租。
- `PutOwnPlaceDeclaration(ctx, access, memoryID, input)`：本人直接填写 LIKED 或 VISITED。
- `DeleteOwnPlaceDeclaration(ctx, access, memoryID, expectedVersion)`：使用原有 Memory CAS／scrub／tombstone 删除。

写入仅复用现有 `agent_memories` 的 `memory_type=PLACE`、`source_type=EXPLICIT`。`structured_value` 严格保存 `schemaVersion/placeId/cityId/kind/basis` 五字段，basis 必须 SELF_DECLARATION；key 为 `place.v1.<真实Place UUID>.<liked|visited>`；中文说明由服务按信号固定生成。`confidence=1` 沿原账本表示直接填写，不是概率、核验或机器许可。PRIVATE 和 AGENT_ONLY 均只为当前人类 self 管理，后者不打开模型读取。

输入不接受 owner、confirmed、confidence、source version、图片、正文、坐标、occurredAt、模型许可或分析 purpose。flat wire 解码拒绝重复字段、未知／大小写变体、null、嵌套对象、越界输入与尾随 JSON。旧通用 PLACE JSON 不自动转成五类事实；typed schema 也只表示可解释的本人声明，不是凭据或权威事实。合法 owner 从通用 Memory 路径明确填写同一严格 schema 的记录仍只是本人声明。

Put 在显式 READ COMMITTED／事务 UTC 下重新验证当前 Person 会话、精确 PersonalAgent、原 metadata 与当前公开 Place/City；不 Ensure 或恢复被删 metadata。锁定目标、metadata 和现有声明后，调用已有 `putOwnMemoryInTx`，不复制原 CRUD SQL。原生 Memory version、JSONB 检查、同内容重试、独立 Evidence 清理和 terminal tombstone 继续由同一 writer 负责。现有地址不能变成另一地点／另一信号，不能覆盖其他 Memory 类型。最后检查实际服务器当前时间、Place/City、会话和声明期限；等待跨期限会回滚写入。

删除先在当前 self 事务限定 typed namespace，再调用原 `DeleteOwnMemory`。两事务间内容变更会推进原生 version，被原 writer 的 CAS 拒绝；同址重复删除复用 tombstone，旧 ID 不能复活。隐藏／过期地点不阻止本人删除声明。没有新的自动 Memory sink 或批准消费路径。

## 当前读取与版本

最终 SQL 同时检查实际会话、active Person、精确 active PersonalAgent、仍存在的本人 metadata，以及当前 Place/City publication 和 expiry。来源由该 SQL 当前快照加载：收藏使用064的真实 created_at opaque digest；Moment 使用真实正 revision 和记录 created/updated 时间；声明使用原生 Memory version。记录时间不取用户写的经历时间、图片 EXIF 或地点坐标。

投影只含主体／Agent／Place／City ID、最小 source ID/version、记录时间、声明可见性／有效期和 opaque 当前摘要。没有 Moment 标题／正文、照片、坐标、Activity ID、公开数量或推断轨迹。原生来源与声明均按确定顺序返回；超过100条信号拒绝，不能把截断结果冒称完整。SQL 每类最多加载101条用于超限判定。

读取租期最多两分钟，使用一次真实 PG clock 作为统一基准，进一步限制为当前 session、Place、City 和声明截止时间。它不是给原生收藏补写 expiry，也不是 event TTL。Revalidate 检查旧租期并比较当前来源／目标／authority 摘要；不返回续期凭据。authority 和 target 采用当前行与 xmin 的 opaque 摘要，authority 另绑定真实 Session ID但不包含 Session xmin；不能作为 monotonic counter、领域版本或授权 epoch。停用恢复、隐藏恢复、metadata 重建、旧会话撤销后换新会话使旧快照失效；普通 Authenticate 的 last_seen/idle 刷新不被当成撤权。

读取两次当前 SQL 并比较 source snapshot；显式 READ COMMITTED 覆盖 pool 默认 Repeatable Read。第二条 SQL 是当前读取线性化点；期间来源删改、目标隐藏或 context 取消拒绝，不允许旧许可快照读出。已经发送的数据不能保证在之后的第三方变更瞬间撤回，后续使用必须重新核验当前来源。

## 完成分类与用户验收

AGE027 原文要求五种不同 Place 信号。当前交付真实 native 收藏／关联和本人喜欢／未经核验到访声明；缺权威到访和出席来源。**建议 PARTIAL**，不能用 catalog、fixture 或当前声明消除缺口。恢复条件：实际权威 visit/attendance writer、当前版本／删除撤回／权限 resolver 与原任务验收，认知目的和真实到访／出席来源仍需另行明确接口与权限，再完成原任务全量验收。

2026-10-03 首批没有 Flutter、HTTP、真机或可访问性界面接线，没有默认分析／推理、模型出口、A2A、视觉、事件消费者或正式部署。UX-CHECK-01/03/05/09/10/13/16 的中文、明确声明性质、私人入口、版本批准和真实验收要求适用；规则引用不算移动端验收完成。

适用测试与完整失败记录见 [审计](../research/BIRDTIE-V5-AGE-027-AUDIT.md) 和 [证据](../testing/evidence/agent-place-memory-2026-10-03/README.md)。实际 native 验证使用自有 fresh001–064 隔离库／合成供给，不是 CSSA 授权资料或试点证据。**Closed Pilot / Consumer Beta：NO。**

## 2026-10-04 恢复：本人单地点人工管理（原任务仍 PARTIAL）

新增真实 Go HTTP 与中文 Flutter 页面，只服务当前本人主动管理一个稳定 Place ID：

| 路由 | 当前含义 |
| --- | --- |
| GET `/v1/me/places/{placeID}/memory` | 本人当前公开 Place／City 的最小来源投影，发送前复验原 native 来源和会话 |
| GET `/v1/me/places/{placeID}/declarations` | 独立本人声明控制集合，可找回过期记录原 ID／CAS；不包含公开地点资料 |
| PUT `/v1/me/place-declarations/{memoryID}` | 六键 human 输入 `agentId/expectedVersion/placeId/kind/visibility/validUntil`，原 Agent 目标、原 Memory CAS、有限有效期 |
| DELETE 同址 | `expectedVersion` 原账本撤回与 tombstone 回执；隐藏地点也可撤回 |

两次 GET 是独立读取，不能当作一个联合快照。人类输出不含原 `SnapshotID`、authority/source/target stamp、Memory summary/value、动态正文、精确坐标或个人地址；返回的 source ID／revision 是本人当前来源，不是 machine permit。Read 租期最多两分钟。声明控制只载一个本人 Place 的 exact typed namespace，最多100条，101条判超限；已撤回的事实不再输出。stored EXPIRED 且期限在未来的矛盾记录拒绝，不复活为 ACTIVE。

新增 `HumanControlStore` 复用当前 PrivateAccess、原 Session／Account／PersonalAgent／metadata 和同一 Memory 账本。隐藏／过期／未知地点的控制读不加载公开资料；只有已有本人声明元数据可用于人工撤回。过期记录标 EXPIRED，不进入当前来源；公开地点仍有效时可用原 ID、原 CAS 重新声明有限有效期，不另造记录或回填到访证明。

`HumanBoundDeclarationStore.PutOwnPlaceDeclarationBound` 与原五键 `DecodePut`／`PutOwnPlaceDeclaration` 共用 guarded writer。取得真实当前 binding、原身份与 metadata 锁之后，写入之前比较人类预览 `agentId` 与原生当前 Agent。该编号只限定写入目标，不提供权限；原 owner/session/target/CAS 检查继续执行。Agent 被实际替换后旧编号提交返回拒绝且零 Memory 写入。此接口没有 storedPreview、HMAC 批准 ledger 或具体 xmin 版本批准，不能描述为机器认知授权；同编号的原生当前 self CRUD 仍按当前身份权限处理。

Flutter `PrivatePlaceMemoryPage` 与根代理 PlaceDetail 本人入口分工：根入口使用同 captured session 的权威 `/v1/me` 获取本人 ID，并用实际身份／工作台监听及 nested route boundary 撤掉旧页面。页面不从 token 猜身份，组织工作台不读私人记录。自身 controller 将 token／owner／workspace＋generation/serial 绑定草稿和确认，身份 ABA／迟到响应不展示旧数据；确认之前具体展示声明、原版本、范围与期限。提交前再次读取当前 control 的 Agent 与 CAS。公开来源不可读时不展示旧公开名称，只显示可撤回的本人控制项，不能续期。

原 PUT／DELETE 的网络未知结果以安全存储保存原记录 ID／Agent／CAS／具体输入和 session fingerprint，含义仅为本机恢复说明，不是权限或新 effect ledger。页面重开先读当前 owner/Agent 控制；只有同 session/current Agent 才能复用原 CAS 重试。记录消失不代表 DELETE 成功；只有严格匹配的原生权威回执才显示成功。设备存储保存失败不发送。用户可明确停止本机核实，但结果仍未知，不声称撤销／回滚。

适用 UX-CHECK-01/02/03/05/06/09/10/11/13/14/16：中文、本人直接路径、具体检查/确认、身份与迟到边界、未知与恢复、移动端及辅助语义。320宽、2倍字号、键盘160 inset、滚动确认和 ChoiceChip 语义已在 Flutter widget 运行；真机和真实 TalkBack 未运行。真实证据与保留失败见 [恢复审计](../research/BIRDTIE-V5-AGE-027-RESUME-AUDIT.md) 和 [本轮证据](../testing/evidence/place-memory-human-2026-10-04/README.md)。

**当前仍 PARTIAL**：verifiedVisit／attendance／模型认知读取恒 UNAVAILABLE，没有权威 check-in、出席服务、客观时间线或机器 processing purpose。收藏、动态关联、RSVP／Plans 和本人 VISITED 均不能替代这些来源。Closed Pilot／Consumer Beta：NO。本轮测试为 owned fresh001–076 隔离 PG 的合成数据，不是 CSSA、发布、生产身份、运营可靠性或真机证据。


## 2026-10-04 真机发现的 RFC3339 偏移兼容修补

根代理在历史 f255 Debug APK 的本人单地点入口观察到“地点记录暂不可用”；真实隔离合成 API 的 memory 与 controls 都是200，control时间为 `2026-10-04T04:40:55.442838+08:00`。客户端原严格解析器只接受Z，错误拒绝这个合法Go时间；该观察不是权限拒绝或生产验收。

`placeMemoryStamp` 现在接受显式Z及合法 `±HH:MM`，先逐项核对原写入日历，再按偏移转换UTC。时区小时00–23、分钟00–59；无时区、非法日期、秒60、尾随字符、偏移越界、转换后超出公元1–9999年仍拒绝。1–9位小数保持微秒截断，未改身份、来源、CAS或有效期判断。测试精确验证真实+08:00控制DTO、负偏移、跨日/闰日、纳秒与非法日期。

旧 final1、9284 Go／702 Flutter 和 f255构建均保留为历史帧。本次仅两份Dart源变动，8份本任务Go源逐字节未改；新增定向Flutter35功能＋3加载通过、零失败/跳过，6文件analyze通过。修补后的完整Flutter/build及真机由根代理继续核验，当前不得声称新APK真机通过；实际TalkBack与设备Vault重启未测。见 [偏移修补证据](../testing/evidence/place-memory-human-2026-10-04/OFFSET-COMPATIBILITY-REPAIR.md)。AGE027仍PARTIAL，Closed Pilot／Consumer Beta仍NO。

## 2026-10-04 接续：本人地点入口连接绑定

三项实际Mock RED证明：旧 `/v1/me` 响应后私密GET到了新endpoint；borrowed→null错误关闭原client；同值身份getter替换仍显示旧批准。PlaceDetail现在在init捕获初始client/ownership/base/getter/listener/pendingStore，并在配置替换后永久退役该State，清除旧详情与通过原nested boundary关闭全部旧私密页/弹窗。Me/详情迟到结果不得进入新来源，A→B→A不能恢复批准；自有/借用client责任不再从更新后的widget推断。

PrivatePlaceMemoryPage也捕获原连接与当前本人入口，配置替换不继承controller或确认。具体确认与await后的保存/撤回/停止核实绑定原controller/generation，停止核实另要求原pending对象仍相同。dispose幂等、清空本机私密状态，closed Controller不读取身份或恢复提示；原安全存储按environment/owner/place原key保持，没有自动重发/删除/迁移。普通当前token/workspace失效仍沿原机制处理；原native Agent/CAS与严格权威回执不变。

新nested回归发现build阶段同步state通知异常，已在本页原有范围修正确生命周期/必要post-frame UI更新，身份失效即时有效。最终六文件target2为74功能+6加载PASS，0FAIL/SKIP；analyze2七项无问题，owned7及当次client247前后SHA稳定。只是本线定向CODE_LOCAL证据，新全Flutter/build/真机由根在共同冻结后验收；没有Go/API/DDL变更，没有开放认知读取、客观到访或出席。原027保持PARTIAL、Closed Pilot/Consumer Beta保持NO。完整审计及失败链：[绑定接续审计](../research/BIRDTIE-V5-AGE-027-BINDING-AUDIT.md)；[专属证据](../testing/evidence/place-memory-binding-2026-10-04/README.md)。旧档案不覆盖。
