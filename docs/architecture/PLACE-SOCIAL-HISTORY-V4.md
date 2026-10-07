# 地点近期公开社交历史

唯一合同：BT-V4-MOM-002；复用 Moment、Activity、Place 和 067 地点审核资料。没有新增私密 Memory、访问轨迹、出席事实、模型许可或来源台账。

## 来源与窗口

- 固定最近 30 天。Moment 使用本人明确公开时的 `published_at`；用户自述 `occurred_at`、收藏、RSVP 和活动结束均不证明到访。
- 摘要计数只覆盖当前 `published/public`、本人确认、精确关联本地点且作者仍 active 的 Moment。最多返回 5 条标题与 280 Unicode 字符正文节选，按公开时间排序。私人草稿和已撤回记录不参与计数。
- 不返回作者、私人关联、历史自述时间、坐标、参与人、Profile、Agent、Memory、聊天、图片或授权信息。明确公开的本人正文可能包含其自行填写的信息，发布前必须展示完整具体标题和正文供本人检查。
- 活动仅统计最近 30 天已结束且仍公开、未取消、来源仍可见的安排，按原 category 与主办方 time_zone 的工作日/周末分组；不是实际举办、到场、人数、热度或个人偏好的证明。Person/Community/Organization/Business 维持各自真实 typed organizer 边界；社区 archived、主办主体失效与 Business 经营关系撤销不能保留旧安排。
- 场所七字段直接复用 067 已审核且仍有效的 Facts/Source/Confidence；无有效来源时整个资料为 null，各字段未提供即 UNKNOWN。编辑 LOW/MEDIUM/HIGH 声明不是概率或实际体验保证。

## 原生公开确认

| 路径 | 合同 |
| --- | --- |
| `GET /v1/me/moments/{momentID}/publication` | 本人当前私人草稿的完整标题/正文/明确关联地点、revision 和 90 秒预览回执；不保存发布状态或审计记录 |
| `POST` 同一路径 | 严格三个字段 `revision`、`snapshot`、`confirmPublic:true`，确认具体预览当前版本，返回 native revision/status/publishedAt |
| `DELETE /v1/me/moments/{momentID}?revision=…` | 复用原本人当前会话与 revision 撤回方法；撤回后 private/withdrawn，不再公开聚合 |
| `GET /v1/places/{placeID}/social-history` | 只读地点当前公开摘要；匿名可读，已提供无效会话不能降为匿名 |

普通 Person 不要求 Agent 存在。本人发布不接受 owner/Agent/status/visibility/时间/用途等替代授权。组织工作区不借用个人私人记录。

预览回执由服务进程随机密钥签名，绑定当前 bearer digest、真实 Account epoch、Session ID、完整 Moment 保留行、City/Place epoch 及 PG 时间期限。调用者持有 bearer 不等于持有签名密钥。回执只是具体原生来源绑定，不能证明真人点击、认知目的许可、费用批准或其他领域动作。重启使尚未提交的短期预览失效，要求重新查看；已保存记录保持原生持久状态。此版本不支持跨实例共享预览密钥，跨进程重试会安全拒绝，不作多实例运营验收。

发布明确 READ COMMITTED、UTC；Account SHARE 与可选元数据锁先于 Moment 等待，Moment 与 City/Place 当前来源锁后才锁 Session；最后 PG 时钟在源更新与 audit 写后重新校验当前会话、主体、来源及期限，失效则整个事务回滚。CAS、重复提交、来源改动与 Account 停用后恢复不能复用旧批准。`published_at`、`author_confirmed_at` 与 `updated_at` 使用同一原生时刻。

原 064 outbox 只捕获私人 draft/withdrawn，不制造不存在的 Published 事件。公开更新的真实 revision/status 会使旧私人来源版本不匹配。撤回继续原控制事实路径，不开放认知处理。

## 摘要当前性与客户端

目标 City/Place 锁等待后，以同一最终 READ COMMITTED SQL / PG `checkedAt` 汇集 Moment、公开安排和 067 准确来源 epoch。带会话访问在返回前另做当前绝对/idle期限检查，目标失效则拒绝。摘要表示 checkedAt 的当前公开视图，不授持续访问权，不能承诺已经发出的网络数据被收回。

地点详情复用原 Place ID，不移动地图。中文公开摘要与本人记录直接路径保留；未完整提供真实 ID/revision/status 的旧 UI fixture 不获得发布按钮。先读预览、检查具体内容、勾选确认、提交；取消不提交。换已观测主体或地点、迟到响应和新预览均清除旧勾选。回调不能观察从未通知/读取的瞬间 ABA，最终写权限仍由真实原生当前会话与快照判断。

响应未知时不自动重复写：先 GET 本人原记录权威状态，分别显示当前已公开、已撤回或重新读取私人草稿预览。预览失败本身不能证明仍私人。撤回不能收回已经被他人看到或转发的内容。

账号/地点变化的处理先同步使旧预览、勾选和待提交状态失效，再在当前 widget 构建完成后的 microtask 通知弹窗；不在 `didUpdateWidget` 中同步触发另一个 route 的重建。审核资料在客户端同样拒绝重复声明、全未知空资料和带片段的来源 URL；客户端校验不替代后端当前权限与原生来源检查。

