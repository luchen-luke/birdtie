# BirdTie 本期消费者界面与 Agent 修复验收

日期：2026-10-10，Asia/Shanghai。正式工作区 `D:\Project\birdtie`。沿用 `BT-FIX-NOW-UI-001`；本轮没有新建重复队列，没有覆盖开始前的未提交修改，没有部署、生成 ZIP 或录屏。

**本轮明确范围通过，整项目仍为 PARTIAL。** 当前手机与本地 API 已实际运行新代码。公共餐厅多轮、历史协议、卡片详情、材料入口和个人页面导航有对应测试或真机证据；付费模型恢复、所有类别外源数据和全应用业务分支验收没有因此完成。

## 用户问题与实际结果

| 问题 | 当前结果与边界 |
| --- | --- |
| 启动“无法读取城市” | 原因是本地 API 进程退出，手机回环地址依赖电脑及 ADB 转发。此前已实现有限重试和本地运行监督；本轮实际启动、重开及城市选择正常。电脑关机或脱离调试连接后的独立使用仍需在线后端，不是客户端改字体可解决。原进程最初退出原因未确定。[城市修复](BIRDTIE-STARTUP-CITY-RECOVERY-2026-10-10.md)。 |
| “有没有好的餐厅”只有零个已发布地点 | 已接通的公开餐厅发现和数据库缓存返回 15 家。真机本轮实际重新提交该句及“第一家地址”成功。当前资料只能证明名称、地址、坐标，不能证明“最好”、评分、菜系或实时营业。[已有发现与缓存实现](BIRDTIE-RESTAURANT-HISTORY-CACHE-REPAIR-2026-10-10.md)。 |
| 结果过会儿消失 | v2 每轮保留完整公开快照、原回答、来源和时间；新鲜度到期提示更新，不删除历史。已实际 force-stop/重启再从最近对话恢复本轮四条消息及卡片。没有自动打开旧会话；24 小时真实等待为 NOT_RUN。 |
| 后续询问仍重复搜索或失去上下文 | 新增封闭的公共餐厅多轮：序号、真实名称、全部地址、前两家比较、到城市中心直线距离；保留同一批有序卡片。前后模型记录 10→10、缓存版本 1／15 家及事实指纹不变。本轮没有新增外部搜索或付费模型调用。 |
| 新问题被旧餐厅抢答 | 公园、活动、组织、酒店／咖啡馆等明确新主题退出旧餐厅分支；最新回答没有餐厅快照时，不越过该轮捞取更早餐厅。退出旧分支不等于所有新类别的在线发现能力已完成。 |
| 普通回应显示“服务返回的数据暂时无法读取” | 手机实测发现本人 CITY 普通 POST 的 `messageResults:null`，GET 却有权威 List。后端已补本人 POST 的原权威历史读取及最终身份复核；Flutter 严格协议保持。API06 真实四轮 POST/GET/SQL 一致，前端离线整包解析回归通过。旧结果仅作历史，不提升成新回答。 |
| 地点只出现一张卡片 | 全部餐厅列表与地图选中预览复用现有卡片，提供地图和详情动作。收紧过大的留白；长标题自然换行；窄屏操作改为可换行布局，保持 48dp。最后真机发现详情满高时侵入状态栏，APK04 增加安全区并实测通过。 |
| Memory 看不到内容 | 已允许有效 Memory 列表显示原摘要及来源时间，详情仍有原授权期限。失效／删除项不泄漏旧摘要。真机本人账号当前为空，没有制造私人记忆或执行记住／删除操作；有内容的布局、失效和撤权由组件测试验证。 |
| 材料、粘贴与设置粗糙或不稳定 | 实际接上 Now 私人图片入口，保留私人 Moment 来源与草稿边界；修复异步粘贴跨任务／城市／来源或迟到时错写；修复来源退休后旧订阅异常。设置→我的智能体原双标题栏已改为完整页面导航，真机一层标题及返回设置／Now 通过。 |

## 后端与 Agent 的实际设计和编码

