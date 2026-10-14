# BirdTie 全应用字体与文字层级统一

日期：2026-10-10。沿用既有 **BT-FIX-NOW-UI-001 / PARTIAL**，正式工作区 `D:\Project\birdtie`。用户已核对第一组 Now／城市／地点卡片真机截图并确认“按这一版继续其余页面”。本轮已实际推广统一样式、验证并安装本地调试构建；没有新建队列、覆盖已有未提交工作、生成 ZIP 或部署。

**文字调整已完成。全应用所有业务分支的真机视觉验收尚未完成；旧私人图片入口有25项测试失败，第一阶段材料另有5项失败，均保留，不能宣称整个 UI 修复任务已通过。**

## 查明的来源与调整

此前 Material 默认 `headlineSmall` 24、局部22／24／28和w700，以及大部分文字沿用 onSurface，让页面出现大、粗、黑同时叠加。侧栏28/w700、收件箱28/w700、我的活动20/w700、动态／作者24标题均有具体调用来源。字体大小与本地运行方式没有因果关系。[第一阶段来源审计](BIRDTIE-TYPOGRAPHY-PHASE1-2026-10-10.md)。

应用未声明新的字体资产或字体服务。继续使用已有 Theme 字体；Android Material 逻辑字体名为Roboto，中文由平台回退。这台手机字体配置实际为 MiSansVF_Overlay、简体中文 MiSansVF，后续 MiSansL3／NotoSansCJK；逐字形Skia最终选用追踪为 **NOT_RUN**。截图夹具使用本机已有微软雅黑与 Flutter 已有 MaterialIcons，仅用于测试，不进入 APK、不冒充 Android 字体。

统一来源为 [NowUiTokens](../../apps/client/lib/src/workspace/now_ui_tokens.dart) 和 [birdtieTheme](../../apps/client/lib/src/app/birdtie_surfaces.dart)：既有 reviewed API 为同一角色的兼容别名，全局 TextTheme、ListTile、按钮、输入提示和局部页面使用相同角色。没有整体乘缩放系数。普通正文15/w400，强调限制在标题与少量操作。

Flutter AppBar 的默认标题缩放会在大字时限制到1.34。新增 [birdtieAppBar](../../apps/client/lib/src/app/birdtie_app_bar.dart)，迁移55个文件的70个标题调用，明确沿用外层系统 TextScaler。标题200%实际20×2=40，自然换行；极长标题超过可用视口40%时在标题区域滚动，保留完整语义及正文／48操作空间。导航、返回与菜单回调保持原行为。[独立测试和完整调用清单](../testing/evidence/typography-phase2-2026-10-10/app-bar/README.md)。

## 最终文字参数

系统字号100%，单位Flutter逻辑像素；系统放大继续生效。

| 角色 | 字号／字重 | 行高 | 浅色文字 |
| --- | --- | --- | --- |
| 页面主标题 | 20／w600 | 1.4 | #20242A |
| 面板标题 | 18／w600 | 1.5 | #20242A |
| 地点／动态／实体卡片及分区标题 | 16／w500 | 1.5 | #20242A |
| 正文／Agent回答／地址／用户需求 | 15／w400 | 1.5 | #374151 |
| Now输入 | 16／w400 | 1.5 | #374151 |
| 其他表单输入 | 15／w400，继承bodyMedium | 1.5 | #374151 |
| 城市入口／菜单／筛选 | 15／w500 | 1.5 | #20242A |
| 按钮／操作 | 14／w500 | 1.5 | 既有primary／按钮容器语义色 |
| 时间／来源／辅助说明 | 13／w400 | 1.5 | #566273 |

辅助色使用 #566273：建议的 #667085 在半透明背景及带色用户气泡未通过4.5；稍浅候选也未满足。颜色集中映射，深色独立使用暗色 ColorScheme 的 onSurface／onSurfaceVariant／primary／error。卡片、收件箱、地图错误与旧Profile的实际可达背景也适配主题。

实际颜色计算覆盖全部文字角色、错误文字、scaffold／surface／secondaryContainer，以及.94／.98面板叠加黑白底色。最低对比度：浅色 **4.8096:1**，深色 **5.4643:1**，4项测试PASS。[日志](../testing/evidence/typography-phase2-2026-10-10/root/root-contrast-error-final.log)。这是合成颜色验证；截图抗锯齿边缘逐像素测量 **NOT_RUN**。

布局同步收紧24→16等与旧大字号相关的内边距／段间距。长标题允许自然多行；草稿关联地点／活动／组织／社群选择器及社交意图卡片取消省略、菜单取消固定48行高，同时保留至少48操作目标。未禁用 TextScaler、未用 Transform.scale，未调整Now V2面板边界、单地图、唯一输入框或键盘规则。

