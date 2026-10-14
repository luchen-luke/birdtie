# Place / Growth 26项增量对账

> 后续增量（2026-10-09）：下文为原只读观察与当时NOT_RUN，保留不改。已开发的当前API20/APK16、单元/隔离HTTP/真机证据见[增量报告](../../testing/evidence/now-place-growth-2026-10-09/REPORT.md)和[逐项映射](../../testing/evidence/now-place-growth-2026-10-09/TASK-MAPPING.md)；全包仍PARTIAL。


> 本文件是只读对账结果，不是新增队列或完成报告。新26项全部为 **PARTIAL**，功能/SQL/供应商/真机/构建验证本次均 **NOT_RUN**。原包状态 RECONCILE_REQUIRED / NOT_RUN 及旧任务历史原样保留。

## 当前基线与写入边界

- 官方仓库：D:/Project/birdtie；分支 `codex/birdtie-import`；HEAD `4637cfb6e7758fc4fa5dd548fad623a8484c4cfb`，与包基准一致。
- 原执行任务：`BT-FIX-NOW-UI-001` **PARTIAL**。
- 只新增本目录 RECONCILIATION.md / RECONCILIATION.json；未改原包、任务队列、AGE状态、已有NOW映射、业务代码或配置。
- 原 source-v2、受限地点续问、human-only图片及 root 当前NOW增量未提交状态保留。代码锚点是本次读取时的字节/位置观察，不能替代已构建/运行证据。
- 26 PG / 30 NOW / 42 UX / 22 DISC 是重叠材料集合；映射已有ID和新增缺口，不相加、不另开一套runtime。精确任务标题/权威status、原材料别名、依赖、逐项代码hash和逆向索引见JSON。

## 首要许可门：PENDING

用户已确认目前没有 SearchPro 留存合同或书面说明。当前账户的允许字段、期限、展示、模型使用、跨用户共享、署名条件均 **NOT_CONFIRMED**；免费额度与API可调用不证明这些许可。

- **禁止新增 WSA 来源持久化、进入城市公共Place资料或共享缓存**，直至许可审查满足。不得通过改写、背景补搜绕过。
- 原 source_batch 保留待审，不自动清库；此结论是新增产品门禁，不作未经核实的法律归责。
- 当前代码确有原 Execute→Authenticate→Preview 写入 model_egress_previews.source_batch；可执行的新增留存三态门 **NOT_IMPLEMENTED**。因此“代码/接线存在”不能写成未来该WSA链已可启用。
- 原用户/商户独立声明、已有明确许可的内部事实可沿原候选审核/ACL/purpose继续实施；不同来源必须各自审核，不能统一继承WSA权利。

## 两条优先纵向链路

### A. 许可来源 → 规范Place → 字段刷新 → 本地复用

可复用 catalog.go 的 ListPlaces/GetPlace、cityseed.go 的 Submit/Review/linkCandidateSource、canonical UUID/aliases/external refs、Place语义资料、Activity和Saved。原审核流程不是空白。

真实缺口：许可三态及字段/操作门；通用CN/EN去重与同名分店区分；常规/假日营业、临停等逐字段观察/验证/有效期；普通Now本地充分则零WSA，必要时按许可补搜；库外Save pending引用再canonical解析。共享缓存/singleflight为后续独立条件，不用等待其完成才做基础本地读取。

eligible新Now POST 当前仍直接新Run→WSA→MODEL；历史GET恢复不重派发，不能将GET零调用当新问题已本地优先。原Place整体verified_at/expires_at、semantic价位/预约声明、PublicFieldEvidence已有，但不等于营业字段新鲜度。UI30秒授权lease也不是营业事实/缓存TTL。

### B. 明确偏好 → Memory → 下一Now使用 → 纠正/忘记

可复用 AGE原EXPLICIT Memory CRUD/CAS、Evidence/候选人工确认、ContextBuilder/用途许可、Settings的管理记忆/候选/本地分析许可、纠正和原Memory outbox失效。Moment create/update/withdraw真实事件已接，不用另造学习引擎。

