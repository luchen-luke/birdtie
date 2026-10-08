# 本次 42 条 UXR 候选与原任务、当前代码对账（实际代码映射）

审查时间：2026-10-08；唯一正式仓库：`D:/Project/birdtie`；分支 `master`；HEAD `d6e86d3e6d82e17eaf9ca2e46d43b47e5e0cbf2e`。审查时有 1537 个 dirty 路径，包含本轮之前的 AIR/AGE/领域工作；没有把这些路径都认作本轮改动，没有重置或覆盖它们。

来源：用户指定 `BirdTie-UI-Repair-Package-2026-10-07.zip` 内的 01–05 与 `backlog-data.json`（42 个 ID/标题），当前正式仓库源码、原 live 队列以及本轮原始日志。此次证据整理与增量修复归属原 **BT-FIX-NOW-UI-001（PARTIAL，CODE_AND_LOCAL_VERIFICATION）**；以下“原任务 ID”是该修复所复用的领域任务，不是新领取或改写其原状态。BT-UXR-* 只是包内候选索引，未导入 live 队列。包内 DISC/LIFE 索引未在当前 live 队列查到，不能杜撰成已接通任务；真实发现链映射至现有 AGT、MAP、AIR 任务继续补缺口。

状态用语：**CODE+UNIT** = 已改实际代码且相关单元覆盖；**REUSED** = 复用现有实际路径；**PARTIAL** = 有实现但未满足完整候选验收；**UNCONNECTED** = 当前会话/接口能力未接通；**NOT_RUN** = 此次没有该验收证据。任何 CODE+UNIT 均不等于真实联网、真机或发布完成。下面所有候选都同时继承 BT-FIX-NOW-UI-001。

## 42 条完整映射

代码列的短链接均指向正式仓库当前实际文件，具体路径见文末；没有指向旧 worktree。

