# BirdTie 字体与文字层级统一：第一组真机核对

日期：2026-10-10（Asia/Shanghai）。关联既有 UI 修复 **BT-FIX-NOW-UI-001 / PARTIAL**，未创建第二条队列，未修改 live queue。正式工作区是 `D:\Project\birdtie`，保留已有未提交修改。字体工作在餐厅历史／缓存修复的自然检查点继续进行。

**本批完成 Now、城市选择、原生地点卡片、外部地点卡片与 Now 通用实体结果卡的文字调整，并提供真机截图。全应用推广等待用户核对第一组截图；不能将本批称为全应用视觉验收通过。** 无部署、无 ZIP、无字体服务、无数据库或 API 改动。

## 字体来源与“大、粗、黑”的来源

- `birdtieTheme` 在 `apps/client/lib/src/app/birdtie_surfaces.dart` 使用 Material TextTheme 与 `NowUiTokens.fromTheme`。旧角色为面板标题20/w600、地点卡18/w600、城市入口16/w600、正文16/w400、辅助12/w400。部分局部卡片／标题另用 w700；未映射的 `headlineSmall` 在动态与作者页仍可产生24号标题。并非本地运行模式把文字放大。
- `pubspec.yaml` 未声明应用字体资产，未引入字体下载服务；一般应用正文未指定新的 fontFamily。测试中的 Android Material 逻辑字体名为 `Roboto`，fontFamilyFallback为空，由平台解析回退。
- 真机 `/system/etc/fonts.xml` 配置的 sans-serif 为 `MiSansVF_Overlay.ttf`；简体中文回退包含 `MiSansVF.ttf`，后续还有 MiSansL3 与 NotoSansCJK。配置 SHA256 为 `c59d2ea59a0fd32e2969263452235405140fa8ba9a6f47f504e5791dd9c45757`。这是实际设备字体配置证据；逐字形 Skia 最终选用字体的追踪 **NOT_RUN**，不能把逻辑 Roboto 名称当作中文实际渲染字体的证明。
- 地图样式自己的街道／POI文字由 Mapbox 管理；`map_marker_bitmap.dart` 的 Roboto22/w700用于地图聚合标记。它们未作为应用正文缩放。
- 第一批颜色之前主要继承 onSurface，使整段回答、卡片标题、地址与来源之间区分较弱。现在通过同一语义角色集中映射标题、正文与辅助颜色，而非页面散落硬编码。

## 第一批最终文字规范

参数单位为 Flutter 逻辑像素，系统字号100%。保留系统 TextScaler，未用 Transform.scale 或全局倍率。复用既有字体，通过现有 TextStyle.copyWith 保留字体与中文回退。

| 语义角色 | 字号 | 字重 | 行高 | 使用方式 |
| --- | ---: | --- | ---: | --- |
| 页面主标题 | 20 | w600 | 1.4 | `pageTitle`，已建立角色；本批实际城市面板使用面板标题 |
| 面板标题 | 18 | w600 | 1.5 | 城市选择、Now结果面板、外部地点详情 |
| 地点／实体卡片标题 | 16 | w500 | 1.5 | PlaceAnswerCard、ExternalPlaceCandidateCard、AgentEntityResultCard |
| 正文／Agent／用户回答／地址 | 15 | w400 | 1.5 | 普通段落不整段加粗，长中文与英文混排自然换行 |
| 输入框 | 16 | w400 | 1.5 | 保持现有1–4行编辑器与动态键盘预算 |
| 城市入口、筛选／菜单标签 | 15 | w500 | 1.5 | 胶囊可随长名称增长，移除强制单行省略 |
| 按钮／来源操作 | 14 | w500 | 1.5 | 文字变小，操作目标仍至少48×48 |
| 时间／来源／辅助说明 | 13 | w400 | 1.5 | 保留来源和时间，允许换行 |

浅色标题 `#20242A`、正文 `#374151`、辅助 `#566273`，按钮仍使用已有 primary。辅助色较用户建议的 `#667085` 深，是因为同时检验浮动半透明面板及已有带色用户气泡后，较浅候选未达到4.5:1。深色独立使用暗色 ColorScheme 的 onSurface / onSurfaceVariant / primary；外部卡片背景也映射到当前主题，避免暗色时残留浅色卡片。

