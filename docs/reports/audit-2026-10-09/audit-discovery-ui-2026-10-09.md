# Discovery22 与 UI Repair42 逐项补审（2026-10-09）

这是现有证据整理，共覆盖64个原来源ID，未创建任务、修改源码/队列或运行测试。DISC22、UI42、Now30、PG26相互重叠，不相加成120项独立开发工作。

实际读取范围：仓库根 `D:\Project\birdtie\BirdTie-Now-Agent-Discovery-P0-2026-10-07.md` 的§12全22条验收/依赖，§9–13相关语义；`docs/handoffs/2026-10-08-now/RECONCILIATION.md` 和JSON全部22/42反向映射；`docs/testing/evidence/ui-repair-2026-10-07/TASK-MAPPING.md` 的全部42行、代码引用和最新附录；已有API27/30/32/33报告及API34后端限制。没有把“读到22个ID”称为原整篇所有段落/测试已复验；没有重新执行DISC或UI场景。

最新已保存手机证据为API33/APK32；API34仅作者范围后端的SQL/HTTP检查，未证明TA地图客户端和真机链。所有候选的整体验收仍PARTIAL。表中“历史已有”保留当时有界结果，“后续局部证据”只补实际新范围；没有后续证据的项不会套用别项测试通过。

关键变化：已有真实模型有来源地点回答与同Task多轮、十个真实OSM餐厅、公开Moment→地点→作者→返回；但通用工具循环/语义追问、混合内容Agent检索、外部主办方原页/场次、SSE实体增量、全范围聚合、全产品设计tokens/读屏仍未交付。最近供应商402阻断真实模型复验；侧栏冷启动第一次点击无响应在APK31/32仍复现。

UI011旧“城市与本人/线上/组织范围合并展示”与最新NOW004要求“目的地只选城市，内部范围移走”有明确覆盖关系。旧UI011有测试不能作为新城市选择器验收；现选择器仍混排，封面/国家/搜索/最近/别名尚缺。

## DISC22 全部ID