| 候选 ID | 主题 | 原任务 ID（相关领域） | 当前实际代码 | 本轮状态与实际缺口 |
|---|---|---|---|---|
| BT-UXR-001 | 锁定当前构建与现有任务 | BT-AUD-001；BT-V4-AUD-001；BT-V5-INT-001；BT-RUN-001 | [队列][queue]、[环境][env]、[工作区][ws] | REUSED/PARTIAL：APK05、API05、手机、源码前后 SHA 与录屏已绑定，见 BUILD05 与 followup03 凭证；原 APK04 专门保留 3→0 缺陷录屏。仍不把旧构建/新后端未构建增量当 APK05。 |
| BT-UXR-002 | 确定壳层与唯一状态归属 | BT-MAP-001；BT-AGT-003；BT-NOW-003 | [工作区][ws]、[控制器][ctrl]、[输入][composer]、[地图状态][mapstate] | CODE+UNIT：MapWorkspace 持有地图实例/视口与布局，控制器持有任务/每轮回复/选择，原生输入持有编辑状态。统一消息流不再依靠 results/conversation 分叉。全平台系统返回与动画仍 NOT_RUN。 |
| BT-UXR-003 | 统一游客/已登录/恢复中状态 | BT-AUT-001；BT-AUT-002；BT-V4-ACT-001 | [认证][auth]、[侧栏][sidebar]、[组织][org] | PARTIAL：游客/个人/组织真实身份复用；401/403 清除旧组织列表、迟到响应退役。尚未形成完整可见“恢复中”状态规范，真实生产 IdP 仍原 BLOCKED；真实登录恢复/退出验证 NOT_RUN。 |
| BT-UXR-004 | 按 capability 生成导航和动作 | BT-ORG-001；BT-V4-ACT-001；BT-V4-ORG-001；BT-V4-BIZ-002 | [侧栏][sidebar]、[组织][org]、[组织能力][orgcap] | CODE+UNIT/PARTIAL：按后端现有 owner/admin role 授权组织管理，游客/普通成员不露管理；已有领域动作能力沿用。并非完成通用所有 Business principal 工作区导航；BIZ-002 原 PARTIAL 保留。 |
| BT-UXR-005 | 服务端与深链越权反例 | BT-ORG-004；BT-V4-PRV-001；BT-V4-E2E-003；BT-V4-SAF-002 | [组织 HTTP][orghttp]、[组织成员][members]、[访问策略][access] | REUSED/NOT_RUN：原服务端授权边界保留，本轮组织 controller 401/403/撤权单元覆盖。该轮真实 API、深链、失效 token 与跨组织集成反例没有运行；原隐私矩阵 BLOCKED 不变。 |
| BT-UXR-006 | 有权管理者显式切换工作区 | BT-ORG-001；BT-V4-ACT-001；BT-V4-ORG-001；BT-V5-AGE-060 | [侧栏][sidebar]、[组织][org]、[工作区][ws]、[控制器][ctrl] | CODE+UNIT：账户内只列有权管理组织，显式切回个人；仅 active ID/role 实变才退役旧任务，账号 context 退役/迟到响应有用例。真实组织与个人记忆隔离集成 NOT_RUN；不宣称所有商家身份入口完成。 |
| BT-UXR-007 | 底部固定新对话与账户入口 | BT-AGT-004；BT-V4-NOW-001 | [侧栏][sidebar]、[壳层测试][shelltest] | CODE+UNIT/DEVICE：底部同一 Row 左新建、右账户；320×480/200%/50条长历史 12项 PASS；APK04 真机截图20观察布局与新建回调。完整系统读屏/所有账号态真机 NOT_RUN。 |
| BT-UXR-008 | 收敛导航并释放历史空间 | BT-V4-NOW-001；BT-V4-CHT-001；BT-PLN-001；BT-SAV-001 | [侧栏][sidebar] | CODE+UNIT/PARTIAL：Now/我的内容、已有个人资产/组织能力与 Recent 实际回调保留，中间空间随历史滚动；账户与新建固定。品牌区历史搜索尚未接入；社交/社区原领域路径仍在，未另造空页。 |
| BT-UXR-009 | 就地登录并回到原任务 | BT-AUT-001；BT-AUT-002；BT-V4-PIL-001 | [旧认证资料页][profile]、[认证][auth]、[工作区][ws]、[收藏][saved] | PARTIAL：账户入口实际通向原 Profile 登录/退出/资料，公开浏览与原会话保留。紧凑就地登录、登录后自动恢复此前 Save/Inbox 待办没有完成；真实 IdP/取消/恢复闭环 NOT_RUN。 |
| BT-UXR-010 | 实现三槽对称顶栏 | BT-V4-NOW-001；BT-POL-001 | [顶栏][top]、[会话测试][conversationtest] | CODE+UNIT：menu / 居中 city / Inbox，移走全局 tools；320/600 与 360/390/430 逻辑像素相关几何用例已通过。长城市名仍单行截断，完整名称辅助说明/真机读屏待验。 |
| BT-UXR-011 | 合并城市选择唯一入口 | BT-V4-CTX-002；BT-V4-NOW-006 | [顶栏][top]、[侧栏][sidebar]、[工作区][ws] | CODE+UNIT/DEVICE：原 NowContextSelectionController 的本人当前/过去/声明/线上及组织范围与城市在同一有限高度选择器，移除独立 Now 情境页入口；保留情境管理和线上领域发现页面。33项选择/身份/跨城市用例 PASS；APK05 匿名城市/线上提示同页截图32。本人 IdP 范围真机 NOT_RUN。 |
| BT-UXR-012 | 城市选择器自适应内容 | BT-V4-CTX-002；BT-V4-NOW-006 | [工作区][ws]、[城市目录][city] | PARTIAL：目录用受限最大高度 + shrinkWrap，只有一个城市不铺整页；loading/error/retry 使用实际目录。没有假当前定位。搜索/最近城市/定位如需展示仍须真实能力，当前未实现这些扩展。 |
| BT-UXR-013 | 分开任务目的地和地图视口 | BT-MAP-003；BT-V4-CTX-002；BT-V4-NOW-006 | [地图状态][mapstate]、[工作区][ws]、[城市目录][city] | CODE+UNIT/PARTIAL：已复核的异地任务提交同步原 City 控制器，未知城市保留原任务/地图/草稿且不提交；选择目的地不立即删除任务，ONLINE 不改地图，视口移动不改目的地。跨城市正负/迟到/ABA 用例 PASS；冷启动持久化及本人范围真机 NOT_RUN。 |
| BT-UXR-014 | 替换顶部标签与更多工具白页 | BT-NOW-001；BT-V4-NOW-001；BT-V4-MAP-001；BT-POL-001 | [工作区][ws]、[顶栏][top]、[图层][layers] | PARTIAL：tools 移至账户、地图图层原能力保留，去掉其查询情境入口和 composer 空闲预设建议。AreaPulseStack 及其信息区仍存在，未完成“一个轻量图层按钮”所有视觉收敛；定位/attribution 碰撞真机 NOT_RUN。 |
| BT-UXR-015 | 移除结果/对话模式分叉 | BT-AGT-002；BT-AGT-004；BT-V4-NOW-004 | [消息流][conversation]、[面板][sheet]、[控制器][ctrl] | CODE+UNIT/PARTIAL：控制器内每条 assistant 绑定自身结果快照、解释/卡片/来源同流；results mode 兼容 API 渲染同一消息流。远端恢复能保留先前真实文字/来源；跨重启旧轮实体集合尚未持久化，只有最后一轮读取当前结果，不用最新卡片嫁接旧轮。完整旧轮实体恢复尚未完成。 |
| BT-UXR-016 | 把状态句替换成真实回答链 | BT-AGT-001；BT-AGT-002；BT-V5-AIR-007；BT-V5-AIR-009；BT-V5-AIR-025；BT-V5-AIR-026；BT-V5-AIR-039 | [远端源][remote]、[Agent HTTP][agenthttp]、[网关][gateway]、[结果投影][projection] | PARTIAL/UNCONNECTED：原结构化站内读取与来源保留；真实模型/联网完整链尚未验收。原 AIR-009 已在 UI 安全检查点后串行接续，真实 Tencent transport、WSA 适配与原预算接线按缺口推进；009-LIVE BLOCKED，025/026 TODO 保留。数量句、dev-seed 与 provider key 不算真实回答。 |
| BT-UXR-017 | 统一地点/活动/故事卡 | BT-V4-NOW-004；BT-V4-PLC-005；BT-V4-MOM-001；BT-DET-001 | [实体卡][card]、[结果投影][projection]、[活动详情][detail] | PARTIAL：多实体使用同一 typed ref/来源/详情入口，空 summary/source 不伪造。活动日期/地点/作者/故事时序尚未在统一卡规范完整表达；Moment/story 不是现行投影类型，不能称三类统一已完成。 |
| BT-UXR-018 | 复用一套实体详情与选择预览 | BT-MAP-002；BT-NOW-003；BT-V4-ACTN-001；BT-V4-NOW-004 | [工作区][ws]、[控制器][ctrl]、[实体卡][card]、[地图][map] | CODE+UNIT/DEVICE：APK05 卡片→地图选中同体育馆→真实领域详情同名称/来源→返回保留；未伪造详情摘要成功。历史 result/迟到选择单元覆盖。完整 cluster、多种实体与身份真机矩阵 NOT_RUN。 |
| BT-UXR-019 | 把手独立居中与有限停靠状态 | BT-MAP-001；BT-AGT-003 | [面板][sheet]、[面板测试][sheettest]、[布局测试][layouttest] | CODE+UNIT：原中文把手语义/逐级 tap/drag 保留，中心把手与右侧 48dp map toggle 不重叠；peek/medium/expanded 有界，可点回地图。隐藏状态只作原兼容路径。真机手势 NOT_RUN。 |
| BT-UXR-020 | 输入聚焦时会话使用可用高度 | BT-MAP-001；BT-AGT-003 | [工作区][ws]、[面板][sheet]、[控制器][ctrl] | CODE+UNIT/DEVICE：APK05 输入 focus 后展开会话，中文 IME 上方消息与输入连续；保留地图选择，返回后可继续追问。窄屏/大字单元通过；多设备键盘动画/零露缝逐帧矩阵 NOT_RUN。 |
| BT-UXR-021 | IME/safe area 只避让一次 | BT-MAP-001；BT-PER-001 | [工作区][ws]、[面板][sheet]、[输入][composer] | CODE+UNIT/DEVICE：viewInsets 在根布局消费一次；APK05 实际中文 IME 打开、提交关闭、再次追问流程无重复避让，截图33/39。iOS/多IME/旋转矩阵 NOT_RUN。 |
| BT-UXR-022 | 统一面板与消息滚动归属 | BT-AGT-003；BT-PER-001 | [消息流][conversation]、[面板][sheet]、[可见性测试][visibilitytest] | CODE+UNIT：消息列表有持续 controller，读旧消息时停止自动跳底，新轮/底部追随；面板保留 outer/inner 协调而非 result/conversation 两棵互斥树。长卡来源与 200% 可滚到/点到已有测试；真机惯性/滚动阻尼 NOT_RUN。 |
| BT-UXR-023 | 原生编辑与中文 IME 正确提交 | BT-AGT-003；BT-V4-NOW-001 | [输入][composer]、[输入测试][composertest] | CODE+UNIT/DEVICE/PARTIAL：APK05 真实中文 T9 composing 候选确认后发送找地点、地点，续问3→3；原编辑/revision guards保留。系统复制/粘贴、多IME及逐会话持久草稿仍 NOT_RUN。 |
| BT-UXR-024 | 清理加号菜单语义 | BT-AGT-003；BT-V5-AIR-030；BT-V5-AIR-031 | [输入][composer]、[工作区][ws] | CODE+UNIT：当前 Now 没有真实当前会话附件链，已隐藏加号与假麦克风；旧素材 helper 代码保留作原兼容契约，不暴露成附件。私有 Moment 图片入口没有冒充聊天附件。 |
| BT-UXR-025 | 附件真实接到当前会话 | BT-V5-AIR-030；BT-V5-AIR-031；BT-V5-AIR-032；BT-V5-AGE-023 | [输入][composer]、[私有图片页][privateimage]、[媒体契约][media] | UNCONNECTED：没有当前 Agent 会话 attachment 预览/移除/发送/授权/失败保留链。原 human-only private Moment 图片路径继续保留，其权限不等于 model/current chat 发送许可；不写成完成。 |
| BT-UXR-026 | 粘贴后按内容给轻量后续动作 | BT-AGT-003；BT-V4-ACTN-001 | [输入][composer] | PARTIAL：原生粘贴与原素材 helper 的显式 clipboard read/草稿 guard 保留，无轮询/自动发送。当前正常会话输入的链接/列表粘贴后轻量预览与后续动作未接入，隐藏 helper 不作为完成依据。 |
| BT-UXR-027 | 统一叠层和返回优先级 | BT-MAP-001；BT-NOW-003；BT-PER-001 | [工作区][ws]、[输入][composer]、[通知边界][boundary] | PARTIAL：菜单/详情前 pauseEditing，关闭 drawer 回调防重复 pop，各路由有身份/epoch boundary；有明确键盘收起按钮。未找到完整统一 system back 叠层状态机，该轮系统返回、附件预览/菜单/详情顺序真机 NOT_RUN。 |
| BT-UXR-028 | 共享结果、相机和双向选择 | BT-MAP-001；BT-MAP-002；BT-V4-MAP-002；BT-V4-NOW-004 | [控制器][ctrl]、[工作区][ws]、[地图][map]、[状态测试][statetest] | CODE+UNIT/DEVICE：同 APK05/API05 20对会话/地图切换，20张地图视野/实体/计数区域像素完全一致，首末图和末会话人工核验3地点及选择保留；详情返回与追问3→3。完整任意pin/cluster双向选择、profile帧数据 NOT_RUN。 |
| BT-UXR-029 | 会话历史恢复与新建语义 | BT-AGT-004；BT-V4-CHT-001；BT-V4-PRV-001 | [控制器][ctrl]、[远端源][remote]、[侧栏][sidebar]、[输入][composer] | PARTIAL/CODE+UNIT+DEVICE：补真实 POST 失败后一次只读 Recent GET，保留原错误、会话、地图与已有历史；范围与轮次变化丢弃迟到结果，不自动 POST/恢复/完成。APK07 189 定向用例包含新增 25 例；真机实际失败后 Recent 显示原两个任务，选择最新任务经原 GET 恢复 1 个真实 Art Gallery 卡片、官方来源与同一地点地图。恢复规则回答不冒充模型回答。逐会话持久草稿与跨重启每轮结果快照尚未完整验收；游客临时历史不冒充账号持久历史。 |
| BT-UXR-030 | 检索/来源/错误统一呈现 | BT-AGT-002；BT-V4-NOW-004；BT-V5-AIR-024；BT-V5-AIR-039 | [消息流][conversation]、[面板][sheet]、[实体卡][card]、[远端源][remote] | CODE+UNIT/PARTIAL：真实 source label 与可点 HTTP(S) 引用同流；401/403/5xx/配置缺失、空结果与重试分开，不把读取失败当空回答。完整 tool-progress/partial success/model/web 错误事件链仍未接通/验收；AIR-024 原 PARTIAL。 |
| BT-UXR-031 | 替换 Inbox/我的活动大空白 | BT-INB-001；BT-PLN-001；BT-AUT-002 | [Inbox][inbox]、[我的活动][plans]、[旧认证资料页][profile] | PARTIAL：原 Inbox/Plans 的实际领域数据、刷新与错误重试保留。游客仍主要文本提示，未变为紧凑登录/取消后返回的具体行动；已登录空态的有效下一步尚未完整重排。 |
| BT-UXR-032 | 重排活动详情决策信息 | BT-DET-001；BT-RSV-001；BT-NTF-002；BT-V4-BIZ-004 | [活动详情][detail] | REUSED/PARTIAL：原日期/地点/费用未知/来源/RSVP/Save/Reminder 实际逻辑继续；每次真实活动 ID 重读不伪造状态。分享/发好友等仍多个按钮，完整决策层级与分享收拢不是本轮已完成项；外部报名不会写成原生报名成功。 |
| BT-UXR-033 | 统一字体/间距/按钮/圆角 | BT-POL-001 | [侧栏][sidebar]、[顶栏][top]、[面板][sheet]、[实体卡][card] | PARTIAL：面板使用 Theme、48dp 主控件，大字关键路径测试；仍有 sidebar/top 等局部硬编码字体、颜色、padding，未形成全产品单一 tokens。BT-POL-001 原 TODO，不因局部修复改 DONE。 |
| BT-UXR-034 | 长标题与文字缩放/读屏 | BT-POL-001；BT-TST-001 | [实体卡][card]、[面板][sheet]、[侧栏][sidebar]、[布局测试][layouttest] | CODE+UNIT/PARTIAL：完整卡标题/来源、大字暗色/滚动/触达/把手语义保留与覆盖。顶栏城市和 Recent 标题仍有限行 ellipsis；全产品200%/系统读屏焦点顺序/地图替代列表真机 NOT_RUN。 |
| BT-UXR-035 | 隔离本地测试与真实服务验收 | BT-DAT-002；BT-RUN-003；BT-TST-002；BT-V4-PIL-002；BT-V5-AIR-009-LIVE | [环境][env]、[远端源][remote]、[Agent HTTP][agenthttp] | REUSED/NOT_RUN：配置错误/无城市/失败不以 seed fallback 伪装成功，本轮 widget fixtures 明确属单元证据。API只读数据快照不等于联网发现链；dev-seed 来源、过期活动、building/unverified 城市不得算真实验收；原 LIVE/pilot 阻塞保留。 |
| BT-UXR-036 | 删除空字段孤立图标与旧组件调用 | BT-NOW-003；BT-V4-NOW-004；BT-DET-001；BT-POL-001 | [实体卡][card]、[面板][sheet]、[工作区][ws]、[活动详情][detail] | PARTIAL：互斥按钮、无真实接线加号/麦克风已撤，summary/source 缺失不造内容；统一流仍复用既有活动卡/AreaPulse 等领域组件。没有进行全域空字段/孤立图标扫清，更不删除原聊天/组织/资产能力。 |
| BT-UXR-037 | 改用用户理解的上下文语言 | BT-V4-CTX-002；BT-V4-NOW-006；BT-POL-001 | [顶栏][top]、[工作区][ws]、[侧栏][sidebar] | PARTIAL：统一选择器文案为城市与范围，本人范围/线上/组织均在该入口；已去掉 Now 独立选择情境页面。原情境管理页仍保留，未宣称全产品文案统一完成。 |
| BT-UXR-038 | 执行键盘/屏幕/权限状态回归 | BT-MAP-001；BT-PER-001；BT-TST-001；BT-TST-002；BT-V4-SAF-002 | [壳层测试][shelltest]、[状态测试][statetest]、[会话测试][conversationtest]、[布局测试][layouttest] | CODE+UNIT/DEVICE/PARTIAL：18个直接相关 Flutter 文件317 PASS，全量 analyze 0问题，Agent Go、API05/APK05构建 exit0；同 APK05 20对切换与中文IME实际运行。真实登录/撤权、多IME、其他平台/iOS NOT_RUN。 |
| BT-UXR-039 | 同构建真实发现与原能力回归 | BT-TST-002；BT-V4-E2E-001；BT-V4-E2E-002；BT-V4-PIL-002；BT-V5-AIR-054-LIVE | [工作区][ws]、[远端源][remote]、[Agent HTTP][agenthttp]、[收藏][saved] | NOT_RUN/PARTIAL：原组织/聊天/活动/提醒/个人资产路由保留，结果与来源/地图状态单元覆盖。尚无同 APK 的 NL→真实站内+联网→来源→可点卡/地图→追问→Save 全链及原能力真机回归；附件仍独立未接通。 |
| BT-UXR-040 | 交付同路径前后录屏与逐项证据 | BT-TST-001；BT-TST-002；BT-V4-PIL-002 | [初始快照][before]、[整合结果][finalresult]、[整合日志][finallog]、[构建日志][buildlog] | PARTIAL：APK04 3→0与APK05 3→3的特定缺陷前后视频已绑定各APK；同APK05/API05连续卡片/地图/详情/追问及20对切换录屏可复核。原始整套UI修复前全流程录屏、真实搜索来源回答链 NOT_RUN。 |
| BT-UXR-041 | 保留个人资产与 Civu 复用关系 | BT-SAV-001；BT-V4-MOM-001；BT-V5-AGE-023；BT-V5-AGE-027；BT-V5-AGE-029；BT-V5-AGE-030 | [侧栏][sidebar]、[收藏][saved]、[Moment][moment]、[私有图片页][privateimage]、[复用矩阵][civu] | REUSED/PARTIAL：我的内容保留活动/群组/Saved，原 Moment/媒体私有路径保留，Civu 接口与部分许可只读核验（见下）。Birdtie LifeMap/LifeImport 原 TODO；未实现的 Journey/ThemeMap 不新增空页、不冒充已有；未复制 Civu 代码或数据。 |
| BT-UXR-042 | 减少无意义动效与布局重建 | BT-PER-001；BT-PER-002；BT-V4-MAP-002；BT-POL-001 | [地图][map]、[工作区][ws]、[面板][sheet]、[消息流][conversation] | CODE+UNIT/PARTIAL：保留地图实例，结果/选择变化才更新视野，停靠动画尊重 disableAnimations；token/text 更新不替换地图组件。真实设备 profile/帧耗时/IME同步动画/reduced motion 系统设置回归 NOT_RUN；MAP-002 原 PARTIAL 保留。 |