真实缺口：普通聊天与“记住海鲜、不吃辣”长期/本人资格；六活动词法LOW以外的closed偏好；普通Now最小相关Memory召回并实际改变本地筛排、memory_id/version trace；自然语言纠正/忘记与设置一致效果。当前live输入仍current query+公开CITY/slots/source，未消费私人Memory。明确偏好先做，不等待通用行为学习、主动通知。

朋友/今晚/规划/收藏只是条件或证据，不能自动认定本人长期偏好、实际出席或到访。私人Memory优先本地筛排；人类读许可与PUBLIC_SEARCH不能扩为MODEL_EGRESS，不发完整画像/历史。

## 26项精确覆盖与缺口

状态列依次为 数据 / 代码 / 接线；全部新验收PARTIAL、测试NOT_RUN。旧ID后的状态保留原queue权威值；DONE仅原范围，未反推新PG完成。

| PG | 标题 / 分类 | 原任务ID（权威状态） | NOW / DISC | 数据 / 代码 / 接线 | 新验收真实缺口 |
|---|---|---|---|---|---|
| BT-PG-20261008-01 | 证据审计与旧队列对账<br>covered | BT-AUD-001 DONE<br>BT-V4-AUD-001 DONE<br>BT-V5-INT-001 DONE<br>BT-FIX-NOW-UI-001 PARTIAL | NOW 001,030<br>DISC 001,021,022 | EXISTING / EXISTING_BASE / READ_ONLY_RECONCILIATION | 本次完成26项只读映射和包结构校验，不等于任何新功能验收；根代理仍需分阶段实际代码、运行、部署证据及唯一队列回写。 |
| BT-PG-20261008-02 | 来源使用权限与留存审查<br>modified | BT-V4-PLC-001 DONE<br>BT-V4-PLC-002 DONE<br>BT-V5-AIR-024 PARTIAL<br>BT-V5-AIR-030 PARTIAL<br>BT-V5-AGE-062 DONE | NOW 015,027,028<br>DISC 008,011,020 | PARTIAL / PARTIAL / CLOSED_PENDING_PERMISSION | 用户已明确无SearchPro留存合同/书面说明，许可PENDING。缺provider/API/账户/版本/字段/留存/展示/模型输入/跨用户/署名的可执行许可三态；新增WSA持久化/公共Place/共享缓存关闭，原batch逐项待审且不自动删除。 |
| BT-PG-20261008-03 | 查询与学习链路可观测性<br>modified | BT-V5-AIR-049 DONE<br>BT-V5-AIR-011 DONE<br>BT-V5-AIR-014 DONE<br>BT-V5-AIR-015 DONE<br>BT-V5-AIR-017 DONE<br>BT-V5-AGE-064 DONE<br>BT-V5-AIR-048 TODO | NOW 001,015,028,030<br>DISC 019,020,021,022 | PARTIAL / PARTIAL / PARTIAL | 缺普通聊天→学习资格→Evidence/候选/Memory→本轮使用的统一trace与闭集拒绝原因；累计保留金额不能当provider实际账单或节约。 |
| BT-PG-20261008-04 | 规范地点实体与合并规则<br>modified | BT-DAT-001 DONE<br>BT-V4-PLC-001 DONE<br>BT-V4-PLC-002 DONE | NOW 003,004,015,016,027<br>DISC 005,010,011 | EXISTING / PARTIAL / PARTIAL | 不能仅凭名称自动合并分店；需中英别名匹配、经许可字段来源/版本、地理冲突候选及可审计纠错。ListPlaces当前按name/summary/category查询，不证明aliases参与通用检索。 |
| BT-PG-20261008-05 | 合规地点候选入库<br>modified | BT-DAT-001 DONE<br>BT-DAT-002 DONE<br>BT-V4-PLC-001 DONE<br>BT-V4-PLC-002 DONE | NOW 016,022,027,028<br>DISC 001,005,010 | EXISTING / PARTIAL / PARTIAL | 缺新增来源字段/操作许可的可执行门和通用候选采集→验证→去重→原生实体接线；新增WSA来源入库必须先有留存许可，不因回答成功直接导入。无许可时仅必要短期诊断。 |
| BT-PG-20261008-06 | 营业信息字段级新鲜度<br>modified | BT-V4-PLC-002 DONE<br>BT-V4-PLC-005 PARTIAL<br>BT-V5-AIR-024 PARTIAL | NOW 015,026<br>DISC 009,010,011,019 | PARTIAL / PARTIAL / NOT_CONNECTED | 未见常规/假日营业时间/临时停业字段的独立observed/verified/valid_until/refresh生命周期；整体Place复核不证明每项营业事实新鲜。source_batch observedAt是本次取得来源的时间。 |
| BT-PG-20261008-07 | 活动独立时效实体<br>modified | BT-ACT-001 DONE<br>BT-V4-PLC-004 DONE<br>BT-V4-NOT-001 DONE<br>BT-V5-AIR-024 PARTIAL | NOW 015,025,026<br>DISC 005,009,011 | EXISTING / PARTIAL / PARTIAL | 库外演出/节庆等尚无统一受许可Event候选→独立时效实体→补搜/更新/取消接线；不得把外部活动写成永久Place标签或外部页面按钮说成已报名。 |
| BT-PG-20261008-08 | 本地充分性判定与受控补搜<br>new_delta | BT-AGT-001 DONE<br>BT-AGT-002 DONE<br>BT-V5-AIR-025 TODO<br>BT-V5-AIR-026 TODO<br>BT-V5-AIR-039 DONE<br>BT-V5-AIR-011 DONE | NOW 015,026,028<br>DISC 003,004,008,011,017,019,020 | PARTIAL / PARTIAL / NOT_CONNECTED | eligible新Now POST当前直接新Run→WSA→MODEL，未见本地充分性判定/可控skip。缺本地授权事实/覆盖/字段新鲜度/即时性评估；充分则零WSA，动态缺口才按许可补搜，不能靠少数pilot名称规则冒充通用。 |
| BT-PG-20261008-09 | 授权缓存与并发去重<br>new_delta | BT-V5-AIR-020 PARTIAL<br>BT-V5-AIR-026 TODO<br>BT-V5-AIR-011 DONE | NOW 028,030<br>DISC 019,020,021 | NOT_IMPLEMENTED / NOT_IMPLEMENTED / CLOSED_PENDING_PERMISSION | 新增许可字段缓存、独立TTL/容量/撤回失效、公共事实单flight尚未接；不能缓存私有原问/私人答案，合并查询也不能复用授权票。SearchPro许可PENDING时不启用WSA共享缓存。 |
| BT-PG-20261008-10 | 库外地点收藏与身份解析<br>modified | BT-SAV-001 DONE<br>BT-V4-PLC-001 DONE<br>BT-V4-PLC-005 PARTIAL | NOW 017,018,019<br>DISC 018 | EXISTING / PARTIAL / NOT_CONNECTED | 当前只支持native place/activity/group，库外地点无受许可pending外部引用→后续canonical解析链。需保Save ID/备注/可见范围、未保存不报成功，不能由保存行为自动认定喜欢/到访。 |
| BT-PG-20261008-11 | 地点与站内内容权限关联<br>modified | BT-V4-MOM-001 DONE<br>BT-V4-MOM-002 DONE<br>BT-V5-AGE-020 TODO<br>BT-V5-AGE-021 TODO<br>BT-V5-AGE-027 PARTIAL<br>BT-V5-AIR-030 PARTIAL | NOW 012,022,027,028<br>DISC 005,006,007,016 | EXISTING / PARTIAL / PARTIAL | 当前可见站内内容聚合/作者署名及撤回/封禁的完整地点页和Agent discovery链未获PG证据；不得搬外部评论、照片/长文，不得用公共Place绕开私密Moment ACL。 |
| BT-PG-20261008-12 | 个人数据与城市公共资料分层<br>covered | BT-V5-AGE-002 DONE<br>BT-V5-AGE-003 DONE<br>BT-V5-AGE-060 DONE<br>BT-V5-AGE-062 DONE<br>BT-V5-AIR-022 DONE<br>BT-V5-AIR-023 DONE<br>BT-V5-AIR-046 TODO | NOW 003,012,020,027,028<br>DISC 007,020 | EXISTING / EXISTING_BASE / PARTIAL | 新PG学习/共享资料接线仍需证明分层、跨用户负例和依赖失效；read grant不等于MODEL_EGRESS，私有query/Memory不能进入城市公共资料或共享缓存。 |
| BT-PG-20261008-13 | 日常事件与学习资格接线<br>modified | BT-V5-AGE-064 DONE<br>BT-V5-AIR-014 DONE<br>BT-V5-AIR-015 DONE<br>BT-V5-AIR-016 DONE<br>BT-V5-AIR-019 DONE<br>BT-V5-AIR-043 TODO<br>BT-V5-AGE-024 DONE<br>BT-V5-AGE-026 TODO | NOW 010,017,022,025,028<br>DISC 005,006,007,018 | PARTIAL / PARTIAL / PARTIAL | UserQuery当前purpose=ActivityQuery；普通聊天/Save/规划没有完整成长hook。实际参加/到访来源仍UNAVAILABLE，须真实actor/purpose/consent/version/source与回放资格校验，不能用函数可调用代替已接线。 |
| BT-PG-20261008-14 | 明确偏好写入闭环<br>modified | BT-V5-AGE-004 DONE<br>BT-V5-AGE-005 DONE<br>BT-V5-AGE-007 PARTIAL<br>BT-V5-AGE-069 DONE<br>BT-V5-AIR-043 TODO<br>BT-V5-AIR-044 TODO | NOW 010,026,028<br>DISC 004,017 | EXISTING / PARTIAL / NOT_CONNECTED | Now“记住我爱海鲜、不吃辣”未串轻量资格/长期性/本人归属→结构化饮食Memory→可见反馈/撤销。朋友临时今晚吃披萨仅Task条件；明确偏好闭环无需等待行为学习。 |
| BT-PG-20261008-15 | 行为证据与候选偏好<br>modified | BT-V5-AGE-005 DONE<br>BT-V5-AGE-007 PARTIAL<br>BT-V5-AGE-008 DONE<br>BT-V5-AGE-009 DONE<br>BT-V5-AGE-025 BLOCKED<br>BT-V5-AGE-027 PARTIAL<br>BT-V5-AIR-045 TODO<br>BT-V5-AIR-035 TODO | NOW 012,017,018,022,025<br>DISC 006,007,018 | PARTIAL / PARTIAL / PARTIAL | 普通点击/搜索/收藏/规划未接通用行为证据，概率校准未证；Save≠喜欢/到访，规划≠出席，GPS≠到访，重复事件不能抬高confidence；推断candidate不能直接ACTIVE。 |
| BT-PG-20261008-16 | 矛盾修正与时效处理<br>modified | BT-V5-AGE-011 DONE<br>BT-V5-AGE-010 DONE<br>BT-V5-AIR-018 DONE<br>BT-V5-AIR-047 TODO<br>BT-V5-AIR-044 TODO | NOW 010,026,028<br>DISC 004,007,017,019 | EXISTING / PARTIAL / PARTIAL | Now自然语言纠正与下一轮实际应用未接；临时不社交不能抹历史，朋友条件不是本人长期偏好；EXPLICIT不自动衰减，用户纠正优先级与Place事实时效须分开。 |
| BT-PG-20261008-17 | 记忆实际进入检索与排序<br>modified | BT-V5-AGE-033 DONE<br>BT-V5-AGE-034 DONE<br>BT-V5-AGE-035 DONE<br>BT-V5-AGE-036 DONE<br>BT-V5-AGE-062 DONE<br>BT-V5-AIR-022 DONE<br>BT-V5-AIR-023 DONE<br>BT-V5-AIR-025 TODO<br>BT-V5-AIR-026 TODO<br>BT-V5-AIR-046 TODO | NOW 010,026,028<br>DISC 003,004,011,017,020 | EXISTING / PARTIAL / NOT_CONNECTED | 普通live Now仍raw current query+public slots/source，不召回私人Memory。需最小相关授权Memory真正改变筛选/排序并有memory_id/version使用trace；优先本地消费，出模型需独立精确出口授权，不发送全量画像/历史。 |
| BT-PG-20261008-18 | 用户记忆控制与反馈入口<br>modified | BT-V5-AGE-046 DONE<br>BT-V5-AGE-047 TODO<br>BT-V5-AGE-048 TODO<br>BT-V5-AGE-049 DONE<br>BT-V5-AGE-011 DONE<br>BT-V5-AIR-050 TODO | NOW 005,010,028,029<br>DISC 007,019,020 | EXISTING / PARTIAL / PARTIAL | 缺Now自然语言忘记与UI一致的效果、学习暂停/导出和推荐理由反馈闭环；已有本地分析撤权不删除独立EXPLICIT Memory。不在空主页堆成长面板；AGE049原DONE是原code/local范围，不覆盖本PG。 |
| BT-PG-20261008-19 | 撤回删除与衍生数据清理<br>modified | BT-V5-AGE-011 DONE<br>BT-V5-AIR-018 DONE<br>BT-V5-AIR-044 TODO<br>BT-V5-AIR-047 TODO<br>BT-V5-AGE-049 DONE | NOW 012,020,022,028<br>DISC 007,019,020 | EXISTING / PARTIAL / PARTIAL | 新index/推荐快照/可选缓存及备份恢复删除账本全链未验；真实vector/provider删除端口未接。基本删除立即生效不应等待可选缓存，撤回不能经旧derived/restore复活。 |
| BT-PG-20261008-20 | 主动接收筛选与通知<br>modified | BT-V5-AGE-037 DONE<br>BT-V5-AGE-038 DONE<br>BT-V5-AGE-040 PARTIAL<br>BT-V5-AGE-041 DONE<br>BT-V5-AGE-042 PARTIAL<br>BT-V5-AGE-043 DONE<br>BT-V5-AIR-041 TODO<br>BT-V4-NOT-001 DONE | NOW 005,006,025,028<br>DISC 007,009,019,020 | EXISTING / PARTIAL / NOT_CONNECTED | Memory兴趣→地点/活动/邀请筛选→用户主动开启的新通知闭环未接；新成长主动通知默认不启用，不能说旧所有通知默认OFF（原路由NORMAL仍存在）。兴趣不能自动发送/加入/付款，拒绝/停用/封禁须当前化。 |
| BT-PG-20261008-21 | 对外行动与邀请接受边界<br>covered | BT-V5-AGE-044 DONE<br>BT-V5-AGE-041 DONE<br>BT-V5-AIR-036 DONE<br>BT-V5-AIR-037 DONE<br>BT-V5-AIR-038 PARTIAL<br>BT-V5-AIR-040 DONE<br>BT-V5-AIR-042 TODO<br>BT-V4-ACTN-001 DONE | NOW 013,019,025,028<br>DISC 018,020 | EXISTING / EXISTING_BASE / PARTIAL | 新成长触发的对外动作仍需原actor/object/recipient/content/fee/confirmation/current与回执；自治真实执行Unavailable/L3禁用，interest/read/rank不授RSVP/邀请接受/发送/付款，无盲重试。 |
| BT-PG-20261008-22 | 安全与敏感推断防线<br>covered | BT-V5-AGE-002 DONE<br>BT-V5-AGE-003 DONE<br>BT-V5-AGE-060 DONE<br>BT-V5-AGE-062 DONE<br>BT-V5-AGE-066 DONE<br>BT-V5-AIR-011 DONE<br>BT-V5-AIR-018 DONE<br>BT-V5-AIR-023 DONE<br>BT-V5-AIR-028 DONE<br>BT-V5-AIR-046 TODO<br>BT-V5-AIR-053 TODO<br>BT-V4-SAF-004 DONE | NOW 012,013,020,027,028<br>DISC 007,020,021 | EXISTING / EXISTING_BASE / PARTIAL | 新聊天学习、Memory出口与Place候选尚未接，必须先加恶意来源注入/越权/敏感推断/儿童数据最小化守卫和负例；默认代码OFF不证明生产配置已验，PG12/22先于对应新写入而非等最后验收。 |
| BT-PG-20261008-23 | 中文纵向验收与回归<br>modified | BT-FIX-NOW-UI-001 PARTIAL<br>BT-TST-001 DONE<br>BT-TST-002 BLOCKED<br>BT-V5-AGE-071 TODO<br>BT-V5-AIR-054 TODO | NOW 007,014,015,016,017,026,029,030<br>DISC 001,002,003,004,005,006,007,008,009,010,011,012,013,014,015,016,017,018,019,020,021,022 | PARTIAL / PARTIAL / NOT_CONNECTED | PG中文/英文混合“记住→下次使用→纠正→忘记”、本地充分零WSA/动态补搜、同Now地图和新紧凑source未验；旧组织/聊天/活动/提醒/个人资产回归继续保留，不能以unit数量/静态页完成。 |
| BT-PG-20261008-24 | 匹配质量与节约评估<br>new_delta | BT-V5-AIR-013 TODO<br>BT-V5-AIR-048 TODO<br>BT-V5-AIR-049 DONE<br>BT-V5-AIR-052 TODO<br>BT-V5-AIR-054 TODO<br>BT-V5-AGE-008 DONE<br>BT-V5-AGE-025 BLOCKED | NOW 030<br>DISC 011,020,021,022 | PARTIAL / PARTIAL / NOT_CONNECTED | 缺获许可脱敏样本、分母/负例与无Memory/有Memory/本地优先对照；stale/误记/删除失效/调用/延迟需真实测量。命中率不等于推荐准确率，预留金额不等于账单或节约。 |
| BT-PG-20261008-25 | 迁移发布回滚与独立门禁<br>modified | BT-V5-AIR-005 TODO<br>BT-V5-AIR-053 TODO<br>BT-V5-AGE-066 DONE<br>BT-TST-002 BLOCKED<br>BT-REL-001 BLOCKED<br>BT-V4-PIL-001 BLOCKED<br>BT-V4-PIL-002 BLOCKED<br>BT-V4-PIL-003 BLOCKED | NOW 027,030<br>DISC 021,022 | PARTIAL / PARTIAL / NOT_CONNECTED | 本PG新schema/index/backfill/down-up/reapply及无Save/Memory丢失回滚尚未实施/验收；原生产IdP/供应/发布仍BLOCKED。不能用本地Debug/真实费用probe替生产允许，root决定安全检查点与发布。 |
| BT-PG-20261008-26 | 增量交付与状态回写<br>covered | BT-FIX-NOW-UI-001 PARTIAL<br>BT-AUD-001 DONE<br>BT-V4-AUD-001 DONE<br>BT-V5-INT-001 DONE | NOW 001,030<br>DISC 001,021,022 | EXISTING / EXISTING_BASE / READ_ONLY_RECONCILIATION | 根代理须每阶段回写真实代码/测试/运行层，并最终核26项（含PG20主动接收或用户明确re-scope）；本次只对账，不把PG25或文档完成称全26DONE。 |