实际主题色与94%／98%面板合成背景、黑白极端地图底色及 secondaryContainer 的标题／正文／输入／辅助文字对比度：最低浅色 **4.8096:1**、深色 **5.4643:1**。这是颜色计算验证，未把截图的抗锯齿边缘像素当作文字主体对比度。具体覆盖角色见 `now_typography_roles_test.dart`。

全局 TextTheme 暂保留原值；本批限定入口采用 `NowUiTokens.reviewedOf`。用户确认后将同一角色推广到其余页面，并收敛为全局规范，不提前改变未核对的页面。

## 修改文件

这次生产改动相对于上一餐厅修复构建的清单仅有以下10个文件。其余生产文件、字体、Android配置、pubspec与全局 Theme 的 hash 均保持一致；该证据比与已有大量未提交修改混合的 HEAD diff 更准确。

| 文件（`apps/client/lib/src/workspace/` 下） | 本批修改 |
| --- | --- |
| `now_ui_tokens.dart` | 集中字号／字重／行高／语义色，保留全局旧主题直到截图确认 |
| `top_controls.dart` | 城市入口15/w500，长标题自然换行，保留48触控和实测高度 |
| `map_workspace.dart` | 仅城市选择面板接入18标题、16输入、15标签、13地区说明 |
| `place_answer_card.dart` | 原生地点事实卡接入统一角色，保持原点击／来源动作 |
| `external_place_candidates.dart` | 每张外部地点卡16标题／15地址／13来源；详情18标题；卡片支持暗色背景 |
| `agent_conversation.dart` | Now普通回答15/w400、用户文字、状态与来源说明统一 |
| `agent_composer.dart` | 输入16、材料菜单15正文／13反馈，保留编辑器身份、焦点与键盘逻辑 |
| `agent_result_sheet.dart` | 测量与绘制使用相同18标题／13说明；移除标题固定两行省略 |
| `agent_answer_sources.dart` | 来源操作14、辅助13，操作目标48、来源标题可换行 |
| `agent_entity_result_card.dart` | Now唯一生产调用的通用实体卡16/w500，13辅助，14按钮与48操作 |

新增测试：`now_typography_roles_test.dart`、`now_typography_city_cards_test.dart`、`now_typography_phase1_test.dart`、`now_typography_typed_cards_test.dart`。已有 `agent_result_sheet_layout_test.dart` 与 `now_ui_v2_layout_composer_test.dart` 更新旧字号期望与实际可滚动夹具，未改变生产面板／自动跟随逻辑来满足测试。

## 构建与同设备真实前后截图

真机 `c641566b`，1220×2656，density520，约375逻辑像素宽。原始 PNG 直接来自 Android screencap，未裁剪、未合成、未录屏。每张 PNG 附同名 JSON，包含 APK SHA、设备、时间、截图 SHA 与当时无障碍树。

| 构建 | APK SHA256 | 用途 |
| --- | --- | --- |
| 修改前：餐厅修复 client-build-03 | `fb7ad25620d34c84556f86f33b942a611f31e33d2fe4a684e844a94dc7acdf7c` | 字体基线；为对齐同一段回答再次安装并保留应用数据 |
| 字体候选 client-build-01 | `00c6c53a9d08c8cb5f09fdf1d790521013f79f4dd6eee287e659e54f2ae0d2da` | 六个主题／字号组合的初次真机检查 |
| 字体最终 client-build-02 | `c114eea27fc22b41aabae24734065bf5406a9a284d5ddfaf532c0fa96b383e1c` | 补齐通用实体卡后最终100%对照及200%深色再检查；当前手机安装版 |

两次字体构建均 exit0、构建过程中源码不变；最终源码仍与02清单相符。补漏只改通用实体卡与材料菜单3处文字角色；候选版的其余字体／布局文件与最终版相同。第一次漏项发现后已补测试并重新构建，未把01当作最终全覆盖版本。