## 本轮最终根代理验证更新

- 当前 UI 任务为 PARTIAL；原 AIR-009 在其自然检查点后串行接续。未导入新候选队列或修改原 DONE/LIVE gate。
- 最终 UI 冻结：18 文件 317 PASS，全量 analyze No issues found；Go AgentWorkspace、API05 和 Android build05 exit0；源码前后 hash 一致。凭证：[followup03](D:/Project/birdtie/work/ui-repair-2026-10-07/followup03-verification-results.json)。旧失败日志保留。
- Xiaomi Android16/c641566b，同APK05/API05实际首问3、续问3、卡片→地图→同地点详情→返回；20对视图切换地图核心像素一致。[真机凭证](D:/Project/birdtie/work/ui-repair-2026-10-07/device/BUILD05-EVIDENCE.json)、[切换核验](D:/Project/birdtie/work/ui-repair-2026-10-07/device/switch-cycles-05/visual-check.json)。数据明确为dev-seed开发示例。
- WSA/模型/移动地图 key 继续只在 Git 忽略的本地配置。API06 首次真机 Now 请求 FAILED；原 Task ACTIVE、Run STOPPED，CALL UNKNOWN ¥0.08 上界保留，模型未调用。不得把已接入或原测试数量当成真实回答。新 110 复用原 CITY Task 的公开条件，真实 SQL db-08 189 PASS；111 修复正常登录续期误使地图快照失效，实际地图 SQL db-09 34 PASS，均无供应商调用。APK07 已安装且 base.apk hash 核验，完整真实来源回答、卡片、地图、多轮仍待后续验收；原任务状态不因这两组测试而改为 DONE。