客户端只把原问句与公开引用（版本、城市／地图范围、有序地点 ID）交给服务器；不提交自造地点事实。服务器从既有 PostgreSQL 公开资料库重读批次、许可和字段，核对范围、顺序、版本与事实变化后才回答。本人会话仍走原 owner/session/Task 检查及事务保存；访客引用不能读取账号 Task，组织与 ONLINE 会话没有被放宽。

这使“第一家地址”“有哪些地址”“比较前两家”“哪家离市中心近”真正复用已有数据，不靠模型重写客观事实。距离明确为城市中心直线距离。未知评分、菜系、价格、营业与预订会明确说明资料不足。原生 FindPlace 的封闭字段查询直接整理已许可字段、引用和缺项；原生来源自然过期时，限定的按名称地址／营业询问返回准确的 503，而非进入模型后变成 500。新鲜原生全字段真实验收仍是 NOT_RUN，未延长来源 TTL 来伪造成功。

公开资料已有 `external_public_places` 与整批搜索缓存，按提供方 ID 去重；不保存私人原话／会话关联，不自动发布为原生地点。保持 UNREVIEWED、OSM 来源与 ODbL 署名。主动“更新餐厅信息”沿用已有显式更新路径。本轮没有新迁移、导入或来源写入。详细代码、失败复现、权限和事实边界见 [后端记录](../testing/evidence/consumer-agent-repair-2026-10-10/backend-agent/README.md)。

模型配置仍为原试用 `tencent_tokenhub / deepseek-v4-pro-0813`，输出上限 768，沿用 owner、出口与预算限制。原单次 ¥0.20／累计 ¥10 试用限额保留；本轮新增付费调用为 0，不把数据库模型记录当作账单节省。现存最后一次真实供应商故障是 HTTP402／业务403004；账号恢复后的新成功调用 NOT_RUN，没有静默换供应商。现有私人偏好只在许可且有真实食品字段的原生路径使用，不能把公开15家猜成全面个性化推荐。

## 最终文字规范

沿用用户已确认并推广的规范；本轮增量没有替换字体或引入字体服务。全局样式来源为 `NowUiTokens`、`birdtieTheme`、`birdtieAppBar`。此前全应用 75 个生产文件的字体推广保留，不能算成本轮新增文件。[字体来源、全页面清单与上一阶段验收](BIRDTIE-TYPOGRAPHY-UNIFICATION-2026-10-10.md)。

| 语义角色 | 默认逻辑像素／字重／行高 |
| --- | --- |
| 页面主标题 | 20／w600／1.4 |
| 面板标题 | 18／w600／1.5 |
| 地点／动态卡片与分区标题 | 16／w500／1.5 |
| 正文、Agent 回答、地址 | 15／w400／1.5 |
| Now 输入／其他表单 | 16／w400／1.5；其他表单15 |
| 城市、菜单、筛选／按钮 | 15／w500／1.5；按钮14 |
| 时间、来源、辅助说明 | 13／w400／1.5 |

浅色标题 `#20242A`、正文 `#374151`、辅助 `#566273`；按钮使用现有语义 primary。建议的 `#667085` 在原半透明／气泡背景不满足4.5，故统一使用更深辅助色。深色使用独立 ColorScheme 语义色。上一阶段合成背景计算最低浅色4.8096:1、深色5.4643:1；本轮颜色未改变，逐像素抗锯齿测量 NOT_RUN。Android 延续平台字体及中文回退（该机 MiSansVF），实际逐字形 Skia 跟踪 NOT_RUN。

保持系统 TextScaler，未用全页缩放；保留 Now V2 面板边界、单地图、唯一输入框及键盘管理。正文不整段加粗，长中文／中英文自然换行；可滚动而非固定高度省略，主要操作至少48×48。

## 构建与真实截图

每张 PNG 均为手机 c641566b 原始 Android 截图，附 JSON 的实际 APK、API（新收据）、系统字号、主题、尺寸、时间和 SHA。51 张现存截图逐一 hash 核对。状态栏包含手机实际通话／提醒状态，未裁剪修改，也未操作用户电话／闹钟。常规1220×2656、density520约375dp；窄屏临时1040×2265即320dp。最后已恢复原尺寸、100%字号和浅色。