第一组：同一设备、浅色、系统100%、同一保存对话／同一份15家餐厅资料（采集时间06:23）。Now截图已对齐同一段回答和首两张卡片，未发送新查询或刷新资料。

| 页面 | 修改前 | 修改后（最终02） |
| --- | --- | --- |
| Now回答与首两张地点卡片 | [前](../testing/evidence/typography-phase1-2026-10-10/phone/before-final-now-cards-100-light.png) | [后](../testing/evidence/typography-phase1-2026-10-10/phone/final-now-cards-100-light.png) |
| 城市选择 | [前](../testing/evidence/typography-phase1-2026-10-10/phone/before-final-city-100-light.png) | [后](../testing/evidence/typography-phase1-2026-10-10/phone/final-city-100-light.png) |
| 中文需求与实际键盘 | [前](../testing/evidence/typography-phase1-2026-10-10/phone/before-keyboard-100-light.png) | [后](../testing/evidence/typography-phase1-2026-10-10/phone/final-keyboard-100-light.png) |

输入框前后内容同为“有没有好的餐厅”；100%键盘展开后编辑区均为原生 `[221,1403][999,1533]`，发送按钮均 `[999,1390][1155,1546]`，即48逻辑像素触控。Now无键盘的同内容截图编辑区位置也相同。键盘组的列表滚动偏移／地图焦点未锁定：该组可验证同一输入内容、输入位置和实际IME，**不称为整帧完全同构图对照**。

候选01真实字号与主题检查：

| 系统字号／主题 | Now或地点卡片 | 城市选择 | 键盘 |
| --- | --- | --- | --- |
| 100%浅色 | [Now](../testing/evidence/typography-phase1-2026-10-10/phone/after-now-cards-100-light-ready.png) | [城市](../testing/evidence/typography-phase1-2026-10-10/phone/after-city-100-light.png) | [键盘](../testing/evidence/typography-phase1-2026-10-10/phone/after-keyboard-100-light.png) |
| 130%浅色 | [Now](../testing/evidence/typography-phase1-2026-10-10/phone/after-now-cards-130-light.png) | [城市](../testing/evidence/typography-phase1-2026-10-10/phone/after-city-130-light.png) | [键盘](../testing/evidence/typography-phase1-2026-10-10/phone/after-keyboard-130-light.png) |
| 200%浅色 | [回答](../testing/evidence/typography-phase1-2026-10-10/phone/after-now-cards-200-light.png)、[滚动后的完整卡片](../testing/evidence/typography-phase1-2026-10-10/phone/after-cards-200-light.png) | [城市](../testing/evidence/typography-phase1-2026-10-10/phone/after-city-200-light.png) | [键盘](../testing/evidence/typography-phase1-2026-10-10/phone/after-keyboard-200-light.png) |
| 100%深色 | [卡片](../testing/evidence/typography-phase1-2026-10-10/phone/after-now-cards-100-dark.png) | [城市](../testing/evidence/typography-phase1-2026-10-10/phone/after-city-100-dark.png) | [键盘](../testing/evidence/typography-phase1-2026-10-10/phone/after-keyboard-100-dark.png) |
| 130%深色 | [卡片](../testing/evidence/typography-phase1-2026-10-10/phone/after-cards-130-dark.png) | [城市](../testing/evidence/typography-phase1-2026-10-10/phone/after-city-130-dark.png) | [键盘](../testing/evidence/typography-phase1-2026-10-10/phone/after-keyboard-130-dark.png) |
| 200%深色 | [卡片](../testing/evidence/typography-phase1-2026-10-10/phone/after-cards-200-dark.png) | [城市](../testing/evidence/typography-phase1-2026-10-10/phone/after-city-200-dark.png) | [键盘](../testing/evidence/typography-phase1-2026-10-10/phone/after-keyboard-200-dark.png) |

