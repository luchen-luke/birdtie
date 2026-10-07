# Agent Context Adapter V5

日期2026-10-05。BT-V5-AIR-022 与 BT-V5-AGE-035 的唯一预算投影规范。来源 `work/v5-materials/BT-V5-AIR-BACKLOG.json:545`、AIR架构§7.2 与 `docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md:1288`；原审计 `work/v5-age033-resume/http-tests/NEXT-AIR022-AUDIT.md`。复用 [Builder](AGENT-CONTEXT-BUILDER-V5.md)、[相关性](AGENT-CONTEXT-RELEVANCE-V5.md)、认知ADR及唯一Memory/Profile/Policy领域服务。

## 权限与接线

`agentcontextadapter.Project(Bundle,Budget)` 是纯数据投影，形状合法不是授权。真正 Runtime 的具体用途原生Preview→Approve→current grant→Builder/current Actor→相关性投影→本adapter→实际中文结果→**原完整sealed来源Revalidate**，由 `httpapi/agent_context_purpose.go` 的唯一hook接入。没有新API、任意预算wire、SQL读取/第二grant/Memory账本或客户端confirmed批准。Runtime请求仍只有grantId。`httpapi/agent_context_adapter.go` helper不创建权限fallback。

只接受当前MACHINE_TASK_CONTEXT内容；HumanSelfReview与public Bundle拒绝继承。源的native版本、具体原批准Task/query/City/期限和当前source/session/Agent/account/grant仍由033核验，相关性/预算过滤掉的源仍属于完整seal，变化后也拒绝。未获准/删源/撤回/过期/跨主体不能降级为UNKNOWN。

## 输入预算和投影

- 默认16384、最大32768的**最终UTF8 JSON编码字节**；unit `UTF8_JSON_BYTES_V1`，`tokenCountStatus=UNKNOWN_NOT_TOKENIZED`。不是provider精确token、模型请求实际计费或全run共享预算。AIR007已有MaxOutputTokens不当输入预算；未来provider tokenizer/egress/billing仍按独立任务。
- `budget.used`以`json.Marshal(View)`最终实际字节固定点计量，含Answer、中文事实、来源、字段引用、状态与预算envelope，计量自身数字也计入。
- Task原query/updatedAt、City/timezone、当前observed/expiry和所选具体Policy完整Settings是必需anchor；无法容纳则ErrBudget，不能剪Policy/身份来源或返回半个JSON。
- 其他Profile完整字段、Memory完整summary、Place/Activity/Tie完整单位确定性尝试；不能装下则整个单位省略。只返回各类省略数量，不泄漏被省略的原文/ID；不截否定句后把残片当完整事实。
- Profile按闭集字段顺序，其余按已获相关投影顺序。预算是bounds，不进行另一次全量relevance或新检索。实际answer/counts/facts只来自最后保留数据，不引用预算前内容。
- 选定空字符串/空列表为`UNKNOWN`和中文「尚未填写（未知）」；根本未选为`NOT_REQUESTED`；全部因预算移除为`OMITTED_BUDGET`，部分移除在budget.omitted逐类计数。相关性过滤发生在前一模块，不能把无关字段再补未知事实。
- 原已批准选择但完全不相关为`NOT_RELEVANT`，不假装根本未请求。`ProjectRelated`与真实Runtime helper只接收相关性Result的closed6 `relevanceExcluded`数量；不得含过滤ID/正文，数量和状态一致且不超过原033选择上限，Policy排除数量必须0。全部计入最终预算，和budget.omitted分别说明两种省略。

## 字段来源与不可信正文

### AGE-035 四项本轮输入控制

`Budget` 仍是服务内部对已经授权、已经相关的数据进行投影的配置；真实 Runtime body 仍只有 `grantId`，任意客户端 `itemLimit/priority/confidenceThreshold/recencyWeight` 输入均拒绝，不建立新许可。原 byte budget 与 AIR011 原四层 request/task/owner/global 预算账本分别执行，不复刻062或把本项条目数当 token、费用或全 Run 额度。

| 控制 | 边界与语义 |
| --- | --- |
| `MaxEncodedBytes` | 保留默认16384、闭集上界32768，仍测量整个最终 UTF8 JSON |
| `ItemLimit` | 可选指针；未提供为19，提供整数0至19，只计完整可选字段/Memory/Place/Activity/Tie；0仍保留 Task/City/当前所选 Policy |
| `Priority` | 未提供保留 profile→memories→places→activities→relationships；提供时必须是这五个类别的完整不重复排列；Policy不在可选闭集内 |
| `ConfidenceThreshold` | 未提供禁用；提供有限数值0至1，只作用于明确 Memory 的原生 `DIRECT_DECLARATION` 评估。实际 native ACTIVE EXPLICIT confidence 必须是1；缺元数据不补1，提供门槛时整条省略。其它来源未提供此评分，不被假设为1，也不按门槛移除 |
| `RecencyWeight` | 默认0；提供有限数值0至1。仅使用已核验 `Source.NativeTime`（原生来源更新时间）与 Builder 最终 PostgreSQL `ObservedAt`，不使用活动开始、访问/居住日期或服务墙钟 |