- 全量 `go test ./...` build07-backend-01 已真实运行并 FAILED：9074 PASS、143 FAIL、2805 SKIP。140 个失败事件来自原 native 测试要求显式隔离库但本次未提供，启动连接前拒绝；3 个事件是原只读工具描述遗漏实际 sources 字段的真实缺口，已修复并取得 agenttool 399 PASS。build07-backend-02 定向 493 PASS、0 FAIL、173 SKIP，API 与维护 CLI 构建成功；跳过项在该命令中为 NOT_RUN。原失败日志保留，后续定向通过不冒充全套通过。
- API07/APK07 新真实请求在 source-preview-payload 阶段 FAILED；原 Task ACTIVE、Run STOPPED，模型未获原生发送许可。新增 UNKNOWN CALL 上界 ¥0.08，与旧占用合计 ¥0.16，不是确认账单；无重发旧 operation、退款或重置。真机已验证失败后历史恢复、1 个官方 Place 卡片、来源在 Chrome 打开及同地点地图选择；完整真实模型与联网来源、多轮仍未完成。
- 以下“子任务对账阶段”日志为历史阶段，不覆盖本节新增根代理证据；未运行的原能力/真实身份/生产/性能回归不继承历史PASS。

## 2026-10-08 API10 / APK09 最新实际对账