最终02额外检查：[200%深色城市](../testing/evidence/typography-phase1-2026-10-10/phone/final-city-200-dark.png)、[实际键盘](../testing/evidence/typography-phase1-2026-10-10/phone/final-keyboard-200-dark.png)、[滚动后完整卡片与两项48操作](../testing/evidence/typography-phase1-2026-10-10/phone/final-cards-reachable-200-dark.png)、[实际点击详情后的长文本](../testing/evidence/typography-phase1-2026-10-10/phone/final-place-details-200-dark.png)。200%内容超过一屏时允许滚动，未用字号钳制、固定卡片高度或标题省略隐藏它。

手机已恢复原来100%字号／浅色／原密度与尺寸；测试未发送的需求已通过重启清除，原有保存对话保留并重新打开。[最终恢复截图](../testing/evidence/typography-phase1-2026-10-10/phone/final-restored-now-100-light.png)。

所有42张原始截图（包括未用于验收的中间截图）校验 SHA 通过。`after-now-cards-100-light` 尚未打开历史；`before-final-keyboard-100-light` 切到输入法语音面板；`final-keyboard-query-candidate-100-light` 的输入准备未就绪、操作到达卡片。这三张未作为验收对照，原证据保留，未改名掩盖或替换图片。

## 实際测试与结果

Flutter命令均使用 `D:\DevTools\flutter\bin\flutter.bat`，cwd `D:\Project\birdtie\apps\client`，`--no-pub --reporter expanded`。本批30个新增字体／窄屏测试通过，另224个直接相关既有回归有最终PASS证据；独立材料文件27项为22 PASS / 5 FAIL。按唯一测试计算，**276 PASS / 5 FAIL**，未将重复运行叠加。

| 实際运行 | 结果／证据 |
| --- | --- |
| `flutter test --no-pub test/now_typography_roles_test.dart test/now_typography_city_cards_test.dart test/now_ui_tokens_test.dart test/now_p0_chrome_test.dart test/external_place_candidates_test.dart --reporter expanded` | 首轮52 PASS / 2 FAIL，包含夹具缺import与辅助颜色对比度不足，原日志保留。修正后 roles4 PASS；cities/cards12 PASS；其余49已PASS。[root日志](../testing/evidence/typography-phase1-2026-10-10/root/) |
| `flutter test --no-pub test/now_typography_roles_test.dart test/now_typography_city_cards_test.dart --reporter expanded`；随后单独cities/cards重跑 | 前次4角色PASS、城市夹具late-final失败；修正仅测试夹具后12 PASS。320×780浅深100/130/200，长中文／中英文混排、自然换行、48操作实点、真实城市选择与地图Element身份保留 |
| `flutter test --no-pub test/now_v2_place_fact_test.dart --reporter expanded` | 20 PASS，原地点卡片动作／来源／大字滚动回归 |
| `flutter test --no-pub test/now_typography_phase1_test.dart --reporter expanded` | 9 PASS，320×780三档浅深，实际Conversation+Composer，模拟280dpIME、编辑器身份／焦点／草稿／发送可达、位置稳定 |
| 8个Now直接回归文件＋2个受影响文件的修正重跑 | 147 distinct PASS；首次143 PASS / 4 FAIL保留。3项旧样式期望更新；300%／极短视口用例改为双向实际阅读滚动，未改生产布局。完整命令与日志见[worker记录](../testing/evidence/typography-phase1-2026-10-10/now-worker/README.md) |
| `flutter test --no-pub test/now_typography_typed_cards_test.dart test/agent_entity_result_card_test.dart test/agent_result_sheet_test.dart test/public_query_field_evidence_card_test.dart --reporter expanded`；新增材料角色断言后再跑新文件 | 27 PASS＋新增断言后新文件5 PASS，重叠不累计。28 distinct补漏测试中15个sheet用例已计入前组；额外13项PASS。覆盖通用实体卡320宽三档字号／浅深／原动作／48触控，材料15正文／13反馈 |
| `flutter test --no-pub ... test/now_composer_material_test.dart ... --reporter expanded` | 该文件22 PASS / **5 FAIL**，下文列明，未伪装PASS |
| Root精选分析：tokens、top_controls、place_answer_card、external_place_candidates及两新测试 | exit0 No issues。map_workspace较宽分析有原unused-element warning与info，未为字体做无关清理 |
| Worker精选分析与补漏分析 | 无error／warning；分别8条／1条旧info，exit1；新的测试精选分析exit0。详见日志 |
| Impeccable typography detector、限定文件 `git diff --check` | detector `[]`；diff-check exit0。机械扫描不替代真实UI检查 |
| 两次APK debug构建＋安装、原始截图与最终源文件哈希校验 | PASS；最后源码等于02构建清单、安装SHA相同、原设置已恢复。无正式发布 |

