# 我的社交近况（BT-V4-NOW-003）

## 页面职责与直接路径

登录的普通个人在 Now 输入框上方或设置打开“我的社交近况”，查看有限、当前有权读取的社交信号，然后检查原活动详情或好友明确分享的意图。页面使用简体中文，返回原入口；组织工作身份提示切回个人。保留原初始设置、社交偏好、完善选择和活动管理入口。

适用 GLOBAL-UX-INTERACTION-CONTRACT 的 UX-CHECK-01–16：读操作整理当前来源，展示理由与限制；不追加无关问题。取消、换身份、晚响应、空态、失败恢复、手机时区、键盘、语义与大字体使用原 Material 组件。这里没有无限加载、自动滚动、点赞诱导或聚合内容持久账本。

## 真实来源与有限投影

| 信号 | 既有权威领域 | 展示限制 |
| --- | --- | --- |
| 好友明确分享的意图 | 当前 accepted friend request + exact 双方的 active Tie，与本人当前可见 ACTIVE SocialIntent 相交 | 最多 10 条；有期限，排除 PRIVATE、过期、非好友；generic 好友标签 |
| 好友主办的活动 | 本人启用 FIND_ACTIVITY + 原活动/地点可见规则，EXISTING_TIE/TIE_ORGANIZER | 最多 5 条 |
| 已加入社群活动 | 当前 active membership + active/published Community 与当前活动供给，SHARED_COMMUNITY/JOINED_COMMUNITY | 最多 5 条；普通 Member 不等于现实 Friend |
| 其他活动机会 | 原 activity-place-v2 engine 当前规则匹配 | 最多 10 条；公开关注不等于好友或组织核验 |

稳定 Activity ID 去重，reasonCodes 通过既有中文 allowlist，未知理由使用中性回退；不显示原始 reason、私密 Profile 名称、handle、好友长期兴趣、出席、距离或亲密度推断。源自身列表上限或页面截断时明确“本次只展示有限的近况”。

本人读页面没有自动创建或激活意图、报名、邀约、消息、Agent consent 或 Feature ON。打开好友意图重新读取当前关系和具体意图；打开活动重新读取机会与权威活动，使用原 ActivityDetailSheet 和原领域动作。管理本人活动意图返回后重新读取近况。

## 原生读取边界

原已注册 `/v1/me/ties`、`/v1/social-intents`、`/v1/social-intents/{intentID}`、`/v1/me/opportunities` 复用当前 Store；登录个人的读通过 `internal/socialnow` 的 human read 合同，不回退 owner-ID legacy Store。digest 来自唯一 Bearer，initialActor 来自服务器原生 AuthenticateHumanSocial；客户端 JSON 或 model/provider 的自证字段没有授权作用。匿名公开 SocialIntent 保留原公开读取路径。

PostgreSQL 新 human gateway 使用同一 ReadCommitted 事务。先取得实际源关系 ACCESS SHARE，再检查并锁定当前个人 Account SHARE，然后检查并锁定 Session SHARE；最后一条 payload SELECT 用 PostgreSQL `clock_timestamp()` 和同一 statement snapshot 核对 accepted request 精确双方、Block、意图状态与期限、社群 membership/publication、原活动 ACL、Activity/Place publication 与期限以及原 Follow 来源。复用原 `publishedActivityFrom` 和 `opportunity.Generate`，不启动另一套匹配、身份或授权账本。

ACCESS SHARE 是表关系读锁，与普通行更新兼容。它把受控源表等待放在 Session 持锁之前；不能把它当作行级撤权锁。最终 SELECT 是来源读取的线性化点：在它之前已提交的变化可见；在它之后提交的变化属于下一次读取，不能宣称 payload 永久新鲜。Account/Session 删除重建不能借旧 digest 跨本人绑定。SQL 会话时钟和输入时钟不来自客户端。

首次身份解析通过原生 HumanSessionStore 复用 AuthenticateOrganizationAgent 的真实 Account SHARE → Session UPDATE 锁 → 新 PostgreSQL clock eligible UPDATE，普通人无需 Organization/Agent 元数据。它检查当前未撤销、绝对与 idle 期限后才刷新 idle，不调用旧 AccessStore.Authenticate 的等待前时间 UPDATE。缺能力返回 503，不回退旧刷新。

JSON materialization 后使用同一实际 Session 源 ValidateHumanSocialResponse：分别以两条语句依序取得真实 Account SHARE、Session SHARE 锁后，复用既有 checkHumanMomentSession 的新 RC statement，用 PG clock 再校主体绑定、Account 状态、revoke、absolute 和 idle deadline；该最终验证不刷新 idle。经过验证后才写 HTTP payload；验证结束后的网络传输不承诺永久授权或撤回已经发送的字节。

全局 /me/ties 保留原 055 独立 displayName fieldAllowed 投影；获准时 name → handle → Birdtie 成员，拒绝时仅 Birdtie 成员。user_profiles.visibility=private 本身不覆盖独立字段许可，普通无 Agent caller 仍可读关系。SocialNow 卡仍使用“好友”标签。最后 payload 同帧读取 agent/metadata/field rule，依赖关系等待位于 Session 锁之前。

LOCAL CityID 与 COMMUNITY CommunityID 从真实 audience target 返回，不返回他人 invitee 全名单。LOCAL City 与显式 Context City 相交，冲突/过期/未公开即无供给；City、Community、Activity、Place 期限都使用同帧 PG clock。本人原列表先取 100 个（保留旧顺序）再筛选 ACTIVE FIND_ACTIVITY，visible 列表保持原 50，最终机会由旧 engine 排序与截断；页面的有限组上限保持独立。

普通本人读不依赖 PersonalAgent/Profile 存在，也不读取 PrivateProfile/Memory 或 Agent 关系上下文。它不授予机器分析、模型出网、长时兴趣披露或代理代办权限；Organization Agent 和 Business 的 activation 边界保留。首次合法 AuthenticateHumanSocial 会刷新会话 idle，所谓“只读”限定领域内容数据，不声称所有 SQL 无写入。

## 证据与完成边界

原始来源为 `BirdTie_V4_Execution_Package.zip!/BirdTie_TAPD_Backlog_V4.xlsx#Requirements`，原依赖 TIE001/OPP002。本任务仅交付当前可见的明确声明与规则活动信号；没有实现好友私密长期兴趣分析、现实出席证明、自动 Agent 关系理解或发布门槛。

客户端实际定向测试和 native RED/修复过程保存在 `work/v4-now003`；最终稳定帧、根独立复验与同 APK 真机证据完成后归档至 `docs/testing/evidence/social-now-2026-10-03`。源码实现、offline transport/widget 合同、真实隔离库权限证明和真机验收分别记录。最终本地稳定帧为 `native-final-green6` 与 `native-final-green7`：每轮 28 PASS/0 FAIL-SKIP（PG17、HTTP11），test/vet/build exit0，001–068 migration SHA、8 owned/461观察Go源、完整public内容均相等，两个owned DB已DROP。客户端目标13 PASS/analyze0、7Dart冻结对应根已保存EEA7 APK；根此前同帧完整Flutter analyze/test278/Debugbuild0是独立记录，不把后续其他任务client源当该APK。

正式 worker 档案：`docs/testing/evidence/social-now-2026-10-03/worker-final1/manifest.json`。根当前完整Go/独立native/同APK手机验收另存独立目录及收据，不重写worker manifest。根最终状态/发布分类仍由原队列决定；本地28项证明不等于生产部署、真实用户试点、现实参加或模型许可。