## 冲突与原能力保留

- **来源留存收紧**：新PG02事实PENDING覆盖未来新WSA写入，原批次保留待审；不据旧成功回执越过新门。
- **新Memory消费**：必须复用原用途边界。TASK_CONTEXT_READ/公开实体read不授外部模型发送，摘要/缓存/事件metadata也不是批准。
- **暂时条件与长期记忆**：本轮Task过滤保留；显式长期本人偏好走原Memory，普通行为走Evidence/candidate，敏感推断/自主行动仍关闭。
- **字段新鲜度与授权TTL**：独立检查，30秒原reply expiry与quiet GET不延长。
- **原DONE与新验收**：例如 AGE033 ContextBuilder与AGE049原五族controls已有code/local DONE历史；早期PARTIAL记录保留，新的PG17/18普通Now使用、忘记/导出/效果不据DONE自动完成。
- 新NOW冲突仍有效：plus可分材料与动作但只呈真实接入；所有业务沿同Now地图；空主页不再永久堆IntentPulse/MySocialNow/seed/internal面板；目的地选择与ONLINE/组织scope需分离。PG不撤销这些要求，也不新授外部行动。
- 原组织、聊天、活动、提醒、RSVP、私人资产、真人分享与机会通知保留。新成长主动通知尚未接/默认不启用，不将原所有通知误写默认OFF；兴趣不自动RSVP/邀请接受/发送/支付。