| 构建 | SHA256／用途 |
| --- | --- |
| 本轮前：字体阶段05 | `1e125c0e48f352485d8f18bbf0814464b767efcde2a6fa0978b92bbe57074d20` |
| 本轮 APK01 | `168df68bac3929ded7ce43eb80ff947ee436125e830a0a093c50d353658ed9ab`；材料入口、列表／详情与100／130浅色、200深色 |
| APK02 | `91f28973a70cbd0a3c5a0ecfaa6f525ee86c98c16b7c125e7eb98a3cfb2a5601`；消费者503文案 |
| APK03 | `87ecf191255115eea344ee883797af6f8b3dba6b25e812bb8c5c69ab3e76623b`；个人完整页面导航、真实新餐厅／追问与重启恢复 |
| **当前 APK04** | **`66b1c0dbaf8577819119c3e2f7f57fe60c4dd9e6003568d3c6921381636477ea`**；03:42:47–03:43:00 UTC构建，debug build exit0，原数据 install-r；详情安全区 |
| **当前 API06** | **`e46962e28d25bea5221bc010376a2ec644e1d1c8cc937fec0d6829ed7053f15b`**；03:19:37 UTC构建，exit0，本地18109实际运行 |

当前 APK04 的280个输入、API06的552个输入与源字节一致，磁盘构建 artifact hash 已核对。相邻客户端01→02只变远程503文案、02→03只变 map_workspace 导航、03→04只变地点详情安全区；API04→05只变原生空源处理，05→06只变本人POST历史投影。[精确构建差异](../testing/evidence/consumer-agent-repair-2026-10-10/builds/receipt.json)。早期截图保留原构建身份，不冒充全部在04重拍。API目录中的 runtime.receipt 是启动模板，实际运行依据是注册配置、EXE hash 和实际 HTTP；不把模板 ready:false 当健康验证。

以下均实际看图核对；相同设备、内容、主题及字号的配对使用同一公开批次／采集时间，地图视口及滚动位置有差异，不声称像素完全相同。

| 验收内容 | 修改前 | 修改后及实际构建 |
| --- | --- | --- |
| 原餐厅 Now 与卡片，100%浅色 | [字体05](../testing/evidence/consumer-agent-repair-2026-10-10/phone/before-now-cards-100-light.png) | [APK01](../testing/evidence/consumer-agent-repair-2026-10-10/phone/after-now-cards-100-light.png) |
| 地点详情，100%浅色 | [字体05](../testing/evidence/consumer-agent-repair-2026-10-10/phone/before-place-detail-100-light.png) | [APK01](../testing/evidence/consumer-agent-repair-2026-10-10/phone/after-place-detail-100-light.png) |
| 城市，100%浅色 | [字体05](../testing/evidence/consumer-agent-repair-2026-10-10/phone/before-city-100-light.png) | [APK02](../testing/evidence/consumer-agent-repair-2026-10-10/phone/final-city-100-light.png) |
| 空输入键盘，100%浅色 | [字体05](../testing/evidence/consumer-agent-repair-2026-10-10/phone/before-keyboard-100-light.png) | [APK02](../testing/evidence/consumer-agent-repair-2026-10-10/phone/final-keyboard-100-light.png) |
| 本人空 Agent 页，一层标题，100%浅色 | [APK02双标题](../testing/evidence/consumer-agent-repair-2026-10-10/phone/before-navigation-agent-profile-current-100-light.png) | [APK03](../testing/evidence/consumer-agent-repair-2026-10-10/phone/final03-agent-profile-current-100-light.png) |
| 长中英地点详情安全区，320dp／200%深色 | [APK03实际侵入状态栏](../testing/evidence/consumer-agent-repair-2026-10-10/phone/final03-narrow-place-detail-200-dark.png) | [APK04准确详情](../testing/evidence/consumer-agent-repair-2026-10-10/phone/final04-narrow-place-detail-verified-200-dark.png) |
| 最新餐厅追问及卡片 | 本轮实际提交“有没有好的餐厅”与“第一家地址” | [APK04／API06，100%浅色](../testing/evidence/consumer-agent-repair-2026-10-10/phone/final04-followup-cards-100-light.png) |
| 重启后从最近对话恢复 | APK03实际force-stop/start，选择同一记录 | [回答、原地址与卡片](../testing/evidence/consumer-agent-repair-2026-10-10/phone/final03-restored-address-100-light.png) |
| 320dp／200%深色关键操作 | APK04 | [来源按钮](../testing/evidence/consumer-agent-repair-2026-10-10/phone/final04-narrow-detail-source-200-dark.png)／[城市选择](../testing/evidence/consumer-agent-repair-2026-10-10/phone/final04-narrow-city-200-dark.png)／[键盘输入框](../testing/evidence/consumer-agent-repair-2026-10-10/phone/final04-narrow-keyboard-200-dark.png) |
| 显示设置已恢复 | APK04，1220×2656、字号1.0、night no | [原始收据与卡片](../testing/evidence/consumer-agent-repair-2026-10-10/phone/final04-restored-settings-cards-100-light.png) |

