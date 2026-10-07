# Agent Context Relevance V5

2026-10-04。BT-V5-AGE-034。唯一此职责正文；复用[Context Builder](AGENT-CONTEXT-BUILDER-V5.md)、[认知ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)和[Memory边界](AGENT-MEMORY-ARCHITECTURE.md)，不重建认知许可或历史源。

## 当前实际契约

`agentcontextrelevance.NewService(acb.Store)`持有原Builder。`Retrieve(ctx, acb.Request)`仅接受MACHINE_TASK_CONTEXT / EXACT_TASK_CONTEXT，原Builder原生Build之后从准确批准且当前装配的有界内容作投影，再对原完整sealedBuilt执行RevalidateOwn。`Result`私有保留原控制、service身份与相关视图，拒绝JSON编解码；`ProjectionBundle()`/`View()`仅返回深拷贝DTO。它们不是许可、ModelGrant或业务事实。

候选最多3个明确Profile字段、3条EXPLICIT PRIVATE ACTIVE Memory、1个配置Policy、5个公开地点、5个公开活动、3条已批准Tie，固定City/当前Task。来源是033准确批准的源/字段/当前native版本，不读取ReadOwnMemories整历史、整份私人Profile、Conversation、待审阅INFERRED或额外活动。没有新表、索引、语义模型或自动Memory写。

## 透明相关规则

方法 `bounded-lexical-zh-en-v1`：当前native Task query为唯一查询。中文连续词与相邻字对、英文整词；使用现ParseMVPIntent类别/时间闭集补中英同义词，最多64项query terms。仅匹配已写Profile字符串/字符串数组、明确Memory Summary、公开地点名称/分类、公开活动标题/分类，不匹配StructuredValue内部隐藏正文。Score是字面匹配数，不是可信度、概率或推断。稳定score降序及native ID/字段排序。

无文本匹配且未明确询问该已批准字段的内容排除；使用closed字段中文/英文别名和准确key识别明确字段问题。明确查询且已选择的空字符串/空数组保留给AIR标UNKNOWN，未知不猜；未选择字段没有补造读取路径。Policy是已声明操作边界的必要锚，保留一次，不能授予提交权；Tie只在明确本人关系意图或查询明确提及该已批准Tie ID时保留当前好友ID/peer/state。找新朋友或运动查询不推断已有好友共同兴趣。地点与活动的匹配只表示标题/类别文本相关，未执行新搜索或认定时间、距离、到访、出席、资格或承诺；中文时间词仅字面偏好相关，实际活动日期由原最小native数据保留供后续领域查询，不伪称完整规划。

RelevantView带保留项目真实kind/source ID/field、匹配terms/score/原因以及排除数量；不带排除项目原文。ProjectionBundle.Sections区分NOT_RELEVANT（选择过但无相关项）与NOT_REQUESTED（未选择）；Result.ExcludedCounts只提供closed六类计数，Runtime调用ProjectRelated的预算投影，relevanceExcluded统计计入最终JSON字节，不泄漏排除source ID/正文。输出Sources只含实际保留的native SourceVersion/NativeTime及City/Task，RowToken不外传。原完整source及xmin仍私有保留用于核验。

## Runtime 与 AIR022

真实注册 `POST /v1/me/agent-context/runtime` 仍只接受grantId；原会话、Person、Agent、Task及具体批准范围由现PurposeStore解析，不开放owner/query/selectors/budget输入。顺序：native批准读取 → RelevantService.Retrieve → AIR022 `consumeBudgetedTaskContext` 默认闭集预算纯投影 → RelevantService.Revalidate原完整批准 → 中文响应。adapter错误fail closed，不回退旧全量consume。

原完整授权包括被相关性过滤或预算省略的源；其中任一变更/撤回/过期/ABA或Session/账号/Agent/ACL变化，旧结果拒绝。不能用filtered subset Request继承exact grant。批准期限不晚于具体Preview原expiresAt（最多5min或原session/source更短），Retrieve/Revalidate/adapter均不续命。无模型、A2A、候选提交、Memory promotion或自动行动端口。

AGE034拥有相关选择与原完整核验；AIR022拥有单一纯预算/unknown/provenance适配，不再复制ranker/身份/权限真源；AGE035未来预算/模型能力不借本项启用。

## 证据与界限

专属work `work/v5-age034` 保留每轮source、真实命令、JSONL、首次失败、独占fresh076完整public/catalog与旧数据往返及ownedDROP。完整Go统一冻帧由root核证。UIUX引用UX-CHECK-02/04/06/08/09/12/16：中文事实回答、准确主体/版本批准、未知不猜、撤权/迟到拒绝、真实验收。本轮只仓库API，不改Client UI；真机/TalkBack/正式生产/模型未运行。Closed Pilot/Consumer Beta仍NO。实际PASS数量以最终不可变证据帧为准，不以本规范取代验收。

## 词法局限

透明literal规则没有模型语义校准：中文字对如“偏好”等泛词可能保留文本有共同字对但语义较弱的项目；此类保留仍仅限已准确批准的当前源，不扩权限或推断事实。不会把匹配称为理解、可信度、自动规划或已满足所有日期/距离过滤。后续规则改善仍应在原任务证据/具体验收中审查，不能默改冻结帧。