## 证据与独立校验

- 已读主指令、README、完整SPEC、JSON、CSV与VALIDATION。独立解析26项：唯一ID、JSON/CSV全部共用标量及两类依赖字段一致；内部依赖存在且DAG无环；SPEC含全部26 ID。
- SHA256SUMS列出的6个文件全部逐字节匹配；其清单本身hash及各输入hash在JSON中。此为材料结构校验PASS，**不是应用测试PASS**。
- NOW30/DISC22均保留完整逆向关系；NOW独立UI/语音/分享/导入等未被PG覆盖的范围明确保留；UX42通过原NOW映射保留，PG23/26为审计/回归关系，不是新增42任务。
- 旧API16/APK14仅限定Place回答/同ref卡→地图→详情→Recent及human-only合成图保存/读取/删除有界证据；不证明本地充分性/公库沉淀/Memory成长/中文全业务/新30UI或部署。
- 本次 application tests / SQL / provider / device / build / release / Git mutations：**NOT_RUN**。只观察文件，不调用taskctl、不碰数据库，不新增费用授权。
- 生产IdP、CSSA供给、LIVE独立验收、Pilot/release等原BLOCKED/TODO门保留。旧FAIL收据不覆盖；reserved/upper hold不当provider账单或实际节约。

## 下一原任务安全检查点