### 尚未通过的材料测试

3项城市／scope用例：切换城市使 taskEpoch变化，但材料边界仅观察 taskIdentityEpoch，导致旧剪贴板读取失效后reading状态未复位；再次粘贴次数实际1而期望2；另一个用例期望切换城市清除已编辑材料，但当前保留。该生命周期／合同问题与字体角色没有直接因果，本批未改相关逻辑；首次出现时间与修改前执行对照 **NOT_RUN**。保留在既有UI修复范围，不能宣称这3项通过。

2项浅深大字IME用例：失败断言是 **composer.bottom**，旧期望 `720−260−16＝444`，现值 `720−260−8＝452`。Now V2无内容时使用8dp底距，`now_workspace_layout.dart` 与此前餐厅构建SHA一致。关闭材料提示可点击、顶部非负的前置断言通过；失败后的重试断言尚未执行。这不是因字体修改而把键盘位置退回旧规则，本批保留最新8dp规则，也没有擅自把旧测试改为通过。

## 保留的交互与未运行项目

- 生产改动限文字角色、换行与必要暗色卡片背景。未重写Now面板边界／单地图／唯一输入框／编辑器身份／键盘所有权／业务入口／卡片行为。
- 真机100%输入框位置、200%输入与卡片操作可达已检查；窄屏320与长中文／混排由真实Flutter组件测试覆盖。**320dp实体设备 NOT_RUN**：当前手机约375dp，未把widget窄屏当作另一台实体设备。
- 静态截图中地图可见，widget验证地图Element身份；**原生地图连续闪烁检测 NOT_RUN**，无录屏，不把静态画面当作无闪烁证明。
- 资料快照的餐厅卡片已真机验证；当前真实城市目录仅Aberdeen。长中文城市名、原生地点事实卡与通用实体卡的完整数据矩阵由widget覆盖；**这些合成测试内容的真机矩阵 NOT_RUN**。
- **动态详情、作者／原生地点完整面板、侧栏、旅程、主题地图、分享预览、创作草稿的字体修改及本轮视觉验收 NOT_RUN**，按用户明确的“先截图核对，再推广”顺序留到确认后。已读审计可见 public_moment_page／public_person_page headlineSmall24、sidebar28/w700、activity_plans20/w700及局部低对比辅助色等后续目标；不会把静态检索当作这些页面已完成。
- **全套仓库测试、API／数据库重测、性能压测、正式部署 NOT_RUN**。本批为前台字体调整，沿用已验证餐厅后台；没有重新联网生成餐厅建议。

详细证据：[最终receipt](../testing/evidence/typography-phase1-2026-10-10/final-receipt.json)、[构建清单](../testing/evidence/typography-phase1-2026-10-10/builds/)、[worker完整测试与失败日志](../testing/evidence/typography-phase1-2026-10-10/now-worker/README.md)。


## 2026-10-10 用户确认与后续推广

用户已核对第一组真机截图并回复“按这一版继续其余页面（推荐）”。第一阶段截图门槛已满足，继续在既有 BT-FIX-NOW-UI-001 中实施全局 Theme、其他页面和导航标题，不新建队列。本文保留第一阶段当时的构建与 NOT_RUN 状态；后续阶段结果以 [全应用字体统一报告](/D:/Project/birdtie/docs/reports/BIRDTIE-TYPOGRAPHY-UNIFICATION-2026-10-10.md) 为准。

补充第一阶段最终角色对比度复验日志 `root-contrast-final.log`：标题、正文、输入、用户文字、地址、导航标签、动作、卡片副标题与辅助说明全部纳入合成背景检查，浅色最低4.809627、深色最低5.464256，4项通过。该复验未增加独立测试计数。