本节补充表中历史版本证据，不改变原队列状态或把部分候选标为整体 DONE。

| 候选 | 本次实际改动、验证及边界 |
|---|---|
| 001、038、040 | APK09 15 个去重测试文件 315 PASS/0 FAIL/0 SKIP，analyze 无问题，APK build 成功，258 个生产输入/268 个测试输入前后一致；手机 base.apk hash 与安装包相同。API10 710 PASS/0 FAIL/173 SKIP，两个构建成功；SQL 跳过项是该命令 NOT_RUN。前述 Go 全套实际 FAILED 继续保留。08 视频包含失败，09 视频止于提交前，10 新视频实际包含失败；不能混作成功闭环。 |
| 007、010、011、012、027 | 四个新断言先复现 MapCanvas 无城市时重复入口，实际移除中央 CTA；只保留顶栏城市与范围入口。目录重读复用原 loadCities、零 Now；底部固定新建/账户、原 Tools/个人资产路径保留。112 项 UI 合约回归通过，原 36 FAIL 及中间失败日志保留。APK09 真机实际观察唯一入口、同一选择器及底部同 Row。完整系统返回与真实 IdP 撤权仍 NOT_RUN。 |
| 018、028、042 | 原生/Web 共用纯相机决策，仅坐标/范围/选中实体改变才聚焦；标题、排序、取消选择不重设用户视角。异步读写守卫检查当前输入、用户手势、生命周期，24 单元通过。APK09/API10 Recent GET 恢复真实 Art Gallery 卡，卡片→选中地图→同 Place 详情→官网来源打开→返回后卡片、pin 与预览仍同一地点。只证明这一实际流程，完整 cluster/离屏点选/跨重启/性能矩阵仍 NOT_RUN。 |
| 019、020、021、022、030、034 | 修复 IME 会话布局未避让真实地图署名的间距，6 几何回归通过；旧 layout 测试改到真实 conversation 状态，保留边界和地图身份断言。消息懒渲染用真实内层滚动显示同一 assistant 文本，不替换文本或弱化断言。多平台 IME 动画/惯性/读屏仍 NOT_RUN。 |
| 016、030、035、039；AIR-009/LIVE、025、026 | 真正接通原 Task/Run/062 的 WSA SearchPro，每次实际持久化 3 来源；原 4KB 消息上限改为确定性完整来源前缀，898 单元/子测试和 213 原生 SQL PASS。模型生成真实尝试失败：API08 原 NATIVE_ERROR 原因未明，API09 腾讯 HTTP400但无业务码，API10 捕获业务401006。官方含义为服务不存在/模型与服务不匹配；实际模型目录 GET200且 hy3 online，只证明目录认证，不证明服务绑定或生成可用。模型 sourced answer / 同结果卡图 / 多轮完整验收 **FAILED/NOT_COMPLETED**，原 gate 状态保留。 |
| 029 | 实际失败后 Recent 5 个原任务可见，选择最新 Task 经原 GET 恢复 1 个真实官网 Place 与地图；站内规则恢复不冒充模型回答。控制器内多轮集合已保留，但远端 Message 尚未持久化每轮实体 refs，跨重启旧轮卡片是实际剩余缺口；不把最新结果 graft 到旧轮。 |
| 024、025、026、031、033、041 | Now 加号仅真实接通附件方可展示，当前附件协议仍 UNCONNECTED；原组织、聊天、活动、提醒、私有 Moment、Saved 路由与队列未替换。紧凑登录/待办恢复、历史搜索、完整 Business 导航/全域 tokens、LIFE/导入等原缺口保留；未运行验收明确 NOT_RUN。三处官方 Place 仅经用户人审批准进入新隔离库，未复制 Civu 代码或数据。 |

