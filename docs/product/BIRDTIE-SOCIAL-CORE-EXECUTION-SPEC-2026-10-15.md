# Birdtie 动态通知和好友闭环执行清单

日期：2026-10-15，Asia/Shanghai。执行仓库：`D:\Project\birdtie`。

目标是让用户从现有页面完成动态发布、查看、修改、删除，以及通知处理、好友申请和管理。先完成这些普通操作，再补 Civu 中已有的相关动态浏览、互动、媒体和串文能力。Agent、模型富集和运营推送不作为普通操作的前置条件。

本清单接入原 `BT-FIX-NOW-UI-001`，编号 SOC-01 至 SOC-20 是该任务内部的执行检查点，不是新 live 任务，也不另存进度。唯一 live 状态仍为 `automation/codex_task_queue.json`。当前快照为 256 项：183 DONE、47 TODO、12 PARTIAL、13 BLOCKED、1 IN_PROGRESS；历史 DONE 和全部未提交工作保留。**清单完成不表示功能完成。** 本次交付是代码对账、实施要求和执行接入，没有新增功能包或真实流程验收。

## 当前应保留的实现

| 能力 | 当前工作树事实 | 接续动作 |
|---|---|---|
| Now 动态创建 | 已有同输入框内的创建预览、144 私人草稿保存、revision/snapshot 绑定的文字发布；公开发布尚无持久原操作回执 | 补原发布事实、找回、刷新和异常恢复，保留当前首页 |
| 草稿修改与删除 | 原 MomentControl 处理私人草稿及原操作回执 | 继续复用，不能以草稿编辑称公开动态编辑完成 |
| 已发布正文修改 | 144 UPDATE 和旧 writer 均限定 draft/private | 新增公开修改领域契约及客户端接线 |
| 公开查看 | 已有单条详情、地点历史、当前地图范围的作者内容 | 补本人管理入口；范围摘要不称完整历史 |
| 公开删除或撤回 | 原撤回后端及地点详情入口已存在，结果为 withdrawn/private | 从本人记录和公开详情直接可达；不重写撤回后端 |
| 公开图片 | 145 已有独立的一张获准公开派生图、详情及地图投影 | 接 Now 真实选图；私人图片上传不等于公开图片发布 |
| 本人记录列表 | 原 owner 读取最多 100 条，公开行无正文编辑入口 | 先补操作入口，后补完整分页 |
| 通知 | 现有组件已有分页、点击标已读和主要目标跳转 | 修剩余未读计数、候选审核结果入口及操作后刷新 |
| 好友申请与关注 | 已有申请、接受、拒绝、撤销；好友与关注分开读取 | 保留原领域动作，核对真实返回和跨页状态 |
| 好友管理 | 移除、屏蔽、举报等组件存在，但 Now 的旧管理回调未被调用 | 把已有动作接到可达的好友行或详情 |
| 私信与实体分享 | 已有好友会话、文字及媒体代码、受控实体分享 | 先跑通文字与同 ID 分享，不重建消息平台 |
| 城市或关注动态流、点赞评论、串文 | 当前没有相应完整领域闭环 | 在核心操作后按下面检查点实现，不能用地图摘要或普通多次发布替代 |

当前事实对应 [首页创建交接](../reports/BIRDTIE-NOW-CREATION-REPAIR-2026-10-15.md)、[持续执行报告](../reports/BIRDTIE-CODEX-EXECUTION-2026-10-14.md) 和本文件末尾代码定位。旧报告中的“尚未实现”按其当时源码理解，不能覆盖今天已完成的 144、145、146、147 接线。

## 执行顺序和共同边界

1. 当前执行者先在正在写入的功能上保存自然安全检查点，再转入本清单；不取消在途工作，不覆盖其他聊天已冻结或正在编辑的文件。
2. 第一批完成 SOC-01 至 SOC-11。优先接文字 CRUD、通知和好友管理；单图按现有 145 合同接线。先编码，再运行必要的直接验证，统一编译一次。
3. 第二批完成 SOC-12 至 SOC-17 的普通动态浏览、互动和通知管理。按真正接口依赖安排，不能要求 AIR 模型或 AGE 富集先完成才能发普通动态。
4. 第三批完成 SOC-18、SOC-19 的公开多媒体和原子串文；缺真实媒体供应商时继续本地合同与接线，实际媒体送达独立记录。
5. SOC-20 统一对账当前源码和产物，再记录真实运行覆盖；缺设备不暂停本地编码。系统推送、真实认证、媒体运营和发布各自保留原门槛。