优先级得分为五类顺序的1、0.75、0.5、0.25、0；近期值为 `1/(1+ageDays/30)`，排序分为 `(1-weight)*priorityScore + weight*recencyValue`。正权重可以改变已选相关条目的纳入顺序；相同分与类别时稳定保留 AGE034 已有相关性顺序，未指定近期/优先级的数量或门槛配置不能以 ID 排序替换高相关条目。默认 `MaxEncodedBytes` 单字段调用保留原尝试顺序。

先验证所有投影来源/条目，再进行门槛、省略与排序。数量、字节和 confidence 每次只移除完整单位；投影返回 `budget.omitted` 总数以及启用新控制时的 `omissionReasons` 数量：`OMITTED_BUDGET`、`OMITTED_LIMIT`、`OMITTED_CONFIDENCE`。全部缺项按真实单一原因呈现，混合原因为 `OMITTED_CONTROLS`；原 `relevanceExcluded/NOT_RELEVANT` 单独保留。缺 confidence 的筛出只说明门槛未满足，不能据此说底层 Memory 不存在或已撤销。计数不包含被省略 ID/正文；实际 Answer/Facts 只引用最终保留数据。

新控制、中文说明和所有省略理由也计入最终 `budget.used` 固定点。若最终理由 envelope 变大，先撤下最低已选完整可选项并重新计量，直到能容纳；只在必需锚点与控制/省略 envelope 无法容纳时返回 `ErrBudget`，不会截断 JSON、保留早期 used 或删必需 Policy。

原 sealed Builder 不改选择集；Memory confidence 属于原 Bundle seal。即使内容被相关性、门槛、数量或字节筛出，原完整选择的 source version、xmin/身份/会话/grant/期限仍必须最后重核。省略、排序或 `DIRECT_DECLARATION=1` 不授模型出网、Memory 晋升或任何领域写权限。

保留Task/City/Profile字段/Memory summary/Policy Settings/公开Place Activity/Tie状态分别带Provenance：kind、稳定sourceId、fields、真实native SourceVersion/NativeTime和origin；仅保留使用到的来源。native RowToken、authority/seal、未选字段、StructuredValue、私聊、坐标/描述不入回应。

`UNTRUSTED_DECLARED_DATA`为明确用户文本/Memory/Policy设置；`NATIVE_DOMAIN_DATA`为当前领域事实，都不是可执行指令（`contentIsInstruction=false`）。Policy自主级别读实际native值并中文表达，但不因此授操作、ModelAccess始终UNAVAILABLE、MemoryPromotionAllowed=false。当前好友状态不冒充亲密/共同兴趣，地点/活动不冒充到访/出席。

## 当前验证边界

初始native1真实3FAIL来自相关性模块复制Policies后重复append，单Policy变两条被adapter严格拒绝；由该worker在自身范围清空后追加并补回归，未放松adapter。native2真实86PASS/0FAIL-SKIP、694源稳定和完整旧public保持，自有fresh076 DB已删除；它是当时定向诊断帧。最终冻结定向/整仓结果以后续本项evidence记录为准，不能提前称DONE。

独立根审查发现：相关性模块原空字段全部过滤，即便本人明确问该已选字段；初期pure UNKNOWN通过不能代替真实Runtime。现相关性模块按闭集字段label匹配保留明确询问的空字段，native registeredHTTP验证UNKNOWN、NOT_RELEVANT、NOT_REQUESTED；adapter补count-only状态与最终预算核验。首次诊断和修复前后帧分别保留，最终实际命令详见evidence。

本项最终定向native5实际96PASS/0FAIL-SKIP、694全源stable、旧完整public unchanged、自有DB DROP；纯adapter94.7% statements，Go target test/vet/build0。证据见 [本项记录](../testing/evidence/agent-context-adapter-2026-10-04/README.md)。当前源码freeze交付根独立整仓核证，未自行写DONE。

适用UX-CHECK-02/04/06/08/09/10/12/16：真实中文结果、具体批准/权威结果分离、来源/换主体/撤权/迟到处理。没有Flutter改动；新增后端用途不宣称已有移动端批准界面、真实手机/AT/正式模型/生产CSSA试点。Closed Pilot/Consumer Beta仍NO。

### AGE-035 当前本地核证（2026-10-05）

保留三份真实 RED：逆ID/不同相关分被旧同类排序反转；4335字节处动态省略 envelope 增大误报 ErrBudget；零可选条目的实际 envelope 可容纳却被初始字节占位理由拒绝。修正为同类相同分稳定保持原相关次序、最低完整已选单位回退重算以及零条目在计量前使用真实 LIMIT 原因；均未放宽 source、权限或必要锚点。全部原日志保留。

最终 worker `native3` 是 actual schema001-088 的冻结定向验收：468PASS/0FAIL-SKIP/pkgFail，Go vet/build及两既有CLI build均0；910 API源码+3原seed共913输入稳定，旧48条完整 public行/原 xmin与可见语义 catalog保持，自有库实际DROP且另查不存在。Adapter包95.9% statements（其余包为本pattern的部分覆盖，不能冒称全仓覆盖）。详细命令、八源码SHA及原RED见 [AGE-035证据](../testing/evidence/agent-context-budget-2026-10-05/README.md) 与 [审计](../research/BIRDTIE-V5-AGE-035-AUDIT.md)。根同帧全仓验收和live DONE由根负责；本记录不替代 root whole 或生产/客户端发布门槛。