凭据：`work/ui-repair-2026-10-07/build09-client-01/freeze.json`、`build10-backend-01.receipt.json`、`source-message-bound-01/result.json`、`live-source-native-db-10.result.json`、`ui-contract-regression-01/freeze.json`、`device/BUILD08-EVIDENCE-PARTIAL.json`、`BUILD09-EVIDENCE-PARTIAL.json`、`live10-native-fee-delta01.json`。API10/同 APK09 当前 UNKNOWN 最坏费用占用 ¥0.999040；各供应商请求上限 ≤¥0.20，原账户累计上限 ¥10；镜像 scope 不双加、旧 UNKNOWN 不释放，非账单或免费证明。原四个 queue/state 文件 working bytes 仍与初始 checkpoint05 一致。代码与原来源继续在正式仓库，没有另起项目。

后续真实全 Go 连接独立新测试库运行，10892 PASS/10 FAIL/23 SKIP，exit1；Postgres 包累计耗尽默认十分钟，680 顶层测试 NOT_RUN、两个 NOT_FINISHED_TIMEOUT，不能称全套通过。旧 062 shared fixture 与原最新问题契约冲突已只改测试，保留历史 canary 和所有安全断言：新独占 SQL 整组 33 PASS，CurrentQuery 17 PASS。个人 Memory 更新 503 在原 guard 31 次及仅隔离库诊断 guard 300 次未再现；根因 NOT_REPRODUCED/修复 NOT_IMPLEMENTED，原失败保留。详见 REPORT 的后续独立回归记录与 `fullgo-disposable-01`、`legacy062-current-query-fixture-01`、`memory-update-diag-01` 凭据。