只读参考 Civu 的行为和边界，Birdtie 实现使用自己的 Account、Moment、Connection、Follow、Conversation、Inbox 和权限真源。Civu 的互关关系、旧 Session、公开许可和隐式问候不能成为 Birdtie 授权。具体结论见 [复用矩阵第 11 节](../architecture/CIVU-REUSE-MATRIX.md)。

Now 保留联系＋通知、短头像列、局部透明材质、唯一地图与输入框；不新增常驻大面板或新的收件箱入口。普通页面与 Now 使用同一实体 ID 和领域动作。界面使用简体中文，正常系统字号，不录屏，不安排大字号专项；中英混合语音继续延期。

## 第一批 动态基本操作通知和好友

### SOC-01 当前客户端和服务能配对运行

**方式：REUSE。前置：当前源码安全检查点。** 核对客户端 API 来源、当前服务版本、账号与个人工作身份。复用现有本机 QA 和登录流程；正常 token 续期保留本人草稿，退出、换账号或工作身份变化退休旧批准和异步响应。运行条件失效要准确显示未登录、离线或服务异常。

完成条件：当前客户端确实读取当前服务；访客不发私有请求；登录后的目标页面可达。固定测试身份、真实认证和旧用户认领分别记录。外部认证缺口不阻塞后续本地编码，不通过关闭鉴权解决。

### SOC-02 Now 发布文字动态后能立即找回

**方式：EXTEND。前置：SOC-01；复用 144 与原 publication。** 动态入口 → 可编辑正文 → 保存同一私人草稿 → 展示主体、受众和关联地点 → 明确发布 → 权威回执 → “查看这条动态”。发布结果必须包含原 Moment ID，并刷新相关本人记录、地点历史和地图来源。

144 草稿已有原 operation；原公开 publication 只有 revision/snapshot/confirmPublic，当前 controller 读取 Moment status 只能确认当前状态，不能证明那次发布。第一批须沿现有发布事务补稳定 operationId、不可变原发布事实、读取原结果及提交前停止的桥接；不能靠重新 GET 看到 published 就关闭 UNKNOWN，也不新增万能效果账本。原成功事实只保存核实所需的最小 metadata，不能保存正文、媒体或批准凭据。

完成条件：保存草稿与发布分开表达；双击和响应丢失沿各自原 operation 核实；失败保留正文；发布成功可打开同 ID 详情。第一批沿当前公开契约要求 canonical Place，不猜地图中心或当前位置。无 Place 的公开发布在 SOC-12 接续。

### SOC-03 本人记录和公开详情有直接操作入口

**方式：EXTEND。前置：SOC-02。** 从现有个人入口找回本人记录；草稿打开原编辑器，公开记录打开权威详情。先接本人公开记录的删除和现有图片管理；SOC-04 领域动作就绪后接正文编辑，不能先启用占位按钮。他人记录仅显示当前允许的阅读、分享及安全操作。

完成条件：Now 成功回执、本人列表、地点历史和地图选择都进入同 ID；返回保留地图、输入和选择。当前最多 100 条的读取边界必须准确表达，不把它称全部历史；完整分页归 SOC-13。

### SOC-04 修改已发布动态正文

**方式：NEW 的领域能力，EXTEND 的页面。前置：SOC-03。** 在 Moment 领域补公开正文修改：本人当前身份、稳定 Moment ID、expectedRevision、完整具体编辑稿、预览批准、一次提交、不可变原操作回执和未知结果核实。保持 ID 不变；版本冲突保留编辑稿并展示最新事实。