## 页面与文件覆盖

本阶段相对于已确认的第一阶段APK02，生产变更 **75个文件**。完整路径与SHA：[修改文件清单](../testing/evidence/typography-phase2-2026-10-10/MODIFIED-FILES.md)、[构建逐文件对照](../testing/evidence/typography-phase2-2026-10-10/production-changes-final.json)。现有脏文件先保存字节快照，以增量diff审核；未用混合旧修改的HEAD diff冒充本次范围。

| 真实页面／组件 | 实际改动与证据 |
| --- | --- |
| Now、城市选择、原生／外部地点卡、通用结果卡 | 沿用用户确认的第一阶段规范；全局接入后再次跑26项窄屏／IME／卡片测试和真机截图 |
| 地点、动态、作者面板 | 标题20／18、分区16、正文15、辅助13，长标题自然换行；18矩阵＋25回归全部通过，[三页证据](../testing/evidence/typography-phase2-2026-10-10/public-panels/README.md) |
| 侧栏、我的活动、图层、地图实体预览、真实LegacyProfile | 全局角色和主题背景、适当间距、长标题；30矩阵＋活动6／图层1测试，组件截图及侧栏／图层真机截图 |
| 分享预览、意图草稿、动态发布、私人媒体选择、私人图片草稿 | 正文／操作／错误语义色，布局换行、48操作、焦点与草稿保留；30矩阵＋49回归全部通过，[证据](../testing/evidence/typography-phase2-2026-10-10/sharing-drafts/README.md) |
| 草稿关联选择器、社交意图卡片 | 最终复核发现的长标题遗漏已补齐，[选择器／卡片证据](../testing/evidence/typography-phase2-2026-10-10/long-titles/README.md) |
| 设置、账户、组织／社群／商家／Agent面板及其他导航页 | 复用全局角色，局部标题／辅助覆盖收敛，55文件70个AppBar保持系统缩放 |

当前产品中“旅程／主题地图”对应可达的“我的活动／地图图层”页面，未凭名称虚构不存在的独立页面。Mapbox街道文字、空间标记和聚合bitmap22/w700仍由地图标签体系管理；不可达的旧Now／Explore组件并未整块重写。真实LegacyProfile及其草稿分支已接入规范；不把不可达旧代码或地图标签当作普通正文。

## 构建与真实截图

真机 c641566b，1220×2656／density520，约375dp。所有手机PNG为原始Android screencap，未裁剪、未合成、未录屏；每张附时间、APK SHA、字号、主题、PNG SHA和附近时点无障碍树。状态栏时钟会变化；部分地图资料存在TTL，无障碍树与随后PNG之间可能变更，画面内容以原始PNG为准。

| 构建 | APK SHA256／用途 |
| --- | --- |
| 第一阶段02 | c114eea27fc22b41aabae24734065bf5406a9a284d5ddfaf532c0fa96b383e1c；本阶段之前基线 |
| 第二阶段02 | 2ccde09b5046f6277b2b049098293ee1ae4e4a0aedd38220796bd3c9f70dea6e；全局规范＋AppBar，100／130浅色和200深色真机检查 |
| 第二阶段03 | 521b9a7da527e06ca526fc8009d17431b0ac7e1a17a46b2656b3fae2f040264b；补齐两个长标题组件，其他APK输入与02完全相同 |
| 第二阶段05，最终安装版 | 1e125c0e48f352485d8f18bbf0814464b767efcde2a6fa0978b92bbe57074d20；仅规范化一个新增import的两处混合换行，逻辑行与03完全相同；最终100%真机复核 |

五次调试构建均exit0且构建期间输入源码不变。05安装APK SHA等于构建SHA，当前每项生产输入与05清单一致。[构建日志与清单](../testing/evidence/typography-phase2-2026-10-10/builds/)、[02→03精确两文件差异](../testing/evidence/typography-phase2-2026-10-10/builds/build-02-to-03.json)、[03→05只改换行](../testing/evidence/typography-phase2-2026-10-10/builds/build-03-to-05.json)。01为清理迁移尾空格前候选，未安装。04因修复脚本的shell引号失败，构建的是与03相同输入及相同APK，未安装；随后改为独立脚本完成换行修复并构建05，全部旧日志保留。

同设备、浅色100%对照如下。Now保留原“有没有好的餐厅”对话、15家餐厅、06:23采集时间，不发送新搜索。