| 来源ID | 范围 | 原验收要求 | NOW关联 | 已保存局部证据 | 当前仍缺 | 状态/证据 |
|---|---|---|---|---|---|---|
| BT-NOW-DISC-001 | 当前状态、故障复现与 Civu 复用核验 | 真实 repo/build、调用链、用户报告复现状态、源/目标证据；无破坏写入 | 001,004,023,024,027,030 | 真实仓库、原任务映射和API/APK冻结已有；本报告补读DISC§12及22/42反向索引。 | Civu逐模块许可/备份恢复、Journey/Theme迁移及全部用户故障复现未完成。 | PARTIAL；E0 |
| BT-NOW-DISC-002 | 统一 ResultSet、来源、选择和能力契约 | 与现有实体/任务/状态机制映射；不新增平行系统 | 007,008,010,028 | 原Task/typed Ref/来源/选择合同存在；API27地点卡图详情同实体；API33失败尾轮当前卡点归零。 | 所有材料与动作typed draft、全域来源/权限/恢复合同未整包；新范围不继承原DONE。 | PARTIAL；E1/E4 |
| BT-NOW-DISC-003 | 真实模型与工具循环 | 用户问题触发真实工具，结果回传模型；工具不可用明确报错 | 008,010,026 | API27真实3次模型+4轮同Task，API29真实十OSM餐厅；站内许可事实进入模型已证明。 | 通用模型自主多工具循环、工具后再规划、任意问题/普通账号范围未完成；最新腾讯402阻断。 | PARTIAL；E1/E2/E3 |
| BT-NOW-DISC-004 | 自然语言与短期上下文 | 城市/条件/时间识别，简单请求直接执行；三轮追问不丢约束 | 002,003,004,026 | 有界城市/筛选、裸地址、本地客观字段续问；API27同Task中文IME续问，API31名称地址实际HTTP。 | 任意三轮语义约束/第二家/安静点/范围约束组合、通用多实体仍未实现或未整体验收。 | PARTIAL；E1/E3 |
| BT-NOW-DISC-005 | 站内混合内容检索 | 地点、故事、媒体关联和活动可按文字/范围/时间检索，先权限过滤 | 006,022,025,026 | 原Place/Activity/Person领域检索存在；API30公开Moment可不问Agent在地图浏览，原许可十餐厅模型检索成功。 | Moment/story/媒体和活动按文字/范围/时间的统一混合Agent检索未完整接通；地图浏览不代替检索。 | PARTIAL；E2 |
| BT-NOW-DISC-006 | 最小媒体/故事发布到地图 | 一条真实有地理关联且合格的内容可被有权用户发现，复用 Civu | 011,012,022,027 | API30/API33真实发布接口写隔离合成Moment，B地点→Moment→作者→返回；API30同构建withdraw导航重验通过。 | 真实媒体/完整图文统一草稿→有权他人Agent检索发现→全缓存撤回链未完成；合成UGC不是现实经历。 | PARTIAL；E2/E4 |
| BT-NOW-DISC-007 | 站内 ACL 与撤权 | 结果、引用、缩略图、计数、缓存均不泄露不可见对象 | 011,012,013,019,020,022,028 | API30作者隐私成对NULL、私密404、Block/Moment和作者404及地图/历史移除；withdraw当前读取拒绝。 | 所有缩略图/媒体签发、计数/聚合跨页、缓存/模型引用即时撤权、全账号矩阵未整体验证。 | PARTIAL；E2/E4 |
| BT-NOW-DISC-008 | 联网搜索与详情读取 | 真实供应商/主办页面来源；失败不说已联网；不要求先入驻 | 014,015,026 | 历史API16真实WSA交通追问；API27使用已有合法OSM站内事实模型回答并可看来源。 | 通用read_external_source/主办正文细节未接；SearchPro持久化许可PENDING；来源控件不等于已读原网页。 | PARTIAL；E1/E0 |
| BT-NOW-DISC-009 | 外部场次、时区和新鲜度 | 过去/未来/售罄/取消/未知分开，搜索摘要不冒充已核实详情 | 015,025,026 | 原Activity时区/状态领域可复用；原地点营业字段TTL和未知资料诚实返回存在。 | 真实外部场次、未来/过去、取消/售罄、主办原页核实及新鲜度全链没有当前完成证据。 | PARTIAL；E0/E2 |
| BT-NOW-DISC-010 | Place 消歧与坐标来源 | 同名不误合并、无法定位不乱放、外部/站内引用可关联 | 003,004,012,015,016,021,026,027 | 原Place stable ID/anchor、独立审核许可；Overpass15条真实隔离发布与再次采集复用；API27原Glenhouse一致。 | 跨源同名消歧/国际同名/未知坐标/多地点故事/模糊引用未整包；有坐标不代表经营权或人实时在场。 | PARTIAL；E1/E0 |
| BT-NOW-DISC-011 | 融合排序和逐项来源 | 理由来自检索证据，来源可点击，未引用对象不虚构入回答 | 014,015,026 | API27真实有来源回复和紧凑引用；有界海鲜/辣Memory真实改变本地筛选排序。 | 多源融合排序/字段级事实支撑和质量评估未完成；Prego有邮编却模型说缺的矛盾仍存在。 | PARTIAL；E1/E3/E4 |
| BT-NOW-DISC-012 | 流式协议与版本隔离 | 已验证实体增量提交；旧 run 不覆盖当前；取消不泄漏后续写动作 | 007,010,028,029 | 原run/fence/current/迟到退休和版本检查存在；API33严格绑定Task末轮当前集合。 | SSE/WebSocket流式、实体增量提交与序号/版本协议没有完成证据；完整流式取消/写退休未验。 | PARTIAL；E4/E0 |
| BT-NOW-DISC-013 | Now 共享结果与视图生命周期 | 地图/对话切换不清空结果；相机、选中、图层按规则保留 | 005,007,018,023,024,029 | API30/APK26公共面板相机/列表/选择恢复；API32/APK30地图与消息阅读返回可见边界一致。 | 全身份/冷启动/逐会话草稿、Saved/Theme/Journey同地图状态、全键盘/分页仍未整包。 | PARTIAL；E2/E3 |
| BT-NOW-DISC-014 | 对话内实体卡与出处 | 对话不是纯文字；地点/活动/故事卡可真实打开和定位 | 008,014,016,022,025 | API27地点卡/出处/地图/详情真实链，API30公开Moment面板与作者可打开。 | 统一地点/活动/故事Agent卡规格与混合回答仍缺；公共Moment面板不等于故事已在Agent ResultSet里。 | PARTIAL；E1/E2 |
| BT-NOW-DISC-015 | 地图双向选择与真机稳定 | 卡→pin、pin→卡一致；键盘/面板/流式更新不让结果消失 | 007,016,021,029 | 同Glenhouse卡→pin→详情返回、公开面板返回、中文IME有界手机证据；相关原状态单元存在。 | 任意pin/cluster双向、连续流式、所有面板IME、性能和全设备未验；APK31/32侧栏冷首点仍FAIL。 | PARTIAL；E1/E3/E4 |
| BT-NOW-DISC-016 | 地图故事层、范围和聚合 | 不发消息也可浏览可见内容；地图移动不暗中改会话；重叠点可选 | 003,005,006,007,018,022,023,024 | API30 B不问Agent能在Now看原公开Moment地点/作者，隐私/Block/撤回过滤已有；API34新增public-author scope后端。 | 全发现/关注/好友/我的范围、distinct作者计数/分页、密集聚类T04/T06、TA地图客户端未完成。 | PARTIAL；E2/E4 |
| BT-NOW-DISC-017 | 连续追问与局部更新 | “第二家/安静点/只看周末/只看站内”正确作用于本轮结果和地图 | 026 | API27裸地址与预约资料问句，API31名称地址的闭合跟进；同Task与原筛选保留。 | “第二家/安静点/只周末/只站内”通用局部修改尚未整包；英语客观字段不是全部语义完成。 | PARTIAL；E1/E3 |
| BT-NOW-DISC-018 | 详情、原来源和私人收藏 | 真实读写、权限与幂等；外部内容不伪造报名或组织身份 | 013,014,016,017,018,019,020,021,023,024,025,027,028 | 原详情/share/Save真实领域已有；API27同地点来源/详情返回、API33本人原资料入口返回。 | 当前同构建实体Save→重开/整理/原图恢复、权限分享链接、真正route/Journey/Theme仍未完成。 | PARTIAL；E1/E4 |
| BT-NOW-DISC-019 | 故障隔离与诚实降级 | 模型/联网/站内/地图/定位独立状态；已成功结果不连带消失 | 002,003,005,006,015,017,028,029,030 | SearchPro可信PENDING503明确反馈；API32模型402保留输入/历史，API33失败ACTIVE不回退全城并显示尚未完成。 | 模型/联网/站内/地图/GPS完整独立状态和部分成功不连带丢失矩阵未全验；未知错误/重试仍需真流程。 | PARTIAL；E3/E4 |
| BT-NOW-DISC-020 | 运行边界、成本与可观测性 | 有界工具循环、超时取消、来源追踪、脱敏日志、供应商能力探针 | 001,009,010,011,013,019,020,024,028,030 | 原预算/once/timeout/UNKNOWN保留；API27–33实际请求ID与阶段记录，不自动重发；最新累计26预留上界保留。 | 通用多工具深度/循环、能力探针、生产质量/成本与观测未完成；实际现金UNKNOWN。 | PARTIAL；E1/E3/E4 |
| BT-NOW-DISC-021 | 自动化契约与场景测试 | §13 测试含成功、反例、乱序、撤权；夹具和真实来源证据分开 | 001,009,026,028,029,030 | 大量相关单元/HTTP/PG与负例历史已保存；近批按用户要求只跑直接相关测试，不累加重叠。 | DISC§13整场景未完整独立重跑，所有撤权/乱序/媒体/新领域覆盖不全；全量仅安排整合检查点。 | PARTIAL；E0/E1/E2/E3/E4 |
| BT-NOW-DISC-022 | 真实环境与真机放行 | 完成 §14 连续演示；原 CSSA/身份/权限回归；未实测不标通过 | 001,026,029,030 | API27真实有来源多轮、API30S1公开浏览、API32返回有界录像和构建配对证明。 | DISC§14全部连续演示、真实IdP/CSSA供给、生产环境、原能力全回归和消费者视觉仍缺，Pilot/Beta NO。 | PARTIAL；E1/E2/E3/E4 |