不得仅放宽 144 的 draft/private 检查：原 ledger 约束也限定私人 UPDATE，必须新增适配迁移并成套修改输入、事务、路由和客户端。迁移编号实施前重新读取，不改已应用文件。受众或位置变化按独立权限规则重新预览，不隐式扩大公开范围。

公开照片必须同时处理：145 在 Moment revision 变化时退休并清除旧缩略图。编辑预览要展示继续公开的具体 photo ID/version/SHA；仅在本次明确批准且原权利仍有效时，同事务锁定原 photo 版本、权利与当前合法像素，在正文变更后按本次批准的新媒体 version 与新 source binding 重新绑定，否则诚实显示图片将撤回并保留重新添加入口。不能只更新 approved_moment_revision 或修改已应用的 145；原操作回执须对应文字与媒体的原子结果。不能保存正文后意外丢图，也不能放松旧版本授权来保图。

完成条件：本人编辑后各入口读到新版本；他人、旧 revision 和旧批准不能写；原操作核实不靠当前正文相等猜成功。

### SOC-05 从列表或详情删除动态

**方式：EXTEND，复用现有撤回后端。前置：SOC-03；与 SOC-04 共享版本策略。** 本人直接打开删除确认，说明当前可见内容将停止展示；公开删除沿既有 144 WITHDRAW 的当前版本批准和原操作回执，旧 raw 撤回入口接到同一受控动作。收到权威回执后退休本人列表、公开详情、作者列表、地点历史、地图及关联公开图片的旧投影，不重写撤回事务。

当前动作是软撤回至 withdrawn/private，并从正常列表排除。“删除动态”、删除草稿、只移除图片和仅停止公开必须按实际结果命名，不能承诺立即物理清除原始媒体或撤回后自动变成可编辑草稿。媒体保留和清理继续走原权利与 retention 合同。

完成条件：取消确认零写入；本人成功删除后同 ID 不再公开；他人无权删除；未知结果只核实原操作，不能先清空本地再报成功。收藏和通知中的旧引用应显示失效对象，不能泄露缓存正文。

### SOC-06 Now 单图动态不漏图

**方式：EXTEND。前置：SOC-02、SOC-04 的媒体版本策略；复用 145。** Now 已选图片显示缩略图、移除和处理状态。把本次真实图片选择接到现有 PublicMomentPhotoController 和公开派生图预览，公开批准绑定具体图片、当前 Moment revision 与受众；不将私人上传结果自动升级为公开许可。

完成条件：用户预览的单图确实出现在同一公开动态详情和地图；仅在该确切文字或媒体结果成功后清对应草稿材料。文字成功、图片失败时分开显示结果并提供恢复，不能显示整条图文已发布。视频和多图仍按 SOC-18 的新合同，不能悄悄只发文字。

### SOC-07 通知读取后计数正确并到达真实结果

**方式：FIX，复用现有 InboxPanel。前置：SOC-01。** 保留已有分页、点击标已读及申请、活动、会话、社群、Agent 任务、商家和组织邀请跳转。标已读后重读现有权威全局未读数，保留当前列表和分页位置；计数未知不能按 0 消掉剩余未读徽标。

补 `activity_candidate`、`place_candidate` 审核结果入口：进入本人提交或审核结果，分别显示 rejected、linked_duplicate、published 的事实；只有真实 published/linked ID 才能打开正式对象。机会通知已有 targetActivityId，不重做该路由。已删除、撤权或屏蔽的目标显示简短失效状态，不恢复旧敏感摘要。

完成条件：多条未读中读一条仍显示剩余权威计数；通知直接处理更新原请求或实体；点击候选结果不只变已读。聊天未读继续归联系或会话，不重复制造通知。

### SOC-08 好友申请到处理结果形成闭环

**方式：REUSE 并定点修复。前置：SOC-07。** 搜索人、扫码或本人码核对身份 → 发申请 → 对方现有通知 → 接受或拒绝；发起人可撤销尚未完成的申请。复用现有请求、决定预览和原操作回执，结果返回后刷新原请求与当前好友投影。

完成条件：角色能执行各自允许的动作，重复点击不建重复请求；接受形成当前 Connection，拒绝和撤销不授权会话；旧请求、屏蔽和账号切换后的迟到结果不能复活关系。Birdtie 不新增“必须先互关”前置，也不自动发问候。