| 页面 | 修改前 | 修改后／对应构建 |
| --- | --- | --- |
| 已确认的Now与地点卡第一组 | [前](../testing/evidence/typography-phase1-2026-10-10/phone/before-final-now-cards-100-light.png) | [第一阶段02](../testing/evidence/typography-phase1-2026-10-10/phone/final-now-cards-100-light.png) |
| 侧栏 | [第一阶段02](../testing/evidence/typography-phase2-2026-10-10/phone/before-sidebar-verified-100-light.png) | [最终05](../testing/evidence/typography-phase2-2026-10-10/phone/final-sidebar-100-light.png) |
| 地图图层，过期提示同内容 | [第一阶段02](../testing/evidence/typography-phase2-2026-10-10/phone/before-layers-100-light.png) | [第二阶段02](../testing/evidence/typography-phase2-2026-10-10/phone/after-layers-100-light.png) |
| 收件箱访客面板 | [第一阶段02](../testing/evidence/typography-phase2-2026-10-10/phone/before-inbox-100-light.png) | [最终05](../testing/evidence/typography-phase2-2026-10-10/phone/final-inbox-100-light.png) |
| 城市选择 | [第一阶段02](../testing/evidence/typography-phase2-2026-10-10/phone/before-city-verified-100-light.png) | [最终05](../testing/evidence/typography-phase2-2026-10-10/phone/final-city-100-light.png) |
| Now回答和首张卡片 | [第一阶段02](../testing/evidence/typography-phase2-2026-10-10/phone/before-now-cards-verified-100-light.png) | [最终05](../testing/evidence/typography-phase2-2026-10-10/phone/final-restored-now-100-light.png) |
| 实际键盘，空输入同内容 | [第一阶段02](../testing/evidence/typography-phase2-2026-10-10/phone/before-keyboard-empty-100-light.png) | [最终05](../testing/evidence/typography-phase2-2026-10-10/phone/final-keyboard-empty-100-light.png) |

第二阶段100%键盘组编辑器原生bounds前后均[221,1403][999,1533]，发送／添加按钮为156物理像素=48dp。保留唯一编辑器。地图构图／列表位置若存在差异不声称整帧完全一致；文字内容与设置相同。旧 `before-sidebar-100-light` 实际拍到Now，已保留并排除侧栏验收，重新安装原基线准确取证，没有替换旧图片。

第二阶段02追加：[130% Now](../testing/evidence/typography-phase2-2026-10-10/phone/after-now-cards-130-light.png)、[130%城市](../testing/evidence/typography-phase2-2026-10-10/phone/after-city-130-light.png)、[130%侧栏](../testing/evidence/typography-phase2-2026-10-10/phone/after-sidebar-130-light.png)、[200%深色侧栏](../testing/evidence/typography-phase2-2026-10-10/phone/after-sidebar-200-dark.png)、[城市](../testing/evidence/typography-phase2-2026-10-10/phone/after-city-200-dark.png)、[键盘](../testing/evidence/typography-phase2-2026-10-10/phone/after-keyboard-empty-200-dark.png)、[滚动后的完整卡片](../testing/evidence/typography-phase2-2026-10-10/phone/after-full-card-200-dark.png)、[实际点击详情](../testing/evidence/typography-phase2-2026-10-10/phone/after-external-detail-200-dark.png)。200%卡片操作均48dp、详情长文本自然换行，原来源按钮可达；没有打开外部浏览器。取证中一次键盘关闭未完成便滚动，产生未发送数字5；卡片该截图如实保留，结束时重启清除，未提交需求。

最终保留 **29张**原始手机截图及JSON，SHA全部校验通过。手机恢复原100%／浅色／原尺寸密度，原保存对话仍在、输入为空。[最终receipt](../testing/evidence/typography-phase2-2026-10-10/final-receipt.json)。

## 组件截图与验收边界

动态／作者／原生地点、分享／草稿等真实生产组件另外生成320dp截图，在相同内容与夹具条件下运行100／130／200%、浅深模式。明确标为 **NOT_PHONE／NOT_APK**：生产组件＋模拟HTTP响应＋测试字体，在Flutter测试引擎真实渲染，不能当作完整应用真机图。