## UI42 全部ID

历史已有证据为原42行对应范围，完整原文不在本表重复。最新列只叠加确有记录的后续切片；“暂无新证据”不等于从未实现，仍按原有scope保留。

| 来源ID | 主题/原任务 | NOW关联 | 原有证据范围 | 后续局部证据 | 当前仍缺 | 状态 |
|---|---|---|---|---|---|---|
| BT-UXR-001 | 锁定当前构建与现有任务；BT-AUD-001,BT-V4-AUD-001,BT-V5-INT-001,BT-RUN-001 | 001,027,030 | REUSED/PARTIAL（E0，原表逐行） | API27/30/32/33保存增量源码与二进制/录像绑定；API34明确仅后端。 | 整包同构建证据不完整；不能以API34后端补验替代手机API33，也不继承历史PASS。 | PARTIAL |
| BT-UXR-002 | 确定壳层与唯一状态归属；BT-MAP-001,BT-AGT-003,BT-NOW-003 | 007,010,029 | CODE+UNIT（E0，原表逐行） | API30公共Place/Moment/Author面板复用同地图；API32父宿主及消息槽稳定key，阅读返回实测。 | 全平台返回/动画、统一typed draft及新对象跨域状态仍未验；侧栏冷首点仍复现。 | PARTIAL |
| BT-UXR-003 | 统一游客/已登录/恢复中状态；BT-AUT-001,BT-AUT-002,BT-V4-ACT-001 | 028 | CODE+UNIT/PARTIAL（E0，原表逐行） | API33本人公开主页进原资料页返回有界通过；未证明真实生产身份/资料保存。 | 生产IdP、跨进程恢复/退出/会话生命周期、全身份真机仍NOT_RUN。 | PARTIAL |
| BT-UXR-004 | 按 capability 生成导航和动作；BT-ORG-001,BT-V4-ACT-001,BT-V4-ORG-001,BT-V4-BIZ-002 | 028 | CODE+UNIT/PARTIAL（E0，原表逐行） | 暂无该项后续独立验收；复用原范围与已有领域实现。 | Business完整capability pack/runtime/工作台导航未完成；suspended元数据不是运行身份。 | PARTIAL |
| BT-UXR-005 | 服务端与深链越权反例；BT-ORG-004,BT-V4-PRV-001,BT-V4-E2E-003,BT-V4-SAF-002 | 020,028 | REUSED/NOT_RUN（E0，原表逐行） | 暂无该项后续独立验收；复用原范围与已有领域实现。 | 全深链/失效token/跨组织/撤权当前接口矩阵未全验；原SAF002仍BLOCKED。 | PARTIAL |
| BT-UXR-006 | 有权管理者显式切换工作区；BT-ORG-001,BT-V4-ACT-001,BT-V4-ORG-001,BT-V5-AGE-060 | 028 | CODE+UNIT（E0，原表逐行） | 暂无该项后续独立验收；复用原范围与已有领域实现。 | 真实组织授权及全部个人/组织隔离真机未验；不能授权Personal Memory给组织。 | PARTIAL |
| BT-UXR-007 | 底部固定新对话与账户入口；BT-AGT-004,BT-V4-NOW-001 | 008,029 | CODE+UNIT/DEVICE（E0，原表逐行） | Now后续固定底部账户仅图标和新建已沿原回调保留；全账号/读屏仍未验。 | 全账号态、辅助技术及系统返回未完成；侧栏首击无响应仍是产品缺陷。 | PARTIAL |
| BT-UXR-008 | 收敛导航并释放历史空间；BT-V4-NOW-001,BT-V4-CHT-001,BT-PLN-001,BT-SAV-001 | 005 | CODE+UNIT/PARTIAL（E0，原表逐行） | 首页常驻社交/seed/AreaPulse空卡移走并保留设置入口；Recent保留。 | 品牌区历史搜索未接；所有资产/组织角色路径未做当前全回归。 | PARTIAL |
| BT-UXR-009 | 就地登录并回到原任务；BT-AUT-001,BT-AUT-002,BT-V4-PIL-001 | 017,028 | CODE+UNIT/PARTIAL（E0，原表逐行） | 暂无该项后续独立验收；复用原范围与已有领域实现。 | 真实IdP/就地登录完整真机、Save/Inbox自动续办、恢复原操作未接通。 | PARTIAL |
| BT-UXR-010 | 实现三槽对称顶栏；BT-V4-NOW-001,BT-POL-001 | 004,029 | CODE+UNIT（E0，原表逐行） | 后续对称32dp视觉/48dp触摸胶囊已改；没有全消费视觉验收。 | 长城市名完整辅助说明/读屏；顶栏/指南针/探索区域等最终消费视觉需同构建验收。 | PARTIAL |
| BT-UXR-011 | 合并城市选择唯一入口；BT-V4-CTX-002,BT-V4-NOW-006 | 002,003,004 | CODE+UNIT/DEVICE（E0，原表逐行） | 最新NOW004明确覆盖旧合页范围设计；当前选择器仍混排城市与内部范围。 | 选择器仍混排城市/内部范围；封面/国家/搜索/最近/中英别名未完成。 | PARTIAL |
| BT-UXR-012 | 城市选择器自适应内容；BT-V4-CTX-002,BT-V4-NOW-006 | 002,004 | PARTIAL（E0，原表逐行） | 默认Aberdeen/探索城市secure storage已有相关单元；真实冷恢复和完整新选择器未验。 | 新目的地选择器功能/定位拒绝/实际冷恢复/国内provider失败全链未验。 | PARTIAL |
| BT-UXR-013 | 分开任务目的地和地图视口；BT-MAP-003,BT-V4-CTX-002,BT-V4-NOW-006 | 002,003,007 | CODE+UNIT/PARTIAL（E0，原表逐行） | 默认探索与Task临时城市分离；手选恢复迟到守卫有单元，GPS授权/相机冷恢复仍未验。 | GPS授权观察与探索分离、持久相机、跨城/临时对象退出全链未验。 | PARTIAL |
| BT-UXR-014 | 替换顶部标签与更多工具白页；BT-NOW-001,BT-V4-NOW-001,BT-V4-MAP-001,BT-POL-001 | 005,006 | PARTIAL（E0，原表逐行） | 后续已移走常驻AreaPulse/社交/两私人入口空卡；全类型bounds/数量尚未统一。 | 图层/数量/全类型范围和组件布局未收口；不能把0活动称0故事/地点。 | PARTIAL |
| BT-UXR-015 | 移除结果/对话模式分叉；BT-AGT-002,BT-AGT-004,BT-V4-NOW-004 | 010,014,026 | CODE+UNIT+DEVICE/PARTIAL（E0，原表逐行） | API27真实模型在同消息流，来源和卡内嵌；API32切地图返回阅读位置真机通过。 | 全部实体混合会话、故事卡、逐轮/失效/跨账号完整验收尚缺。 | PARTIAL |
| BT-UXR-016 | 把状态句替换成真实回答链；BT-AGT-001,BT-AGT-002,BT-V5-AIR-007,BT-V5-AIR-009,BT-V5-AIR-025,BT-V5-AIR-026,BT-V5-AIR-039 | 015,025,026 | 限定 LIVE/DEVICE/PARTIAL（E0，原表逐行） | API27真实模型3次+同Task4轮，API29十OSM餐厅成功；最新API32腾讯402且事实矛盾未解决。 | 通用自主工具循环/多领域中文多轮未完成；供应商402、Prego事实矛盾未解决。 | PARTIAL |
| BT-UXR-017 | 统一地点/活动/故事卡；BT-V4-NOW-004,BT-V4-PLC-005,BT-V4-MOM-001,BT-DET-001 | 006,014,015,016,022,025 | PARTIAL（E0，原表逐行） | API30公开Moment/作者面板加入Now；不等于活动/故事全部Agent卡统一。 | 地点/活动/故事三类统一内容层级、字段/来源/时间语义尚未完成。 | PARTIAL |
| BT-UXR-018 | 复用一套实体详情与选择预览；BT-MAP-002,BT-NOW-003,BT-V4-ACTN-001,BT-V4-NOW-004 | 007,016,019,021 | CODE+UNIT/DEVICE（E0，原表逐行） | API27同Glenhouse卡图详情；API30同地图公开面板多层返回；API33本人资料返回。 | 任意cluster/多实体/全身份，Saved/Theme/Journey临时详情与原图恢复尚缺。 | PARTIAL |
| BT-UXR-019 | 把手独立居中与有限停靠状态；BT-MAP-001,BT-AGT-003 | 029 | CODE+UNIT（E0，原表逐行） | API30公开Place面板收起/返回保持地图与列表；所有高度/手势矩阵未验。 | 所有面板高度、drag/back、IME/小屏/大字/辅助技术组合未验。 | PARTIAL |
| BT-UXR-020 | 输入聚焦时会话使用可用高度；BT-MAP-001,BT-AGT-003 | 029 | CODE+UNIT/DEVICE（E0，原表逐行） | API27中文IME地址实际发送，新回复优先进入可见区；全设备动画与高度未验。 | 多设备键盘动画/零露缝/嵌套多面板的全部可用高度未验。 | PARTIAL |
| BT-UXR-021 | IME/safe area 只避让一次；BT-MAP-001,BT-PER-001 | 029 | CODE+UNIT/DEVICE（E0，原表逐行） | API27中文选词确认后发送，输入提交和发送分开；全平台安全区/旋转未验。 | iOS/多IME/旋转/safe area矩阵未验；单元PASS不等于当前所有手机排版。 | PARTIAL |
| BT-UXR-022 | 统一面板与消息滚动归属；BT-AGT-003,BT-PER-001 | 029 | CODE+UNIT（E0，原表逐行） | API32/APK30原父页面稳定key后手动阅读切地图返回全部可见文字和边界一致；早期失败保留。 | 列表分页/多动态、惯性阻尼、多个嵌套面板/全部消息返回状态未验。 | PARTIAL |
| BT-UXR-023 | 原生编辑与中文 IME 正确提交；BT-AGT-003,BT-V4-NOW-001 | 026,029 | CODE+UNIT/DEVICE/PARTIAL（E0，原表逐行） | API27真实中文九键选词地址发送有原71秒录屏；系统编辑/多IME矩阵未全验。 | 系统复制粘贴、多IME、逐会话持久草稿/进程恢复未验。 | PARTIAL |
| BT-UXR-024 | 清理加号菜单语义；BT-AGT-003,BT-V5-AIR-030,BT-V5-AIR-031 | 008,009,010,011 | CODE+UNIT/限定 DEVICE/PARTIAL（E0，原表逐行） | 后续支持本机PNG/JPEG检查和文字/链接到唯一输入；材料/行动菜单与typed draft整体未接。 | 语音/视频/文件/地点材料与发布/收藏/Theme/Journey/Activity typed draft未完成。 | PARTIAL |
| BT-UXR-025 | 附件真实接到当前会话；BT-V5-AIR-030,BT-V5-AIR-031,BT-V5-AIR-032,BT-V5-AGE-023 | 008,011,012,022,028 | CODE+UNIT+NATIVE_HTTP/限定 DEVICE/PARTIAL（E0，原表逐行） | API16/APK14合成human-only图片保存/像素读/删设备链；首次无Task相关单元后续已有，完整冷恢复/视频/文件仍缺。 | 图片不进模型；完整冷恢复/图库权限/续传与视频/文件未完成。 | PARTIAL |
| BT-UXR-026 | 粘贴后按内容给轻量后续动作；BT-AGT-003,BT-V4-ACTN-001 | 008,009,010,013,024 | PARTIAL（E0，原表逐行） | 后续短文字/链接粘贴入当前草稿；轻量内容解析/列表导入/跨类型业务草稿未接。 | 链接/列表/长文/外部计划解析后的可编辑动作未接；不自动执行导入内容。 | PARTIAL |
| BT-UXR-027 | 统一叠层和返回优先级；BT-MAP-001,BT-NOW-003,BT-PER-001 | 007,011,028,029 | PARTIAL（E0，原表逐行） | API30公开Place→Moment→Author→两层返回→关闭恢复有界录像；API33本人入口返回。 | 统一system back优先级，菜单/附件/键盘/领域草稿/详情全叠层矩阵未验。 | PARTIAL |
| BT-UXR-028 | 共享结果、相机和双向选择；BT-MAP-001,BT-MAP-002,BT-V4-MAP-002,BT-V4-NOW-004 | 003,006,007,016,021,026,029 | CODE+UNIT/DEVICE/PARTIAL（E0，原表逐行） | API27地点卡图详情同ID；API29十餐厅，API32刷新后的同实体/源校验；API33失败卡点归零。 | 任意pin/聚类双向、全范围/失效/撤权、流式/性能全矩阵未验。 | PARTIAL |
| BT-UXR-029 | 会话历史恢复与新建语义；BT-AGT-004,BT-V4-CHT-001,BT-V4-PRV-001 | 007 | PARTIAL/CODE+UNIT+DEVICE（E0，原表逐行） | 原Task多轮历史保留；API32地图/消息返回阅读位置，API33失败末轮当前空集诚实；逐会话草稿/自动恢复仍缺。 | 逐会话持久草稿、自动冷启动任务恢复、全身份隔离未完整实现/验收。 | PARTIAL |
| BT-UXR-030 | 检索/来源/错误统一呈现；BT-AGT-002,BT-V4-NOW-004,BT-V5-AIR-024,BT-V5-AIR-039 | 014,015 | CODE+UNIT+限定 LIVE/PARTIAL（E0，原表逐行） | 紧凑来源和原网站/维护者区分；API33失败ACTIVE解释尚未完成；邮编事实矛盾仍保留。 | 完整tool-progress/partial/error、所有来源事实核验和字段冲突可见性仍缺。 | PARTIAL |
| BT-UXR-031 | 替换 Inbox/我的活动大空白；BT-INB-001,BT-PLN-001,BT-AUT-002 | 030 | CODE+UNIT/PARTIAL（E0，原表逐行） | 暂无该项后续独立验收；复用原范围与已有领域实现。 | 已登录全部空态下一步、真实IdP/Inbox/Plans完整当前手机链未验。 | PARTIAL |
| BT-UXR-032 | 重排活动详情决策信息；BT-DET-001,BT-RSV-001,BT-NTF-002,BT-V4-BIZ-004 | 013,016,017,019,025 | REUSED/PARTIAL（E0，原表逐行） | 暂无该项后续独立验收；复用原范围与已有领域实现。 | 活动费用/时间/报名决策层级和分享收拢未完成；真实授权主办方/活动不足。 | PARTIAL |
| BT-UXR-033 | 统一字体/间距/按钮/圆角；BT-POL-001 | 029 | PARTIAL（E0，原表逐行） | 后续Now局部胶囊/来源/卡片精简；全产品统一tokens未实现，BT-POL-001仍TODO。 | 全产品单一tokens/按钮/字体/间距/圆角未完成；消费视觉未验收。 | PARTIAL |
| BT-UXR-034 | 长标题与文字缩放/读屏；BT-POL-001,BT-TST-001 | 014,029 | CODE+UNIT/PARTIAL（E0，原表逐行） | 后续来源短标签点开完整名称；全产品200%/读屏/TalkBack未实际验。 | 全产品200%/长标题/系统读屏焦点/地图可访问替代列表真机未验。 | PARTIAL |
| BT-UXR-035 | 隔离本地测试与真实服务验收；BT-DAT-002,BT-RUN-003,BT-TST-002,BT-V4-PIL-002,BT-V5-AIR-009-LIVE | 001,002,015,026,030 | REUSED/限定 LIVE/DEVICE/PARTIAL（E0，原表逐行） | API27/29真实模型使用合法站内OSM地点，API30UGC明确合成；非生产数据和现实用户。 | 真实生产环境/现实组织活动、权限/供给/完整LIVE/Pilot门未通过。 | PARTIAL |
| BT-UXR-036 | 删除空字段孤立图标与旧组件调用；BT-NOW-003,BT-V4-NOW-004,BT-DET-001,BT-POL-001 | 005,006,016 | PARTIAL（E0，原表逐行） | 首页常驻空态和私人入口已移走；最新失败模型语义区分，其他全域空字段收敛未验。 | 全域空字段/孤立图标/旧组件调用未全部核对；最新双空态等仍需收口。 | PARTIAL |
| BT-UXR-037 | 改用用户理解的上下文语言；BT-V4-CTX-002,BT-V4-NOW-006,BT-POL-001 | 003,004,005 | PARTIAL（E0，原表逐行） | 最新Now明确目的地城市与内部范围分开；当前选择器仍混排，未完成全产品文案。 | 用户理解的城市/范围与所有产品文案未统一；旧入口完成不能代表新设计完成。 | PARTIAL |
| BT-UXR-038 | 执行键盘/屏幕/权限状态回归；BT-MAP-001,BT-PER-001,BT-TST-001,BT-TST-002,BT-V4-SAF-002 | 001,026,028,029,030 | CODE+UNIT/限定 DEVICE/PARTIAL（E0，原表逐行） | API27中文IME、API30面板、API32阅读返回、API33本人返回有界证据；并非完整状态矩阵。 | 全部登录/撤权、手机pending journal、其他平台/IME/读屏矩阵未验。 | PARTIAL |
| BT-UXR-039 | 同构建真实发现与原能力回归；BT-TST-002,BT-V4-E2E-001,BT-V4-E2E-002,BT-V4-PIL-002,BT-V5-AIR-054-LIVE | 001,017,026,030 | 限定 LIVE/DEVICE/PARTIAL（E0，原表逐行） | 后续API27/30/32/33有同构建链；Save/原CSSA/全领域回归和全文事实未完成。 | 当前实体Save、原能力全回归、通用多轮、AI视觉和正式闭环未完成。 | PARTIAL |
| BT-UXR-040 | 交付同路径前后录屏与逐项证据；BT-TST-001,BT-TST-002,BT-V4-PIL-002 | 001,026,030 | PARTIAL（E0，原表逐行） | 新增API27原中文录屏、API30S1/withdraw、API32返回、API33本人返回原录像；原失败保留。 | 64来源全部验收/完整连续演示及所有前后同路径对照仍未完成。 | PARTIAL |
| BT-UXR-041 | 保留个人资产与 Civu 复用关系；BT-SAV-001,BT-V4-MOM-001,BT-V5-AGE-023,BT-V5-AGE-027,BT-V5-AGE-029,BT-V5-AGE-030 | 004,007,010,011,012,013,017,018,019,020,022,023,024,025,027,030 | REUSED/PARTIAL（E0，原表逐行） | 原个人资产继续保留；Saved整理上图/路线/Journey/ThemeMap仍未接通。 | ThemeMap/Journey/导入/收藏整理上图未接，Civu生产迁移未执行。 | PARTIAL |
| BT-UXR-042 | 减少无意义动效与布局重建；BT-PER-001,BT-PER-002,BT-V4-MAP-002,BT-POL-001 | 007,029,030 | CODE+UNIT/PARTIAL（E0，原表逐行） | 稳定地图、相机与消息宿主生命周期已有局部修复；真实profile/帧率/减弱动效全矩阵未验。 | 真实profile/帧耗时/系统reduced motion/IME同步性能未验；原MAP002仍PARTIAL。 | PARTIAL |