### SOC-09 好友管理和关注操作真实可达

**方式：FIX，复用现有关系与安全组件。前置：SOC-08。** 现有 NowSocialEdge.onOpenManagement 是未调用的兼容字段，不能把它视为已有可达入口。将 SocialInboxSection 中的移除好友、屏蔽、举报及现有亲密好友管理接到好友行菜单或稳定详情；不新增收件箱导航。

好友与关注保持独立语义。公开资料关注或取消关注、接受请求、移除、屏蔽后，退休旧行并重新读取当前联系人投影；取消屏蔽沿现有设置路径，不能自动恢复好友、Close 或私信许可。

完成条件：用户从当前页面完成管理并读到真实结果；移除或屏蔽后会话发送权限同步失效；只有当前合法数据进入好友和关注列表。

### SOC-10 好友能开会话并分享这条动态

**方式：REUSE 并补刷新。前置：SOC-08、SOC-09、SOC-03。** 从真实好友打开既有会话，完成文字发送、历史读取、已读更新和返回；从公开动态分享同一 canonical Moment 引用到允许的好友或会话。复用原消息 writer、FriendConversationController 和 143 实体分享控制。

完成条件：收件人可检查，Agent 输入和真人会话草稿不串用；超时沿原消息或分享 operation 核实；收到同 ID 内容；对方已屏蔽、好友已移除或内容已删除时不能发送或泄露旧快照。图片和音频现有代码保留，真实设备未运行不扩大本批验收结论。

### SOC-11 第一批统一编译并交付包

**方式：DELIVERY。前置：SOC-01 至 SOC-10 的本地接线及必要直接检查。** 将关联接口和客户端改动合并一个当前源码帧，编译实际需要的 API、Android APK 和 Web。源未改的工具或平台不重复编译；没有交付要求或环境的平台记录 NOT_RUN。

完成条件：记录源码与产物路径、SHA、API 来源、命令、目录和退出码。已有调试会话支持的 Dart/UI 用热重载；包的构建成功和真实用户完成流程分别报告。真实媒体缺口保留，不因此停止不依赖它的文字、通知和好友编码。

## 第二批 完整浏览互动和通知管理

### SOC-12 支持仅关联城市的公开动态

**方式：NEW 的公开契约。前置：SOC-02、SOC-04、SOC-05。** 私人草稿已允许 PlaceID 为空，公开端当前不允许。成套扩展 publication、公开 reader、模型、严格 wire、详情和分页，使用户明确选定的当前合法城市能成为公开上下文；Place 为可选关联。

第一版城市动态仅公开文字。145 的照片 source 仍强制 Place，单图界面必须准确显示此能力边界；城市无 Place 的照片和视频在 SOC-18 成套扩展 source/current/metadata、投影与严格客户端模型，不能直接沿用原 Place 图片批准。

完成条件：无 Place 动态可以发布、修改、查看和删除；不伪造地点、当前位置或拍摄坐标；无合法空间锚点时不画地图点。城市字段、地点字段及显示文案不能只在客户端放宽。

### SOC-13 本人和作者历史可完整分页找回

**方式：EXTEND。前置：SOC-03、SOC-05。** 为本人记录读取补稳定时间序与 ID 次排序、绑定 owner/查询的游标、真实分页和刷新；作者公开历史使用当前阅读者权限过滤，不受地图范围或最多 30 个投影点限制。旧 owner 接口保持明确兼容边界。

完成条件：超过 100 条本人记录仍可找回；草稿和公开筛选准确；翻页不重复、不泄露撤回内容。原 PublicAuthorMoments 范围摘要继续保留并准确命名。

### SOC-14 城市关注和好友动态采用有界列表

**方式：NEW，沿 City Graph 读取。前置：SOC-12、SOC-13、SOC-09。** 通过现有二级浏览路径提供明确城市、时间范围、作者或关系筛选，有限分页及稳定详情；不增加 Now 常驻 Feed 或复制 Civu 推荐排序、曝光模型。