- 动态三页最终18张：[动态100%](../testing/evidence/typography-phase2-2026-10-10/public-panels/after-final-appbar/moment-light-100-top.png)、[动态200%深色](../testing/evidence/typography-phase2-2026-10-10/public-panels/after-final-appbar/moment-dark-200-top.png)、[作者](../testing/evidence/typography-phase2-2026-10-10/public-panels/after-final-appbar/person-light-100-top.png)、[地点](../testing/evidence/typography-phase2-2026-10-10/public-panels/after-final-appbar/place-light-100-top.png)。旧before-confirm／after-confirm有严格相同测试字体前后图，但旧AppBar1.34限制是历史；已另存最终全缩放截图，不冒充新构建。
- 分享／草稿最终20张：[分享确认](../testing/evidence/typography-phase2-2026-10-10/sharing-drafts/widget-fixtures-final-appbar/WIDGET_FIXTURE_NOT_PHONE_light_100_share_confirmation.png)、[200%键盘草稿保存可达](../testing/evidence/typography-phase2-2026-10-10/sharing-drafts/widget-fixtures-final-appbar/WIDGET_FIXTURE_NOT_PHONE_dark_200_draft_keyboard_save_reachable.png)。原20历史截图保留。
- 侧栏／实体／图层／Profile／活动最终30张：[活动100%](../testing/evidence/typography-phase2-2026-10-10/root/widget-fixtures-final/plans-light-100.png)、[Profile200%](../testing/evidence/typography-phase2-2026-10-10/root/widget-fixtures-final/profile-dark-200.png)。长混排名称自然换行和滚动，操作回调实点、48触控检查。[fixture清单](../testing/evidence/typography-phase2-2026-10-10/root/widget-fixtures-final-manifest.json)。
- 两个最后补齐的长标题组件：同组件旧源码与新源码截图／失败对照，详见 [long-titles](../testing/evidence/typography-phase2-2026-10-10/long-titles/README.md)。

## 实际运行的测试

命令、环境、退出码和完整日志在各证据目录，不把重复执行累加。Flutter cwd为 `D:\Project\birdtie\apps\client`，测试使用 expanded reporter；root analyze从仓库根执行Dart。

| 本阶段独立覆盖 | 最终结果 |
| --- | --- |
| 公共动态／作者／地点：18新矩阵＋已有25 | 43 PASS |
| 分享／草稿：30新矩阵＋已有49＋旧私人入口32 | 86 PASS／25 FAIL；79字体及直接回归PASS，旧入口7 PASS／25 FAIL |
| root全局角色4／tokens5＋shell新矩阵30＋活动6／图层1＋Now整合26 | 72 PASS |
| AppBar新增独立10 | 10 PASS |
| 两个长标题最终补齐 | 18 PASS（12新矩阵＋意图卡4／草稿关联2） |

本阶段合计 **229 PASS／25 FAIL**；第一阶段另有5项材料失败。阶段间有重叠测试，不能简单与第一阶段276 PASS相加成整个仓库覆盖数。

root新的6文件分析与新的AppBar／公共页／分享草稿测试分析无问题。root最终较宽32文件＋Theme／tokens分析 **0 error、1原unused-element warning、20 info，exit2**；AppBar 57文件分析0 error、25条既有诊断。没有将非零退出码描述为全仓clean。[root分析](../testing/evidence/typography-phase2-2026-10-10/root/root-analyze-final.log)。长标题分析结果见自己的README。限定diff-check结果见最终receipt附件；静态字体detector结果[]仅机械检查，不代替视觉。

矩阵检查了320窄屏、长中文／中英文混排、三个系统字号、两主题、无溢出／省略、正文与元信息角色、实际滚动与点击。Now实际Conversation／Composer测试覆盖280dp模拟IME、编辑器身份／焦点／草稿／发送与地图Element身份；分享草稿测试覆盖260dp模拟IME、重试可达、取消不发POST。初始fixture import／timer清理／异步PNG捕获及旧样式期待失败日志全部保留，最终修正明确是测试夹具。PNG捕获首次停滞后结束本次自建测试进程，修正runAsync，第二次30项与30图全部通过，未影响应用进程。

## 保留失败与NOT_RUN

旧私人图片测试期望“打开快捷操作／个人资料”菜单，但当前NowV2材料按钮进入showNowMaterialMenu，私人图片仅存在于没有调用者的旧showMaterialTools。仅更新selector也不能到达原路径；诊断修改已以hash恢复，不为字体擅自改入口。**25 FAIL**保留在同一UI修复任务中，首次出现时间 **UNKNOWN**，缺少修改前执行证据，不能断言已证明是前置缺陷。

第一阶段另有 **5 FAIL**：3项城市／scope材料生命周期与reading锁／草稿边界，2项旧IME测试期望16dp底距而当前NowV2为空面板8dp。未为了测试改变已约定面板和键盘规则。[完整保留说明](../testing/evidence/typography-phase2-2026-10-10/root/preserved-failures.md)。

以下未运行，统一明确 **NOT_RUN**：所有登录业务分支的真机前后矩阵、原生动态详情／作者／原生地点完整详情真机图（有组件图）、所有剩余页面的真机视觉验收、320dp实体设备、02／05所有六组合完整真机矩阵、原生地图连续闪烁／帧时间检测、逐字形字体追踪、全仓测试、API／数据库重测、性能压测、正式部署。半透明面板已做主题颜色合成计算，原生地图闪烁不能由静态截图或widget Element身份证明。

当前成果为可审查的源码、独立测试、带构建身份的原始截图和完整日志。字体规范已推广；既有UI修复任务继续保持PARTIAL，没有将未运行项或保留失败标成通过。