未用作通过的原始失败／误命名证据保留：`before-material-menu-100-light` 实际不是菜单；`final03-original-query-pinyin` 没有成功开启键盘，后来补 verified 才提交；`final04-narrow-place-detail-200-dark` 实际是 Now，采用上表后续 verified 详情。`final-first-address-100-light` 是真实普通POST协议失败，修复后新任务和SQL才通过。旧失败没有被替换成成功图片。

## 实际测试与结果

| 运行范围 | 实际结果／证据 |
| --- | --- |
| 根客户端6个测试文件 | **101 PASS**，exit0，32项本轮新增＋69相关回归。[root-check-06](../testing/evidence/consumer-agent-repair-2026-10-10/root-check-06/receipt.json)。涵盖本人／访客闭合上下文、主题切换、严格null拒绝、API06四轮原始包离线解析、地点和Memory布局。 |
| 文字／布局矩阵 | 地点6组合＋安全区1、Memory6组合＋失效1：320dp、长中文、中英混排、100／130／200%×浅／深、自然换行、48动作和滚动。使用组件夹具，不能叫真机6组合。安全区新增测试修改前FAIL（top20<inset36），修改后通过。 |
| 根定向静态分析 | 3生产＋3新测试，exit0、No issues；不代表全仓 analyze。 |
| 材料、图片及入口检查点 | **90 PASS**；27材料、32私人入口、12真实页面入口／退休、5卡片、9workspace图片及5菜单。[记录](../testing/evidence/consumer-agent-repair-2026-10-10/client-material/README.md)。此前字体报告的25＋5失败本轮已修复；旧报告保留当时状态。 |
| 最新导航检查点 | **18 PASS**，其中新增2、回归16；320dp100／200、一层AppBar、返回、撤权／ABA不复活。[导航记录](../testing/evidence/consumer-agent-repair-2026-10-10/client-material/navigation/README.md)。与90组重叠，不能累加成108。 |
| 材料／导航静态分析 | 零error；原 map_workspace warning/info保留，导航检查exit1含20原诊断。没有宣称整应用分析零问题。 |
| 后端3包定向测试 | **563 leaf PASS，0 FAIL／SKIP**；623含父事件不再相加，源码冻结、diff-check exit0。[tests-14-final](../testing/evidence/consumer-agent-repair-2026-10-10/backend-agent/tests-14-final/receipt.json)。 |
| API04公共核心HTTP／SQL | 42核心响应，45实际HTTP含认证；9访客续问、9新目标，本人5轮10消息5快照，6个错误引用409，401／400隔离，5本人新目标；原事实、计数与缓存指纹不变。[core receipt](../testing/evidence/consumer-agent-repair-2026-10-10/backend-agent/api-05/core-receipt.json)。不是API06全42重跑。 |
| API05过期原生错误 | 本人 `Prego的地址` 实际503 `place_answer_unavailable`；1查询＋2认证，共3HTTP，旧500修复；未获得新鲜原生事实。[receipt](../testing/evidence/consumer-agent-repair-2026-10-10/backend-agent/native-error-03/receipt.json)。 |
| API06本人历史协议 | 餐厅→第一家地址→sports→第一家地址；4 POST＋4 GET＋2认证共10HTTP，8消息／2快照，POST与GET权威List及逐轮SQL一致；不跨新主题误用旧15家。[receipt](../testing/evidence/consumer-agent-repair-2026-10-10/backend-agent/api06-signed-envelope-02/receipt.json)。 |
| 本轮手机任务的只读SQL | 4消息、2绑定正确的公开快照，同15家、事实、顺序和06:23时间；模型10→10、缓存1/version1/15、私人关联0及MD5不变。[手机SQL收据](../testing/evidence/consumer-agent-repair-2026-10-10/backend-agent/phone-final-history-proof-01/receipt.json)。该检查自身0HTTP，实际手机两问由根操作。 |
| 源码及原工作区保护 | 13生产＋13测试文件，本轮开始基线2018项文件字节，登记范围外差异0；APK280/API552输入不一致0，51图片hash一致。[清单](../testing/evidence/consumer-agent-repair-2026-10-10/source-audit-01/MODIFIED-FILES.md)／[审计](../testing/evidence/consumer-agent-repair-2026-10-10/source-audit-01/receipt.json)。 |