公开、关注和好友受众必须以 Birdtie 当前权限判断。第一版可先提供“筛选当前可公开动态”的关注作者列表，若扩展 followers/connections 可见动态，须同时补 Moment 受众合同；不能借 Profile 的五档可见规则自动授予 Moment 权限。

完成条件：关注或好友筛选页在取消对应关系后退休旧成员；普通城市 PUBLIC 列表仍按当前公开阅读权限显示，不能因取关或解除好友隐藏仍公开可读内容。屏蔽、受众失权和删除按各自当前权限退休旧对象；范围与排序可检查；地图摘要不是列表总量，空与错误分开表达。

### SOC-15 动态点赞和评论

**方式：NEW。前置：SOC-04、SOC-05、SOC-14。** 在 Moment 领域实现点赞/取消、评论发布/读取/本人删除，并接稳定详情。读取及写入复验动态受众、账号状态、Block、正文限制和原反滥用边界；计数来自当前合法集合。

完成条件：重复点赞不重复计数，评论操作有稳定 ID 和未知结果核实；作者删动态后互动入口失效。地点偏好 `liked` 不是动态点赞，不复用为业务事实。回复层级等额外形态先沿明确合同，不扩成无关社交平台。

### SOC-16 动态互动生成真实通知

**方式：NEW，扩展原 Inbox 闭集。前置：SOC-15、SOC-07。** 点赞、评论等真实领域事件进入既有收件规则，明确接收者、去重、本人事件抑制和当前受众；客户端严格类型与目标路由同步升级，跳到当前合法动态和评论。

完成条件：通知来自成功领域写入，失败或提案不生成成功通知；同操作不重复通知；失权或删除后去掉旧敏感快照。当前 Inbox 没有 Moment 互动类型，不能只添 UI 枚举称通知已接通。

### SOC-17 通知筛选标记和清理

**方式：EXTEND 或有理由的 NEW。前置：SOC-07、SOC-16。** 在已有展开通知组件中核对分类、仅未读、明确全部已读、删除单条和清理已读能力。已有实现复用；缺少的服务端动作绑定本人、分类和明确快照边界，并返回权威计数。

完成条件：看见通知不自动清全部未读；批量动作不误处理并发新增通知；清理通知不删除对应动态、好友请求或会话。删除/撤权目标仍保持安全记录与准确结果。

## 第三批 公开媒体串文和最终接续

### SOC-18 多图和视频随动态公开

**方式：NEW 的公开媒体合同，复用既有存储边界。前置：SOC-06、SOC-04、SOC-12。** 定义有序附件、稳定 media ID/version、公开派生、版权/扫描/尺寸与时长、当前受众、失败恢复和撤回。视频材料的私人 purpose 不得直接当公开附件；公开前展示具体就绪材料。

完成条件：同一动态详情实际呈现批准的全部附件；修改或删除时正确退休旧媒体；部分成功如实呈现。真实供应商、扫描及权利证据缺失记录 NOT_RUN/BLOCKED，不以样例封面、测试 fixture 或选取预览冒充可发布媒体。

### SOC-19 原子发布串文并保持顺序

**方式：NEW 的领域能力。前置：SOC-18、SOC-04、SOC-05、SOC-13。** 建立 Birdtie 自有 thread/root/order 关系，共享明确受众，每条有独立正文、媒体及合法位置；一次具体预览、稳定提交 ID 和同事务发布，提供同 thread 的详情及历史定位。

完成条件：重试不生成重复串文，部分失败不留假完整结果；每条顺序、附件和位置可读；修改、删一条或删整串的权限与影响先明确，不能把 Civu 原子创建等同原子编辑。循环发布普通动态不能记为串文完成。

### SOC-20 合批交付恢复和条件验收对账

**方式：DELIVERY。前置：各批次实际代码检查点。** 对当前可完成批次生成配对源码和产物；核对重启找回、普通认证续期、换账号、晚响应和操作结果未知。保留首失败、修复及本次未运行范围，不为每个小项重复全仓测试、全量分析、数据库整检、装包和截图。