根代理为唯一队列/状态写入者。映射完成后本agent交回两文件所有权；后续优先落原候选的许可字段/操作闭集和营业字段链，WSA新持久化仍PENDING。明确Memory纵向沿原服务另做小切片，PG12/22守卫先于对应新写/出口。每阶段代码/测试/真实运行分别报告，最终覆盖26项（含PG20主动接收或用户明确重新定界），不能仅PG25发布门或文档完成就写DONE。

## 队列与原材料保存

- automation/codex_task_queue.json：`51a483dd3a6a890aab4edc2f89096d33c837a0cab94acae6e4f020597e6e3ad4`；与checkpoint05及本pass前后字节一致。
- automation/bt_v5_age_status.json：`74de1fdab27fb13559492a0ecb4d4d1ccec2b0b21923b4a0bf35a6bb2b4fac8b`；与checkpoint05及本pass前后字节一致。
- automation/v5_requirement_mapping.json：`984711f7556f370cad9abceb6f798f14dd8d62d3828e225d6a522d2a6f7abf11`；与checkpoint05及本pass前后字节一致。
- automation/snapshots/pre_v4_2026-10-01.json：`6aa908792736c4168e585df7b92b2db470b515055e6b148f2170fc0e813e8cc3`；与checkpoint05及本pass前后字节一致。

原主指令/README/SPEC/JSON/CSV/VALIDATION/SHA256SUMS、原NOW两映射及原共用报告前后hash相同。业务锚点为只读即时观察；其他agent获批并行改动不归本次映射，不能把锚点更新当该旧构建运行证据。


## 后续实际检查点 · 2026-10-09 API26/APK18

原只读记录保留；[实际代码与真实运行证据](../../testing/evidence/now-place-growth-2026-10-09/increment-26/REPORT.md)、[逐项差异与原任务映射](../../testing/evidence/now-place-growth-2026-10-09/increment-26/TASK-MAPPING.md)。原ID/历史不变，整包PARTIAL。


## 后续实际检查点 · 2026-10-09 API27/APK20

原记录保留；[实际代码和同构建运行证据](../../testing/evidence/now-place-growth-2026-10-09/increment-27/REPORT.md)、[逐项差异与原任务映射](../../testing/evidence/now-place-growth-2026-10-09/increment-27/TASK-MAPPING.md)。真实模型三轮及中文同Task地址通过；整包仍PARTIAL。