独立实施审查01–06和最后只读验收复核未发现本轮新增阻断；没有重复运行付费请求或将模拟测试改称真实SQL并发。原普通地址失败、测试夹具错误、旧端口诊断和脚本错误日志全部保留。材料粘贴、来源退休、异步迟到、私密边界与导航变化对应 UX-CHECK-01/02/03/04/06/10/11/12/13/14/15 的限定场景；没有把所有UX业务动作门禁称为完成。

## 剩余工作与 NOT_RUN

- **模型服务仍未恢复验收**：账号状态／供应商成功调用、当前模型实际推荐质量、新费用账单；沿用既有 PROVIDER_APPROVAL、CREDENTIALS、SPEND_LIMIT 和 owner 限制。已向用户询问账号恢复状态，未收到答案不能当成新付费批准。
- **数据和通用 Agent 尚未完成**：全类别地点获取、实时评分／菜系／营业／预订、任意指代焦点、通用工具循环／自动长期私人学习、多模态全面处理及全部自由回答grounding；需要真实许可数据与对应业务实现。当前实现是可验的公共餐厅及原生客观字段范围。
- **全应用业务真机验收 NOT_RUN**：动态／作者详情、分享／创作草稿、非空私人Memory、旅程／主题地图全部业务分支、生产 OIDC／真实短信账户、多设备历史迁移、完整TalkBack、性能和地图闪烁逐帧测量。此前字体组件矩阵与本轮真机局部不能替代这些检查。
- **全量 Flutter、Go、数据库整检及 go vet NOT_RUN**；当前仅上述针对性范围。真实来源撤权竞态／多进程缓存并发、24小时等待、强制外源更新故障、过期Prego的新鲜全字段回复、503手机渲染均NOT_RUN。200%窄屏键盘/城市/详情已运行，当前APK04全6主题字号真机矩阵未运行。
- **首次触摸行为仍有不确定性**：ADB零时长合成tap曾未聚焦，保持100ms触摸成功。没有证据证明真实手指首次点击有同一缺陷，也没有宣称已修复它。

本期结果可核对的核心修复已落地；不能标记消费者发布ready或“全部旧任务完成”。已记录原任务为PARTIAL并释放实施lease，其他任务在该检查点前后逐项相同。实际队列：DONE180、PARTIAL15、BLOCKED14、TODO47，总256，仍有76项未完成；`next --parallel` 返回 candidates=[]、active_count=0、unleased_active=[]。[队列检查点](../testing/evidence/consumer-agent-repair-2026-10-10/queue-checkpoint-01/receipt.json)。保留依赖及供应商／发布门禁；这不是把76项都归因于同一个故障，也不表示剩余项目没有工作要做。