## 可定位的证据来源

- E0：docs/testing/evidence/ui-repair-2026-10-07/TASK-MAPPING.md 全42行及末尾API16/APK14；docs/handoffs/2026-10-08-now/RECONCILIATION.md/.json 的22/42反向映射；根Discovery原文§12。
- E1：docs/testing/evidence/now-place-growth-2026-10-09/increment-27/REPORT.md，API27/APK20真实模型、同实体、中文IME。
- E2：docs/testing/evidence/now-place-growth-2026-10-09/increment-30/REPORT.md，API29十餐厅与API30/APK26公开Moment/作者/返回/撤回。
- E3：docs/testing/evidence/now-place-growth-2026-10-09/increment-32/REPORT.md，API32/APK30阅读返回、Follow中间构建与供应商402。
- E4：docs/testing/evidence/now-place-growth-2026-10-09/increment-33/REPORT.md，API33/APK32失败尾轮空集、本人与原资料返回、API34仅作者后端、侧栏首击FAIL。
- 更完整NOW/PG差异见D:/BirdTie/audit-now-pg-56-2026-10-09.md；原任务当前状态见audit-tasks-256-2026-10-09.md。

只读保护：队列SHA256前后均507F6E011C40F925442F1FC0B7D05FA4C4A174B32662308EF3EE1BAFBEBD226E；无测试/代码/队列变更。