适用 UX-CHECK-01、02、03、04、05、06、07、08、09、10、11、12、13、14、15、16；实际测试/截图范围见本任务证据，规则接入不等于真机或 Closed Pilot 验收。

## 当前验证状态

当前 production1 已使用官方仓库真实三个注册路由、fresh001–068 随机独立数据库：两轮各 80 Test PASS、0 FAIL/测试 SKIP，目标 Go vet/build exit0，所有非空旧 public 完整行保持、13 个产品 Go 与本轮全 API 源 SHA 稳定，数据库已删除。正例由普通无 Agent 的人类 Create → Preview → Publish → 新 Store 公开读取 → 原生撤回完成；不以 raw SQL published fixture 作发布正例。

早期个人入口帧五目标 20 PASS、十个 Dart analyze0 保留在 worker-final1。根代理随后整客户端 analyze/test/Debug Build exit0（295 个具名功能测试，另 66 loading 事件），185 源与 APK94247 的证明属于该历史帧。发现实际组织工作区入口未传隔离状态后，不把该帧视为最终工作区验收。

当前 workspace-final4 五目标两轮各 28 个功能 PASS、0 FAIL/SKIP（每轮另 5 loading），11 个获分配 Dart Flutter analyze0；11 源与本轮167个 lib/test Dart SHA稳定，冻结13Go仍逐hash相同。完整新工作区帧 Flutter/Build/真机尚由根代理单独执行。所有初次失败均保留。

当前完整默认并发 Go 回归、新工作区完整 Flutter/test/Debug APK、真机截图、App/API 进程重启分别由根代理提供实际结果，不能从目标范围 PASS 或旧 APK 推断完成。所有本轮数据库资料为本地明确合成数据；生产身份、CSSA 真实供给、HTTPS/正式地图配置与正式 A→H 尚未获证，Closed Pilot / Consumer Beta Ready = NO。证据见 [本任务证据](../testing/evidence/place-social-history-2026-10-03/README.md)。

## 地图实际工作区入口补强（workspace-final4）

实际 MapWorkspace 地点结果/地图详情入口传递当前 Organization ID 和 Auth/Organization 合并监听。可选 placeDetailClient/base 仅为依赖注入，未提供时仍使用原生产配置，不添加匿名重试或替代权限。保留现有 NOT Inbox 接线。

- 组织工作区不读取 `/me/moments`、publication/status，也不调用原 Person-viewer `/places/{id}/activities`（该投影可能含当前人合法可见的私人成员活动，不能标为公开组织结果）。只读取原公开 Place、Venue 和隐私安全 social-history；带 bearer 仍执行后端当前会话规则，不降匿名。个人聊天分享、商家关注/举报按钮在组织模式隐藏。公开地点 OS 分享、导航、公开关联组织仍保留。
- 账号、组织、目标地点变化立即清除私人列表/活动/批准/权威结果显示。原请求捕获 token、workspace、Place 和本地观测 epoch；监听已发生的 Person→Org→Person 或账号 ABA，旧响应即使 token 相同也不能覆盖新读取。回到个人工作区必须重新读取，不复用旧 preview/checked。界面隔离不授后端权限。
- 组织切换时已经发出的写请求无法撤销；客户端丢弃其迟到结果，不报告当前组织成功，也不重复写。权威结果不确定时须回本人工作区重新核对。
- 真实 Widget 正例从 Map 输入、结果点击、Sidebar 组织菜单选择进入详情；测试组织列表/HTTP均为明确合成 transport fixture，证明实际接线而非实际组织 membership。原生身份/版本的领域验收仍由独立 PG 测试证明。
- 实际负控仅移除地图向详情的两行 workspace getter/notifier 传递，其余实现/测试不变，组织后私人读次数由应保持1变为2，功能测试失败；恢复字节后通过。单独同主体 invalidate 的旧 resolvedStatus=published RED 也保留，现明确清除。
- 390×844真实入口 Widget无异常。早先800×600入口测试发现现有 Sidebar 50px overflow，未在本次租约修正，不声称横屏/低高度布局通过；360px大字公开确认/摘要的已有专项仍为自己的实际证据。TalkBack/外接键盘仍 NOT_RUN。

## 当前真实工作区手机证据

新188 Client完整analyze/test/Debug0、316具名PASS/69loading单列；实际手机base.apk SHA与保存7d2a7ce… APK一致。正常UI本地登录并创建新LOCAL_QA私人草稿，通过实际Map Place入口预览取消零领域写，UI创建本地QA组织/真实owner会员后实际入口无私人成果或公开批准；回个人重新读取，再具体rev1确认公开/native rev2/公开摘要count1，具体rev2原DELETE撤回/native rev3/private/摘要count0。旧私人记录910107…revision5完整不变，没有SQL公开或生产身份/组织核验造假。

Android字体1.6滚动可达主要操作，已实恢复1.0；13Go+11Dart冻结SHA保持。当前243文件手机/native/API/源码不可变档案与详细限制见 [DEVICE-VALIDATION](../testing/evidence/place-social-history-2026-10-03/DEVICE-VALIDATION.md)。App/API进程重启、TalkBack、外接键盘、横屏未跑；真实试点和Closed Pilot仍NO，不能把本地Debug/合成供给表述为生产验收。