系统推送已有 gateway/controller/API 和有限 dispatch worker，默认关闭；只补实际缺少的接线或运行安排。供应商配置、系统权限、调度和真实后台送达各自满足原授权门槛，站内通知可用不能称系统推送可用。不自动启动新 scheduler、付费 provider 或向真实用户试发。

完成条件：每个检查点分别记录代码实现、受影响流程验证、视觉交互验收、实际交付。正常字号的窄屏、键盘、长文本、操作可达和辅助技术在必要整合点检查；只保存必要真实截图，不录屏。设备/真实认证/媒体/远端/iOS/生产未运行如实记录。核心包交付不必等第三批外部条件，Closed Pilot/Consumer Beta 继续按原门槛判断。

## 代码定位和原任务映射

下面路径用于实施前审计，不是整目录写许可。省略前缀的客户端路径相对 `apps/client/lib/src`，API 路径相对 `apps/api`；同格只有文件名时位于前一个已注明目录。只有执行根代理登记精确文件后才能写；共享 DTO、DDL、Moment 媒体版本及 Now 宿主按真实依赖串行衔接。独立 worker 只实施被分配文件，不领取或改队列。

| 检查点 | 原任务来源 | 优先读取的真实路径 |
|---|---|---|
| SOC-01/02/06 | BT-FIX-NOW-UI-001；原 NOW008–013/C1、MOM/地图来源 | `apps/client/lib/src/workspace/map_workspace.dart`、`now_creation_draft.dart`、`now_creation_panel.dart`；客户端 `apps/client/lib/src/content/private_moment_controller.dart`、`moment_publication_controller.dart`；API `internal/postgres/moment_publication.go`、`internal/httpapi/moment_publication.go` |
| SOC-03/04/05/13 | BT-V4-MOM-001/002、BT-V5-AGE-023 的既有对象；不重开 DONE | `apps/client/lib/src/legacy/legacy_shell.dart`、`workspace/public_moment_page.dart`、`public_author_moments.dart`；客户端 `content/moment_control.dart`、`content/private_moment_controller.dart`；API `internal/content/moment_control.go`、`internal/postgres/moment_control.go`、`moments.go`、`human_moments.go`、`public_moments.go`；迁移 144/145 |
| SOC-06/18 | 原 MOM 和 Map2/145，不代表独立 AIR 媒体任务完成 | 客户端 `workspace/public_moment_photo.dart`、`public_moment_photo_editor.dart`、`public_moment_photo_controller.dart`；API `internal/content/public_moment_photo.go`、`internal/postgres/moment_photo.go`、`migrations/145_public_moment_photos.sql` |
| SOC-07/16/17 | BT-INB-001、BT-NTF-001/002、BT-V4-NOT-001、BT-V5-AGE-038 | 客户端 `workspace/inbox.dart`、`remote_inbox_source.dart`、`notification_destination_router.dart`；API `internal/inbox/page.go`、`internal/postgres/inbox_page.go`、`notification_routing.go`、`internal/httpapi/inbox.go` |
| SOC-08/09 | BT-V4-TIE-001/002、BT-V4-FOL-001、BT-V4-PRV-002、BT-V4-SAF-002/003 | 客户端 `workspace/connections.dart`、`connection_request_review_page.dart`、`now_social_edge.dart`、`now_social_edge_source.dart`、`public_person_page.dart`、`settings_page.dart`；API `internal/connection/model.go`、`internal/connection/decision_operation.go`、`internal/postgres/connections.go`、`internal/postgres/agent_connection_decision_operation.go`、`internal/httpapi/connection_decision_operation.go` |
| SOC-10 | BT-V4-CHT-001/002/003、BT-V4-E2E-001 的 PARTIAL 不自动解除 | 客户端 `workspace/friend_conversation_controller.dart`、`workspace/friend_conversation_thread.dart`、`workspace/connections.dart`、`workspace/entity_share_control_controller.dart`；API `internal/connection/model.go`、`internal/connection/entity_share.go`、`internal/postgres/chat_page.go`、`internal/postgres/chat_entity_share_control.go`、`internal/httpapi/chat_entity_sharing.go` |
| SOC-12/14/15/19 | 当前 UI 增量与原 MOM/Now/City Graph 对象；没有对应已完成互动或串文事实 | API `internal/content/moment.go`、`public_moment.go`、`internal/postgres/public_moments.go`、`map_public_author.go`、`map_projections.go`；客户端 `workspace/public_moment_api.dart`、`public_moment_page.dart`、`public_author_moments.dart`；新增文件实施前登记 |
| SOC-11/20 | 原 BT-FIX-NOW-UI-001、V4 E2E/发布检查点 | 当前源码与构建 receipts；`system_push_gateway.dart`、`system_push_controller.dart`、`system_push_api.dart`、`apps/api/cmd/system-push-worker/main.go`；保留原 AUTH/MIG/AIR-LIVE 及发布门槛 |