源码与两份报告已正常推送 [GitHub main](https://github.com/luchen-luke/birdtie)，实际核验 commit `91962eb88aa74ca34b2df295e51f668c80c41b1a`，源树与四个原队列字节保留、凭据扫描零命中。其后 fixture 两份测试及本段证据将增量接续，不重写已有任务或把候选批量标成 DONE。同构建实际设备索引 `device/BUILD10-EVIDENCE-PARTIAL.json` 包含规则恢复的官网 Place 联动，**不等于模型/联网多轮完整发现验收**。

## 子任务对账阶段验证（历史证据）

- `flutter test --no-pub --concurrency=1 --reporter expanded`，11 个直接相关文件：`ui_repair_shell_test`、`ui_repair_state_test`、`ui_repair_conversation_test`、`agent_workspace_controller_test`、`map_canvas_test`、`agent_conversation_latest_visibility_test`、`agent_result_sheet_test`、`agent_result_sheet_layout_test`、`agent_composer_test`、`now_composer_material_test`、`map_workspace_shell_test`。目录 `D:/Project/birdtie/apps/client`，**211 PASS，exit 0**；生产输入在这次测试期间 hash 未变。见 [整合结果][finalresult] / [整合日志][finallog]。这是后续底栏 Row 修复前的检查点，不能自动覆盖其变更后构建。
- `agent-state-regression-03.log` 最终88 PASS；`sheet-regression-03.log` 最终43 PASS；`composer-shell-regression-04.log` 最终45 PASS；`new-conversation-05.log` 最终14 PASS。它们存在重叠测试，不能相加宣称总覆盖；原失败输出仍保留。
- [分析日志][analysislog]：Flutter analyze 无问题（9.2s）。[构建日志][buildlog]：assembleDebug 165.1s、成功输出 APK。其后生产源有继续改动，这两条日志不代表当前最终构建或真机已验收。
- 底栏 Row 变更后，`flutter test --no-pub --concurrency=1 --reporter expanded test/ui_repair_shell_test.dart`，目录 `D:/Project/birdtie/apps/client`，**12 PASS，exit 0**；见 [本次底栏日志][rowlog] / [命令与源码 hash][rowresult]。修改前非初始快照保存为 [device-build03-sidebar.dart][rowsnapshot]；该次测试没有操作设备。
- 此子任务对账阶段没有启动服务、写数据库、运行全套测试或操作设备。真实 IdP、生产/联网模型与搜索、真实 negative API 集成、同构建真机发现与原能力、手机前后录屏、20轮切换、设备性能及其他平台均 **NOT_RUN**（原 LIVE/pilot 条目继续 **BLOCKED**），由根代理后续按实际新证据更新。

## Civu 只读核验摘要（复用矩阵的实际补充依据）

先读 [既有复用矩阵][civu]，再选择性查 `D:/Program/Civu` 代码/依赖/许可证；没有读取私人用户数据、复制代码/数据或启动 Civu 服务。

- **地点**：`Civu-server/api/internal/placeprovider/provider.go` 的 Provider Search/Detail、Candidate 来源 URL/attribution/provider+externalID，以及 `store/migrations/018_place_provider_refs.sql` 历史 provider alias 可以作为接口适配参考。`store/place_types.go` 绑定 Civu region UUID、city string、taxonomy、reactions、journey、claims 等，不能直接替换 Birdtie canonical city/Place/source/freshness 模型。
- **Post/Moment**：Civu 实体实际是 `store/note_types.go` Note；`store/moment_map_context.go` viewer-safe GetMomentMapContext 对 note/thread/Place 再做可见性检查，可参考“同一实体、按 viewer 生成地图”。Birdtie 继续用本仓库 Moment、ownerAccount、canonical City、private/draft 默认及 human session approval，不搬 Note/Civu scope-key 草稿状态。
- **媒体地理关联**：`store/photo_locations.go` / `photo_locations_postgres.go` 的 mediaID→PlaceIDs+MapEnabled、50m complete-link grouping、属于本 note/author 的事务校验是可重构模型；client `models/moment_media_metadata.dart` 与 `moment_photo_assignment.dart` 可参考逐图片关联。`services/photo_location_metadata.dart` 的 DMS/EXIF parsing 是待单独许可审查的纯逻辑候选。`services/photo_upload_preparer.dart` 依赖 Civu native bridge；`httpapi/media_handlers.go` ready 图片可公开服务/跳转，不能移植到 Birdtie private original / quarantine / public derivative 契约。
- **Saved**：`store/place_save_sources_postgres.go` 的来源关联可见性重验可参考；Civu `store/notes_postgres.go:843` SaveNote 会发 author reaction 通知、对 public Note 自动同步 Place Saves，不能直接接 Birdtie SAVE。继续复用 Birdtie `saved_items.dart` + `/v1/me/saved` 独立收藏和 owner/visibility/expiry/block 校验。
- **许可**：Civu root、client、server、website、backend 顶层未找到 LICENSE/COPYING/NOTICE，不能据此声称全部业务代码有明确复制许可。local `packages/amap_map/LICENSE` 为 Apache-2.0，仅覆盖该包。当前解析缓存核见 exif3.3.0 MIT、dio5.11.1 MIT、x_amap_base1.0.3 Apache-2.0、pgx5.7.2 MIT；mapbox_maps_flutter2.26.0 LICENSE 指向 Mapbox 产品条款与账户许可，非整体自由复制许可。完整依赖许可/外部服务条款/数据权属迁移验收仍 NOT_RUN。矩阵应追加这些具体文件事实，不把“已审接口”改称“已迁移”。

## 代码与证据路径

[queue]: D:/Project/birdtie/automation/codex_task_queue.json
[env]: D:/Project/birdtie/apps/client/lib/src/config/birdtie_environment.dart
[ws]: D:/Project/birdtie/apps/client/lib/src/workspace/map_workspace.dart
[ctrl]: D:/Project/birdtie/apps/client/lib/src/workspace/agent_workspace_controller.dart
[composer]: D:/Project/birdtie/apps/client/lib/src/workspace/agent_composer.dart
[mapstate]: D:/Project/birdtie/apps/client/lib/src/workspace/map_entities.dart
[auth]: D:/Project/birdtie/apps/client/lib/src/auth/birdtie_auth_controller.dart
[sidebar]: D:/Project/birdtie/apps/client/lib/src/workspace/sidebar.dart
[org]: D:/Project/birdtie/apps/client/lib/src/workspace/organization_workspaces.dart
[orgcap]: D:/Project/birdtie/apps/api/internal/agentorganization/capability.go
[orghttp]: D:/Project/birdtie/apps/api/internal/httpapi/organizations.go
[members]: D:/Project/birdtie/apps/api/internal/httpapi/organization_memberships.go
[access]: D:/Project/birdtie/apps/api/internal/agentruntime/context_access.go
[profile]: D:/Project/birdtie/apps/client/lib/src/legacy/legacy_shell.dart
[saved]: D:/Project/birdtie/apps/client/lib/src/workspace/saved_items.dart
[top]: D:/Project/birdtie/apps/client/lib/src/workspace/top_controls.dart
[city]: D:/Project/birdtie/apps/client/lib/src/city/public_city_controller.dart
[layers]: D:/Project/birdtie/apps/client/lib/src/workspace/map_layers_controller.dart
[conversation]: D:/Project/birdtie/apps/client/lib/src/workspace/agent_conversation.dart
[sheet]: D:/Project/birdtie/apps/client/lib/src/workspace/agent_result_sheet.dart
[remote]: D:/Project/birdtie/apps/client/lib/src/workspace/remote_agent_task_source.dart
[agenthttp]: D:/Project/birdtie/apps/api/internal/httpapi/agent_workspace.go
[gateway]: D:/Project/birdtie/apps/api/internal/modelgateway/gateway.go
[projection]: D:/Project/birdtie/apps/api/internal/agentresultprojection/model.go
[card]: D:/Project/birdtie/apps/client/lib/src/workspace/agent_entity_result_card.dart
[detail]: D:/Project/birdtie/apps/client/lib/src/workspace/activity_detail_sheet.dart
[map]: D:/Project/birdtie/apps/client/lib/src/workspace/map_canvas.dart
[privateimage]: D:/Project/birdtie/apps/client/lib/src/workspace/now_private_moment_image_page.dart
[media]: D:/Project/birdtie/apps/api/internal/media/contract.go
[boundary]: D:/Project/birdtie/apps/client/lib/src/workspace/notification_destination_router.dart
[inbox]: D:/Project/birdtie/apps/client/lib/src/workspace/inbox.dart
[plans]: D:/Project/birdtie/apps/client/lib/src/workspace/activity_plans.dart
[moment]: D:/Project/birdtie/apps/api/internal/content/moment.go
[civu]: D:/Project/birdtie/docs/architecture/CIVU-REUSE-MATRIX.md
[shelltest]: D:/Project/birdtie/apps/client/test/ui_repair_shell_test.dart
[statetest]: D:/Project/birdtie/apps/client/test/ui_repair_state_test.dart
[conversationtest]: D:/Project/birdtie/apps/client/test/ui_repair_conversation_test.dart
[sheettest]: D:/Project/birdtie/apps/client/test/agent_result_sheet_test.dart
[layouttest]: D:/Project/birdtie/apps/client/test/agent_result_sheet_layout_test.dart
[visibilitytest]: D:/Project/birdtie/apps/client/test/agent_conversation_latest_visibility_test.dart
[composertest]: D:/Project/birdtie/apps/client/test/agent_composer_test.dart
[before]: D:/Project/birdtie/work/ui-repair-2026-10-07/before-files.json
[finalresult]: D:/Project/birdtie/work/ui-repair-2026-10-07/final-affected-tests.result.json
[finallog]: D:/Project/birdtie/work/ui-repair-2026-10-07/final-affected-tests.log
[analysislog]: D:/Project/birdtie/work/ui-repair-2026-10-07/analysis-full-05.log
[buildlog]: D:/Project/birdtie/work/ui-repair-2026-10-07/android-build-03.log
[rowlog]: D:/Project/birdtie/work/ui-repair-2026-10-07/sidebar-row-regression-06.log
[rowresult]: D:/Project/birdtie/work/ui-repair-2026-10-07/sidebar-row-regression-06.result.json
[rowsnapshot]: D:/Project/birdtie/work/ui-repair-2026-10-07/device-build03-sidebar.dart