关键证据：公开正文 UPDATE 限制在 `apps/api/internal/postgres/moment_control.go:295`，不是 content 输入文件；私人控制 ledger 见 144。公开读写强制 Place 见 `postgres/moment_publication.go:48` 与 `public_moments.go:62`；公开图片 revision 退休见 145:97。本人记录上限见 `human_moments.go:217`；地图公开动态投影上限及排序见 `map_projections.go:107`。这些行号是本次审计快照，实施时用符号重新定位。

通知计数差额见 `inbox.dart:420`、`map_workspace.dart:4394/6555`；候选通知见 `notification_routing.go:252/266` 与 `inbox.dart:445`。好友管理死回调见 `now_social_edge.dart:38/75` 和 `map_workspace.dart:4936/7162`。申请和主要通知路由已存在，不能按枚举未匹配就推断全部缺失。

## Codex 领取执行和记证

当前环境的 `python` 可能是 Windows 应用别名，使用已提供的运行时：

```powershell
$birdtiePython = 'C:\Users\chens\.cache\codex-runtimes\codex-primary-runtime\dependencies\python\python.exe'
& $birdtiePython automation/taskctl.py validate
& $birdtiePython automation/taskctl.py summary
& $birdtiePython automation/taskctl.py next
```

`next` 返回正在进行的 `BT-FIX-NOW-UI-001`，其 `social_core_execution_20261015` 指向本文件及执行顺序。接续该项，**不重复 start，不将 SOC 编号交给 start，也不自动 reset PARTIAL/BLOCKED**。所有队列写入带 `--coordinator root`，使用原持久锁和读取 hash 比较；冲突后重新读取，不强制覆盖。

每个批次执行者先列 3–8 条实施步骤、精确文件、接口/迁移依赖和必要直接验证，再编码。批次完成后把命令、cwd、退出码、源码帧、产物 SHA、首失败与未运行范围写入原持续报告及已有证据体系；更新原父任务的增量证据。本文不再维护一份状态表。

采用 [权威交互规则](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md) 的 UX-CHECK-01/04/05/06/07/08/09/10/11/12/13/14/15/16；预填和缺项交互涉及 02/03 时再纳入。开发只运行改动必要的直接验证；真实流程、视觉和发布验证在必要整合点分开记录。UX-CHECK-14 按最新范围只含正常字号、窄屏、键盘和辅助技术。

记录用四个维度：代码 `IMPLEMENTED/PARTIAL/NOT_IMPLEMENTED`，流程 `PASS/FAIL/NOT_RUN`，视觉 `PASS/FAIL/NOT_RUN/NOT_INSPECTED`，交付 `ARTIFACT_READY/INSTALLED/NOT_RUN`，并附实际来源。它们是证据标签，不替代原 live 状态。不能以旧测试、静态审计或编译推导本次流程通过，也不能因本清单单个检查点完成关闭整个父任务。

来源范围：本次用户指示、Birdtie 当前未提交树，以及只读 `D:\Program\Civu\Civu-client` 和 `Civu-server` 的指定社交流程。没有从 Civu 或 `D:\BirdTie` 复制业务代码、数据、Session、媒体或文档；本文件为 Birdtie 原创执行规范。方案接入凭据位于 `docs/research/evidence/social-core-plan-2026-10-15/receipt.json`。
